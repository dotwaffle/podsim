package session

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// incidentGeneration reads the generation that the session gave its
// simulation. The field is not exported, and stage 1 makes no record that
// would show it.
func incidentGeneration(s *Session) uint64 {
	return reflect.ValueOf(s.simulation).Elem().FieldByName("incidentGeneration").Uint()
}

// TestIncidentGenerationHook checks that the session gives its simulation
// the session generation after each change of the generation: the start,
// a reset, the demo, a project apply, a rewind and a restart.
func TestIncidentGenerationHook(t *testing.T) {
	t.Parallel()
	config := markedProject()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	client := newTestClient(s, "incident")
	check := func(stage string, s *Session) {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if got := incidentGeneration(s); got != s.generation || got == 0 {
			t.Fatalf("%s: simulation generation %d, session generation %d", stage, got, s.generation)
		}
	}
	check("start", s)
	checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	client.mustApply(t, Command{Action: "reset"})
	check("reset", s)
	client.mustApply(t, Command{Action: "demo"})
	check("demo", s)
	client.mustApply(t, Command{Action: "pause", Paused: true})
	next := markedProject()
	next.Name = "Incident project apply"
	client.mustApply(t, Command{Action: "project", Project: &next, ProjectRevision: s.Project().Revision})
	check("project", s)
	client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
	check("rewind", s)
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	restored := startFromStore(t, StoreInput{Store: store})
	t.Cleanup(restored.Close)
	check("restart", restored)
}

// TestIncidentSerialSaveMember checks the saved member
// /simulation/incidentSerial. A marked state keeps the serial through a
// restore, also an explicit 0. Without the marker, the decoder refuses the
// member, also as 0 or null.
func TestIncidentSerialSaveMember(t *testing.T) {
	t.Parallel()
	encode := func(config project.Config, serial uint64) []byte {
		t.Helper()
		s, err := NewWithProject(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		file := sessionStateFile(t, s)
		file.Simulation.IncidentSerial = serial
		return decompressTestJSON(t, encodeTestState(t, file))
	}
	restore := func(raw []byte) (*sim.Simulation, error) {
		t.Helper()
		s := newTestSession(t)
		t.Cleanup(s.Close)
		loaded, err := s.loadState(loadInput{data: compressTestJSON(t, raw), steps: realRestoreSteps()})
		return loaded.simulation, err
	}
	marked := encode(markedProject(), 7)
	if !bytes.Contains(marked, []byte(`"incidentSerial":7`)) {
		t.Fatal("marked save lost the serial")
	}
	simulation, err := restore(marked)
	if err != nil || simulation.ExportState().IncidentSerial != 7 {
		t.Fatalf("marked restore: %v", err)
	}
	unmarked := encode(project.Default(), 0)
	if bytes.Contains(unmarked, []byte("incidentSerial")) {
		t.Fatal("unmarked save has an incident serial")
	}
	explicit := func(raw []byte, value string) []byte {
		t.Helper()
		edited := bytes.Replace(raw, []byte(`"simulation":{`), []byte(`"simulation":{"incidentSerial":`+value+`,`), 1)
		if bytes.Equal(edited, raw) {
			t.Fatal("fixture did not add the serial")
		}
		return edited
	}
	if _, err := restore(explicit(encode(markedProject(), 0), "0")); err != nil {
		t.Fatal("marked save refused an explicit zero serial", err)
	}
	if _, err := restore(unmarked); err != nil {
		t.Fatal("control refused", err)
	}
	for _, value := range []string{"0", "null", "3"} {
		_, err := restore(explicit(unmarked, value))
		if !errors.Is(err, errIncidentMemberUnmarked) {
			t.Errorf("unmarked save with serial %s: error %v, want %v", value, err, errIncidentMemberUnmarked)
		}
	}
}
