package sim

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

func remainingTestContext(t *testing.T, occupied bool) (couplingReservationInput, *couplingMotionContext) {
	t.Helper()
	in := couplingMotionFixture(t, occupied, false)
	// Completed private history belongs to its original cabin in both cases.
	v := &in.Members[0].Vehicle
	v.RiddenMeters = 123
	if occupied {
		v.Riders[0].SharingConsent = SharedConsent
		v.Boardings = []RiderBoarding{{BerthID: "origin-1", MetersAtBoarding: 112}}
	}
	v.Riders = append(v.Riders, Request{ID: 3, From: "origin", To: "front-goal", PodID: "front", PartySize: 1,
		SharingConsent: PrivateConsent, Service: OnDemandService, RequestedTick: 1, BoardedTick: 2, Completed: true})
	v.Boardings = append(v.Boardings, RiderBoarding{BerthID: "origin-1", MetersAtBoarding: 110})
	p, err := planCouplingReservation(in)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: p, Current: in, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	return in, c
}

func remainingTestSnapshot(t *testing.T, original couplingReservationInput, c *couplingMotionContext, state couplingMotionState) couplingRemainingInput {
	t.Helper()
	in := cloneCouplingInput(original)
	in.Tick = state.Tick
	in.Waiting = nil
	in.Owners = make(map[resource]resourceOwner)
	for _, d := range c.dependencies {
		in.Owners[d.Resource] = c.dependencyOwner(d, state)
	}
	for _, claim := range c.reservation.PreservedClaims {
		in.Owners[claim.Resource] = claim.Expected
	}
	members := c.membersAt(state)
	for i := range in.Members {
		m := &in.Members[i]
		m.Vehicle.Pod = members[i].Pod
		m.Vehicle.Pod.Speed = 0
		m.Vehicle.Riders, m.Vehicle.Stops, m.Vehicle.Boardings = members[i].Riders, members[i].Stops, members[i].Boardings
		m.Vehicle.RiddenMeters = members[i].RiddenMeters
		m.Distance, m.BlockIndex, m.ReservedThrough = state.Distances[i], state.Cells[i], c.through[i]
		m.Retained = nil
	}
	leg := state.Leg
	if state.Phase == couplingLatching {
		leg = 0
	}
	if state.Phase == couplingUnlatching {
		leg = 1
	}
	return couplingRemainingInput{Current: in, GroupID: "pair", FormationTick: c.formationTick,
		Phase: state.Phase, Leg: leg, DwellTicks: state.Dwell, DrainFirstMember: c.drainOrder[0]}
}

func remainingTestFind(t *testing.T, c *couplingMotionContext, phase couplingReservationPhase, leg int) couplingMotionState {
	t.Helper()
	for elapsed := uint64(1); elapsed < c.ticks; elapsed++ {
		state, err := c.stateAt(elapsed)
		if err != nil {
			t.Fatal(err)
		}
		if state.Phase == phase && ((leg >= 0 && state.Leg == leg && state.Cursor > c.legs[leg].ticks/2) || (leg == -1 && state.Dwell == 43)) {
			return state
		}
	}
	t.Fatalf("fixture has no interior phase=%d leg=%d", phase, leg)
	return couplingMotionState{}
}

