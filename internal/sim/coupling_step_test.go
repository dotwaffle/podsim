package sim

import (
	"fmt"
	"maps"
	"reflect"
	"testing"
)

// These tests continue strict cold-restored phases through the actual Step caller.
// The starting states come from the private certificate, not live recruitment.
func TestCouplingNativeStepRestoredPhases(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, occupied := range []bool{false, true} {
		for _, phase := range []struct {
			phase couplingReservationPhase
			leg   int
		}{
			{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1}, {couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4},
		} {
			t.Run(fmt.Sprintf("occupied=%t/phase=%d/leg=%d", occupied, phase.phase, phase.leg), func(t *testing.T) {
				t.Parallel()
				input := nativeCouplingSavedFixture(t, occupied, phase.phase, phase.leg)
				s, _, err := RestoreState(input)
				if err != nil {
					t.Fatal(err)
				}
				s.SetPaused(false)
				s.SetMotionRecording(true)
				initial := s.ExportState()
				total := [2]float64{}
				retired := false
				for range 30000 {
					previous := s.Clone()
					s.Step()
					if err := s.CouplingError(); err != nil {
						t.Fatal("native Step failed", s.tick, err)
					}
					if s.tick != previous.tick+1 {
						t.Fatal("native Step did not advance its clock")
					}
					if err := s.CheckContract(); err != nil {
						t.Fatal("native cabin contract failed", s.tick, err)
					}
					if _, err := s.SafetyObservation().Check(); err != nil {
						t.Fatal("native safety failed", s.tick, err)
					}
					if !maps.Equal(s.owners, s.retainedOwners()) {
						t.Fatalf("native owner retention differs at tick %d", s.tick)
					}
					frame, ok := s.MotionFrame()
					if !ok || frame.Tick != s.tick {
						t.Fatal("native motion frame not published")
					}
					seen := map[string]bool{}
					for _, sample := range frame.Samples {
						if seen[sample.ID] {
							t.Fatal("native member measured more than once", sample.ID)
						}
						seen[sample.ID] = true
						before, after := previous.findVehicle(sample.ID), s.findVehicle(sample.ID)
						if sample.DistanceMeters != after.distance-before.distance || sample.StartSpeed != before.Pod.Speed || sample.EndSpeed != after.Pod.Speed {
							t.Fatal("native sample differs from actual member motion", sample)
						}
					}
					for i := range s.vehicles {
						v, before := &s.vehicles[i], &previous.vehicles[i]
						distance := v.distance - before.distance
						if distance < 0 {
							t.Fatal("native route progress decreased")
						}
						total[i] += distance
						if !reflect.DeepEqual(v.Riders, before.Riders) || !reflect.DeepEqual(v.Boardings, before.Boardings) || !reflect.DeepEqual(v.Route, before.Route) || v.routeVersion != before.routeVersion {
							t.Fatal("native movement changed cabin facts or the original route")
						}
					}
					if len(s.couplingGroups) == 0 {
						retired = true
						break
					}
				}
				if !retired || s.couplingFleet != nil {
					t.Fatal("native group did not retire its registry and cache")
				}
				for i := range s.vehicles {
					if s.vehicles[i].couplingID != "" {
						t.Fatal("retired member still excluded from ordinary movement")
					}
				}
				measured := s.emptyDistanceMeters - initial.EmptyDistanceMeters
				if occupied {
					measured = s.passengerDistanceMeters - initial.PassengerDistanceMeters
				}
				if difference := measured - (total[0] + total[1]); difference < -1e-8 || difference > 1e-8 {
					t.Fatal("native distance accounting differs", measured, total)
				}
				for range 30000 {
					s.Step()
					if err := s.CouplingError(); err != nil {
						t.Fatal(err)
					}
					if err := s.CheckContract(); err != nil {
						t.Fatal("ordinary continuation failed", err)
					}
					if _, err := s.SafetyObservation().Check(); err != nil {
						t.Fatal("ordinary continuation unsafe", err)
					}
					if !maps.Equal(s.owners, s.retainedOwners()) {
						t.Fatal("ordinary owners differ after retirement")
					}
					idle := true
					for i := range s.vehicles {
						idle = idle && s.vehicles[i].Pod.Activity == Idle
					}
					if idle {
						break
					}
				}
				for i := range s.vehicles {
					if s.vehicles[i].Pod.Activity != Idle {
						t.Fatal("retired member did not finish its journey")
					}
				}
				if occupied && s.completed != 2 {
					t.Fatal("occupied cabins did not complete both original orders", s.completed)
				}
			})
		}
	}
}

