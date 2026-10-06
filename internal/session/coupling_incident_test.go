package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingIncidentPhase is the first occupied fixture phase. Its committed
// train closes at tick 491, and it splits at the end of tick 7296.
const couplingIncidentPhase = "occupied-true-phase-1-leg-0.json"

// couplingIncidentFile returns the save of the occupied fixture phase with
// the incident marker. With emergencies, the project also has the fault
// marker, the emergency marker, and an evacuation delay of one hour.
func couplingIncidentFile(t *testing.T, emergencies bool) stateFile {
	t.Helper()
	data := couplingPhaseFixtures(t)
	index := slices.IndexFunc(data.Frames, func(frame couplingPhaseFrame) bool { return frame.Name == couplingIncidentPhase })
	if index < 0 {
		t.Fatal("unknown fixture phase", couplingIncidentPhase)
	}
	input := couplingPhaseInput(t, data, data.Frames[index])
	config := couplingProject(input)
	config.IncidentContract = sim.IncidentV1Contract
	if emergencies {
		config.FaultContract = project.FaultV1Contract
		config.Faults = &project.FaultConfig{EvacuationSeconds: new(3600)}
		config.EmergencyContract = project.EmergencyV1Contract
		config.Emergencies = &project.EmergencyConfig{}
	}
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	file := sessionStateFile(t, s)
	file.CouplingContract, file.RestoreAttempts = input.CouplingContract, 0
	file.Simulation = input.State
	return file
}

// couplingIncidentClient restores the save of couplingIncidentFile with
// each marker in the physical tier, and returns a client of the paused
// session.
func couplingIncidentClient(t *testing.T) *testClient {
	t.Helper()
	s, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: encodeTestState(t, couplingIncidentFile(t, true))}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if s.restore.Tier != "physical" {
		t.Fatalf("restore %+v", s.restore)
	}
	client := newTestClient(s, "incident")
	client.mustApply(t, Command{Action: "pause", Paused: true})
	return client
}

// Fault commands on the members of the fixture train and on a lane that no
// pod uses. The segment of memberDebris is on the remaining route of both
// members, past their grants and claims. The segment of freeDebris is on
// a return lane that no route uses.
var (
	memberDebris = Command{Action: "fault", LaneID: "rear-road", FromMeters: new(100.0), ToMeters: new(102.0)}
	freeDebris   = Command{Action: "fault", LaneID: "rear-return", FromMeters: new(10.0), ToMeters: new(12.0)}
)

// couplingIncidentState returns the exported state of the session.
func couplingIncidentState(s *Session) sim.SavedState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.simulation.ExportState()
}

// couplingIncidentRun is one run of TestCouplingIncidentSession. It
// returns the replies of the commands, the split tick, and the SHA-256 of
// the saved state at the end of the run.
type couplingIncidentRun struct {
	replies []string
	split   int64
	save    [32]byte
}