func remainingTestRun(t *testing.T, c *couplingMotionContext, initial couplingMotionStep, owners map[resource]resourceOwner) couplingMotionStep {
	t.Helper()
	applyCouplingTestWrites(t, owners, initial.Writes)
	view, err := sealCouplingMotionOwners(c, initial.State, owners)
	if err != nil {
		t.Fatal(err)
	}
	step := initial
	for !step.State.Finished {
		old := step.State
		step, err = planCouplingMotion(couplingMotionInput{Context: c, Previous: old, Owners: view})
		if err != nil {
			t.Fatalf("remaining phase=%d leg=%d elapsed=%d: %v", old.Phase, old.Leg, old.Elapsed, err)
		}
		couplingIndependentBodyOracle(t, c, old, step)
		couplingIndependentLaneOracle(t, c, old, step.State)
		for i, member := range &step.Members {
			if !reflect.DeepEqual(member.Riders, initial.Members[i].Riders) || !reflect.DeepEqual(member.Boardings, initial.Members[i].Boardings) {
				t.Fatal("remaining motion changed cabin identity, consent, or history")
			}
			if step.State.Distances[i] != old.Distances[i]+step.State.Speeds[i]/60 || step.Samples[i].DistanceMeters != step.State.Distances[i]-old.Distances[i] {
				t.Fatal("remaining samples do not conserve strict Euler distance")
			}
		}
		applyCouplingTestWrites(t, owners, step.Writes)
		if len(step.Writes) > 0 {
			view, err = advanceCouplingMotionOwners(view, old, step.State, step.Writes, owners)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	return step
}

func TestCouplingRemainingEveryPhase(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		for _, phase := range []struct {
			name  string
			phase couplingReservationPhase
			leg   int
		}{{"closing", couplingClosing, 0}, {"latching", couplingLatching, -1}, {"connected", couplingConnected, 1},
			{"unlatching", couplingUnlatching, -1}, {"opening", couplingOpening, 2}, {"drain_first", couplingDraining, 3}, {"drain_second", couplingDraining, 4}} {
			t.Run(fmt.Sprintf("%s/occupied_%v", phase.name, occupied), func(t *testing.T) {
				t.Parallel()
				original, full := remainingTestContext(t, occupied)
				saved := remainingTestFind(t, full, phase.phase, phase.leg)
				input := remainingTestSnapshot(t, original, full, saved)
				before := cloneCouplingInput(input.Current)
				c, initial, err := prepareRemainingCouplingMotion(input)
				if err != nil {
					t.Fatal(err)
				}
				if !couplingInputsEqual(before, input.Current) || initial.State.Speeds != ([2]float64{}) || initial.State.Elapsed != 0 ||
					initial.State.Tick != saved.Tick || initial.State.Phase != saved.Phase || initial.State.Dwell != saved.Dwell ||
					initial.State.Distances != saved.Distances || c.formationTick != full.formationTick || c.drainOrder != full.drainOrder || len(c.reservation.waiting) != 0 {
					t.Fatal("resume changed saved facts, initial clock, or phase")
				}
				for leg := range c.firstLeg {
					if c.legs[leg].ticks != 0 {
						t.Fatal("resume prepared a completed motion leg")
					}
				}
				owners := maps.Clone(input.Current.Owners)
				final := remainingTestRun(t, c, initial, owners)
				if final.State.Distances != full.terminal {
					t.Fatal("resume changed derived terminal coordinates")
				}
				for i, member := range final.Members {
					want := original.Members[i].Vehicle.RiddenMeters
					if occupied {
						want += final.State.Distances[i] - original.Members[i].Distance
					}
					if math.Abs(member.RiddenMeters-want) > 1e-10 {
						t.Fatal("resume lost or rebased the cabin distance clock")
					}
				}
				for _, d := range c.dependencies {
					if owners[d.Resource] == c.owner {
						t.Fatal("completed remaining motion retained a group owner")
					}
				}
			})
		}
	}
}

func TestCouplingRemainingClearedForeignOwners(t *testing.T) {
	t.Parallel()
	original, full := remainingTestContext(t, true)
	saved := remainingTestFind(t, full, couplingConnected, 1)
	in := remainingTestSnapshot(t, original, full, saved)
	var cleared, unresolved resource
	foundCleared, foundUnresolved := false, false
	for _, d := range full.dependencies {
		if d.Resource.kind != trackResource {
			continue
		}
		if full.dependencyOwner(d, saved).isZero() {
			cleared, foundCleared = d.Resource, true
		} else {
			unresolved, foundUnresolved = d.Resource, true
		}
	}
	if !foundCleared || !foundUnresolved {
		t.Fatal("fixture lacks cleared and unresolved track dependencies")
	}
	foreign := podResourceOwner("foreign")
	in.Current.Owners[cleared] = foreign
	c, step, err := prepareRemainingCouplingMotion(in)
	if err != nil {
		t.Fatal(err)
	}
	actual := maps.Clone(in.Current.Owners)
	applyCouplingTestWrites(t, actual, step.Writes)
	if actual[cleared] != foreign {
		t.Fatal("resume overwrote a cleared foreign-owned resource")
	}
	if _, sealErr := sealCouplingMotionOwners(c, step.State, actual); sealErr != nil {
		t.Fatal(sealErr)
	}
	for _, event := range c.events {
		if event.write.Resource == cleared {
			t.Fatal("future remaining event resurrects a cleared dependency")
		}
	}
	in.Current.Owners[unresolved] = foreign
	before := cloneCouplingInput(in.Current)
	c, step, err = prepareRemainingCouplingMotion(in)
	if !errors.Is(err, errCouplingReservationDenied) || c != nil || step.State.context != nil || len(step.Writes) != 0 || !couplingInputsEqual(before, in.Current) {
		t.Fatalf("unresolved foreign conflict did not reject atomically: %v", err)
	}
}

func TestCouplingRemainingMalformed(t *testing.T) {
	t.Parallel()
	original, full := remainingTestContext(t, true)
	base := remainingTestSnapshot(t, original, full, remainingTestFind(t, full, couplingConnected, 1))
	for _, test := range []struct {
		name   string
		change func(*couplingRemainingInput)
	}{
		{"future_formation", func(in *couplingRemainingInput) { in.FormationTick = in.Current.Tick + 1 }},
		{"clock_overflow", func(in *couplingRemainingInput) { in.Current.Tick = math.MaxInt64 }},
		{"negative_formation", func(in *couplingRemainingInput) { in.FormationTick = -1 }},
		{"phase", func(in *couplingRemainingInput) { in.Phase = 99 }},
		{"leg", func(in *couplingRemainingInput) { in.Leg = 3 }},
		{"drain_order", func(in *couplingRemainingInput) { in.DrainFirstMember = 2 }},
		{"unsafe_drain_order", func(in *couplingRemainingInput) { in.DrainFirstMember = 1 - in.DrainFirstMember }},
		{"dwell", func(in *couplingRemainingInput) { in.DwellTicks = 1 }},
		{"negative_dwell", func(in *couplingRemainingInput) { in.DwellTicks = -1 }},
		{"nonfinite_distance", func(in *couplingRemainingInput) { in.Current.Members[1].Distance = math.NaN() }},
		{"reversed", func(in *couplingRemainingInput) {
			in.Current.Members[0], in.Current.Members[1] = in.Current.Members[1], in.Current.Members[0]
		}},
		{"duplicate", func(in *couplingRemainingInput) { in.Current.Members[1] = in.Current.Members[0] }},
		{"wrong_class", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Pod.Class = GroupClass }},
		{"moving", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Pod.Speed = 1 }},
		{"lane_pose", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Pod.LaneDistance++ }},
		{"body_pose", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Pod.Position.X++ }},
		{"nonfinite_lane_pose", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Pod.LaneDistance = math.Inf(1) }},
		{"nonfinite_pose", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Pod.Position.X = math.NaN() }},
		{"cell", func(in *couplingRemainingInput) { in.Current.Members[1].BlockIndex++ }},
		{"route", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Route = nil }},
		{"route_source", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Route[0].SpeedLimit-- }},
		{"corridor", func(in *couplingRemainingInput) { in.Current.CorridorID = "unknown" }},
		{"waiting_binding", func(in *couplingRemainingInput) { in.Current.Waiting = []Request{{PodID: "rear"}} }},
		{"foreign_member", func(in *couplingRemainingInput) { in.ForeignIDs = []string{"rear"} }},
		{"unknown_consent", func(in *couplingRemainingInput) { in.Current.Members[0].Vehicle.Riders[0].SharingConsent = "unknown" }},
		{"unknown_completed_consent", func(in *couplingRemainingInput) { in.Current.Members[0].Vehicle.Riders[1].SharingConsent = "unknown" }},
		{"history_alignment", func(in *couplingRemainingInput) {
			in.Current.Members[0].Vehicle.Boardings = in.Current.Members[0].Vehicle.Boardings[:1]
		}},
		{"negative_history_baseline", func(in *couplingRemainingInput) { in.Current.Members[0].Vehicle.Boardings[1].MetersAtBoarding = -1 }},
		{"nonfinite_history_baseline", func(in *couplingRemainingInput) {
			in.Current.Members[0].Vehicle.Boardings[1].MetersAtBoarding = math.NaN()
		}},
		{"nonfinite_clock", func(in *couplingRemainingInput) { in.Current.Members[0].Vehicle.RiddenMeters = math.Inf(1) }},
		{"party_capacity", func(in *couplingRemainingInput) { in.Current.Members[1].Vehicle.Riders[0].PartySize = 5 }},
		{"cross_cabin_party", func(in *couplingRemainingInput) {
			in.Current.Members[1].Vehicle.Riders[0].ID = in.Current.Members[0].Vehicle.Riders[0].ID
		}},
		{"partial_receiving", func(in *couplingRemainingInput) {
			delete(in.Current.Owners, berthResources(in.Current.Members[1].Destination)[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := base
			in.Current = cloneCouplingInput(base.Current)
			test.change(&in)
			owners := maps.Clone(in.Current.Owners)
			c, out, err := prepareRemainingCouplingMotion(in)
			if err == nil || c != nil || out.State.context != nil || len(out.Writes) != 0 || !maps.Equal(owners, in.Current.Owners) {
				t.Fatalf("malformed remaining input accepted or mutated ledger: %v", err)
			}
		})
	}
}

