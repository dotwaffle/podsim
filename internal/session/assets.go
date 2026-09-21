package session

import (
	"net/http"
	"os"
	"path/filepath"
)

// precompressedWASM serves the build artifact when it matches the source file's age.
// Range requests retain the uncompressed representation and its byte offsets.
func precompressedWASM(directory string, fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/podsim.wasm" || r.Method != http.MethodGet && r.Method != http.MethodHead || r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			fallback.ServeHTTP(w, r)
			return
		}
		source, err := os.Stat(filepath.Join(directory, "podsim.wasm"))
		if err != nil || !source.Mode().IsRegular() {
			fallback.ServeHTTP(w, r)
			return
		}
		compressed, err := os.Open(filepath.Join(directory, "podsim.wasm.gz")) // #nosec G304 -- The operator supplies the static build directory.
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
		w.Header().Set("Content-Type", "application/wasm")
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		http.ServeContent(w, r, "podsim.wasm", source.ModTime(), compressed)
	})
}
