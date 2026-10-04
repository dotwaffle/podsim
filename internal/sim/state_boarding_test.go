package sim

import (
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// recordedRestoreFixture describes native records, without claiming production reachability.
func recordedRestoreFixture(t *testing.T) (restoreFixture, SavedState) {
	t.Helper()
	f := newRestoreFleetFixture(t, Example(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}})
	pod := f.boarding(t, "01", "garden-1")
	pod.Occupied = true
	pod.Riders[0].From = "harbor"
	pod.Riders = append(pod.Riders, SavedRequest{ID: 2, From: "garden", To: "market", PartySize: 1, PodID: "01", RequestedTick: 30, BoardedTick: 40, SharingConsent: SharedConsent, Service: OnDemandService})
	pod.Boardings = []RiderBoarding{{BerthID: "harbor-1"}, {BerthID: "garden-1", MetersAtBoarding: 50}}
	pod.RiddenMeters = 100
	state := f.state(pod)
	state.SharedRidePartyLimit = 4
	state.SharedRideMode = SharedRideDropOffs
	state.SharedRideMaxStops = DefaultSharedRideMaxStops
	return f, state
}

func boardingRestoreInput(f restoreFixture, state SavedState) RestoreStateInput {
	return RestoreStateInput{Network: f.network, Fleet: f.fleet, State: state, BoardingRecords: true}
}

func TestBoardingRecordsRejectBeforeFallback(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*RestoreStateInput)
	}{
		{"version gate", func(i *RestoreStateInput) { i.BoardingRecords = false }},
		{"empty array", func(i *RestoreStateInput) { i.State.Pods[0].Boardings = []RiderBoarding{} }},
		{"short array", func(i *RestoreStateInput) { i.State.Pods[0].Boardings = i.State.Pods[0].Boardings[:1] }},
		{"long array", func(i *RestoreStateInput) {
			i.State.Pods[0].Boardings = append(i.State.Pods[0].Boardings, make([]RiderBoarding, 7)...)
		}},
		{"unknown berth", func(i *RestoreStateInput) { i.State.Pods[0].Boardings[0].BerthID = "missing" }},
		{"wrong source station", func(i *RestoreStateInput) { i.State.Pods[0].Boardings[0].BerthID = "garden-1" }},
		{"parking origin", func(i *RestoreStateInput) {
			i.State.Pods[0].Riders[0].From = "parking"
			i.State.Pods[0].Boardings[0].BerthID = "parking-1"
		}},
		{"class restriction", func(i *RestoreStateInput) {
			for j := range i.Network.Stations {
				if i.Network.Stations[j].ID == "harbor" {
					i.Network.Stations[j].Berths[0].VehicleClasses = classBit(string(CompactClass))
				}
			}
		}},
		{"negative baseline", func(i *RestoreStateInput) { i.State.Pods[0].Boardings[0].MetersAtBoarding = -1 }},
		{"NaN baseline", func(i *RestoreStateInput) { i.State.Pods[0].Boardings[0].MetersAtBoarding = math.NaN() }},
		{"infinite baseline", func(i *RestoreStateInput) { i.State.Pods[0].Boardings[0].MetersAtBoarding = math.Inf(1) }},
		{"future baseline", func(i *RestoreStateInput) { i.State.Pods[0].Boardings[0].MetersAtBoarding = 101 }},
		{"infinite cumulative", func(i *RestoreStateInput) { i.State.Pods[0].RiddenMeters = math.Inf(1) }},
		{"negative cumulative", func(i *RestoreStateInput) { i.State.Pods[0].RiddenMeters = -1 }},
		{"journey origin", func(i *RestoreStateInput) { i.State.Pods[0].JourneyOrigin = "harbor-1" }},
		{"closed cohort", func(i *RestoreStateInput) { i.State.Pods[0].LegacyCohort = true }},
		{"unknown consent", func(i *RestoreStateInput) { i.State.Pods[0].Riders[0].SharingConsent = LegacyUnknownConsent }},
		{"private active party", func(i *RestoreStateInput) { i.State.Pods[0].Riders[0].SharingConsent = PrivateConsent }},
		{"single private active party", func(i *RestoreStateInput) {
			i.State.Pods[0].Riders[0].Completed = true
			i.State.Pods[0].Riders[1].SharingConsent = PrivateConsent
		}},
		{"unknown completed consent", func(i *RestoreStateInput) {
			i.State.Pods[0].Riders[0].Completed = true
			i.State.Pods[0].Riders[0].SharingConsent = LegacyUnknownConsent
		}},
		{"boarding dwell", func(i *RestoreStateInput) { i.State.Pods[0].PhaseTicks = boardingTicks + 1 }},
		{"missing occupied records", func(i *RestoreStateInput) { i.State.Pods[0].Boardings = nil }},
		{"unknown placement", func(i *RestoreStateInput) { i.State.Pods[0].BerthID = "missing"; i.State.Pods[0].Origin = "missing" }},
	}
	for _, test := range tests {
		for _, logical := range []bool{false, true} {
			t.Run(test.name+map[bool]string{false: "/physical", true: "/logical"}[logical], func(t *testing.T) {
				t.Parallel()
				f, state := recordedRestoreFixture(t)
				input := boardingRestoreInput(f, state)
				input.LogicalOnly = logical
				test.edit(&input)
				if s, _, err := restoreState(input, func() (*Simulation, error) {
					t.Fatal("invalid records reached a restore tier")
					return nil, nil
				}); err == nil || s != nil {
					t.Fatalf("invalid records restored: %+v, %v", s, err)
				}
			})
		}
	}
}

