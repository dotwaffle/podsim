package sim

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"maps"
	"math"
	"slices"
	"testing"
)

// The native fleet is real. This is a controller certificate fixture, not admission.
// The pair identities are present at their berths, outside the inactive train engine.
func nativeForeignFixture(t *testing.T) (*Simulation, *couplingMotionContext, *vehicle) {
	t.Helper()
	input := couplingMotionWithForeignStation(t, couplingMotionFixture(t, false, false))
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair", ForeignIDs: []string{"moving"}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := input.Prepared.NewFleetWithOrderContract([]Placement{
		{ID: "front", Class: CompactClass, StationID: "front-goal", BerthID: "front-goal-1"},
		{ID: "rear", Class: CompactClass, StationID: "rear-goal", BerthID: "rear-goal-1"},
		{ID: "moving", Class: CompactClass, StationID: "foreign-station", BerthID: "foreign-1"},
	}, input.OrderContract)
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("moving")
	var route []Lane
	for _, lane := range s.network.Lanes {
		if lane.ID == "foreign-out" {
			route = append(route, lane)
		}
	}
	s.setVehicleRoute(v, route)
	station, _ := s.network.Station(v.Pod.StationID)
	v.origin = station.Berths[0]
	v.Pod.Activity = DepartingEmpty
	index := s.vehicleIndexes[v.Pod.ID]
	s.grant(intent{index: index, block: 0, through: 0})
	if v.reservedThrough != 0 {
		t.Fatal("native first-cell admission did not match fixture")
	}
	return s, c, v
}

