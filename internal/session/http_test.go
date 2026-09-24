package session

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	handler := newTestSession(t).Handler(t.TempDir())
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", http.NoBody)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
		t.Fatalf("health response: status %d body %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/plain; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health headers: %v", response.Header())
	}
}

func TestStaticCacheControl(t *testing.T) {
	t.Parallel()
	module := bytes.Repeat([]byte("\x00asm test module"), 100)
	var artifact bytes.Buffer
	writer := gzip.NewWriter(&artifact)
	if _, err := writer.Write(module); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	modified := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	for name, body := range map[string][]byte{
		"index.html":     []byte("<!doctype html><title>index</title>"),
		"game.html":      []byte("<!doctype html><title>game</title>"),
		"editor.js":      []byte("console.log('editor');"),
		"editor.css":     []byte("body { margin: 0; }"),
		"podsim.wasm.gz": artifact.Bytes(),
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	stamp := modified.Format(http.TimeFormat)
	handler := newTestSession(t).Handler(directory)
	for _, tc := range []struct {
		name, path, encoding    string
		revalidate              bool
		wantStatus              int
		wantCache, wantModified string
	}{
		{name: "index", path: "/", encoding: "gzip", wantStatus: http.StatusOK, wantCache: "no-cache", wantModified: stamp},
		{name: "game page", path: "/game.html", encoding: "gzip", wantStatus: http.StatusOK, wantCache: "no-cache", wantModified: stamp},
		{name: "script", path: "/editor.js", encoding: "gzip", wantStatus: http.StatusOK, wantCache: "no-cache", wantModified: stamp},
		{name: "style sheet", path: "/editor.css", encoding: "gzip", wantStatus: http.StatusOK, wantCache: "no-cache", wantModified: stamp},
		{name: "precompressed module", path: "/podsim.wasm", encoding: "gzip", wantStatus: http.StatusOK, wantCache: "no-cache", wantModified: ""},
		{name: "uncompressed module", path: "/podsim.wasm", wantStatus: http.StatusOK, wantCache: "no-cache", wantModified: ""},
		{name: "revalidated index", path: "/", encoding: "gzip", revalidate: true, wantStatus: http.StatusNotModified, wantCache: "no-cache", wantModified: stamp},
		{name: "revalidated game page", path: "/game.html", encoding: "gzip", revalidate: true, wantStatus: http.StatusNotModified, wantCache: "no-cache", wantModified: stamp},
		{name: "state", path: "/api/state", encoding: "gzip", wantStatus: http.StatusOK, wantCache: "no-store"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, http.NoBody)
			request.Header.Set("Accept-Encoding", tc.encoding)
			if tc.revalidate {
				request.Header.Set("If-Modified-Since", stamp)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", response.Code, tc.wantStatus)
			}
			if got := response.Header().Get("Cache-Control"); got != tc.wantCache {
				t.Fatalf("Cache-Control %q, want %q", got, tc.wantCache)
			}
			if got := response.Header().Get("Last-Modified"); got != tc.wantModified {
				t.Fatalf("Last-Modified %q, want %q", got, tc.wantModified)
			}
			if tc.wantStatus == http.StatusNotModified && response.Body.Len() != 0 {
				t.Fatalf("not modified response has a %d byte body", response.Body.Len())
			}
		})
	}
}
