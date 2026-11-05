package session

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
)

// PresetGenerator builds one project. The server calls it for each request,
// so that the memory of the project is free after the reply.
type PresetGenerator func() (project.Config, error)

// projectFileHeader is on the reply of a preset request when the server has
// a project file. A project apply overwrites that file. The header keeps the
// reply body the same as a project file, so the editor reads the body as it
// reads an imported file.
const projectFileHeader = "X-Podsim-Project-File"

// presetRetryAfter is the Retry-After value in seconds when another
// generation is in progress. The largest preset takes about 2 s.
const presetRetryAfter = "2"

// WithPresets serves each generator at POST /api/presets/{name}. Without
// generators, the server does not have this endpoint. The browser module
// links this package, so the caller supplies the generators. This keeps the
// preset data out of the browser module.
func WithPresets(presets map[string]PresetGenerator) Option {
	return func(s *Session) { s.presets = presets }
}

// presetHTTP replies with the JSON of a new project from the named preset.
// A preset request has no body, and the handler does not read one.
//
// The refusals come before the generation and in this order: the origin, the
// name, and a generation in progress. The place in s.presetSlot stays in use
// until the reply is sent, because the reply bytes use memory until then. A
// failed generation sends no part of a project.
func (s *Session) presetHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength != 0 {
		closeUnread(w)
	}
	if !s.originAllowed(r) {
		writeError(w, "cross-origin requests are not allowed", http.StatusForbidden)
		return
	}
	name := r.PathValue("name")
	generate, ok := s.presets[name]
	if !ok {
		writeError(w, "unknown preset", http.StatusNotFound)
		return
	}
	select {
	case s.presetSlot <- struct{}{}:
		defer func() { <-s.presetSlot }()
	default:
		w.Header().Set("Retry-After", presetRetryAfter)
		writeError(w, "the server is generating another preset, try again", http.StatusServiceUnavailable)
		return
	}
	data, failure := s.encodePreset(r.Context(), name, generate)
	if failure != nil {
		writeError(w, failure.message, failure.status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if s.saveProject != nil {
		w.Header().Set(projectFileHeader, "true")
	}
	// #nosec G705 -- The reply is JSON with a JSON content type.
	if _, err := w.Write(data); err != nil {
		s.logger.Warn("Send preset", slog.String("name", name), slog.Any("error", err))
		return
	}
	// The slot covers the whole reply, and compressResponse closes its writer
	// only after this function returns.
	if compressed, ok := w.(*compressedResponse); ok {
		if err := compressed.finishBody(); err != nil {
			s.logger.Warn("Send preset", slog.String("name", name), slog.Any("error", err))
		}
	}
}

// encodePreset builds the preset and gives its JSON. It holds the large
// command guard while it works, so that a generation and a large project
// apply do not use their memory at the same time. It logs the result with
// the time that it took after it got the guard.
func (s *Session) encodePreset(ctx context.Context, name string, generate PresetGenerator) ([]byte, *errorReply) {
	unguard, failure := s.waitLargeCommand(ctx)
	defer unguard()
	if failure != nil {
		return nil, failure
	}
	started := time.Now()
	data, err := presetJSON(generate)
	elapsed := time.Since(started)
	if err != nil {
		s.logger.Error("Generate preset", slog.String("name", name), slog.Duration("duration", elapsed), slog.Any("error", err))
		return nil, &errorReply{"the server could not generate the preset", http.StatusInternalServerError}
	}
	s.logger.Info("Generated preset", slog.String("name", name), slog.Duration("duration", elapsed), slog.Int("bytes", len(data)))
	return data, nil
}

// presetJSON gives the JSON of the project of generate, in the encoding of
// the project in GET /api/project. A panic in generate gives an error. The
// generators read embedded data, so a panic is a fault of one request, and
// it does not affect the session.
func presetJSON(generate PresetGenerator) (data []byte, err error) {
	defer func() {
		if cause := recover(); cause != nil {
			err = fmt.Errorf("panic: %v", cause)
		}
	}()
	config, err := generate()
	if err != nil {
		return nil, err
	}
	return jsonv2.Marshal(config, json.DefaultOptionsV1())
}
