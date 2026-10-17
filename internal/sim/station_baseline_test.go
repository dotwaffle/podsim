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

type stationBaselineResource struct {
	Kind int
	ID   string
	Cell int
}

type stationBaselineOwner struct {
	Resource stationBaselineResource
	Kind     uint8
	ID       string
}

type stationBaselineRelease struct {
	Resource stationBaselineResource
	Distance float64
}

type stationBaselineVehicle struct {
	ID              string
	PhaseTicks      int
	Distance        float64
	ReservedThrough int
	Pending         int
	Releases        []stationBaselineRelease
}

func stationBaselineCompare(a, b stationBaselineResource) int {
	return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.ID, b.ID), cmp.Compare(a.Cell, b.Cell))
}

func stationBaselineResourceOf(r resource) stationBaselineResource {
	return stationBaselineResource{Kind: int(r.kind), ID: r.id, Cell: r.cell}
}

// stationBaselineObserve includes raw owners and release frontiers that views omit.
func stationBaselineObserve(t *testing.T, digest hash.Hash, s *Simulation) {
	t.Helper()
	owners := make([]stationBaselineOwner, 0, len(s.owners))
	for r, owner := range s.owners {
		owners = append(owners, stationBaselineOwner{Resource: stationBaselineResourceOf(r), Kind: uint8(owner.kind), ID: owner.id})
	}
	slices.SortFunc(owners, func(a, b stationBaselineOwner) int { return stationBaselineCompare(a.Resource, b.Resource) })
	vehicles := make([]stationBaselineVehicle, 0, len(s.vehicles))
	for _, v := range s.vehicles {
		item := stationBaselineVehicle{ID: v.Pod.ID, PhaseTicks: v.phaseTicks, Distance: v.distance, ReservedThrough: v.reservedThrough, Pending: v.pending}
		for r, distance := range v.routeReleases {
			item.Releases = append(item.Releases, stationBaselineRelease{Resource: stationBaselineResourceOf(r), Distance: distance})
		}
		slices.SortFunc(item.Releases, func(a, b stationBaselineRelease) int { return stationBaselineCompare(a.Resource, b.Resource) })
		vehicles = append(vehicles, item)
	}
	data, err := json.Marshal(struct {
		View     Snapshot
		Saved    SavedState
		Owners   []stationBaselineOwner
		Vehicles []stationBaselineVehicle
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

// TestStationIntegrationLegacyBaseline freezes every tick, not only final poses.
// This small corpus does not replace preset, session, or restore qualification.
func TestStationIntegrationLegacyBaseline(t *testing.T) {
	for _, test := range []struct {
		name        string
		destination string
		ticks       int
		want        string
	}{
		{name: "harbor-garden", destination: "garden", ticks: 18000, want: "913e0a899c296d1a65e19db7722c1bca879d2284a0401f52faa7235dc82dec94"},
		{name: "harbor-market", destination: "market", ticks: 18000, want: "653c37f7ec317d5f4bcd948b3f838fc70613c4f1f5b7d3ad6dfd2843d4df27a4"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := newExample(t)
			digest := sha256.New()
			stationBaselineObserve(t, digest, s)
			if err := s.RequestJourney("01", test.destination); err != nil {
				t.Fatal(err)
			}
			stationBaselineObserve(t, digest, s)
			var traveled, unloaded bool
			for range test.ticks {
				s.Step()
				if err := s.CheckContract(); err != nil {
					t.Fatalf("tick %d: %v", s.tick, err)
				}
				stationBaselineObserve(t, digest, s)
				traveled = traveled || s.vehicles[0].Pod.Activity == Traveling
				unloaded = unloaded || s.vehicles[0].Pod.Activity == Unloading
			}
			if !traveled || !unloaded || s.completed == 0 {
				t.Fatalf("incomplete baseline: traveled=%t unloaded=%t completed=%d", traveled, unloaded, s.completed)
			}
			got := hex.EncodeToString(digest.Sum(nil))
			if got != test.want {
				t.Fatalf("legacy native-tick digest = %s, want %s", got, test.want)
			}
		})
	}
}
