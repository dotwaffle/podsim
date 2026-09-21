package session

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCompressionNegotiation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		header string
		want   bool
	}{{"", false}, {"br", false}, {"gzip", true}, {"br, gzip;q=0.5", true}, {"gzip;q=0, *;q=1", false}, {"*;q=1, gzip;q=0", false}, {"*", true}, {"gzip;q=bad", false}, {"GZIP; q=1", true}, {"gzip;q=NaN", false}} {
		if got := acceptsGzip(tc.header); got != tc.want {
			t.Errorf("%q: %v", tc.header, got)
		}
	}
}

func TestCompressedSnapshotsAndAssets(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	asset := bytes.Repeat([]byte("\x00asm example"), 1024)
	if err := os.WriteFile(filepath.Join(directory, "podsim.wasm"), asset, 0o600); err != nil {
		t.Fatal(err)
	}
	shared := newTestSession(t)
	handler := shared.Handler(directory)
	for _, path := range []string{"/api/state", "/podsim.wasm"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Accept-Encoding", "gzip")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != 200 || response.Header().Get("Content-Encoding") != "gzip" || response.Header().Get("Vary") != "Accept-Encoding" {
				t.Fatalf("headers %v status %d", response.Header(), response.Code)
			}
			if response.Header().Get("Content-Length") != "" {
				t.Fatal("retained uncompressed content length")
			}
			reader, err := gzip.NewReader(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if path == "/api/state" {
				var state State
				if err := json.Unmarshal(decoded, &state); err != nil {
					t.Fatal(err)
				}
				if state.Epoch != shared.State().Epoch {
					t.Fatal("incorrect snapshot")
				}
			} else if !bytes.Equal(decoded, asset) || response.Header().Get("Content-Type") != "application/wasm" {
				t.Fatal("invalid WASM response")
			}
		})
	}
	for _, tc := range []struct{ encoding, byteRange string }{{"gzip;q=0", ""}, {"gzip", "bytes=0-3"}} {
		request := httptest.NewRequest(http.MethodGet, "/podsim.wasm", nil)
		request.Header.Set("Accept-Encoding", tc.encoding)
		request.Header.Set("Range", tc.byteRange)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Header().Get("Content-Encoding") != "" {
			t.Fatal("compressed identity/range response")
		}
		want := asset
		if tc.byteRange != "" {
			want = asset[:4]
		}
		if !bytes.Equal(response.Body.Bytes(), want) {
			t.Fatal("invalid identity/range body")
		}
	}
}

func TestCompressionKeepsBodylessResponsesEmpty(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		handler := compressResponse(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Encoding", "gzip")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != status || response.Body.Len() != 0 {
			t.Fatalf("status%d body%q", response.Code, response.Body.String())
		}
	}
}

func TestCompressedEmptyFile(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "empty.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newTestSession(t).Handler(directory)
	request := httptest.NewRequest(http.MethodGet, "/empty.txt", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil || len(body) != 0 {
		t.Fatalf("body%q, err%v", body, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
}
