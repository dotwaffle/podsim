package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/dotwaffle/podsim/internal/session"
)

func TestEnabledSignals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                    string
		environment             map[string]string
		wantTraces, wantMetrics bool
	}{
		{name: "disabled"},
		{name: "common endpoint", environment: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318"}, wantTraces: true, wantMetrics: true},
		{name: "traces only", environment: map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://collector/v1/traces"}, wantTraces: true},
		{name: "metrics only", environment: map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://collector/v1/metrics"}, wantMetrics: true},
		{name: "metrics suppressed", environment: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318", "OTEL_METRICS_EXPORTER": "none"}, wantTraces: true},
		{name: "SDK disabled", environment: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318", "OTEL_SDK_DISABLED": "true"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			traces, metrics := enabledSignals(func(name string) string { return test.environment[name] })
			if traces != test.wantTraces || metrics != test.wantMetrics {
				t.Fatalf("enabled signals = %t, %t", traces, metrics)
			}
		})
	}
}

func TestInstrumentRequestExcludesHighRateAndHealthPaths(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path string
		want bool
	}{{path: "/api/state"}, {path: "/healthz"}, {path: "/api/project", want: true}, {path: "/game.html", want: true}} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, http.NoBody)
		if got := instrumentRequest(request); got != test.want {
			t.Fatalf("instrument %s = %t", test.path, got)
		}
	}
}

func TestSessionMetricsReportRetainedCheckpoints(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		checkpoints int
	}{{name: "none"}, {name: "limit", checkpoints: 8}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter(instrumentationName)
			snapshot := func() session.Metrics { return session.Metrics{Checkpoints: test.checkpoints} }
			if _, err := registerSessionMetrics(meter, snapshot); err != nil {
				t.Fatal(err)
			}
			var collected metricdata.ResourceMetrics
			if err := reader.Collect(t.Context(), &collected); err != nil {
				t.Fatal(err)
			}
			retained, ok := findMetric(collected, "podsim.checkpoint.retained")
			if !ok {
				t.Fatal("podsim.checkpoint.retained is missing")
			}
			gauge, ok := retained.Data.(metricdata.Gauge[int64])
			if !ok || retained.Unit != "{checkpoint}" || len(gauge.DataPoints) != 1 ||
				gauge.DataPoints[0].Value != int64(test.checkpoints) {
				t.Fatalf("podsim.checkpoint.retained = %+v, want one %d {checkpoint} gauge point", retained, test.checkpoints)
			}
		})
	}
}

func findMetric(collected metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, scope := range collected.ScopeMetrics {
		for _, data := range scope.Metrics {
			if data.Name == name {
				return data, true
			}
		}
	}
	return metricdata.Metrics{}, false
}
