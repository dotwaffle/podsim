package sim

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
)

func compactRestore(t *testing.T, s *Simulation, state SavedState, mode StationQueueSpacing) *Simulation {
	t.Helper()
	r, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, StationQueueSpacing: mode, PlatoonLimit: s.platoonLimit})
	if err != nil || result.Tier != RestorePhysical || result.PhysicalError != nil || len(result.Demoted)+len(result.Dropped)+len(result.Requeued) != 0 {
		t.Fatalf("physical compact restore: %+v %v", result, err)
	}
	// A restored compact group and the compact policy need station buffers
	// before the caller applies the project settings.
	if (hasCompactCertificate(state) || mode == StationQueueCompactV1) && !r.stationBuffers {
		t.Fatal("compact restore left station buffers disabled")
	}
	if err := r.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	r.SetStationBuffers(true)
	if err := r.SetStationQueueSpacing(mode); err != nil {
		t.Fatal(err)
	}
	return r
}

func compactStateFixture(t *testing.T) *Simulation {
	t.Helper()
	s := occupiedBufferQueue(t, PlatooningVirtual)
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	for range 200 * TicksPerSecond {
		compactTick(t, s)
		if len(s.compactGroups) == 1 && len(s.compactGroups[0].members) == 4 && s.vehicles[3].Pod.LaneDistance > 350 {
			return s
		}
	}
	t.Fatal("compact snapshot fixture did not form")
	return nil
}

func TestStationCompactColdRestore(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	state := roundTripState(t, s.ExportState())
	r := compactRestore(t, s, state, StationQueueCompactV1)
	if !reflect.DeepEqual(state, r.ExportState()) {
		t.Fatalf("cold physical restore changed certificate or speed\nexpected=%+v\ngot=%+v", state, r.ExportState())
	}
	for range 300 {
		compactTick(t, s)
		compactTick(t, r)
		if !reflect.DeepEqual(s.ExportState(), r.ExportState()) {
			t.Fatal("cold snapshot trajectories differ")
		}
	}
	clone := s.Clone()
	for range 100 {
		compactTick(t, s)
		compactTick(t, clone)
		if !reflect.DeepEqual(s.ExportState(), clone.ExportState()) {
			t.Fatal("clone shares compact proof state")
		}
	}
	for _, mode := range []StationQueueSpacing{StationQueueOrdinary, StationQueueCompactV1} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			r := compactRestore(t, s, state, mode)
			if err := r.SetStationQueueSpacing(StationQueueOrdinary); err != nil {
				t.Fatal(err)
			}
			for range 1000 * TicksPerSecond {
				compactTick(t, r)
				if len(r.compactGroups) == 0 {
					break
				}
			}
			if len(r.compactGroups) != 0 {
				t.Fatal("disabled cold group failed finite recovery")
			}
			for i := 1; i < 4; i++ {
				if r.vehicles[i-1].Pod.LaneDistance-r.vehicles[i].Pod.LaneDistance < 12.01 {
					t.Fatal("disabled group lost ordinary spacing")
				}
			}
		})
	}
}

func TestStationCompactMalformedRestore(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	baseState := s.ExportState()
	for _, tc := range []struct {
		name string
		edit func(*SavedState)
	}{
		{"missing contract", func(st *SavedState) { st.Pods[0].CompactQueue.Kind = "unknown" }},
		{"phase", func(st *SavedState) { st.Pods[0].CompactQueue.Phase = "draining" }},
		{"missing speeds", func(st *SavedState) { st.Pods[0].CompactQueue.Speeds = nil }},
		{"duplicate member", func(st *SavedState) { q := st.Pods[0].CompactQueue; q.Members[3] = q.Members[2] }},
		{"missing member", func(st *SavedState) { st.Pods[0].CompactQueue.Members[3] = "absent" }},
		{"speed above band", func(st *SavedState) { st.Pods[0].CompactQueue.Speeds[2] = 2.50001 }},
		{"speed nonfinite", func(st *SavedState) { st.Pods[0].CompactQueue.Speeds[2] = math.NaN() }},
		{"target changed", func(st *SavedState) { st.Pods[0].CompactQueue.Targets[1] += 1 }},
		{"frontier changed", func(st *SavedState) { st.Pods[0].CompactQueue.Frontier += 30 }},
		{"owned cell enlarged", func(st *SavedState) { st.Pods[0].CompactQueue.StopCells[1] += 2 }},
		{"owned cell overflow", func(st *SavedState) { st.Pods[0].CompactQueue.StopCells[1] = math.MaxInt }},
		{"proof changed", func(st *SavedState) { st.Pods[0].CompactQueue.LandingSpeeds[0] = 0 }},
		{"head certificate missing", func(st *SavedState) { st.Pods[0].CompactQueue = nil }},
		{"predecessor changed", func(st *SavedState) { st.Pods[2].Platoon.Leader = "01" }},
		{"draining compact link", func(st *SavedState) { st.Pods[2].Platoon.Draining = true }},
		{"missing follower link", func(st *SavedState) { st.Pods[2].Platoon = nil }},
		{"distance inconsistent", func(st *SavedState) { st.Pods[2].Distance += 1 }},
		{"profile unsupported", func(st *SavedState) { st.Pods[2].Class = GroupClass }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, logical := range []bool{false, true} {
				state := roundTripState(t, baseState)
				tc.edit(&state)
				r, _, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, StationQueueSpacing: StationQueueCompactV1, LogicalOnly: logical})
				if err == nil || r != nil {
					t.Fatal("invalid compact certificate fell back or repaired")
				}
			}
		})
	}
	_, _, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), LogicalOnly: true})
	if err == nil {
		t.Fatal("logical-only discarded valid compact physical proof")
	}
}

