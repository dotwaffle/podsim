package session

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

// incidentSaveProject returns the example project with the incident
// marker, the first pods of its fleet, and shared rides of 4 parties.
// Demand is off, so the test submits the orders itself.
func incidentSaveProject(pods int) project.Config {
	config := project.Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.Fleet = config.Fleet[:pods]
	config.SharedRidePartyLimit = 4
	return config
}

// incidentSave is a session with a store, for the full saves of section
// 14.5 of the incident contract.
type incidentSave struct {
	s      *Session
	store  *fakeStore
	stream *incidentStream
}

// incidentStream is the stream client of an incidentSave: the last
// applied frame, its sequence, and the assembler of the topology.
type incidentStream struct {
	assembler *StreamAssembler
	frame     StreamFrame
	sequence  uint64
}

func newIncidentSave(t *testing.T, config project.Config) incidentSave {
	t.Helper()
	store := &fakeStore{}
	s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return incidentSave{s: s, store: store, stream: &incidentStream{}}
}

// operate runs one stage 1 operation at a paused command boundary: under
// the lock, followed by a pause command. The epilogue of the command
// delivers the interruptions. operate checks the delivery before it
// releases the lock, so the guards of a save and a state read cannot hide
// a missed epilogue.
func (x incidentSave) operate(t *testing.T, pod string, operation sim.IncidentTestOperation) {
	t.Helper()
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	if err := x.s.simulation.IncidentForTest(pod, operation); err != nil {
		t.Fatalf("%s %s: %v", operation.Kind, pod, err)
	}
	if _, err := x.s.apply(Command{Action: "pause", Paused: true}); err != nil {
		t.Fatal(err)
	}
	if undelivered := x.s.simulation.DrainInterruptions(); undelivered != nil {
		t.Fatalf("%s %s: undelivered interruptions %v after the command", operation.Kind, pod, undelivered)
	}
}

// run ends the pause of the session with a command. The caller holds
// x.s.mu.
func (x incidentSave) run(t *testing.T) {
	t.Helper()
	if _, err := x.s.apply(Command{Action: "pause", Paused: false}); err != nil {
		t.Fatal(err)
	}
}

// submit adds an order of one party with shared consent.
func (x incidentSave) submit(t *testing.T, options sim.TripOptions) int {
	t.Helper()
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	options.SharingConsent = sim.SharedConsent
	id, err := x.s.simulation.SubmitTripOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// stepUntil ends the pause of the session and steps it until done
// reports true for the exported state, for at most 10 simulated minutes.
func (x incidentSave) stepUntil(t *testing.T, what string, done func(sim.SavedState) bool) {
	t.Helper()
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	x.run(t)
	for range 600 * sim.TicksPerSecond {
		if done(x.s.simulation.ExportState()) {
			return
		}
		x.s.step()
	}
	t.Fatalf("no %s after 10 minutes", what)
}

// fixtureRestoreSteps returns the restore steps of NewFromStore without
// the incident policy step. The stage 1 test entry IncidentForTest makes
// states with the emergency hold or an emergency unload and no emergency
// marker, which no production path makes and which the policy step
// rejects (section 8 of the incident emergency contract). Only the
// session restores of these fixtures use these steps.
// TestIncidentSavePolicy checks that NewFromStore rejects the same saves.
func fixtureRestoreSteps() restoreSteps {
	steps := realRestoreSteps()
	steps.checkPolicy = nil
	return steps
}

// check saves the session and checks the save: the live state passes the
// contract, the adapter gives back the exported state exactly, the session
// restores it with the physical tier and the same export, and a logical
// restore passes the contract. The session restore has no incident policy
// step (see fixtureRestoreSteps). It also checks the stream and the HTTP
// state with checkStream. It returns the exported state.
func (x incidentSave) check(t *testing.T, name string) sim.SavedState {
	t.Helper()
	x.checkStream(t, name)
	x.s.mu.Lock()
	if err := x.s.simulation.CheckContract(); err != nil {
		x.s.mu.Unlock()
		t.Fatalf("%s: %v", name, err)
	}
	want := x.s.simulation.ExportState()
	// A step does not change the revision. A new one makes the save write.
	x.s.revision++
	x.s.mu.Unlock()
	if err := x.s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatalf("%s: save: %v", name, err)
	}
	writes := x.store.writeList()
	data := writes[len(writes)-1]
	file, err := decodeStateFile(data)
	if err != nil {
		t.Fatalf("%s: decode: %v", name, err)
	}
	if resolveErr := file.resolveBoardings(); resolveErr != nil {
		t.Fatalf("%s: references: %v", name, resolveErr)
	}
	if !reflect.DeepEqual(file.Simulation, want) {
		t.Fatalf("%s: the save gives\n%+v\nwant\n%+v", name, file.Simulation, want)
	}
	restored, err := newFromStore(t.Context(), StoreInput{Store: &fakeStore{data: data}}, fixtureRestoreSteps())
	if err != nil || restored.restore.Tier != "physical" {
		t.Fatalf("%s: restore: %v, %+v", name, err, restored.restore)
	}
	defer restored.Close()
	restored.mu.Lock()
	got := restored.simulation.ExportState()
	restored.mu.Unlock()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: the physical restore exports\n%+v\nwant\n%+v", name, got, want)
	}
	config := file.Project
	logical, _, err := sim.RestoreState(sim.RestoreStateInput{
		OrderContract: config.OrderContract, IncidentContract: config.IncidentContract,
		Network: config.Network, Fleet: config.Fleet, State: file.Simulation, LogicalOnly: true,
		ExpressServices: config.ExpressServices, OnboardPickups: config.OnboardPickups,
		FaultContract: config.FaultContract, Faults: project.EffectiveFaultSettings(config),
		EmergencyContract: config.EmergencyContract,
	})
	if err != nil {
		t.Fatalf("%s: logical restore: %v", name, err)
	}
	if err := logical.CheckContract(); err != nil {
		t.Fatalf("%s: logical restore: %v", name, err)
	}
	return want
}

