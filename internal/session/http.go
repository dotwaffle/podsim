package session

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
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

const (
	// MaxCommandBytes is the largest body of a command request, as it
	// comes from the network. For a body with the gzip content encoding,
	// this is the compressed size.
	MaxCommandBytes = 4 << 20
	// MaxInflatedCommandBytes is the largest command JSON in a body with
	// the gzip content encoding. A project command holds the compact JSON
	// form of a project, and project.Validate limits this form to
	// project.MaxFileBytes. The other members of the command use much less
	// than the 64 KiB that remain. A project that is larger than
	// MaxCommandBytes must come in a gzip body.
	MaxInflatedCommandBytes = project.MaxFileBytes + 64<<10
)

// largeCommandBytes is the largest plain command body that the server
// decodes without the large command guard of the session. The guard lets
// one gzip body, or one larger plain body, decompress, decode and apply at
// a time. At 8 MiB, one request allocates about 200 MB, so the guard
// limits the memory of concurrent large commands.
const largeCommandBytes = 1 << 20

// errContentEncoding means that a command request has a content encoding
// other than gzip.
var errContentEncoding = errors.New("use the gzip content encoding or no content encoding")

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
	body, release, failure := s.readCommandBody(w, r)
	defer release()
	if failure == nil && errors.Is(prescanCommand(body), errCommandShape) {
		failure = &errorReply{"invalid command JSON", http.StatusBadRequest}
	}
	if failure != nil {
		writeError(w, failure.message, failure.status)
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

// readCommandBody reads the command JSON of r. It decompresses a body with
// the gzip content encoding. For a body with a content encoding that it
// does not know, it sets the Accept-Encoding header of w.
//
// For a gzip body, and for a plain body of more than largeCommandBytes,
// readCommandBody waits for the large command guard. It reads the body
// from the network before it waits, so a slow client does not keep the
// guard. It decompresses a gzip body only after it has the guard. A small
// gzip body can decompress to a large size, so a waiting request keeps
// only the bytes that it received. The caller must call release after it
// applies the command. release is never nil.
func (s *Session) readCommandBody(w http.ResponseWriter, r *http.Request) (body []byte, release func(), failure *errorReply) {
	gzipped, err := gzipEncoded(r.Header)
	if err != nil {
		w.Header().Set("Accept-Encoding", "gzip")
		return nil, noRelease, &errorReply{err.Error(), http.StatusUnsupportedMediaType}
	}
	body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, MaxCommandBytes))
	if tooLarge := new(http.MaxBytesError); errors.As(err, &tooLarge) {
		return nil, noRelease, &errorReply{fmt.Sprintf("the command body must have at most %d bytes", tooLarge.Limit), http.StatusRequestEntityTooLarge}
	}
	if err != nil {
		return nil, noRelease, &errorReply{"invalid command JSON", http.StatusBadRequest}
	}
	if !gzipped && len(body) <= largeCommandBytes {
		return body, noRelease, nil
	}
	release, failure = s.waitLargeCommand(r.Context())
	if failure == nil && gzipped {
		body, failure = inflateCommand(body)
	}
	return body, release, failure
}

// noRelease is the release function of a command that does not have the
// large command guard.
func noRelease() {}

// waitLargeCommand waits for the large command guard of the session. It
// gives a failure when ctx ends first.
func (s *Session) waitLargeCommand(ctx context.Context) (func(), *errorReply) {
	select {
	case s.largeCommands <- struct{}{}:
		return func() { <-s.largeCommands }, nil
	case <-ctx.Done():
		return noRelease, &errorReply{"the request ended while it waited for another large command", http.StatusServiceUnavailable}
	}
}

// gzipEncoded reports whether a command request with header has the gzip
// content encoding. A request with no content encoding, or with identity,
// gives false. Another encoding, or a list of more than one encoding,
// gives errContentEncoding.
func gzipEncoded(header http.Header) (bool, error) {
	values := header.Values("Content-Encoding")
	if len(values) == 0 {
		return false, nil
	}
	if len(values) == 1 {
		switch strings.ToLower(strings.TrimSpace(values[0])) {
		case "", "identity":
			return false, nil
		case "gzip", "x-gzip":
			return true, nil
		default:
		}
	}
	return false, errContentEncoding
}

// errorReply is the status and the text of an error reply.
type errorReply struct {
	message string
	status  int
}

// inflateCommand decompresses compressed, the gzip body of a command. The
// command JSON must have at most MaxInflatedCommandBytes. The limit reader
// stops the decompression one byte after the limit, so a small body that
// decompresses to a very large size cannot use more memory. The body must
// have one gzip member and no data after it. The gzip reader uses the
// ReadByte method of bytes.Reader, so it does not read past the member, and
// the bytes that remain are the data after it.
func inflateCommand(compressed []byte) ([]byte, *errorReply) {
	source := bytes.NewReader(compressed)
	reader, err := gzip.NewReader(source)
	if err != nil {
		return nil, &errorReply{"invalid gzip body", http.StatusBadRequest}
	}
	reader.Multistream(false)
	// The gzip trailer gives the decompressed size modulo 2^32. The client
	// sets this value, so it is only a capacity hint, and it is at most the
	// limit. With the correct size, the buffer does not grow.
	hint := int(min(binary.LittleEndian.Uint32(compressed[len(compressed)-4:]), MaxInflatedCommandBytes+1))
	buffer := bytes.NewBuffer(make([]byte, 0, hint+bytes.MinRead))
	_, err = buffer.ReadFrom(io.LimitReader(reader, MaxInflatedCommandBytes+1))
	switch {
	case buffer.Len() > MaxInflatedCommandBytes:
		return nil, &errorReply{fmt.Sprintf("the decompressed command body must have at most %d bytes", MaxInflatedCommandBytes), http.StatusRequestEntityTooLarge}
	case err != nil:
		return nil, &errorReply{"invalid gzip body", http.StatusBadRequest}
	case source.Len() > 0:
		return nil, &errorReply{"send one gzip member only", http.StatusBadRequest}
	default:
		return buffer.Bytes(), nil
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
