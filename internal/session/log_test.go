package session

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"log/slog"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
)

// logRecords decodes the JSON log records in buffer. It removes the time and
// the duration. It stops the test when a duration is missing or negative.
func logRecords(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range bytes.Lines(buffer.Bytes()) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode log record %q: %v", line, err)
		}
		if duration, ok := record["duration"].(float64); !ok || duration < 0 {
			t.Fatalf("log record %v has no valid duration", record)
		}
		delete(record, "time")
		delete(record, "duration")
		records = append(records, record)
	}
	return records
}

func TestCheckpointLogs(t *testing.T) {
	t.Parallel()
	checkpoint := Command{Action: "checkpoint"}
	tests := []struct {
		name string
		// prepare returns the command to check. The test does not check the
		// records of the commands in prepare.
		prepare func(*testing.T, *testClient) Command
		want    []map[string]any
	}{
		{
			name: "checkpoint",
			prepare: func(_ *testing.T, client *testClient) Command {
				advanceTicks(client.session, 30)
				return client.next(checkpoint)
			},
			want: []map[string]any{{
				"level": "INFO", "msg": "Saved checkpoint", "client": "test",
				"checkpoint": 1.0, "tick": 30.0, "projectRevision": 1.0, "retained": 1.0, "evicted": 0.0,
			}},
		},
		{
			name: "checkpoint at the limit",
			prepare: func(t *testing.T, client *testClient) Command {
				t.Helper()
				for range checkpointLimit {
					client.mustApply(t, checkpoint)
				}
				return client.next(checkpoint)
			},
			want: []map[string]any{{
				"level": "INFO", "msg": "Saved checkpoint", "client": "test",
				"checkpoint": float64(checkpointLimit + 1), "tick": 0.0, "projectRevision": 1.0,
				"retained": float64(checkpointLimit), "evicted": 1.0,
			}},
		},
		{
			name: "rewind",
			prepare: func(t *testing.T, client *testClient) Command {
				t.Helper()
				advanceTicks(client.session, 30)
				id := client.mustApply(t, checkpoint).Checkpoint
				advanceTicks(client.session, 30)
				return client.next(Command{Action: "rewind", Checkpoint: id})
			},
			want: []map[string]any{{
				"level": "INFO", "msg": "Rewound session", "client": "test", "checkpoint": 1.0,
				"fromTick": 60.0, "toTick": 30.0, "generation": 2.0, "projectRevision": 1.0, "projectRestored": false,
			}},
		},
		{
			name: "project-restoring rewind",
			prepare: func(t *testing.T, client *testClient) Command {
				t.Helper()
				advanceTicks(client.session, 30)
				id := client.mustApply(t, checkpoint).Checkpoint
				applyTestProject(t, client)
				return client.next(Command{Action: "rewind", Checkpoint: id})
			},
			want: []map[string]any{{
				"level": "INFO", "msg": "Rewound session", "client": "test", "checkpoint": 1.0,
				"fromTick": 0.0, "toTick": 30.0, "generation": 3.0, "projectRevision": 3.0, "projectRestored": true,
			}},
		},
		{
			name: "other action",
			prepare: func(_ *testing.T, client *testClient) Command {
				return client.next(Command{Action: "pause", Paused: true})
			},
		},
		{
			name: "rejected rewind",
			prepare: func(_ *testing.T, client *testClient) Command {
				return client.next(Command{Action: "rewind", Checkpoint: 99})
			},
		},
		{
			name: "exact retry",
			prepare: func(t *testing.T, client *testClient) Command {
				t.Helper()
				command := client.next(checkpoint)
				if reply := client.session.Apply(command); reply.Error != "" {
					t.Fatal(reply.Error)
				}
				return command
			},
		},
		{
			name: "closed session",
			prepare: func(_ *testing.T, client *testClient) Command {
				client.session.Close()
				return client.next(checkpoint)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var buffer bytes.Buffer
			s, err := NewWithProject(project.Default(),
				WithProjectSaver((&projectFile{}).save), WithLogger(slog.New(slog.NewJSONHandler(&buffer, nil))))
			if err != nil {
				t.Fatal(err)
			}
			command := test.prepare(t, newTestClient(s, "test"))
			buffer.Reset()
			s.Apply(command)
			if got := logRecords(t, &buffer); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("records = %v, want %v", got, test.want)
			}
		})
	}
}

// lockProbe is a log handler that reads the session in Handle. The read
// waits for the session lock. If the session logs while it holds the lock,
// Handle never returns.
type lockProbe struct {
	session *Session
	handled atomic.Int64
}

func (p *lockProbe) Enabled(context.Context, slog.Level) bool { return true }

func (p *lockProbe) Handle(context.Context, slog.Record) error {
	p.session.Metrics()
	p.handled.Add(1)
	return nil
}

func (p *lockProbe) WithAttrs([]slog.Attr) slog.Handler { return p }

func (p *lockProbe) WithGroup(string) slog.Handler { return p }

func TestSessionLogsOutsideLock(t *testing.T) {
	t.Parallel()
	probe := &lockProbe{}
	s, err := NewWithProject(project.Default(), WithLogger(slog.New(probe)))
	if err != nil {
		t.Fatal(err)
	}
	probe.session = s
	client := newTestClient(s, "test")
	done := make(chan []Reply, 1)
	go func() {
		saved := s.Apply(client.next(Command{Action: "checkpoint"}))
		rewound := s.Apply(client.next(Command{Action: "rewind", Checkpoint: saved.Checkpoint}))
		done <- []Reply{saved, rewound}
	}()
	select {
	case replies := <-done:
		for _, reply := range replies {
			if reply.Error != "" {
				t.Fatal(reply.Error)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the commands did not complete in 5 s. The session logs while it holds the lock.")
	}
	if got := probe.handled.Load(); got != 2 {
		t.Fatalf("the handler got %d records, want 2", got)
	}
}