func TestStationCompactRecoveringSnapshot(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	if err := s.SetStationQueueSpacing(StationQueueOrdinary); err != nil {
		t.Fatal(err)
	}
	compactTick(t, s)
	state := roundTripState(t, s.ExportState())
	if state.Pods[0].CompactQueue == nil || state.Pods[0].CompactQueue.Phase != "recovering" {
		t.Fatal("no frozen recovery snapshot")
	}
	r := compactRestore(t, s, state, StationQueueOrdinary)
	targets := slices.Clone(state.Pods[0].CompactQueue.Targets)
	for range 1000 * TicksPerSecond {
		compactTick(t, s)
		compactTick(t, r)
		if len(r.compactGroups) == 0 {
			if len(s.compactGroups) != 0 {
				t.Fatal("recovery completed at a different tick")
			}
			break
		}
		if !reflect.DeepEqual(s.ExportState(), r.ExportState()) {
			t.Fatalf("retained recovery trajectory changed on restore\nexpected=%+v\ngot=%+v", s.ExportState(), r.ExportState())
		}
		if len(r.compactGroups) == 0 {
			break
		}
		if !slices.Equal(targets, r.compactGroups[0].recovery.targets) {
			t.Fatal("recovery recomputed its destinations")
		}
	}
	if len(r.compactGroups) != 0 {
		t.Fatal("retained recovery did not finish")
	}
}

// compactHeldDepartureQueue prevents idle clearing by omitting outgoing roads.
// The four incoming trips have real riders. Release restores the same prefix.
func compactHeldDepartureQueue(t *testing.T) *Simulation {
	t.Helper()
	geometry := departingBufferQueue(t)
	detachIndexes(geometry)
	geometry.network.Lanes = slices.DeleteFunc(slices.Clone(geometry.network.Lanes), func(lane Lane) bool {
		return lane.From == "market-exit" && lane.StationRole == ""
	})
	s := stageBufferFleet(t, geometry.network, geometry.initial, 4, false)
	s.owners[resource{kind: berthResource, id: "market-1"}] = podResourceOwner("05")
	state := s.ExportState()
	for i := range 4 {
		pod := &state.Pods[i]
		position := float64(300 - i*30)
		pod.Distance += position - pod.LaneDistance
		pod.LaneDistance = position
	}
	return compactRestore(t, s, state, StationQueueCompactV1)
}

func compactReleaseDepartureQueue(t *testing.T, s *Simulation, mode StationQueueSpacing) *Simulation {
	t.Helper()
	state := roundTripState(t, s.ExportState())
	template := s.Clone()
	detachIndexes(template)
	template.network.Lanes = slices.Clone(s.network.Lanes)
	for _, lane := range Example().Lanes {
		if lane.From == "market-exit" && lane.StationRole == "" {
			template.network.Lanes = append(template.network.Lanes, lane)
		}
	}
	r := compactRestore(t, template, state, mode)
	if !reflect.DeepEqual(state, r.ExportState()) {
		t.Fatal("opening departure roads changed the physical prefix, proof, or riders")
	}
	return r
}

