package sim

import (
	"errors"
	"maps"
	"slices"
	"testing"
)

// This isolates actual native foreign controllers from train admission.
// All poses, grants, links, groups, and plans come from the existing native fixture.
// The geometry-free marker permits no train formation.
func nativeForeignControllerFrame(t *testing.T, s *Simulation) *nativeForeignTick {
	t.Helper()
	p := &PreparedNetwork{network: s.network, graph: s.graph, stationIndexes: s.stationIndexes, stationForbidden: s.stationForbidden, geometry: s.geometry, junctionConflicts: s.junctionConflicts, berthResources: s.berthResources, laneCells: s.laneCells, laneSafety: s.laneSafety, berthSafety: s.berthSafety}
	n, err := prepareCouplingReservations(p, CouplingGeometryInput{Contract: CompactPairV1CouplingContract, Network: p.Network()})
	if err != nil {
		t.Fatal(err)
	}
	c := &couplingMotionContext{reservation: couplingReservationPlan{network: n, orderContract: s.orderContract}}
	f := &nativeForeignFleet{source: s, context: c}
	frame := &nativeForeignTick{fleet: f, tick: s.tick + 1, owners: maps.Clone(s.owners), proofs: make(map[string]*nativeForeignProof)}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		entry := nativeForeignEntry{id: v.Pod.ID, class: v.Pod.Class, route: v.Route, routeVersion: v.routeVersion}
		if len(v.Route) > 0 {
			entry.path, err = prepareCouplingForeignPath(n, s.orderContract, v.Pod.ID, v.Pod.Class, v.Route)
			if err != nil {
				t.Fatal(err)
			}
			for _, lane := range entry.path.blocks.lanes {
				if lane.cells != nil {
					entry.maxTail = max(entry.maxTail, Clearance, lane.cells.tail, lane.cells.fromTail)
				}
			}
		}
		if v.Pod.BerthID != "" {
			entry.parked, err = prepareCouplingParkedForeign(n, s.orderContract, v.Pod.ID, v.Pod.Class, v.Pod.StationID, v.Pod.BerthID)
			if err != nil {
				t.Fatal(err)
			}
		}
		f.entries = append(f.entries, entry)
		fact := nativeForeignFact{pod: v.Pod, distance: v.distance, blockIndex: v.blockIndex, through: v.reservedThrough, phaseTicks: v.phaseTicks, origin: v.origin, destination: v.destination, retained: maps.Clone(v.routeReleases), link: v.link, follower: v.follower, cap: v.platoonCap}
		if len(s.compactMotions) > 0 {
			fact.compact = s.compactMotions[i]
		}
		frame.facts = append(frame.facts, fact)
		c.foreignIDs = append(c.foreignIDs, v.Pod.ID)
	}
	slices.Sort(c.foreignIDs)
	return frame
}

func TestNativeForeignCompactActualPlans(t *testing.T) {
	t.Parallel()
	for _, disable := range []bool{false, true} {
		t.Run(map[bool]string{false: "holding", true: "disabled_recovery"}[disable], func(t *testing.T) {
			t.Parallel()
			nativeForeignCompactActualPlan(t, disable)
		})
	}
}

func nativeForeignCompactActualPlan(t *testing.T, disable bool) {
	t.Helper()
	s := occupiedBufferQueue(t, PlatooningVirtual)
	if err := s.SetStationQueueSpacing(StationQueueCompactV1); err != nil {
		t.Fatal(err)
	}
	for range 12000 {
		s.formCompactQueues()
		if err := s.planCompactQueues(); err != nil {
			t.Fatal(err)
		}
		s.platoonCaps()
		if len(s.compactGroups) == 0 || len(s.compactGroups[0].members) < 2 {
			s.Step()
			continue
		}
		if disable {
			if err := s.SetStationQueueSpacing(StationQueueOrdinary); err != nil {
				t.Fatal(err)
			}
			if err := s.planCompactQueues(); err != nil {
				t.Fatal(err)
			}
			if !s.compactNextGroups[0].recovering {
				t.Fatal("native disable did not plan constructive recovery")
			}
		}
		frame := nativeForeignControllerFrame(t, s)
		groups, err := frame.compactCertificates(s.compactGroups, s.compactNextGroups)
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) == 0 {
			t.Fatal("actual compact controller produced no certified members")
		}
		for index, member := range groups {
			proof, proofErr := frame.prepareProof(index, member)
			if proofErr != nil {
				t.Fatal(proofErr)
			}
			frame.proofs[proof.raw.Path.id] = proof
			sweep := proof.raw
			sweep.native = proof
			if err = couplingCheckForeignMotion(sweep); err != nil {
				t.Fatal(err)
			}
			v := &s.vehicles[index]
			if !s.moveCompact(v) {
				t.Fatal("actual native compact consumer did not apply its plan")
			}
			if v.Pod.Speed != sweep.NextSpeed || v.distance != sweep.NextDistance || v.Pod.Position != proof.nextPosition {
				t.Fatal("native compact consumer differs from the certified local command")
			}
		}
		invalidGroups := cloneCompactGroups(s.compactNextGroups)
		invalidGroups[0].recovery.targets[0] = invalidGroups[0].bounds.frontier + 100
		if _, err = frame.compactCertificates(s.compactGroups, invalidGroups); !errors.Is(err, errCouplingMotionInvariant) {
			t.Fatal("corrupt actual native recovery certificate was accepted")
		}
		bad := *frame
		bad.facts = slices.Clone(frame.facts)
		first := s.compactGroups[0].members[0]
		bad.facts[first].compact.state.position++
		if _, err = bad.compactCertificates(s.compactGroups, s.compactNextGroups); !errors.Is(err, errCouplingMotionInvariant) {
			t.Fatal("changed actual compact plan was accepted")
		}
		return
	}
	t.Fatal("native compact plan fixture exceeded its frozen cap")
}

