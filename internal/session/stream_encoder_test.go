package session

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"math"
	"math/rand/v2"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func freshStreamCompression(tb testing.TB, data []byte) []byte {
	tb.Helper()
	var out bytes.Buffer
	w, err := gzip.NewWriterLevel(&out, gzip.BestSpeed)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err = w.Write(data); err != nil {
		tb.Fatal(err)
	}
	if err = w.Close(); err != nil {
		tb.Fatal(err)
	}
	return slices.Clone(out.Bytes())
}

func TestStreamEncoderIndependentMembers(t *testing.T) {
	t.Parallel()
	encoder := new(streamEncoder)
	noise := make([]byte, 128<<10)
	random := rand.New(rand.NewPCG(1, 2))
	for i := range noise {
		noise[i] = byte(random.Uint32())
	}
	var previous, saved []byte
	for _, data := range [][]byte{nil, {}, []byte("null"), bytes.Repeat([]byte("state"), 30000), noise, []byte("[]"), bytes.Repeat([]byte{0, 255, 3}, 25000)} {
		got, err := encoder.compressJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, freshStreamCompression(t, data)) {
			t.Fatal("reset changed gzip bytes")
		}
		decoded, err := InflateStream(got)
		if err != nil || !bytes.Equal(decoded, data) {
			t.Fatal("not one independent member", err)
		}
		if !bytes.Equal(previous, saved) {
			t.Fatal("later compression changed earlier output")
		}
		if encoder.output.Cap() != 0 {
			t.Fatal("encoder retained completed output")
		}
		previous, saved = got, slices.Clone(got)
		// Mutating the caller's output must not alter the next member.
		previous[0] ^= 1
		saved[0] ^= 1
	}
}

func TestStreamEncoderRejectsAndRecovers(t *testing.T) {
	t.Parallel()
	encoder := new(streamEncoder)
	if _, err := encoder.compressJSON(make([]byte, MaxStreamJSON+1)); err == nil {
		t.Fatal("accepted oversized input")
	}
	if encoder.writer != nil {
		t.Fatal("oversized input allocated compressor")
	}
	_, frame := streamFixture(t)
	e := StreamEnvelope{Kind: "full", Full: &frame}
	good, err := encoder.encode(e)
	if err != nil {
		t.Fatal(err)
	}
	writer := encoder.writer
	frame.State.Simulation.PassengerDistanceMeters = math.NaN()
	if _, err = encoder.encode(e); err == nil {
		t.Fatal("accepted nonfinite JSON")
	}
	if _, err = encoder.compressJSON(make([]byte, MaxStreamJSON+1)); err == nil {
		t.Fatal("accepted oversized input after success")
	}
	frame.State.Simulation.PassengerDistanceMeters = 0
	again, err := encoder.encode(e)
	if err != nil || !bytes.Equal(good, again) || encoder.writer != writer || encoder.output.Cap() != 0 {
		t.Fatal("failed to recover with the same compressor", err)
	}
}

func TestStreamPublisherReusesEncoder(t *testing.T) {
	t.Parallel()
	s, _ := streamFixture(t)
	p := &statePublisher{session: s, clients: map[*streamSubscriber]bool{}}
	if err := p.publish(t.Context(), true, true); err != nil {
		t.Fatal(err)
	}
	if p.encoder == nil || p.encoder.writer == nil {
		t.Fatal("publisher bypassed reusable compressor")
	}
	encoder, writer := p.encoder, p.encoder.writer
	for range 3 {
		s.advance()
		if err := p.publish(t.Context(), false, true); err != nil {
			t.Fatal(err)
		}
		if p.encoder != encoder || p.encoder.writer != writer || p.encoder.output.Cap() != 0 {
			t.Fatal("publisher did not reuse and detach compressor")
		}
	}
	if p.metrics.Delta != 3 {
		t.Fatal("fixture did not publish consecutive deltas")
	}
}

func TestStreamEncoderPublisherLifecycle(t *testing.T) {
	t.Parallel()
	s, _ := streamFixture(t)
	t.Cleanup(s.Close)
	server := httptest.NewServer(s.HandlerFS(nil))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	connect := func() *websocket.Conn {
		t.Helper()
		conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		for range 2 {
			if _, _, err := conn.Read(ctx); err != nil {
				t.Fatal(err)
			}
		}
		return conn
	}
	conn := connect()
	p := s.publisher()
	p.mu.Lock()
	first := p.encoder
	p.mu.Unlock()
	if first == nil {
		t.Fatal("active publisher has no compressor")
	}
	_ = conn.CloseNow()
	for {
		p.mu.Lock()
		idle := p.encoder == nil
		p.mu.Unlock()
		if idle {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("idle publisher retained compressor")
		case <-time.After(10 * time.Millisecond):
		}
	}
	connect()
	p.mu.Lock()
	replaced := p.encoder != nil && p.encoder != first
	p.mu.Unlock()
	if !replaced {
		t.Fatal("new subscriber did not get a new compressor")
	}
	s.Close()
	if err := s.WaitStreams(ctx); err != nil {
		t.Fatal(err)
	}
	if p.encoder != nil {
		t.Fatal("shutdown retained compressor")
	}
}

func BenchmarkStreamCompression(b *testing.B) {
	for _, size := range []int{4096, 256 << 10} {
		data, err := json.Marshal(map[string]string{"state": strings.Repeat("network-state-0123456789", size/24)})
		if err != nil {
			b.Fatal(err)
		}
		for _, reuse := range []bool{false, true} {
			name := "fresh"
			if reuse {
				name = "reused"
			}
			b.Run(name+"/"+strconv.Itoa(size), func(b *testing.B) {
				encoder := new(streamEncoder)
				if _, err := encoder.compressJSON(data); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if reuse {
						if _, err := encoder.compressJSON(data); err != nil {
							b.Fatal(err)
						}
					} else {
						freshStreamCompression(b, data)
					}
				}
			})
		}
	}
}
