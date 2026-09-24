package session

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

// gzipModule returns raw compressed with gzip.
func gzipModule(t *testing.T, raw []byte) []byte {
	t.Helper()
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func TestPrecompressedWASM(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte("\x00asm test module"), 100)
	encoded := gzipModule(t, raw)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "podsim.wasm.gz"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestSession(t).Handler(directory)
	for _, tc := range []struct {
		name, method, encoding, byteRange string
		wantStatus                        int
		wantGzip                          bool
		want                              []byte
	}{
		{name: "gzip", encoding: "gzip", wantStatus: http.StatusOK, wantGzip: true, want: encoded},
		{name: "gzip head", method: http.MethodHead, encoding: "gzip", wantStatus: http.StatusOK, wantGzip: true, want: encoded},
		{name: "identity", encoding: "gzip;q=0", wantStatus: http.StatusOK, want: raw},
		{name: "no encoding", wantStatus: http.StatusOK, want: raw},
		{name: "identity head", method: http.MethodHead, wantStatus: http.StatusOK, want: raw},
		{name: "range", encoding: "gzip", byteRange: "bytes=4-19", wantStatus: http.StatusPartialContent, want: raw[4:20]},
		{name: "identity range", byteRange: "bytes=1500-", wantStatus: http.StatusPartialContent, want: raw[1500:]},
		{name: "range head", method: http.MethodHead, byteRange: "bytes=0-3", wantStatus: http.StatusPartialContent, want: raw[:4]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), tc.method, "/podsim.wasm", http.NoBody)
			request.Header.Set("Accept-Encoding", tc.encoding)
			request.Header.Set("Range", tc.byteRange)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", response.Code, tc.wantStatus)
			}
			header := response.Header()
			if got := header.Get("Cache-Control"); got != "no-cache" {
				t.Fatalf("Cache-Control %q", got)
			}
			if got := header.Get("Content-Type"); got != "application/wasm" {
				t.Fatalf("Content-Type %q", got)
			}
			if got := header.Get("Vary"); got != "Accept-Encoding" {
				t.Fatalf("Vary %q", got)
			}
			if got := header.Get("Content-Encoding") == "gzip"; got != tc.wantGzip {
				t.Fatalf("Content-Encoding %q", header.Get("Content-Encoding"))
			}
			// ServeContent sets no Content-Length for an encoded body.
			if got := header.Get("Content-Length"); !tc.wantGzip && got != strconv.Itoa(len(tc.want)) {
				t.Fatalf("Content-Length %s, want %d", got, len(tc.want))
			}
			if tc.method == http.MethodHead {
				if response.Body.Len() != 0 {
					t.Fatal("HEAD response has a body")
				}
				return
			}
			if !bytes.Equal(response.Body.Bytes(), tc.want) {
				t.Fatal("incorrect response body")
			}
		})
	}
}

// TestPrecompressedWASMRawOnly checks a -dir tree from an older build that
// has only podsim.wasm.
func TestPrecompressedWASMRawOnly(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte("\x00asm old module"), 100)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "podsim.wasm"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestSession(t).Handler(directory)
	for _, tc := range []struct{ encoding, byteRange string }{{"", ""}, {"gzip", ""}, {"", "bytes=0-3"}} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/podsim.wasm", http.NoBody)
		request.Header.Set("Accept-Encoding", tc.encoding)
		request.Header.Set("Range", tc.byteRange)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		body := response.Body.Bytes()
		if response.Header().Get("Content-Encoding") == "gzip" {
			reader, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if body, err = io.ReadAll(reader); err != nil {
				t.Fatal(err)
			}
		}
		want := raw
		if tc.byteRange != "" {
			want = raw[:4]
		}
		if response.Code >= 300 || !bytes.Equal(body, want) {
			t.Fatalf("%q %q: status %d, incorrect body", tc.encoding, tc.byteRange, response.Code)
		}
	}
}

// countingFS counts the bytes that the handler reads from its files.
type countingFS struct {
	fs.FS
	read *atomic.Int64
}

func (c countingFS) Open(name string) (fs.File, error) {
	file, err := c.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return countingFile{File: file, read: c.read}, nil
}

type countingFile struct {
	fs.File
	read *atomic.Int64
}

func (c countingFile) Read(p []byte) (int, error) {
	n, err := c.File.Read(p)
	c.read.Add(int64(n))
	return n, err
}

func (c countingFile) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := c.File.(io.Seeker)
	if !ok {
		return 0, errors.ErrUnsupported
	}
	return seeker.Seek(offset, whence)
}

