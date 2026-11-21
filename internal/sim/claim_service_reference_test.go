package sim

import (
	"math/rand/v2"
	"testing"
)

// referenceOwnsServiceClaim is ownsServiceClaim before ownedServiceClaim
// tested claimService first. It classifies each owned resource with
// claimKind.
func referenceOwnsServiceClaim(s *Simulation, v *vehicle) bool {
	if v.destination.ID == "" {
		return false
	}
	for _, r := range berthResources(v.destination) {
		if referenceOwnedServiceClaim(s, v, r) {
			return true
		}
	}
	return false
}

// referenceOwnedServiceClaim is the test of each resource in
// ownsServiceClaim and surrenderServiceClaims before ownedServiceClaim.
func referenceOwnedServiceClaim(s *Simulation, v *vehicle, r resource) bool {
	return s.owners[r].isPod(v.Pod.ID) && s.claimKind(v, r) == claimService
}

// serviceClaimCounts counts the coverage of
// TestOwnedServiceClaimMatchesReference.
type serviceClaimCounts struct {
	// owned counts the destination resources that a pod owns. service
	// counts those that claimKind classifies as claimService. shadowed
	// counts those for which claimService holds but an earlier kind of
	// claimKind wins. other counts those for which claimService does not
	// hold.
	ticks, owned, service, shadowed, other, outstanding, surrendered int
}

// serviceClaimTick compares ownedServiceClaim, ownsServiceClaim, and
// surrenderServiceClaims of each pod of s with the references at one tick.
// surrenderServiceClaims runs on a clone of s, so the run does not change.
func serviceClaimTick(t *testing.T, s *Simulation, counts *serviceClaimCounts) {
	t.Helper()
	counts.ticks++
	if s.incidentOutstanding() {
		counts.outstanding++
	}
	// want holds the resources that each pod gives up, by pod index.
	want := make(map[int][]resource)
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if got, ref := s.ownsServiceClaim(v), referenceOwnsServiceClaim(s, v); got != ref {
			t.Fatalf("tick %d pod %s: ownsServiceClaim %v, reference %v", s.tick, v.Pod.ID, got, ref)
		}
		if v.destination.ID == "" {
			continue
		}
		for _, r := range berthResources(v.destination) {
			got, ref := s.ownedServiceClaim(v, r), referenceOwnedServiceClaim(s, v, r)
			if got != ref {
				t.Fatalf("tick %d pod %s resource %v: ownedServiceClaim %v, reference %v", s.tick, v.Pod.ID, r, got, ref)
			}
			if !s.owners[r].isPod(v.Pod.ID) {
				continue
			}
			counts.owned++
			switch {
			case ref:
				counts.service++
				want[index] = append(want[index], r)
			case v.claimService(r):
				counts.shadowed++
			default:
				counts.other++
			}
		}
	}
	if len(want) == 0 {
		return
	}
	// Each pod gives up only resources that it owns, and the kinds of the
	// claims of the other pods do not read them. So the surrenders of all
	// pods can run on one clone.
	clone := s.Clone()
	for index := range clone.vehicles {
		if v := &clone.vehicles[index]; v.destination.ID != "" {
			clone.surrenderServiceClaims(v)
		}
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.destination.ID == "" {
			continue
		}
		for _, r := range berthResources(v.destination) {
			released := s.owners[r].isPod(v.Pod.ID) && !clone.owners[r].isPod(v.Pod.ID)
			wanted := false
			for _, w := range want[index] {
				wanted = wanted || w == r
			}
			if released != wanted {
				t.Fatalf("tick %d pod %s resource %v: surrenderServiceClaims released %v, reference %v", s.tick, v.Pod.ID, r, released, wanted)
			}
			if released {
				counts.surrendered++
			}
		}
	}
}

// TestOwnedServiceClaimMatchesReference runs the shared-track preset with
// trips, Express parties, faults, and emergencies. At each tick, it
// compares the service claims of each pod, and the claims that
// surrenderServiceClaims gives up, with the classification by claimKind
// alone.
func TestOwnedServiceClaimMatchesReference(t *testing.T) {
	t.Parallel()
	if raceEnabled {
		t.Skip("the preset run runs without -race")
	}
	p := readSharedExpressProject(t)
	s, err := NewFleetWithOrderContract(p.Network, p.Fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	p.configure(t, s)
	// The samplers start faults, debris, and emergencies, so incidents are
	// outstanding for part of the run.
	rates := IncidentRates{Faults: FaultRate{PerHour: 60, DebrisShare: 0.5, DebrisMeters: 2, Duration: FaultDurationRule{Kind: FaultDurationFixed, Seconds: 120}}, EmergenciesPerHour: 60, FaultSeed: [2]uint64{1, 2}, EmergencySeed: [2]uint64{3, 4}}
	if err := s.StartIncidentSampling(rates); err != nil {
		t.Fatal(err)
	}
	ticks := 18000
	if testing.Short() {
		ticks = 9000
	}
	random := rand.New(rand.NewPCG(5, 6))
	var counts serviceClaimCounts
	services := map[string]string{}
	for _, service := range p.ExpressServices {
		services[service.From] = service.ID
	}
	stations := s.network.Stations
	for range ticks {
		// The demand: a trip between two stations each second, and an
		// Express party each 20 s. A refused trip is part of the run.
		if s.tick%TicksPerSecond == 0 {
			from, to := stations[random.IntN(len(stations))].ID, stations[random.IntN(len(stations))].ID
			_, _ = s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: 1 + random.IntN(3), SharingConsent: SharedConsent})
		}
		if s.tick%(20*TicksPerSecond) == 0 {
			from, to := sharedExpressWaterloo, sharedExpressBank
			if s.tick%(40*TicksPerSecond) != 0 {
				from, to = to, from
			}
			_, _ = s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: 13, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: services[from]})
		}
		s.Step()
		serviceClaimTick(t, s, &counts)
	}
	t.Logf("%+v", counts)
	if counts.service == 0 || counts.shadowed == 0 || counts.other == 0 || counts.surrendered == 0 || counts.outstanding == 0 {
		t.Fatalf("the run lacks coverage: %+v", counts)
	}
}
