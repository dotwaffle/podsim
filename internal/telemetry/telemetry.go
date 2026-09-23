// Package telemetry configures optional OpenTelemetry export for the server.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/dotwaffle/podsim/internal/session"
)

const instrumentationName = "github.com/dotwaffle/podsim/internal/telemetry"

// Provider owns optional OpenTelemetry providers and HTTP instrumentation.
type Provider struct {
	traces       *sdktrace.TracerProvider
	metrics      *sdkmetric.MeterProvider
	registration metric.Registration
}

// New configures OTLP export from standard OpenTelemetry environment variables.
// Export remains disabled until an OTLP endpoint is present.
func New(ctx context.Context, version string, snapshot func() session.Metrics) (*Provider, error) {
	tracesEnabled, metricsEnabled := enabledSignals(os.Getenv)
	provider := &Provider{}
	if !tracesEnabled && !metricsEnabled {
		return provider, nil
	}
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName("podsim"),
			semconv.ServiceVersion(version),
		),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, fmt.Errorf("create telemetry resource: %w", err)
	}
	if tracesEnabled {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
		}
		provider.traces = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(res),
		)
		otel.SetTracerProvider(provider.traces)
	}
	if metricsEnabled {
		exporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			_ = provider.Shutdown(ctx)
			return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
		}
		provider.metrics = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
			sdkmetric.WithResource(res),
		)
		otel.SetMeterProvider(provider.metrics)
		if runtimeErr := otelruntime.Start(otelruntime.WithMeterProvider(provider.metrics)); runtimeErr != nil {
			_ = provider.Shutdown(ctx)
			return nil, fmt.Errorf("start Go runtime metrics: %w", runtimeErr)
		}
		provider.registration, err = registerSessionMetrics(provider.metrics.Meter(instrumentationName), snapshot)
		if err != nil {
			_ = provider.Shutdown(ctx)
			return nil, err
		}
	}
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider, nil
}

// Enabled reports whether any telemetry provider is active.
func (p *Provider) Enabled() bool {
	return p.traces != nil || p.metrics != nil
}

// HTTPHandler instruments low-rate application requests.
func (p *Provider) HTTPHandler(next http.Handler) http.Handler {
	if !p.Enabled() {
		return next
	}
	return otelhttp.NewHandler(next, "podsim.http",
		otelhttp.WithFilter(instrumentRequest),
	)
}

func instrumentRequest(r *http.Request) bool {
	return r.URL.Path != "/api/state" && r.URL.Path != "/healthz"
}

// Shutdown flushes telemetry and releases exporter resources.
func (p *Provider) Shutdown(ctx context.Context) error {
	var errs []error
	if p.registration != nil {
		errs = append(errs, p.registration.Unregister())
	}
	if p.metrics != nil {
		errs = append(errs, p.metrics.Shutdown(ctx))
	}
	if p.traces != nil {
		errs = append(errs, p.traces.Shutdown(ctx))
	}
	return errors.Join(errs...)
}

func enabledSignals(getenv func(string) string) (bool, bool) {
	if strings.EqualFold(getenv("OTEL_SDK_DISABLED"), "true") {
		return false, false
	}
	common := getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
	traces := common || getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != ""
	metrics := common || getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != ""
	if strings.EqualFold(getenv("OTEL_TRACES_EXPORTER"), "none") {
		traces = false
	}
	if strings.EqualFold(getenv("OTEL_METRICS_EXPORTER"), "none") {
		metrics = false
	}
	return traces, metrics
}

type sessionInstruments struct {
	tick, submitted, completed, pending                        metric.Int64ObservableGauge
	vehicles, activeVehicles, passengerVehicles, stoppedPods   metric.Int64ObservableGauge
	checkpoints                                                metric.Int64ObservableGauge
	passengerDistance, emptyDistance, averageWait, maximumWait metric.Float64ObservableGauge
}

