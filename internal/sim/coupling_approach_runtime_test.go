package sim

import (
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

func newCouplingApproachJourney(t *testing.T, occupied bool) *Simulation {
	t.Helper()
	p, n := couplingApproachJourneyNetwork(t)
	s, err := p.NewFleetWithContracts([]Placement{
		{ID: "blocker", Class: CompactClass, StationID: "origin"},
		{ID: "front", Class: CompactClass, StationID: "front-origin"},
		{ID: "rear", Class: CompactClass, StationID: "rear-origin"},
	}, FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
		CouplingSites: []CouplingSite{n.sites["assembly"], n.sites["split"]}, CouplingCorridors: []CouplingCorridor{n.corridors["corridor"]}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	for _, trip := range []struct {
		id, goal string
		size     int
	}{{"blocker", "block-goal", 1}, {"front", "front-goal", 1}, {"rear", "rear-goal", 2}} {
		if occupied {
			if err := s.RequestJourneyOptions(trip.id, TripOptions{To: trip.goal, PartySize: trip.size, SharingConsent: PrivateConsent}); err != nil {
				t.Fatal(err)
			}
		} else {
			station, _ := s.station(trip.goal)
			if err := s.startEmptyMove(s.findVehicle(trip.id), emptyDestination{station: trip.goal, berth: station.Berths[0], reserveBerth: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.SetMotionRecording(true)
	return s
}

func checkCouplingApproachNativeBoundary(t *testing.T, s *Simulation) {
	t.Helper()
	if err := s.CouplingError(); err != nil {
		t.Fatalf("native coupling failed at tick %d: %v", s.tick, err)
	}
	if err := s.CheckContract(); err != nil {
		t.Fatalf("native cabin contract failed at tick %d: %v", s.tick, err)
	}
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatalf("native safety failed at tick %d: %v", s.tick, err)
	}
	if err := checkCouplingApproachHandoff(s); err != nil {
		t.Fatalf("approach link handoff failed at tick %d: %v", s.tick, err)
	}
	checkIncrementalOwners(t, s)
	view, err := s.CheckedSnapshot()
	if err != nil || view.Tick != s.tick || view.CouplingContract != s.CouplingContract() ||
		view.CouplingEnabled != s.CouplingEnabled() || len(view.CouplingGroups) != len(s.couplingGroups) {
		t.Fatalf("successful native tick lacks a coherent train view at tick %d: %v", s.tick, err)
	}
	frame, ok := s.MotionFrame()
	if !ok || frame.Tick != s.tick {
		t.Fatal("successful native tick did not publish its motion frame")
	}
	seen := make(map[string]bool)
	for _, sample := range frame.Samples {
		if seen[sample.ID] || sample.DistanceMeters < 0 {
			t.Fatal("native movement was measured twice or moved backward", sample)
		}
		seen[sample.ID] = true
	}
}

// All starting poses, links, grants, and movements come from ordinary departures.
func TestCouplingApproachRuntimeNaturalFormation(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "occupied"}[occupied], func(t *testing.T) {
			t.Parallel()
			s := newCouplingApproachJourney(t, occupied)
			var twin, cold *Simulation
			var waitTick, deadline int64 = -1, -1
			var formationTick, retirementTick int64
			var approach, moving, unlinked, formed, retired bool
			phases := make(map[couplingReservationPhase]bool)
			completed := make(map[int]bool)
			var party [2]Request
			var measured float64
			for range 18000 {
				s.Step()
				checkCouplingApproachNativeBoundary(t, s)
				frame, _ := s.MotionFrame()
				for _, sample := range frame.Samples {
					measured += sample.DistanceMeters
				}
				if math.Abs(measured-s.passengerDistanceMeters-s.emptyDistanceMeters) > 1e-7 {
					t.Fatal("native motion samples disagree with distance accounting")
				}
				for _, completion := range s.StepCompletions() {
					if completed[completion.RequestID] {
						t.Fatal("native completion replayed", completion)
					}
					completed[completion.RequestID] = true
				}
				for _, a := range s.couplingApproaches {
					approach = true
					if a.context.members[0].id != "front" || a.context.members[1].id != "rear" {
						t.Fatal("natural candidate selected unrelated pods")
					}
					moving = moving || a.state.Phase == couplingApproachMoving
					if a.state.WaitTick >= 0 {
						if waitTick < 0 {
							waitTick, deadline = a.state.WaitTick, a.state.DeadlineTick
						}
						if a.state.WaitTick != waitTick || a.state.DeadlineTick != deadline || deadline-waitTick != 300 {
							t.Fatal("actual intentional wait reset or changed its five-second bound")
						}
					}
					unlinked = unlinked || waitTick >= 0 && s.findVehicle("rear").link.leader == 0 && !s.holdsPending(s.findVehicle("rear"))
					if twin == nil && a.state.Phase == couplingApproachMoving {
						twin = s.Clone()
					}
				}
				if len(s.couplingGroups) != 0 {
					g := s.couplingGroups[0]
					phases[g.state.Phase] = true
					if !formed {
						formed = true
						formationTick = s.tick
						var legTicks [5]uint64
						for i, leg := range g.context.legs {
							legTicks[i] = leg.ticks
						}
						t.Logf("actual formation tick=%d totalMechanicalTicks=%d legTicks=%v", formationTick, g.context.ticks, legTicks)
						if !approach || !moving || !unlinked || waitTick < 0 || g.formationTick > deadline || g.formationTick > 12000 || s.platoonLinks != 0 {
							t.Fatal("formation lacks actual powered staging, retired link, or original deadline")
						}
						for i, id := range []string{"front", "rear"} {
							v := s.findVehicle(id)
							if occupied {
								if len(v.Riders) != 1 || v.Riders[0].PartySize != i+1 || v.Riders[0].SharingConsent != PrivateConsent {
									t.Fatal("formation changed an immutable whole private party")
								}
								party[i] = v.Riders[0]
							}
							member := g.context.reservation.members[i]
							if (v.RidersAboard() > 0 || len(v.Boardings) != 0) && member.Vehicle.RiddenMeters != v.riddenMeters() ||
								member.nativeRiddenBase == nil || *member.nativeRiddenBase != v.riddenBase {
								t.Fatal("formation omitted actual native mileage provenance")
							}
						}
						var result RestoreResult
						var err error
						n := s.couplingNetwork
						saved := s.ExportState()
						for _, pod := range saved.Pods {
							if pod.ID == "front" || pod.ID == "rear" {
								if pod.Waiting || pod.Platoon != nil || pod.CompactQueue != nil || pod.Released {
									t.Fatalf("committed native member retained ordinary refusal fields: %+v", pod)
								}
							}
						}
						cold, result, err = RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: saved,
							CouplingContract: n.contract, CouplingEnabled: true, CouplingSites: []CouplingSite{n.sites["assembly"], n.sites["split"]},
							CouplingCorridors: []CouplingCorridor{n.corridors["corridor"]}})
						if err != nil || result.Tier != RestorePhysical {
							t.Fatal("actual formed native state failed strict cold restore", result, err)
						}
						cold.SetMotionRecording(true)
					}
					if occupied {
						for i, id := range []string{"front", "rear"} {
							if !slices.Equal(s.findVehicle(id).Riders, []Request{party[i]}) {
								t.Fatal("committed motion changed order, party, consent, or timing")
							}
						}
					}
				} else if formed && !retired {
					retired, retirementTick = true, s.tick
				}
				if twin != nil {
					// The clone starts at this same boundary, then follows each real tick.
					if twin.tick < s.tick {
						twin.Step()
					}
					checkCouplingApproachNativeBoundary(t, twin)
					if !reflect.DeepEqual(twin.ExportState(), s.ExportState()) || !maps.Equal(twin.owners, s.owners) {
						t.Fatal("live approach clone continuation diverged", s.tick)
					}
				}
				if cold != nil && cold.tick < s.tick {
					cold.Step()
					checkCouplingApproachNativeBoundary(t, cold)
				}
				if retired && s.findVehicle("front").Pod.Activity == Idle && s.findVehicle("rear").Pod.Activity == Idle &&
					(cold == nil || len(cold.couplingGroups) == 0 && cold.findVehicle("front").Pod.Activity == Idle && cold.findVehicle("rear").Pod.Activity == Idle) {
					for _, phase := range []couplingReservationPhase{couplingClosing, couplingLatching, couplingConnected, couplingUnlatching, couplingOpening, couplingDraining} {
						if !phases[phase] {
							t.Fatal("actual journey skipped a mechanical phase", phase)
						}
					}
					if occupied && (!completed[party[0].ID] || !completed[party[1].ID] || s.boarded != 3) {
						t.Fatal("native party completion or boarding conservation failed")
					}
					t.Logf("natural formation=%d wait=%d deadline=%d retirement=%d completed continuation=%d", formationTick, waitTick, deadline, retirementTick, s.tick)
					return
				}
			}
			if len(s.couplingGroups) != 0 {
				t.Logf("last committed state=%+v", s.couplingGroups[0].state)
			}
			if len(s.couplingApproaches) != 0 {
				t.Logf("last approach=%+v", s.couplingApproaches[0].state)
			}
			t.Fatalf("natural journey missed its separate 18000-tick continuation bound: approach=%t moving=%t unlinked=%t formed=%t retired=%t front=%+v rear=%+v", approach, moving, unlinked, formed, retired, s.findVehicle("front").Pod, s.findVehicle("rear").Pod)
		})
	}
}

