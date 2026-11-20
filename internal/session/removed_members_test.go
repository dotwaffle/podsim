package session

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
	"testing"

	"github.com/dotwaffle/podsim/internal/jsonerr"
	"github.com/dotwaffle/podsim/internal/project"
)

// removedMemberCase is a network that has removed members: inserted is the
// text after the opening brace of the network object, and first is the
// member that a refusal names.
type removedMemberCase struct {
	inserted, first string
}

// removedMemberCases returns each removed network member with the values
// true, false, and null, and both members in each order.
func removedMemberCases() []removedMemberCase {
	var cases []removedMemberCase
	for _, member := range []string{"lateralMerges", "shapeClearance"} {
		for _, value := range []string{"true", "false", "null"} {
			cases = append(cases, removedMemberCase{inserted: `"` + member + `":` + value + `,`, first: member})
		}
	}
	return append(cases,
		removedMemberCase{inserted: `"lateralMerges":true,"shapeClearance":true,`, first: "lateralMerges"},
		removedMemberCase{inserted: `"shapeClearance":false,"lateralMerges":null,`, first: "shapeClearance"})
}

// insertNetworkMembers inserts the members of c at the start of the first
// network object of data.
func insertNetworkMembers(t *testing.T, data []byte, c removedMemberCase) []byte {
	t.Helper()
	const network = `"network":{`
	if !bytes.Contains(data, []byte(network)) {
		t.Fatal("the JSON has no network object")
	}
	return bytes.Replace(data, []byte(network), []byte(network+c.inserted), 1)
}

// TestProjectCommandRefusesRemovedMembers pins the reply to a project
// command whose network has a removed member: HTTP 400 with the reply of a
// command that does not decode. The session does not change.
func TestProjectCommandRefusesRemovedMembers(t *testing.T) {
	t.Parallel()
	for _, c := range removedMemberCases() {
		t.Run(c.inserted, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t)
			handler := s.Handler(t.TempDir())
			pause, err := jsonv2.Marshal(Command{Client: "pause", Sequence: 1, Epoch: s.State().Epoch, Action: "pause", Paused: true}, json.DefaultOptionsV1())
			if err != nil {
				t.Fatal(err)
			}
			if recorder := postCommand(t, handler, pause); recorder.Code != http.StatusOK {
				t.Fatalf("pause: status %d, reply %q", recorder.Code, recorder.Body.String())
			}
			config := project.Default()
			config.Name = "Removed members"
			before := s.State()
			command, err := jsonv2.Marshal(Command{Client: "project", Sequence: 1, Epoch: before.Epoch, Action: "project", Project: &config, ProjectRevision: before.ProjectRevision}, json.DefaultOptionsV1())
			if err != nil {
				t.Fatal(err)
			}
			recorder := postCommand(t, handler, insertNetworkMembers(t, command, c))
			if recorder.Code != http.StatusBadRequest || recorder.Body.String() != "invalid command JSON\n" {
				t.Fatalf("status %d, reply %q", recorder.Code, recorder.Body.String())
			}
			if after := s.State(); after.ProjectRevision != before.ProjectRevision || s.Project().Project.Name == config.Name {
				t.Fatalf("the session changed: revision %d, name %q", after.ProjectRevision, s.Project().Project.Name)
			}
			// The control: the same command without the members.
			if recorder := postCommand(t, handler, command); recorder.Code != http.StatusOK {
				t.Fatalf("control: status %d, reply %q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestRestoreRefusesRemovedMembers pins the restore of a saved state whose
// project network has a removed member: the file is invalid_state with the
// text of the network decode. The test compares the fixed wording of
// jsonerr, which the log line of a refused file uses.
func TestRestoreRefusesRemovedMembers(t *testing.T) {
	t.Parallel()
	s, err := NewWithProject(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	reader, err := gzip.NewReader(bytes.NewReader(encodeTestState(t, sessionStateFile(t, s))))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	// The project is the first member with a network object.
	if bytes.Index(plain, []byte(`"project":`)) > bytes.Index(plain, []byte(`"network":{`)) {
		t.Fatal("the first network object is not the network of the project")
	}
	for _, c := range removedMemberCases() {
		t.Run(c.inserted, func(t *testing.T) {
			t.Parallel()
			data := compressTestJSON(t, insertNetworkMembers(t, plain, c))
			want := `invalid_state: decode session state: json: invalid JSON object at /network/` + c.first + `: unknown object member name "` + c.first + `"`
			_, err := decodeStateFile(data)
			if err == nil || stateReason(err) != reasonInvalidState || jsonerr.Text(err) != want {
				t.Fatalf("decode error = %s (%s), want %s", jsonerr.Text(err), stateReason(err), want)
			}
			restored := newTestSession(t)
			if _, err := restored.loadState(loadInput{data: data, steps: realRestoreSteps()}); err == nil || jsonerr.Text(err) != want {
				t.Fatalf("restore error = %v, want %s", err, want)
			}
		})
	}
	if _, err := decodeStateFile(compressTestJSON(t, plain)); err != nil {
		t.Fatalf("control: %v", err)
	}
}

// TestTopologyRefusesRemovedMembers pins the topology decode of a network
// with a removed member. The stream, the HTTP state, and the Go client use
// this decode.
func TestTopologyRefusesRemovedMembers(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	data, err := jsonv2.Marshal(s.Topology(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var control TopologySnapshot
	if err := control.UnmarshalJSON(data); err != nil {
		t.Fatalf("control: %v", err)
	}
	for _, c := range removedMemberCases() {
		var topology TopologySnapshot
		want := `json: unknown field "` + c.first + `"`
		if err := topology.UnmarshalJSON(insertNetworkMembers(t, data, c)); err == nil || err.Error() != want {
			t.Errorf("%s: topology error = %v, want %s", c.inserted, err, want)
		}
	}
}
