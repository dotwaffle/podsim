package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/telemetry"
)

func TestRunStopsBeforeTelemetryAfterCancellation(t *testing.T) {
	// Exporter settings are process-wide, so this test and its cases are
	// serial. They must not depend on the developer's OTLP environment.
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	t.Setenv("OTEL_SDK_DISABLED", "false")
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_METRICS_EXPORTER", "otlp")
	for _, endpoint := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	} {
		t.Run(endpoint, func(t *testing.T) {
			for _, name := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"} {
				t.Setenv(name, "")
			}
			t.Setenv(endpoint, collector.URL)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			readyCalls := 0
			input := runInput{
				args:  []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t)},
				ready: func(string) { readyCalls++ },
			}
			if err := run(ctx, input); err != nil {
				t.Fatalf("canceled startup with OTLP enabled = %v, want nil", err)
			}
			if readyCalls != 0 {
				t.Fatalf("canceled startup opened listeners: ready calls = %d", readyCalls)
			}
		})
	}
}

func TestRunTelemetryStartupCancellation(t *testing.T) {
	t.Parallel()
	failed := errors.New("exporter configuration failed")
	for _, test := range []struct {
		name         string
		cancelBefore bool
		cancelDuring bool
		expired      bool
		failure      error
		want         error
		wantCalls    int
	}{
		{name: "already canceled", cancelBefore: true, failure: context.Canceled},
		{name: "already expired", expired: true, failure: context.DeadlineExceeded},
		{name: "canceled during initialization", cancelDuring: true, failure: context.Canceled, wantCalls: 1},
		{name: "active configuration error", failure: failed, want: failed, wantCalls: 1},
		{name: "active exporter cancellation", failure: context.Canceled, want: context.Canceled, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.cancelBefore {
				cancel()
			}
			if test.expired {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(ctx, time.Time{})
				defer stop()
			}
			calls, readyCalls := 0, 0
			input := runInput{
				args:  []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t)},
				ready: func(string) { readyCalls++ },
				newTelemetry: func(context.Context, string, func() session.Metrics) (*telemetry.Provider, error) {
					calls++
					if test.cancelDuring {
						cancel()
					}
					return nil, fmt.Errorf("create exporter: %w", test.failure)
				},
			}
			if err := run(ctx, input); !errors.Is(err, test.want) {
				t.Fatalf("startup = %v, want %v", err, test.want)
			}
			if calls != test.wantCalls || readyCalls != 0 {
				t.Fatalf("constructor calls = %d, want %d; ready calls = %d, want 0", calls, test.wantCalls, readyCalls)
			}
		})
	}
}

func TestRunTelemetryCancellationKeepsSavedState(t *testing.T) {
	t.Parallel()
	store := openInput{config: project.Default(), stateURL: fileURL(t.TempDir()), logger: slog.New(slog.DiscardHandler)}
	epoch := writeFinalState(t, store)
	args := []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t), "-state", store.stateURL}
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		readyCalls := 0
		err := run(ctx, runInput{
			args:  args,
			ready: func(string) { readyCalls++ },
			newTelemetry: func(context.Context, string, func() session.Metrics) (*telemetry.Provider, error) {
				cancel()
				return nil, ctx.Err()
			},
		})
		cancel()
		if err != nil || readyCalls != 0 {
			t.Fatalf("canceled telemetry startup = %v, ready calls = %d", err, readyCalls)
		}
	}
	shared, closeStore, err := openSession(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	t.Cleanup(func() {
		if err := closeStore(); err != nil {
			t.Error(err)
		}
	})
	state := shared.State()
	if state.Epoch != epoch || state.Restore.Tier != "physical" || state.Restore.Reason != "" {
		t.Fatalf("canceled starts lost the physical save: epoch kept %t, restore %+v", state.Epoch == epoch, state.Restore)
	}
}