// checkStream sends the presentation frame through the stream codec, as a
// full frame first and then as a delta from the frame of the last check,
// and through the HTTP state. The client gets the same incident members,
// and the assembler and the boarding validator accept each frame.
func (x incidentSave) checkStream(t *testing.T, name string) {
	t.Helper()
	topology := x.s.Topology()
	x.s.mu.Lock()
	saved := x.s.simulation.ExportState()
	frame, err := x.s.presentationFrameLocked()
	x.s.mu.Unlock()
	if err != nil {
		t.Fatalf("%s: frame: %v", name, err)
	}
	want := incidentView(frame.State.Simulation)
	// The frame shows the holds, the purpose, and the counters of the
	// simulation.
	purposes := []string{"", sim.OperationalEmergencyUnload, sim.OperationalRefuge, sim.OperationalEmptyRecovery}
	if want.Interrupted != saved.Interrupted || want.InterruptedPassengers != saved.InterruptedPassengers {
		t.Fatalf("%s: the frame counters are %d and %d", name, want.Interrupted, want.InterruptedPassengers)
	}
	for _, vehicle := range frame.State.Simulation.Vehicles {
		pod := savedPod(saved, vehicle.Pod.ID)
		if vehicle.Withdrawn != pod.Withdrawn || vehicle.Operational != purposes[pod.Purpose] {
			t.Fatalf("%s: vehicle %s shows %d %q, the save has %d %d", name, pod.ID, vehicle.Withdrawn, vehicle.Operational, pod.Withdrawn, pod.Purpose)
		}
	}
	checkFrameEmergencies(t, name, saved, frame.State.Simulation)
	client := x.stream
	envelope := StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "full", Stream: "incident", Sequence: client.sequence + 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	if client.assembler != nil {
		delta, deltaErr := makeDelta(client.frame, frame)
		if deltaErr != nil {
			t.Fatalf("%s: delta: %v", name, deltaErr)
		}
		envelope.Kind, envelope.Full, envelope.Delta, envelope.Base = "delta", nil, &delta, client.sequence
	} else if client.assembler, err = NewStreamAssembler(topology); err != nil {
		t.Fatalf("%s: assembler: %v", name, err)
	}
	data, err := EncodeStreamJSON(envelope)
	if err != nil {
		t.Fatalf("%s: encode: %v", name, err)
	}
	decoded, err := DecodeStreamJSON(data)
	if err != nil {
		t.Fatalf("%s: decode: %v", name, err)
	}
	applied, err := ApplyStream(client.frame, "incident", client.sequence, decoded)
	if err != nil {
		t.Fatalf("%s: apply the %s envelope: %v", name, envelope.Kind, err)
	}
	if got := incidentView(applied.State.Simulation); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: the %s envelope gives\n%+v\nwant\n%+v", name, envelope.Kind, got, want)
	}
	if _, assembleErr := client.assembler.State(applied); assembleErr != nil {
		t.Fatalf("%s: assemble the %s envelope: %v", name, envelope.Kind, assembleErr)
	}
	client.frame, client.sequence = applied, envelope.Sequence
	data, err = EncodeStateJSON(topology, frame)
	if err != nil {
		t.Fatalf("%s: encode the HTTP state: %v", name, err)
	}
	state, err := DecodeStateJSON(data)
	if err != nil {
		t.Fatalf("%s: decode the HTTP state: %v", name, err)
	}
	if got := incidentView(stateFrame(state).Simulation); !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: the HTTP state gives\n%+v\nwant\n%+v", name, got, want)
	}
}

