package sim

import (
	"errors"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
)

// debrisFleet returns a simulation with faults on, on a line network of a
// parking station and the passenger stations s0 to s5. The lane "return"
// is 2,250 m long, with 75 cells of 30 m, and no berth is near it. Pod 01
// is idle at s0, and pod 02 at s5. The fault marker needs the incident
// marker, so the fleet has the incident contract.
func debrisFleet(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(lineNetwork(lineStations(1, 1, 1, 1, 1, 1, 1)), place("s0-1", "s5-1"))
	if err != nil {
		t.Fatal(err)
	}
	s.incidentContract = IncidentV1Contract
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	return s
}

// laneIndex returns the index of the lane with the ID.
func laneIndex(t *testing.T, s *Simulation, id string) int {
	t.Helper()
	index, ok := s.graph.lanes[id]
	if !ok {
		t.Fatalf("no lane %s", id)
	}
	return index
}

// startDebris starts debris on the lane with the ID and fails the test on
// an error.
func startDebris(t *testing.T, s *Simulation, lane string, from, to float64, duration int64) string {
	t.Helper()
	id, err := s.startDebris(laneIndex(t, s, lane), from, to, duration)
	if err != nil {
		t.Fatalf("debris on %s from %g to %g: %v", lane, from, to, err)
	}
	return id
}

// track returns the track resource of a cell of the lane.
func track(lane string, cell int) resource {
	return resource{kind: trackResource, id: lane, cell: cell}
}

// returnTraveler makes pod 02 travel from s5 to s0 with one rider, and
// steps until it cruises on the lane "return". Then its body is in cell 0
// of the lane, and its grants end at 60 m, the end of cell 1.
func returnTraveler(t *testing.T, s *Simulation) *vehicle {
	t.Helper()
	v := s.findVehicle("02")
	if err := s.board(v, newTrip(s, "s5", "s0")); err != nil {
		t.Fatal(err)
	}
	cruiseOn(t, s, v, "return")
	lane := v.blocks.locate(v.blockIndex, 0)
	if start := v.blocks.lanes[lane].start; v.distance-start > 1 || v.blocks.end(v.reservedThrough)-start != 60 {
		t.Fatalf("pod 02 at %g m on the lane with grants to %g m", v.distance-start, v.blocks.end(v.reservedThrough)-start)
	}
	return v
}

// checkOwners checks that the owners equal the retention rules, which
// give each debris its footprint.
func checkOwners(s *Simulation) error {
	if !maps.Equal(s.owners, s.retainedOwners()) {
		return errors.New("the resource owners differ from the retention rules")
	}
	return nil
}

// checkDebrisEachTick makes s check, after each tick and each public
// command, the state contract, with the blocked set, and the owners.
func checkDebrisEachTick(t *testing.T, s *Simulation) {
	t.Helper()
	s.monitor = func(s *Simulation) {
		for _, check := range []func(*Simulation) error{(*Simulation).CheckContract, checkOwners} {
			if err := check(s); err != nil {
				t.Fatalf("tick %d: %v", s.tick, err)
			}
		}
	}
}

// TestDebrisFootprint checks the cells of a debris footprint. The
// footprint holds each cell that shares a point with the segment widened
// by Clearance at each end. A footprint at a lane end holds the node, and
// it blocks the other lanes at the node.
func TestDebrisFootprint(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	returnLane := laneIndex(t, s, "return")
	for _, test := range []struct {
		name     string
		from, to float64
		cells    []int
	}{
		{"inside one cell", 615, 616, []int{20}},
		{"the clearance reaches the cell before", 601, 602, []int{19, 20}},
		{"the widened segment ends on cell bounds", 612, 618, []int{19, 20, 21}},
		{"segment on a cell bound", 600, 600.5, []int{19, 20}},
	} {
		var want []resource
		for _, cell := range test.cells {
			want = append(want, track("return", cell))
		}
		if got := s.debrisFootprint(returnLane, test.from, test.to); !slices.Equal(got, want) {
			t.Errorf("%s: footprint %v, want %v", test.name, got, want)
		}
	}
	checkDebrisEachTick(t, s)
	end := s.graph.lengths[returnLane]
	id := startDebris(t, s, "return", end-10, end, 0)
	footprint := s.debrisFootprint(returnLane, end-10, end)
	if !slices.Contains(footprint, resource{kind: nodeResource, id: "return-west"}) || !slices.Contains(footprint, track("return", 74)) {
		t.Fatalf("footprint at the lane end %v", footprint)
	}
	if got := blockedLaneIDs(s); !slices.Equal(got, []string{"return", "return-up"}) {
		t.Fatalf("blocked lanes %v, want the lane and the next lane at its node", got)
	}
	if s.blocked.by[resource{kind: nodeResource, id: "return-west"}] != id {
		t.Fatalf("the node is blocked by %q, want %s", s.blocked.by[resource{kind: nodeResource, id: "return-west"}], id)
	}
	// Debris at a station entry blocks the junction and each lane at it.
	startDebris(t, s, "s1-1-in", 0, 5, 0)
	for _, lane := range []string{"s1-1-in", "s1-through", "s0-link"} {
		if !laneBlocked(s, lane) {
			t.Errorf("lane %s is not blocked", lane)
		}
	}
}