func TestBoardingRecordsPhysicalRestore(t *testing.T) {
	t.Parallel()
	for _, ticks := range []int{0, 73, boardingTicks} {
		for _, policy := range []bool{false, true} {
			t.Run(strconv.Itoa(ticks)+map[bool]string{false: "/off", true: "/on"}[policy], func(t *testing.T) {
				t.Parallel()
				f, state := recordedRestoreFixture(t)
				state.Pods[0].PhaseTicks = ticks
				input := boardingRestoreInput(f, state)
				input.OnboardPickups = policy
				s, result, err := RestoreState(input)
				if err != nil || result.Tier != RestorePhysical {
					t.Fatalf("restore: %+v %v", result, err)
				}
				v := &s.vehicles[0]
				if v.phaseTicks != ticks || v.Pod.Activity != Boarding || !v.Pod.Occupied || v.riddenBase != 100 || !slices.Equal(v.Boardings, state.Pods[0].Boardings) {
					t.Fatalf("lost accepted phase: %+v", v)
				}
				for _, claim := range berthResources(f.berth(t, "garden-1").berth) {
					if s.owners[claim] != podResourceOwner("01") {
						t.Fatalf("lost berth ownership: %+v", s.owners)
					}
				}
				state.Pods[0].Boardings[0].BerthID = "changed"
				if v.Boardings[0].BerthID != "harbor-1" {
					t.Fatal("restore aliases input records")
				}
			})
		}
	}
}

func TestBoardingRecordsCompletedDestinationRevisit(t *testing.T) {
	t.Parallel()
	f, state := recordedRestoreFixture(t)
	state.Pods[0].Riders[0].Completed = true
	state.Pods[0].Riders[0].SharingConsent = PrivateConsent
	state.Completed = 1
	if s, result, err := RestoreState(boardingRestoreInput(f, state)); err != nil || result.Tier != RestorePhysical || !s.vehicles[0].Riders[0].Completed {
		t.Fatalf("completed history restore: %+v %v", result, err)
	}
	old := state.Pods[0]
	old.Boardings = nil
	old.Occupied = false
	if err := state.checkPod(old); err == nil {
		t.Fatal("ordinary boarding accepted completed destination history")
	}
}

func TestBoardingRecordsLogicalRequeue(t *testing.T) {
	t.Parallel()
	f, state := recordedRestoreFixture(t)
	input := boardingRestoreInput(f, state)
	input.LogicalOnly = true
	s, result, err := RestoreState(input)
	if err != nil || result.Tier != RestoreLogical || !slices.Equal(result.Requeued, []int{1, 2}) {
		t.Fatalf("logical restore: %+v %v", result, err)
	}
	checkRecordedRequeue(t, state, s)
}