func TestCouplingRemainingIgnoresUnrelatedWaiting(t *testing.T) {
	t.Parallel()
	original, full := remainingTestContext(t, false)
	in := remainingTestSnapshot(t, original, full, remainingTestFind(t, full, couplingClosing, 0))
	in.Current.Waiting = []Request{{ID: 100, PodID: "other"}}
	c, _, err := prepareRemainingCouplingMotion(in)
	if err != nil || len(c.reservation.waiting) != 0 {
		t.Fatalf("unrelated pending queue was retained or rejected: %v", err)
	}
	p, err := planCouplingReservation(original)
	if err != nil {
		t.Fatal(err)
	}
	formed, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: p, Current: original, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	original.Waiting = in.Current.Waiting
	if _, err := initialCouplingMotion(formed, original); err != nil || len(formed.reservation.waiting) != 0 {
		t.Fatalf("unrelated pending change invalidated member commitment: %v", err)
	}
}

func TestCouplingRemainingEndpoints(t *testing.T) {
	t.Parallel()
	original, full := remainingTestContext(t, true)
	for _, test := range []struct {
		name               string
		phase              couplingReservationPhase
		leg                int
		distances          [2]float64
		wantPhase          couplingReservationPhase
		wantLeg, wantDwell int
	}{
		{"closing_end", couplingClosing, 0, full.reservation.ClosingStops, couplingLatching, -1, 120},
		{"latch_zero", couplingLatching, 0, full.reservation.ClosingStops, couplingConnected, 1, 0},
		{"connected_end", couplingConnected, 1, full.reservation.SplitStops, couplingUnlatching, -1, 120},
		{"unlatch_zero", couplingUnlatching, 1, full.reservation.SplitStops, couplingOpening, 2, 0},
		{"opening_end", couplingOpening, 2, full.reservation.OpeningStops, couplingDraining, 3, 0},
		{"first_drain_end", couplingDraining, 3, full.legs[3].end, couplingDraining, 4, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := couplingMotionState{Tick: 20000, Phase: test.phase, Leg: test.leg, Distances: test.distances}
			state, err := full.poseState(state)
			if err != nil {
				t.Fatal(err)
			}
			in := remainingTestSnapshot(t, original, full, state)
			in.Leg = test.leg
			c, step, err := prepareRemainingCouplingMotion(in)
			if err != nil {
				t.Fatal(err)
			}
			if step.State.Phase != test.wantPhase || step.State.Leg != test.wantLeg || step.State.Dwell != test.wantDwell || step.State.Distances != test.distances {
				t.Fatalf("noncanonical endpoint: %+v", step.State)
			}
			if c.firstLeg < test.leg || step.State.Speeds != ([2]float64{}) {
				t.Fatal("endpoint replayed a completed leg or retained speed")
			}
		})
	}
	terminal, err := full.stateAt(full.ticks)
	if err != nil {
		t.Fatal(err)
	}
	if c, step, err := prepareRemainingCouplingMotion(remainingTestSnapshot(t, original, full, terminal)); !errors.Is(err, errCouplingReservationDenied) || c != nil || step.State.context != nil {
		t.Fatal("terminal group accepted for serialization")
	}
}

