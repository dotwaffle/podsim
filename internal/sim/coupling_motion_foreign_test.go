package sim

import (
	"errors"
	"maps"
	"math"
	"testing"
)

func couplingMotionWithForeignStation(t *testing.T, input couplingReservationInput) couplingReservationInput {
	t.Helper()
	network := input.Prepared.Network()
	classes := classBit(string(CompactClass)) | classBit(string(GroupClass)) | classBit(string(ExpressClass))
	network.Nodes = append(network.Nodes, Node{ID: "foreign-entry", Position: Point{X: -300, Y: 600}}, Node{ID: "foreign-berth", Position: Point{X: -250, Y: 650}}, Node{ID: "foreign-exit", Position: Point{X: -200, Y: 600}})
	network.Stations = append(network.Stations, Station{ID: "foreign-station", Name: "Foreign", Entry: "foreign-entry", Exit: "foreign-exit", VehicleClasses: classes, Berths: []Berth{{ID: "foreign-1", Node: "foreign-berth", VehicleClasses: classes}}})
	for _, lane := range []Lane{{ID: "foreign-in", From: "foreign-entry", To: "foreign-berth"}, {ID: "foreign-out", From: "foreign-berth", To: "foreign-exit"}, {ID: "foreign-through", From: "foreign-entry", To: "foreign-exit"}} {
		lane.VehicleClasses = classes
		lane.SpeedLimit = 7
		network.Lanes = append(network.Lanes, lane)
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
	input.Network, input.Prepared, input.OrderContract = n, prepared, ExpressOrderContract
	return input
}

func TestCouplingMotionParkedForeignClasses(t *testing.T) {
	t.Parallel()
	for _, class := range []VehicleClass{CompactClass, GroupClass, ExpressClass} {
		t.Run(string(class), func(t *testing.T) {
			t.Parallel()
			input := couplingMotionWithForeignStation(t, couplingMotionFixture(t, true, false))
			path, err := prepareCouplingParkedForeign(input.Network, input.OrderContract, "parked", class, "foreign-station", "foreign-1")
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range berthResources(path.berth) {
				input.Owners[r] = podResourceOwner("parked")
			}
			foreignView, err := sealCouplingForeignOwners(path, 0, -1, input.Owners)
			if err != nil {
				t.Fatal(err)
			}
			reservation, err := planCouplingReservation(input)
			if err != nil {
				t.Fatal(err)
			}
			c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair", ForeignIDs: []string{"parked"}})
			if err != nil {
				t.Fatal(err)
			}
			initial, err := initialCouplingMotion(c, input)
			if err != nil {
				t.Fatal(err)
			}
			owners := maps.Clone(input.Owners)
			applyCouplingTestWrites(t, owners, initial.Writes)
			view, err := sealCouplingMotionOwners(c, initial.State, owners)
			if err != nil {
				t.Fatal(err)
			}
			state := initial.State
			for !state.Finished {
				previous := state
				sweep := couplingForeignSweep{Path: path, Tick: state.Tick, ReservedThrough: -1, Owners: foreignView}
				step, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: state, Owners: view, Foreign: []couplingForeignSweep{sweep}})
				if err != nil {
					t.Fatal(err)
				}
				state = step.State
				couplingIndependentBodyOracle(t, c, previous, step)
				couplingIndependentLaneOracle(t, c, previous, state)
				couplingIndependentParkedSweep(t, c, previous, state, path)
				for member, position := range state.Positions {
					minimum := 12.0
					if class == GroupClass || class == ExpressClass {
						minimum = 20
					}
					if pointDistance(position, path.position) < minimum {
						t.Fatal("independent foreign class-pair exclusion failed")
					}
					if class == GroupClass || class == ExpressClass {
						for _, corner := range step.Bodies[member].Corners {
							if pointDistance(corner, path.position) < 6 {
								t.Fatal("approved six-meter large circle touches real body corner")
							}
						}
					}
				}
				applyCouplingTestWrites(t, owners, step.Writes)
				if len(step.Writes) > 0 {
					view, err = advanceCouplingMotionOwners(view, previous, state, step.Writes, owners)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}

func TestCouplingMotionForeignFactsAndSet(t *testing.T) {
	t.Parallel()
	input := couplingMotionWithForeignStation(t, couplingMotionFixture(t, false, false))
	path, err := prepareCouplingParkedForeign(input.Network, input.OrderContract, "parked", GroupClass, "foreign-station", "foreign-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range berthResources(path.berth) {
		input.Owners[r] = podResourceOwner("parked")
	}
	foreignView, err := sealCouplingForeignOwners(path, 0, -1, input.Owners)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair", ForeignIDs: []string{"parked"}})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := initialCouplingMotion(c, input)
	if err != nil {
		t.Fatal(err)
	}
	actual := maps.Clone(input.Owners)
	applyCouplingTestWrites(t, actual, initial.Writes)
	view, err := sealCouplingMotionOwners(c, initial.State, actual)
	if err != nil {
		t.Fatal(err)
	}
	valid := couplingForeignSweep{Path: path, Tick: initial.State.Tick, ReservedThrough: -1, Owners: foreignView}
	for _, test := range []struct {
		name   string
		change func([]couplingForeignSweep) []couplingForeignSweep
	}{
		{"missing", func(s []couplingForeignSweep) []couplingForeignSweep { return nil }},
		{"duplicate", func(s []couplingForeignSweep) []couplingForeignSweep { return append(s, s[0]) }},
		{"stale_tick", func(s []couplingForeignSweep) []couplingForeignSweep { s[0].Tick--; return s }},
		{"unsealed", func(s []couplingForeignSweep) []couplingForeignSweep { s[0].Owners = nil; return s }},
		{"parked_motion", func(s []couplingForeignSweep) []couplingForeignSweep { s[0].NextSpeed = .1; return s }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sweeps := test.change([]couplingForeignSweep{valid})
			before := maps.Clone(actual)
			out, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: initial.State, Owners: view, Foreign: sweeps})
			if !errors.Is(err, errCouplingMotionInvariant) || out.State.context != nil || len(out.Writes) != 0 || !maps.Equal(before, actual) {
				t.Fatalf("invalid foreign facts accepted: %v", err)
			}
		})
	}
	for _, r := range berthResources(path.berth) {
		bad := maps.Clone(input.Owners)
		bad[r] = resourceOwner{kind: groupOwnerKind, id: "parked"}
		if _, err := sealCouplingForeignOwners(path, 0, -1, bad); !errors.Is(err, errCouplingMotionInvariant) {
			t.Fatal("same-ID foreign group substituted for parked pod")
		}
	}
	if _, err := prepareCouplingParkedForeign(input.Network, "", "parked", ExpressClass, "foreign-station", "foreign-1"); !errors.Is(err, errCouplingReservationDenied) {
		t.Fatal("foundation marker accepted an unsupported Express envelope")
	}
}

// Ordinary native stopping snaps are outside this inactive strict-sweep certificate.
func TestCouplingMotionOrdinarySnapCompatibilityLimit(t *testing.T) {
	t.Parallel()
	input := couplingMotionWithForeignStation(t, couplingMotionFixture(t, false, false))
	var route []Lane
	for _, lane := range input.Prepared.Network().Lanes {
		if lane.ID == "foreign-out" {
			route = append(route, lane)
		}
	}
	path, err := prepareCouplingForeignPath(input.Network, input.OrderContract, "moving", CompactClass, route)
	if err != nil {
		t.Fatal(err)
	}
	s, err := input.Prepared.NewFleetWithOrderContract([]Placement{{ID: "moving", Class: CompactClass, StationID: "foreign-station", BerthID: "foreign-1"}}, input.OrderContract)
	if err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	v.Pod.Activity, v.Pod.LaneID, v.Pod.Speed = Traveling, "", 0
	s.setVehicleRoute(v, route)
	v.reservedThrough = 0
	v.Pod.StationID, v.Pod.BerthID = "", ""
	frontier := v.blocks.at(0).end
	for _, b := range v.blocks.span(0, 1) {
		for _, r := range b.resources {
			if resourceReleaseDistance(b, r) > 0 {
				s.owners[r] = podResourceOwner("moving")
			}
		}
	}
	view, err := sealCouplingForeignOwners(path, 0, 0, s.owners)
	if err != nil {
		t.Fatal(err)
	}
	roundedContinuousCount, maxContinuousExcess := 0, 0.0
	for tick := range 10000 {
		before, speed := v.distance, v.Pod.Speed
		if stop := before + couplingOrdinaryDiscreteStop(speed); stop > frontier {
			t.Fatalf("native sample cannot brake within its actual frontier: tick=%d stop=%.17g frontier=%.17g", tick, stop, frontier)
		}
		if excess := before + speed*speed/4 - frontier; excess > 0 {
			roundedContinuousCount++
			maxContinuousExcess = max(maxContinuousExcess, excess)
		}
		t.Logf("ordinary_sample tick=%d distance=%.17g speed=%.17g frontier=%.17g stop=%.17g", tick, before, speed, frontier, before+speed*speed/4)
		if s.moveCompact(v) {
			t.Fatal("snap control unexpectedly used compact queue motion")
		}
		s.move(v)
		if v.distance != frontier {
			continue
		}
		if before+speed*speed/4 > frontier {
			t.Fatal("natural pre-snap sample lacks exact continuous owned stopping room")
		}
		if v.Pod.Speed != 0 || v.distance == before+v.Pod.Speed/60 {
			t.Fatal("natural terminal tick did not expose published-speed snap")
		}
		sweep := couplingForeignSweep{Path: path, Distance: before, Speed: speed, NextDistance: v.distance, NextSpeed: v.Pod.Speed, ReservedThrough: 0, Owners: view}
		if !errors.Is(couplingCheckForeignMotion(sweep), errCouplingMotionInvariant) {
			t.Fatal("strict private sweep silently accepted ordinary snap")
		}
		t.Logf("ordinary tick=%d frontier=%.17g before=%.17g previous_speed=%.17g next=%.17g published_speed=%.17g strict_foreign_refused=true", tick+1, frontier, before, speed, v.distance, v.Pod.Speed)
		t.Logf("native continuous_roundoff_samples=%d max_excess=%.17g pre_snap_continuous_bound_exact=true", roundedContinuousCount, maxContinuousExcess)
		return
	}
	t.Fatal("ordinary first-cell stop did not finish within fixture cap")
}

// One ordinary native tick supplies a strict positive certificate away from its snap.
func TestCouplingMotionMovingForeignCertificate(t *testing.T) {
	t.Parallel()
	input := couplingMotionWithForeignStation(t, couplingMotionFixture(t, false, false))
	var route []Lane
	for _, lane := range input.Prepared.Network().Lanes {
		if lane.ID == "foreign-out" {
			route = append(route, lane)
		}
	}
	path, err := prepareCouplingForeignPath(input.Network, input.OrderContract, "moving", CompactClass, route)
	if err != nil {
		t.Fatal(err)
	}
	s, err := input.Prepared.NewFleetWithOrderContract([]Placement{{ID: "moving", Class: CompactClass, StationID: "foreign-station", BerthID: "foreign-1"}}, input.OrderContract)
	if err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	v.Pod.Activity, v.Pod.LaneID, v.Pod.Speed = Traveling, "", 0
	s.setVehicleRoute(v, route)
	v.reservedThrough = 0
	v.Pod.StationID, v.Pod.BerthID = "", ""
	actual := s.owners
	for _, b := range v.blocks.span(0, 1) {
		for _, r := range b.resources {
			if resourceReleaseDistance(b, r) > 0 {
				actual[r] = podResourceOwner("moving")
			}
		}
	}
	view, err := sealCouplingForeignOwners(path, 0, 0, actual)
	if err != nil {
		t.Fatal(err)
	}
	s.move(v)
	valid := couplingForeignSweep{Path: path, Distance: 0, Speed: 0, NextDistance: v.distance, NextSpeed: v.Pod.Speed, ReservedThrough: 0, Owners: view}
	if err = couplingCheckForeignMotion(valid); err != nil {
		t.Fatalf("actual nonsnap ordinary tick refused: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(*couplingForeignSweep)
	}{
		{"wrong_euler", func(s *couplingForeignSweep) { s.NextDistance += 1 }},
		{"acceleration", func(s *couplingForeignSweep) { s.NextSpeed = 1; s.NextDistance = 1.0 / 60 }},
		{"missing_grant", func(s *couplingForeignSweep) { s.Owners = nil }},
		{"changed_frontier", func(s *couplingForeignSweep) { s.ReservedThrough = 1 }},
		{"nonfinite", func(s *couplingForeignSweep) { s.Speed = math.NaN() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			bad := valid
			test.change(&bad)
			if !errors.Is(couplingCheckForeignMotion(bad), errCouplingMotionInvariant) {
				t.Fatal("invalid strict moving foreign certificate accepted")
			}
		})
	}
	// This remains exact Euler and bounded acceleration, but its stopping frontier is too short.
	frontier := path.blocks.at(0).end
	shortView, err := sealCouplingForeignOwners(path, frontier-1e-6, 0, actual)
	if err != nil {
		t.Fatal(err)
	}
	short := couplingForeignSweep{Path: path, Distance: frontier - 1e-6, Speed: .1, NextSpeed: .1, NextDistance: frontier - 1e-6 + .1/60, ReservedThrough: 0, Owners: shortView}
	if !errors.Is(couplingCheckForeignMotion(short), errCouplingMotionInvariant) {
		t.Fatal("exact foreign Euler moved past owned stopping frontier")
	}
}

// Sum the positive end-speed Euler velocities after maximum ordinary braking.
func couplingOrdinaryDiscreteStop(speed float64) float64 {
	step := 2.0 / 60
	count := math.Floor(speed / step)
	return max(0, (count*speed-step*count*(count+1)/2)/60)
}
