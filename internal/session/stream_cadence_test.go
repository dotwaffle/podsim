package session

import (
	"testing"
	"time"
)

func TestStreamCaptureDeadline(t *testing.T) {
	t.Parallel()
	start := time.Unix(100, 0)
	for _, test := range []struct {
		name     string
		captured time.Time
		elapsed  time.Duration
		want     time.Duration
	}{
		{"initial", time.Time{}, 0, streamCaptureInterval},
		{"early wake", start, 10 * time.Millisecond, streamCaptureInterval - 10*time.Millisecond},
		{"early ticker jitter", start, streamCaptureInterval - time.Microsecond, time.Microsecond},
		{"exact deadline", start, streamCaptureInterval, 0},
		{"slow publication", start, streamCaptureInterval + 20*time.Millisecond, 0},
		{"future capture", start, -5 * time.Millisecond, streamCaptureInterval + 5*time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := streamCaptureDelay(test.captured, start.Add(test.elapsed)); got != test.want {
				t.Fatalf("capture delay = %s, want %s", got, test.want)
			}
		})
	}
}

func TestStreamRecoveryWakesKeepCaptureDeadline(t *testing.T) {
	t.Parallel()
	captured := time.Unix(100, 0)
	deadline := captured.Add(streamCaptureInterval)
	for _, elapsed := range []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond, streamCaptureInterval - time.Millisecond} {
		now := captured.Add(elapsed)
		if next := now.Add(streamCaptureDelay(captured, now)); next != deadline {
			t.Fatalf("wake at %s moved deadline to %s", elapsed, next)
		}
	}
}
