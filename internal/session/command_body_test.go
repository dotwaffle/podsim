package session

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
)

// gzipBody compresses data as one gzip member at level.
func gzipBody(t *testing.T, data []byte, level int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, level)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// paddedPause returns the JSON of a pause command from client with spaces
// after it, so that the JSON has size bytes.
func paddedPause(t *testing.T, s *Session, client string, size int) []byte {
	t.Helper()
	body := pauseWithLanes(t, s, client, "Lanes", 0)
	if len(body) > size {
		t.Fatalf("the command has %d bytes, more than %d", len(body), size)
	}
	return append(body, bytes.Repeat([]byte(" "), size-len(body))...)
}

// postEncoded sends body to the command endpoint of handler with the
// Content-Encoding values encodings.
func postEncoded(t *testing.T, handler http.Handler, body []byte, encodings ...string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/command", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	for _, encoding := range encodings {
		request.Header.Add("Content-Encoding", encoding)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// TestCommandContentEncoding checks the command bodies with and without the
// gzip content encoding, and the replies to bodies that the server refuses.
func TestCommandContentEncoding(t *testing.T) {
	s := newTestSession(t)
	handler := s.Handler(t.TempDir())
	pause := func(client string) []byte { return pauseWithLanes(t, s, client, "Lanes", 0) }
	compressed := gzipBody(t, pause("trailing"), gzip.DefaultCompression)
	checksum := bytes.Clone(gzipBody(t, pause("checksum"), gzip.DefaultCompression))
	checksum[len(checksum)-8] ^= 0xff
	wireLimit := fmt.Sprintf("the command body must have at most %d bytes\n", MaxCommandBytes)
	inflatedLimit := fmt.Sprintf("the decompressed command body must have at most %d bytes\n", MaxInflatedCommandBytes)
	encodingReply := errContentEncoding.Error() + "\n"
	tests := []struct {
		name      string
		body      []byte
		encodings []string
		code      int
		reply     string
	}{
		{"gzip", gzipBody(t, pause("gzip"), gzip.DefaultCompression), []string{"gzip"}, http.StatusOK, ""},
		{"gzip in capitals", gzipBody(t, pause("capitals"), gzip.DefaultCompression), []string{" GZIP "}, http.StatusOK, ""},
		{"x-gzip", gzipBody(t, pause("x-gzip"), gzip.DefaultCompression), []string{"x-gzip"}, http.StatusOK, ""},
		{"identity", pause("identity"), []string{"identity"}, http.StatusOK, ""},
		{"gzip at the decompressed limit", gzipBody(t, paddedPause(t, s, "inflated", MaxInflatedCommandBytes), gzip.BestSpeed), []string{"gzip"}, http.StatusOK, ""},
		{"gzip one byte over the decompressed limit", gzipBody(t, paddedPause(t, s, "over", MaxInflatedCommandBytes+1), gzip.BestSpeed), []string{"gzip"}, http.StatusRequestEntityTooLarge, inflatedLimit},
		// A gzip body without compression is larger than its content.
		{"gzip over the body limit", gzipBody(t, paddedPause(t, s, "stored", MaxCommandBytes), gzip.NoCompression), []string{"gzip"}, http.StatusRequestEntityTooLarge, wireLimit},
		{"plain over the body limit", paddedPause(t, s, "plain", MaxCommandBytes+1), nil, http.StatusRequestEntityTooLarge, wireLimit},
		{"plain at the body limit", paddedPause(t, s, "limit", MaxCommandBytes), nil, http.StatusOK, ""},
		{"brotli", pause("brotli"), []string{"br"}, http.StatusUnsupportedMediaType, encodingReply},
		{"gzip twice in one value", gzipBody(t, gzipBody(t, pause("list"), gzip.DefaultCompression), gzip.DefaultCompression), []string{"gzip, gzip"}, http.StatusUnsupportedMediaType, encodingReply},
		{"two encoding headers", pause("headers"), []string{"gzip", "identity"}, http.StatusUnsupportedMediaType, encodingReply},
		{"not gzip", pause("not gzip"), []string{"gzip"}, http.StatusBadRequest, "invalid gzip body\n"},
		{"bad checksum", checksum, []string{"gzip"}, http.StatusBadRequest, "invalid gzip body\n"},
		{"truncated", compressed[:len(compressed)-4], []string{"gzip"}, http.StatusBadRequest, "invalid gzip body\n"},
		{"data after the member", append(bytes.Clone(compressed), 0), []string{"gzip"}, http.StatusBadRequest, "send one gzip member only\n"},
		{"two members", append(bytes.Clone(compressed), compressed...), []string{"gzip"}, http.StatusBadRequest, "send one gzip member only\n"},
		{"gzip JSON over a shape limit", gzipBody(t, pauseWithLanes(t, s, "shape", "Lanes", 1_000_000), gzip.DefaultCompression), []string{"gzip"}, http.StatusBadRequest, "invalid command JSON\n"},
	}
	// The cases share one session, so they run one at a time. Else they
	// could use all admission places of the session.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			recorder := postEncoded(t, handler, tc.body, tc.encodings...)
			if recorder.Code != tc.code || (tc.reply != "" && recorder.Body.String() != tc.reply) {
				t.Fatalf("status %d, reply %q, want %d and %q", recorder.Code, recorder.Body.String(), tc.code, tc.reply)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control %q, want no-store", got)
			}
			wantAccept := ""
			if tc.code == http.StatusUnsupportedMediaType {
				wantAccept = "gzip"
			}
			if got := recorder.Header().Get("Accept-Encoding"); got != wantAccept {
				t.Fatalf("Accept-Encoding %q, want %q", got, wantAccept)
			}
		})
	}
}