// incidentView is the incident marker and the stage 1 members of a frame,
// with each order in the form "id:legFrom", the fault marker and the
// faults of the frame, and the emergency marker and the emergencies of the
// frame.
type incidentFrameView struct {
	Marker                             sim.IncidentContract
	Interrupted, InterruptedPassengers int
	Vehicles                           []string
	Pending                            []string
	FaultMarker                        sim.FaultContract
	Faults                             sim.FaultsView
	EmergencyMarker                    sim.EmergencyContract
	Emergencies                        sim.EmergenciesView
}

func incidentView(frame SimulationFrame) incidentFrameView {
	order := func(r sim.Request) string { return fmt.Sprintf("%d:%s", r.ID, r.LegFrom) }
	view := incidentFrameView{Marker: frame.IncidentContract, Interrupted: frame.Interrupted, InterruptedPassengers: frame.InterruptedPassengers,
		FaultMarker: frame.FaultContract, Faults: frame.Faults, EmergencyMarker: frame.EmergencyContract, Emergencies: frame.Emergencies}
	for _, vehicle := range frame.Vehicles {
		riders := make([]string, len(vehicle.Riders))
		for i, rider := range vehicle.Riders {
			riders[i] = order(rider)
		}
		view.Vehicles = append(view.Vehicles, fmt.Sprintf("%s %d %q %v", vehicle.Pod.ID, vehicle.Withdrawn, vehicle.Operational, riders))
	}
	for _, request := range frame.Pending {
		view.Pending = append(view.Pending, order(request))
	}
	return view
}

// savedPod returns the saved pod id of state.
func savedPod(state sim.SavedState, id string) sim.SavedPod {
	index := slices.IndexFunc(state.Pods, func(pod sim.SavedPod) bool { return pod.ID == id })
	return state.Pods[index]
}

// activeRiders counts the active riders of a saved pod.
func activeRiders(pod sim.SavedPod) int {
	count := 0
	for _, rider := range pod.Riders {
		if !rider.Completed {
			count++
		}
	}
	return count
}

// boardTwo submits two orders from harbor to market and steps until pod
// 01 carries both on the lane to the branch.
func (x incidentSave) boardTwo(t *testing.T) {
	t.Helper()
	for range 2 {
		x.submit(t, sim.TripOptions{From: "harbor", To: "market"})
	}
	x.stepUntil(t, "pod 01 with two riders on approach-branch", func(state sim.SavedState) bool {
		pod := savedPod(state, "01")
		return pod.Activity == "traveling" && pod.LaneID == "approach-branch" && activeRiders(pod) == 2
	})
}

