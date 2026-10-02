package sim

import (
	"maps"
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestPickupReassignmentAtAssignment(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	s.waiting[0].request.PodID = ""
	s.findVehicle("01").released = true
	s.SetPickupSwaps(true)
	s.dispatch()
	if s.PickupSwapStats().AssignmentChecks != 1 || s.PickupSwapStats().Swaps != 1 ||
		s.waiting[0].request.PodID != "02" || s.waiting[1].request.PodID != "01" {
		t.Fatalf("assignment did not check crossed pickups immediately: %+v %+v", s.PickupSwapStats(), s.waiting)
	}
	if _, err := s.ExportState().checkContract(); err != nil {
		t.Fatal(err)
	}
}

func pickupTransferFixture(t *testing.T, alternative string) *Simulation {
	t.Helper()
	s, err := NewFleet(lineNetwork(lineStations(0, 2, 2, 2, 2)), place("s0-1", "s2-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.sendPickup(s.findVehicle("01"), "s3"); err != nil {
		t.Fatal(err)
	}
	s.requestID = 1
	s.waiting = []waitingTrip{{request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, PodID: "01", From: "s3", To: "s0", PartySize: 1}}}
	s.SetExperimentRecords(true)
	if alternative != "idle" {
		v := s.findVehicle("02")
		target := "s0"
		if alternative == "parking" {
			target = "s1"
		}
		station, _ := s.station(target)
		if err := s.startEmptyMove(v, emptyDestination{station: station.ID, berth: station.Berths[0], rebalance: alternative == "repositioning"}); err != nil {
			t.Fatal(err)
		}
		if alternative == "parking" {
			// A parking station is an eligible destination for empty diversion.
			s.network.Stations[1].ParkingOnly = true
		}
	}
	advance(s, 5*TicksPerSecond)
	return s
}

func TestPickupReassignmentEmptyAlternatives(t *testing.T) {
	t.Parallel()
	for _, alternative := range []string{"idle", "parking", "repositioning"} {
		t.Run(alternative, func(t *testing.T) {
			t.Parallel()
			s := pickupTransferFixture(t, alternative)
			control := s.Clone()
			v := s.findVehicle("02")
			before := *v
			owners := maps.Clone(s.owners)
			prefix, _, _ := s.divertStart(v)
			s.SetPickupSwaps(true)
			s.swapPickups()
			if s.PickupSwapStats().Transfers != 1 || s.waiting[0].request.PodID != "02" {
				t.Fatalf("empty alternative not assigned: %+v %+v", s.PickupSwapStats(), s.waiting)
			}
			if alternative != "idle" && (v.distance != before.distance || v.Pod.Speed != before.Pod.Speed ||
				v.Pod.Position != before.Pod.Position || v.reservedThrough != before.reservedThrough || !slices.Equal(v.Route[:prefix], before.Route[:prefix])) {
				t.Fatal("transfer changed committed motion or route prefix")
			}
			for resource, owner := range owners {
				if resource.kind == trackResource && s.owners[resource] != owner {
					t.Fatal("transfer changed admitted track owners")
				}
			}
			if alternative == "idle" && s.owners[resource{kind: berthResource, id: "s2-1"}] != "02" {
				t.Fatal("idle replacement lost its origin claim")
			}
			if s.waiting[0].request.ID != control.waiting[0].request.ID || s.waiting[0].request.RequestedTick != control.waiting[0].request.RequestedTick {
				t.Fatal("transfer changed request identity or age")
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
			if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
				t.Fatalf("transferred state did not restore physically: %+v %v", result, err)
			}
			for _, simulation := range []*Simulation{control, s, restored} {
				for range 600 * TicksPerSecond {
					simulation.Step()
					if _, err := simulation.SafetyObservation().Check(); err != nil {
						t.Fatal(err)
					}
					if _, err := simulation.ExportState().checkContract(); err != nil {
						t.Fatal(err)
					}
					if simulation.completed == 1 {
						break
					}
				}
				if simulation.completed != 1 {
					t.Fatal("transfer lost or stalled its request")
				}
			}
			if s.RequestTimings()[0].BoardedTick >= control.RequestTimings()[0].BoardedTick {
				t.Fatal("replacement did not improve the controlled pickup")
			}
		})
	}
}

func TestPickupReassignmentSharedBudget(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	s.SetPickupSwaps(true)
	s.pickupSwaps.resetBudget(s.tick)
	s.pickupSwaps.scans = pickupSwapScanLimit
	before := s.ExportState()
	if s.reassignPickup(0) {
		t.Fatal("assignment ignored the shared scan limit")
	}
	s.swapPickups()
	if s.PickupSwapStats().RoutePairs != 0 || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("periodic check ignored assignment's exhausted budget")
	}
	s.tick += TicksPerSecond
	if !s.reassignPickup(0) || s.PickupSwapStats().Swaps != 1 {
		t.Fatal("new simulated second did not restore the work budget")
	}
	before = s.ExportState()
	if s.reassignPickup(0) || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("immediate repeated assignment bypassed cooldowns")
	}
}