// TestCommandGzipBomb sends a small gzip body that decompresses to 64 MiB.
// The server must stop the decompression at the limit. It does not run in
// parallel, because it measures the heap.
func TestCommandGzipBomb(t *testing.T) {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	zeros := make([]byte, 1<<20)
	for range 64 {
		if _, err := writer.Write(zeros); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	handler := newTestSession(t).Handler(t.TempDir())
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	recorder := postEncoded(t, handler, buffer.Bytes(), "gzip")
	runtime.GC()
	runtime.ReadMemStats(&after)
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, reply %q, want 413", recorder.Code, recorder.Body.String())
	}
	// The server decompresses into one buffer of the limit. The measured
	// allocation is about 1.05 times the limit, also with the race
	// detector.
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("%d compressed bytes, allocated %d bytes", buffer.Len(), allocated)
	if allocated >= 4*MaxInflatedCommandBytes {
		t.Fatalf("the request allocated %d bytes", allocated)
	}
}

// TestCommandBodyReadError checks that a body that fails to read gets the
// same reply as before the gzip support.
func TestCommandBodyReadError(t *testing.T) {
	t.Parallel()
	handler := newTestSession(t).Handler(t.TempDir())
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/command", io.MultiReader(bytes.NewReader([]byte("{")), errorReader{}))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || recorder.Body.String() != "invalid command JSON\n" {
		t.Fatalf("status %d, reply %q", recorder.Code, recorder.Body.String())
	}
}

// errorReader fails each read.
type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// httpResult is the reply to a command that a goroutine sends.
type httpResult struct {
	code  int
	reply string
}

// sendCommand sends body in a new goroutine with ctx and the Content-Encoding
// values encodings. The channel gives the result.
func sendCommand(ctx context.Context, handler http.Handler, body []byte, encodings ...string) <-chan httpResult {
	done := make(chan httpResult, 1)
	go func() {
		request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/command", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		for _, encoding := range encodings {
			request.Header.Add("Content-Encoding", encoding)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		done <- httpResult{recorder.Code, recorder.Body.String()}
	}()
	return done
}

// finished tells if done has a result, and gives it.
func finished(done <-chan httpResult) (httpResult, bool) {
	select {
	case result := <-done:
		return result, true
	default:
		return httpResult{}, false
	}
}

