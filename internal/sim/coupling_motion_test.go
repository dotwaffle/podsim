package sim

import (
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// This fixture proves stopped ordinary footprints, not live recruitment.
func couplingMotionFixture(t *testing.T, occupied, rotate bool) couplingReservationInput {
	t.Helper()
	return couplingMotionFixtureWith(t, occupied, rotate, nil)
}

// couplingMotionFixtureWith is couplingMotionFixture with edit applied to
// the network before the rotation, when edit is not nil.
func couplingMotionFixtureWith(t *testing.T, occupied, rotate bool, edit func(*Network)) couplingReservationInput {
	t.Helper()
	geometry := couplingGeometryFixture()
	compact := classBit(string(CompactClass))
	geometry.Network.Nodes = []Node{{ID: "a"}, {ID: "b", Position: Point{X: 210}}, {ID: "c", Position: Point{X: 420}},
		{ID: "origin-entry", Position: Point{X: -60, Y: 50}}, {ID: "origin-exit", Position: Point{X: -60, Y: -50}},
		{ID: "front-entry", Position: Point{X: 620, Y: 150}}, {ID: "rear-entry", Position: Point{X: 620, Y: -150}},
		{ID: "front-berth", Position: Point{X: 670, Y: 175}}, {ID: "rear-berth", Position: Point{X: 670, Y: -175}},
		{ID: "front-exit", Position: Point{X: 730, Y: 175}}, {ID: "rear-exit", Position: Point{X: 730, Y: -175}}}
	geometry.Network.Lanes = nil
	for _, lane := range []Lane{{ID: "ab", From: "a", To: "b"}, {ID: "bc", From: "b", To: "c"},
		{ID: "origin-in", From: "origin-entry", To: "a"}, {ID: "origin-out", From: "a", To: "origin-exit"}, {ID: "origin-through", From: "origin-entry", To: "origin-exit"},
		{ID: "front-road", From: "c", To: "front-entry"}, {ID: "rear-road", From: "c", To: "rear-entry"},
		{ID: "front-in", From: "front-entry", To: "front-berth"}, {ID: "rear-in", From: "rear-entry", To: "rear-berth"},
		{ID: "front-through", From: "front-entry", To: "front-exit"}, {ID: "rear-through", From: "rear-entry", To: "rear-exit"},
		{ID: "front-out", From: "front-berth", To: "front-exit"}, {ID: "rear-out", From: "rear-berth", To: "rear-exit"}} {
		lane.SpeedLimit = 7.123456789
		lane.VehicleClasses = compact
		geometry.Network.Lanes = append(geometry.Network.Lanes, lane)
	}
	geometry.Network.Stations = []Station{
		{ID: "origin", Name: "Origin", Entry: "origin-entry", Exit: "origin-exit", VehicleClasses: compact, Berths: []Berth{{ID: "origin-1", Node: "a", VehicleClasses: compact}}},
		{ID: "front-goal", Name: "Front goal", Entry: "front-entry", Exit: "front-exit", VehicleClasses: compact, Berths: []Berth{{ID: "front-goal-1", Node: "front-berth", VehicleClasses: compact}}},
		{ID: "rear-goal", Name: "Rear goal", Entry: "rear-entry", Exit: "rear-exit", VehicleClasses: compact, Berths: []Berth{{ID: "rear-goal-1", Node: "rear-berth", VehicleClasses: compact}}}}
	if edit != nil {
		edit(&geometry.Network)
	}
	if rotate {
		for i, node := range geometry.Network.Nodes {
			x, y := node.Position.X, node.Position.Y
			geometry.Network.Nodes[i].Position = Point{X: 130.125 + .6*x - .8*y, Y: 137.625 + .8*x + .6*y}
		}
	}
	prepared, err := PrepareNetwork(geometry.Network)
	if err != nil {
		t.Fatal(err)
	}
	geometry.Network = prepared.Network()
	count := prepared.laneCells["ab"].count()
	end := float64(2) * prepared.graph.lengths[prepared.graph.lanes["ab"]] / float64(count)
	geometry.Sites[0].RearStagingMeters = end
	geometry.Sites[0].FrontStagingMeters = end + 12
	geometry.Sites[0].EndMeters = 120
	n, err := prepareCouplingReservations(prepared, geometry)
	if err != nil {
		t.Fatal(err)
	}
	input := couplingReservationInput{Network: n, Prepared: prepared, CorridorID: "corridor", Tick: 10, Owners: make(map[resource]resourceOwner)}
	for i, id := range []string{"front", "rear"} {
		distance, cell := end+12, 2
		if i == 1 {
			distance, cell = end, 1
		}
		goal, road, in := "front-goal", "front-road", "front-in"
		if i == 1 {
			goal, road, in = "rear-goal", "rear-road", "rear-in"
		}
		station, _ := geometry.Network.Station(goal)
		m := couplingMemberSnapshot{Distance: distance, BlockIndex: cell, ReservedThrough: cell, DestinationStation: goal, Origin: geometry.Network.Stations[0].Berths[0], Destination: station.Berths[0], Retained: make(map[resource]float64)}
		m.Vehicle = Vehicle{Pod: Pod{ID: id, Class: CompactClass, Activity: Traveling, LaneID: "ab", LaneDistance: distance, Position: couplingOffset(n.lanes["ab"].from, n.lanes["ab"].direction, distance)}, RelocatingTo: goal}
		for _, laneID := range []string{"ab", "bc", road, in} {
			for _, lane := range geometry.Network.Lanes {
				if lane.ID == laneID {
					m.Vehicle.Route = append(m.Vehicle.Route, lane)
				}
			}
		}
		for _, r := range berthResources(m.Destination) {
			input.Owners[r] = podResourceOwner(id)
		}
		if occupied {
			m.Vehicle.Pod.Occupied = true
			m.Vehicle.RelocatingTo = ""
			m.Vehicle.Stops = []string{goal}
			m.Vehicle.Riders = []Request{{ID: i + 1, From: "origin", To: goal, PartySize: i + 1, PodID: id, SharingConsent: PrivateConsent, Service: OnDemandService, RequestedTick: 1, BoardedTick: 2}}
		}
		// The old cell releases at <= front distance; the rear owns that old cell.
		for k := 0; k <= cell; k++ {
			release := float64(k+1)*210/float64(count) + 12
			if release <= distance {
				continue
			}
			r := resource{kind: trackResource, id: "ab", cell: k}
			if owner := input.Owners[r]; !owner.isZero() {
				t.Fatal("staged native footprints overlap")
			}
			input.Owners[r] = podResourceOwner(id)
			m.Retained[r] = release
		}
		input.Members[i] = m
	}
	return input
}

func TestCouplingMotionIndependentTicks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		occupied, rotate bool
	}{{"empty", false, false}, {"occupied", true, false}, {"non_axis_offset", false, true}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingMotionFixture(t, test.occupied, test.rotate)
			before := cloneCouplingInput(input)
			reservation, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			context, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
			if err != nil {
				t.Fatal(err)
			}
			if context.ticks > 30000 {
				t.Fatal("fixture exceeds declared oracle cap")
			}
			initial, err := initialCouplingMotion(context, input)
			if err != nil {
				t.Fatal(err)
			}
			owners := maps.Clone(input.Owners)
			applyCouplingTestWrites(t, owners, initial.Writes)
			state := initial.State
			view, err := sealCouplingMotionOwners(context, state, owners)
			if err != nil {
				t.Fatal(err)
			}
			latchTicks, unlatchTicks := 0, 0
			for !state.Finished {
				old := state
				if old.Phase == couplingLatching {
					latchTicks++
					if old.Dwell != 121-latchTicks {
						t.Fatal("independent remaining latch dwell differs")
					}
				}
				if old.Phase == couplingUnlatching {
					unlatchTicks++
					if old.Dwell != 121-unlatchTicks {
						t.Fatal("independent remaining unlatch dwell differs")
					}
				}
				step, err := planCouplingMotion(couplingMotionInput{Context: context, Previous: old, Owners: view})
				if err != nil {
					t.Fatalf("tick %d phase %d: %v", old.Elapsed, old.Phase, err)
				}
				state = step.State
				couplingIndependentBodyOracle(t, context, old, step)
				couplingIndependentLaneOracle(t, context, old, state)
				for i := range state.Distances {
					bound := 2.0
					if old.Phase == couplingClosing || old.Phase == couplingOpening {
						bound = .5
					}
					if state.Distances[i] != old.Distances[i]+state.Speeds[i]/60 || math.Abs(state.Speeds[i]-old.Speeds[i]) > bound/60 {
						t.Fatal("independent strict Euler/acceleration failure")
					}
					lane := input.Network.lanes[state.Lanes[i]].lane
					if state.Speeds[i] > lane.SpeedLimit {
						t.Fatal("independent current lane speed failure")
					}
					blocks := context.reservation.routes[i]
					frontier := blocks.at(context.through[i]).end
					if state.Distances[i]+state.Speeds[i]*state.Speeds[i]/(2*bound)+state.Speeds[i]/60 > frontier {
						t.Fatal("independent owned stop failure")
					}
					if step.Samples[i].DistanceMeters != state.Distances[i]-old.Distances[i] || step.Samples[i].EndSpeed != state.Speeds[i] {
						t.Fatal("actual sample differs from motion")
					}
					if !reflect.DeepEqual(step.Members[i].Riders, input.Members[i].Vehicle.Riders) || !slices.Equal(step.Members[i].Stops, input.Members[i].Vehicle.Stops) {
						t.Fatal("whole parties/consent/stops changed")
					}
				}
				if state.Phase == couplingConnected && state.Speeds[0] != state.Speeds[1] {
					t.Fatal("connected speeds differ")
				}
				if state.Phase == couplingDraining && pointDistance(state.Positions[0], state.Positions[1]) < 12-1e-9 {
					t.Fatal("ordinary drain separation failed")
				}
				applyCouplingTestWrites(t, owners, step.Writes)
				if len(step.Writes) > 0 {
					view, err = advanceCouplingMotionOwners(view, old, state, step.Writes, owners)
					if err != nil {
						t.Fatal(err)
					}
				}
				// Raw canonical resources, with native release tails, protect each body.
				for i := range state.Distances {
					blocks := context.reservation.routes[i]
					for _, b := range blocks.span(0, context.through[i]+1) {
						for _, r := range b.resources {
							release := b.end + max(12, b.tail)
							if r.kind == junctionResource {
								release = b.end
							}
							if r.kind == nodeResource && r.id == b.lane.From {
								release = b.start + max(12, b.fromTail)
							}
							if release > state.Distances[i] && owners[r] != context.owner && !owners[r].isPod(input.Members[i].Vehicle.Pod.ID) {
								t.Fatalf("raw current/future resource lost: %+v", r)
							}
						}
					}
				}
			}
			if latchTicks != 120 || unlatchTicks != 120 {
				t.Fatalf("dwell ticks %d/%d", latchTicks, unlatchTicks)
			}
			for _, d := range context.dependencies {
				if couplingJointDependency(d) && owners[d.Resource] == context.owner {
					t.Fatal("terminal joint dependency did not retire")
				}
			}
			if !couplingInputsEqual(before, input) {
				t.Fatal("motion preparation or ticks changed input")
			}
			t.Logf("total_ticks=%d claims=%d static_exit=%v closed_exit=%v drain_order=%v", context.ticks, len(context.claims), reservation.Exits, context.terminal, context.drainOrder)
		})
	}
}