// Every checkpoint comes from the same real boarding and departure run.
func TestStationCompactColdDeparturePhases(t *testing.T) {
	t.Parallel()
	skipLong(t)
	s := compactHeldDepartureQueue(t)
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	requested := false
	phases := make(map[string]*Simulation)
	capture := func(name string) {
		if phases[name] == nil {
			phases[name] = s.Clone()
		}
	}
	for range 1000 * TicksPerSecond {
		compactTick(t, s)
		for _, group := range s.compactGroups {
			if group.recovering {
				capture("recovering")
			}
			for _, index := range group.members[1:] {
				v := &s.vehicles[index]
				leader := &s.vehicles[v.link.leader-1]
				gap := leader.Pod.LaneDistance - v.Pod.LaneDistance
				if gap > Clearance+stoppingDistance(2.5) {
					capture("wide")
				}
				if s.holdsPending(v) {
					capture("shared")
				}
				if len(group.members) == 4 && gap < Clearance {
					capture("compacted")
				}
			}
		}
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if v.destination.ID != "" && v.follower != 0 && s.vehicles[v.follower-1].link.buffer && s.vehicles[v.follower-1].link.draining {
				capture("suffix")
			}
		}
		if !requested && len(s.compactGroups) == 1 && len(s.compactGroups[0].members) == 4 {
			stopped := true
			for _, index := range s.compactGroups[0].members {
				stopped = stopped && s.vehicles[index].Pod.Speed == 0
			}
			if stopped && s.vehicles[0].Pod.LaneDistance-s.vehicles[3].Pod.LaneDistance <= 3*compactQueueStandstillGap+1e-9 {
				s = compactReleaseDepartureQueue(t, s, StationQueueCompactV1)
				if err := s.RequestJourney("05", "harbor"); err != nil {
					t.Fatal(err)
				}
				requested = true
			}
		}
		if s.completed == 5 {
			break
		}
	}
	if !requested || s.completed != 5 {
		t.Fatal("original departure did not complete")
	}
	for _, phase := range []string{"wide", "shared", "compacted", "recovering", "suffix"} {
		checkpoint := phases[phase]
		if checkpoint == nil {
			t.Fatalf("missing %s physical checkpoint", phase)
		}
		state := roundTripState(t, checkpoint.ExportState())
		for _, mode := range []StationQueueSpacing{StationQueueOrdinary, StationQueueCompactV1} {
			t.Run(fmt.Sprintf("%s_%s", phase, mode), func(t *testing.T) {
				t.Parallel()
				r := compactRestore(t, checkpoint, state, mode)
				if r.findVehicle("05").Pod.Activity == Idle {
					r = compactReleaseDepartureQueue(t, r, mode)
				}
				if r.findVehicle("05").Pod.Activity == Idle {
					if err := r.RequestJourney("05", "harbor"); err != nil {
						t.Fatal(err)
					}
				}
				for range 1000 * TicksPerSecond {
					compactTick(t, r)
					if r.completed == 5 {
						break
					}
				}
				if r.completed != 5 || len(r.compactGroups) != 0 || r.NeedsBufferPlatoonState() {
					t.Fatalf("cold discharge failed: completed=%d state=%+v", r.completed, r.Snapshot())
				}
			})
		}
	}
}

func TestStationCompactClassDeparture(t *testing.T) {
	t.Parallel()
	skipLong(t)
	fixture := departingBufferQueue(t)
	state := fixture.ExportState()
	fleet := slices.Clone(fixture.initial)
	// Select the physical fixture's class in both saved pods and initial fleet.
	// The running simulation never changes a vehicle's class.
	for i := range fleet {
		fleet[i].Class = CompactClass
		state.Pods[i].Class = CompactClass
	}
	s, result, err := RestoreState(RestoreStateInput{Network: fixture.network, Fleet: fleet, State: state})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("compact-class fixture restore: %+v %v", result, err)
	}
	s.SetStationBuffers(true)
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	restored := false
	for range 1000 * TicksPerSecond {
		compactTick(t, s)
		for _, v := range s.vehicles {
			if v.Pod.Class != CompactClass {
				t.Fatal("compact queue or arrival changed immutable class")
			}
		}
		if !restored && len(s.compactGroups) != 0 && len(s.compactGroups[0].members) == 4 {
			s = compactRestore(t, s, roundTripState(t, s.ExportState()), StationQueueCompactV1)
			restored = true
		}
		if s.completed == 5 {
			break
		}
	}
	if !restored || s.completed != 5 {
		t.Fatal("compact-class cold queue did not discharge")
	}
}

