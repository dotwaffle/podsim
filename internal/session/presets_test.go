package session

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
)

// presetFixture is a session with presets and a test server, with a log that
// the test can read after the requests.
type presetFixture struct {
	session *Session
	server  *httptest.Server
	log     *lockedBuffer
}

// lockedBuffer is a log destination for handlers that run in other
// goroutines.
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func newPresetFixture(t *testing.T, presets map[string]PresetGenerator, options ...Option) presetFixture {
	t.Helper()
	log := new(lockedBuffer)
	options = append(options, WithPresets(presets), WithLogger(slog.New(slog.NewTextHandler(log, nil))))
	shared, err := NewWithProject(project.Default(), options...)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(shared.HandlerFS(fstest.MapFS{}))
	t.Cleanup(server.Close)
	t.Cleanup(shared.Close)
	return presetFixture{session: shared, server: server, log: log}
}

// presetResult is a preset reply after the helper closed its body.
type presetResult struct {
	StatusCode int
	Header     http.Header
}

// post sends a preset request and gives the reply with its body.
func (f presetFixture) post(t *testing.T, method, name string, change ...func(*http.Request)) (presetResult, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, f.server.URL+"/api/presets/"+name, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	for _, apply := range change {
		apply(request)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return presetResult{StatusCode: response.StatusCode, Header: response.Header}, body
}

// status sends a preset request from another goroutine than the test and gives
// the status, or 0 when the request fails.
func (f presetFixture) status(ctx context.Context, name string) int {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+"/api/presets/"+name, http.NoBody)
	if err != nil {
		return 0
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		return 0
	}
	defer func() { _ = response.Body.Close() }()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return 0
	}
	return response.StatusCode
}

func namedProject(name string) PresetGenerator {
	return func() (project.Config, error) {
		config := project.Default()
		config.Name = name
		return config, nil
	}
}

func TestPresetRepliesWithProjectFile(t *testing.T) {
	t.Parallel()
	fixture := newPresetFixture(t, map[string]PresetGenerator{"one": namedProject("One")})
	response, body := fixture.post(t, http.MethodPost, "one")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", response.StatusCode, body)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := response.Header.Get(projectFileHeader); got != "" {
		t.Errorf("%s = %q without a project file", projectFileHeader, got)
	}
	var config project.Config
	if err := json.Unmarshal(body, &config, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if config.Name != "One" || project.Validate(config) != nil {
		t.Fatalf("the reply has project %q, valid: %v", config.Name, project.Validate(config))
	}
	log := fixture.log.String()
	for _, want := range []string{"Generated preset", "name=one", "duration=", "bytes=" + strconv.Itoa(len(body))} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %q:\n%s", want, log)
		}
	}
}

func TestPresetCompressesTheReply(t *testing.T) {
	t.Parallel()
	fixture := newPresetFixture(t, map[string]PresetGenerator{"one": namedProject("One")})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fixture.server.URL+"/api/presets/one", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept-Encoding", "gzip")
	// With its own Accept-Encoding header, the client does not decompress.
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if got := response.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := json.UnmarshalRead(reader, &config); err != nil || config.Name != "One" {
		t.Fatalf("project %q, error %v", config.Name, err)
	}
}

func TestPresetNamesTheProjectFile(t *testing.T) {
	t.Parallel()
	saver := WithProjectSaver(func(project.Config) error { return errors.New("the test does not apply a project") })
	fixture := newPresetFixture(t, map[string]PresetGenerator{"one": namedProject("One")}, saver)
	response, body := fixture.post(t, http.MethodPost, "one")
	if response.StatusCode != http.StatusOK || response.Header.Get(projectFileHeader) != "true" {
		t.Fatalf("status %d, %s = %q: %s", response.StatusCode, projectFileHeader, response.Header.Get(projectFileHeader), body)
	}
}