// Controlled native stages isolate caller guards; they do not prove reachability.
func controlledCouplingApproachRuntime(t *testing.T) *Simulation {
	t.Helper()
	input := couplingApproachFixture(t, false)
	s := input.Simulation
	s.couplingNetwork, s.couplingEnabled = input.Network, true
	s.discoverCouplingApproaches()
	if len(s.couplingApproaches) != 1 || len(s.couplingAttempts) != 1 {
		t.Fatal("controlled stage did not discover one bounded native candidate")
	}
	return s
}

func TestCouplingApproachRuntimeAdmissionAndLinkGuards(t *testing.T) {
	t.Parallel()
	s := controlledCouplingApproachRuntime(t)
	front, rear := s.findVehicle("front"), s.findVehicle("rear")
	c := s.couplingApproaches[0].context
	before := maps.Clone(s.owners)
	through := rear.reservedThrough
	s.grant(intent{index: 1, block: c.ceiling + 1, through: c.ceiling + 1})
	if rear.reservedThrough != through || !maps.Equal(before, s.owners) || rear.Pod.WaitReason != TrackOccupied {
		t.Fatal("actual grant wrote beyond the rear staging ceiling")
	}
	link := rear.link
	s.extendLink(rear, front)
	if rear.link != link {
		t.Fatal("actual extension changed an active staging link")
	}
	// A drained candidate cannot relink through the ordinary caller.
	s.unlink(rear)
	s.tryLink(1, 0)
	if rear.link.leader != 0 || front.follower != 0 {
		t.Fatal("actual ordinary caller relinked a staging member")
	}
}

