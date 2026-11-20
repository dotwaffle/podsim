package sim

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"os"
	"slices"
	"testing"
)

// londonExpressRunTicks bounds the run of TestLondonExpressEnvelopeRun, 15
// simulated minutes. The run takes at least sharedExpressRunTicks, and 30 s
// after the emergency.
const londonExpressRunTicks = 15 * 60 * TicksPerSecond

// readLondonExpressProject reads the project D of the Express
// qualification: the network of testdata/express_network.json.gz and the
// other parts of testdata/express_project.json. TestExpressProjectFixture
// in package scenarios writes the second file.
func readLondonExpressProject(t *testing.T) sharedExpressProject {
	t.Helper()
	data, err := os.ReadFile("testdata/express_project.json")
	if err != nil {
		t.Fatal(err)
	}
	var p sharedExpressProject
	if err := jsonv2.Unmarshal(data, &p, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	p.Network = expressNetworkFixture(t)
	if len(p.Fleet) != 343 || len(p.ExpressServices) != 2 {
		t.Fatalf("the fixture is not project D: %d pods, %d services", len(p.Fleet), len(p.ExpressServices))
	}
	return p
}

// londonExpressCoverage holds the coverage counts of section 5.1 of the
// always-on proposal.
type londonExpressCoverage struct {
	// dropped counts the pod ticks of a traveling pod where the envelope
	// with the largest front bound of the route (the route envelope of the
	// design before S1) holds a plane that the oracle envelope does not
	// hold: a plane that A1 and the anchor fronts drop.
	dropped int
	// clusters counts the envelopes where rule 2 adds a lane, and berths
	// the envelopes of a pod at a berth where rule 2 adds a lane.
	clusters, berths int
}

// count adds the coverage of each pod of s at one tick. gate is the oracle
// with the largest front bound of the route.
func (c *londonExpressCoverage) count(s *Simulation, gate *sharedOracle) {
	clusters := s.clusterSafety
	for i := range s.vehicles {
		v := &s.vehicles[i]
		envelope := s.appendVehicleSafetyLocations(nil, v)
		s.clusterSafety = nil
		selected := s.appendVehicleSafetyLocations(nil, v)
		s.clusterSafety = clusters
		if len(envelope) > len(selected) {
			c.clusters++
			if v.Pod.Activity != Traveling && v.Pod.BerthID != "" {
				c.berths++
			}
		}
		if v.Pod.Activity != Traveling || !v.originReleased {
			continue
		}
		for _, location := range gate.envelope(v, v.distance) {
			if !slices.Contains(envelope, location) {
				c.dropped++
				break
			}
		}
	}
}

// TestLondonExpressEnvelopeRun is the envelope run of project D under the
// shape clearance rule (section 5.1 of the always-on proposal). With
// background Compact trips between Waterloo and Bank and Express hub
// parties in both directions, it checks at each tick the oracle, the D6
// oracle, and that the indexed envelope construction and the reference
// construction give the same locations in the same order for each pod
// (see sharedExpressTick). A fault stops an Express pod with riders at
// speed, and an emergency follows on another Express pod with riders. At
// minute 1, at the fault, and at the emergency, it saves and restores the
// simulation, and the restored copy runs for 10 s with the same checks.
// It logs the coverage counts of londonExpressCoverage. Each count must be
// more than 0, except the count of A1 drops when no lane of D has a front
// bound over the least anchor front.
func TestLondonExpressEnvelopeRun(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the London Express envelope run runs without -short or -race")
	}
	p := readLondonExpressProject(t)
	s, err := NewFleetWithOrderContract(p.Network, p.Fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	p.configure(t, s)
	if s.envelopes == nil || len(s.clusterSafety) == 0 {
		t.Fatal("project D has no compiled envelope reach or no rule 2 cluster")
	}
	reference := newEnvelopeReference(s)
	gate := &sharedOracle{s: s, fronts: sharedTrackFronts(s.network, s.tails, false)}
	services := map[string]string{}
	for _, service := range p.ExpressServices {
		services[service.From] = service.ID
	}
	party := func(from, to string, size int) {
		if _, submitErr := s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: size, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: services[from]}); submitErr != nil {
			t.Fatalf("tick %d: party of %d from %s: %v", s.tick, size, from, submitErr)
		}
	}
	var counts sharedExpressCounts
	var coverage londonExpressCoverage
	restore := func(name string) {
		cold := restoreExpressSave(t, p, s, name)
		counts.restores++
		runRestoredExpress(t, cold, name)
	}
	fault, emergency, faulted, saved := "", "", "", false
	// after is the tick until which the run continues after the emergency.
	after := int64(0)
	for s.tick < sharedExpressRunTicks || emergency == "" || s.tick < after {
		tick := s.tick
		if tick >= londonExpressRunTicks {
			t.Fatalf("the run did not end in %d s: fault %q, emergency %q", tick/TicksPerSecond, fault, emergency)
		}
		if tick%(20*TicksPerSecond) == 0 && tick < 3*60*TicksPerSecond {
			party(sharedExpressWaterloo, sharedExpressBank, 13+int(tick/(20*TicksPerSecond))%8)
		}
		if tick%(90*TicksPerSecond) == 45*TicksPerSecond && tick < 3*60*TicksPerSecond {
			party(sharedExpressBank, sharedExpressWaterloo, 12+int(tick/(90*TicksPerSecond))%9)
		}
		if tick%(10*TicksPerSecond) == 5*TicksPerSecond {
			from, to := sharedExpressWaterloo, sharedExpressBank
			if tick%(20*TicksPerSecond) != 5*TicksPerSecond {
				from, to = to, from
			}
			if _, submitErr := s.SubmitTrip(from, to); submitErr != nil {
				t.Fatalf("tick %d: trip from %s: %v", tick, from, submitErr)
			}
		}
		s.Step()
		sharedExpressTick(t, s, reference, "live", &counts)
		coverage.count(s, gate)
		if s.tick%TicksPerSecond != 0 {
			continue
		}
		if !saved && s.tick >= 60*TicksPerSecond {
			saved = true
			restore("minute 1")
		}
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if v.Pod.Class != ExpressClass || v.Pod.Activity != Traveling {
				continue
			}
			switch {
			case fault == "" && v.PassengersAboard() > 0 && v.Pod.Speed >= 10:
				fault, err = s.Fault(FaultRequest{PodID: v.Pod.ID, DurationSeconds: new(int64(60))})
				if err != nil {
					t.Fatal(err)
				}
				faulted = v.Pod.ID
				t.Logf("tick %d: fault %s on %s on %s at %.2f m/s with %d riders", s.tick, fault, v.Pod.ID, v.Pod.LaneID, v.Pod.Speed, v.PassengersAboard())
				restore("fault")
			case fault != "" && emergency == "" && v.Pod.ID != faulted && v.PassengersAboard() > 0 && v.Pod.Speed > 0:
				emergency, err = s.Emergency(v.Pod.ID, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("tick %d: emergency %s on %s on %s at %.2f m/s with %d riders", s.tick, emergency, v.Pod.ID, v.Pod.LaneID, v.Pod.Speed, v.PassengersAboard())
				after = s.tick + 30*TicksPerSecond
				restore("emergency")
			default:
				continue
			}
			break
		}
	}
	t.Logf("%d ticks, %d envelope comparisons, %d ticks with two pods on merge or shape pair lanes, %d of them with an Express pod among them, %d ticks with a fault, %d ticks with an emergency, %d restores",
		counts.ticks, counts.comparisons, counts.lateral, counts.lateralExpress, counts.fault, counts.emergency, counts.restores)
	t.Logf("coverage: %d pod ticks where A1 drops a plane of the largest front bound, %d envelopes where rule 2 adds a cluster lane, %d berth envelopes that hold a rule 2 lane",
		coverage.dropped, coverage.clusters, coverage.berths)
	if counts.fault == 0 || counts.emergency == 0 || counts.restores != 3 {
		t.Fatalf("coverage: fault %q, emergency %q, counts %+v", fault, emergency, counts)
	}
	// A count of 0 is a failure for a case that the geometry of D has. The
	// anchor front of a lane is at least largeEnvelopeRadius, so A1 can
	// drop a plane of the largest front bound only on a route with a
	// larger front bound.
	largest := slices.Max(gate.fronts)
	t.Logf("the largest front bound of a lane is %g m, and the anchor front is at least %g m", largest, largeEnvelopeRadius)
	if coverage.dropped == 0 && largest > largeEnvelopeRadius+1e-6 || coverage.clusters == 0 || coverage.berths == 0 {
		t.Fatalf("a coverage count is 0: %+v", coverage)
	}
}