func TestPresetRefusals(t *testing.T) {
	t.Parallel()
	called := false
	fixture := newPresetFixture(t, map[string]PresetGenerator{"one": func() (project.Config, error) {
		called = true
		return project.Default(), nil
	}})
	foreign := func(request *http.Request) { request.Header.Set("Origin", "https://other.example") }
	tests := []struct {
		name, method, preset string
		change               []func(*http.Request)
		status               int
		allow                string
	}{
		{name: "unknown name", method: http.MethodPost, preset: "two", status: http.StatusNotFound},
		{name: "case of the name", method: http.MethodPost, preset: "ONE", status: http.StatusNotFound},
		{name: "other path", method: http.MethodPost, preset: "one/two", status: http.StatusNotFound},
		{name: "traversal", method: http.MethodPost, preset: "..%2fone", status: http.StatusNotFound},
		{name: "GET", method: http.MethodGet, preset: "one", status: http.StatusMethodNotAllowed, allow: "POST"},
		{name: "PUT", method: http.MethodPut, preset: "one", status: http.StatusMethodNotAllowed, allow: "POST"},
		{name: "other origin", method: http.MethodPost, preset: "one", change: []func(*http.Request){foreign}, status: http.StatusForbidden},
		{name: "other origin before the name", method: http.MethodPost, preset: "two", change: []func(*http.Request){foreign}, status: http.StatusForbidden},
	}
	for _, test := range tests {
		response, body := fixture.post(t, test.method, test.preset, test.change...)
		if response.StatusCode != test.status || response.Header.Get("Allow") != test.allow {
			t.Errorf("%s: status %d, Allow %q: %s", test.name, response.StatusCode, response.Header.Get("Allow"), body)
		}
		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", test.name, got)
		}
	}
	if called {
		t.Error("a refused request called the generator")
	}
}

func TestPresetWithoutGeneratorsHasNoEndpoint(t *testing.T) {
	t.Parallel()
	fixture := newPresetFixture(t, nil)
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		if response, body := fixture.post(t, method, "one"); response.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status %d: %s", method, response.StatusCode, body)
		}
	}
}

func TestPresetOneAtATime(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	finish := make(chan struct{})
	fixture := newPresetFixture(t, map[string]PresetGenerator{
		"slow":  func() (project.Config, error) { close(entered); <-finish; return project.Default(), nil },
		"other": namedProject("Other"),
	})
	first := make(chan int, 1)
	go func() { first <- fixture.status(t.Context(), "slow") }()
	<-entered
	response, body := fixture.post(t, http.MethodPost, "other")
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Retry-After") != presetRetryAfter {
		t.Errorf("second request: status %d, Retry-After %q: %s", response.StatusCode, response.Header.Get("Retry-After"), body)
	}
	// The refusal of an unknown name does not depend on the slot.
	if response, _ := fixture.post(t, http.MethodPost, "two"); response.StatusCode != http.StatusNotFound {
		t.Errorf("unknown name during a generation: status %d", response.StatusCode)
	}
	close(finish)
	if status := <-first; status != http.StatusOK {
		t.Errorf("first request: status %d", status)
	}
	// The first request gave back the slot.
	if response, body := fixture.post(t, http.MethodPost, "other"); response.StatusCode != http.StatusOK {
		t.Errorf("request after the generation: status %d: %s", response.StatusCode, body)
	}
}

// stallingResponse blocks its second body write until release closes. A gzip
// writer sends its header at the first write and the rest at its close.
type stallingResponse struct {
	http.ResponseWriter
	writes  atomic.Int32
	stalled chan struct{}
	release chan struct{}
}

func (w *stallingResponse) Write(body []byte) (int, error) {
	if w.writes.Add(1) == 2 {
		close(w.stalled)
		<-w.release
	}
	return w.ResponseWriter.Write(body)
}

// TestPresetSlotCoversTheCompressedReply checks that a second request gets the
// refusal while the end of the first compressed reply waits for the client.
func TestPresetSlotCoversTheCompressedReply(t *testing.T) {
	t.Parallel()
	fixture := newPresetFixture(t, map[string]PresetGenerator{"one": namedProject("One")})
	handler := compressResponse(http.HandlerFunc(fixture.session.presetHTTP))
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/presets/one", nil)
	request.SetPathValue("name", "one")
	request.Header.Set("Accept-Encoding", "gzip")
	writer := &stallingResponse{ResponseWriter: httptest.NewRecorder(), stalled: make(chan struct{}), release: make(chan struct{})}
	first := make(chan struct{})
	go func() {
		defer close(first)
		handler.ServeHTTP(writer, request)
	}()
	<-writer.stalled
	response, body := fixture.post(t, http.MethodPost, "one")
	close(writer.release)
	<-first
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("request during the reply: status %d: %s", response.StatusCode, body)
	}
}

