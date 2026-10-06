package session

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// faultSessionProject returns the example project with the incident
// marker, the fault marker, and faults.
func faultSessionProject(faults project.FaultConfig) project.Config {
	config := project.Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.FaultContract = project.FaultV1Contract
	config.Faults = &faults
	return config
}

// newFaultClient returns a paused session of config and its client.
func newFaultClient(t *testing.T, config project.Config) *testClient {
	t.Helper()
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	client := newTestClient(s, "faults")
	client.mustApply(t, Command{Action: "pause", Paused: true})
	return client
}

// debrisCommand is a fault command for debris on a free segment of the
// lane bypass-in of the example network.
func debrisCommand() Command {
	return Command{Action: "fault", LaneID: "bypass-in", FromMeters: new(80.0), ToMeters: new(82.0)}
}

// mustReject applies command and checks that the session rejects it with
// command_rejected and the message, and that the exported state and the
// revision do not change.
func (c *testClient) mustReject(t *testing.T, command Command, message string) {
	t.Helper()
	c.session.mu.Lock()
	before := c.session.simulation.ExportState()
	c.session.mu.Unlock()
	revision := c.session.Frame().Revision
	reply := c.session.Apply(c.next(command))
	if reply.ErrorCode != CommandRejected || reply.Error != message || reply.FaultID != "" || reply.EmergencyID != "" {
		t.Fatalf("%s: reply %q %q %q, want %s %q", command.Action, reply.ErrorCode, reply.Error, reply.FaultID, CommandRejected, message)
	}
	c.session.mu.Lock()
	after := c.session.simulation.ExportState()
	c.session.mu.Unlock()
	if !reflect.DeepEqual(before, after) || c.session.Frame().Revision != revision {
		t.Fatalf("%s: a rejected command changed the session", command.Action)
	}
}

// TestFaultCommands checks the fault and clearFault commands in a paused
// session. A fault command replies with the fault ID. An exact retry gets
// the stored reply and starts no second fault. A clear ends the fault, and
// a second clear of the ID is rejected.
func TestFaultCommands(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, faultSessionProject(project.FaultConfig{}))
	pod := client.next(Command{Action: "fault", PodID: "01", DurationSeconds: new(int64(600))})
	reply := client.session.Apply(pod)
	if reply.Error != "" || reply.FaultID != "i1.1" {
		t.Fatalf("pod fault reply %+v, want fault i1.1", reply)
	}
	if retry := client.session.Apply(pod); !reflect.DeepEqual(retry, reply) {
		t.Fatalf("retry reply %+v, want %+v", retry, reply)
	}
	debris := client.mustApply(t, debrisCommand())
	if debris.FaultID != "i1.2" || debris.Revision != reply.Revision+1 {
		t.Fatalf("debris reply %+v, want fault i1.2 after one revision", debris)
	}
	for _, id := range []string{reply.FaultID, debris.FaultID} {
		cleared := client.mustApply(t, Command{Action: "clearFault", FaultID: id})
		if cleared.FaultID != "" {
			t.Fatalf("clear reply has the fault ID %s", cleared.FaultID)
		}
		client.mustReject(t, Command{Action: "clearFault", FaultID: id}, "unknown fault")
	}
	// The pod is free again, so a new fault starts with the next serial.
	if again := client.mustApply(t, Command{Action: "fault", PodID: "01"}); again.FaultID != "i1.3" {
		t.Fatalf("fault after the clear has the ID %s, want i1.3", again.FaultID)
	}
}