func TestNativeForeignNaturalSnap(t *testing.T) {
	t.Parallel()
	s, c, v := nativeForeignFixture(t)
	fleet, err := prepareNativeForeignFleet(s, c)
	if err != nil {
		t.Fatal(err)
	}
	// The real Step departure branch changes presentation without moving this tick.
	s.tick++
	departure, err := buildNativeForeignTick(s, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err = couplingCheckForeignMotion(departure.sweeps()[0]); err != nil {
		t.Fatal(err)
	}
	if departs(v.Pod.Activity) && v.phaseTicks == 0 && v.reservedThrough >= 0 {
		v.Pod.Activity = Traveling
		v.Pod.StationID, v.Pod.BerthID = "", ""
	}
	if err = departure.checkApplied(s); err != nil {
		t.Fatal(err)
	}
	frontier := v.blocks.end(v.reservedThrough)
	rounded, snapped := 0, false
	for tick := range 1000 {
		s.tick++
		previous, speed := v.distance, v.Pod.Speed
		if previous+couplingOrdinaryDiscreteStop(speed) > frontier {
			t.Fatal("actual ordinary sample lacks native discrete stopping room")
		}
		if previous+speed*speed/4 > frontier {
			rounded++
		}
		frame, frameErr := buildNativeForeignTick(s, fleet)
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		sweep := frame.sweeps()[0]
		if err = couplingCheckForeignMotion(sweep); err != nil {
			t.Fatalf("actual native certificate refused tick %d: %v", tick, err)
		}
		s.move(v)
		if err = frame.checkApplied(s); err != nil {
			t.Fatal(err)
		}
		if v.distance == frontier {
			if v.Pod.Speed != 0 || v.distance == previous+v.Pod.Speed/60 {
				t.Fatal("natural native snap was not observed")
			}
			sweep.native = nil
			if !errors.Is(couplingCheckForeignMotion(sweep), errCouplingMotionInvariant) {
				t.Fatal("synthetic certificate accepted the published native snap")
			}
			t.Logf("native_snap tick=%d previous=%.17g speed=%.17g commanded=%.17g next=%.17g rounded_samples=%d", s.tick, previous, speed, frame.proofs[v.Pod.ID].ordinary.commandedSpeed, v.distance, rounded)
			snapped = true
			break
		}
	}
	if !snapped || rounded == 0 {
		t.Fatal("native snap and rounded stopping boundary controls were incomplete")
	}
}

func TestNativeForeignFrozenFacts(t *testing.T) {
	t.Parallel()
	s, c, v := nativeForeignFixture(t)
	v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
	s.tick++
	fleet, err := prepareNativeForeignFleet(s, c)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := buildNativeForeignTick(s, fleet)
	if err != nil {
		t.Fatal(err)
	}
	valid := frame.sweeps()[0]
	for _, test := range []struct {
		name   string
		change func(*couplingForeignSweep)
	}{
		{"tick", func(x *couplingForeignSweep) { x.Tick++ }},
		{"distance", func(x *couplingForeignSweep) { x.NextDistance = math.Nextafter(x.NextDistance, math.Inf(1)) }},
		{"speed", func(x *couplingForeignSweep) { x.NextSpeed = 0 }},
		{"frontier", func(x *couplingForeignSweep) { x.ReservedThrough++ }},
		{"no_producer", func(x *couplingForeignSweep) { x.native = &nativeForeignProof{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			x := valid
			test.change(&x)
			if !errors.Is(couplingCheckForeignMotion(x), errCouplingMotionInvariant) {
				t.Fatal("changed native fact was accepted")
			}
		})
	}
	before := frame.sweeps()
	clear(s.owners)
	v.Pod.Speed = 1
	v.routeReleases = maps.Clone(v.routeReleases)
	for r := range v.routeReleases {
		v.routeReleases[r] = 0
	}
	if !slices.Equal(before, frame.sweeps()) {
		t.Fatal("caller mutation changed the immutable complete frame")
	}
	if err = frame.checkApplied(s); !errors.Is(err, errCouplingMotionInvariant) {
		t.Fatal("unapplied native command was accepted")
	}
}

func TestNativeForeignOrdinaryInverseControl(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name            string
		lengths, limits []float64
	}{
		{"one_lane", []float64{100}, []float64{7.123456789}},
		{"lower_future", []float64{100, 10, 100}, []float64{14, 8, 2.5}},
		{"crossed_short_lane", []float64{100, .001, 100}, []float64{14, 2.5, 14}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, v := laneSpeedFixture(test.lengths, test.limits)
			actual.vehicles = []vehicle{*v}
			v = &actual.vehicles[0]
			actual.SetMotionRecording(true)
			control := actual.Clone()
			old := &control.vehicles[0]
			for tick := range 20000 {
				actual.tick++
				control.tick++
				actual.beginMotionFrame()
				control.beginMotionFrame()
				actual.moveAndMeasure(v)
				control.oldMoveAndMeasureNativeForeign(old)
				actual.publishMotionFrame()
				control.publishMotionFrame()
				af, _ := actual.MotionFrame()
				bf, _ := control.MotionFrame()
				if af.Tick != bf.Tick || !slices.Equal(af.Samples, bf.Samples) || actual.emptyDistanceMeters != control.emptyDistanceMeters || actual.passengerDistanceMeters != control.passengerDistanceMeters {
					t.Fatalf("ordinary MotionFrame or accounting differs at tick %d", tick)
				}
				a, err := json.Marshal(actual.ExportState())
				if err != nil {
					t.Fatal(err)
				}
				b, err := json.Marshal(control.ExportState())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(a, b) || v.Pod != old.Pod || v.distance != old.distance || v.blocks.cursors != old.blocks.cursors {
					t.Fatalf("ordinary caller differs from frozen inverse control at tick %d", tick)
				}
				lane := v.blocks.currentLane(v.blockIndex)
				if v.Pod.Speed > lane.SpeedLimit {
					t.Fatal("actual current lane limit failed")
				}
				if v.distance == v.blocks.end(v.reservedThrough) && v.Pod.Speed == 0 {
					break
				}
				if tick == 19999 {
					t.Fatal("inverse fixture exceeded frozen tick cap")
				}
			}
		})
	}
}

func TestNativeForeignFleetBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*Simulation, *couplingMotionContext)
	}{
		{"missing_declared_foreign", func(_ *Simulation, c *couplingMotionContext) { c.foreignIDs = nil }},
		{"duplicate_actual_identity", func(s *Simulation, _ *couplingMotionContext) { s.vehicles[2].Pod.ID = s.vehicles[0].Pod.ID }},
		{"different_prepared_identity", func(s *Simulation, _ *couplingMotionContext) {
			detachIndexes(s)
			s.network.Nodes = slices.Clone(s.network.Nodes)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, c, _ := nativeForeignFixture(t)
			test.change(s, c)
			if _, err := prepareNativeForeignFleet(s, c); !errors.Is(err, errCouplingMotionInvariant) {
				t.Fatal("unbound actual fleet or geometry was accepted")
			}
		})
	}
	for _, saturated := range []bool{false, true} {
		t.Run(map[bool]string{false: "route_version", true: "saturated_route_replacement"}[saturated], func(t *testing.T) {
			t.Parallel()
			s, c, v := nativeForeignFixture(t)
			if saturated {
				v.routeVersion = math.MaxUint64
			}
			fleet, err := prepareNativeForeignFleet(s, c)
			if err != nil {
				t.Fatal(err)
			}
			if saturated {
				s.setVehicleRoute(v, slices.Clone(v.Route))
			} else {
				v.routeVersion++
			}
			s.tick++
			if _, err = buildNativeForeignTick(s, fleet); !errors.Is(err, errCouplingMotionInvariant) {
				t.Fatal("changed native immutable route reused a stale prepared certificate")
			}
		})
	}
}