func checkRecordedRequeue(t *testing.T, state SavedState, s *Simulation) {
	t.Helper()
	if len(s.waiting) != 2 {
		t.Fatalf("queue: %+v", s.waiting)
	}
	for index, trip := range s.waiting {
		want := Request(state.Pods[0].Riders[index])
		want.PodID = ""
		if !trip.boarded || !reflect.DeepEqual(trip.request, want) {
			t.Fatalf("requeued party: %+v, want %+v", trip, want)
		}
	}
	for _, v := range s.vehicles {
		if len(v.Boardings) != 0 || len(v.Riders) != 0 || v.riddenBase != 0 {
			t.Fatalf("physical metadata retained: %+v", v)
		}
	}
}

func TestBoardingRecordsDemotionRequeues(t *testing.T) {
	t.Parallel()
	f, state := recordedRestoreFixture(t)
	pod := f.carrying(t, f.traveling(t, travelInput{id: "01", from: "harbor-1", to: "market-1", lane: "market-approach", distance: 1}), 1, 2)
	pod.Riders[1].From = "garden"
	pod.Boardings = slices.Clone(state.Pods[0].Boardings)
	pod.RiddenMeters = 100
	pod.Route = nil
	state.Pods[0] = pod
	s, result, err := RestoreState(boardingRestoreInput(f, state))
	if err != nil || result.Tier != RestorePhysical || !slices.Equal(result.Demoted, []string{"01"}) || !slices.Equal(result.Requeued, []int{1, 2}) {
		t.Fatalf("demotion: %+v %v", result, err)
	}
	checkRecordedRequeue(t, state, s)
}

func TestBoardingRecordsPhaseDistance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		phase   string
		history bool
	}{
		{name: "occupied travel", phase: "traveling"},
		{name: "continuing", phase: "continuing"},
		{name: "final unloading", phase: "unloading"},
		{name: "idle history", phase: "idle", history: true},
		{name: "departing history", phase: "departing", history: true},
		{name: "empty traveling history", phase: "traveling", history: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, state := recordedRestoreFixture(t)
			saved := state.Pods[0]
			pod := saved
			switch test.phase {
			case "traveling", "departing":
				pod = f.traveling(t, travelInput{id: "01", from: "garden-1", to: "market-1", lane: "market-approach", distance: 1})
				pod.Riders, pod.Boardings, pod.RiddenMeters = saved.Riders, saved.Boardings, saved.RiddenMeters
				if test.history {
					pod = relocating(pod)
				} else {
					pod.Occupied, pod.Stops = true, []string{"market"}
				}
				if test.phase == "departing" {
					pod.Activity, pod.StationID, pod.BerthID = "departing", "garden", "garden-1"
					pod.Distance, pod.LaneDistance, pod.LaneID, pod.RouteIndex = 0, 0, "", 0
				}
			case "continuing":
				pod.Activity, pod.PhaseTicks = "continuing", 0
			case "unloading":
				pod.Activity, pod.PhaseTicks = "unloading", 30
				pod.StationID, pod.BerthID, pod.Destination, pod.DestinationStation, pod.Stops = "market", "market-1", "market-1", "market", nil
			case "idle":
				pod = f.idle(t, "01", "garden-1")
				pod.Riders, pod.Boardings, pod.RiddenMeters = saved.Riders, saved.Boardings, saved.RiddenMeters
			}
			if test.history {
				for index := range pod.Riders {
					pod.Riders[index].Completed = true
				}
				state.Completed = 2
			}
			state.Pods[0] = pod
			s, result, err := RestoreState(boardingRestoreInput(f, state))
			if err != nil || result.Tier != RestorePhysical {
				t.Fatalf("phase restore: %+v %v", result, err)
			}
			v := &s.vehicles[0]
			if v.Pod.Activity != activityOfTestCode(t, test.phase) || v.riddenBase != pod.RiddenMeters || !slices.Equal(v.Boardings, pod.Boardings) {
				t.Fatalf("phase or distance changed: %+v", v)
			}
			if test.history && v.RidersAboard() != 0 {
				t.Fatal("history regained active riders")
			}
		})
	}
}