func TestCouplingApproachRuntimeProducerAndPublicationGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*couplingApproachTransition)
	}{
		{"command", func(a *couplingApproachTransition) { a.step.Front.Distance++ }},
		{"clock", func(a *couplingApproachTransition) { a.step.State.Tick++ }},
		{"hold", func(a *couplingApproachTransition) { a.step.HoldFront = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := controlledCouplingApproachRuntime(t)
			s.grant(intent{index: 0, block: 2, through: 2})
			s.tick++
			s.platoonCaps()
			transitions, err := s.planCouplingApproaches()
			if err != nil || len(transitions) != 1 || transitions[0].step.Front == nil {
				t.Fatal("controlled producer did not plan a real front command", err)
			}
			fleet, err := prepareNativeForeignFleetBound(s, s.couplingNetwork, s.orderContract)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := buildNativeForeignApproachTick(s, fleet, transitions)
			if err != nil || len(frame.proofs) != len(s.vehicles) || len(frame.fleet.pairs) != 0 {
				t.Fatal("actual whole-fleet frame requires a fictitious committed group", err)
			}
			tc.change(&transitions[0])
			if _, err := buildNativeForeignApproachTick(s, fleet, transitions); err == nil {
				t.Fatal("actual frame accepted an approach caller override")
			}
		})
	}
	s := controlledCouplingApproachRuntime(t)
	s.SetMotionRecording(true)
	before, _ := s.MotionFrame()
	s.couplingApproaches[0].state.Tick--
	s.Step()
	after, _ := s.MotionFrame()
	if s.CouplingError() == nil || !s.paused || !reflect.DeepEqual(before, after) {
		t.Fatal("failed approach tick published an unproved motion frame")
	}
	tick := s.tick
	s.Step()
	if s.tick != tick {
		t.Fatal("failed native approach continued advancing")
	}
}

