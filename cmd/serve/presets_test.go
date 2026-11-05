package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
)

// postPreset sends a preset request and gives the status, the headers, and the
// body of the reply.
func postPreset(t *testing.T, client *http.Client, address, method, name string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, "http://"+address+"/api/presets/"+name, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, body
}

func TestPresetGeneratorsAreTheBundledPresets(t *testing.T) {
	t.Parallel()
	got := slices.Sorted(maps.Keys(presetGenerators()))
	if want := []string{"london-central", "london-full"}; !slices.Equal(got, want) {
		t.Fatalf("presets = %v, want %v", got, want)
	}
}

func TestRunServesPresets(t *testing.T) {
	t.Parallel()
	address, stop := startRun(t, []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t)})
	defer stop()
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()

	status, header, body := postPreset(t, client, address, http.MethodPost, "london-central")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %.200s", status, body)
	}
	if got := header.Get("X-Podsim-Project-File"); got != "" {
		t.Errorf("X-Podsim-Project-File = %q without -project", got)
	}
	var config project.Config
	if err := json.Unmarshal(body, &config, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	want, err := scenarios.GenerateLondonCentral()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalJSON(t, config), canonicalJSON(t, want)) {
		t.Error("the reply differs from the generated project")
	}
	// A generation does not change the session.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/api/project", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	var live struct {
		Revision int `json:"revision"`
	}
	doJSON(t, client, request, &live)
	if live.Revision != 1 {
		t.Errorf("the project revision is %d after a generation", live.Revision)
	}

	for _, name := range []string{"small", "busy", "scale100", "rail-hub", "compact-station", "London-Full", "london-full.json", "london-full%2f"} {
		refused, _, reply := postPreset(t, client, address, http.MethodPost, name)
		if refused != http.StatusNotFound {
			t.Errorf("%s: status = %d: %.200s", name, refused, reply)
		}
	}
	status, header, _ = postPreset(t, client, address, http.MethodGet, "london-central")
	if status != http.StatusMethodNotAllowed || header.Get("Allow") != http.MethodPost {
		t.Errorf("GET: status %d, Allow %q", status, header.Get("Allow"))
	}
}

// TestRunWarnsOfTheProjectFile applies a preset on a server with -project. The
// reply names the project file, and the apply overwrites that file.
func TestRunWarnsOfTheProjectFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "project.json")
	if err := saveProject(path, project.Default()); err != nil {
		t.Fatal(err)
	}
	address, stop := startRun(t, []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t), "-project", path})
	defer stop()
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()

	status, header, body := postPreset(t, client, address, http.MethodPost, "london-central")
	if status != http.StatusOK || header.Get("X-Podsim-Project-File") != "true" {
		t.Fatalf("status %d, X-Podsim-Project-File %q", status, header.Get("X-Podsim-Project-File"))
	}
	if loaded, err := loadProject(path); err != nil || loaded.Name != project.Default().Name {
		t.Fatalf("a generation changed the file: %q, %v", loaded.Name, err)
	}
	applyPresetBody(t, client, address, body)
	loaded, err := loadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name != "LondonCentral" {
		t.Fatalf("the file has project %q after the apply", loaded.Name)
	}
}

// TestRunAppliesIndentedLondonFull applies the largest preset with the JSON
// form of cmd/scenario, about 14.9 MB, with the request form of the editor.
func TestRunAppliesIndentedLondonFull(t *testing.T) {
	if testing.Short() {
		t.Skip("the test generates and applies the largest preset")
	}
	t.Parallel()
	if raceEnabled {
		t.Skip("the required embedded tests apply large projects without race instrumentation")
	}
	address, stop := startRun(t, []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t)})
	defer stop()
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()

	status, _, body := postPreset(t, client, address, http.MethodPost, "london-full")
	if status != http.StatusOK {
		t.Fatalf("status = %d: %.200s", status, body)
	}
	indented := jsontext.Value(bytes.Clone(body))
	if err := indented.Indent(jsontext.WithIndentPrefix(""), jsontext.WithIndent("  ")); err != nil {
		t.Fatal(err)
	}
	if len(indented) < 14<<20 || len(indented) > session.MaxCommandBytes {
		t.Fatalf("the indented project has %d bytes", len(indented))
	}
	t.Logf("compact %d bytes, indented %d bytes", len(body), len(indented))
	applyPresetBody(t, client, address, indented)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/api/project", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	var live struct {
		Project project.Config `json:"project"`
	}
	doJSON(t, client, request, &live)
	want, err := scenarios.GenerateLondonFull()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalJSON(t, live.Project), canonicalJSON(t, want)) {
		t.Fatal("the live project differs from the generated project")
	}
}