func activityOfTestCode(t *testing.T, code string) Activity {
	t.Helper()
	activity, ok := activityOfCode(code)
	if !ok {
		t.Fatalf("unknown test activity %q", code)
	}
	return activity
}

func TestBoardingRecordsDistanceBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                     string
		active                   bool
		base, distance, baseline float64
		valid                    bool
	}{
		{"occupied sum", true, 50, 40, 90, true},
		{"occupied past sum", true, 50, 40, 91, false},
		{"occupied negative distance", true, 50, -1, 0, false},
		{"occupied infinite distance", true, 50, math.Inf(1), 0, false},
		{"occupied NaN distance", true, 50, math.NaN(), 0, false},
		{"occupied overflow", true, math.MaxFloat64, math.MaxFloat64, 0, false},
		{"empty frozen sum", false, 50, 40, 50, true},
		{"empty ignores route distance", false, 50, 40, 51, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, state := recordedRestoreFixture(t)
			saved := state.Pods[0]
			pod := f.traveling(t, travelInput{id: "01", from: "garden-1", to: "market-1", lane: "market-approach", distance: 1})
			pod.Riders, pod.Boardings = saved.Riders, saved.Boardings
			pod.RiddenMeters, pod.Distance = test.base, test.distance
			pod.Boardings[1].MetersAtBoarding = test.baseline
			if test.active {
				pod.Occupied, pod.Stops = true, []string{"market"}
			} else {
				pod = relocating(pod)
				for index := range pod.Riders {
					pod.Riders[index].Completed = true
				}
				state.Completed = 2
			}
			state.Pods[0] = pod
			input := boardingRestoreInput(f, state)
			if err := checkBoardingFields(input); (err == nil) != test.valid {
				t.Fatalf("distance validation: %v, want valid %t", err, test.valid)
			}
			if !test.valid {
				for _, logical := range []bool{false, true} {
					input.LogicalOnly = logical
					if _, _, err := RestoreState(input); err == nil {
						t.Fatal("bad distance survived fallback")
					}
				}
			}
		})
	}
}

func TestBoardingRecordsLogicalCompletion(t *testing.T) {
	t.Parallel()
	f, state := recordedRestoreFixture(t)
	pod := &state.Pods[0]
	pod.Activity, pod.StationID, pod.BerthID, pod.Destination, pod.DestinationStation = "unloading", "market", "market-1", "market-1", "market"
	pod.PhaseTicks, pod.Stops = 30, []string{"garden"}
	pod.Riders[1].From, pod.Riders[1].To = "harbor", "garden"
	pod.Boardings[1].BerthID = "harbor-1"
	input := boardingRestoreInput(f, state)
	input.LogicalOnly = true
	s, result, err := RestoreState(input)
	if err != nil || !slices.Equal(result.LogicalCompleted, []int{1}) || !slices.Equal(result.Requeued, []int{2}) || s.completed != 1 {
		t.Fatalf("logical alighting: %+v %v", result, err)
	}
	if len(s.waiting) != 1 || s.waiting[0].request.ID != 2 || !s.waiting[0].boarded || len(s.vehicles[0].Boardings) != 0 {
		t.Fatalf("logical onward party: %+v", s.waiting)
	}
}

func TestBoardingRecordsPreparedRestore(t *testing.T) {
	t.Parallel()
	f, state := recordedRestoreFixture(t)
	prepared, err := PrepareNetwork(f.network)
	if err != nil {
		t.Fatal(err)
	}
	input := PreparedRestoreInput{Fleet: f.fleet, State: state, BoardingRecords: true, OnboardPickups: true}
	if _, result, err := prepared.RestoreState(input); err != nil || result.Tier != RestorePhysical {
		t.Fatalf("prepared restore: %+v %v", result, err)
	}
	input.BoardingRecords = false
	if _, _, err := prepared.RestoreState(input); err == nil {
		t.Fatal("prepared restore omitted the record eligibility gate")
	}
	input.BoardingRecords = true
	input.State.SharedRidePartyLimit = 1
	if _, _, err := prepared.RestoreState(input); err == nil {
		t.Fatal("prepared restore omitted the runtime policy gate")
	}
}