// TestFaultCommandErrors checks the message of each command error of the
// incident suspension contract that a session command can reach. Each
// rejection is command_rejected and changes nothing. The simulation tests
// reach the limits and the reserved resources.
func TestFaultCommandErrors(t *testing.T) {
	t.Parallel()
	unmarked := newFaultClient(t, project.Default())
	unmarked.mustReject(t, Command{Action: "fault", PodID: "01"}, "faults are not enabled")
	unmarked.mustReject(t, debrisCommand(), "faults are not enabled")
	unmarked.mustReject(t, Command{Action: "clearFault", FaultID: "i1.1"}, "faults are not enabled")

	client := newFaultClient(t, faultSessionProject(project.FaultConfig{}))
	client.mustApply(t, Command{Action: "fault", PodID: "02"})
	client.mustApply(t, debrisCommand())
	for _, test := range []struct {
		command Command
		message string
	}{
		{Command{Action: "fault"}, "fault target is not supported"},
		{Command{Action: "fault", PodID: "01", LaneID: "bypass-in", FromMeters: new(80.0), ToMeters: new(82.0)}, "fault target is not supported"},
		{Command{Action: "fault", PodID: "01", FromMeters: new(0.0)}, "fault target is not supported"},
		{Command{Action: "fault", PodID: "01", ToMeters: new(1.0)}, "fault target is not supported"},
		{Command{Action: "fault", PodID: "99"}, "unknown pod"},
		{Command{Action: "fault", PodID: "02"}, "pod already has a fault"},
		{Command{Action: "fault", LaneID: "nowhere", FromMeters: new(0.0), ToMeters: new(1.0)}, "unknown lane"},
		{Command{Action: "fault", LaneID: "bypass-in", ToMeters: new(2.0)}, "invalid debris segment"},
		{Command{Action: "fault", LaneID: "bypass-in", FromMeters: new(2.0), ToMeters: new(2.0)}, "invalid debris segment"},
		{Command{Action: "fault", LaneID: "bypass-in", FromMeters: new(0.0), ToMeters: new(51.0)}, "invalid debris segment"},
		{Command{Action: "fault", LaneID: "bypass-in", FromMeters: new(84.0), ToMeters: new(85.0)}, "debris overlaps a pod or another fault"},
		{Command{Action: "fault", PodID: "01", DurationSeconds: new(int64(0))}, "invalid fault duration"},
		{Command{Action: "fault", PodID: "01", DurationSeconds: new(int64(86_401))}, "invalid fault duration"},
		{Command{Action: "clearFault"}, "unknown fault"},
		{Command{Action: "clearFault", FaultID: "i9.1"}, "unknown fault"},
	} {
		client.mustReject(t, test.command, test.message)
	}
	// The limits of the duration are accepted.
	for _, duration := range []int64{1, 86_400} {
		reply := client.mustApply(t, Command{Action: "fault", PodID: "01", DurationSeconds: new(duration)})
		client.mustApply(t, Command{Action: "clearFault", FaultID: reply.FaultID})
	}
}

// TestFaultSessionEvents checks the faults across the session events of
// the incident suspension contract. A reset, a rewind, and the apply of a
// changed project end every fault and keep faults on. The apply of the
// same project keeps the faults. The apply of a project without the
// marker turns faults off. A demo ends every fault, also a fault on a demo
// route, and turns faults off until a reset.
func TestFaultSessionEvents(t *testing.T) {
	t.Parallel()
	config := faultSessionProject(project.FaultConfig{})
	changed := faultSessionProject(project.FaultConfig{EvacuationSeconds: new(0)})
	unmarked := project.Default()
	pod := Command{Action: "fault", PodID: "01"}
	tests := []struct {
		name  string
		fault Command
		// event runs the event after the fault command. It returns false
		// when the fault stays.
		event func(t *testing.T, client *testClient) bool
	}{
		{"reset", pod, func(t *testing.T, client *testClient) bool {
			t.Helper()
			client.mustApply(t, Command{Action: "reset"})
			return true
		}},
		{"same project", pod, func(t *testing.T, client *testClient) bool {
			t.Helper()
			same := project.Clone(config)
			client.mustApply(t, Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &same})
			return false
		}},
		{"changed faults", pod, func(t *testing.T, client *testClient) bool {
			t.Helper()
			client.mustApply(t, Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &changed})
			return true
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newFaultClient(t, config)
			id := client.mustApply(t, test.fault).FaultID
			if test.event(t, client) {
				client.mustReject(t, Command{Action: "clearFault", FaultID: id}, "unknown fault")
				client.mustApply(t, test.fault)
				return
			}
			client.mustReject(t, test.fault, "pod already has a fault")
			client.mustApply(t, Command{Action: "clearFault", FaultID: id})
		})
	}
	t.Run("rewind", func(t *testing.T) {
		t.Parallel()
		client := newFaultClient(t, config)
		checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
		id := client.mustApply(t, Command{Action: "fault", PodID: "01"}).FaultID
		client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
		client.mustReject(t, Command{Action: "clearFault", FaultID: id}, "unknown fault")
		if again := client.mustApply(t, Command{Action: "fault", PodID: "01"}).FaultID; again == id {
			t.Fatalf("the rewound session reused the fault ID %s", id)
		}
	})
	t.Run("demo", func(t *testing.T) {
		t.Parallel()
		client := newFaultClient(t, config)
		ids := []string{client.mustApply(t, debrisCommand()).FaultID, client.mustApply(t, pod).FaultID}
		client.mustApply(t, Command{Action: "demo"})
		client.mustApply(t, Command{Action: "pause", Paused: true})
		for _, id := range ids {
			client.mustReject(t, Command{Action: "clearFault", FaultID: id}, "faults are not enabled")
		}
		client.mustReject(t, debrisCommand(), "faults are not enabled")
		client.mustReject(t, pod, "faults are not enabled")
		client.mustApply(t, Command{Action: "reset"})
		client.mustApply(t, debrisCommand())
		client.mustApply(t, pod)
	})
	t.Run("unmarked project", func(t *testing.T) {
		t.Parallel()
		client := newFaultClient(t, config)
		client.mustApply(t, Command{Action: "fault", PodID: "01"})
		client.mustApply(t, Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &unmarked})
		client.mustReject(t, Command{Action: "fault", PodID: "01"}, "faults are not enabled")
	})
}