// TestLargeCommandGuard checks that one large command at a time decodes and
// applies. The project saver stops a large project command while it has
// the guard. Then two large commands wait, and a canceled request stops
// its wait.
func TestLargeCommandGuard(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		saving, proceed := make(chan struct{}), make(chan struct{})
		s, err := NewWithProject(project.Default(), WithProjectSaver(func(project.Config) error {
			saving <- struct{}{}
			<-proceed
			return nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		handler := s.Handler(t.TempDir())
		if recorder := postEncoded(t, handler, pauseWithLanes(t, s, "pause", "Lanes", 0)); recorder.Code != http.StatusOK {
			t.Fatalf("pause: status %d, reply %q", recorder.Code, recorder.Body.String())
		}
		// The current project is a no-op apply without a save, so change it.
		config := project.Default()
		config.Name = "Large command guard"
		command, err := json.Marshal(Command{Client: "project", Sequence: 1, Epoch: s.State().Epoch, Action: "project", Project: &config, ProjectRevision: s.State().ProjectRevision})
		if err != nil {
			t.Fatal(err)
		}
		command = append(command, bytes.Repeat([]byte(" "), largeCommandBytes+1-len(command))...)
		// The project command keeps the session lock while it saves, so
		// make the other bodies first.
		plainBody := paddedPause(t, s, "plain", largeCommandBytes+1)
		largeGzip := gzipBody(t, paddedPause(t, s, "gzip", largeCommandBytes+1), gzip.BestSpeed)
		applying := sendCommand(t.Context(), handler, gzipBody(t, command, gzip.BestSpeed), "gzip")
		<-saving
		if len(s.largeCommands) != 1 {
			t.Fatal("the project command does not have the guard")
		}

		canceled, cancel := context.WithCancel(t.Context())
		plain := sendCommand(canceled, handler, plainBody)
		compressed := sendCommand(t.Context(), handler, largeGzip, "gzip")
		synctest.Wait()
		for name, done := range map[string]<-chan httpResult{"plain": plain, "gzip": compressed} {
			if result, ok := finished(done); ok {
				t.Fatalf("the large %s command did not wait: %+v", name, result)
			}
		}
		cancel()
		synctest.Wait()
		if result, ok := finished(plain); !ok || result.code != http.StatusServiceUnavailable {
			t.Fatalf("the canceled command gave %+v, %t, want 503", result, ok)
		}
		if _, ok := finished(compressed); ok {
			t.Fatal("the large gzip command did not wait")
		}

		close(proceed)
		synctest.Wait()
		for name, done := range map[string]<-chan httpResult{"project": applying, "gzip": compressed} {
			if result, ok := finished(done); !ok || result.code != http.StatusOK {
				t.Fatalf("the %s command gave %+v, %t, want 200", name, result, ok)
			}
		}
		if len(s.largeCommands) != 0 {
			t.Fatal("a command did not release the guard")
		}
	})
}

// TestSmallPlainCommandsSkipGuard checks that a plain body of at most
// largeCommandBytes does not wait for the guard.
func TestSmallPlainCommandsSkipGuard(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	handler := s.Handler(t.TempDir())
	s.largeCommands <- struct{}{}
	defer func() { <-s.largeCommands }()
	if recorder := postEncoded(t, handler, paddedPause(t, s, "plain", largeCommandBytes)); recorder.Code != http.StatusOK {
		t.Fatalf("status %d, reply %q", recorder.Code, recorder.Body.String())
	}
}

// TestGzipCommandsWaitBeforeDecompression sends small gzip bodies while
// the guard is held. Each body decompresses to more than
// largeCommandBytes. The waiting requests must keep only the compressed
// bytes. When all admission places are in use, another gzip body gets 503
// at once, also a small one. A canceled request must stop its wait and
// give back its place. The test does not run in parallel, because it
// measures the heap.
func TestGzipCommandsWaitBeforeDecompression(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newTestSession(t)
		handler := s.Handler(t.TempDir())
		spaces := gzipBody(t, bytes.Repeat([]byte(" "), largeCommandBytes+1), gzip.BestCompression)
		pause := gzipBody(t, pauseWithLanes(t, s, "pause", "Lanes", 0), gzip.DefaultCompression)
		s.largeCommands <- struct{}{}

		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		canceled, cancel := context.WithCancel(t.Context())
		results := make([]<-chan httpResult, 0, maxLargeBodies)
		for range maxLargeBodies {
			results = append(results, sendCommand(canceled, handler, spaces, "gzip"))
		}
		synctest.Wait()
		runtime.GC()
		runtime.ReadMemStats(&after)
		growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
		t.Logf("%d waiting requests of %d compressed bytes, heap growth %d bytes", maxLargeBodies, len(spaces), growth)
		// A request that decompressed its body before the wait would keep
		// more than largeCommandBytes.
		if growth >= largeCommandBytes {
			t.Fatalf("the heap grew by %d bytes", growth)
		}
		for _, done := range results {
			if result, ok := finished(done); ok {
				t.Fatalf("a gzip command did not wait: %+v", result)
			}
		}
		if recorder := postEncoded(t, handler, pause, "gzip"); recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" {
			t.Fatalf("a gzip command with no admission place gave status %d, Retry-After %q, want 503 and 1", recorder.Code, recorder.Header().Get("Retry-After"))
		}

		cancel()
		synctest.Wait()
		for _, done := range results {
			if result, ok := finished(done); !ok || result.code != http.StatusServiceUnavailable {
				t.Fatalf("a canceled command gave %+v, %t, want 503", result, ok)
			}
		}
		if len(s.largeCommands) != 1 || len(s.largeBodies) != 0 {
			t.Fatalf("after the cancel, the guard has %d values and the admission places %d, want 1 and 0", len(s.largeCommands), len(s.largeBodies))
		}
		<-s.largeCommands
		small := sendCommand(t.Context(), handler, pause, "gzip")
		synctest.Wait()
		if result, ok := finished(small); !ok || result.code != http.StatusOK {
			t.Fatalf("the small gzip command gave %+v, %t, want 200", result, ok)
		}
		if len(s.largeCommands) != 0 || len(s.largeBodies) != 0 {
			t.Fatal("the small gzip command did not release the guard and its place")
		}
	})
}