func applyCouplingTestWrites(t *testing.T, owners map[resource]resourceOwner, writes []couplingOwnerWrite) {
	t.Helper()
	for _, w := range writes {
		if owners[w.Resource] != w.Expected {
			t.Fatal("write expected owner differs")
		}
		owners[w.Resource] = w.Next
	}
}

func TestCouplingMotionCommittedFailuresAreAtomic(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, false, false)
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := initialCouplingMotion(c, input)
	if err != nil {
		t.Fatal(err)
	}
	base := maps.Clone(input.Owners)
	applyCouplingTestWrites(t, base, initial.Writes)
	view, err := sealCouplingMotionOwners(c, initial.State, base)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*couplingMotionInput)
	}{
		{"stale_cursor", func(in *couplingMotionInput) { in.Previous.Cursor++ }},
		{"moving_pause", func(in *couplingMotionInput) { in.Previous.Speeds[1] = .1 }},
		{"missing_owner_view", func(in *couplingMotionInput) { in.Owners = nil }},
		{"unknown_sweep", func(in *couplingMotionInput) { in.Foreign = []couplingForeignSweep{{Tick: initial.State.Tick}} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			in := couplingMotionInput{Context: c, Previous: initial.State, Owners: view}
			test.change(&in)
			before := maps.Clone(base)
			out, err := planCouplingMotion(in)
			if !errors.Is(err, errCouplingMotionInvariant) || out.State.context != nil || len(out.Writes) != 0 || !maps.Equal(before, base) {
				t.Fatalf("non-atomic committed refusal: %v %+v", err, out)
			}
		})
	}
}