func TestStationCompactPreparedRestore(t *testing.T) {
	t.Parallel()
	s := compactStateFixture(t)
	prepared, err := PrepareNetwork(s.network)
	if err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	for _, mode := range []StationQueueSpacing{StationQueueCompactV1, StationQueueOrdinary} {
		r, result, restoreErr := prepared.RestoreState(PreparedRestoreInput{Fleet: s.initial, State: state,
			StationQueueSpacing: mode, PlatoonLimit: s.platoonLimit})
		if restoreErr != nil || result.Tier != RestorePhysical || r.StationQueueSpacing() != mode || r.platoonLimit != s.platoonLimit {
			t.Fatalf("prepared compact restore: %+v %v", result, restoreErr)
		}
		if mode == StationQueueCompactV1 && !reflect.DeepEqual(state, r.ExportState()) {
			t.Fatal("prepared restore changed the physical proof")
		}
		if mode == StationQueueOrdinary && !r.compactGroups[0].recovering {
			t.Fatal("prepared ordinary policy discarded retained recovery")
		}
	}
}

// TestCheckCompactFieldsRefusalOrder pins the error that invalid compact
// fields get. checkCompactFields checks the queue spacing, the platoon
// limit, each pod in saved order, and then the followers. For each pod it
// checks the link, the head certificate, and then each member. Each case
// breaks two adjacent checks, and the earlier check gives the refusal.
func TestCheckCompactFieldsRefusalOrder(t *testing.T) {
	t.Parallel()
	const (
		spacing  = "invalid restored station queue spacing"
		limit    = "invalid restored platoon limit"
		link     = "invalid compact predecessor link fields"
		head     = "invalid compact head certificate fields"
		member   = "invalid compact member fields"
		follower = "compact follower has no head certificate"
	)
	compactLink := func(leader string) *SavedPlatoonLink {
		return &SavedPlatoonLink{Kind: "compact-buffer-v1", TerminalCell: new(0), Leader: leader, Lanes: 1}
	}
	compactQueue := func(members ...string) *SavedCompactQueue {
		n := len(members)
		return &SavedCompactQueue{Kind: "compact-buffer-v1", Phase: "compact", Lane: "lane", Members: members, Frontier: 1,
			StopCells: make([]int, n), Speeds: make([]float64, n), Targets: make([]float64, n), LandingSpeeds: make([]float64, n)}
	}
	base := func() RestoreStateInput {
		return RestoreStateInput{StationQueueSpacing: StationQueueCompactV1, PlatoonLimit: MaxPlatoonLimit, State: SavedState{Pods: []SavedPod{
			{ID: "a", CompactQueue: compactQueue("a", "b")},
			{ID: "b", Platoon: compactLink("a")},
		}}}
	}
	if err := checkCompactFields(base()); err != nil {
		t.Fatalf("base compact fields: %v", err)
	}
	pods := func(in *RestoreStateInput) []SavedPod { return in.State.Pods }
	for _, test := range []struct {
		name string
		edit func(*RestoreStateInput)
		want string
	}{
		{"spacing_before_limit", func(in *RestoreStateInput) { in.StationQueueSpacing, in.PlatoonLimit = "unknown", -1 }, spacing},
		{"limit_without_certificate", func(in *RestoreStateInput) {
			in.PlatoonLimit, in.State.Pods = -1, []SavedPod{{ID: "a"}}
		}, limit},
		{"limit_before_pods", func(in *RestoreStateInput) { in.PlatoonLimit, pods(in)[1].Platoon.Draining = -1, true }, limit},
		{"link_before_head", func(in *RestoreStateInput) {
			pods(in)[0].Platoon = &SavedPlatoonLink{Kind: "compact-buffer-v1"}
		}, link},
		{"head_before_member", func(in *RestoreStateInput) {
			q := pods(in)[0].CompactQueue
			q.Phase, q.Members[1] = "draining", ""
		}, head},
		{"member_before_next_pod", func(in *RestoreStateInput) {
			pods(in)[0].CompactQueue.Speeds[1] = math.NaN()
			pods(in)[1].Platoon.Draining = true
		}, member},
		{"saved_pod_order", func(in *RestoreStateInput) {
			p := pods(in)
			p[0].CompactQueue.Speeds[1] = math.NaN()
			p[1].Platoon.Draining = true
			p[0], p[1] = p[1], p[0]
		}, link},
		{"member_of_earlier_head", func(in *RestoreStateInput) {
			in.State.Pods = append(in.State.Pods, SavedPod{ID: "c", CompactQueue: compactQueue("c", "b")})
		}, member},
		{"member_before_follower", func(in *RestoreStateInput) {
			pods(in)[0].CompactQueue.StopCells[1] = -1
			in.State.Pods = append(in.State.Pods, SavedPod{ID: "d", Platoon: compactLink("x")})
		}, member},
		{"follower", func(in *RestoreStateInput) {
			in.State.Pods = append(in.State.Pods, SavedPod{ID: "d", Platoon: compactLink("x")})
		}, follower},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := base()
			test.edit(&in)
			if err := checkCompactFields(in); err == nil || err.Error() != test.want {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
