package sim

import (
	"maps"
	"reflect"
	"testing"
)

// These tests inject inactive owner tags into existing native callers.
// They do not form or move a mechanical train.
func TestResourceOwnerGrantIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		owner resourceOwner
		grant bool
	}{
		{name: "free", grant: true},
		{name: "same pod", owner: podResourceOwner("01"), grant: true},
		{name: "foreign pod", owner: podResourceOwner("02")},
		{name: "colliding group", owner: resourceOwner{kind: groupOwnerKind, id: "01"}},
		{name: "colliding unknown", owner: resourceOwner{kind: 255, id: "01"}},
		{name: "unknown zero kind", owner: resourceOwner{id: "01"}},
		{name: "group without ID", owner: resourceOwner{kind: groupOwnerKind}},
		{name: "pod without ID", owner: resourceOwner{kind: podOwnerKind}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := resource{kind: trackResource, id: "lane", cell: 0}
			v := vehicle{Pod: Pod{ID: "01", Activity: Traveling}, reservedThrough: -1}
			v.blocks = blockListOf([]block{{lane: Lane{ID: "lane", SpeedLimit: 14}, end: 30, resources: []resource{r}}})
			s := &Simulation{vehicles: []vehicle{v}, owners: map[resource]resourceOwner{r: test.owner}}
			before := maps.Clone(s.owners)
			s.grant(intent{index: 0, block: 0})
			if test.grant {
				if s.owners[r] != (resourceOwner{kind: podOwnerKind, id: "01"}) || s.vehicles[0].reservedThrough != 0 {
					t.Fatal("ordinary grant did not retain the individual pod owner")
				}
				return
			}
			if !maps.Equal(before, s.owners) || s.vehicles[0].reservedThrough != -1 || len(s.vehicles[0].routeReleases) != 0 {
				t.Fatal("denied owner changed a claim, grant, or retention ledger")
			}
			if s.vehicles[0].Pod.WaitReason != TrackOccupied || s.vehicles[0].Pod.BlockedBy == "" {
				t.Fatal("foreign owner did not produce an occupied-track signal")
			}
		})
	}
}

func TestResourceOwnerVirtualBorrow(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		owner resourceOwner
		grant bool
	}{
		{name: "individual predecessor", owner: podResourceOwner("front"), grant: true},
		{name: "group named as predecessor", owner: resourceOwner{kind: groupOwnerKind, id: "front"}},
		{name: "unknown named as predecessor", owner: resourceOwner{kind: 255, id: "front"}},
		{name: "group named as follower", owner: resourceOwner{kind: groupOwnerKind, id: "rear"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := resource{kind: trackResource, id: "lane", cell: 0}
			blocks := blockListOf([]block{{lane: Lane{ID: "lane", SpeedLimit: 14}, end: 30, resources: []resource{r}}})
			front := vehicle{Pod: Pod{ID: "front", Activity: Traveling}, blocks: blocks, reservedThrough: 0, follower: 2}
			rear := vehicle{Pod: Pod{ID: "rear", Activity: Traveling}, blocks: blocks, reservedThrough: -1,
				link: platoonLink{leader: 1, lane: 0, lanes: 1, end: 0}}
			s := &Simulation{platooning: PlatooningVirtual, vehicles: []vehicle{front, rear}, owners: map[resource]resourceOwner{r: test.owner}}
			s.grant(intent{index: 1, block: 0})
			if s.owners[r] != test.owner {
				t.Fatal("virtual grant changed the physical resource owner")
			}
			if test.grant {
				if s.vehicles[1].reservedThrough != 0 || len(s.vehicles[1].routeReleases) != 1 {
					t.Fatal("individual virtual predecessor did not share its grant")
				}
			} else if s.vehicles[1].reservedThrough != -1 || len(s.vehicles[1].routeReleases) != 0 {
				t.Fatal("virtual link borrowed a group or unknown owner")
			}
		})
	}
}

func TestResourceOwnerVirtualHandoff(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		owner resourceOwner
		want  resourceOwner
	}{
		{name: "individual handoff", owner: podResourceOwner("front"), want: podResourceOwner("rear")},
		{name: "group keeps dependency", owner: resourceOwner{kind: groupOwnerKind, id: "front"}, want: resourceOwner{kind: groupOwnerKind, id: "front"}},
		{name: "unknown keeps dependency", owner: resourceOwner{kind: 255, id: "front"}, want: resourceOwner{kind: 255, id: "front"}},
		{name: "foreign pod keeps dependency", owner: podResourceOwner("other"), want: podResourceOwner("other")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r := resource{kind: junctionResource, id: "junction"}
			front := vehicle{Pod: Pod{ID: "front"}, follower: 2}
			rear := vehicle{Pod: Pod{ID: "rear"}, routeReleases: map[resource]float64{r: 100}}
			s := &Simulation{vehicles: []vehicle{front, rear}, owners: map[resource]resourceOwner{r: test.owner}}
			s.releaseRouteResource(&s.vehicles[0], r)
			if s.owners[r] != test.want {
				t.Fatal("individual release substituted a foreign retained owner")
			}
		})
	}
}