func TestCouplingApproachRuntimeCheckpointGuard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Simulation)
	}{
		{"proposed full prefix", func(s *Simulation) {
			v := s.findVehicle("front")
			v.Route = slices.Repeat(v.Route[:1], newRouteLimits(s.network).pod+1)
		}},
		{"existing group full prefix", func(s *Simulation) {
			other := *s.findVehicle("rear")
			other.Pod.ID = "other"
			other.Route = slices.Repeat(other.Route[:1], newRouteLimits(s.network).pod+1)
			s.vehicles = append(s.vehicles, other)
			s.couplingGroups = []couplingNativeGroup{{context: &couplingMotionContext{reservation: couplingReservationPlan{members: [2]couplingMemberSnapshot{{Vehicle: Vehicle{Pod: Pod{ID: "other"}}}, {Vehicle: Vehicle{Pod: Pod{ID: "other-partner"}}}}}}}}
		}},
		{"waiting route", func(s *Simulation) {
			s.waiting = []waitingTrip{{route: slices.Repeat(s.findVehicle("front").Route[:1], newRouteLimits(s.network).trip+1)}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, false)
			c, state := runCouplingApproachTest(t, input)
			s := input.Simulation
			s.couplingNetwork = input.Network
			tc.change(s)
			before := maps.Clone(s.owners)
			group, _, err := s.prepareCouplingAdoption(couplingApproachTransition{context: c, step: couplingApproachStep{State: state, Ready: true}}, 0)
			if !errors.Is(err, errCouplingCheckpointWork) || group.context != nil || !maps.Equal(before, s.owners) {
				t.Fatal("actual adoption bypassed checkpoint work before reservation preparation", err)
			}
		})
	}
}

func TestCouplingApproachRuntimeDrainAndPolicyAbort(t *testing.T) {
	t.Parallel()
	s := controlledCouplingApproachRuntime(t)
	a := &s.couplingApproaches[0]
	rear := s.findVehicle("rear")
	a.state.Phase = couplingApproachWaiting
	if !s.holdsPending(rear) {
		t.Fatal("controlled drain lacks an unresolved borrowed owner")
	}
	before := maps.Clone(s.owners)
	s.finishCouplingApproachLinks()
	if rear.link.leader == 0 || !rear.link.draining || !maps.Equal(before, s.owners) {
		t.Fatal("drain retired or transferred owners before the global handoff")
	}
	// Restore the authored initial phase. Native Step owns all later motion.
	a.state.Phase = couplingApproachGrant
	s.grant(intent{index: 0, block: 2, through: 2})
	s.SetMotionRecording(true)
	s.Step()
	if s.CouplingError() != nil || len(s.couplingApproaches) != 1 || s.couplingApproaches[0].state.Phase != couplingApproachMoving {
		t.Fatal("controlled native stage did not begin its actual powered command", s.CouplingError())
	}
	front := s.findVehicle("front")
	speed := front.Pod.Speed
	if err := s.SetCouplingEnabled(false); err != nil {
		t.Fatal(err)
	}
	s.Step()
	if s.CouplingError() != nil || front.Pod.Speed != max(0, speed-acceleration/TicksPerSecond) || len(s.couplingGroups) != 0 {
		t.Fatal("actual policy change bypassed bounded braking", s.CouplingError())
	}
	if err := s.SetCouplingEnabled(true); err != nil {
		t.Fatal(err)
	}
	for range 1000 {
		s.Step()
		if s.CouplingError() != nil {
			t.Fatal("native abort continuation failed", s.CouplingError())
		}
		if len(s.couplingApproaches) == 0 {
			break
		}
	}
	if len(s.couplingApproaches) != 0 || len(s.couplingGroups) != 0 || len(s.couplingAttempts) != 1 {
		t.Fatal("policy abort renewed a candidate or lost its original attempt")
	}
	s.discoverCouplingApproaches()
	if len(s.couplingApproaches) != 0 {
		t.Fatal("candidate loss restarted the same journey attempt")
	}
	s.Reset()
	if len(s.couplingAttempts) != 0 || len(s.couplingApproaches) != 0 || s.CouplingContract() != CompactPairV1CouplingContract || !s.CouplingEnabled() {
		t.Fatal("reset retained transient approach state or changed the contract")
	}
}