// editorPresetScript loads a preset with the page code of web/editor.js from
// the server at the second argument, and applies it as the editor does.
const editorPresetScript = `
const editor = require(process.argv[1]);
const base = process.argv[2];
(async () => {
  const connection = { fetch: (url, init) => fetch(base + url, init), clientID: "editor-preset-test", sequence: 0, epoch: "" };
  const live = await editor.readLive(connection, async (project) => project);
  connection.epoch = live.epoch;
  const preset = await editor.readPreset(connection, process.argv[3], new AbortController().signal);
  const project = editor.parseDocument(preset.text).scenario;
  const applied = await editor.applyToServer({ connection, project, revision: live.revision, serverStart: live.serverStart });
  process.stdout.write(JSON.stringify({ bytes: preset.text.length, projectFile: preset.projectFile, revision: applied.revision }));
})().catch((error) => { process.stderr.write(String(error.stack || error)); process.exit(1); });
`

// TestEditorAppliesLondonFull runs the requests of the editor against a real
// server: the preset menu, then Pause and apply with its gzip command.
func TestEditorAppliesLondonFull(t *testing.T) {
	if testing.Short() {
		t.Skip("the test generates and applies the largest preset")
	}
	t.Parallel()
	if raceEnabled {
		t.Skip("the required embedded tests apply large projects without race instrumentation")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	editor, err := filepath.Abs("../../web/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	address, stop := startRun(t, []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t)})
	defer stop()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, node, "-e", editorPresetScript, editor, "http://"+address, "london-full")
	command.WaitDelay = 5 * time.Second
	command.Env = append(os.Environ(), "NODE_NO_WARNINGS=1")
	var stderr strings.Builder
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatal(err, stderr.String())
	}
	var got struct {
		Bytes       int  `json:"bytes"`
		ProjectFile bool `json:"projectFile"`
		Revision    int  `json:"revision"`
	}
	if unmarshalErr := json.Unmarshal(output, &got); unmarshalErr != nil {
		t.Fatal(unmarshalErr, string(output))
	}
	if got.Bytes < 5<<20 || got.ProjectFile || got.Revision != 2 {
		t.Fatalf("the editor run gave %+v", got)
	}
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/api/project", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	var live struct {
		Project project.Config `json:"project"`
	}
	doJSON(t, client, request, &live)
	want, err := scenarios.GenerateLondonFull()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonicalJSON(t, live.Project), canonicalJSON(t, want)) {
		t.Fatal("the live project differs from the generated project")
	}
}

// TestEditorMenuHasThePresets checks the menu of web/editor.html against the
// presets of the server.
func TestEditorMenuHasThePresets(t *testing.T) {
	t.Parallel()
	html, err := os.ReadFile("../../web/editor.html")
	if err != nil {
		t.Fatal(err)
	}
	menu := regexp.MustCompile(`(?s)<select id="presetSelect".*?</select>`).Find(html)
	if menu == nil {
		t.Fatal("web/editor.html has no preset menu")
	}
	var got []string
	for _, match := range regexp.MustCompile(`<option value="([^"]+)"`).FindAllSubmatch(menu, -1) {
		got = append(got, string(match[1]))
	}
	slices.Sort(got)
	if want := slices.Sorted(maps.Keys(presetGenerators())); !slices.Equal(got, want) {
		t.Fatalf("the menu has %v, the server has %v", got, want)
	}
}

// applyPresetBody pauses the session and applies the project JSON in a gzip
// command, as the editor does.
func applyPresetBody(t *testing.T, client *http.Client, address string, projectJSON []byte) {
	t.Helper()
	epoch := getFrame(t, client, address).Epoch
	if reply := postCommand(t, client, address, session.Command{Client: "preset-test", Sequence: 1, Epoch: epoch, Action: "pause", Paused: true}); reply.Error != "" {
		t.Fatalf("pause reply = %+v", reply)
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := fmt.Fprintf(writer, `{"client":"preset-test","sequence":2,"epoch":%q,"action":"project","projectRevision":1,"project":`, epoch); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(projectJSON); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("}")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+address+"/api/command", &compressed)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var reply session.Reply
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &reply) != nil || reply.Error != "" || reply.ProjectRevision != 2 {
		t.Fatalf("project reply = %d %.500s", response.StatusCode, body)
	}
}
