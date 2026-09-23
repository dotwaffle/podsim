package session

import (
	"io"
	"io/fs"
	"net/http"
)

// staticCacheControl makes browsers revalidate a cached browser file before they use it.
// A reload after a server upgrade then loads the new files.
const staticCacheControl = "no-cache"

// staticFiles serves the browser files with staticCacheControl.
func staticFiles(files fs.FS) http.Handler {
	server := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", staticCacheControl)
		server.ServeHTTP(w, r)
	})
}

// precompressedWASM serves the build artifact when it matches the source file's age.
// Range requests retain the uncompressed representation and its byte offsets.
func precompressedWASM(files fs.FS, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/podsim.wasm" || r.Method != http.MethodGet && r.Method != http.MethodHead || r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			fallback.ServeHTTP(w, r)
			return
		}
		source, err := fs.Stat(files, "podsim.wasm")
		if err != nil || !source.Mode().IsRegular() {
			fallback.ServeHTTP(w, r)
			return
		}
		compressed, err := files.Open("podsim.wasm.gz")
		if err != nil {
			fallback.ServeHTTP(w, r)
			return
		}
		defer func() { _ = compressed.Close() }()
		info, err := compressed.Stat()
		if err != nil || !info.Mode().IsRegular() || info.ModTime().Before(source.ModTime()) {
			fallback.ServeHTTP(w, r)
			return
		}
		seeker, ok := compressed.(io.ReadSeeker)
		if !ok {
			fallback.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/wasm")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Cache-Control", staticCacheControl)
		w.Header().Add("Vary", "Accept-Encoding")
		http.ServeContent(w, r, "podsim.wasm", source.ModTime(), seeker)
	})
}