func TestCouplingRemainingDwellBounds(t *testing.T) {
	t.Parallel()
	original, full := remainingTestContext(t, false)
	for _, phase := range []couplingReservationPhase{couplingLatching, couplingUnlatching} {
		for _, dwell := range []int{-1, 121} {
			t.Run(fmt.Sprintf("phase_%d/dwell_%d", phase, dwell), func(t *testing.T) {
				t.Parallel()
				in := remainingTestSnapshot(t, original, full, remainingTestFind(t, full, phase, -1))
				in.DwellTicks = dwell
				before := cloneCouplingInput(in.Current)
				c, step, err := prepareRemainingCouplingMotion(in)
				if !errors.Is(err, errCouplingReservationDenied) || c != nil || step.State.context != nil || !couplingInputsEqual(before, in.Current) {
					t.Fatalf("invalid dwell accepted: %v", err)
				}
			})
		}
	}
}

func TestCouplingRemainingRepeatedResumeAndOwnedInputs(t *testing.T) {
	t.Parallel()
	original, full := remainingTestContext(t, true)
	context := full
	for _, phase := range []struct {
		phase couplingReservationPhase
		leg   int
	}{{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1}, {couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4}} {
		state := remainingTestFind(t, context, phase.phase, phase.leg)
		input := remainingTestSnapshot(t, original, context, state)
		c, initial, err := prepareRemainingCouplingMotion(input)
		if err != nil {
			t.Fatal(err)
		}
		if c.formationTick != full.formationTick || c.drainOrder != full.drainOrder || initial.State.Distances != state.Distances || initial.State.Tick != state.Tick {
			t.Fatal("repeated resume changed formation, drain order, position, or time")
		}
		for i, member := range initial.Members {
			want := original.Members[i].Vehicle.RiddenMeters + state.Distances[i] - original.Members[i].Distance
			if math.Abs(member.RiddenMeters-want) > 1e-10 || !reflect.DeepEqual(member.Riders, original.Members[i].Vehicle.Riders) || !reflect.DeepEqual(member.Boardings, original.Members[i].Vehicle.Boardings) {
				t.Fatal("repeated resume rebased distance or changed whole-party history")
			}
		}
		owners := maps.Clone(input.Current.Owners)
		applyCouplingTestWrites(t, owners, initial.Writes)
		view, err := sealCouplingMotionOwners(c, initial.State, owners)
		if err != nil {
			t.Fatal(err)
		}
		control, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: initial.State, Owners: view})
		if err != nil {
			t.Fatal(err)
		}
		input.Current.Members[0].Vehicle.Route[0].SpeedLimit = .1
		input.Current.Members[0].Vehicle.Riders[0].PartySize = 4
		input.Current.Members[0].Vehicle.Boardings[0].MetersAtBoarding = 0
		input.Current.Owners = nil
		initial.Members[0].Riders[0].SharingConsent = PrivateConsent
		initial.Members[0].Boardings[0].MetersAtBoarding = 0
		again, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: initial.State, Owners: view})
		if err != nil || !couplingMotionOutputsEqual(control, again) {
			t.Fatal("remaining context aliases its mutable input or output arrays")
		}
		context = c
	}
	// Finish the last reconstructed context from its initial saved state.
	initialState, err := context.stateAt(0)
	if err != nil {
		t.Fatal(err)
	}
	input := remainingTestSnapshot(t, original, context, initialState)
	c, initial, err := prepareRemainingCouplingMotion(input)
	if err != nil {
		t.Fatal(err)
	}
	final := remainingTestRun(t, c, initial, maps.Clone(input.Current.Owners))
	if final.State.Distances != full.terminal {
		t.Fatal("repeated resumes changed final physical positions")
	}
}

