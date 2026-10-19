package sim

import "testing"

// depotScopeKind returns the kind of depot pod v in the depot at index
// (see depotPodKinds), or "".
func (s *Simulation) depotScopeKind(v *vehicle, depot int) string {
	info := &s.depots.depots[depot]
	station := info.station.ID
	at, idle := s.depotIdleSlot(v)
	if idle && at.depot != depot {
		return ""
	}
	destination, bound := s.depots.slot(v.destination.ID)
	if bound && destination.depot != depot {
		return ""
	}
	last := resource{kind: trackResource, id: info.incoming.ID, cell: s.laneCells[info.incoming.ID].count() - 1}
	switch {
	case idle:
		return "idle"
	case s.depotParked(v):
		return "moving up"
	case bound && s.owners[resource{kind: berthResource, id: v.destination.ID}].isPod(v.Pod.ID):
		return "bound with its slot"
	case bound:
		return "bound without its slot"
	case s.depotUnbound(v, station) && s.owners[last].isPod(v.Pod.ID):
		return "unbound at the entry"
	}
	return ""
}

// TestDepotScopeRestore restores a congested run of three depots at many
// ticks. Each restore must keep every pod in place and match the live
// simulation, and the samples must hold each depot with an idle pod, a pod
// that moves up, a bound pod, and an unbound pod at the entry. Each depot
// starts with a pod behind the head of lane 1, which leaves first, so each
// depot has a move-up.
func TestDepotScopeRestore(t *testing.T) {
	t.Parallel()
	network := depotScopeNetwork(depotScopes)
	s := newDepotFleet(t, network, depotScopePlacements(depotScopes, []int{4, 4, 3}, true))
	for _, scope := range depotScopes {
		for _, id := range []string{"harbor-pod", "garden-pod", "market-pod"} {
			v := s.findVehicle(scope.prefix + id)
			if err := s.startEmptyMove(v, emptyDestination{station: scope.prefix + "parking", depot: true}); err != nil {
				t.Fatal(err)
			}
		}
	}
	stations := []string{"harbor", "garden", "market"}
	const demandEnd = 6 * 60 * TicksPerSecond
	seen := make(map[string]map[string]bool)
	trips := 0
	stepDepot(t, s, 60*60*TicksPerSecond, func() bool {
		if s.tick < demandEnd && s.tick%(8*TicksPerSecond) == 0 {
			for index, scope := range depotScopes {
				// The parts get different trips.
				n := int(s.tick/(8*TicksPerSecond)) + index
				if _, err := s.SubmitTrip(scope.prefix+stations[n%3], scope.prefix+stations[(n+1+n/3%2)%3]); err != nil {
					t.Fatal(err)
				}
				trips++
			}
		}
		kinds := make(map[string]map[string]bool)
		restore := s.tick%41 == 0
		for depot, info := range s.depots.depots {
			kinds[info.station.ID] = make(map[string]bool)
			for i := range s.vehicles {
				if kind := s.depotScopeKind(&s.vehicles[i], depot); kind != "" {
					kinds[info.station.ID][kind] = true
					restore = restore || kind == "unbound at the entry" || kind == "moving up" && s.tick%5 == 0
				}
			}
		}
		if restore {
			restoreDepot(t, network, s)
			for station, found := range kinds {
				if seen[station] == nil {
					seen[station] = make(map[string]bool)
				}
				for kind := range found {
					seen[station][kind] = true
				}
			}
		}
		return s.tick > demandEnd && s.completed == trips
	})
	for _, scope := range depotScopes {
		station := scope.prefix + "parking"
		for _, kind := range []string{"idle", "moving up", "unbound at the entry"} {
			if !seen[station][kind] {
				t.Errorf("no restore with a pod %s in %s: %v", kind, station, seen[station])
			}
		}
		if !seen[station]["bound with its slot"] && !seen[station]["bound without its slot"] {
			t.Errorf("no restore with a bound pod in %s: %v", station, seen[station])
		}
	}
}
