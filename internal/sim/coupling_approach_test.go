package sim

import (
	"errors"
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

// This authored native stage tests the component. It does not prove recruitment.
func couplingApproachFixture(t *testing.T, occupied bool) couplingApproachPrepareInput {
	t.Helper()
	formation := couplingMotionFixture(t, occupied, false)
	n := formation.Network
	fleet := []Placement{{ID: "front", Class: CompactClass, StationID: "front-goal"}, {ID: "rear", Class: CompactClass, StationID: "rear-goal"}}
	s, err := n.prepared.NewFleet(fleet)
	if err != nil {
		t.Fatal(err)
	}
	s.tick, s.platooning, s.platoonLimit = formation.Tick, PlatooningVirtual, 2
	s.owners = make(map[resource]resourceOwner)
	stage := n.sites["assembly"]
	for i := range s.vehicles {
		m := formation.Members[i]
		v := &s.vehicles[i]
		v.Vehicle = m.Vehicle
		v.origin, v.journeyOrigin, v.destination, v.destinationStation = m.Origin, m.Origin, m.Destination, m.DestinationStation
		v.originReleased = true
		v.blocks, v.routeLengths = s.routeBlocks(v.Route)
		v.distance = stage.RearStagingMeters
		if i == 1 {
			v.distance -= 12.02
		}
		v.blockIndex, v.reservedThrough = 1, 1
		v.pending = -1
		v.routeReleases = make(map[resource]float64)
		couplingApproachTestPose(t, v)
		for _, b := range v.blocks.span(0, v.reservedThrough+1) {
			for _, r := range b.resources {
				release := resourceReleaseDistance(b, r)
				if release <= v.distance {
					continue
				}
				v.retainRouteResource(r, release)
				if s.owners[r].isZero() {
					s.owners[r] = podResourceOwner(v.Pod.ID)
				}
			}
		}
		if !occupied {
			for _, r := range berthResources(v.destination) {
				s.owners[r] = podResourceOwner(v.Pod.ID)
			}
		}
	}
	link, ok := s.planLink(linkPlan{v: &s.vehicles[1], leader: &s.vehicles[0], lane: 0, leaderLane: 0})
	if !ok {
		t.Fatal("authored component pair lacks an ordinary link proof")
	}
	s.link(1, 0, link)
	return couplingApproachPrepareInput{Simulation: s, Network: n, Prepared: n.prepared, CorridorID: "corridor", Members: [2]string{"front", "rear"}, Enabled: true}
}

func couplingApproachTestPose(t *testing.T, v *vehicle) {
	t.Helper()
	lane, point, _, err := couplingMotionPose(&v.blocks, v.distance)
	if err != nil {
		t.Fatal(err)
	}
	v.Pod.LaneID, v.Pod.Position = v.Route[lane].ID, point
	v.Pod.LaneDistance = v.distance - v.blocks.lanes[lane].start
}

// This adapter is test-only. Production Step integration belongs to the caller.
func applyCouplingApproachTestStep(t *testing.T, s *Simulation, step couplingApproachStep) {
	t.Helper()
	front, rear := s.findVehicle("front"), s.findVehicle("rear")
	if step.RequestDrain && rear.link.leader != 0 {
		rear.link.draining = true
	}
	if step.Unlink {
		if s.holdsPending(rear) {
			t.Fatal("unlink proposed before native ownership handoff")
		}
		s.unlink(rear)
	}
	s.platoonCaps()
	if command := step.Front; command != nil {
		if command.PreviousDistance != front.distance || command.PreviousSpeed != front.Pod.Speed {
			t.Fatal("front command is not bound to native previous motion")
		}
		front.distance, front.Pod.Speed = command.Distance, command.Speed
		for front.blockIndex < front.reservedThrough && front.distance >= front.blocks.end(front.blockIndex) {
			front.blockIndex++
		}
		couplingApproachTestPose(t, front)
		travel := command.Distance - command.PreviousDistance
		s.recordMotion(MotionSample{ID: front.Pod.ID, Class: front.Pod.Class, DistanceMeters: travel, StartSpeed: command.PreviousSpeed, EndSpeed: command.Speed})
		if front.Pod.Occupied {
			s.passengerDistanceMeters += travel
		} else {
			s.emptyDistanceMeters += travel
		}
	} else if !step.HoldFront && !step.Ready {
		s.moveAndMeasure(front)
	}
	if !step.Ready {
		s.moveAndMeasure(rear)
	}
	s.tick = step.State.Tick
	s.releaseCleared()
	checkIncrementalOwners(t, s)
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
}

func runCouplingApproachTest(t *testing.T, input couplingApproachPrepareInput) (*couplingApproachContext, couplingApproachState) {
	t.Helper()
	c, state, err := prepareCouplingApproach(input)
	if err != nil {
		t.Fatal(err)
	}
	s := input.Simulation
	s.grant(intent{index: 0, block: 2, through: 2})
	for range 1200 {
		step, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		if step.Aborted {
			t.Fatalf("component approach aborted: %s", step.Reason)
		}
		applyCouplingApproachTestStep(t, s, step)
		state = step.State
		if step.Ready {
			return c, state
		}
	}
	t.Fatal("component approach did not finish within its test cap")
	return nil, couplingApproachState{}
}

func TestCouplingApproachComponentConservation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		occupied bool
	}{{"empty", false}, {"occupied", true}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, test.occupied)
			s := input.Simulation
			routes, cabins := [2][]Lane{}, [2]Vehicle{}
			starts := [2]float64{s.vehicles[0].distance, s.vehicles[1].distance}
			for i, v := range s.vehicles {
				routes[i], cabins[i] = cloneLanes(v.Route), couplingApproachCabin(&v)
			}
			c, state := runCouplingApproachTest(t, input)
			if state.WaitTick < 0 || state.DeadlineTick-state.WaitTick != 300 || state.Tick > state.DeadlineTick ||
				s.vehicles[0].distance != c.target || s.vehicles[1].distance != c.rearTarget || s.platoonLinks != 0 {
				t.Fatalf("wrong exact endpoint, deadline, or retirement: %+v", state)
			}
			wantDistance := 0.0
			for i, v := range s.vehicles {
				if v.Pod.Speed != 0 || !reflect.DeepEqual(v.Route, routes[i]) || !reflect.DeepEqual(couplingApproachCabin(&v), cabins[i]) {
					t.Fatal("component changed full route or cabin facts")
				}
				wantDistance += v.distance - starts[i]
			}
			got := s.emptyDistanceMeters
			if test.occupied {
				got = s.passengerDistanceMeters
				if s.vehicles[1].Riders[0].PartySize != 2 || s.vehicles[1].Riders[0].SharingConsent != PrivateConsent {
					t.Fatal("whole private party changed")
				}
			}
			if math.Abs(got-wantDistance) > 1e-10 {
				t.Fatalf("actual member distance=%v, metric=%v", wantDistance, got)
			}
		})
	}
}