// runCouplingIncident starts the fixture train with each incident marker
// and sends the commands of section 1.4 of the incident suspension
// contract and section 5.6 of the incident emergency contract to its
// members. It checks the state contract after each command and each tick.
func runCouplingIncident(t *testing.T) couplingIncidentRun {
	t.Helper()
	client := couplingIncidentClient(t)
	s := client.session
	var run couplingIncidentRun
	record := func(reply Reply) {
		// The epoch is random for each session, so the record leaves it
		// out.
		run.replies = append(run.replies, fmt.Sprintf("%d %d %d %s %q %s %s", reply.Revision, reply.ProjectRevision, reply.Generation, reply.ErrorCode, reply.Error, reply.FaultID, reply.EmergencyID))
	}
	reject := func(command Command) {
		t.Helper()
		client.mustReject(t, command, "fault target is not supported")
		run.replies = append(run.replies, command.Action+" "+command.PodID+command.LaneID+" refused")
	}
	apply := func(command Command) Reply {
		t.Helper()
		reply := client.mustApply(t, command)
		record(reply)
		return reply
	}
	check := func() sim.SavedState {
		t.Helper()
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := s.simulation.CheckContract(); err != nil {
			t.Fatalf("tick %d: %v", s.simulation.Tick(), err)
		}
		return s.simulation.ExportState()
	}
	step := func() sim.SavedState {
		t.Helper()
		s.mu.Lock()
		err := s.step()
		s.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		return check()
	}

	// A pod fault on each member and debris on the route of the members
	// are refused. Debris on a free lane starts.
	reject(Command{Action: "fault", PodID: "front"})
	reject(Command{Action: "fault", PodID: "rear"})
	reject(memberDebris)
	if apply(freeDebris).FaultID == "" {
		t.Fatal("the debris on a free lane did not start")
	}
	// An emergency on a member starts. The record is deferred, and the
	// member gets no hold and no purpose while it is in the train.
	if apply(Command{Action: "emergency", PodID: "rear"}).EmergencyID == "" {
		t.Fatal("the emergency on a member did not start")
	}
	state := check()
	rear := savedPod(state, "rear")
	riders := slices.Clone(rear.Riders)
	if rear.Withdrawn != 0 || rear.Purpose != 0 || state.Emergencies == nil || len(state.Emergencies.Records) != 1 {
		t.Fatalf("member %+v, emergencies %+v", rear, state.Emergencies)
	}
	client.mustApply(t, Command{Action: "pause", Paused: false})
	for len(state.CouplingGroups) != 0 {
		rear = savedPod(state, "rear")
		if rear.Withdrawn != 0 || rear.Purpose != 0 || !reflect.DeepEqual(rear.Riders, riders) {
			t.Fatalf("tick %d: member %+v", state.Tick, rear)
		}
		if state.Tick == 3000 {
			// The refusals hold in each phase of the train.
			reject(Command{Action: "fault", PodID: "front"})
			reject(Command{Action: "fault", PodID: "rear"})
			reject(memberDebris)
		}
		if state.Tick > 20000 {
			t.Fatal("the train did not split")
		}
		state = step()
	}
	// The split clears the membership at the end of the tick. The
	// emergency stage of the next tick withdraws the pod.
	run.split = state.Tick
	if rear = savedPod(state, "rear"); rear.Withdrawn != 0 {
		t.Fatalf("tick %d: the member has a hold at the split: %+v", state.Tick, rear)
	}
	state = step()
	if rear = savedPod(state, "rear"); rear.Withdrawn != 2 || len(state.Emergencies.Records) != 1 {
		t.Fatalf("tick %d: the stage after the split did not withdraw the pod: %+v", state.Tick, rear)
	}
	// After the split, the pods are not members. A pod fault on the front
	// starts.
	if apply(Command{Action: "fault", PodID: "front"}).FaultID == "" {
		t.Fatal("the pod fault after the split did not start")
	}
	for len(state.Emergencies.Records) != 0 {
		if state.Tick > 20000 {
			t.Fatal("the emergency did not end")
		}
		state = step()
	}
	if state.Emergencies.Counters.Started != 1 || state.Emergencies.Counters.Ended != 1 || savedPod(state, "rear").Withdrawn != 0 {
		t.Fatalf("emergencies %+v, pod %+v", state.Emergencies, savedPod(state, "rear"))
	}
	for state.Tick < 12000 {
		state = step()
	}
	s.mu.Lock()
	file := sessionStateFile(t, s)
	s.mu.Unlock()
	run.save = sha256.Sum256(encodeTestState(t, file))
	return run
}

// TestCouplingIncidentSession checks the fault and emergency commands on
// the members of a committed train in a session with each incident
// marker. A pod fault on a member, and debris on the remaining route of a
// member, are refused with "fault target is not supported" and change
// nothing. Debris on a free lane starts. An emergency on a member starts,
// and the record stays deferred with no hold until the train splits. The
// emergency stage of the next tick withdraws the pod, and the record ends
// as for a pod that is not a member. After the split, a pod fault on the
// front starts.
//
// The test makes the run twice from the same save. Both runs give the
// same replies, split at the same tick, and save the same bytes.
func TestCouplingIncidentSession(t *testing.T) {
	t.Parallel()
	first := runCouplingIncident(t)
	second := runCouplingIncident(t)
	if !slices.Equal(first.replies, second.replies) || first.split != second.split || first.save != second.save {
		t.Fatalf("the runs differ:\n%v split %d save %x\n%v split %d save %x",
			first.replies, first.split, first.save, second.replies, second.split, second.save)
	}
	t.Logf("split at tick %d, save %x", first.split, first.save)
}

// TestCouplingIncidentCommandsOverHTTP sends the commands of
// TestCouplingIncidentSession to the session handler. A refusal is
// command_rejected with HTTP status 409 and the message of the contract.
func TestCouplingIncidentCommandsOverHTTP(t *testing.T) {
	t.Parallel()
	client := couplingIncidentClient(t)
	handler := client.session.Handler(t.TempDir())
	post := func(command Command) (int, Reply) {
		t.Helper()
		body, err := json.Marshal(client.next(command))
		if err != nil {
			t.Fatal(err)
		}
		recorder := postCommand(t, handler, body)
		var reply Reply
		if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		return recorder.Code, reply
	}
	before := couplingIncidentState(client.session)
	for _, command := range []Command{{Action: "fault", PodID: "front"}, {Action: "fault", PodID: "rear"}, memberDebris} {
		code, reply := post(command)
		if code != http.StatusConflict || reply.ErrorCode != CommandRejected || reply.Error != "fault target is not supported" || reply.FaultID != "" {
			t.Fatalf("%+v: %d %+v", command, code, reply)
		}
	}
	if !reflect.DeepEqual(couplingIncidentState(client.session), before) {
		t.Fatal("a refused command changed the state")
	}
	if code, reply := post(freeDebris); code != http.StatusOK || reply.FaultID == "" {
		t.Fatalf("debris on a free lane: %d %+v", code, reply)
	}
	code, reply := post(Command{Action: "emergency", PodID: "rear"})
	if code != http.StatusOK || reply.EmergencyID == "" {
		t.Fatalf("emergency on a member: %d %+v", code, reply)
	}
	if rear := savedPod(couplingIncidentState(client.session), "rear"); rear.Withdrawn != 0 || rear.Purpose != 0 {
		t.Fatalf("the member has a hold or a purpose: %+v", rear)
	}
}

