package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type streamWriteProbe func(context.Context, websocket.MessageType, []byte) error

func (f streamWriteProbe) Write(ctx context.Context, kind websocket.MessageType, data []byte) error {
	return f(ctx, kind, data)
}

func TestStreamWriteDeadline(t *testing.T) {
	t.Parallel()
	writeErr := errors.New("write failed")
	for _, test := range []struct {
		name     string
		parent   time.Duration
		canceled bool
		writeErr error
	}{
		{name: "production timeout"},
		{name: "parent deadline", parent: time.Second},
		{name: "parent cancellation", canceled: true},
		{name: "writer error", writeErr: writeErr},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			parent, cancel := context.WithCancel(t.Context())
			if test.parent != 0 {
				cancel()
				parent, cancel = context.WithTimeout(t.Context(), test.parent)
			}
			defer cancel()
			if test.canceled {
				cancel()
			}
			data := []byte("message")
			var childDone <-chan struct{}
			var deadline time.Time
			var hasDeadline bool
			calls := 0
			probe := streamWriteProbe(func(ctx context.Context, kind websocket.MessageType, got []byte) error {
				childDone = ctx.Done()
				deadline, hasDeadline = ctx.Deadline()
				calls++
				if kind != websocket.MessageBinary || len(got) != len(data) || &got[0] != &data[0] {
					t.Fatal("write changed the message")
				}
				if test.canceled && !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatal("write lost parent cancellation", ctx.Err())
				}
				return test.writeErr
			})
			before := time.Now()
			gotErr := streamWrite(parent, probe, websocket.MessageBinary, data)
			after := time.Now()
			if !errors.Is(gotErr, test.writeErr) || calls != 1 {
				t.Fatal("write changed the error or call count", gotErr, calls)
			}
			if !hasDeadline {
				t.Fatal("write has no deadline")
			}
			if want, parentBounded := parent.Deadline(); test.parent != 0 && parentBounded {
				if !deadline.Equal(want) {
					t.Fatal("write changed the earlier parent deadline", deadline, want)
				}
			} else if deadline.Before(before.Add(30*time.Second)) || deadline.After(after.Add(30*time.Second)) {
				t.Fatal("write changed the production thirty-second timeout", deadline)
			}
			select {
			case <-childDone:
			default:
				t.Fatal("write did not cancel its child context")
			}
		})
	}
}

func TestStreamSlowWriteBudget(t *testing.T) {
	t.Parallel()
	for _, read := range []bool{true, false} {
		name := "stalled"
		timeout := 500 * time.Millisecond
		if read {
			name = "slow reader"
			timeout = 3 * time.Second
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result := make(chan error, 1)
			entered := make(chan time.Time, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
				if err != nil {
					result <- err
					return
				}
				defer func() { _ = conn.CloseNow() }()
				writer := streamWriteProbe(func(ctx context.Context, kind websocket.MessageType, data []byte) error {
					entered <- time.Now()
					return conn.Write(ctx, kind, data)
				})
				result <- streamWriteWithin(r.Context(), writer, websocket.MessageBinary, make([]byte, MaxStreamMessage), timeout)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.CloseNow() }()
			conn.SetReadLimit(MaxStreamMessage)
			started := <-entered
			if read {
				// Scale the old five-second budget and six-second delay by ten.
				// The wrapper deadline test checks the production thirty seconds.
				time.Sleep(600 * time.Millisecond)
				_, data, readErr := conn.Read(ctx)
				if readErr != nil || len(data) != MaxStreamMessage {
					t.Fatal("legal-size slow baseline failed", len(data), readErr)
				}
			}
			select {
			case err := <-result:
				elapsed := time.Since(started)
				if read && (err != nil || elapsed < 500*time.Millisecond || elapsed >= timeout) {
					t.Fatal("slow write budget", elapsed, err)
				}
				if !read && (err == nil || elapsed < timeout-100*time.Millisecond || elapsed > timeout+2*time.Second) {
					t.Fatal("stalled write deadline", elapsed, err)
				}
			case <-ctx.Done():
				t.Fatal("write did not stop", ctx.Err())
			}
		})
	}
}

func TestStreamWatchdogTracksACKProgress(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p := &statePublisher{}
	now := time.Now()
	c := &streamSubscriber{sent: []streamSent{{"s", 1, 1}, {"s", 2, 1}}, bytes: 2, progress: now.Add(-streamProgressTimeout + time.Second)}
	done := make(chan struct{})
	go func() { defer close(done); p.watchProgress(ctx, c, cancel) }()
	p.mu.Lock()
	if err := p.control(c, "ack", "s", "1", "", now); err != nil {
		t.Fatal(err)
	}
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		t.Fatal("ACK did not extend deadline")
	case <-time.After(1100 * time.Millisecond):
	}
	p.mu.Lock()
	c.progress = time.Now().Add(-streamProgressTimeout - time.Second)
	c.heartbeat = 1
	if err := p.control(c, "heartbeat", "", "", "1", time.Now()); err != nil {
		t.Fatal(err)
	}
	p.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("heartbeat extended state deadline or watchdog stalled")
	}
}