func TestCouplingApproachPreparationGuards(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*testing.T, *couplingApproachPrepareInput)
	}{
		{"disabled", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Enabled = false }},
		{"virtual_off", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.platooning = PlatooningOff }},
		{"wrong_network", func(_ *testing.T, i *couplingApproachPrepareInput) { prepared := *i.Prepared; i.Prepared = &prepared }},
		{"nil_prepared", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Network.prepared, i.Prepared = nil, nil }},
		{"unknown_corridor", func(_ *testing.T, i *couplingApproachPrepareInput) { i.CorridorID = "missing" }},
		{"duplicate_members", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Members[1] = i.Members[0] }},
		{"imported_endpoint", func(t *testing.T, i *couplingApproachPrepareInput) {
			t.Helper()
			v := &i.Simulation.vehicles[0]
			v.distance += 12
			v.blockIndex = 2
			couplingApproachTestPose(t, v)
		}},
		// A coherent pose 5 mm short of the staging frontier. Grants,
		// retention, and virtual clearance stay valid.
		{"off_staging", func(t *testing.T, i *couplingApproachPrepareInput) {
			t.Helper()
			v := &i.Simulation.vehicles[0]
			v.distance -= 0.005
			couplingApproachTestPose(t, v)
		}},
		{"mixed_occupancy", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[1].Pod.Occupied = true }},
		{"moving", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[0].Pod.Speed = 1 }},
		{"wrong_class", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[0].Pod.Class = GroupClass }},
		{"legacy_front", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[0].Pod.Class = LegacyClass }},
		{"legacy_rear", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[1].Pod.Class = LegacyClass }},
		{"express_front", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[0].Pod.Class = ExpressClass }},
		{"express_rear", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[1].Pod.Class = ExpressClass }},
		{"third_partner", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[1].follower = 1 }},
		{"negative_partner", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[0].follower = -1 }},
		{"negative_grant", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[0].reservedThrough = -1 }},
		{"future_rear_grant", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[1].reservedThrough++ }},
		{"larger_tail", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Prepared.laneCells["ab"].tail = 20 }},
		{"foreign_owner", func(_ *testing.T, i *couplingApproachPrepareInput) {
			for r := range i.Simulation.vehicles[1].routeReleases {
				i.Simulation.owners[r] = podResourceOwner("foreign")
			}
		}},
		{"short_retention", func(_ *testing.T, i *couplingApproachPrepareInput) {
			for r := range i.Simulation.vehicles[1].routeReleases {
				i.Simulation.vehicles[1].routeReleases[r]--
			}
		}},
		{"nonfinite_retention", func(_ *testing.T, i *couplingApproachPrepareInput) {
			for r := range i.Simulation.vehicles[0].routeReleases {
				i.Simulation.vehicles[0].routeReleases[r] = math.NaN()
			}
		}},
		{"pending_pickup", func(_ *testing.T, i *couplingApproachPrepareInput) {
			i.Simulation.waiting = append(i.Simulation.waiting, waitingTrip{request: Request{PodID: "rear"}})
		}},
		{"wrong_position", func(_ *testing.T, i *couplingApproachPrepareInput) { i.Simulation.vehicles[0].Pod.Position.X++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, false)
			test.change(t, &input)
			owners := maps.Clone(input.Simulation.owners)
			if _, _, err := prepareCouplingApproach(input); err == nil {
				t.Fatal("invalid actual approach state accepted")
			}
			if !maps.Equal(owners, input.Simulation.owners) {
				t.Fatal("failed preparation changed owners")
			}
		})
	}
}