// TestIncidentSaveEmergencyUnload saves each state of an emergency unload
// at a command boundary and at the end of the tick of each transition
// (incident contract, section 14.5): a withdrawn traveling pod, purpose 1
// traveling and unloading, the interrupted counters, a withdrawn pod at a
// berth, and a leg origin waiting, aboard, and in history.
func TestIncidentSaveEmergencyUnload(t *testing.T) {
	t.Parallel()
	x := newIncidentSave(t, incidentSaveProject(1))
	x.boardTwo(t)
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "withdraw", Hold: 2})
	if pod := savedPod(x.check(t, "withdrawn traveling"), "01"); pod.Withdrawn != 2 {
		t.Fatalf("holds %d", pod.Withdrawn)
	}
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "destination", Hold: 2, Purpose: 1, Interrupt: 1, Station: "garden", Berth: "garden-1"})
	if pod := savedPod(x.check(t, "purpose 1 traveling"), "01"); pod.Purpose != 1 || pod.Owner != 2 || pod.Interrupt != 1 {
		t.Fatalf("operational %d %d %d", pod.Purpose, pod.Owner, pod.Interrupt)
	}
	x.stepUntil(t, "the unload", func(state sim.SavedState) bool { return savedPod(state, "01").Activity == "unloading" })
	x.check(t, "purpose 1 unloading")
	x.stepUntil(t, "the end of the unload", func(state sim.SavedState) bool { return savedPod(state, "01").Activity == "idle" })
	state := x.check(t, "after the unload")
	if state.Interrupted != 1 || state.InterruptedPassengers != 1 || len(state.Waiting) != 1 ||
		state.Waiting[0].Request.LegFrom != "garden" || !state.Waiting[0].Boarded {
		t.Fatalf("interrupted %d and %d, queue %+v", state.Interrupted, state.InterruptedPassengers, state.Waiting)
	}
	transferred := state.Waiting[0].Request.ID
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "restore", Hold: 2})
	x.stepUntil(t, "the transferred party aboard", func(state sim.SavedState) bool { return activeRiders(savedPod(state, "01")) == 1 })
	if rider := savedPod(x.check(t, "leg origin aboard"), "01").Riders; !slices.ContainsFunc(rider, func(r sim.SavedRequest) bool {
		return r.ID == transferred && r.LegFrom == "garden" && !r.Completed
	}) {
		t.Fatalf("riders %+v", rider)
	}
	x.stepUntil(t, "the completion", func(state sim.SavedState) bool { return state.Completed == 1 })
	if rider := savedPod(x.check(t, "leg origin in history"), "01").Riders; !slices.ContainsFunc(rider, func(r sim.SavedRequest) bool {
		return r.ID == transferred && r.LegFrom == "garden" && r.Completed
	}) {
		t.Fatalf("riders %+v", rider)
	}
}

// TestIncidentSavePolicy checks the incident policy step of NewFromStore
// (section 8 of the incident emergency contract). A save without the
// emergency marker with the emergency hold, and one with an emergency
// unload, restore in the physical tier without the policy step. With the
// step, each one is invalid_state before either tier, and it moves aside.
// With the emergency marker, the policy accepts the same saves, and the
// pre-tier checks reject them, because no record names the pod (E2).
func TestIncidentSavePolicy(t *testing.T) {
	t.Parallel()
	x := newIncidentSave(t, incidentSaveProject(1))
	x.boardTwo(t)
	var saves [][]byte
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "withdraw", Hold: 2})
	x.check(t, "emergency hold")
	saves = append(saves, x.store.writeList()[len(x.store.writeList())-1])
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "destination", Hold: 2, Purpose: 1, Interrupt: 1, Station: "garden", Berth: "garden-1"})
	x.check(t, "emergency unload")
	saves = append(saves, x.store.writeList()[len(x.store.writeList())-1])
	marked := func(file *stateFile) {
		file.Project.EmergencyContract, file.Project.Emergencies = project.EmergencyV1Contract, &project.EmergencyConfig{}
	}
	for index, data := range saves {
		for _, test := range []struct {
			save []byte
			want string
		}{
			{data, "without the emergency contract"},
			{storedRun{data: data}.edited(t, marked), "E2: pod 01 has the emergency hold and no emergency record"},
		} {
			if _, err := newTestSession(t).loadState(loadInput{data: test.save, steps: realRestoreSteps()}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("save %d: load error %v, want %q", index, err, test.want)
			}
			store := &fakeStore{data: test.save}
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			if err != nil {
				t.Fatal(err)
			}
			if s.restore.Tier != restoreEmpty || s.restore.Reason != reasonInvalidState ||
				!slices.Equal(store.callList(), []string{"read", "reject", "write"}) {
				t.Errorf("save %d: restore %+v, store calls %v", index, s.restore, store.callList())
			}
			s.Close()
		}
	}
}