func TestNativeForeignOwnerCapture(t *testing.T) {
	t.Parallel()
	for _, mutateView := range []bool{false, true} {
		t.Run(map[bool]string{false: "ledger_only", true: "captured_view"}[mutateView], func(t *testing.T) {
			t.Parallel()
			s, c, v := nativeForeignFixture(t)
			v.Pod.Activity, v.Pod.StationID, v.Pod.BerthID = Traveling, "", ""
			s.tick++
			fleet, err := prepareNativeForeignFleet(s, c)
			if err != nil {
				t.Fatal(err)
			}
			frame, err := buildNativeForeignTick(s, fleet)
			if err != nil {
				t.Fatal(err)
			}
			sweep := frame.sweeps()[0]
			if mutateView {
				for r := range sweep.Owners.owners {
					sweep.Owners.owners[r] = resourceOwner{kind: groupOwnerKind, id: v.Pod.ID}
					break
				}
				if !errors.Is(couplingCheckForeignMotion(sweep), errCouplingMotionInvariant) {
					t.Fatal("changed captured typed owner was accepted")
				}
				return
			}
			s.move(v)
			if err = frame.checkApplied(s); err != nil {
				t.Fatal("passing applied native control failed", err)
			}
			for r := range s.owners {
				s.owners[r] = resourceOwner{kind: groupOwnerKind, id: v.Pod.ID}
				break
			}
			if !errors.Is(frame.checkApplied(s), errCouplingMotionInvariant) {
				t.Fatal("ledger-only movement-stage change was accepted")
			}
		})
	}
}

func prepareNativeForeignFleet(s *Simulation, c *couplingMotionContext, pairs ...*couplingMotionContext) (*nativeForeignFleet, error) {
	if c == nil {
		return nil, couplingMotionInvariant("native fleet has no reference context")
	}
	f, err := prepareNativeForeignFleetBound(s, c.reservation.network, c.reservation.orderContract, pairs...)
	if err != nil {
		return nil, err
	}
	f.context = c
	var foreign []string
	members := 0
	for _, entry := range f.entries {
		if entry.id == c.reservation.members[0].Vehicle.Pod.ID || entry.id == c.reservation.members[1].Vehicle.Pod.ID {
			if entry.class != CompactClass {
				return nil, couplingMotionInvariant("native pair member changed its class")
			}
			members++
		} else {
			foreign = append(foreign, entry.id)
		}
	}
	slices.Sort(foreign)
	if members != 2 || !slices.Equal(foreign, c.foreignIDs) {
		return nil, couplingMotionInvariant("context excludes a different actual fleet")
	}
	return f, nil
}

// The caller invokes this once after native planning and before all movement.
// Later consumers read this frame, never the partly moved Simulation.
func buildNativeForeignTick(s *Simulation, f *nativeForeignFleet, pairs ...couplingNativeForeignPair) (*nativeForeignTick, error) {
	return buildNativeForeignApproachTick(s, f, nil, pairs...)
}

func (frame *nativeForeignTick) sweeps() []couplingForeignSweep {
	result := make([]couplingForeignSweep, 0, len(frame.proofs))
	for _, id := range frame.foreignIDs() {
		proof := frame.proofs[id]
		sweep := proof.raw
		sweep.native = proof
		result = append(result, sweep)
	}
	return result
}

func (frame *nativeForeignTick) foreignIDs() []string {
	if frame.fleet.context != nil {
		return frame.fleet.context.foreignIDs
	}
	ids := make([]string, 0, len(frame.fleet.entries))
	for _, entry := range frame.fleet.entries {
		ids = append(ids, entry.id)
	}
	slices.Sort(ids)
	return ids
}
