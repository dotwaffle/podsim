package sim

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

// The input phases are strict cold-restored private certificates.
// Natural formation and journey completion have separate runtime tests.
func TestCouplingPresentationPhases(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		for _, phase := range []struct {
			phase couplingReservationPhase
			leg   int
		}{{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1}, {couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4}} {
			t.Run(fmt.Sprintf("occupied=%t/phase=%d/leg=%d", occupied, phase.phase, phase.leg), func(t *testing.T) {
				t.Parallel()
				s, _, err := RestoreState(nativeCouplingSavedFixture(t, occupied, phase.phase, phase.leg))
				if err != nil {
					t.Fatal(err)
				}
				before := s.ExportState()
				view, err := s.CouplingPresentation()
				if err != nil || view.Contract != CompactPairV1CouplingContract || len(view.Groups) != 1 {
					t.Fatal("checked view lost a valid group", err, view)
				}
				group := view.Groups[0]
				if group.SavedCouplingGroup != before.CouplingGroups[0] || group.OwnerID != group.ID || group.Profile != view.Contract {
					t.Fatal("view changed immutable membership or profile")
				}
				claims := 0
				for _, owner := range s.owners {
					if owner.kind == groupOwnerKind && owner.id == group.ID {
						claims++
					}
				}
				if group.ResourceClaims != claims {
					t.Fatal("view did not report actual typed ownership")
				}
				connected := phase.phase == couplingConnected || phase.phase == couplingLatching || phase.phase == couplingUnlatching
				maneuver := phase.phase == couplingClosing || phase.phase == couplingOpening
				if (group.CommonSpeed != nil) != connected || (group.Connector != nil) != connected || (group.ManeuverEnvelope != nil) != maneuver {
					t.Fatal("view confused latch attachment with protected maneuver space")
				}
				for i, id := range group.Members {
					pod := s.findVehicle(id).Pod
					corners := group.Bodies[i].Corners
					center := Point{}
					for _, corner := range corners {
						center.X += corner.X / 4
						center.Y += corner.Y / 4
					}
					if math.Hypot(center.X-pod.Position.X, center.Y-pod.Position.Y) > 1e-9 ||
						math.Abs(pointDistance(corners[0], corners[1])-4) > 1e-9 || math.Abs(pointDistance(corners[1], corners[2])-2) > 1e-9 {
						t.Fatal("body did not match the actual Compact cabin", id)
					}
					if connected && *group.CommonSpeed != pod.Speed {
						t.Fatal("view common speed did not match both cabins")
					}
				}
				state, err := s.CheckedSnapshot()
				if err != nil || !reflect.DeepEqual(state.CouplingGroups, view.Groups) {
					t.Fatal("checked snapshot lost coherent group data", err)
				}
				for _, cabin := range state.Vehicles {
					if cabin.CouplingID != s.findVehicle(cabin.Pod.ID).couplingID {
						t.Fatal("checked cabin lost its native mechanical membership")
					}
				}
				for _, cabin := range s.Snapshot().Vehicles {
					if cabin.CouplingID != "" {
						t.Fatal("ordinary snapshot gained mechanical presentation fields")
					}
				}
				view.Groups[0].Members[0] = "caller mutation"
				view.Groups[0].Bodies[0].Corners[0].X = 999
				if view.Groups[0].CommonSpeed != nil {
					*view.Groups[0].CommonSpeed = 999
					view.Groups[0].Connector.Corners[0].X = 999
				}
				if view.Groups[0].ManeuverEnvelope != nil {
					view.Groups[0].ManeuverEnvelope.Corners[0].X = 999
				}
				if !reflect.DeepEqual(before, s.ExportState()) {
					t.Fatal("view exposed mutable simulation storage")
				}
				again, err := s.CouplingPresentation()
				if err != nil || !reflect.DeepEqual(again.Groups, state.CouplingGroups) {
					t.Fatal("caller changed later group observations", err)
				}
			})
		}
	}
}

func TestCouplingPresentationRejectsPartialState(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*Simulation)
	}{
		{"clock", func(s *Simulation) { s.tick++ }},
		{"pose", func(s *Simulation) { s.vehicles[0].Pod.Position.X++ }},
		{"route", func(s *Simulation) {
			s.vehicles[0].Route = cloneLanes(s.vehicles[0].Route)
			s.vehicles[0].Route[0].SpeedLimit++
		}},
		{"membership", func(s *Simulation) { s.vehicles[0].couplingID = "other" }},
		{"phase", func(s *Simulation) { s.couplingGroups[0].state.Phase = 255 }},
		{"missing owner", func(s *Simulation) {
			for r, owner := range s.owners {
				if owner.kind == groupOwnerKind {
					delete(s.owners, r)
					return
				}
			}
		}},
		{"extra owner", func(s *Simulation) {
			s.owners[resource{kind: trackResource, id: "unknown"}] = s.couplingGroups[0].context.owner
		}},
		{"orphan owner", func(s *Simulation) {
			s.owners[resource{kind: trackResource, id: "unknown"}] = resourceOwner{kind: groupOwnerKind, id: "unknown"}
		}},
		{"duplicate group", func(s *Simulation) { s.couplingGroups = append(s.couplingGroups, s.couplingGroups[0]) }},
		{"orphan member", func(s *Simulation) { s.couplingGroups = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, _, err := RestoreState(nativeCouplingSavedFixture(t, true, couplingConnected, 1))
			if err != nil {
				t.Fatal(err)
			}
			test.change(s)
			if view, err := s.CouplingPresentation(); err == nil || !reflect.DeepEqual(view, CouplingPresentation{}) {
				t.Fatal("invalid group produced a partial view", err, view)
			}
			if view, err := s.CheckedSnapshot(); err == nil || !reflect.DeepEqual(view, Snapshot{}) {
				t.Fatal("invalid group produced a partial fleet", err)
			}
		})
	}
}

func TestCouplingPresentationOldAndEmpty(t *testing.T) {
	t.Parallel()
	for _, contract := range []CouplingContract{"", CompactPairV1CouplingContract} {
		for _, enabled := range []bool{false, true} {
			if contract == "" && enabled {
				continue
			}
			t.Run(fmt.Sprintf("contract=%s/enabled=%t", contract, enabled), func(t *testing.T) {
				t.Parallel()
				s, err := NewFleetWithContracts(Example(), []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}, FleetContracts{CouplingContract: contract, CouplingEnabled: enabled})
				if err != nil {
					t.Fatal(err)
				}
				state, err := s.CheckedSnapshot()
				if err != nil || state.CouplingContract != contract || state.CouplingEnabled != enabled || len(state.CouplingGroups) != 0 {
					t.Fatal("empty or disabled contract downgraded", err)
				}
				if contract == "" {
					old, _ := json.Marshal(s.Snapshot())
					current, _ := json.Marshal(state)
					if !bytes.Equal(old, current) || bytes.Contains(current, []byte("coupling")) {
						t.Fatal("checked observation changed old bytes")
					}
				}
				fault := errors.New("retained controller fault")
				s.couplingFault = fault
				if _, err := s.CheckedSnapshot(); !errors.Is(err, fault) {
					t.Fatal("checked view lost original fault")
				}
			})
		}
	}
}