// Route distances whose lane prefixes are not exact in float must keep their
// exact sweep ends. The observed case is a native foreign pod on lane ab.
func TestCouplingMotionSegmentsInexactPrefix(t *testing.T) {
	t.Parallel()
	straight := func(lengths ...float64) blockList {
		blocks := blockList{lanes: make([]routeLaneCells, len(lengths)+1)}
		for i, length := range lengths {
			blocks.route = append(blocks.route, Lane{ID: strconv.Itoa(i)})
			from := Point{X: blocks.lanes[i].start}
			blocks.lanes[i].length = length
			blocks.lanes[i].geometry = &laneGeometry{segments: []laneSegment{{from: from, to: Point{X: from.X + length}, end: length}}}
			blocks.lanes[i+1].start = blocks.lanes[i].start + length
		}
		return blocks
	}
	feed := math.Hypot(145, 90)
	for _, tc := range []struct {
		name       string
		blocks     blockList
		start, end func(blockList) float64
	}{
		{"observed foreign sweep", straight(202.67610284592436, 600),
			func(blockList) float64 { return 458.61666666667276 }, func(blockList) float64 { return 458.8500000000061 }},
		{"inexact start", straight(202.67610284592436, 600),
			func(blockList) float64 { return 458.8500000000061 }, func(blockList) float64 { return 459.08333333333945 }},
		{"end at lane boundary below", straight(98.86863694964386, feed, 100),
			func(b blockList) float64 { return b.lanes[1].start + 100 }, func(b blockList) float64 { return b.lanes[2].start }},
		{"end at lane boundary above", straight(258.80710897305283, feed, 100),
			func(b blockList) float64 { return b.lanes[1].start + 100 }, func(b blockList) float64 { return b.lanes[2].start }},
		{"crossing lane boundary", straight(202.67610284592436, feed, 100),
			func(b blockList) float64 { return b.lanes[2].start - 0.1 }, func(b blockList) float64 { return b.lanes[2].start + 0.13333333333333333 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			start, end := tc.start(tc.blocks), tc.end(tc.blocks)
			segments, err := couplingMotionSegments(&tc.blocks, start, end)
			if err != nil {
				t.Fatalf("sweep %.17g..%.17g failed: %v", start, end, err)
			}
			if segments[0].start != start || segments[len(segments)-1].end != end {
				t.Fatalf("sweep ends %.17g..%.17g differ from %.17g..%.17g", segments[0].start, segments[len(segments)-1].end, start, end)
			}
			for i := 1; i < len(segments); i++ {
				if segments[i].start != segments[i-1].end {
					t.Fatal("sweep has a gap at a lane boundary", segments)
				}
			}
		})
	}
	// A sweep that ends one step past a lane end must not take its end
	// from the lane, even when the local end rounds to the lane length.
	overrun := math.Nextafter(800, math.Inf(1))
	if _, err := couplingMotionSegments(new(straight(200.00000000000006, 600)), 799, overrun); err == nil {
		t.Fatal("sweep accepted an end past its route")
	}
	if segments, err := couplingMotionSegments(new(straight(200.00000000000006, 600, 100)), 799, overrun); err != nil {
		t.Fatalf("sweep into the next lane failed: %v", err)
	} else if len(segments) != 2 || segments[0].end != segments[1].start || segments[0].end > segments[1].end || segments[1].end != overrun {
		t.Fatal("sweep into the next lane overlaps its lane boundary", segments)
	}
	// The final check still rejects geometry that does not reach the end.
	short := straight(202.67610284592436, 600)
	short.lanes[1].geometry.segments[0].end = 590
	if _, err := couplingMotionSegments(&short, 400, 795); err == nil {
		t.Fatal("sweep accepted a real gap before its end")
	}
}