// TestFaultRestoreKeepsFaultsOn checks that a session restored from a save
// of a project with the fault marker has faults on, also when the save has
// an active fault.
func TestFaultRestoreKeepsFaultsOn(t *testing.T) {
	t.Parallel()
	for _, active := range []bool{false, true} {
		config := faultSessionProject(project.FaultConfig{})
		store := &fakeStore{}
		s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
		if err != nil {
			t.Fatal(err)
		}
		client := newTestClient(s, "faults")
		client.mustApply(t, Command{Action: "pause", Paused: true})
		if active {
			client.mustApply(t, Command{Action: "fault", PodID: "01"})
		}
		if err = s.SaveState(t.Context(), SavePeriodic); err != nil {
			t.Fatal(err)
		}
		s.Close()
		writes := store.writeList()
		restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: writes[len(writes)-1]}})
		if err != nil || restored.restore.Tier != "physical" {
			t.Fatalf("active fault %t: restore %v, %+v", active, err, restored.restore)
		}
		next := newTestClient(restored, "faults-restored")
		next.mustApply(t, Command{Action: "fault", PodID: "02"})
		restored.Close()
	}
}

// TestFaultRestoreDemoKeepsFaultsOff checks that a session restored from a
// save of the traffic demo has faults off, as the demo command leaves
// them. The parked demo pods stay after the demo ends, so the restore of
// an ended demo also has faults off. The logical tier stops the demo and
// removes those pods, so faults are on again.
func TestFaultRestoreDemoKeepsFaultsOff(t *testing.T) {
	t.Parallel()
	config := faultSessionProject(project.FaultConfig{})
	store := &fakeStore{}
	s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(s, "faults")
	client.mustApply(t, Command{Action: "pause", Paused: true})
	client.mustApply(t, Command{Action: "demo"})
	client.mustApply(t, Command{Action: "pause", Paused: true})
	if err = s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatal(err)
	}
	s.Close()
	writes := store.writeList()
	run := storedRun{data: writes[len(writes)-1]}
	tests := []struct {
		name string
		data []byte
		tier string
		// on tells whether the restored session has faults on.
		on bool
	}{
		{"running demo", run.data, "physical", false},
		{"ended demo", run.edited(t, func(file *stateFile) { file.Simulation.Demo = nil }), "physical", false},
		{"logical tier", run.edited(t, logicalOnly), "logical", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: test.data}})
			if err != nil || restored.restore.Tier != test.tier {
				t.Fatalf("restore %v, %+v", err, restored.restore)
			}
			t.Cleanup(restored.Close)
			next := newTestClient(restored, "faults-restored")
			next.mustApply(t, Command{Action: "pause", Paused: true})
			if test.on {
				next.mustApply(t, Command{Action: "fault", PodID: "01"})
				return
			}
			next.mustReject(t, Command{Action: "fault", PodID: "01"}, "faults are not enabled")
		})
	}
}

// TestFaultCommandsOverHTTP checks the member names of the fault commands
// and that the JSON decode keeps the presence of a value of 0: a debris
// start at 0 m starts, and a duration of 0 seconds is rejected. A
// duration that is not an integer is invalid command JSON. The reply has
// the fault ID.
func TestFaultCommandsOverHTTP(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, faultSessionProject(project.FaultConfig{}))
	handler := client.session.Handler(t.TempDir())
	post := func(members string) (int, Reply) {
		t.Helper()
		client.sequence++
		body := fmt.Sprintf(`{"client":%q,"sequence":%d,"epoch":%q,%s}`, client.name, client.sequence, client.epoch, members)
		recorder := postCommand(t, handler, []byte(body))
		var reply Reply
		_ = json.Unmarshal(recorder.Body.Bytes(), &reply)
		return recorder.Code, reply
	}
	if code, reply := post(`"action":"fault","laneID":"bypass-in","fromMeters":0,"toMeters":2,"durationSeconds":60`); code != http.StatusOK || reply.FaultID != "i1.1" {
		t.Fatalf("debris at 0 m: %d %+v", code, reply)
	}
	if code, reply := post(`"action":"fault","podID":"01","durationSeconds":0`); code != http.StatusConflict || reply.Error != "invalid fault duration" {
		t.Fatalf("duration 0: %d %+v", code, reply)
	}
	if code, _ := post(`"action":"fault","podID":"01","durationSeconds":1.5`); code != http.StatusBadRequest {
		t.Fatalf("duration 1.5: %d", code)
	}
	if code, reply := post(`"action":"clearFault","faultID":"i1.1"`); code != http.StatusOK || reply.Error != "" {
		t.Fatalf("clear: %d %+v", code, reply)
	}
}