func TestCouplingApproachAbortBrakesAndDrains(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"disabled", "route_version", "pickup", "follower_bound"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, false)
			s := input.Simulation
			c, state, err := prepareCouplingApproach(input)
			if err != nil {
				t.Fatal(err)
			}
			s.grant(intent{index: 0, block: 2, through: 2})
			for range 30 {
				var step couplingApproachStep
				step, err = planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
				if err != nil {
					t.Fatal(err)
				}
				applyCouplingApproachTestStep(t, s, step)
				state = step.State
			}
			before := s.vehicles[0].Pod.Speed
			enabled := true
			switch change {
			case "disabled":
				enabled = false
			case "route_version":
				s.vehicles[0].routeVersion++
			case "pickup":
				s.waiting = append(s.waiting, waitingTrip{request: Request{PodID: "rear"}})
			case "follower_bound":
				s.vehicles[0].follower = len(s.vehicles) + 1
			}
			step, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: enabled})
			if err != nil {
				t.Fatal(err)
			}
			if step.State.Phase != couplingApproachAborting || step.Front == nil || step.Front.Speed <= 0 || before-step.Front.Speed > acceleration/TicksPerSecond+conflictSlack {
				t.Fatal("moving invalidation reset or clamped the front")
			}
			if change == "follower_bound" {
				return // Corrupt input is refused without indexing the invalid follower.
			}
			applyCouplingApproachTestStep(t, s, step)
			state = step.State
			for range 1200 {
				step, err = planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: enabled})
				if err != nil {
					t.Fatal(err)
				}
				if step.State.BrakeComplete && state.BrakeComplete && step.Front != nil {
					t.Fatal("finished braking still overrides ordinary progress")
				}
				applyCouplingApproachTestStep(t, s, step)
				state = step.State
				if state.Phase == couplingApproachFinished {
					if s.platoonLinks != 0 || s.holdsPending(&s.vehicles[1]) || state.WaitTick != -1 {
						t.Fatal("abort lost pending owners or invented a wait")
					}
					return
				}
			}
			t.Fatal("abort did not drain its real pending owners")
		})
	}
}

func TestCouplingApproachClockAndClone(t *testing.T) {
	t.Parallel()
	input := couplingApproachFixture(t, false)
	c, state, err := prepareCouplingApproach(input)
	if err != nil {
		t.Fatal(err)
	}
	owners := maps.Clone(input.Simulation.owners)
	step, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: input.Simulation, Enabled: true})
	if err != nil || step.Front != nil || step.State.WaitTick != -1 || !maps.Equal(owners, input.Simulation.owners) {
		t.Fatalf("ordinary obstruction became an intentional hold: %+v %v", step, err)
	}
	applyCouplingApproachTestStep(t, input.Simulation, step)
	state = step.State
	input.Simulation.grant(intent{index: 0, block: 2, through: 2})
	clone := input.Simulation.Clone()
	for range 20 {
		a, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: input.Simulation, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		b, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: clone, Enabled: true})
		if err != nil || a.State != b.State || a.Front == nil || b.Front == nil || *a.Front != *b.Front {
			t.Fatalf("clone changed immutable plan or clock: %v", err)
		}
		applyCouplingApproachTestStep(t, input.Simulation, a)
		applyCouplingApproachTestStep(t, clone, b)
		state = a.State
	}
	for _, mutate := range []func(*couplingApproachState){
		func(s *couplingApproachState) { s.Tick++ },
		func(s *couplingApproachState) { s.Cursor++ },
		func(s *couplingApproachState) { s.WaitTick, s.DeadlineTick = s.Tick, s.Tick+301 },
		func(s *couplingApproachState) { s.BrakeComplete = true },
	} {
		bad := state
		mutate(&bad)
		probe := clone.Clone()
		if _, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: bad, Simulation: probe, Enabled: true}); !errors.Is(err, errCouplingMotionInvariant) {
			t.Fatal("invalid approach clock accepted", err)
		}
	}
}

