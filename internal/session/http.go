package session

import (
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

// Handler serves the session API and supplied static application files.
func (s *Session) Handler(directory string, routes ...func(*http.ServeMux)) http.Handler {
	return s.HandlerFS(os.DirFS(directory), routes...)
}

// HandlerFS serves the session API and supplied static application files.
func (s *Session) HandlerFS(files fs.FS, routes ...func(*http.ServeMux)) http.Handler {
	application := http.NewServeMux()
	application.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "ok\n")
	})
	// The API has its own mux, so that the static files never answer an
	// /api request. For an unknown path, or a known path with a different
	// method, this mux gives 404 or 405 with an Allow header. apiNotCacheable
	// sets the cache header of each API reply.
	api := http.NewServeMux()
	api.HandleFunc("GET /api/topology", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.Topology()) })
	api.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.Frame()) })
	api.HandleFunc("GET /api/project", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.Project()) })
	api.HandleFunc("POST /api/command", s.commandHTTP)
	application.Handle("/api", api)
	application.Handle("/api/", api)
	application.Handle("/", staticFiles(files))

	root := http.NewServeMux()
	for _, register := range routes {
		register(root)
	}
	root.Handle("/", precompressedWASM(files, compressResponse(application)))
	return apiNotCacheable(root)
}

func (s *Session) commandHTTP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != r.Host || parsed.Scheme != scheme {
			writeError(w, "cross-origin commands are not allowed", http.StatusForbidden)
			return
		}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, "use application/json", http.StatusUnsupportedMediaType)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	var command Command
	if err := decoder.Decode(&command); err != nil {
		writeError(w, "invalid command JSON", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, "send one command only", http.StatusBadRequest)
		return
	}
	reply := s.Apply(command)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if reply.Error != "" {
		w.WriteHeader(http.StatusConflict)
	}
	if err := json.NewEncoder(w).Encode(reply); err != nil {
		slog.Error("Encode command reply", slog.Any("error", err))
	}
}

// apiNotCacheable tells caches not to store a reply to an API request, also
// an error or a redirect that a mux writes. It runs before the muxes clean
// the path, so it checks the path before and after cleaning. Thus a
// redirect from /api//state or /api/./state to /api/state also has the
// header.
func apiNotCacheable(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || isAPIPath(path.Clean(r.URL.Path)) {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// isAPIPath reports whether p is /api or starts with /api/.
func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

// writeError sends a plain-text error that caches must not store.
func writeError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, status)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("Encode state", slog.Any("error", err))
	}
}