func TestCouplingApproachRuntimePartnerReplacementKeepsAttempt(t *testing.T) {
	t.Parallel()
	s := controlledCouplingApproachRuntime(t)
	attempt := s.couplingAttempts["front"]
	// Adversarial replacement remains the same front journey and staged link.
	s.couplingApproaches = nil
	s.vehicles[1].Pod.ID = "replacement"
	s.vehicleIndexes = map[string]int{"front": 0, "replacement": 1}
	s.discoverCouplingApproaches()
	if len(s.couplingApproaches) != 0 || s.couplingAttempts["front"] != attempt {
		t.Fatal("a new partner renewed the same front journey attempt")
	}
	// The memo resets only at a real ordinary departure boundary, or Reset.
	front := s.findVehicle("front")
	front.Pod.Activity, front.Pod.Occupied, front.Pod.BerthID = DepartingEmpty, false, front.origin.ID
	front.distance, front.originReleased = 0, false
	s.couplingEnabled = false
	s.discoverCouplingApproaches()
	if len(s.couplingAttempts) != 0 {
		t.Fatal("disabled policy hid the next ordinary journey epoch")
	}
}

// The two native stages are authored controls, not natural multi-pair recruitment.
func controlledCouplingApproachBatch(t *testing.T, crossing bool) *Simulation {
	t.Helper()
	inputs := [2]couplingReservationInput{couplingMotionFixture(t, false, false), couplingMotionFixture(t, false, false)}
	geometry := CouplingGeometryInput{Contract: CompactPairV1CouplingContract}
	var placements []Placement
	for i := range inputs {
		prefix := string(rune('a'+i)) + "-"
		nativeForeignRenamePairInput(&inputs[i], prefix, float64(i)*1000, &geometry)
		// An exact 128-meter past prefix keeps the authored drain start
		// representable on the existing terminal distance grid.
		for j := range geometry.Network.Nodes {
			if geometry.Network.Nodes[j].ID == prefix+"origin-exit" {
				geometry.Network.Nodes[j].Position = Point{X: -64, Y: float64(i) * 1000}
			}
		}
		geometry.Network.Lanes = append(geometry.Network.Lanes, Lane{ID: prefix + "origin-back", From: prefix + "origin-exit", To: prefix + "a", SpeedLimit: 7.123456789, VehicleClasses: classBit(string(CompactClass))})
		for _, m := range &inputs[i].Members {
			placements = append(placements, Placement{ID: m.Vehicle.Pod.ID, Class: CompactClass, StationID: m.DestinationStation})
		}
	}
	if crossing {
		// Author perpendicular corridors with one real shared future junction.
		for i := range geometry.Network.Nodes {
			node := &geometry.Network.Nodes[i]
			if len(node.ID) >= 2 && node.ID[:2] == "b-" {
				x, y := node.Position.X, node.Position.Y-1000
				node.Position = Point{X: 210 - y, Y: x - 210}
			}
		}
		geometry.Network.Nodes = slices.DeleteFunc(geometry.Network.Nodes, func(node Node) bool { return node.ID == "b-b" })
		for i := range geometry.Network.Lanes {
			lane := &geometry.Network.Lanes[i]
			if lane.From == "b-b" {
				lane.From = "a-b"
			}
			if lane.To == "b-b" {
				lane.To = "a-b"
			}
		}
	}
	p, err := PrepareNetwork(geometry.Network)
	if err != nil {
		t.Fatal(err)
	}
	geometry.Network = p.Network()
	n, err := prepareCouplingReservations(p, geometry)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.NewFleet(placements)
	if err != nil {
		t.Fatal(err)
	}
	s.couplingNetwork, s.couplingEnabled = n, true
	s.platooning, s.platoonLimit, s.tick = PlatooningVirtual, 2, inputs[0].Tick
	s.owners = make(map[resource]resourceOwner)
	for group := range inputs {
		prefix := string(rune('a'+group)) + "-"
		for i, m := range &inputs[group].Members {
			v := s.findVehicle(m.Vehicle.Pod.ID)
			v.Vehicle = m.Vehicle
			route := []Lane{n.lanes[prefix+"origin-out"].lane, n.lanes[prefix+"origin-back"].lane}
			for _, lane := range m.Vehicle.Route {
				route = append(route, n.lanes[lane.ID].lane)
			}
			s.setVehicleRoute(v, route)
			v.origin, v.journeyOrigin, v.destination, v.destinationStation = m.Origin, m.Origin, m.Destination, m.DestinationStation
			v.originReleased, v.pending = true, -1
			v.distance = v.blocks.lanes[2].start + n.sites[prefix+"assembly"].RearStagingMeters
			if i == 1 {
				v.distance -= 12.02
			}
			v.blockIndex, v.reservedThrough = v.blocks.laneFirst(2)+1, v.blocks.laneFirst(2)+1
			couplingApproachTestPose(t, v)
			v.routeReleases = make(map[resource]float64)
			for _, b := range v.blocks.span(0, v.reservedThrough+1) {
				for _, r := range b.resources {
					release := resourceReleaseDistance(b, r)
					if release > v.distance {
						v.retainRouteResource(r, release)
						if s.owners[r].isZero() {
							s.owners[r] = podResourceOwner(v.Pod.ID)
						}
					}
				}
			}
			for _, r := range berthResources(v.destination) {
				s.owners[r] = podResourceOwner(v.Pod.ID)
			}
		}
		front, rear := s.findVehicle(prefix+"front"), s.findVehicle(prefix+"rear")
		link, ok := s.planLink(linkPlan{v: rear, leader: front, lane: 2, leaderLane: 2})
		if !ok {
			t.Fatal("controlled full-prefix pair has no ordinary link certificate")
		}
		s.link(2*group+1, 2*group, link)
	}
	for range 1200 {
		s.Step()
		if s.CouplingError() != nil {
			t.Fatal("controlled batch stage failed native motion", s.CouplingError())
		}
		ready := len(s.couplingApproaches) == 2
		for _, a := range s.couplingApproaches {
			rear := s.findVehicle(a.context.members[1].id)
			ready = ready && a.state.Phase == couplingApproachWaiting && rear.link.leader == 0 && rear.Pod.Speed == 0 && rear.distance == a.context.rearTarget
		}
		if ready {
			return s
		}
		if len(s.couplingGroups) != 0 {
			t.Fatal("controlled batch did not share one ready boundary")
		}
	}
	t.Fatal("controlled full-prefix batch never reached its ready boundary")
	return nil
}