func TestPrecompressedWASMDecompressesOnce(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte("\x00asm shared module"), 4096)
	encoded := gzipModule(t, raw)
	var read atomic.Int64
	files := countingFS{FS: fstest.MapFS{"podsim.wasm.gz": {Data: encoded}}, read: &read}
	handler := newTestSession(t).HandlerFS(files)
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for range 16 {
		wg.Go(func() {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/podsim.wasm", http.NoBody)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), raw) {
				errs <- "incorrect identity response"
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	// Each request reads the 8-byte gzip trailer to identify the file.
	if got, want := read.Load(), int64(len(encoded)+16*8); got != want {
		t.Fatalf("read %d compressed bytes, want %d for one decompression", got, want)
	}
}

// TestPrecompressedWASMReplaced checks that a new podsim.wasm.gz in a -dir
// tree replaces the kept module.
func TestPrecompressedWASMReplaced(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "podsim.wasm.gz")
	handler := newTestSession(t).Handler(directory)
	for i, raw := range [][]byte{[]byte("\x00asm first"), []byte("\x00asm second build")} {
		if err := os.WriteFile(path, gzipModule(t, raw), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Date(2026, time.September, 1+i, 12, 0, 0, 0, time.UTC)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/podsim.wasm", http.NoBody)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if !bytes.Equal(response.Body.Bytes(), raw) {
			t.Fatalf("build %d: body %q, want %q", i, response.Body.Bytes(), raw)
		}
	}
}

func TestPrecompressedWASMCorrupt(t *testing.T) {
	t.Parallel()
	files := fstest.MapFS{"podsim.wasm.gz": {Data: []byte("not gzip")}}
	handler := newTestSession(t).HandlerFS(files)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/podsim.wasm", http.NoBody)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", response.Code)
	}
}

// TestPrecompressedWASMRawNewer checks that a podsim.wasm that is newer than
// podsim.wasm.gz wins, as in a -dir tree where only the raw module was
// rebuilt.
func TestPrecompressedWASMRawNewer(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	old, current := []byte("\x00asm old build"), []byte("\x00asm new raw build")
	writeStamped(t, filepath.Join(directory, "podsim.wasm.gz"), gzipModule(t, old), 1)
	writeStamped(t, filepath.Join(directory, "podsim.wasm"), current, 2)
	handler := newTestSession(t).Handler(directory)
	for _, encoding := range []string{"", "gzip"} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/podsim.wasm", http.NoBody)
		request.Header.Set("Accept-Encoding", encoding)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		body := response.Body.Bytes()
		if response.Header().Get("Content-Encoding") == "gzip" {
			reader, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if body, err = io.ReadAll(reader); err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(body, current) {
			t.Fatalf("%q: body %q, want %q", encoding, body, current)
		}
	}
}

// TestPrecompressedWASMReplacedSameStamp checks that a new podsim.wasm.gz
// with the same size and modification time replaces the kept module, and
// that a conditional request with the old validators gets the new module.
func TestPrecompressedWASMReplacedSameStamp(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "podsim.wasm.gz")
	handler := newTestSession(t).Handler(directory)
	first, second := []byte("\x00asm build A"), []byte("\x00asm build B")
	writeStamped(t, path, gzipModule(t, first), 1)
	for _, encoding := range []string{"", "gzip"} {
		get := func(etag, modified string) *httptest.ResponseRecorder {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/podsim.wasm", http.NoBody)
			request.Header.Set("Accept-Encoding", encoding)
			request.Header.Set("If-None-Match", etag)
			request.Header.Set("If-Modified-Since", modified)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			return response
		}
		writeStamped(t, path, gzipModule(t, first), 1)
		response := get("", "")
		etag, modified := response.Header().Get("ETag"), time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC).Format(http.TimeFormat)
		if etag == "" {
			t.Fatalf("%q: no ETag", encoding)
		}
		if unchanged := get(etag, modified); unchanged.Code != http.StatusNotModified {
			t.Fatalf("%q: unchanged module status %d, want 304", encoding, unchanged.Code)
		}
		writeStamped(t, path, gzipModule(t, second), 1)
		response = get(etag, modified)
		body := response.Body.Bytes()
		if encoding == "gzip" {
			reader, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if body, err = io.ReadAll(reader); err != nil {
				t.Fatal(err)
			}
		}
		if response.Code != http.StatusOK || !bytes.Equal(body, second) {
			t.Fatalf("%q: status %d, body %q, want 200 %q", encoding, response.Code, body, second)
		}
		// Date validators must not match, because the module has no
		// Last-Modified header.
		for _, header := range []string{"If-Modified-Since", "If-Range"} {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/podsim.wasm", http.NoBody)
			request.Header.Set("Accept-Encoding", encoding)
			request.Header.Set(header, modified)
			if header == "If-Range" {
				request.Header.Set("Range", "bytes=0-3")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Header().Get("Last-Modified") != "" {
				t.Fatalf("%q %s: status %d, Last-Modified %q, want 200 without Last-Modified",
					encoding, header, response.Code, response.Header().Get("Last-Modified"))
			}
		}
	}
}

// writeStamped writes data to path with a modification time on the given
// day of September 2026.
func writeStamped(t *testing.T, path string, data []byte, day int) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, time.September, day, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}
