package session

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
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
	s     *Session
	store *fakeStore
}

func newIncidentSave(t *testing.T, config project.Config) incidentSave {
	t.Helper()
	store := &fakeStore{}
	s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return incidentSave{s: s, store: store}
}

// operate runs one stage 1 operation as a command does: under the lock,
// with the delivery of the command epilogue.
func (x incidentSave) operate(t *testing.T, pod string, operation sim.IncidentTestOperation) {
	t.Helper()
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	defer x.s.deliverInterruptions()
	if err := x.s.simulation.IncidentForTest(pod, operation); err != nil {
		t.Fatalf("%s %s: %v", operation.Kind, pod, err)
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

// stepUntil steps the session until done reports true for the exported
// state, for at most 10 simulated minutes.
func (x incidentSave) stepUntil(t *testing.T, what string, done func(sim.SavedState) bool) {
	t.Helper()
	x.s.mu.Lock()
	defer x.s.mu.Unlock()
	for range 600 * sim.TicksPerSecond {
		if done(x.s.simulation.ExportState()) {
			return
		}
		if err := x.s.step(); err != nil {
			t.Fatal(err)
		}
	}
	t.Fatalf("no %s after 10 minutes", what)
}

// check saves the session and checks the save: the live state passes the
// contract, the adapter gives back the exported state exactly, the session
// restores it with the physical tier and the same export, and a logical
// restore passes the contract. It returns the exported state.
func (x incidentSave) check(t *testing.T, name string) sim.SavedState {
	t.Helper()
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
	restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: data}})
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
		StationQueueSpacing: project.EffectiveStationQueueSpacing(config), PlatoonLimit: config.PlatoonLimit,
		ExpressServices: config.ExpressServices, OnboardPickups: config.OnboardPickups,
	})
	if err != nil {
		t.Fatalf("%s: logical restore: %v", name, err)
	}
	if err := logical.CheckContract(); err != nil {
		t.Fatalf("%s: logical restore: %v", name, err)
	}
	return want
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

// TestIncidentSaveEmptyRecovery saves an excluded trip after the release
// of its pod, purpose 3 traveling, and the arrival, and the excluded trip
// after its assignment to another pod.
func TestIncidentSaveEmptyRecovery(t *testing.T) {
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
	x.operate(t, pod, sim.IncidentTestOperation{Kind: "destination", Hold: 1, Purpose: 3, Station: "parking", Berth: "parking-2"})
	if saved := savedPod(x.check(t, "purpose 3 traveling"), pod); saved.Purpose != 3 || saved.RelocatingTo != "parking" {
		t.Fatalf("pod %+v", saved)
	}
	x.stepUntil(t, "the arrival", func(state sim.SavedState) bool { return savedPod(state, pod).Activity == "idle" })
	x.check(t, "purpose 3 arrival")
	x.stepUntil(t, "the assignment to another pod", func(state sim.SavedState) bool {
		return len(state.Waiting) == 1 && state.Waiting[0].Request.PodID != "" || len(state.Waiting) == 0
	})
	state = x.check(t, "excluded trip assigned")
	if len(state.Waiting) == 1 && (state.Waiting[0].Request.PodID == pod || state.Waiting[0].ExcludedPod != pod) {
		t.Fatalf("trip %+v", state.Waiting[0])
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
	if err := x.s.step(); err != nil {
		t.Fatal(err)
	}
	x.s.mu.Unlock()
	x.check(t, "stranded after a dispatch")
}