// TestIncidentSaveRefuge saves purpose 2 traveling and holding at a
// parking refuge, a fault evacuation at the berth, and a resume from the
// refuge.
func TestIncidentSaveRefuge(t *testing.T) {
	t.Parallel()
	hold := func(t *testing.T) incidentSave {
		t.Helper()
		x := newIncidentSave(t, incidentSaveProject(1))
		x.boardTwo(t)
		x.operate(t, "01", sim.IncidentTestOperation{Kind: "withdraw", Hold: 1})
		x.operate(t, "01", sim.IncidentTestOperation{Kind: "destination", Hold: 1, Purpose: 2, Station: "parking", Berth: "parking-1"})
		x.check(t, "purpose 2 traveling")
		x.stepUntil(t, "the refuge", func(state sim.SavedState) bool { return savedPod(state, "01").Activity == "unloading" })
		if pod := savedPod(x.check(t, "purpose 2 holding"), "01"); pod.BerthID != "parking-1" || pod.Purpose != 2 || activeRiders(pod) != 2 {
			t.Fatalf("holding pod %+v", pod)
		}
		return x
	}
	t.Run("evacuate", func(t *testing.T) {
		t.Parallel()
		x := hold(t)
		x.operate(t, "01", sim.IncidentTestOperation{Kind: "evacuate"})
		state := x.check(t, "evacuated at the refuge")
		if pod := savedPod(state, "01"); state.Interrupted != 2 || pod.Activity != "idle" || pod.Purpose != 0 || pod.Withdrawn != 1 {
			t.Fatalf("interrupted %d, pod %+v", state.Interrupted, pod)
		}
	})
	t.Run("resume", func(t *testing.T) {
		t.Parallel()
		x := hold(t)
		x.operate(t, "01", sim.IncidentTestOperation{Kind: "resume"})
		if pod := savedPod(x.check(t, "resumed"), "01"); pod.Activity != "continuing" || pod.Purpose != 0 {
			t.Fatalf("resumed pod %+v", pod)
		}
	})
}

// TestIncidentSaveExcludedTrip saves an excluded trip after the release
// of its pod, and after its assignment to another pod.
func TestIncidentSaveExcludedTrip(t *testing.T) {
	t.Parallel()
	x := newIncidentSave(t, incidentSaveProject(2))
	order := x.submit(t, sim.TripOptions{From: "market", To: "harbor"})
	var pod string
	x.stepUntil(t, "an empty pod on its way to market", func(state sim.SavedState) bool {
		if len(state.Waiting) != 1 || state.Waiting[0].Request.PodID == "" {
			return false
		}
		pod = state.Waiting[0].Request.PodID
		saved := savedPod(state, pod)
		return saved.Activity == "traveling" && !saved.Occupied
	})
	x.operate(t, pod, sim.IncidentTestOperation{Kind: "withdraw", Hold: 1})
	state := x.check(t, "excluded trip")
	if trip := state.Waiting[0]; trip.Request.ID != order || trip.ExcludedPod != pod || trip.Request.PodID != "" {
		t.Fatalf("trip %+v", trip)
	}
	x.stepUntil(t, "the assignment to another pod", func(state sim.SavedState) bool {
		return len(state.Waiting) == 1 && state.Waiting[0].Request.PodID != "" || len(state.Waiting) == 0
	})
	state = x.check(t, "excluded trip assigned")
	if len(state.Waiting) == 1 && (state.Waiting[0].Request.PodID == pod || state.Waiting[0].ExcludedPod != pod) {
		t.Fatalf("trip %+v", state.Waiting[0])
	}
}

// railEvacuationSave returns a session with the stage 1 marker and two
// pods. The rail-bound order of interruptionProject and its companion ride
// pod 01 from market to harbor. Pod 02 has a fault hold at the only berth
// of harbor, so pod 01 stops on the berth access lane with both orders
// aboard. The session runs.
func railEvacuationSave(t *testing.T) (incidentSave, railPair) {
	t.Helper()
	config := interruptionProject(3600)
	config.Fleet = []sim.Placement{{ID: "01", StationID: "market", BerthID: "market-1"}, {ID: "02", StationID: "harbor", BerthID: "harbor-1"}}
	x := newIncidentSave(t, config)
	x.operate(t, "02", sim.IncidentTestOperation{Kind: "withdraw", Hold: 1})
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	x.run(t)
	pair := boardRailPair(t, x.s)
	for range 600 * sim.TicksPerSecond {
		vehicle := x.s.simulation.Snapshot().Vehicles[0]
		if vehicle.Pod.Activity == sim.Traveling && vehicle.Pod.LaneID != "" && vehicle.Pod.Speed == 0 {
			if pair.pod != "01" || vehicle.RidersAboard() != 2 || vehicle.Pod.BlockedBy != "02" {
				t.Fatalf("pair %+v, pod %+v with %d riders", pair, vehicle.Pod, vehicle.RidersAboard())
			}
			return x, pair
		}
		x.s.step()
	}
	t.Fatal("pod 01 does not stop on a lane")
	return x, pair
}