func TestCouplingNativeStepClone(t *testing.T) {
	t.Parallel()
	input := nativeCouplingSavedFixture(t, true, couplingConnected, 1)
	s, _, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	s.SetPaused(false)
	s.SetMotionRecording(true)
	s.Step()
	if err := s.CouplingError(); err != nil {
		t.Fatal(err)
	}
	if s.couplingFleet == nil {
		t.Fatal("native fleet cache did not fill")
	}
	clone := s.Clone()
	if clone.couplingFleet != nil {
		t.Fatal("clone retained the source fleet cache")
	}
	for range 30000 {
		s.Step()
		clone.Step()
		if s.CouplingError() != nil || clone.CouplingError() != nil {
			t.Fatal("native clone evolution failed", s.CouplingError(), clone.CouplingError())
		}
		if !reflect.DeepEqual(s.ExportState(), clone.ExportState()) || !maps.Equal(s.owners, clone.owners) {
			t.Fatal("clone changed future native evolution", s.tick)
		}
		a, _ := s.MotionFrame()
		b, _ := clone.MotionFrame()
		if !reflect.DeepEqual(a, b) {
			t.Fatal("clone changed native motion measurements")
		}
		if len(s.couplingGroups) == 0 {
			break
		}
	}
	if len(s.couplingGroups) != 0 {
		t.Fatal("clone did not drain")
	}
	clone.Reset()
	if clone.couplingFleet != nil || len(clone.couplingGroups) != 0 {
		t.Fatal("reset retained native state")
	}
}

func TestCouplingNativeStepRefusesBeforeMovement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Simulation)
	}{
		{"owner", func(s *Simulation) {
			for r, owner := range s.owners {
				if owner.kind == groupOwnerKind {
					delete(s.owners, r)
					return
				}
			}
		}},
		{"pose", func(s *Simulation) { s.vehicles[0].Pod.Position.X++ }},
		{"route version", func(s *Simulation) { s.vehicles[0].routeVersion++ }},
		{"membership", func(s *Simulation) { s.vehicles[0].couplingID = "wrong" }},
		{"clock", func(s *Simulation) { s.couplingGroups[0].formationTick++ }},
		{"cabins", func(s *Simulation) { s.vehicles[0].Riders[0].PartySize++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := nativeCouplingSavedFixture(t, true, couplingConnected, 1)
			s, _, err := RestoreState(input)
			if err != nil {
				t.Fatal(err)
			}
			s.SetPaused(false)
			s.SetMotionRecording(true)
			tc.change(s)
			poses := []Pod{s.vehicles[0].Pod, s.vehicles[1].Pod}
			distances := []float64{s.vehicles[0].distance, s.vehicles[1].distance}
			owners := maps.Clone(s.owners)
			before, _ := s.MotionFrame()
			s.Step()
			if s.CouplingError() == nil || !s.paused {
				t.Fatal("invalid committed state moved without an explicit fault")
			}
			if !maps.Equal(owners, s.owners) {
				t.Fatal("failed committed proof changed owners")
			}
			for i := range s.vehicles {
				if s.vehicles[i].Pod != poses[i] || s.vehicles[i].distance != distances[i] {
					t.Fatal("failed committed proof moved a member")
				}
			}
			after, _ := s.MotionFrame()
			if !reflect.DeepEqual(before, after) {
				t.Fatal("failed tick published an unproved motion frame")
			}
			tick := s.tick
			s.SetPaused(false)
			s.Step()
			if s.tick != tick || !s.paused {
				t.Fatal("a coupling fault resumed without reset")
			}
		})
	}
}

