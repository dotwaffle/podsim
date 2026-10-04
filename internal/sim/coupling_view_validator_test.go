package sim

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func couplingValidatorFixture(t *testing.T, occupied bool, phase couplingReservationPhase, leg int) (*Simulation, *CouplingViewValidator) {
	t.Helper()
	input := nativeCouplingSavedFixture(t, occupied, phase, leg)
	s, _, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewCouplingViewValidator(CouplingGeometryInput{Contract: input.CouplingContract,
		Network: input.Network, Sites: input.CouplingSites, Corridors: input.CouplingCorridors})
	if err != nil {
		t.Fatal(err)
	}
	return s, v
}

func TestCouplingViewValidatorPhases(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		for _, phase := range []struct {
			phase couplingReservationPhase
			leg   int
		}{{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1}, {couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4}} {
			t.Run(fmt.Sprintf("occupied=%t/phase=%d/leg=%d", occupied, phase.phase, phase.leg), func(t *testing.T) {
				t.Parallel()
				s, v := couplingValidatorFixture(t, occupied, phase.phase, phase.leg)
				for range 25 {
					state, err := s.CheckedSnapshot()
					if err != nil {
						t.Fatal(err)
					}
					before := s.ExportState()
					if err := v.Validate(state); err != nil {
						t.Fatalf("valid native phase rejected at tick %d: %v", state.Tick, err)
					}
					if !reflect.DeepEqual(before, s.ExportState()) {
						t.Fatal("consumer validation changed native state")
					}
					s.Step()
					if err := s.CouplingError(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestCouplingViewValidatorRejectsInconsistentViews(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		edit func(*Snapshot)
	}{
		{"missing marker", func(s *Snapshot) { s.CouplingContract = "" }},
		{"unknown marker", func(s *Snapshot) { s.CouplingContract = "unknown" }},
		{"missing registry", func(s *Snapshot) { s.CouplingGroups = nil }},
		{"missing cabin binding", func(s *Snapshot) { s.Vehicles[0].CouplingID = "" }},
		{"wrong cabin binding", func(s *Snapshot) { s.Vehicles[0].CouplingID = "other" }},
		{"future formation", func(s *Snapshot) { s.CouplingGroups[0].FormationTick = s.Tick + 1 }},
		{"duplicate group", func(s *Snapshot) { s.CouplingGroups = append(s.CouplingGroups, s.CouplingGroups[0]) }},
		{"duplicate member", func(s *Snapshot) { s.CouplingGroups[0].Members[1] = s.CouplingGroups[0].Members[0] }},
		{"unknown member", func(s *Snapshot) { s.CouplingGroups[0].Members[0] = "absent" }},
		{"reversed members", func(s *Snapshot) {
			s.CouplingGroups[0].Members[0], s.CouplingGroups[0].Members[1] = s.CouplingGroups[0].Members[1], s.CouplingGroups[0].Members[0]
		}},
		{"owner identity", func(s *Snapshot) { s.CouplingGroups[0].OwnerID = "other" }},
		{"owner count", func(s *Snapshot) { s.CouplingGroups[0].ResourceClaims = -1 }},
		{"profile", func(s *Snapshot) { s.CouplingGroups[0].Profile = "unknown" }},
		{"site", func(s *Snapshot) { s.CouplingGroups[0].AssemblySiteID = "other" }},
		{"phase", func(s *Snapshot) { s.CouplingGroups[0].Phase = "other" }},
		{"dwell", func(s *Snapshot) { s.CouplingGroups[0].DwellTicks++ }},
		{"leg", func(s *Snapshot) { s.CouplingGroups[0].Progress.Leg++ }},
		{"drain order", func(s *Snapshot) { s.CouplingGroups[0].Progress.DrainFirstMember = 2 }},
		{"missing connector", func(s *Snapshot) { s.CouplingGroups[0].Connector = nil }},
		{"extra envelope", func(s *Snapshot) { s.CouplingGroups[0].ManeuverEnvelope = new(CouplingRectangle{}) }},
		{"wrong connector", func(s *Snapshot) { s.CouplingGroups[0].Connector.Corners[0].X++ }},
		{"wrong body", func(s *Snapshot) { s.CouplingGroups[0].Bodies[0].Corners[0].X++ }},
		{"nonfinite body", func(s *Snapshot) { s.CouplingGroups[0].Bodies[0].Corners[0].X = math.NaN() }},
		{"nonfinite speed", func(s *Snapshot) { *s.CouplingGroups[0].CommonSpeed = math.NaN() }},
		{"different speed", func(s *Snapshot) { s.Vehicles[0].Pod.Speed++ }},
		{"different pose", func(s *Snapshot) { s.Vehicles[0].Pod.Position.X++ }},
		{"different distance", func(s *Snapshot) { s.Vehicles[0].Pod.LaneDistance++ }},
		{"different class", func(s *Snapshot) { s.Vehicles[0].Pod.Class = GroupClass }},
		{"virtual membership", func(s *Snapshot) { s.Vehicles[0].PlatoonID = "virtual" }},
		{"mixed occupancy", func(s *Snapshot) { s.Vehicles[0].Pod.Occupied = false }},
		{"pending member", func(s *Snapshot) { s.Pending = append(s.Pending, Request{PodID: s.CouplingGroups[0].Members[0]}) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, v := couplingValidatorFixture(t, true, couplingConnected, 1)
			state, err := s.CheckedSnapshot()
			if err != nil || v.Validate(state) != nil {
				t.Fatal("invalid control", err)
			}
			before := s.ExportState()
			test.edit(&state)
			if err := v.Validate(state); err == nil {
				t.Fatal("accepted an inconsistent train view")
			}
			if !reflect.DeepEqual(before, s.ExportState()) {
				t.Fatal("failed validation changed native state")
			}
		})
	}
}

func TestCouplingViewValidatorOwnsGeometry(t *testing.T) {
	t.Parallel()
	input := nativeCouplingSavedFixture(t, false, couplingConnected, 1)
	s, _, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewCouplingViewValidator(CouplingGeometryInput{Contract: input.CouplingContract,
		Network: input.Network, Sites: input.CouplingSites, Corridors: input.CouplingCorridors})
	if err != nil {
		t.Fatal(err)
	}
	input.Network.Nodes[0].Position.X++
	input.Network.Lanes[0].SpeedLimit = 0
	input.CouplingSites[0].FrontStagingMeters++
	input.CouplingCorridors[0].LaneIDs[0] = "mutated"
	state, err := s.CheckedSnapshot()
	if err != nil || v.Validate(state) != nil {
		t.Fatal("caller changed retained geometry", err)
	}
}

func TestCouplingViewValidatorRotatedGeometry(t *testing.T) {
	t.Parallel()
	for _, angle := range []float64{math.Pi / 7, math.Pi / 2, math.Pi} {
		t.Run(fmt.Sprint(angle), func(t *testing.T) {
			t.Parallel()
			input := nativeCouplingSavedFixture(t, true, couplingConnected, 1)
			s, _, err := RestoreState(input)
			if err != nil {
				t.Fatal(err)
			}
			state, err := s.CheckedSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			rotate := func(p Point) Point {
				return Point{X: 13 + p.X*math.Cos(angle) - p.Y*math.Sin(angle), Y: -37 + p.X*math.Sin(angle) + p.Y*math.Cos(angle)}
			}
			for i := range input.Network.Nodes {
				input.Network.Nodes[i].Position = rotate(input.Network.Nodes[i].Position)
			}
			for i := range input.Network.Lanes {
				if input.Network.Lanes[i].Control != nil {
					input.Network.Lanes[i].Control = new(rotate(*input.Network.Lanes[i].Control))
				}
			}
			for i := range state.Vehicles {
				state.Vehicles[i].Pod.Position = rotate(state.Vehicles[i].Pod.Position)
			}
			for i := range state.CouplingGroups[0].Bodies {
				for j := range state.CouplingGroups[0].Bodies[i].Corners {
					state.CouplingGroups[0].Bodies[i].Corners[j] = rotate(state.CouplingGroups[0].Bodies[i].Corners[j])
				}
			}
			for i := range state.CouplingGroups[0].Connector.Corners {
				state.CouplingGroups[0].Connector.Corners[i] = rotate(state.CouplingGroups[0].Connector.Corners[i])
			}
			v, err := NewCouplingViewValidator(CouplingGeometryInput{Contract: input.CouplingContract,
				Network: input.Network, Sites: input.CouplingSites, Corridors: input.CouplingCorridors})
			if err != nil {
				t.Fatal(err)
			}
			if err := v.Validate(state); err != nil {
				t.Fatal("valid rigid transform rejected", err)
			}
		})
	}
}