// TestDebrisStart checks the record, the ID, the owners, the blocked set
// and the counter of debris on free track ahead of a traveling pod. The
// debris changes no claim of a pod.
func TestDebrisStart(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		duration int64
	}{{"no end", 0}, {"one second", 1}, {"longest", maxFaultSeconds}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := debrisFleet(t)
			v := returnTraveler(t, s)
			checkDebrisEachTick(t, s)
			s.SetIncidentGeneration(3)
			s.incidentSerial = 17
			owners := maps.Clone(s.owners)
			retained := maps.Clone(v.routeReleases)
			id := startDebris(t, s, "return", 300, 310, test.duration)
			want := faultRecord{generation: 3, serial: 18, kind: debrisFault, start: s.tick, lane: laneIndex(t, s, "return"), from: 300, to: 310}
			if test.duration > 0 {
				want.end = s.tick + test.duration*TicksPerSecond
			}
			if id != "i3.18" || !slices.Equal(s.faults, []faultRecord{want}) || s.faultCounters.started != 1 {
				t.Fatalf("ID %s, records %+v, started %d", id, s.faults, s.faultCounters.started)
			}
			owner := resourceOwner{kind: faultOwnerKind, id: id}
			for _, r := range []resource{track("return", 9), track("return", 10)} {
				if s.owners[r] != owner {
					t.Fatalf("resource %v has the owner %v", r, s.owners[r])
				}
				delete(s.owners, r)
			}
			if !maps.Equal(s.owners, owners) || !maps.Equal(v.routeReleases, retained) {
				t.Fatal("the debris changed a claim of a pod")
			}
			for _, r := range []resource{track("return", 9), track("return", 10)} {
				s.owners[r] = owner
			}
			if got := blockedLaneIDs(s); !slices.Equal(got, []string{"return"}) {
				t.Fatalf("blocked lanes %v", got)
			}
			if err := s.CheckContract(); err != nil {
				t.Fatal(err)
			}
			if err := checkOwners(s); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// debrisCall holds the arguments of a debris start.
type debrisCall struct {
	lane     int
	from, to float64
	duration int64
}

// fillDebris starts debris in the middle of each cell from 4 to 67 of the
// lane "return": 64 debris faults with disjoint footprints. It returns
// their IDs.
func fillDebris(s *Simulation) ([]string, error) {
	var ids []string
	for cell := 4; cell < 4+maxDebrisFaults; cell++ {
		middle := float64(cell)*30 + 15
		id, err := s.startDebris(s.graph.lanes["return"], middle-1, middle+1, 0)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// sameCursors reports whether the block cursors of each pod are the same
// in a and b. sameState leaves them out, because two equal runs can look
// up blocks in another order. A refused operation must not move them.
func sameCursors(a, b *Simulation) bool {
	if len(a.vehicles) != len(b.vehicles) {
		return false
	}
	for index := range a.vehicles {
		x, y := &a.vehicles[index].blocks, &b.vehicles[index].blocks
		if x.cursors != y.cursors || x.scan != y.scan {
			return false
		}
	}
	return true
}

// TestDebrisRefusals checks each precondition of a debris start at its
// limit and one step past it, against the refusal oracle: the state after
// a refused call equals a clone from before it, also in its block
// cursors. Then the case removes its cause, and the call succeeds. In the
// fixture, pod 02 cruises at the start of the lane "return", with its body
// in cell 0 and its grants to the end of cell 1, and the lane "s3-link" is
// free.
func TestDebrisRefusals(t *testing.T) {
	t.Parallel()
	limit := int64(math.MaxInt64)
	// on returns a call on the lane with the ID.
	on := func(s *Simulation, lane string, from, to float64) debrisCall {
		return debrisCall{lane: s.graph.lanes[lane], from: from, to: to}
	}
	free := func(s *Simulation) debrisCall { return on(s, "s3-link", 70, 80) }
	tests := []struct {
		name string
		want error
		// cause sets up the refusal and returns the call. undo removes the
		// cause and can change the call. A nil undo means no control.
		cause func(s *Simulation, v *vehicle) debrisCall
		undo  func(s *Simulation, v *vehicle, call *debrisCall)
	}{
		{"faults off", errFaultsOff,
			func(s *Simulation, _ *vehicle) debrisCall { s.faultsOn = false; return free(s) },
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.faultsOn = true }},
		{"negative duration", errFaultDuration,
			func(s *Simulation, _ *vehicle) debrisCall { call := free(s); call.duration = -1; return call },
			func(_ *Simulation, _ *vehicle, call *debrisCall) { call.duration = 0 }},
		{"duration above the limit", errFaultDuration,
			func(s *Simulation, _ *vehicle) debrisCall {
				call := free(s)
				call.duration = maxFaultSeconds + 1
				return call
			},
			func(_ *Simulation, _ *vehicle, call *debrisCall) { call.duration = maxFaultSeconds }},
		{"end tick past the limit", errIncidentLimit,
			func(s *Simulation, _ *vehicle) debrisCall {
				s.tick = limit - maxFaultSeconds*TicksPerSecond + 1
				call := free(s)
				call.duration = maxFaultSeconds
				return call
			},
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.tick-- }},
		{"evacuation tick past the limit", errIncidentLimit,
			func(s *Simulation, _ *vehicle) debrisCall {
				s.faultSettings.evacuationSeconds = 3600
				s.tick = limit - 3600*TicksPerSecond + 1
				return free(s)
			},
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.tick-- }},
		{"serial at the limit", errIncidentLimit,
			func(s *Simulation, _ *vehicle) debrisCall { s.incidentSerial = math.MaxUint64; return free(s) },
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.incidentSerial-- }},
		{"negative lane", errUnknownLane,
			func(s *Simulation, _ *vehicle) debrisCall { call := free(s); call.lane = -1; return call }, nil},
		{"lane past the network", errUnknownLane,
			func(s *Simulation, _ *vehicle) debrisCall {
				call := free(s)
				call.lane = len(s.network.Lanes)
				return call
			}, nil},
		{"start not a number", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "s3-link", math.NaN(), 80) }, nil},
		{"end not a number", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "s3-link", 70, math.NaN()) }, nil},
		{"infinite end", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "s3-link", 70, math.Inf(1)) }, nil},
		{"negative start", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "s3-link", math.Nextafter(0, -1), 10) },
			func(_ *Simulation, _ *vehicle, call *debrisCall) { call.from = 0 }},
		{"empty segment", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "s3-link", 70, 70) },
			func(_ *Simulation, _ *vehicle, call *debrisCall) { call.to = math.Nextafter(70, 71) }},
		{"end past the lane", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall {
				length := s.graph.lengths[s.graph.lanes["s3-link"]]
				return on(s, "s3-link", length-10, math.Nextafter(length, length+1))
			},
			func(s *Simulation, _ *vehicle, call *debrisCall) { call.to = s.graph.lengths[call.lane] }},
		{"longer than the limit", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "s3-link", 50, math.Nextafter(100, 101)) },
			func(_ *Simulation, _ *vehicle, call *debrisCall) { call.to = 100 }},
		{"berth in the footprint", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall {
				length := s.graph.lengths[s.graph.lanes["s3-1-in"]]
				return on(s, "s3-1-in", length-10, length)
			},
			func(_ *Simulation, _ *vehicle, call *debrisCall) { call.from, call.to = 0, 5 }},
		{"berth node in the footprint", errDebrisSegment,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "s3-1-out", 0, 5) },
			func(s *Simulation, _ *vehicle, call *debrisCall) {
				length := s.graph.lengths[call.lane]
				call.from, call.to = length-10, length
			}},
		{"debris limit", errDebrisLimit,
			func(s *Simulation, _ *vehicle) debrisCall {
				if _, err := fillDebris(s); err != nil {
					panic(err)
				}
				return free(s)
			},
			func(s *Simulation, _ *vehicle, _ *debrisCall) {
				if err := s.clearFault(s.faults[len(s.faults)-1].id()); err != nil {
					panic(err)
				}
			}},
		{"another debris", errDebrisOverlap,
			func(s *Simulation, _ *vehicle) debrisCall {
				call := free(s)
				if _, err := s.startDebris(call.lane, call.from+5, call.to+5, 0); err != nil {
					panic(err)
				}
				return call
			},
			func(s *Simulation, _ *vehicle, _ *debrisCall) {
				if err := s.clearFault(s.faults[0].id()); err != nil {
					panic(err)
				}
			}},
		{"pod fault footprint ahead of the body", errDebrisOverlap,
			func(s *Simulation, v *vehicle) debrisCall {
				if _, err := s.startPodFault(v, 0); err != nil {
					panic(err)
				}
				// The rebuild of the fault start left the scan cursor at
				// the first lane. A lookup on the current lane moves it
				// there, so a footprint walk from the first lane would
				// move it back.
				v.blocks.routeLane(v.blockIndex)
				if v.blocks.scan == 0 {
					panic("pod 02 is on the first lane of its route")
				}
				return on(s, "return", 45, 46)
			},
			func(s *Simulation, _ *vehicle, call *debrisCall) { *call = free(s) }},
		{"under the body of a pod", errDebrisOverlap,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "return", 0, 1) },
			func(s *Simulation, _ *vehicle, call *debrisCall) { *call = free(s) }},
		{"stopping grant of a pod", errDebrisClaim,
			func(s *Simulation, _ *vehicle) debrisCall { return on(s, "return", 72, 76) },
			// The widened segment no longer meets the end of the grants.
			func(_ *Simulation, _ *vehicle, call *debrisCall) { call.from = math.Nextafter(72, 73) }},
		{"retained resource with no owner", errDebrisClaim,
			func(s *Simulation, v *vehicle) debrisCall {
				if !v.retains(track("return", 1)) {
					panic("pod 02 does not retain cell 1")
				}
				delete(s.owners, track("return", 1))
				return on(s, "return", 45, 46)
			},
			func(_ *Simulation, v *vehicle, _ *debrisCall) { delete(v.routeReleases, track("return", 1)) }},
		{"coupling group claim", errFaultTarget,
			func(s *Simulation, _ *vehicle) debrisCall {
				claims := []couplingClaim{{Resource: track("s3-link", 2)}}
				s.couplingGroups = []couplingNativeGroup{{context: &couplingMotionContext{claims: claims}}}
				return free(s)
			},
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.couplingGroups = nil }},
		{"preserved claim", errFaultTarget,
			func(s *Simulation, _ *vehicle) debrisCall {
				c := &couplingMotionContext{}
				c.reservation.PreservedClaims = []couplingClaim{{Resource: track("s3-link", 2)}}
				s.couplingGroups = []couplingNativeGroup{{context: c}}
				return free(s)
			},
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.couplingGroups = nil }},
		{"coupling member route past its grants", errFaultTarget,
			func(s *Simulation, v *vehicle) debrisCall { v.couplingID = "pair"; return on(s, "return-up", 200, 210) },
			func(_ *Simulation, v *vehicle, _ *debrisCall) { v.couplingID = "" }},
		{"approach member route past its grants", errFaultTarget,
			func(s *Simulation, v *vehicle) debrisCall {
				c := &couplingApproachContext{}
				c.members[1].id = v.Pod.ID
				s.couplingApproaches = append(s.couplingApproaches, couplingNativeApproach{context: c})
				return on(s, "return-up", 200, 210)
			},
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.couplingApproaches = nil }},
		{"dispatch pass", errFaultDispatch,
			func(s *Simulation, _ *vehicle) debrisCall { s.pass.active = true; return free(s) },
			func(s *Simulation, _ *vehicle, _ *debrisCall) { s.pass.active = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := debrisFleet(t)
			v := returnTraveler(t, s)
			if s.pass == nil {
				s.pass = new(dispatchPass)
			}
			call := test.cause(s, v)
			before := s.Clone()
			if _, err := s.startDebris(call.lane, call.from, call.to, call.duration); !errors.Is(err, test.want) {
				t.Fatalf("error %v, want %v", err, test.want)
			}
			if !sameState(before, s) || !sameCursors(before, s) {
				t.Fatal("the refused debris changed the state")
			}
			if test.undo == nil {
				return
			}
			// Control: the call succeeds once the cause is gone.
			test.undo(s, v, &call)
			if _, err := s.startDebris(call.lane, call.from, call.to, call.duration); err != nil {
				t.Fatalf("control: %v", err)
			}
			if err := s.CheckContract(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestDebrisOnApproachCorridor forms a native approach, as the coupling
// approach tests do, and starts debris on the free corridor of its route
// past the grants of its members: in the lane of the members, and in the
// next corridor lane. Stage 2 refuses debris there, with the refusal
// oracle. Without the approach, the same debris starts, so the segment is
// free and no other precondition refuses it.
func TestDebrisOnApproachCorridor(t *testing.T) {
	t.Parallel()
	for _, segment := range []struct {
		lane     string
		from, to float64
	}{{"ab", 150, 160}, {"bc", 100, 110}} {
		t.Run(segment.lane, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, false)
			c, state, err := prepareCouplingApproach(input)
			if err != nil {
				t.Fatal(err)
			}
			s := input.Simulation
			s.couplingApproaches = []couplingNativeApproach{{context: c, state: state}}
			if err := s.SetFaults(true, FaultSettings{}); err != nil {
				t.Fatal(err)
			}
			corridor := input.Network.corridors[input.CorridorID]
			if !slices.Contains(corridor.LaneIDs, segment.lane) {
				t.Fatalf("lane %s is not in the corridor %v", segment.lane, corridor.LaneIDs)
			}
			lane := laneIndex(t, s, segment.lane)
			for _, id := range []string{"front", "rear"} {
				v := s.findVehicle(id)
				if !s.couplingApproachMember(id) || v.couplingID != "" {
					t.Fatalf("pod %s is not an approach member", id)
				}
				if end := v.blocks.end(v.reservedThrough); s.graph.lanes[v.Route[0].ID] == lane && end >= segment.from-Clearance {
					t.Fatalf("the grants of pod %s reach %g m", id, end)
				}
			}
			before := s.Clone()
			if _, err := s.startDebris(lane, segment.from, segment.to, 0); !errors.Is(err, errFaultTarget) {
				t.Fatalf("error %v, want %v", err, errFaultTarget)
			}
			if !sameState(before, s) || !sameCursors(before, s) {
				t.Fatal("the refused debris changed the state")
			}
			s.couplingApproaches = nil
			if _, err := s.startDebris(lane, segment.from, segment.to, 0); err != nil {
				t.Fatalf("control without the approach: %v", err)
			}
		})
	}
}

// TestDebrisPreconditionOrder checks the order of the preconditions. The
// call starts with each precondition false, and the test makes them true
// one at a time. Each call refuses with the error of the first false one.
func TestDebrisPreconditionOrder(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	v := returnTraveler(t, s)
	ids, err := fillDebris(s)
	if err != nil {
		t.Fatal(err)
	}
	if s.pass == nil {
		s.pass = new(dispatchPass)
	}
	serial := s.incidentSerial
	s.faultsOn, s.incidentSerial, v.couplingID, s.pass.active = false, math.MaxUint64, "pair", true
	call := debrisCall{lane: -1, duration: -1}
	for _, step := range []struct {
		want error
		fix  func()
	}{
		{errFaultsOff, func() { s.faultsOn = true }},
		{errFaultDuration, func() { call.duration = 0 }},
		{errIncidentLimit, func() { s.incidentSerial = serial }},
		{errUnknownLane, func() { call.lane = laneIndex(t, s, "return") }},
		// The segment of the first debris.
		{errDebrisSegment, func() { call.from, call.to = 4*30+14, 4*30+16 }},
		{errDebrisLimit, func() {
			if err := s.clearFault(ids[len(ids)-1]); err != nil {
				t.Fatal(err)
			}
		}},
		// A segment in the grants of pod 02.
		{errDebrisOverlap, func() { call.from, call.to = 45, 46 }},
		// A segment on the route of pod 02 past its grants.
		{errDebrisClaim, func() { call.lane, call.from, call.to = laneIndex(t, s, "return-up"), 200, 210 }},
		{errFaultTarget, func() { v.couplingID = "" }},
		{errFaultDispatch, func() { s.pass.active = false }},
	} {
		if _, err := s.startDebris(call.lane, call.from, call.to, call.duration); !errors.Is(err, step.want) {
			t.Fatalf("error %v, want %v", err, step.want)
		}
		step.fix()
	}
	if _, err := s.startDebris(call.lane, call.from, call.to, call.duration); err != nil {
		t.Fatal(err)
	}
}

// waitAtDebris starts debris on the lane "return" ahead of pod 02, and
// steps until the pod waits at rest for the debris. It returns the fault
// ID and the last block of the grants of the pod.
func waitAtDebris(t *testing.T, s *Simulation, duration int64) (*vehicle, string, int) {
	t.Helper()
	v := returnTraveler(t, s)
	checkDebrisEachTick(t, s)
	id := startDebris(t, s, "return", 300, 310, duration)
	stepUntil(t, s, "pod 02 waits for the debris", func() bool { return v.Pod.Speed == 0 && v.Pod.BlockedBy == id })
	if v.Pod.WaitReason != blockedByIncident || v.blocks.end(v.reservedThrough)-v.blocks.lanes[v.blocks.locate(v.blockIndex, 0)].start != 270 {
		t.Fatalf("pod 02 waits with %q at the grant end %g", v.Pod.WaitReason, v.blocks.end(v.reservedThrough))
	}
	return v, id, v.reservedThrough
}

// TestDebrisClearAtCommandBoundary checks a clear at a command boundary.
// The clear releases the footprint at once, and the next admission grants
// it to the waiting pod.
func TestDebrisClearAtCommandBoundary(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	v, id, through := waitAtDebris(t, s, 0)
	if err := s.clearFault(id); err != nil {
		t.Fatal(err)
	}
	for _, r := range []resource{track("return", 9), track("return", 10)} {
		if owner, held := s.owners[r]; held {
			t.Fatalf("resource %v has the owner %v after the clear", r, owner)
		}
	}
	if len(s.faults) != 0 || s.blockedActive() || v.Pod.BlockedBy != "" || v.Pod.WaitReason != NoWait || s.faultCounters.cleared != 1 {
		t.Fatalf("records %v, blocked %t, pod 02 waits with %q by %q", faultIDs(s), s.blockedActive(), v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	s.Step()
	if v.reservedThrough <= through {
		t.Fatalf("the pod has the grants to block %d after the clear, want more than %d", v.reservedThrough, through)
	}
}

// TestDebrisClearInFaultStage checks a timed clear. The fault stage
// removes the record, and the release boundary of the same tick releases
// the footprint, so the waiting pod gets it in the next tick and not in
// the tick of the clear. After the tick, no wait report names the removed
// record.
func TestDebrisClearInFaultStage(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	v, _, through := waitAtDebris(t, s, 60)
	end := s.faults[0].end
	for s.tick < end-1 {
		s.Step()
	}
	if len(s.faults) != 1 || v.reservedThrough != through {
		t.Fatalf("tick %d: records %v, grants to block %d", s.tick, faultIDs(s), v.reservedThrough)
	}
	s.Step()
	if len(s.faults) != 0 || s.faultCounters.cleared != 1 || s.faultReleased != nil {
		t.Fatalf("tick %d: records %v, cleared %d, released %v", s.tick, faultIDs(s), s.faultCounters.cleared, s.faultReleased)
	}
	for _, r := range []resource{track("return", 9), track("return", 10)} {
		if owner, held := s.owners[r]; held {
			t.Fatalf("resource %v has the owner %v after the tick of the clear", r, owner)
		}
	}
	// Admission saw the fault owner in the tick of the clear, and the
	// release ended the report that names the removed record.
	if v.reservedThrough != through || v.Pod.BlockedBy != "" || v.Pod.WaitReason != NoWait {
		t.Fatalf("in the tick of the clear, the pod has the grants to block %d and waits with %q by %q", v.reservedThrough, v.Pod.WaitReason, v.Pod.BlockedBy)
	}
	s.Step()
	if v.reservedThrough <= through {
		t.Fatalf("the pod has the grants to block %d after the release, want more than %d", v.reservedThrough, through)
	}
}

// TestDebrisClearOnPlanningError checks a timed clear in a tick that ends
// early. A compact group with no member makes the compact planning fail
// after the fault stage, so Step pauses before the release boundary of
// the tick. The released debris resources are still free when the tick
// ends, and the state contract holds.
func TestDebrisClearOnPlanningError(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	_, _, through := waitAtDebris(t, s, 60)
	s.monitor = nil
	for s.tick < s.faults[0].end-1 {
		s.Step()
	}
	s.compactGroups = []*compactBufferGroup{{}}
	s.Step()
	if s.compactFault == nil || !s.paused || len(s.faults) != 0 {
		t.Fatalf("compact fault %v, paused %t, records %v", s.compactFault, s.paused, faultIDs(s))
	}
	for _, r := range []resource{track("return", 9), track("return", 10)} {
		if owner, held := s.owners[r]; held {
			t.Fatalf("resource %v has the owner %v after the tick of the clear", r, owner)
		}
	}
	// The injected group is the cause of the planning error, not a state
	// of the run.
	s.compactGroups, s.compactFault = nil, nil
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	if err := checkOwners(s); err != nil {
		t.Fatal(err)
	}
	if v := s.findVehicle("02"); v.reservedThrough != through || v.Pod.BlockedBy != "" {
		t.Fatalf("pod 02 has the grants to block %d, want %d, and is blocked by %q", v.reservedThrough, through, v.Pod.BlockedBy)
	}
}

// TestDebrisCongestionCosts checks that the debris footprint adds no
// congestion cost to its lane, so a cost does not outlive a timed clear.
// The owned cells of a pod still add their cost.
func TestDebrisCongestionCosts(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	lane := laneIndex(t, s, "return")
	before := s.congestionCosts()[lane]
	startDebris(t, s, "return", 300, 310, 0)
	if got := s.congestionCosts()[lane]; got != before {
		t.Fatalf("the lane costs %g s with the debris, want %g s", got, before)
	}
	s.owners[track("return", 20)] = podResourceOwner("01")
	if got := s.congestionCosts()[lane]; got != before+ownedTrackCongestionSeconds {
		t.Fatalf("the lane costs %g s with a pod cell, want %g s", got, before+ownedTrackCongestionSeconds)
	}
}

// TestDebrisDoesNotEvacuate checks that the fault stage evacuates no pod
// for a debris record. With an evacuation delay of 10 seconds, debris
// starts 5 seconds before a pod fault on pod 01, at index 0, which boards
// riders at s0. The pod is evacuated 10 seconds after its own fault, and
// not 10 seconds after the debris.
func TestDebrisDoesNotEvacuate(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	if err := s.board(v, newTrip(s, "s0", "s2")); err != nil {
		t.Fatal(err)
	}
	startDebris(t, s, "s3-link", 70, 80, 0)
	for range 5 * TicksPerSecond {
		s.Step()
	}
	startFault(t, s, v, 0)
	evacuateTick := s.tick + 10*TicksPerSecond
	for s.tick < evacuateTick-1 {
		s.Step()
	}
	if v.RidersAboard() != 1 || s.interrupted != 0 || s.faultCounters.evacuations != 0 {
		t.Fatalf("tick %d: pod 01 has %d riders, interrupted %d, evacuations %d", s.tick, v.RidersAboard(), s.interrupted, s.faultCounters.evacuations)
	}
	s.Step()
	if v.RidersAboard() != 0 || s.faultCounters.evacuations != 1 {
		t.Fatalf("tick %d: pod 01 has %d riders after its evacuation tick", s.tick, v.RidersAboard())
	}
}

// TestDebrisSaveAndReplay saves, in the physical format, after a debris
// command, and checks a replay from a checkpoint through a timed clear.
// The save format has no fault member yet, so the restored state has no
// debris. See physicalSave.
func TestDebrisSaveAndReplay(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	returnTraveler(t, s)
	checkDebrisEachTick(t, s)
	id := startDebris(t, s, "return", 300, 310, 5)
	physicalSave(t, s, "debris command")
	checkpoint := s.Clone()
	for range 600 {
		s.Step()
		checkpoint.Step()
	}
	// DeepEqual refuses two func values that are not nil.
	s.monitor, checkpoint.monitor = nil, nil
	if s.faultCounters.cleared != 1 || !sameState(checkpoint, s) {
		t.Fatalf("cleared %d: the replay differs from the source", s.faultCounters.cleared)
	}
	// A second debris clears at a command boundary on the checkpoint.
	again := startDebris(t, checkpoint, "return", 1000, 1010, 0)
	if again == id {
		t.Fatalf("the checkpoint reused the ID %s", id)
	}
	if err := checkpoint.clearFault(again); err != nil {
		t.Fatal(err)
	}
}

// TestDebrisReset checks that Reset ends the debris and frees its
// footprint.
func TestDebrisReset(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	startDebris(t, s, "return", 300, 310, 0)
	s.Reset()
	for r, owner := range s.owners {
		if owner.kind == faultOwnerKind {
			t.Fatalf("resource %v has the owner %v after the reset", r, owner)
		}
	}
	if s.faults != nil || s.faultReleased != nil || s.blockedActive() {
		t.Fatalf("records %v, released %v, blocked %t", faultIDs(s), s.faultReleased, s.blockedActive())
	}
	if err := s.CheckContract(); err != nil {
		t.Fatal(err)
	}
	if err := checkOwners(s); err != nil {
		t.Fatal(err)
	}
}

// TestCheckDebris checks that CheckContract refuses each damaged debris
// record and each damaged debris owner (F5 to F8).
func TestCheckDebris(t *testing.T) {
	t.Parallel()
	faultOwner := func(id string) resourceOwner { return resourceOwner{kind: faultOwnerKind, id: id} }
	tests := []struct {
		name   string
		damage func(s *Simulation)
		want   string
	}{
		{"65 debris records", func(s *Simulation) {
			first := s.faults[0]
			for index := range maxDebrisFaults - 1 {
				first.serial = s.faults[len(s.faults)-1].serial + uint64(index) + 1
				s.faults = append(s.faults, first)
			}
		}, "more than 64 debris"},
		{"lane past the network", func(s *Simulation) { s.faults[0].lane = len(s.network.Lanes) }, "unknown lane"},
		{"end past the lane", func(s *Simulation) { s.faults[0].to = s.graph.lengths[s.faults[0].lane] + 1 }, "invalid debris segment"},
		{"empty segment", func(s *Simulation) { s.faults[0].to = s.faults[0].from }, "invalid debris segment"},
		{"shared footprint", func(s *Simulation) {
			s.faults[1].lane, s.faults[1].from, s.faults[1].to = s.faults[0].lane, s.faults[0].from, s.faults[0].to
		}, "share the resource"},
		{"footprint without its owner", func(s *Simulation) { delete(s.owners, track("return", 9)) }, "does not own"},
		{"footprint with another owner", func(s *Simulation) { s.owners[track("return", 9)] = podResourceOwner("01") }, "does not own"},
		{"pod fault footprint", func(s *Simulation) {
			s.faults[1].from, s.faults[1].to = 45, 46
			s.owners[track("return", 1)] = faultOwner(s.faults[1].id())
		}, "meets the footprint"},
		{"fault owner outside a footprint", func(s *Simulation) {
			s.owners[track("s3-link", 2)] = faultOwner(s.faults[0].id())
		}, "outside the footprint"},
		{"owner of a removed fault", func(s *Simulation) { s.owners[track("s3-link", 2)] = faultOwner("i9.9") }, "outside the footprint"},
		{"fault owner with no ID", func(s *Simulation) { s.owners[track("s3-link", 2)] = faultOwner("") }, "outside the footprint"},
		{"released resources at a boundary", func(s *Simulation) { s.faultReleased = []resource{track("s3-link", 2)} }, "released debris resources"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := debrisFleet(t)
			v := returnTraveler(t, s)
			startDebris(t, s, "return", 300, 310, 0)
			startDebris(t, s, "return", 400, 410, 0)
			startFault(t, s, v, 0)
			if err := s.CheckContract(); err != nil {
				t.Fatalf("before the damage: %v", err)
			}
			test.damage(s)
			err := s.CheckContract()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %v, want one with %q", err, test.want)
			}
		})
	}
}