func TestStreamPressureShedsLeaseOwnerAndDelivers(t *testing.T) {
	s, frame := streamFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The reduced admission budget exercises the same accounting with one
	// blocked eight-MiB writer, without allocating the global budget in a test.
	const budget = 8 << 20
	old := &streamPayload{stream: "test", sequence: 1, data: make([]byte, budget), refs: 1, full: true}
	p := &statePublisher{session: s, cancel: cancel, clients: map[*streamSubscriber]bool{}, wake: make(chan struct{}, 1), full: old, stream: "test", sequence: 1, retained: budget}
	s.stream = p
	server := httptest.NewServer(s.HandlerFS(nil))
	defer server.Close()
	defer func() { s.Close(); p.workers.Wait() }()
	connect := func() *websocket.Conn {
		t.Helper()
		conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		conn.SetReadLimit(MaxStreamMessage)
		return conn
	}
	lagger := connect()
	defer func() { _ = lagger.CloseNow() }()
	healthy := connect()
	defer func() { _ = healthy.CloseNow() }()
	readCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	if _, _, err := healthy.Read(readCtx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := healthy.Read(readCtx); err != nil {
		t.Fatal(err)
	}
	ack := streamJSON(t, map[string]string{"kind": "ack", "stream": "test", "sequence": "1"})
	if err := healthy.Write(readCtx, websocket.MessageText, ack); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		p.mu.Lock()
		writers, cleared := 0, 0
		for c := range p.clients {
			if c.writing != nil {
				writers++
			}
			if c.bytes == 0 {
				cleared++
			}
		}
		p.mu.Unlock()
		if writers == 1 && cleared == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("did not isolate blocked writer", writers, cleared)
		}
		time.Sleep(time.Millisecond)
	}
	frame.State.Revision++
	data, err := encodeStream(StreamEnvelope{Kind: "full", Stream: "test", Sequence: 2, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame})
	if err != nil {
		t.Fatal(err)
	}
	next, err := p.admitEncoded(readCtx, &streamPayload{stream: "test", sequence: 2, data: data, full: true}, budget)
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	valid := p.metrics.PressureSheds == 1 && old.data == nil && p.retained <= budget && p.encoding == 0
	p.full = next
	p.sequence = 2
	p.notify()
	p.mu.Unlock()
	if !valid {
		t.Fatal("pressure accounting mismatch")
	}
	kind, data, err := healthy.Read(readCtx)
	if err != nil || kind != websocket.MessageBinary {
		t.Fatal("healthy subscriber lost state", err)
	}
	raw, err := InflateStream(data)
	if err != nil {
		t.Fatal(err)
	}
	var envelope StreamEnvelope
	if err = json.Unmarshal(raw, &envelope); err != nil || envelope.Sequence != 2 {
		t.Fatal("healthy subscriber did not receive subsequent full", err)
	}
}

func TestStreamPressureWaitsForReleaseOrShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		canceled := make(chan struct{})
		old := &streamPayload{data: make([]byte, 16), refs: 2}
		c := &streamSubscriber{writing: old, writingAt: time.Now(), cancel: func() { close(canceled) }}
		p := &statePublisher{full: old, retained: 16, clients: map[*streamSubscriber]bool{c: true}}
		type admission struct {
			payload *streamPayload
			err     error
		}
		finished := make(chan admission, 1)
		go func() {
			b, err := p.admitEncoded(ctx, &streamPayload{data: make([]byte, 1)}, 16)
			finished <- admission{b, err}
		}()
		select {
		case <-canceled:
		case <-time.After(time.Second):
			t.Fatal("lease owner was not canceled")
		}
		p.mu.Lock()
		refs, retained, encoded := old.refs, p.retained, p.encoding
		p.mu.Unlock()
		if refs != 1 || retained != 16 || encoded != 1 {
			t.Fatal("cancellation reclaimed active lease", refs, retained, encoded)
		}
		select {
		case <-finished:
			t.Fatal("admitted before write exit")
		default:
		}
		if shutdown {
			cancel()
		} else {
			p.mu.Lock()
			p.release(old)
			c.writing = nil
			wakeStream(p.released)
			p.mu.Unlock()
		}
		select {
		case result := <-finished:
			if shutdown && !errors.Is(result.err, context.Canceled) || !shutdown && result.err != nil {
				t.Fatal("pressure wait result", result.err)
			}
			p.mu.Lock()
			if shutdown {
				p.release(old)
				c.writing = nil
			} else {
				p.release(result.payload)
			}
			remaining, encoded := p.retained, p.encoding
			p.mu.Unlock()
			if remaining != 0 || encoded != 0 {
				t.Fatal("admission retained workspace", remaining, encoded)
			}
		case <-time.After(time.Second):
			t.Fatal("pressure wait did not finish")
		}
		cancel()
	}
}
