package session

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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

// TestErrorsNotCacheable checks that each error reply tells caches not to
// store it, as the other API replies do. It also checks that the static
// files do not answer an unknown API path or a wrong API method.
func TestErrorsNotCacheable(t *testing.T) {
	t.Parallel()
	handler := newTestSession(t).HandlerFS(fstest.MapFS{"podsim.wasm.gz": {Data: []byte("not gzip")}})
	for _, tc := range []struct {
		name, method, path, body, origin, contentType string
		wantStatus                                    int
		wantAllow                                     string
	}{
		{name: "cross origin", method: http.MethodPost, path: "/api/command", body: "{}", origin: "http://elsewhere", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "media type", method: http.MethodPost, path: "/api/command", body: "{}", contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "invalid JSON", method: http.MethodPost, path: "/api/command", body: `{"unknown":1}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "long string", method: http.MethodPost, path: "/api/command", body: `{"client":"` + strings.Repeat("a", (2<<20)+1) + `"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "oversize", method: http.MethodPost, path: "/api/command", body: `{"client":"` + strings.Repeat("a", MaxCommandBytes+1) + `"}`, contentType: "application/json", wantStatus: http.StatusRequestEntityTooLarge},
		{name: "two commands", method: http.MethodPost, path: "/api/command", body: "{} {}", contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "rejected command", method: http.MethodPost, path: "/api/command", body: "{}", contentType: "application/json", wantStatus: http.StatusConflict},
		{name: "WASM module", method: http.MethodGet, path: "/podsim.wasm", wantStatus: http.StatusInternalServerError},
		{name: "unknown API path", method: http.MethodGet, path: "/api/missing", wantStatus: http.StatusNotFound},
		{name: "API root", method: http.MethodGet, path: "/api", wantStatus: http.StatusNotFound},
		{name: "API root with slash", method: http.MethodGet, path: "/api/", wantStatus: http.StatusNotFound},
		{name: "read of command", method: http.MethodGet, path: "/api/command", wantStatus: http.StatusMethodNotAllowed, wantAllow: "POST"},
		{name: "post to state", method: http.MethodPost, path: "/api/state", body: "{}", contentType: "application/json", wantStatus: http.StatusMethodNotAllowed, wantAllow: "GET, HEAD"},
		{name: "delete of project", method: http.MethodDelete, path: "/api/project", wantStatus: http.StatusMethodNotAllowed, wantAllow: "GET, HEAD"},
		{name: "head of state", method: http.MethodHead, path: "/api/state", wantStatus: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), tc.method, "http://example.com"+tc.path, strings.NewReader(tc.body))
			if tc.origin != "" {
				request.Header.Set("Origin", tc.origin)
			}
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d, body %q", response.Code, tc.wantStatus, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control %q, want no-store", got)
			}
			if got := response.Header().Get("Allow"); got != tc.wantAllow {
				t.Fatalf("Allow %q, want %q", got, tc.wantAllow)
			}
		})
	}
}

// TestAPIRedirectsNotCacheable checks that a redirect to the clean form of
// an API path tells caches not to store it, and that a static file keeps
// its cache header.
func TestAPIRedirectsNotCacheable(t *testing.T) {
	t.Parallel()
	handler := newTestSession(t).HandlerFS(fstest.MapFS{"editor.js": {Data: []byte("console.log('editor');")}})
	for _, tc := range []struct {
		name, path, wantCache, wantLocation string
		wantStatus                          int
	}{
		{name: "duplicate slash", path: "/api//state", wantStatus: http.StatusTemporaryRedirect, wantCache: "no-store", wantLocation: "/api/state"},
		{name: "dot segment", path: "/api/./project", wantStatus: http.StatusTemporaryRedirect, wantCache: "no-store", wantLocation: "/api/project"},
		{name: "leading dot segment", path: "/./api/topology", wantStatus: http.StatusTemporaryRedirect, wantCache: "no-store", wantLocation: "/api/topology"},
		{name: "static file", path: "/editor.js", wantStatus: http.StatusOK, wantCache: "no-cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com"+tc.path, http.NoBody)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d", response.Code, tc.wantStatus)
			}
			if got := response.Header().Get("Cache-Control"); got != tc.wantCache {
				t.Fatalf("Cache-Control %q, want %q", got, tc.wantCache)
			}
			if got := response.Header().Get("Location"); got != tc.wantLocation {
				t.Fatalf("Location %q, want %q", got, tc.wantLocation)
			}
		})
	}
}
