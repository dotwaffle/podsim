package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
)

// maxCommandBytes is the largest body of a command request.
const maxCommandBytes = 2 << 20

// commandJSONLimits bound a command body before it is decoded. Without them,
// a body with an array of empty objects decodes to about 37 times its size,
// also for an action that does not use the project. Each array of a
// command has the limit that project.Validate applies to it. A project
// action with a larger array is not valid, and the other actions do not
// use the project. The decoder rejects an array at another path, so its
// limit is 0. encoding/json matches member names without case, so the
// limits do too. The scan goes on past invalid UTF-8, and the decoder then
// rejects the body.
//
// The string limit keeps a long string out of the error of a reply. The
// longest valid string of a command is a client ID of maxClientBytes bytes.
// A name in a project has at most 80 bytes. JSON escapes can make each
// byte 6 bytes, which gives at most 602 bytes with the quotes. The longest
// string of a preset has 47 bytes with the quotes.
var commandJSONLimits = jsonLimits{
	depth: 64, elements: 0, members: 256, stringBytes: 1024, foldNames: true, allowInvalidUTF8: true,
	arrays: map[string]int64{
		"/project/network/Nodes":                    project.MaxNodes,
		"/project/network/Lanes":                    project.MaxLanes,
		"/project/network/Stations":                 project.MaxStations,
		"/project/network/Stations/*/Berths":        project.MaxBerths,
		"/project/fleet":                            project.MaxPods,
		"/project/demandProfiles":                   project.MaxProfiles,
		"/project/demandProfiles/*/bands":           project.MaxBands,
		"/project/demandProfiles/*/flows":           project.MaxFlows,
		"/project/demandProfiles/*/flows/*/weights": project.MaxBands,
	},
}

// errCommandShape means that a command body is larger than a limit of
// commandJSONLimits.
var errCommandShape = errors.New("command JSON is larger than a shape limit")

// prescanCommand checks body against commandJSONLimits. It returns an error
// that wraps errCommandShape when the body is larger than a limit. For other
// errors, such as a syntax error, the decoder gives the reply, so that the
// reply is the same as without the scan.
func prescanCommand(body []byte) error {
	err := prescanJSON(body, commandJSONLimits)
	if errors.Is(err, errJSONTooDeep) || errors.Is(err, errJSONArrayTooLong) || errors.Is(err, errJSONObjectTooLong) ||
		errors.Is(err, errJSONStringTooLong) {
		return fmt.Errorf("%w: %w", errCommandShape, err)
	}
	return err
}

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
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCommandBytes))
	if err != nil || errors.Is(prescanCommand(body), errCommandShape) {
		writeError(w, "invalid command JSON", http.StatusBadRequest)
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
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