func TestPickupTransferImprovement(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		before, after float64
		want          bool
	}{
		{name: "faster", before: 100, after: 80, want: true},
		{name: "equal", before: 100, after: 100},
		{name: "slower", before: 100, after: 120},
		{name: "small gain", before: 100, after: 95},
		{name: "unknown", before: math.Inf(1), after: 20},
		{name: "invalid", before: 100, after: math.NaN()},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := pickupTransferImproves(test.before, test.after); got != test.want {
				t.Fatalf("transfer improvement=%t want%t", got, test.want)
			}
		})
	}
}

func bufferedCrossedPickupFixture(t *testing.T) *Simulation {
	t.Helper()
	network := lineNetwork(lineStations(0, 2, 2, 2, 2))
	for index := range network.Lanes {
		lane := &network.Lanes[index]
		for _, station := range network.Stations {
			if lane.To == station.Entry {
				lane.StationID, lane.StationRole = station.ID, StationEntryRole
			}
		}
	}
	s, err := NewFleet(network, place("s0-1", "s2-1"))
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	for index, pickup := range []struct{ from, to string }{{"s3", "s0"}, {"s1", "s2"}} {
		v := &s.vehicles[index]
		if err := s.sendPickup(v, pickup.from); err != nil {
			t.Fatal(err)
		}
		s.requestID++
		s.waiting = append(s.waiting, waitingTrip{request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: s.requestID, PodID: v.Pod.ID, From: pickup.from, To: pickup.to, PartySize: 1}})
	}
	advance(s, 5*TicksPerSecond)
	return s
}

func TestPickupReassignmentUpstreamBuffers(t *testing.T) {
	t.Parallel()
	s := bufferedCrossedPickupFixture(t)
	for _, v := range s.vehicles {
		if !v.buffered || v.destination.ID != "" {
			t.Fatal("fixture did not dispatch buffered pickups")
		}
	}
	before := s.ExportState()
	s.SetPickupSwaps(true)
	s.swapPickups()
	if s.PickupSwapStats().Swaps != 1 {
		t.Fatalf("upstream buffer pickups did not swap: %+v", s.PickupSwapStats())
	}
	for index, v := range s.vehicles {
		if !v.buffered || v.destination.ID != "" || v.distance != before.Pods[index].Distance {
			t.Fatal("upstream swap lost buffer membership or motion")
		}
	}
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), StationBuffers: true})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
		t.Fatalf("swapped buffer pickups did not restore physically: %+v %v", result, err)
	}
	for range 600 * TicksPerSecond {
		restored.Step()
		if _, err := restored.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		if _, err := restored.ExportState().checkContract(); err != nil {
			t.Fatal(err)
		}
		if restored.completed == 2 {
			return
		}
	}
	t.Fatal("restored swapped buffers did not complete both requests")
}

func TestPickupReassignmentKeepsCommittedBuffer(t *testing.T) {
	t.Parallel()
	s := bufferedCrossedPickupFixture(t)
	v := s.findVehicle("01")
	stepUntil(t, s, "reserved buffer entry", func() bool {
		plan, ok := s.bufferPlan(v)
		return ok && v.reservedThrough >= plan.first
	})
	if _, _, ok := s.divertStart(v); !ok {
		t.Fatal("fixture already committed to a berth")
	}
	s.SetPickupSwaps(true)
	before := s.ExportState()
	s.swapPickups()
	if s.PickupSwapStats().Swaps+s.PickupSwapStats().Transfers != 0 || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("reassignment changed a committed buffer entry")
	}
	// Dispatch can release this member to a local idle pod before parking.
	s.waiting[0].request.PodID = ""
	s.releasePickup(v)
	if s.freePickupAlternative(v) {
		t.Fatal("released member bypassed the committed buffer exclusion")
	}
	before = s.ExportState()
	s.tick += TicksPerSecond
	before.Tick = s.tick
	s.swapPickups()
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("released committed buffer changed during a replacement check")
	}
}

func TestPickupReassignmentRecords(t *testing.T) {
	t.Parallel()
	s := crossedPickupFixture(t)
	s.SetPickupSwaps(true)
	s.swapPickups()
	records := s.PickupReassignments()
	if len(records) != 2 {
		t.Fatalf("swap recorded %d affected requests, want2", len(records))
	}
	for _, record := range records {
		if record.Tick != s.tick || record.RequestID < 1 || record.RequestID > 2 ||
			record.OldPod == record.NewPod || record.NewSeconds >= record.OldSeconds {
			t.Fatalf("invalid reassignment record: %+v", record)
		}
	}
	records[0].NewPod = "changed"
	if s.PickupReassignments()[0].NewPod == "changed" {
		t.Fatal("record read returned shared storage")
	}
	clone := s.Clone()
	clone.pickupSwaps.records[0].NewPod = "changed"
	if s.PickupReassignments()[0].NewPod == "changed" {
		t.Fatal("clone shared reassignment records")
	}
	s.SetExperimentRecords(false)
	if s.PickupReassignments() != nil || s.pickupSwaps.records != nil {
		t.Fatal("disabled experiment recording retained history")
	}
	s.SetExperimentRecords(true)
	if len(s.PickupReassignments()) != 0 {
		t.Fatal("reenabling recording recovered discarded history")
	}
	clone.Reset()
	if len(clone.PickupReassignments()) != 0 {
		t.Fatal("reset retained reassignment history")
	}
}