// TestCouplingIncidentSaves checks the saves of a train with an incident
// member (section 10.7 of the incident emergency contract). A save with a
// deferred record on a member restores in the physical tier and saves the
// same bytes again. The restore loop gives such a save only the logical
// tier, and the coupling contract refuses the logical tier for a train,
// so the save moves aside as invalid_state.
//
// A save in which a member has a hold, or a hold and a purpose, breaks
// invariant E6. It moves aside as invalid_state before either tier, with
// each marker and with the incident marker alone. The save decoder
// refuses a purpose whose owner is not a hold of the pod before E6, so
// the simulation test TestEmergencyCouplingRestore checks a purpose
// without a hold.
func TestCouplingIncidentSaves(t *testing.T) {
	t.Parallel()
	t.Run("deferred member", func(t *testing.T) {
		t.Parallel()
		client := couplingIncidentClient(t)
		client.mustApply(t, Command{Action: "emergency", PodID: "rear"})
		s := client.session
		s.mu.Lock()
		file := sessionStateFile(t, s)
		s.mu.Unlock()
		file.RestoreAttempts = 0
		data := encodeTestState(t, file)
		store := &fakeStore{data: data}
		restored, err := NewFromStore(t.Context(), StoreInput{Store: store})
		if err != nil || restored.restore.Tier != "physical" {
			t.Fatalf("restore %v, %+v", err, restored.restore)
		}
		t.Cleanup(restored.Close)
		state := couplingIncidentState(restored)
		if !reflect.DeepEqual(state, file.Simulation) || savedPod(state, "rear").Withdrawn != 0 || len(state.Emergencies.Records) != 1 {
			t.Fatal("the physical restore changed the state")
		}
		again := file
		again.Simulation = store.lastWrite(t).Simulation
		if !bytes.Equal(encodeTestState(t, again), data) {
			t.Fatal("the save of the restored session differs")
		}
		file.RestoreAttempts = restoreLoopAttempts - 1
		logical := &fakeStore{data: encodeTestState(t, file)}
		restarted, err := NewFromStore(t.Context(), StoreInput{Store: logical})
		assertMovedAside(t, restarted, err, logical, reasonInvalidState)
	})
	for _, test := range []struct {
		name        string
		emergencies bool
		withdrawn   uint8
		purpose     uint8
		owner       uint8
	}{
		{"emergency hold", true, 2, 0, 0},
		{"fault hold", true, 1, 0, 0},
		{"recovery", true, 1, 3, 1},
		{"hold without the emergency marker", false, 1, 0, 0},
		{"recovery without the emergency marker", false, 1, 3, 1},
	} {
		for _, attempts := range []int{0, restoreLoopAttempts - 1} {
			t.Run(fmt.Sprintf("%s/attempts %d", test.name, attempts), func(t *testing.T) {
				t.Parallel()
				file := couplingIncidentFile(t, test.emergencies)
				file.RestoreAttempts = attempts
				file.Simulation.Pods = slices.Clone(file.Simulation.Pods)
				index := slices.IndexFunc(file.Simulation.Pods, func(pod sim.SavedPod) bool { return pod.ID == "rear" })
				pod := &file.Simulation.Pods[index]
				pod.Withdrawn, pod.Purpose, pod.Owner = test.withdrawn, test.purpose, test.owner
				store := &fakeStore{data: encodeTestState(t, file)}
				var restoreErr error
				steps := realRestoreSteps()
				steps.restoreSimulation = func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
					s, result, err := sim.RestoreState(input)
					restoreErr = err
					return s, result, err
				}
				s, err := newFromStore(t.Context(), StoreInput{Store: store}, steps)
				assertMovedAside(t, s, err, store, reasonInvalidState)
				// The restore loop refuses the logical tier for a train
				// before the restore. Otherwise the pre-tier E6 check
				// refuses the save.
				if attempts == 0 && (restoreErr == nil || !strings.Contains(restoreErr.Error(), "E6: coupling member rear has a hold or a purpose")) {
					t.Fatalf("restore error %v, want E6", restoreErr)
				}
			})
		}
	}
}