// The disjoint private pair fixture exercises a shared frame and independent retirement.
func TestCouplingNativeStepMultiplePairs(t *testing.T) {
	t.Parallel()
	source, contexts, pairs := nativeForeignPairFixture(t)
	source.couplingNetwork = contexts[0].reservation.network
	source.tick = pairs[0].previous.Tick
	for i, c := range contexts {
		source.couplingGroups = append(source.couplingGroups, couplingNativeGroup{context: c, state: pairs[i].previous, formationTick: c.formationTick})
		for _, member := range c.reservation.members {
			v := source.findVehicle(member.Vehicle.Pod.ID)
			v.couplingID = c.owner.id
			v.journeyOrigin = v.origin
		}
	}
	// Combine distinct private saved phases so one pair retires first.
	c := contexts[1]
	later := remainingTestFind(t, c, couplingConnected, 1)
	source.tick = later.Tick
	source.couplingGroups[1].state = later
	members := c.membersAt(later)
	for i, member := range c.reservation.members {
		v := source.findVehicle(member.Vehicle.Pod.ID)
		v.Pod = members[i].Pod
		v.distance, v.blockIndex = later.Distances[i], later.Cells[i]
	}
	for _, dependency := range c.dependencies {
		putCouplingOwner(source.owners, dependency.Resource, c.dependencyOwner(dependency, later))
	}
	state := source.ExportState()
	// Each private corridor uses local request IDs before the combined fixture.
	state.RequestID, state.Boarded = 2, 2
	id := 0
	for i := range state.Pods {
		for j := range state.Pods[i].Riders {
			id++
			state.Pods[i].Riders[j].ID = id
		}
	}
	n := source.couplingNetwork
	var sites []CouplingSite
	var corridors []CouplingCorridor
	for _, site := range n.sites {
		sites = append(sites, site)
	}
	for _, corridor := range n.corridors {
		corridors = append(corridors, corridor)
	}
	s, _, err := RestoreState(RestoreStateInput{Network: source.network, Fleet: source.initial, State: state,
		CouplingContract: n.contract, CouplingSites: sites, CouplingCorridors: corridors})
	if err != nil {
		t.Fatal(err)
	}
	s.SetPaused(false)
	s.SetMotionRecording(true)
	sawBoth, sawMixed := false, false
	for range 30000 {
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatal("shared native tick failed", s.tick, err)
		}
		if err := s.CheckContract(); err != nil {
			t.Fatal("shared native cabin contract failed", err)
		}
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal("shared native geometry unsafe", err)
		}
		if !maps.Equal(s.owners, s.retainedOwners()) {
			t.Fatal("shared native ownership differs", s.tick)
		}
		sawBoth = sawBoth || len(s.couplingGroups) == 2
		sawMixed = sawMixed || len(s.couplingGroups) == 1
		if s.couplingFleet != nil {
			if len(s.couplingFleet.pairs) != len(s.couplingGroups) {
				t.Fatal("cache retains retired context")
			}
		}
		if len(s.couplingGroups) == 0 {
			break
		}
	}
	if len(s.couplingGroups) != 0 || !sawBoth || !sawMixed {
		t.Fatal("independent pair retirements not exercised", sawBoth, sawMixed)
	}
}

func TestCouplingNativeStepAppliedGuard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Simulation, *couplingNativeTick)
	}{
		{"position", func(s *Simulation, _ *couplingNativeTick) { s.vehicles[0].Pod.Position.X++ }},
		{"distance", func(s *Simulation, _ *couplingNativeTick) { s.vehicles[0].distance++ }},
		{"speed", func(s *Simulation, _ *couplingNativeTick) { s.vehicles[0].Pod.Speed++ }},
		{"ledger", func(s *Simulation, _ *couplingNativeTick) {
			for r := range s.owners {
				delete(s.owners, r)
				return
			}
		}},
		{"owner writes", func(_ *Simulation, tick *couplingNativeTick) {
			r := resource{kind: couplingSiteResourceKind(), id: "unproved"}
			tick.writes = append(tick.writes, couplingOwnerWrite{Resource: r, Next: podResourceOwner("front")}, couplingOwnerWrite{Resource: r, Expected: podResourceOwner("rear")})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := nativeCouplingSavedFixture(t, false, couplingConnected, 1)
			s, _, err := RestoreState(input)
			if err != nil {
				t.Fatal(err)
			}
			s.tick++
			tick, err := s.planNativeCouplingTick()
			if err != nil {
				t.Fatal(err)
			}
			s.moveNativeCoupling(tick, 0)
			tc.change(s, tick)
			owners := maps.Clone(s.owners)
			registry := s.savedCouplingGroups()
			if err := s.finishNativeCoupling(tick); err == nil {
				t.Fatal("unproved applied tick accepted")
			}
			if !maps.Equal(owners, s.owners) || !reflect.DeepEqual(registry, s.savedCouplingGroups()) {
				t.Fatal("failed applied proof transferred ownership or group state")
			}
		})
	}
}
