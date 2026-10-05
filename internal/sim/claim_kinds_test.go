package sim

import (
	"slices"
	"testing"
)

// claimKindFixture is yieldFixture after the relocating pod passes its
// origin tail. Pod 01 travels empty to Market and holds the Market berth
// and its node. Its reserved track does not reach Market. Pod 02 is idle at
// Garden.
func claimKindFixture(t *testing.T) (s *Simulation, relocating, idle *vehicle) {
	t.Helper()
	s, relocating, idle = yieldFixture(t)
	for range 20 * TicksPerSecond {
		if relocating.originReleased {
			break
		}
		s.Step()
	}
	if relocating.Pod.Activity != Traveling || !relocating.originReleased || s.relocationDestinationAdmitted(relocating) {
		t.Fatalf("pod 01 is not traveling to an unadmitted Market berth: %+v", relocating.Pod)
	}
	return s, relocating, idle
}

func TestClaimKind(t *testing.T) {
	t.Parallel()
	market := resource{kind: berthResource, id: "market-1"}
	marketNode := resource{kind: nodeResource, id: "market-berth"}
	garden := resource{kind: berthResource, id: "garden-1"}
	// elsewhere is a resource that no route of the fixture uses.
	elsewhere := resource{kind: nodeResource, id: "claim-kind-test"}
	tests := []struct {
		name  string
		setup func(s *Simulation, relocating, idle *vehicle) (*vehicle, resource)
		want  claimKind
	}{
		{"not held", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			return relocating, garden
		}, claimNotHeld},
		{"service berth", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			return relocating, market
		}, claimService},
		{"service node", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			return relocating, marketNode
		}, claimService},
		{"coupled member", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			relocating.couplingID = "pair"
			return relocating, market
		}, claimCommitted},
		{"approach member", func(s *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			c := &couplingApproachContext{}
			c.members[1].id = relocating.Pod.ID
			s.couplingApproaches = append(s.couplingApproaches, couplingNativeApproach{context: c})
			return relocating, market
		}, claimCommitted},
		{"group owner", func(s *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			s.owners[market] = resourceOwner{kind: groupOwnerKind, id: "pair"}
			relocating.routeReleases[market] = relocating.distance
			return relocating, market
		}, claimCommitted},
		{"group claim", func(s *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			s.couplingGroups = append(s.couplingGroups, couplingNativeGroup{context: &couplingMotionContext{claims: []couplingClaim{{Resource: market}}}})
			return relocating, market
		}, claimCommitted},
		{"group preserved claim", func(s *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			c := &couplingMotionContext{}
			c.reservation.PreservedClaims = []couplingClaim{{Resource: marketNode, Expected: podResourceOwner(relocating.Pod.ID)}}
			s.couplingGroups = append(s.couplingGroups, couplingNativeGroup{context: c})
			return relocating, marketNode
		}, claimCommitted},
		{"idle at berth", func(_ *Simulation, _, idle *vehicle) (*vehicle, resource) {
			return idle, garden
		}, claimOccupied},
		{"origin before release", func(s *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			origin := resource{kind: berthResource, id: relocating.origin.ID}
			s.owners[origin], relocating.originReleased = podResourceOwner(relocating.Pod.ID), false
			return relocating, origin
		}, claimOccupied},
		{"service claim at origin", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			// A released pod can go back to its origin berth.
			relocating.origin, relocating.originReleased = relocating.destination, false
			return relocating, market
		}, claimOccupied},
		{"footprint", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			return relocating, relocating.footprint(relocating.blockIndex, relocating.distance)[0]
		}, claimOccupied},
		{"admitted destination", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			relocating.reservedThrough = relocating.blocks.len() - 1
			return relocating, marketNode
		}, claimStopping},
		{"retained", func(s *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			s.owners[elsewhere] = podResourceOwner(relocating.Pod.ID)
			relocating.routeReleases[elsewhere] = relocating.distance + 1
			return relocating, elsewhere
		}, claimRetained},
		{"retained service claim", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			relocating.routeReleases[market] = relocating.distance + 1
			return relocating, market
		}, claimRetained},
		{"lent", func(s *Simulation, relocating, idle *vehicle) (*vehicle, resource) {
			relocating.follower = vehicleIndex(s, idle) + 1
			idle.routeReleases = map[resource]float64{market: idle.distance + 1}
			return relocating, market
		}, claimLent},
		{"passenger destination", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			relocating.Pod.Occupied = true
			return relocating, market
		}, claimOther},
		{"boarding passenger", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			relocating.Pod.Activity = Boarding
			relocating.Riders = []Request{{ID: 1, From: "parking", To: "market", PartySize: 1}}
			return relocating, market
		}, claimOther},
		{"no relocation", func(_ *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			relocating.RelocatingTo = ""
			return relocating, market
		}, claimOther},
		{"stale retention", func(s *Simulation, relocating, _ *vehicle) (*vehicle, resource) {
			relocating.routeReleases[elsewhere] = relocating.distance
			return relocating, elsewhere
		}, claimOther},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, relocating, idle := claimKindFixture(t)
			v, r := test.setup(s, relocating, idle)
			if got := s.claimKind(v, r); got != test.want {
				t.Fatalf("claimKind(%s, %v) = %d, want %d", v.Pod.ID, r, got, test.want)
			}
			if got := s.revocable(v, r); got != (test.want == claimService) {
				t.Fatalf("revocable(%s, %v) = %t", v.Pod.ID, r, got)
			}
		})
	}
}

