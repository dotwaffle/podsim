package session

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrecompressedWASM(t *testing.T) {
	t.Parallel()
	raw := bytes.Repeat([]byte("\x00asm test module"), 100)
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	writer.Name = "build artifact"
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, encoding, byteRange string
		stale, missing                    bool
	}{
		{name: "artifact", encoding: "gzip"},
		{name: "head", method: http.MethodHead, encoding: "gzip"},
		{name: "identity", encoding: "gzip;q=0"},
		{name: "range", encoding: "gzip", byteRange: "bytes=0-3"},
		{name: "stale", encoding: "gzip", stale: true},
		{name: "missing", encoding: "gzip", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			source := filepath.Join(directory, "podsim.wasm")
			if err := os.WriteFile(source, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			artifact := source + ".gz"
			if !tc.missing {
				if err := os.WriteFile(artifact, encoded.Bytes(), 0o600); err != nil {
					t.Fatal(err)
				}
				if tc.stale {
					old := time.Now().Add(-time.Hour)
					if err := os.Chtimes(artifact, old, old); err != nil {
						t.Fatal(err)
					}
				}
			}
			handler := newTestSession(t).Handler(directory)
			request := httptest.NewRequestWithContext(t.Context(), tc.method, "/podsim.wasm", http.NoBody)
			request.Header.Set("Accept-Encoding", tc.encoding)
			request.Header.Set("Range", tc.byteRange)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			wantStatus := http.StatusOK
			if tc.byteRange != "" {
				wantStatus = http.StatusPartialContent
			}
			if response.Code != wantStatus {
				t.Fatalf("status %d", response.Code)
			}
			if tc.method == http.MethodHead {
				if response.Body.Len() != 0 || response.Header().Get("Content-Encoding") != "gzip" {
					t.Fatal("invalid HEAD response")
				}
				return
			}
			body := response.Body.Bytes()
			if response.Header().Get("Content-Encoding") == "gzip" {
				if tc.encoding == "gzip;q=0" || tc.byteRange != "" {
					t.Fatal("compressed identity or range")
				}
				if !tc.stale && !tc.missing && !bytes.Equal(body, encoded.Bytes()) {
					t.Fatal("did not serve build artifact")
				}
				if (tc.stale || tc.missing) && bytes.Equal(body, encoded.Bytes()) {
					t.Fatal("served stale artifact")
				}
				reader, err := gzip.NewReader(bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				body, err = io.ReadAll(reader)
				if err != nil {
					t.Fatal(err)
				}
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
			} else if tc.encoding == "gzip" && tc.byteRange == "" {
				t.Fatal("missing gzip encoding")
			}
			want := raw
			if tc.byteRange != "" {
				want = raw[:4]
			}
			if !bytes.Equal(body, want) {
				t.Fatal("incorrect response body")
			}
		})
	}
}