// TestLargeBodyAdmission sends plain bodies of almost MaxCommandBytes while
// the guard is held. maxLargeBodies of them wait, and the next one gets 503
// at once. A body with no Content-Length also needs a place. A small plain
// command does not need a place. The test then checks that each request
// gives back its place after a cancel, a success, and a failure.
func TestLargeBodyAdmission(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s := newTestSession(t)
		handler := s.Handler(t.TempDir())
		bodies := make([][]byte, maxLargeBodies+1)
		for index := range bodies {
			bodies[index] = paddedPause(t, s, fmt.Sprintf("large-%d", index), MaxCommandBytes)
		}
		small := paddedPause(t, s, "small", smallBodyBytes)
		unknown := pauseWithLanes(t, s, "unknown", "Lanes", 0)
		stored := gzipBody(t, paddedPause(t, s, "stored", MaxCommandBytes), gzip.NoCompression)
		s.largeCommands <- struct{}{}

		canceled, cancel := context.WithCancel(t.Context())
		results := []<-chan httpResult{sendCommand(canceled, handler, bodies[0])}
		for _, body := range bodies[1:maxLargeBodies] {
			results = append(results, sendCommand(t.Context(), handler, body))
		}
		synctest.Wait()
		if len(s.largeBodies) != maxLargeBodies {
			t.Fatalf("%d admission places are in use, want %d", len(s.largeBodies), maxLargeBodies)
		}
		recorder := postEncoded(t, handler, bodies[maxLargeBodies])
		if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "1" ||
			recorder.Body.String() != "the server is busy with other large commands, try again\n" {
			t.Fatalf("a body with no admission place gave status %d, Retry-After %q, reply %q", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
		}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/command", io.NopCloser(bytes.NewReader(unknown)))
		request.Header.Set("Content-Type", "application/json")
		request.ContentLength = -1
		recorder = httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("a body with no Content-Length gave status %d, want 503", recorder.Code)
		}
		if recorder := postEncoded(t, handler, small); recorder.Code != http.StatusOK {
			t.Fatalf("a small plain command gave status %d, reply %q, want 200", recorder.Code, recorder.Body.String())
		}

		cancel()
		synctest.Wait()
		if result, ok := finished(results[0]); !ok || result.code != http.StatusServiceUnavailable {
			t.Fatalf("the canceled command gave %+v, %t, want 503", result, ok)
		}
		if len(s.largeBodies) != maxLargeBodies-1 {
			t.Fatalf("after the cancel, %d admission places are in use, want %d", len(s.largeBodies), maxLargeBodies-1)
		}
		<-s.largeCommands
		synctest.Wait()
		for _, done := range results[1:] {
			if result, ok := finished(done); !ok || result.code != http.StatusOK {
				t.Fatalf("a large command gave %+v, %t, want 200", result, ok)
			}
		}
		for _, tc := range []struct {
			name      string
			body      []byte
			encodings []string
			code      int
		}{
			{"success", bodies[maxLargeBodies], nil, http.StatusOK},
			{"body over the limit", append(bytes.Clone(bodies[0]), ' '), nil, http.StatusRequestEntityTooLarge},
			{"compressed body over the limit", stored, []string{"gzip"}, http.StatusRequestEntityTooLarge},
			{"invalid gzip", bodies[0], []string{"gzip"}, http.StatusBadRequest},
		} {
			if recorder := postEncoded(t, handler, tc.body, tc.encodings...); recorder.Code != tc.code {
				t.Fatalf("%s: status %d, reply %q, want %d", tc.name, recorder.Code, recorder.Body.String(), tc.code)
			}
			if len(s.largeBodies) != 0 || len(s.largeCommands) != 0 {
				t.Fatalf("%s: the request did not release its place and the guard", tc.name)
			}
		}
	})
}