// TestInFootprintMatchesFootprint compares inFootprint with a search of
// footprint in a busy traffic run. On each fifth tick, each traveling pod
// tests the resources of its route, its origin berth, its destination
// berth, and one resource that no route uses, at its block index and at its
// reserved block.
func TestInFootprintMatchesFootprint(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(twoBerthMarket(), []Placement{
		{ID: "01", StationID: "parking", BerthID: "parking-1"},
		{ID: "02", StationID: "parking", BerthID: "parking-2"},
		{ID: "03", StationID: "garden"},
		{ID: "04", StationID: "harbor"},
		{ID: "05", StationID: "market", BerthID: "market-1"},
		{ID: "06", StationID: "market", BerthID: "market-2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	pairs := [][2]string{
		{"market", "garden"}, {"garden", "market"}, {"harbor", "market"}, {"market", "harbor"},
		{"market", "garden"}, {"garden", "harbor"}, {"harbor", "garden"}, {"market", "harbor"},
	}
	elsewhere := resource{kind: nodeResource, id: "claim-kind-test"}
	held, free, origin := 0, 0, 0
	for tick := range 200 * TicksPerSecond {
		if tick%(10*TicksPerSecond) == 0 {
			rebalanceToMarket(s)
		}
		if second := tick / TicksPerSecond; tick%TicksPerSecond == 0 && second%7 == 0 && second/7 < 29 {
			pair := pairs[(second/7)%len(pairs)]
			if err := s.RequestTrip(pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if tick%5 != 0 || v.Pod.Activity != Traveling {
				continue
			}
			candidates := []resource{elsewhere}
			for resources := range v.blocks.spanResources(0, v.blocks.len()) {
				candidates = append(candidates, resources...)
			}
			originClaims, destinationClaims := berthResources(v.origin), berthResources(v.destination)
			candidates = append(append(candidates, originClaims[:]...), destinationClaims[:]...)
			for _, through := range []int{v.blockIndex, v.reservedThrough} {
				footprint := v.footprint(through, v.distance)
				for _, r := range candidates {
					want := slices.Contains(footprint, r)
					if got := v.inFootprint(r, through, v.distance); got != want {
						t.Fatalf("tick %d: pod %s through %d: inFootprint(%v) = %t, footprint %v", s.tick, v.Pod.ID, through, r, got, footprint)
					}
					switch {
					case want && ofBerth(v.origin, r) && v.distance < v.originTail():
						origin++
					case want:
						held++
					default:
						free++
					}
				}
			}
		}
		s.Step()
	}
	if held == 0 || free == 0 || origin == 0 {
		t.Fatalf("the run did not test all cases: %d held, %d free, %d origin", held, free, origin)
	}
}

func vehicleIndex(s *Simulation, v *vehicle) int {
	for i := range s.vehicles {
		if &s.vehicles[i] == v {
			return i
		}
	}
	return -1
}

// A group owner never reaches the classification from the two callers.
// bufferClaimCanYield finds no remote pod for the owner, and
// yieldRelocationClaims releases only a claim that the relocating pod owns.
// The control case shows that the same state yields a pod-owned claim.
func TestClaimYieldSkipsGroupOwner(t *testing.T) {
	t.Parallel()
	for _, group := range []bool{false, true} {
		s, relocating, head := claimKindFixture(t)
		head.destination = relocating.destination
		assign(s, head)
		claims := berthResources(relocating.destination)
		if group {
			for _, r := range claims {
				s.owners[r] = resourceOwner{kind: groupOwnerKind, id: "pair"}
			}
		}
		_, buffered := s.bufferBerthClaims(head, relocating.destination)
		s.yieldRelocationClaims()
		kept := s.owners[claims[0]].kind == groupOwnerKind && s.owners[claims[1]].kind == groupOwnerKind
		if buffered == group || kept != group {
			t.Fatalf("group=%t: buffer yield %t, claims %v %v", group, buffered, s.owners[claims[0]], s.owners[claims[1]])
		}
	}
}

// A released pod parks at another berth only after it yields a claim. When
// it holds no revocable claim, it keeps its destination.
func TestYieldRelocationClaimsKeepsReleasedPodWithoutRevocableClaim(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(s *Simulation, relocating, other *vehicle)
	}{
		{"admitted", func(_ *Simulation, relocating, _ *vehicle) {
			relocating.reservedThrough = relocating.blocks.len() - 1
		}},
		{"retention entry on another pod's claim", func(s *Simulation, relocating, other *vehicle) {
			for _, r := range berthResources(relocating.destination) {
				s.owners[r] = podResourceOwner(other.Pod.ID)
				relocating.routeReleases[r] = relocating.distance
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, relocating, other := claimKindFixture(t)
			relocating.Rebalancing, relocating.released = false, true
			other.destination = relocating.destination
			assign(s, other)
			test.setup(s, relocating, other)
			destination := relocating.destination
			s.yieldRelocationClaims()
			if relocating.destination != destination || !relocating.released {
				t.Fatalf("pod 01 changed its destination from %s to %s", destination.ID, relocating.destination.ID)
			}
		})
	}
	// The control case: a released pod with a revocable claim yields it
	// and parks at another berth.
	s, relocating, other := claimKindFixture(t)
	relocating.Rebalancing, relocating.released = false, true
	other.destination = relocating.destination
	assign(s, other)
	s.yieldRelocationClaims()
	if relocating.destination.ID == "market-1" {
		t.Fatal("pod 01 did not park at another berth after the yield")
	}
}