func TestCouplingApproachRuntimeCombinedCheckpointAdmission(t *testing.T) {
	t.Parallel()
	s := controlledCouplingApproachBatch(t, false)
	blocks, budget := physicalBlockBudget(s)
	fullCost := 0
	for _, v := range s.vehicles {
		start, _, _ := s.savedStart(&v)
		if start < 2 {
			t.Fatal("controlled guard does not distinguish full and ordinary prefixes")
		}
		for _, lane := range v.Route {
			fullCost += blocks[s.graph.lanes[lane.ID]]
		}
	}
	remaining := budget - fullCost + 1
	for remaining > 0 {
		route := s.vehicles[0].Route
		count := min(remaining, len(route))
		s.waiting = append(s.waiting, waitingTrip{route: slices.Clone(route[:count])})
		remaining -= count
	}
	for _, a := range s.couplingApproaches {
		pair := [2]string{a.context.members[0].id, a.context.members[1].id}
		if err := s.checkCouplingCheckpointWork(pair); err != nil {
			t.Fatal("single-pair control does not fit", err)
		}
	}
	s.tick++
	s.platoonCaps()
	before := maps.Clone(s.owners)
	tick, err := s.planNativeCouplingTick()
	if err != nil || tick == nil || len(tick.adopted) != 2 || !tick.adopted[0] || tick.adopted[1] || !maps.Equal(before, s.owners) {
		logCouplingApproachAdmission(t, s, tick)
		t.Fatal("actual caller did not admit a fitting subset without owner writes", err)
	}
	if !tick.approaches[1].step.HoldFront || tick.approaches[1].step.State.WaitTick != s.couplingApproaches[1].state.WaitTick || tick.approaches[1].step.State.DeadlineTick != s.couplingApproaches[1].state.DeadlineTick {
		t.Fatal("batch refusal renewed the rejected proposal's original hold")
	}
	// Both actual sweeps are zero at this boundary. The normal publication
	// check still proves them before the first controlled group adopts.
	if err := s.finishNativeCoupling(tick); err != nil || len(s.couplingGroups) != 1 {
		t.Fatal("controlled fitting group did not publish its checked boundary", err)
	}
	before = maps.Clone(s.owners)
	if _, _, err := s.prepareCouplingAdoption(tick.approaches[1], 1); !errors.Is(err, errCouplingCheckpointWork) || !maps.Equal(before, s.owners) {
		t.Fatal("actual later adoption trimmed an existing group's full prefix", err)
	}
}

