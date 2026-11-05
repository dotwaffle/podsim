package sim

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"hash"
	"slices"
	"testing"
)

type baselineResource struct {
	Kind int
	ID   string
	Cell int
}

type baselineOwner struct {
	Resource baselineResource
	Kind     uint8
	ID       string
}

type baselineRelease struct {
	Resource baselineResource
	Distance float64
}

type baselineVehicle struct {
	ID              string
	PhaseTicks      int
	Distance        float64
	ReservedThrough int
	Pending         int
	Releases        []baselineRelease
}

func baselineCompare(a, b baselineResource) int {
	return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID), cmp.Compare(a.Cell, b.Cell))
}

func baselineResourceOf(r resource) baselineResource {
	return baselineResource{Kind: int(r.kind), ID: r.id, Cell: r.cell}
}

// baselineObserve includes raw owners and release frontiers that views omit.
func baselineObserve(t *testing.T, digest hash.Hash, s *Simulation) {
	t.Helper()
	owners := make([]baselineOwner, 0, len(s.owners))
	for r, owner := range s.owners {
		owners = append(owners, baselineOwner{Resource: baselineResourceOf(r), Kind: uint8(owner.kind), ID: owner.id})
	}
	slices.SortFunc(owners, func(a, b baselineOwner) int { return baselineCompare(a.Resource, b.Resource) })
	vehicles := make([]baselineVehicle, 0, len(s.vehicles))
	for _, v := range s.vehicles {
		item := baselineVehicle{ID: v.Pod.ID, PhaseTicks: v.phaseTicks, Distance: v.distance, ReservedThrough: v.reservedThrough, Pending: v.pending}
		for r, distance := range v.routeReleases {
			item.Releases = append(item.Releases, baselineRelease{Resource: baselineResourceOf(r), Distance: distance})
		}
		slices.SortFunc(item.Releases, func(a, b baselineRelease) int { return baselineCompare(a.Resource, b.Resource) })
		vehicles = append(vehicles, item)
	}
	data, err := json.Marshal(struct {
		View     Snapshot
		Saved    SavedState
		Owners   []baselineOwner
		Vehicles []baselineVehicle
	}{s.Snapshot(), s.ExportState(), owners, vehicles}, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := digest.Write(data); err != nil {
		t.Fatal(err)
	}
	if _, err := digest.Write([]byte{'\n'}); err != nil {
		t.Fatal(err)
	}
}

// TestTripBaseline freezes every tick, not only final poses.
// This small corpus does not replace preset, session, or restore qualification.
func TestTripBaseline(t *testing.T) {
	for _, test := range []struct {
		name        string
		destination string
		ticks       int
		want        string
	}{
		{name: "harbor-garden", destination: "garden", ticks: 18000, want: "ac839248fd64bc0a92154d978868da2ac20862e5dd64817ea2ff727fb2064a4a"},
		{name: "harbor-market", destination: "market", ticks: 18000, want: "c6e29cb3cf71c422b0c1fcb174022fca79f161e4ed6a52a5673a86f6cdb59d81"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newExample(t)
			digest := sha256.New()
			baselineObserve(t, digest, s)
			if err := s.RequestJourney("01", test.destination); err != nil {
				t.Fatal(err)
			}
			baselineObserve(t, digest, s)
			var traveled, unloaded bool
			for range test.ticks {
				s.Step()
				if err := s.CheckContract(); err != nil {
					t.Fatalf("tick %d: %v", s.tick, err)
				}
				baselineObserve(t, digest, s)
				traveled = traveled || s.vehicles[0].Pod.Activity == Traveling
				unloaded = unloaded || s.vehicles[0].Pod.Activity == Unloading
			}
			if !traveled || !unloaded || s.completed == 0 {
				t.Fatalf("incomplete baseline: traveled=%t unloaded=%t completed=%d", traveled, unloaded, s.completed)
			}
			got := hex.EncodeToString(digest.Sum(nil))
			if got != test.want {
				t.Fatalf("baseline tick digest = %s, want %s", got, test.want)
			}
		})
	}
}
