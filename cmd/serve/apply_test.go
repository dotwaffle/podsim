package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json/v2"
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
)

// TestRunAppliesLargestProjectWithGzip applies a project at the size limit
// of project.Validate through the server. The command is larger than
// session.MaxCommandBytes, so it must come in a gzip body. The test logs
// the time and the memory of the request.
func TestRunAppliesLargestProjectWithGzip(t *testing.T) {
	if testing.Short() {
		t.Skip("the test starts the server and applies a project at the size limit")
	}
	t.Parallel()
	if raceEnabled {
		t.Skip("the required embedded tests apply the maximum project without race instrumentation")
	}
	testRunAppliesProjectWithGzip(t, limitProject(t, widestDemand()), 0)
}

func TestRunAppliesProjectWithGzip(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Name = "Gzip project application"
	testRunAppliesProjectWithGzip(t, config, session.MaxCommandBytes+1)
}

func testRunAppliesProjectWithGzip(t *testing.T, config project.Config, minimumCommandBytes int) {
	t.Helper()
	address, stop := startRun(t, []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t)})
	defer stop()
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()

	epoch := getFrame(t, client, address).Epoch
	if reply := postCommand(t, client, address, session.Command{Client: "apply-test", Sequence: 1, Epoch: epoch, Action: "pause", Paused: true}); reply.Error != "" {
		t.Fatalf("pause reply = %+v", reply)
	}
	command, err := json.Marshal(session.Command{
		Client: "apply-test", Sequence: 2, Epoch: epoch, Action: "project", Project: &config, ProjectRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Padding crosses the wire limit without a maximum project workload.
	command = append(command, bytes.Repeat([]byte(" "), max(0, minimumCommandBytes-len(command)))...)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err = writer.Write(command); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if len(command) <= session.MaxCommandBytes || len(command) > session.MaxInflatedCommandBytes || compressed.Len() > session.MaxCommandBytes {
		t.Fatalf("the command has %d bytes and %d compressed bytes", len(command), compressed.Len())
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+address+"/api/command", bytes.NewReader(compressed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	started := time.Now()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	elapsed := time.Since(started)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d command bytes, %d compressed bytes, %v, allocated %d bytes", len(command), compressed.Len(), elapsed, after.TotalAlloc-before.TotalAlloc)
	var reply session.Reply
	if response.StatusCode != http.StatusOK || json.Unmarshal(body, &reply) != nil || reply.Error != "" || reply.ProjectRevision != 2 {
		t.Fatalf("project reply = %d %s", response.StatusCode, body)
	}

	request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/api/project", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	var live struct {
		Project project.Config `json:"project"`
	}
	doJSON(t, client, request, &live)
	if !bytes.Equal(canonicalJSON(t, live.Project), canonicalJSON(t, config)) {
		t.Fatal("the live project is different from the applied project")
	}
}