func TestCouplingApproachRuntimeSharedFutureAdmission(t *testing.T) {
	t.Parallel()
	s := controlledCouplingApproachBatch(t, true)
	s.tick++
	s.platoonCaps()
	before := maps.Clone(s.owners)
	tick, err := s.planNativeCouplingTick()
	if err != nil || tick == nil || len(tick.adopted) != 2 || !tick.adopted[0] || tick.adopted[1] || !maps.Equal(before, s.owners) {
		logCouplingApproachAdmission(t, s, tick)
		t.Fatal("shared future junction did not produce ordinary proposal contention", err)
	}
	if !tick.approaches[1].step.HoldFront || tick.approaches[1].step.State.WaitTick != s.couplingApproaches[1].state.WaitTick || tick.approaches[1].step.State.DeadlineTick != s.couplingApproaches[1].state.DeadlineTick {
		t.Fatal("shared-owner refusal changed the original intentional wait")
	}
}

func logCouplingApproachAdmission(t *testing.T, s *Simulation, tick *couplingNativeTick) {
	t.Helper()
	if tick != nil {
		t.Logf("planned adoption=%v", tick.adopted)
	}
	for i, a := range s.couplingApproaches {
		front, rear := s.findVehicle(a.context.members[0].id), s.findVehicle(a.context.members[1].id)
		input := a.context.formationInput(s, front, rear)
		reservation, reservationErr := planCouplingReservation(input)
		t.Logf("candidate%d tick=%d state=%+v front=%+v rear=%+v reservation=%v", i, s.tick, a.state, front.Pod, rear.Pod, reservationErr)
		if reservationErr == nil {
			logCouplingApproachDrain(t, reservation, input)
		}
		if tick != nil {
			t.Logf("planned approach=%+v", tick.approaches[i].step)
			_, _, adoptionErr := s.prepareCouplingAdoption(tick.approaches[i], i)
			t.Logf("adoption refusal=%v", adoptionErr)
		}
	}
}

// Report existing drain guards from the actual reservation without owner writes.
func logCouplingApproachDrain(t *testing.T, reservation couplingReservationPlan, input couplingReservationInput) {
	t.Helper()
	c := &couplingMotionContext{reservation: reservation}
	if err := c.closeExits(input.Owners); err != nil {
		t.Logf("drain closure refusal=%v", err)
		return
	}
	profile, _ := LookupCouplingProfile(reservation.network.contract)
	start := reservation.OpeningStops
	for _, order := range [][2]int{{0, 1}, {1, 0}} {
		middle := start
		middle[order[0]] = c.terminal[order[0]]
		_, firstErr := c.prepareLeg(couplingDraining, [2]bool{order[0] == 0, order[0] == 1}, start, middle, profile.Acceleration, math.Inf(1))
		_, secondErr := c.prepareLeg(couplingDraining, [2]bool{order[1] == 0, order[1] == 1}, middle, c.terminal, profile.Acceleration, math.Inf(1))
		t.Logf("drain order=%v opening=%v terminal=%v through=%v firstSeparation=%v secondSeparation=%v firstLeg=%v secondLeg=%v", order, start, c.terminal, c.through, c.serialSeparation(order[0], start, middle), c.serialSeparation(order[1], middle, c.terminal), firstErr, secondErr)
	}
}