func registerSessionMetrics(meter metric.Meter, snapshot func() session.Metrics) (metric.Registration, error) {
	instruments, err := newSessionInstruments(meter)
	if err != nil {
		return nil, err
	}
	return meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		state := snapshot()
		observer.ObserveInt64(instruments.tick, state.Tick)
		observer.ObserveInt64(instruments.submitted, int64(state.Submitted))
		observer.ObserveInt64(instruments.completed, int64(state.Completed))
		observer.ObserveInt64(instruments.pending, int64(state.Pending))
		observer.ObserveInt64(instruments.vehicles, int64(state.Vehicles))
		observer.ObserveInt64(instruments.activeVehicles, int64(state.ActiveVehicles))
		observer.ObserveInt64(instruments.passengerVehicles, int64(state.PassengerVehicles))
		observer.ObserveInt64(instruments.stoppedPods, int64(state.StoppedVehicles))
		observer.ObserveFloat64(instruments.passengerDistance, state.PassengerDistanceMeters)
		observer.ObserveFloat64(instruments.emptyDistance, state.EmptyDistanceMeters)
		observer.ObserveFloat64(instruments.averageWait, state.AverageWaitSeconds)
		observer.ObserveFloat64(instruments.maximumWait, state.MaximumWaitSeconds)
		observer.ObserveInt64(instruments.checkpoints, int64(state.Checkpoints))
		return nil
	}, instruments.observables()...)
}

func newSessionInstruments(meter metric.Meter) (sessionInstruments, error) {
	var instruments sessionInstruments
	var err error
	if instruments.tick, err = meter.Int64ObservableGauge("podsim.simulation.tick", metric.WithUnit("{tick}")); err != nil {
		return instruments, fmt.Errorf("create simulation tick metric: %w", err)
	}
	if instruments.submitted, err = meter.Int64ObservableGauge("podsim.journey.submitted", metric.WithUnit("{journey}")); err != nil {
		return instruments, fmt.Errorf("create submitted journey metric: %w", err)
	}
	if instruments.completed, err = meter.Int64ObservableGauge("podsim.journey.completed", metric.WithUnit("{journey}")); err != nil {
		return instruments, fmt.Errorf("create completed journey metric: %w", err)
	}
	if instruments.pending, err = meter.Int64ObservableGauge("podsim.journey.pending", metric.WithUnit("{journey}")); err != nil {
		return instruments, fmt.Errorf("create pending journey metric: %w", err)
	}
	if instruments.vehicles, err = meter.Int64ObservableGauge("podsim.pod.total", metric.WithUnit("{pod}")); err != nil {
		return instruments, fmt.Errorf("create total pod metric: %w", err)
	}
	if instruments.activeVehicles, err = meter.Int64ObservableGauge("podsim.pod.active", metric.WithUnit("{pod}")); err != nil {
		return instruments, fmt.Errorf("create active pod metric: %w", err)
	}
	if instruments.passengerVehicles, err = meter.Int64ObservableGauge("podsim.pod.passenger", metric.WithUnit("{pod}")); err != nil {
		return instruments, fmt.Errorf("create passenger pod metric: %w", err)
	}
	if instruments.stoppedPods, err = meter.Int64ObservableGauge("podsim.pod.stopped", metric.WithUnit("{pod}")); err != nil {
		return instruments, fmt.Errorf("create stopped pod metric: %w", err)
	}
	if instruments.passengerDistance, err = meter.Float64ObservableGauge("podsim.travel.passenger.distance", metric.WithUnit("m")); err != nil {
		return instruments, fmt.Errorf("create passenger distance metric: %w", err)
	}
	if instruments.emptyDistance, err = meter.Float64ObservableGauge("podsim.travel.empty.distance", metric.WithUnit("m")); err != nil {
		return instruments, fmt.Errorf("create empty distance metric: %w", err)
	}
	if instruments.averageWait, err = meter.Float64ObservableGauge("podsim.wait.average", metric.WithUnit("s")); err != nil {
		return instruments, fmt.Errorf("create average wait metric: %w", err)
	}
	if instruments.maximumWait, err = meter.Float64ObservableGauge("podsim.wait.maximum", metric.WithUnit("s")); err != nil {
		return instruments, fmt.Errorf("create maximum wait metric: %w", err)
	}
	if instruments.checkpoints, err = meter.Int64ObservableGauge("podsim.checkpoint.retained", metric.WithUnit("{checkpoint}"),
		metric.WithDescription("Save points that the session keeps in memory.")); err != nil {
		return instruments, fmt.Errorf("create retained checkpoint metric: %w", err)
	}
	return instruments, nil
}

func (i sessionInstruments) observables() []metric.Observable {
	return []metric.Observable{
		i.tick, i.submitted, i.completed, i.pending,
		i.vehicles, i.activeVehicles, i.passengerVehicles, i.stoppedPods,
		i.passengerDistance, i.emptyDistance, i.averageWait, i.maximumWait,
		i.checkpoints,
	}
}