func TestCouplingApproachDeadlinePublication(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		late bool
	}{{"exact_deadline", false}, {"one_tick_late", true}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, false)
			c, done := runCouplingApproachTest(t, input)
			s := input.Simulation
			state := done
			state.Phase = couplingApproachWaiting
			for s.tick < state.DeadlineTick-1 {
				s.tick++
			}
			if test.late {
				s.tick++
			}
			state.Tick = s.tick
			step, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
			if err != nil || step.Ready == test.late || step.State.DeadlineTick != done.DeadlineTick || step.State.WaitTick != done.WaitTick {
				t.Fatalf("wrong publication deadline decision: %+v %v", step, err)
			}
			if test.late && (step.Front != nil || step.HoldFront || !step.Aborted) {
				t.Fatal("deadline expiry added an extra stopped publication")
			}
		})
	}
}

func TestCouplingApproachNativeRiddenClock(t *testing.T) {
	t.Parallel()
	input := couplingApproachFixture(t, true)
	v := &input.Simulation.vehicles[0]
	v.Riders[0].SharingConsent = SharedConsent
	v.Boardings = []RiderBoarding{{BerthID: v.origin.ID, MetersAtBoarding: 25}}
	v.riddenBase = 50
	c, _, err := prepareCouplingApproach(input)
	if err != nil {
		t.Fatal(err)
	}
	actual := c.formationInput(input.Simulation, &input.Simulation.vehicles[0], &input.Simulation.vehicles[1]).Members[0].Vehicle
	if actual.RiddenMeters != v.riddenBase+v.distance || !slices.Equal(actual.Boardings, v.Boardings) {
		t.Fatal("formation input lost the native cumulative riding clock")
	}
}

func TestCouplingApproachMovingOwnerLoss(t *testing.T) {
	t.Parallel()
	input := couplingApproachFixture(t, false)
	s := input.Simulation
	c, state, err := prepareCouplingApproach(input)
	if err != nil {
		t.Fatal(err)
	}
	s.grant(intent{index: 0, block: 2, through: 2})
	for range 10 {
		var step couplingApproachStep
		step, err = planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		applyCouplingApproachTestStep(t, s, step)
		state = step.State
	}
	s.owners[resource{kind: trackResource, id: "ab", cell: 2}] = podResourceOwner("foreign")
	before := s.vehicles[0].Pod
	step, err := planCouplingApproachTest(couplingApproachInput{Context: c, Previous: state, Simulation: s, Enabled: true})
	if !errors.Is(err, errCouplingMotionInvariant) || step.Front != nil || s.vehicles[0].Pod != before {
		t.Fatal("moving owner loss published an unproved command", err)
	}
}

func TestCouplingApproachFrontCommandBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		speed, distance float64
	}{
		{"nonfinite_distance", 0, math.NaN()},
		{"nonfinite_speed", math.NaN(), 60},
		{"negative_speed", -1, 60},
		{"backward_distance", 0, 59},
		{"non_euler", .01, 61},
		{"excess_acceleration", 1, 60 + 1.0/60},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, false)
			if _, err := couplingApproachFrontCommand(input.Simulation, &input.Simulation.vehicles[0], test.distance, test.speed, input.Simulation.tick+1); !errors.Is(err, errCouplingMotionInvariant) {
				t.Fatal("invalid command accepted", err)
			}
		})
	}
	for _, name := range []string{"authored_speed", "stopping_envelope"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := couplingApproachFixture(t, false)
			s := input.Simulation
			s.grant(intent{index: 0, block: 2, through: 2})
			front := &s.vehicles[0]
			front.blockIndex = 2
			speed := 7.14
			front.Pod.Speed = 7.12
			if name == "stopping_envelope" {
				front.distance, front.Pod.Speed, speed = 89.93, .5, .53
			}
			couplingApproachTestPose(t, front)
			s.releaseCleared()
			if _, err := couplingApproachFrontCommand(s, front, front.distance+speed/TicksPerSecond, speed, s.tick+1); !errors.Is(err, errCouplingMotionInvariant) {
				t.Fatal("command exceeded its actual lane or stopping bound", err)
			}
		})
	}
}

// Component cases use the same real tick increment as native Step.
func planCouplingApproachTest(input couplingApproachInput) (couplingApproachStep, error) {
	input.Simulation.tick++
	return planCouplingApproach(input)
}