func TestNativeForeignVirtualDrainingOwners(t *testing.T) {
	t.Parallel()
	s := restoreCorridor(t, forkNetwork(14), []corridorPod{
		{route: []string{"exit", "branch"}, station: "fork", distance: corridorExitLength - 53, leader: "p02"},
		{route: []string{"exit", "approach"}, station: "dest", distance: corridorExitLength - 40},
	})
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	s.platoonCaps()
	frame := nativeForeignControllerFrame(t, s)
	follower := s.vehicleIndexes["p01"]
	path := frame.fleet.entries[follower].path
	view, err := frame.sealOwners(follower, path)
	if err != nil {
		t.Fatal(err)
	}
	var borrowed resource
	found := false
	for r, owner := range view.owners {
		if owner.isPod("p02") && r.kind == trackResource {
			borrowed = r
			found = true
			break
		}
	}
	if !found {
		t.Fatal("actual native follower has no borrowed resource")
	}
	if err = s.SetPlatooning(PlatooningOff); err != nil {
		t.Fatal(err)
	}
	s.formPlatoons()
	s.platoonCaps()
	frame = nativeForeignControllerFrame(t, s)
	if frame.facts[follower].link.leader == 0 || s.coupledSpan(s.findVehicle("p01"), s.findVehicle("p01").blockIndex, s.findVehicle("p01").reservedThrough) {
		t.Fatal("native disable lost its retained link or allowed a new coupled grant")
	}
	if _, err = frame.sealOwners(follower, frame.fleet.entries[follower].path); err != nil {
		t.Fatal("actual retained draining borrow was rejected", err)
	}
	for _, test := range []struct {
		name   string
		change func(*nativeForeignTick)
	}{
		{"same_id_group", func(f *nativeForeignTick) { f.owners[borrowed] = resourceOwner{kind: groupOwnerKind, id: "p02"} }},
		{"missing_predecessor_hold", func(f *nativeForeignTick) { delete(f.facts[s.vehicleIndexes["p02"]].retained, borrowed) }},
		{"broken_reciprocity", func(f *nativeForeignTick) { f.facts[s.vehicleIndexes["p02"]].follower = 0 }},
		{"off_run", func(f *nativeForeignTick) { f.facts[follower].link.lane = 1000 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			bad := *frame
			bad.owners = maps.Clone(frame.owners)
			bad.facts = slices.Clone(frame.facts)
			for i := range bad.facts {
				bad.facts[i].retained = maps.Clone(frame.facts[i].retained)
			}
			test.change(&bad)
			if _, err := bad.sealOwners(follower, bad.fleet.entries[follower].path); !errors.Is(err, errCouplingMotionInvariant) {
				t.Fatal("invalid native predecessor evidence was accepted")
			}
		})
	}
	for range 3000 {
		s.Step()
		if s.owners[borrowed].isPod("p01") {
			if release, held := s.findVehicle("p01").routeReleases[borrowed]; !held || release <= s.findVehicle("p01").distance {
				t.Fatal("actual end-of-tick owner handoff lost the following retained body")
			}
			current := nativeForeignControllerFrame(t, s)
			if _, err := current.sealOwners(follower, current.fleet.entries[follower].path); err != nil {
				t.Fatal("actual draining native owner handoff was refused", err)
			}
			return
		}
	}
	t.Fatal("actual virtual draining ownership handoff exceeded its frozen tick cap")

}

