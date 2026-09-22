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
)

// Handler serves the session API and supplied static application files.
func (s *Session) Handler(directory string) http.Handler {
	return s.HandlerFS(os.DirFS(directory))
}

// HandlerFS serves the session API and supplied static application files.
func (s *Session) HandlerFS(files fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.State()) })
	mux.HandleFunc("GET /api/project", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, s.Project()) })
	mux.HandleFunc("POST /api/command", s.commandHTTP)
	mux.Handle("/", http.FileServerFS(files))
	return precompressedWASM(files, compressResponse(mux))
}

func (s *Session) commandHTTP(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != r.Host || parsed.Scheme != scheme {
			http.Error(w, "cross-origin commands are not allowed", http.StatusForbidden)
			return
		}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "use application/json", http.StatusUnsupportedMediaType)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	var command Command
	if err := decoder.Decode(&command); err != nil {
		http.Error(w, "invalid command JSON", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "send one command only", http.StatusBadRequest)
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

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("Encode state", slog.Any("error", err))
	}
}
