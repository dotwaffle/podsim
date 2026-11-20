package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stateRequest sends a state request with ctx to serveState and writes the
// reply to w. accept is the Accept header.
func stateRequest(ctx context.Context, s *Session, w http.ResponseWriter, steps stateSteps, accept string) {
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/state", http.NoBody)
	request.Header.Set("Accept", accept)
	s.serveState(w, request, steps)
}

// recordState sends a state request and returns the recorded reply.
func recordState(ctx context.Context, s *Session, steps stateSteps, accept string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	stateRequest(ctx, s, response, steps, accept)
	return response
}

// blockingWriter records a reply. Each Write first sends a value on
// entered and waits until unblock closes, when entered is set. Then it
// fails with err, or it records the data when err is nil.
type blockingWriter struct {
	*httptest.ResponseRecorder
	entered chan<- struct{}
	unblock <-chan struct{}
	err     error
}

func (w *blockingWriter) Write(data []byte) (int, error) {
	if w.entered != nil {
		w.entered <- struct{}{}
		<-w.unblock
	}
	if w.err != nil {
		return 0, w.err
	}
	return w.ResponseRecorder.Write(data)
}

// TestStateHTTPAdmission fills the places of the HTTP state with requests
// that wait in the write of their reply. The next request gets HTTP 503
// with Retry-After. An ended request gets HTTP 503 without Retry-After,
// and a request without the media type still gets HTTP 406. When one
// write ends, a request gets the state again. A failed write also frees
// its place.
func TestStateHTTPAdmission(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	steps := stateSteps{capture: s.captureHTTPStateLocked, encode: EncodeStateJSON}
	entered := make(chan struct{})
	releases := make([]func(), maxStateRequests)
	done := make([]chan int, maxStateRequests)
	for index := range maxStateRequests {
		unblock := make(chan struct{})
		releases[index], done[index] = sync.OnceFunc(func() { close(unblock) }), make(chan int, 1)
		t.Cleanup(releases[index])
		writer := &blockingWriter{ResponseRecorder: httptest.NewRecorder(), entered: entered, unblock: unblock}
		go func() {
			stateRequest(t.Context(), s, writer, steps, StateMediaType)
			done[index] <- writer.Code
		}()
	}
	for range maxStateRequests {
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("the state requests did not reach the write of their reply")
		}
	}
	busy := recordState(t.Context(), s, steps, StateMediaType)
	if busy.Code != http.StatusServiceUnavailable || busy.Header().Get("Retry-After") != stateRetryAfter ||
		busy.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(busy.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("busy reply: status %d, headers %v, want 503 with Retry-After %s", busy.Code, busy.Header(), stateRetryAfter)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ended := recordState(ctx, s, steps, StateMediaType)
	if ended.Code != http.StatusServiceUnavailable || ended.Header().Get("Retry-After") != "" || ended.Body.String() != stateEndedText+"\n" {
		t.Fatalf("ended request: status %d, Retry-After %q, body %q, want 503 without Retry-After",
			ended.Code, ended.Header().Get("Retry-After"), ended.Body.String())
	}
	if code := recordState(t.Context(), s, steps, "application/json").Code; code != http.StatusNotAcceptable {
		t.Fatalf("a request without the media type got status %d while all places were in use, want 406", code)
	}
	releases[0]()
	if code := <-done[0]; code != http.StatusOK {
		t.Fatalf("a blocked state request got status %d, want 200", code)
	}
	if code := recordState(t.Context(), s, steps, StateMediaType).Code; code != http.StatusOK {
		t.Fatalf("a state request after one write ended got status %d, want 200", code)
	}
	failing := &blockingWriter{ResponseRecorder: httptest.NewRecorder(), err: errors.New("test write failed")}
	stateRequest(t.Context(), s, failing, steps, StateMediaType)
	if used := len(s.stateSlots); used != 1 {
		t.Fatalf("%d places are in use after a failed write, want only the blocked write", used)
	}
}

// TestStateHTTPEndedRequest checks that the server does not copy the state
// for a request that ended before the copy, also while it waited for the
// session lock, and does not encode the state for a request that ended
// during the copy.
func TestStateHTTPEndedRequest(t *testing.T) {
	t.Parallel()
	const (
		endBefore = iota
		endWhileLocked
		endInCapture
	)
	tests := []struct {
		name     string
		end      int
		captures int64
	}{
		{"before the request", endBefore, 0},
		{"while it waits for the session lock", endWhileLocked, 0},
		{"during the copy", endInCapture, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var captures, encodes atomic.Int64
			steps := stateSteps{
				capture: func() (TopologySnapshot, StreamFrame, error) {
					captures.Add(1)
					if test.end == endInCapture {
						cancel()
					}
					return s.captureHTTPStateLocked()
				},
				encode: func(topology TopologySnapshot, frame StreamFrame) ([]byte, error) {
					encodes.Add(1)
					return EncodeStateJSON(topology, frame)
				},
			}
			var response *httptest.ResponseRecorder
			switch test.end {
			case endBefore:
				cancel()
				response = recordState(ctx, s, steps, StateMediaType)
			case endWhileLocked:
				s.mu.Lock()
				replied := make(chan *httptest.ResponseRecorder, 1)
				go func() { replied <- recordState(ctx, s, steps, StateMediaType) }()
				// The request has its place. It waits for the session lock,
				// or it goes to the lock next.
				for deadline := time.Now().Add(10 * time.Second); len(s.stateSlots) == 0; time.Sleep(time.Millisecond) {
					if time.Now().After(deadline) {
						s.mu.Unlock()
						t.Fatal("the request did not get a place")
					}
				}
				cancel()
				s.mu.Unlock()
				response = <-replied
			default:
				response = recordState(ctx, s, steps, StateMediaType)
			}
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "" {
				t.Fatalf("status %d with Retry-After %q, want 503 without Retry-After", response.Code, response.Header().Get("Retry-After"))
			}
			if got, want := captures.Load(), test.captures; got != want {
				t.Fatalf("the server copied the state %d times, want %d", got, want)
			}
			if got := encodes.Load(); got != 0 {
				t.Fatalf("the server encoded the state %d times for an ended request", got)
			}
			if used := len(s.stateSlots); used != 0 {
				t.Fatalf("%d places stay in use after the reply", used)
			}
		})
	}
}