func TestResourceOwnerRetainedDependency(t *testing.T) {
	t.Parallel()
	for _, owner := range []resourceOwner{
		{kind: groupOwnerKind, id: "01"}, {kind: 255, id: "01"}, {kind: groupOwnerKind},
	} {
		t.Run(owner.String(), func(t *testing.T) {
			t.Parallel()
			r := resource{kind: junctionResource, id: "junction"}
			v := vehicle{Pod: Pod{ID: "01", Activity: Traveling}, distance: 60, nextRelease: 0,
				routeReleases: map[resource]float64{r: 50}}
			s := &Simulation{vehicles: []vehicle{v}, owners: map[resource]resourceOwner{r: owner}}
			s.releaseOwned(&s.vehicles[0], r)
			s.releasePassedResources(&s.vehicles[0])
			if s.owners[r] != owner {
				t.Fatal("individual release removed a foreign retained dependency")
			}
		})
	}
}

func TestResourceOwnerBufferYield(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		kind ownerKind
	}{
		{name: "individual", kind: podOwnerKind},
		{name: "group", kind: groupOwnerKind},
		{name: "unknown", kind: 255},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := bufferedHeadWithClaim(t, true)
			head := s.findVehicle("01")
			for _, r := range berthResources(s.findVehicle("02").destination) {
				s.owners[r] = resourceOwner{kind: test.kind, id: "02"}
			}
			before, owners := s.ExportState(), maps.Clone(s.owners)
			s.grant(intent{index: 0, block: head.pending, since: head.waitSince})
			if test.kind == podOwnerKind {
				if head.destination.ID != "market-1" || s.owners[resource{kind: berthResource, id: "market-1"}] != (resourceOwner{kind: podOwnerKind, id: "01"}) {
					t.Fatal("ordinary remote berth claim did not yield")
				}
				return
			}
			if !maps.Equal(owners, s.owners) || !reflect.DeepEqual(before.Pods, s.ExportState().Pods) {
				t.Fatal("buffered grant yielded or changed a group or unknown claim")
			}
		})
	}
}

func TestResourceOwnerRestoreRelease(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		owner resourceOwner
		keep  bool
	}{
		{name: "individual releases", owner: podResourceOwner("01")},
		{name: "group remains", owner: resourceOwner{kind: groupOwnerKind, id: "01"}, keep: true},
		{name: "unknown remains", owner: resourceOwner{kind: 255, id: "01"}, keep: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newTraffic(t)
			r := resource{kind: trackResource, id: "retained", cell: 0}
			s.owners[r] = test.owner
			station, _ := s.station("parking")
			restore := physicalRestore{s: s}
			restore.moveTo(&s.vehicles[0], station.Berths[0])
			got, held := s.owners[r]
			if held != test.keep || held && got != test.owner {
				t.Fatal("restore release confused pod and foreign retained ownership")
			}
		})
	}
}

func TestResourceOwnerBerthLoad(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	station, _ := s.station("parking")
	berth := station.Berths[0]
	s.vehicles[0].Pod.Activity = DepartingEmpty
	s.vehicles[0].destination = berth
	s.owners[resource{kind: berthResource, id: berth.ID}] = resourceOwner{kind: groupOwnerKind, id: "01"}
	s.owners[resource{kind: nodeResource, id: berth.Node}] = resourceOwner{kind: podOwnerKind, id: "01"}
	if got := s.berthLoad(berth); got != 2 {
		t.Fatalf("colliding pod and group berth load = %d, want 2", got)
	}
	if s.berthAvailable(berth) {
		t.Fatal("group-held berth appeared free")
	}
}

func TestResourceOwnerCloneAndPresentation(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	before := s.ExportState()
	r := resource{kind: trackResource, id: "retained", cell: 0}
	owner := resourceOwner{kind: groupOwnerKind, id: "01"}
	s.owners[r] = owner
	clone := s.Clone()
	delete(clone.owners, r)
	if s.owners[r] != owner || !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("owner clone shared mutable storage or changed ordinary saved state")
	}
	for _, berth := range s.Snapshot().Berths {
		if berth.Occupant != "" && berth.ReservedBy != berth.Occupant {
			t.Fatal("ordinary berth presentation lost the original pod ID")
		}
	}
}
