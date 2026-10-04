package session

import (
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
)

// editorLiveScript reads the live project and state with web/editor.js, the
// page code, from the server at the second argument. It unpauses the
// simulation, applies an invalid project so that the editor resumes the
// paused simulation, then applies the live project with a new name. Before
// that, it reads the state as the debug capture of web/index.html does,
// with web/shell.js at the third argument.
const editorLiveScript = `
const editor = require(process.argv[1]);
const base = process.argv[2];
const shell = require(process.argv[3]);
(async () => {
  const response = await fetch(base + "/api/state", { cache: "no-store", headers: { Accept: shell.STATE_ACCEPT } });
  if (!response.ok) throw new Error("capture HTTP " + response.status);
  const captured = shell.captureState(await response.json());
  const capture = { epoch: captured.epoch, revision: captured.projectRevision, tick: captured.simulation && captured.simulation.Tick };
  const connection = { fetch: (url, init) => fetch(base + url, init), clientID: "editor-live-test", sequence: 0, epoch: "" };
  const live = await editor.readLive(connection, async (project) => project);
  connection.epoch = live.epoch;
  await editor.postCommand(connection, { action: "pause", paused: false });
  let failed = null;
  try { await editor.applyToServer({ connection, project: { ...live.project, name: "" }, revision: live.revision, serverStart: live.serverStart }); }
  catch (error) { failed = { status: error.status, code: error.errorCode || "", pause: error.pause }; }
  const applied = await editor.applyToServer({ connection, project: { ...live.project, name: live.project.name + " applied" }, revision: live.revision, serverStart: live.serverStart });
  process.stdout.write(JSON.stringify({ capture, revision: live.revision, epoch: live.epoch, serverStart: live.serverStart, version: live.project.version, failed, applied: applied.revision }));
})().catch((error) => { process.stderr.write(String(error.stack || error)); process.exit(1); });
`

// The editor and the debug capture read the live state of a plain, an
// Express, and a coupling project from a real server. The last two reply
// only to their own media types, in an envelope.
func TestEditorReadsLiveStateOfEachProjectKind(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	editor, err := filepath.Abs("../../web/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	shell, err := filepath.Abs("../../web/shell.js")
	if err != nil {
		t.Fatal(err)
	}
	data := couplingPhaseFixtures(t)
	coupling := couplingProject(couplingPhaseInput(t, data, data.Frames[0]))
	for _, test := range []struct {
		name   string
		config project.Config
	}{{"plain", project.Default()}, {"express", expressConsumerProject(t)}, {"coupling", coupling}} {
		t.Run(test.name, func(t *testing.T) {
			s, err := NewWithProject(test.config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			server := httptest.NewServer(s.HandlerFS(nil))
			t.Cleanup(server.Close)
			topology := s.Topology()
			// A stalled node process fails the test and is killed. WaitDelay
			// also ends the wait when a child process keeps the output open.
			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, node, "-e", editorLiveScript, editor, server.URL, shell)
			command.WaitDelay = 5 * time.Second
			command.Env = append(os.Environ(), "NODE_NO_WARNINGS=1")
			var stderr strings.Builder
			command.Stderr = &stderr
			output, err := command.Output()
			if err != nil {
				t.Fatal(err, ctx.Err(), stderr.String())
			}
			var got struct {
				Capture struct {
					Epoch    string  `json:"epoch"`
					Revision uint64  `json:"revision"`
					Tick     *uint64 `json:"tick"`
				} `json:"capture"`
				Revision    uint64 `json:"revision"`
				Epoch       string `json:"epoch"`
				ServerStart string `json:"serverStart"`
				Version     int    `json:"version"`
				Failed      *struct {
					Status int    `json:"status"`
					Code   string `json:"code"`
					Pause  string `json:"pause"`
				} `json:"failed"`
				Applied uint64 `json:"applied"`
			}
			if err := json.Unmarshal(output, &got); err != nil {
				t.Fatal(err, string(output))
			}
			if got.Revision != topology.ProjectRevision || got.Epoch != topology.Epoch || got.ServerStart == "" || got.ServerStart != topology.ServerStart || got.Version != test.config.Version {
				t.Fatalf("live read %+v, topology revision %d epoch %q start %q", got, topology.ProjectRevision, topology.Epoch, topology.ServerStart)
			}
			if got.Capture.Epoch != topology.Epoch || got.Capture.Revision != topology.ProjectRevision || got.Capture.Tick == nil {
				t.Fatalf("debug capture read %+v", got.Capture)
			}
			// The server refused the project command after the editor paused
			// the simulation. The editor read the simulation ID again and
			// resumed the simulation.
			if got.Failed == nil || got.Failed.Status != http.StatusConflict || got.Failed.Code != "command_rejected" || got.Failed.Pause != "resumed" {
				t.Fatalf("failed apply %+v", got.Failed)
			}
			if got.Applied != topology.ProjectRevision+1 || s.Topology().ProjectRevision != got.Applied {
				t.Fatalf("applied revision %d, server %d", got.Applied, s.Topology().ProjectRevision)
			}
		})
	}
}
