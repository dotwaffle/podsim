package session

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

const stoppingMessage = "The server is stopping. Try again after it restarts."

func TestCloseStopsClockAndRejectsNewCommands(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	first := commandFor(s, "speed")
	first.Speed = 2
	accepted := s.Apply(first)
	if accepted.Error != "" {
		t.Fatal(accepted.Error)
	}
	s.advance()
	s.Close()
	s.Close()
	frozen := s.State()
	s.advance()
	stopping := Reply{
		Epoch: frozen.Epoch, Revision: frozen.Revision,
		ProjectRevision: frozen.ProjectRevision, Generation: frozen.Generation,
		ErrorCode: ServerStopping, Error: stoppingMessage,
	}
	pause := commandFor(s, "pause")
	pause.Sequence, pause.Paused = 2, true
	trip := commandFor(s, "trip")
	trip.Client, trip.Origin, trip.Destination = "other", "harbor", "market"
	// Each step runs in order. A repeated rejection gets ServerStopping again because no receipt is stored.
	for _, step := range []struct {
		name    string
		command Command
		want    Reply
	}{
		{name: "exact retry gets the stored reply", command: first, want: accepted},
		{name: "new sequence", command: pause, want: stopping},
		{name: "retry of a rejected sequence", command: pause, want: stopping},
		{name: "new client", command: trip, want: stopping},
		{name: "retry from a rejected client", command: trip, want: stopping},
		{name: "exact retry after rejections", command: first, want: accepted},
	} {
		if got := s.Apply(step.command); !reflect.DeepEqual(got, step.want) {
			t.Errorf("%s: reply = %+v, want %+v", step.name, got, step.want)
		}
	}
	s.advance()
	if !reflect.DeepEqual(s.State(), frozen) {
		t.Fatal("closed session changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if want := map[string]receipt{"test": {command: first, reply: accepted}}; !reflect.DeepEqual(s.receipts, want) {
		t.Fatalf("receipts = %+v, want %+v", s.receipts, want)
	}
}

func TestCloseFreezesConcurrentClockAndCommands(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	const clients = 4
	var workers, started, rejected sync.WaitGroup
	started.Add(clients)
	rejected.Add(clients)
	stop := make(chan struct{})
	workers.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				s.advance()
			}
		}
	})
	speeds := []int{1, 2, 4, 8}
	for i := range clients {
		workers.Go(func() {
			markStarted, markRejected := sync.OnceFunc(started.Done), sync.OnceFunc(rejected.Done)
			command := commandFor(s, "speed")
			command.Client = "client-" + strconv.Itoa(i)
			for sequence := uint64(1); ; sequence++ {
				select {
				case <-stop:
					return
				default:
				}
				command.Sequence = sequence
				command.Speed = speeds[sequence%uint64(len(speeds))]
				reply := s.Apply(command)
				markStarted()
				if reply.ErrorCode == ServerStopping {
					markRejected()
				} else if reply.Error != "" {
					t.Errorf("client %d: reply = %+v", i, reply)
				}
			}
		})
	}
	started.Wait()
	s.Close()
	frozen := s.State()
	rejected.Wait()
	close(stop)
	workers.Wait()
	s.advance()
	if !reflect.DeepEqual(s.State(), frozen) {
		t.Fatal("state changed after Close returned")
	}
}

func TestRunStopsTickingAfterClose(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newTestSession(t)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			s.Run(ctx)
			close(done)
		}()
		time.Sleep(time.Second + time.Millisecond)
		if got := s.State().Revision; got != 60 {
			t.Fatalf("revision after one second = %d, want 60", got)
		}
		s.Close()
		time.Sleep(time.Second)
		if got := s.State().Revision; got != 60 {
			t.Fatalf("closed clock advanced to revision %d", got)
		}
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("Run did not return after cancellation")
		}
	})
}

func TestClosedSessionRejectsCommandsOverHTTP(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	s.Close()
	handler := s.Handler(t.TempDir())
	body := mustJSON(t, commandFor(s, "pause"))
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/api/command", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var reply Reply
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || reply.ErrorCode != ServerStopping {
		t.Fatalf("command status = %d, reply = %+v", response.Code, reply)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/api/state", http.NoBody))
	if response.Code != http.StatusOK {
		t.Fatalf("state status = %d", response.Code)
	}
}
