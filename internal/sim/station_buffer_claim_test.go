package sim

import (
	"maps"
	"reflect"
	"slices"
	"testing"
)

// bufferedHeadWithRelocation has an empty pod behind a passenger head.
// The empty pod claims the only berth after it becomes a released pickup.
func bufferedHeadWithRelocation(t *testing.T) *Simulation {
	t.Helper()
	return bufferedHeadWithClaim(t, false)
}

func bufferedHeadWithClaim(t *testing.T, departing bool) *Simulation {
	t.Helper()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	barrier := resource{kind: berthResource, id: "market-1"}
	s.owners[barrier] = podResourceOwner("external")
	stepUntil(t, s, "passenger head at frontier", func() bool {
		v := s.findVehicle("01")
		plan, ok := s.bufferPlan(v)
		return ok && v.Pod.Speed == 0 && v.distance == v.blocks.end(plan.frontier)
	})
	station, _ := s.station("market")
	if departing {
		delete(s.owners, barrier)
	}
	if err := s.startEmptyMove(s.findVehicle("02"), emptyDestination{station: station.ID, berth: station.Berths[0], reserveBerth: departing}); err != nil {
		t.Fatal(err)
	}
	if departing {
		s.findVehicle("02").released = true
		return s
	}
	stepUntil(t, s, "empty relocation behind head", func() bool {
		v := s.findVehicle("02")
		return v.Pod.Speed == 0 && v.Pod.BlockedBy == "01"
	})
	delete(s.owners, barrier)
	empty := s.findVehicle("02")
	empty.released = true
	s.parkReleased(empty)
	if empty.destination.ID != "market-1" || s.owners[barrier] != podResourceOwner("02") || s.relocationDestinationAdmitted(empty) {
		t.Fatal("fixture did not claim the unadmitted berth behind the head")
	}
	return s
}

func TestStationBufferDefersReleasedClaimantReroute(t *testing.T) {
	t.Parallel()
	s := bufferedHeadWithClaim(t, true)
	empty := s.findVehicle("02")
	if empty.Pod.Activity != DepartingEmpty || empty.reservedThrough != -1 {
		t.Fatal("claimant has already entered its route")
	}
	route := slices.Clone(empty.Route)
	s.owners[resource{kind: trackResource, id: route[0].ID, cell: 0}] = podResourceOwner("external")
	s.admit()
	if s.owners[resource{kind: berthResource, id: "market-1"}] != podResourceOwner("01") {
		t.Fatal("head did not acquire the remote berth claim")
	}
	if empty.Pod.Activity != DepartingEmpty || !slices.Equal(empty.Route, route) {
		t.Fatal("claimant rerouted while its admission intent was queued")
	}
	delete(s.owners, resource{kind: trackResource, id: route[0].ID, cell: 0})
	loaded, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
		t.Fatalf("yielded claim restore: %+v %v", result, err)
	}
	for _, continuation := range []*Simulation{s, loaded} {
		continuation.Step()
		if v := continuation.findVehicle("02"); v.Pod.Activity != Idle || v.Pod.StationID != "garden" {
			t.Fatal("next dispatch did not return the released claimant to its origin berth")
		}
		checkTraffic(t, continuation.Snapshot())
		if _, err := continuation.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStationBufferYieldsUnadmittedRelocationClaim(t *testing.T) {
	t.Parallel()
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "restore-disabled"}[restore], func(t *testing.T) {
			t.Parallel()
			s := bufferedHeadWithRelocation(t)
			if restore {
				loaded, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
				if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
					t.Fatalf("buffer cycle restore: %+v %v", result, err)
				}
				s = loaded
			}
			for range 600 * TicksPerSecond {
				s.Step()
				checkTraffic(t, s.Snapshot())
				if _, err := s.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				if s.completed == 1 && s.findVehicle("02").Pod.Activity == Idle {
					return
				}
			}
			t.Fatal("buffer head and released relocation did not clear")
		})
	}
}

func TestStationBufferFailedPathKeepsRemoteClaims(t *testing.T) {
	t.Parallel()
	s := bufferedHeadWithRelocation(t)
	s.owners[resource{kind: trackResource, id: "market-in", cell: 0}] = podResourceOwner("external")
	head, empty := s.findVehicle("01"), s.findVehicle("02")
	owners := maps.Clone(s.owners)
	before := s.ExportState()
	nextRelease := empty.nextRelease
	for range 2 {
		s.grant(intent{index: 0, block: head.pending, since: head.waitSince})
	}
	after := s.ExportState()
	if !maps.Equal(owners, s.owners) || !reflect.DeepEqual(before.Pods, after.Pods) || empty.nextRelease != nextRelease {
		t.Fatal("failed complete path changed a remote claim or physical state")
	}
}

func TestStationBufferProtectsAdmittedRelocationClaim(t *testing.T) {
	t.Parallel()
	s := bufferedHeadWithRelocation(t)
	empty := s.findVehicle("02")
	empty.reservedThrough = empty.blocks.len() - 1
	if !s.relocationDestinationAdmitted(empty) {
		t.Fatal("fixture did not admit the relocation destination")
	}
	owners := maps.Clone(s.owners)
	head := s.findVehicle("01")
	s.grant(intent{index: 0, block: head.pending, since: head.waitSince})
	if !maps.Equal(owners, s.owners) || head.destination.ID != "" {
		t.Fatal("buffer head took an admitted relocation claim")
	}
}