// TestPresetReleasesMemory checks that each generation ends with a release,
// also a failed generation, and that a refusal does not cause one. The slot
// stays in use during the release, so a new generation cannot start before
// the memory is free.
func TestPresetReleasesMemory(t *testing.T) {
	t.Parallel()
	fixture := newPresetFixture(t, map[string]PresetGenerator{
		"one":   namedProject("One"),
		"error": func() (project.Config, error) { return project.Config{}, errors.New("failed") },
	})
	var mu sync.Mutex
	releases, slotInUse := 0, 0
	fixture.session.releaseMemory = func() {
		mu.Lock()
		defer mu.Unlock()
		releases++
		slotInUse += len(fixture.session.presetSlot)
	}
	for _, test := range []struct {
		name string
		want int
	}{{"one", 1}, {"error", 2}, {"two", 2}} {
		fixture.post(t, http.MethodPost, test.name)
		mu.Lock()
		got, inUse := releases, slotInUse
		mu.Unlock()
		if got != test.want || inUse != test.want {
			t.Errorf("after %s: %d releases, %d with the slot in use, want %d", test.name, got, inUse, test.want)
		}
	}
}

func TestPresetFailureSendsNoProject(t *testing.T) {
	t.Parallel()
	fixture := newPresetFixture(t, map[string]PresetGenerator{
		"error": func() (project.Config, error) { return project.Config{}, errors.New("secret detail") },
		"panic": func() (project.Config, error) { panic("secret detail") },
		"one":   namedProject("One"),
	})
	for _, name := range []string{"error", "panic"} {
		response, body := fixture.post(t, http.MethodPost, name)
		if response.StatusCode != http.StatusInternalServerError {
			t.Errorf("%s: status %d: %s", name, response.StatusCode, body)
		}
		if got := string(body); got != "the server could not generate the preset\n" {
			t.Errorf("%s: body %q", name, got)
		}
		if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
			t.Errorf("%s: Content-Type = %q", name, got)
		}
		if log := fixture.log.String(); !strings.Contains(log, "name="+name) || !strings.Contains(log, "secret detail") {
			t.Errorf("%s: the log does not have the failure:\n%s", name, log)
		}
	}
	// A failure gives back the slot and the guard.
	if response, body := fixture.post(t, http.MethodPost, "one"); response.StatusCode != http.StatusOK {
		t.Errorf("request after the failures: status %d: %s", response.StatusCode, body)
	}
	if len(fixture.session.largeCommands) != 0 || len(fixture.session.presetSlot) != 0 {
		t.Error("a failure kept the guard or the slot")
	}
}

// TestPresetWaitsForALargeCommand checks that a generation and a large apply
// do not run at the same time. A generation does not start while the guard is
// in use, and it holds the guard itself.
func TestPresetWaitsForALargeCommand(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, 1)
	guarded := make(chan int, 1)
	var fixture presetFixture
	fixture = newPresetFixture(t, map[string]PresetGenerator{"one": func() (project.Config, error) {
		guarded <- len(fixture.session.largeCommands)
		entered <- struct{}{}
		return project.Default(), nil
	}})
	fixture.session.largeCommands <- struct{}{}
	result := make(chan int, 1)
	go func() { result <- fixture.status(t.Context(), "one") }()
	select {
	case <-entered:
		t.Fatal("the generation did not wait for the guard")
	case <-time.After(100 * time.Millisecond):
	}
	<-fixture.session.largeCommands
	if status := <-result; status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if got := <-guarded; got != 1 {
		t.Fatalf("the generator ran with %d guard values", got)
	}
	if len(fixture.session.largeCommands) != 0 {
		t.Fatal("the generation kept the guard")
	}
}
