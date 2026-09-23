package telemetry

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
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

func TestSessionMetricsReportStateSaves(t *testing.T) {
	t.Parallel()
	// saving returns the metrics of a session that saves its state.
	saving := func(change func(*session.Metrics)) session.Metrics {
		metrics := session.Metrics{Tick: 1200, StateConfigured: true, StateEnabled: true}
		change(&metrics)
		return metrics
	}
	unsaved := func(seconds float64) func(*session.Metrics) {
		return func(metrics *session.Metrics) { metrics.StateUnsavedSeconds = seconds }
	}
	tests := []struct {
		name string
		// snapshots holds the session metrics of each collection.
		snapshots []session.Metrics
		// want holds the state series of each collection.
		want []map[string]float64
	}{
		{"no store", []session.Metrics{{Tick: 1200, Checkpoints: 1}}, []map[string]float64{{}}},
		{"no good save", []session.Metrics{saving(unsaved(30)), saving(unsaved(90))}, []map[string]float64{
			{"podsim.state.saves{result=ok}": 0, "podsim.state.saves{result=error}": 0, "podsim.state.unsaved": 30, "podsim.state.enabled": 1},
			{"podsim.state.saves{result=ok}": 0, "podsim.state.saves{result=error}": 0, "podsim.state.unsaved": 90, "podsim.state.enabled": 1},
		}},
		{"good and bad save", []session.Metrics{saving(func(metrics *session.Metrics) {
			metrics.StateSaves, metrics.StateSaveErrors, metrics.StateBytes, metrics.StateUnsavedSeconds = 1, 1, 4096, 30
		})}, []map[string]float64{{
			"podsim.state.saves{result=ok}": 1, "podsim.state.saves{result=error}": 1,
			"podsim.state.size": 4096, "podsim.state.unsaved": 30, "podsim.state.enabled": 1,
		}}},
		{"saved revision", []session.Metrics{saving(func(metrics *session.Metrics) {
			metrics.StateSaves, metrics.StateBytes = 2, 4096
		})}, []map[string]float64{{
			"podsim.state.saves{result=ok}": 2, "podsim.state.saves{result=error}": 0,
			"podsim.state.size": 4096, "podsim.state.unsaved": 0, "podsim.state.enabled": 1,
		}}},
		{"saving off", []session.Metrics{{StateConfigured: true, StateUnsavedSeconds: 600}}, []map[string]float64{{
			"podsim.state.saves{result=ok}": 0, "podsim.state.saves{result=error}": 0,
			"podsim.state.unsaved": 600, "podsim.state.enabled": 0,
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader := sdkmetric.NewManualReader()
			meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter(instrumentationName)
			// Collect runs the callback in the goroutine of the test.
			var collection int
			snapshot := func() session.Metrics { return test.snapshots[collection] }
			if _, err := registerSessionMetrics(meter, snapshot); err != nil {
				t.Fatal(err)
			}
			for index, want := range test.want {
				collection = index
				var collected metricdata.ResourceMetrics
				if err := reader.Collect(t.Context(), &collected); err != nil {
					t.Fatal(err)
				}
				if got := stateSeries(t, collected); !maps.Equal(got, want) {
					t.Fatalf("collection %d: state series = %v, want %v", index, got, want)
				}
			}
		})
	}
}

// stateSeries returns the value of each podsim.state series in collected.
// The key of a series with attributes adds them in braces. stateSeries also
// checks the data type and the unit of each state instrument.
func stateSeries(t *testing.T, collected metricdata.ResourceMetrics) map[string]float64 {
	t.Helper()
	kinds := map[string]string{
		"podsim.state.saves":   "metricdata.Sum[int64] {save}",
		"podsim.state.size":    "metricdata.Gauge[int64] By",
		"podsim.state.unsaved": "metricdata.Gauge[float64] s",
		"podsim.state.enabled": "metricdata.Gauge[int64] {1}",
	}
	series := make(map[string]float64)
	for _, scope := range collected.ScopeMetrics {
		for _, data := range scope.Metrics {
			if !strings.HasPrefix(data.Name, "podsim.state.") {
				continue
			}
			if kind := fmt.Sprintf("%T %s", data.Data, data.Unit); kind != kinds[data.Name] {
				t.Errorf("%s is %s, want %s", data.Name, kind, kinds[data.Name])
			}
			switch points := data.Data.(type) {
			case metricdata.Sum[int64]:
				if !points.IsMonotonic || points.Temporality != metricdata.CumulativeTemporality {
					t.Errorf("%s is not a cumulative counter", data.Name)
				}
				addPoints(series, data.Name, points.DataPoints)
			case metricdata.Gauge[int64]:
				addPoints(series, data.Name, points.DataPoints)
			case metricdata.Gauge[float64]:
				addPoints(series, data.Name, points.DataPoints)
			}
		}
	}
	return series
}

// addPoints adds the value of each point to series.
func addPoints[N int64 | float64](series map[string]float64, name string, points []metricdata.DataPoint[N]) {
	for _, point := range points {
		key := name
		if point.Attributes.Len() > 0 {
			key += "{" + point.Attributes.Encoded(attribute.DefaultEncoder()) + "}"
		}
		series[key] = float64(point.Value)
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