// These component controls use the actual station and ordinary move consumers.
// They do not claim live train admission or a complete Step integration.
func TestNativeForeignStationPresentation(t *testing.T) {
	t.Parallel()
	s, _, v := nativeForeignFixture(t)
	v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
	before := nativeForeignCabin(v)
	s.updateStationPhase(v)
	after := nativeForeignCabin(v)
	if before.Pod == after.Pod || after.Pod.StationPhase != DepartingBerth || after.Pod.ManeuverStationID != "foreign-station" {
		t.Fatal("actual station controller did not change the departure presentation")
	}
	if !nativeForeignCabinEqual(after, before) {
		t.Fatal("actual station presentation changed the bound cabin or physical facts")
	}
	after.Pod.Position.X++
	if nativeForeignCabinEqual(after, before) {
		t.Fatal("station presentation comparison waived a physical position change")
	}
}

func TestNativeForeignDerivedCabinMileage(t *testing.T) {
	t.Parallel()
	s, _, v := nativeForeignFixture(t)
	v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
	if nativeForeignCabin(v).RiddenMeters != 0 {
		t.Fatal("empty unrecorded cabin published passenger mileage")
	}
	v.Pod.Occupied = true
	v.Riders = []Request{{ID: 1, From: "foreign-station", To: "front-goal", PartySize: 1, PodID: v.Pod.ID, SharingConsent: PrivateConsent, Service: OnDemandService, BoardedTick: 1}}
	v.Boardings = []RiderBoarding{{BerthID: "foreign-1", MetersAtBoarding: 100}}
	v.riddenBase, v.RiddenMeters = 123.875, -1
	s.move(v)
	want := v.riddenBase + v.distance
	captured := nativeForeignCabin(v)
	if v.distance <= 0 || captured.RiddenMeters != want || captured.RiddenMeters == v.RiddenMeters || !slices.Equal(captured.Boardings, v.Boardings) || !slices.Equal(captured.Riders, v.Riders) {
		t.Fatal("actual occupied mileage or boarding facts did not bind their native source")
	}
	v.Riders[0].Completed = true
	frozen := v.riddenBase
	s.move(v)
	if captured = nativeForeignCabin(v); captured.RiddenMeters != frozen {
		t.Fatal("empty recorded history gained distance from later native movement")
	}
}

// The controller stage mirrors ordinary Step before any movement and release.
// It uses a validated native restore fixture, not a claimed Run-origin trajectory.
func TestNativeForeignActualArrival(t *testing.T) {
	t.Parallel()
	for _, lower := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "lower_future"}[lower], func(t *testing.T) {
			t.Parallel()
			network := Example()
			if lower {
				for i := range network.Lanes {
					if network.Lanes[i].ID == "market-approach" {
						network.Lanes[i].SpeedLimit = 2.5
					}
				}
			}
			s, err := NewFleet(network, []Placement{{ID: "moving", Class: CompactClass, StationID: "harbor", BerthID: "harbor-1"}})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.RequestJourneyOptions("moving", TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: PrivateConsent, Service: OnDemandService}); err != nil {
				t.Fatal(err)
			}
			v := &s.vehicles[0]
			for range 20000 {
				s.tick++
				if v.phaseTicks > 0 {
					v.phaseTicks--
				}
				s.formPlatoons()
				s.admit()
				s.platoonCaps()
				frame := nativeForeignControllerFrame(t, s)
				frame.tick = s.tick
				proof, err := frame.prepareProof(0, nativeCompactMember{})
				if err != nil {
					t.Fatal(err)
				}
				frame.proofs[v.Pod.ID] = proof
				sweep := proof.raw
				sweep.native = proof
				if err = couplingCheckForeignMotion(sweep); err != nil {
					t.Fatal(err)
				}
				owners := maps.Clone(s.owners)
				if departs(v.Pod.Activity) && v.phaseTicks == 0 && v.reservedThrough >= 0 {
					v.Pod.Occupied = v.Pod.Activity == Boarding || v.Pod.Activity == Continuing
					v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
				} else if v.Pod.Activity == Traveling {
					s.moveAndMeasure(v)
				}
				if !maps.Equal(owners, s.owners) {
					t.Fatal("actual ordinary arrival changed owners before end-of-tick release")
				}
				if err = frame.checkApplied(s); err != nil {
					t.Fatal(err)
				}
				if v.Pod.Activity == Unloading {
					if v.Pod.Position != proof.nextPosition || v.Pod.Speed != 0 || v.Pod.BerthID != v.destination.ID {
						t.Fatal("native arrival publication differs from its exact certificate")
					}
					return
				}
				s.releaseCleared()
				s.updateStationPhase(v)
			}
			t.Fatal("native arrival exceeded its frozen tick cap")
		})
	}
}