// checkRailSave checks the last save of x after a fault evacuation that
// interrupted the rail-bound order of pair and its companion. The save
// and published, the counts of a state read right after the command, show
// one unserved order. The save restores, the rail record of the order is
// unserved with reason interrupted, and the restore accounts for each
// order.
func (x incidentSave) checkRailSave(t *testing.T, pair railPair, published rail.Counts) {
	t.Helper()
	want := rail.Counts{Unserved: 1}
	if file := x.store.lastWrite(t); published != want || file.Demand.State.Connections != want {
		t.Fatalf("published counts %+v, saved counts %+v, want %+v", published, file.Demand.State.Connections, want)
	}
	writes := x.store.writeList()
	restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: writes[len(writes)-1]}})
	if err != nil || restored.restore.Tier != "physical" {
		t.Fatalf("restore: %v, %+v", err, restored.restore)
	}
	t.Cleanup(restored.Close)
	restored.mu.Lock()
	defer restored.mu.Unlock()
	checkDelivered(t, restored, pair.bound, want)
	if got := restored.simulation.Snapshot(); got.Interrupted != 2 || got.InterruptedPassengers != 2 || restored.restore.Unaccounted != 0 {
		t.Fatalf("restored counters %d and %d with %d unaccounted orders, want 2, 2, and 0",
			got.Interrupted, got.InterruptedPassengers, restored.restore.Unaccounted)
	}
}

// TestIncidentSaveRailEvacuation saves a fault evacuation on a lane with a
// rail-bound order aboard (incident contract, section 14.5): at the paused
// command boundary, purpose 3 traveling with the delivered interruptions,
// and at the end of the tick of the arrival.
func TestIncidentSaveRailEvacuation(t *testing.T) {
	t.Parallel()
	x, pair := railEvacuationSave(t)
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "withdraw", Hold: 1})
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "evacuate"})
	x.s.mu.Lock()
	checkDelivered(t, x.s, pair.bound, rail.Counts{Unserved: 1})
	x.s.mu.Unlock()
	published := x.s.State().Demand.Connections
	state := x.check(t, "purpose 3 traveling")
	if pod := savedPod(state, "01"); pod.Activity != "traveling" || pod.LaneID == "" || pod.Purpose != 3 || pod.Owner != 1 ||
		pod.RelocatingTo != "harbor" || len(pod.Riders) != 0 || state.Interrupted != 2 || state.InterruptedPassengers != 2 {
		t.Fatalf("interrupted %d and %d, pod %+v", state.Interrupted, state.InterruptedPassengers, pod)
	}
	x.checkRailSave(t, pair, published)
	x.operate(t, "02", sim.IncidentTestOperation{Kind: "restore", Hold: 1})
	x.stepUntil(t, "the arrival", func(state sim.SavedState) bool { return savedPod(state, "01").Activity == "idle" })
	if pod := savedPod(x.check(t, "purpose 3 arrival"), "01"); pod.BerthID != "harbor-1" || pod.Purpose != 0 || pod.Withdrawn != 1 {
		t.Fatalf("pod after the arrival %+v", pod)
	}
}

// TestIncidentSaveStranded saves a stranded transferred Express order: an
// emergency unload transfers each party at garden, where no Express pod
// has a path to market.
func TestIncidentSaveStranded(t *testing.T) {
	t.Parallel()
	config := expressConsumerProject(t)
	config.IncidentContract = sim.IncidentV1Contract
	classes, err := sim.NewClassSet("group")
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.Network.Lanes {
		if config.Network.Lanes[i].ID == "garden-1-out" {
			config.Network.Lanes[i].VehicleClasses = classes
		}
	}
	x := newIncidentSave(t, config)
	for range 2 {
		x.submit(t, sim.TripOptions{From: "harbor", To: "market", Service: sim.ExpressServiceChoice, ServiceID: "harbor-market"})
	}
	x.stepUntil(t, "pod 01 with two riders on its way", func(state sim.SavedState) bool {
		pod := savedPod(state, "01")
		return pod.Activity == "traveling" && activeRiders(pod) == 2 && len(pod.Route) > 2
	})
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "withdraw", Hold: 2})
	x.operate(t, "01", sim.IncidentTestOperation{Kind: "destination", Hold: 2, Purpose: 1, Station: "garden", Berth: "garden-1"})
	x.stepUntil(t, "the end of the unload", func(state sim.SavedState) bool { return savedPod(state, "01").Activity == "idle" })
	state := x.check(t, "stranded")
	if len(state.Waiting) != 2 || state.Waiting[0].Request.LegFrom != "garden" || state.Waiting[0].Request.PodID != "" {
		t.Fatalf("queue %+v", state.Waiting)
	}
	x.s.mu.Lock()
	x.s.step()
	x.s.mu.Unlock()
	x.check(t, "stranded after a dispatch")
}