// TestUnreadBodyRepliesAtOnce sends request headers, and for some cases
// part of the body, to a real server, then holds back the rest of the body.
// The server must send each error reply that does not read the whole body
// at once and close the connection. It must not wait for the body. Each
// request accepts gzip, so compressResponse wraps the ResponseWriter. For
// the full cases, the test fills all admission places, so a gzip body and
// a body with no Content-Length get 503.
func TestUnreadBodyRepliesAtOnce(t *testing.T) {
	t.Parallel()
	jsonType := "Content-Type: application/json\r\n"
	over := strings.Repeat(" ", MaxCommandBytes+1)
	for _, tc := range []struct {
		name, headers, body, retryAfter string
		full                            bool
		code                            int
	}{
		{"gzip with no admission place", jsonType + "Content-Encoding: gzip\r\nContent-Length: 1000\r\n", "", "1", true, http.StatusServiceUnavailable},
		{"chunked with no admission place", jsonType + "Transfer-Encoding: chunked\r\n", "", "1", true, http.StatusServiceUnavailable},
		{"unknown content encoding", jsonType + "Content-Encoding: br\r\nContent-Length: 1000\r\n", "", "", false, http.StatusUnsupportedMediaType},
		{"media type", "Content-Type: text/plain\r\nContent-Length: 1000\r\n", "", "", false, http.StatusUnsupportedMediaType},
		{"cross origin", jsonType + "Origin: http://elsewhere\r\nContent-Length: 1000\r\n", "", "", false, http.StatusForbidden},
		{"stalled body over the limit", jsonType + fmt.Sprintf("Content-Length: %d\r\n", MaxCommandBytes+2), over, "", false, http.StatusRequestEntityTooLarge},
		{"unfinished chunked body over the limit", jsonType + "Transfer-Encoding: chunked\r\n", fmt.Sprintf("%x\r\n", len(over)) + over, "", false, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t)
			server := httptest.NewServer(s.Handler(t.TempDir()))
			defer server.Close()
			if tc.full {
				for range maxLargeBodies {
					s.largeBodies <- struct{}{}
				}
			}
			var dialer net.Dialer
			conn, err := dialer.DialContext(t.Context(), "tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			request := "POST /api/command HTTP/1.1\r\nHost: " + server.Listener.Addr().String() + "\r\nAccept-Encoding: gzip\r\n" + tc.headers + "\r\n" + tc.body
			if _, err = io.WriteString(conn, request); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatalf("no reply before the end of the body: %v", err)
			}
			_ = response.Body.Close()
			if response.StatusCode != tc.code || !response.Close || response.Header.Get("Retry-After") != tc.retryAfter {
				t.Fatalf("status %d, close %t, Retry-After %q, want %d, true and %q", response.StatusCode, response.Close, response.Header.Get("Retry-After"), tc.code, tc.retryAfter)
			}
			if tc.full && len(s.largeBodies) != maxLargeBodies {
				t.Fatalf("%d admission places are in use, want %d", len(s.largeBodies), maxLargeBodies)
			}
		})
	}
}