// Only the old attempt memo is authored. Orders, arrivals, and new departures
// use the ordinary native API on a known, enabled coupling network.
func TestCouplingApproachRuntimeOccupiedJourneyEpoch(t *testing.T) {
	t.Parallel()
	t.Run("completed journey starts new boarding", func(t *testing.T) {
		t.Parallel()
		s := newCouplingApproachEpochFleet(t)
		if err := s.RequestJourneyOptions("01", TripOptions{To: "garden", PartySize: 2, SharingConsent: PrivateConsent}); err != nil {
			t.Fatal(err)
		}
		advanceCouplingApproachEpoch(t, s, func() bool { return s.findVehicle("01").Pod.Activity == Traveling })
		memo := couplingApproachAttempt{context: &couplingApproachContext{}}
		s.couplingAttempts = map[string]couplingApproachAttempt{"01": memo}
		advanceCouplingApproachEpoch(t, s, func() bool { return s.findVehicle("01").Pod.Activity == Idle })
		v := s.findVehicle("01")
		if s.completed != 1 || v.Pod.StationID != "garden" || s.couplingAttempts["01"] != memo {
			t.Fatal("ordinary completion changed the original attempt memo")
		}
		if err := s.RequestJourneyOptions("01", TripOptions{To: "market", PartySize: 2, SharingConsent: PrivateConsent}); err != nil {
			t.Fatal(err)
		}
		if v.Pod.Activity != Boarding || v.Pod.Occupied || v.distance != 0 || v.riddenBase != 0 || v.RidersAboard() != 1 {
			t.Fatal("new request did not enter native initial boarding")
		}
		s.Step()
		checkCouplingApproachNativeBoundary(t, s)
		if len(s.couplingAttempts) != 0 {
			t.Fatal("new occupied journey retained the previous attempt")
		}
		advanceCouplingApproachEpoch(t, s, func() bool { return v.Pod.Activity == Traveling })
		if !v.Pod.Occupied || v.Riders[0].PartySize != 2 || v.Riders[0].SharingConsent != PrivateConsent || len(s.couplingAttempts) != 0 {
			t.Fatal("new actual departure changed its party or restored the old attempt")
		}
	})
	t.Run("intermediate occupied continuation retains attempt", func(t *testing.T) {
		t.Parallel()
		s := newCouplingApproachEpochFleet(t)
		if err := s.SetSharedRidePartyLimit(2); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSharedRideMode(SharedRideDropOffs, DefaultSharedRideMaxStops); err != nil {
			t.Fatal(err)
		}
		for _, destination := range []string{"market", "garden"} {
			if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: destination, PartySize: 1, SharingConsent: SharedConsent}); err != nil {
				t.Fatal(err)
			}
		}
		v := s.findVehicle("01")
		if len(v.Riders) != 2 || !slices.Equal(v.Stops, []string{"garden", "market"}) {
			t.Fatal("actual submissions did not create one two-stop cohort")
		}
		advanceCouplingApproachEpoch(t, s, func() bool { return v.Pod.Activity == Traveling })
		memo := couplingApproachAttempt{context: &couplingApproachContext{}}
		s.couplingAttempts = map[string]couplingApproachAttempt{"01": memo}
		advanceCouplingApproachEpoch(t, s, func() bool { return v.Pod.Activity == Unloading && v.Pod.StationID == "garden" && v.phaseTicks == 1 })
		// Expose the existing ordinary transition at the stopped intermediate berth.
		// An unblocked Step normally consumes Continuing at the same boundary.
		s.alight(v)
		s.continueJourney(v)
		if v.Pod.Activity != Continuing || !v.Pod.Occupied || v.riddenBase <= 0 || v.RidersAboard() != 1 {
			t.Fatal("controlled ordinary continuation lost its active cohort")
		}
		s.Step()
		checkCouplingApproachNativeBoundary(t, s)
		if s.couplingAttempts["01"] != memo {
			t.Fatal("intermediate continuation renewed the original attempt")
		}
	})
}

func newCouplingApproachEpochFleet(t *testing.T) *Simulation {
	t.Helper()
	network := Example()
	for i := range network.Lanes {
		if network.Lanes[i].ID == "return-to-parking" || network.Lanes[i].ID == "parking-through" {
			network.Lanes[i].VehicleClasses = classBit(string(CompactClass))
		}
	}
	s, err := NewFleetWithContracts(network, []Placement{{ID: "01", Class: CompactClass, StationID: "harbor"}}, FleetContracts{
		CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true,
		CouplingSites: []CouplingSite{{ID: "assembly", LaneID: "return-to-parking", StartMeters: 30, EndMeters: 120, RearStagingMeters: 60, FrontStagingMeters: 72},
			{ID: "split", LaneID: "parking-through", StartMeters: 50, EndMeters: 140, RearStagingMeters: 70, FrontStagingMeters: 82}},
		CouplingCorridors: []CouplingCorridor{{ID: "corridor", AssemblySiteID: "assembly", SplitSiteID: "split", LaneIDs: []string{"return-to-parking", "parking-through"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	s.SetMotionRecording(true)
	if s.couplingNetwork == nil || !s.couplingEnabled || s.platooning != PlatooningVirtual {
		t.Fatal("journey epoch control bypasses coupling discovery")
	}
	return s
}

func advanceCouplingApproachEpoch(t *testing.T, s *Simulation, done func() bool) {
	t.Helper()
	for range 600 * TicksPerSecond {
		s.Step()
		checkCouplingApproachNativeBoundary(t, s)
		if done() {
			return
		}
	}
	t.Fatal("ordinary journey epoch control did not reach its boundary")
}
