package session

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
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
	t.Parallel()
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
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
