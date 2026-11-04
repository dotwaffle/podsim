package session

import (
	"bytes"
	"reflect"
	"strings"
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
	config := project.Default()
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
	next := project.Default()
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
// /simulation/incidentSerial. A state keeps the serial through a restore,
// also an explicit 0. A serial of 0 has no member, and the decoder refuses
// null.
func TestIncidentSerialSaveMember(t *testing.T) {
	t.Parallel()
	encode := func(serial uint64) []byte {
		t.Helper()
		s, err := NewWithProject(project.Default())
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
	withSerial := encode(7)
	if !bytes.Contains(withSerial, []byte(`"incidentSerial":7`)) {
		t.Fatal("the save lost the serial")
	}
	simulation, err := restore(withSerial)
	if err != nil || simulation.ExportState().IncidentSerial != 7 {
		t.Fatalf("restore: %v", err)
	}
	zero := encode(0)
	if bytes.Contains(zero, []byte("incidentSerial")) {
		t.Fatal("the save has a zero incident serial")
	}
	explicit := func(raw []byte, value string) []byte {
		t.Helper()
		edited := bytes.Replace(raw, []byte(`"simulation":{`), []byte(`"simulation":{"incidentSerial":`+value+`,`), 1)
		if bytes.Equal(edited, raw) {
			t.Fatal("fixture did not add the serial")
		}
		return edited
	}
	for value, serial := range map[string]uint64{"0": 0, "3": 3} {
		restored, restoreErr := restore(explicit(zero, value))
		if restoreErr != nil || restored.ExportState().IncidentSerial != serial {
			t.Fatalf("explicit serial %s: %v", value, restoreErr)
		}
	}
	if _, err = restore(zero); err != nil {
		t.Fatal("control refused", err)
	}
	_, err = restore(explicit(zero, "null"))
	const want = "saved incident member /simulation/incidentSerial is null"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("null serial: error %v, want %q", err, want)
	}
}