// remainingSpeedContext plans the motion fixture with the given lane speed
// limits.
func remainingSpeedContext(t *testing.T, speeds map[string]float64) (couplingReservationInput, *couplingMotionContext) {
	t.Helper()
	input := couplingMotionFixture(t, false, false)
	network := input.Prepared.Network()
	for i, lane := range network.Lanes {
		if speed, ok := speeds[lane.ID]; ok {
			network.Lanes[i].SpeedLimit = speed
		}
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	corridor := input.Network.corridors[input.CorridorID]
	geometry := CouplingGeometryInput{Contract: CompactPairV1CouplingContract, Network: prepared.Network(), Sites: []CouplingSite{input.Network.sites[corridor.AssemblySiteID], input.Network.sites[corridor.SplitSiteID]}, Corridors: []CouplingCorridor{corridor}}
	n, err := prepareCouplingReservations(prepared, geometry)
	if err != nil {
		t.Fatal(err)
	}
	input.Network, input.Prepared = n, prepared
	for i := range input.Members {
		for j, lane := range input.Members[i].Vehicle.Route {
			input.Members[i].Vehicle.Route[j] = n.lanes[lane.ID].lane
		}
	}
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	return input, c
}

// A resumed leg keeps the speed cap of the lane where the leg started. The
// assembly lane is slower than the split lane, and the exit lanes are much
// faster than both. Each save is past the lane where its leg started: the
// connected leg on the split lane, and each drain leg on an exit lane. The
// resume restarts the members from rest, so the resumed leg has the cap of
// the uninterrupted leg, and each later tick equals the uninterrupted run.
func TestCouplingRemainingKeepsStartLaneCap(t *testing.T) {
	t.Parallel()
	original, full := remainingSpeedContext(t, map[string]float64{"ab": 3, "front-road": 720, "rear-road": 720, "front-in": 720, "rear-in": 720})
	for _, tc := range []struct {
		name      string
		phase     couplingReservationPhase
		leg       int
		startLane string
	}{{"connected", couplingConnected, 1, "ab"}, {"drain_first", couplingDraining, 3, "bc"}, {"drain_second", couplingDraining, 4, "bc"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			moving := full.legs[tc.leg].moving
			past := func(state couplingMotionState) bool {
				for i, blocks := range full.reservation.routes {
					lane := slices.IndexFunc(blocks.route, func(lane Lane) bool { return lane.ID == tc.startLane })
					if moving[i] && state.Distances[i] <= blocks.lanes[lane+1].start {
						return false
					}
				}
				return true
			}
			var saved couplingMotionState
			for elapsed := uint64(1); elapsed < full.ticks; elapsed++ {
				state, err := full.stateAt(elapsed)
				if err != nil {
					t.Fatal(err)
				}
				if state.Phase == tc.phase && state.Leg == tc.leg && past(state) {
					saved = state
					break
				}
			}
			if saved.Elapsed == 0 {
				t.Fatal("fixture has no save past the start lane of the leg")
			}
			input := remainingTestSnapshot(t, original, full, saved)
			c, initial, err := prepareRemainingCouplingMotion(input)
			if err != nil {
				t.Fatal("resume past the start lane failed", err)
			}
			if c.legs[tc.leg].cap != full.legs[tc.leg].cap {
				t.Fatalf("resumed leg cap %g differs from the uninterrupted cap %g", c.legs[tc.leg].cap, full.legs[tc.leg].cap)
			}
			for leg := tc.leg + 1; leg < len(c.legs); leg++ {
				if !reflect.DeepEqual(c.legs[leg], full.legs[leg]) {
					t.Fatalf("resume changed later leg %d", leg)
				}
			}
			resumed := uint64(0)
			for ; resumed < c.ticks; resumed++ {
				state, err := c.stateAt(resumed)
				if err != nil {
					t.Fatal(err)
				}
				if state.Phase != saved.Phase || state.Leg != saved.Leg {
					break
				}
				if max(state.Speeds[0], state.Speeds[1]) > full.legs[tc.leg].cap {
					t.Fatalf("resumed speeds %v exceed the start lane cap %g", state.Speeds, full.legs[tc.leg].cap)
				}
			}
			later := saved.Elapsed
			for ; later < full.ticks; later++ {
				state, err := full.stateAt(later)
				if err != nil {
					t.Fatal(err)
				}
				if state.Phase != saved.Phase || state.Leg != saved.Leg {
					break
				}
			}
			if c.ticks-resumed != full.ticks-later {
				t.Fatal("resume changed the duration after the resumed leg")
			}
			for ; resumed <= c.ticks; resumed, later = resumed+1, later+1 {
				a, err := c.stateAt(resumed)
				if err != nil {
					t.Fatal(err)
				}
				b, err := full.stateAt(later)
				if err != nil {
					t.Fatal(err)
				}
				if a.Phase != b.Phase || a.Leg != b.Leg || a.Speeds != b.Speeds || a.Distances != b.Distances {
					t.Fatalf("resumed tick %d differs from uninterrupted tick %d", resumed, later)
				}
			}
			if final := remainingTestRun(t, c, initial, maps.Clone(input.Current.Owners)); final.State.Distances != full.terminal {
				t.Fatal("resume changed derived terminal coordinates")
			}
		})
	}
}
