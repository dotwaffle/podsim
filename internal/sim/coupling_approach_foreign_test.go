package sim

import (
	"errors"
	"maps"
	"reflect"
	"testing"
)

// The added berth is canonical. The unsafe occupancy is an authored fault.
// This checks refusal at the native planner, not ordinary admission reachability.
func couplingApproachForeignBodyStage(t *testing.T, held bool, clearance float64) *Simulation {
	t.Helper()
	input := couplingApproachFixture(t, false)
	geometry := CouplingGeometryInput{Contract: input.Network.contract, Network: input.Prepared.Network(),
		Sites:     []CouplingSite{input.Network.sites["assembly"], input.Network.sites["split"]},
		Corridors: []CouplingCorridor{input.Network.corridors["corridor"]}}
	x := geometry.Sites[0].FrontStagingMeters
	geometry.Network.Nodes = append(geometry.Network.Nodes,
		Node{ID: "foreign-entry", Position: Point{X: x - 30, Y: clearance}},
		Node{ID: "foreign-berth", Position: Point{X: x, Y: clearance}},
		Node{ID: "foreign-exit", Position: Point{X: x + 30, Y: clearance}})
	compact := classBit(string(CompactClass))
	for _, lane := range []Lane{{ID: "foreign-in", From: "foreign-entry", To: "foreign-berth"},
		{ID: "foreign-out", From: "foreign-berth", To: "foreign-exit"},
		{ID: "foreign-through", From: "foreign-entry", To: "foreign-exit"}} {
		lane.SpeedLimit, lane.VehicleClasses = 7.123456789, compact
		geometry.Network.Lanes = append(geometry.Network.Lanes, lane)
	}
	geometry.Network.Stations = append(geometry.Network.Stations, Station{ID: "foreign", Name: "Foreign", Entry: "foreign-entry", Exit: "foreign-exit", VehicleClasses: compact,
		Berths: []Berth{{ID: "foreign-1", Node: "foreign-berth", VehicleClasses: compact}}})
	p, err := PrepareNetwork(geometry.Network)
	if err != nil {
		t.Fatal(err)
	}
	geometry.Network = p.Network()
	n, err := prepareCouplingReservations(p, geometry)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.NewFleet(input.Simulation.initial)
	if err != nil {
		t.Fatal(err)
	}
	s.tick, s.platooning, s.platoonLimit = input.Simulation.tick, PlatooningVirtual, 2
	s.platoonLinks = input.Simulation.platoonLinks
	s.couplingNetwork, s.couplingEnabled, s.owners = n, true, maps.Clone(input.Simulation.owners)
	for i := range s.vehicles {
		s.vehicles[i] = input.Simulation.vehicles[i]
		v := &s.vehicles[i]
		s.setVehicleRoute(v, v.Route)
		couplingApproachTestPose(t, v)
	}
	s.discoverCouplingApproaches()
	if len(s.couplingApproaches) != 1 {
		t.Fatal("authored native approach did not prepare")
	}
	if held {
		for range 1200 {
			s.Step()
			if s.CouplingError() != nil {
				t.Fatal("unoccupied foreign berth prevented the safe hold", s.CouplingError())
			}
			if len(s.couplingApproaches) == 1 && s.couplingApproaches[0].state.WaitTick >= 0 {
				break
			}
		}
		if len(s.couplingApproaches) != 1 || s.couplingApproaches[0].state.WaitTick < 0 {
			t.Fatal("authored native stage never reached its hold")
		}
	}
	foreignPlacement := Placement{ID: "foreign", Class: CompactClass, StationID: "foreign"}
	foreign, err := p.NewFleet([]Placement{foreignPlacement})
	if err != nil {
		t.Fatal(err)
	}
	s.vehicles = append(s.vehicles, foreign.vehicles[0])
	s.initial = append(s.initial, foreignPlacement)
	s.vehicleIndexes = indexVehicles(s.vehicles)
	for r, owner := range foreign.owners {
		if !s.owners[r].isZero() {
			t.Fatal("foreign berth aliases an existing owner")
		}
		s.owners[r] = owner
	}
	s.SetMotionRecording(true)
	return s
}

func TestCouplingApproachRuntimeForeignBodyRefusal(t *testing.T) {
	t.Parallel()
	for _, held := range []bool{false, true} {
		t.Run(map[bool]string{false: "powered", true: "held"}[held], func(t *testing.T) {
			t.Parallel()
			for _, unsafe := range []bool{false, true} {
				clearance := Clearance + 1
				if unsafe {
					clearance = Clearance - .1
				}
				s := couplingApproachForeignBodyStage(t, held, clearance)
				for range 1200 {
					frame, _ := s.MotionFrame()
					front, rear := s.findVehicle("front").Pod, s.findVehicle("rear").Pod
					s.Step()
					if err := s.CouplingError(); err != nil {
						if !unsafe || !errors.Is(err, errCouplingMotionInvariant) {
							t.Fatal("safe body control failed", err)
						}
						t.Log("native refusal:", err)
						after, _ := s.MotionFrame()
						if !s.paused || !reflect.DeepEqual(frame, after) || front.Position != s.findVehicle("front").Pod.Position || rear.Position != s.findVehicle("rear").Pod.Position || front.Speed != s.findVehicle("front").Pod.Speed || rear.Speed != s.findVehicle("rear").Pod.Speed {
							t.Fatal("unsafe approach published motion or moved a cabin")
						}
						return
					}
					if unsafe {
						foreign := s.findVehicle("foreign").Pod.Position
						for _, id := range []string{"front", "rear"} {
							if pointDistance(s.findVehicle(id).Pod.Position, foreign) < Clearance-conflictSlack {
								t.Fatal("actual native planner published an unsafe foreign body approach")
							}
						}
					}
					if !unsafe && (held || len(s.couplingGroups) != 0) {
						break
					}
				}
				if unsafe {
					t.Fatal("actual native planner accepted an unsafe foreign body")
				}
			}
		})
	}
}
