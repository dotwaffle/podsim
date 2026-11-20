package sim

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"maps"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
)

// The tests of this file give the evidence of rows SX2, SX3, and SX4 of the
// Express shared-track qualification on the preset
// london-full-shared-express. TestSharedExpressFixture in package
// scenarios writes the preset to testdata.

const (
	sharedExpressWaterloo, sharedExpressBank = "940GZZLUWLO", "940GZZLUBNK"
	// sharedExpressRunTicks is the length of the run of
	// TestSharedExpressEnvelopeRun, 4 simulated minutes.
	sharedExpressRunTicks = 4 * 60 * TicksPerSecond
)

// sharedExpressLinkLanes are the lanes of link 100 on the shared route.
var sharedExpressLinkLanes = map[string]bool{
	"london-link-100-ab-1": true, "london-link-100-ab-2": true, "london-link-100-ba-1": true, "london-link-100-ba-2": true,
}

// sharedExpressProject holds the parts of the preset that the tests use.
type sharedExpressProject struct {
	Network         Network          `json:"network"`
	Fleet           []Placement      `json:"fleet"`
	ExpressServices []ExpressService `json:"expressServices"`
	PlatoonLimit    int              `json:"platoonLimit"`
	Faults          FaultSettings    `json:"faults"`
}

// readSharedExpressProject reads testdata/shared_express_project.json.gz.
func readSharedExpressProject(t *testing.T) sharedExpressProject {
	t.Helper()
	data, err := os.ReadFile("testdata/shared_express_project.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var p sharedExpressProject
	if err := jsonv2.UnmarshalRead(reader, &p, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if len(p.Fleet) != 343 || len(p.ExpressServices) != 2 {
		t.Fatalf("the fixture is not the preset: %d pods, %d services", len(p.Fleet), len(p.ExpressServices))
	}
	return p
}

// sharedExpressNetwork returns the network of the preset.
func sharedExpressNetwork(t *testing.T) Network {
	t.Helper()
	return readSharedExpressProject(t).Network
}

// configure sets the Express services, the platoons, the faults, and the
// emergencies of the preset on s, as a session does.
func (p sharedExpressProject) configure(t *testing.T, s *Simulation) {
	t.Helper()
	if err := s.SetExpressServices(p.ExpressServices); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatoonLimit(p.PlatoonLimit); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFaults(true, p.Faults); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmergencies(true); err != nil {
		t.Fatal(err)
	}
}

// sharedExpressCounts counts the coverage of a run.
type sharedExpressCounts struct {
	ticks, comparisons, lateral, lateralExpress, mixed, fault, emergency, restores, policiesOff, control int
}

// sharedExpressTick runs the checks of SX3 on s at one tick: the oracle,
// the D6 oracle, and the parity of the indexed and the reference envelope
// of each pod.
// r holds the tables of the reference construction of s.
func sharedExpressTick(t *testing.T, s *Simulation, r *envelopeReference, name string, counts *sharedExpressCounts) {
	t.Helper()
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatalf("%s: tick %d: %v", name, s.tick, err)
	}
	if err := s.lateralBodyCheck(); err != nil {
		t.Fatalf("%s: tick %d: D6 oracle: %v", name, s.tick, err)
	}
	checkSafetyLocationParityWith(t, s, r)
	counts.ticks++
	counts.comparisons += len(s.vehicles)
	lateral, lateralExpress, express, compact := 0, false, false, false
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity != Traveling {
			continue
		}
		if lane, ok := s.graph.lanes[v.Pod.LaneID]; ok && (len(s.merges.neighborLanes(lane)) > 0 || len(s.shapes.neighborLanes(lane)) > 0) {
			lateral++
			lateralExpress = lateralExpress || v.Pod.Class == ExpressClass
		}
		if sharedExpressLinkLanes[v.Pod.LaneID] {
			express = express || v.Pod.Class == ExpressClass
			compact = compact || v.Pod.Class != ExpressClass
		}
	}
	if lateral >= 2 {
		counts.lateral++
		if lateralExpress {
			counts.lateralExpress++
		}
	}
	if express && compact {
		counts.mixed++
	}
	if len(s.faults) > 0 {
		counts.fault++
	}
	if len(s.emergencies) > 0 {
		counts.emergency++
	}
}

// sharedExpressRestore saves s and restores the save (see
// restoreExpressSave). It turns the policies off in a copy of the restored
// simulation (see sharedExpressPoliciesOff). The restored copy then runs
// for 10 s with the checks of sharedExpressTick at each tick.
func sharedExpressRestore(t *testing.T, p sharedExpressProject, s *Simulation, name string, counts *sharedExpressCounts) {
	t.Helper()
	cold := restoreExpressSave(t, p, s, name)
	counts.restores++
	resets, kept, dropped, rebuilt := sharedExpressRestoreChanges(t, s, cold)
	t.Logf("%s: tick %d restored at the physical tier: %d speed resets; forward grants: %d kept, %d not rebuilt, %d new from the route geometry", name, s.tick, resets, kept, dropped, rebuilt)
	if links, control := sharedExpressPoliciesOff(t, cold.Clone(), name); links > 0 {
		counts.policiesOff++
		counts.control += control
	}
	runRestoredExpress(t, cold, name)
}

// restoreExpressSave saves s, restores the save, and requires the physical
// tier with no change to the pods, the counters, or the queue, except a
// distance within restoreTolerance (see checkRestoredMatches). It returns
// the restored simulation.
func restoreExpressSave(t *testing.T, p sharedExpressProject, s *Simulation, name string) *Simulation {
	t.Helper()
	// With no incident, the state takes a strict JSON round trip first. The
	// session adapter saves the fault and emergency records, which the plain
	// JSON omits, and TestLondonSharedExpressPhysicalCheckpoints restores
	// them through the encoded session path.
	state := s.ExportState()
	if len(s.faults)+len(s.emergencies) == 0 {
		state = roundTripState(t, state)
	}
	cold, r, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, FaultsOn: true, Faults: p.Faults, EmergenciesOn: true,
		ExpressServices: p.ExpressServices, Network: p.Network, Fleet: p.Fleet, State: state})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !cleanRestore(r) || len(r.Interrupted)+len(r.LogicalCompleted)+r.DroppedFaults+r.DroppedEmergencies+r.Unaccounted != 0 {
		t.Fatalf("%s: restore %+v", name, r)
	}
	if err := cold.SetPlatoonLimit(p.PlatoonLimit); err != nil {
		t.Fatal(err)
	}
	if err := cold.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	checkRestoredMatches(t, s, cold)
	return cold
}

// runRestoredExpress runs a restored simulation for 10 s with the checks
// of sharedExpressTick at each tick.
func runRestoredExpress(t *testing.T, cold *Simulation, name string) {
	t.Helper()
	reference := newEnvelopeReference(cold)
	for range 10 * TicksPerSecond {
		cold.Step()
		sharedExpressTick(t, cold, reference, name+", restored", &sharedExpressCounts{})
	}
}

// sharedExpressRestoreChanges classifies the changes of a physical
// restore apart. A restore resets the speed of each pod to zero, and it
// builds the forward grants of each pod again from its route geometry.
// Each pod of restored must have a speed of zero. Each resource that a pod
// holds at its station, or behind its position on its route (the blocks up
// to its block, released after its distance), must keep its owner, as
// checkLargeColdRetainedOwners requires for track resources. The forward
// grants are the resources of the later blocks. It returns the number of
// pods with a speed above zero in live, the forward grants of live that
// restored keeps and that it does not keep, and the pod resources of
// restored that live does not have.
func sharedExpressRestoreChanges(t *testing.T, live, restored *Simulation) (resets, kept, dropped, rebuilt int) {
	t.Helper()
	for i := range restored.vehicles {
		if speed := restored.vehicles[i].Pod.Speed; speed != 0 {
			t.Fatalf("tick %d: pod %s restored at %g m/s", live.tick, restored.vehicles[i].Pod.ID, speed)
		}
		if live.vehicles[i].Pod.Speed > 0 {
			resets++
		}
	}
	for i := range live.vehicles {
		v := &live.vehicles[i]
		owner := podResourceOwner(v.Pod.ID)
		retained := make(map[resource]bool)
		if v.Pod.Activity == Traveling {
			for _, b := range v.blocks.span(0, v.blockIndex+1) {
				for _, held := range b.resources {
					if resourceReleaseDistance(b, held) > v.distance {
						retained[held] = true
					}
				}
			}
		}
		for held, o := range live.owners {
			if o != owner {
				continue
			}
			switch {
			case v.Pod.Activity != Traveling || retained[held]:
				if restored.owners[held] != owner {
					t.Fatalf("tick %d: the restore lost the retained resource %+v of %s", live.tick, held, v.Pod.ID)
				}
			case restored.owners[held] == owner:
				kept++
			default:
				dropped++
			}
		}
	}
	for held, owner := range restored.owners {
		if owner.podID() != "" && live.owners[held] != owner {
			rebuilt++
		}
	}
	return resets, kept, dropped, rebuilt
}

// sharedExpressPoliciesOff turns platooning and occupied pickups off in a
// restored copy s, and runs it. The preset has occupied pickups off, so
// TestSharedExpressOccupiedPickupPolicy checks that policy. The function runs s with the oracle and the D6 oracle at each
// tick. For the first 5 minutes, a Compact trip between the hubs starts
// each 10 s, so that pods can meet. The platoon members of the restore
// finish: each link ends, and each accepted order completes or is
// interrupted. No new member forms: a link that the restore did not have
// is a failure. A control copy with the policies on runs the same 5
// minutes and counts its new links. The function returns the number of
// links at the restore and the new links of the control, and does not run
// a copy with no link.
func sharedExpressPoliciesOff(t *testing.T, s *Simulation, name string) (links, control int) {
	t.Helper()
	initial := map[[2]int]bool{}
	for i := range s.vehicles {
		if leader := s.vehicles[i].link.leader; leader > 0 {
			initial[[2]int{i, leader - 1}] = true
		}
	}
	if len(initial) == 0 {
		return 0, 0
	}
	const demand = 5 * 60 * TicksPerSecond
	start := s.tick
	// step runs one tick of c with the trips, and returns the links of c
	// that are not in exempt. A link of exempt that ends leaves exempt, so
	// a link that forms again later is new.
	step := func(c *Simulation, label string, exempt map[[2]int]bool) int {
		if tick := c.tick - start; tick < demand && tick%(10*TicksPerSecond) == 0 {
			from, to := sharedExpressWaterloo, sharedExpressBank
			if tick%(20*TicksPerSecond) != 0 {
				from, to = to, from
			}
			if _, err := c.SubmitTrip(from, to); err != nil {
				t.Fatalf("%s, %s: trip: %v", name, label, err)
			}
		}
		c.Step()
		if _, err := c.SafetyObservation().Check(); err != nil {
			t.Fatalf("%s, %s: tick %d: %v", name, label, c.tick, err)
		}
		if err := c.lateralBodyCheck(); err != nil {
			t.Fatalf("%s, %s: tick %d: D6 oracle: %v", name, label, c.tick, err)
		}
		for pair := range exempt {
			if c.vehicles[pair[0]].link.leader != pair[1]+1 {
				delete(exempt, pair)
			}
		}
		fresh := 0
		for i := range c.vehicles {
			if leader := c.vehicles[i].link.leader; leader > 0 && !exempt[[2]int{i, leader - 1}] {
				fresh++
			}
		}
		return fresh
	}
	on := s.Clone()
	seen := 0
	for exempt := maps.Clone(initial); on.tick < start+demand; {
		seen = max(seen, step(on, "policies on", exempt))
	}
	if err := s.SetPlatooning(PlatooningOff); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOnboardPickups(false); err != nil {
		t.Fatal(err)
	}
	terminal := func() bool {
		snapshot := s.Snapshot()
		return snapshot.Completed+snapshot.Interrupted == snapshot.Submitted && len(snapshot.Pending) == 0
	}
	exempt := maps.Clone(initial)
	for s.tick < start+demand || s.platoonLinks != 0 || s.tick%TicksPerSecond != 0 || !terminal() {
		if s.tick > start+30*60*TicksPerSecond {
			t.Fatalf("%s, policies off: %d links and %+v orders after 30 minutes", name, s.platoonLinks, s.Snapshot().Submitted)
		}
		if fresh := step(s, "policies off", exempt); fresh > 0 {
			t.Fatalf("%s, policies off: tick %d: %d new platoon links", name, s.tick, fresh)
		}
	}
	snapshot := s.Snapshot()
	t.Logf("%s, policies off: %d links end, and %d orders complete and %d are interrupted, in %d s; with the policies on, the same 5 minutes have up to %d new links", name, len(initial), snapshot.Completed, snapshot.Interrupted, (s.tick-start)/TicksPerSecond, seen)
	return len(initial), seen
}

// TestSharedExpressEnvelopeRun is the run of SX3 and the sim part of SX4 on
// the preset. With background Compact trips between Waterloo and Bank and
// Express hub parties in both directions, it checks at each tick the
// oracle, the D6 oracle, and that the indexed envelope construction and the
// reference construction give the same locations in the same order for
// each pod. A fault stops an Express pod with riders on link 100, and an
// emergency follows on another Express pod with riders on link 100. At the
// first tick with Express and Compact pods on link 100, at the fault, and at
// the emergency, it saves and restores the simulation, and the restored copy
// runs for 10 s with the same checks.
func TestSharedExpressEnvelopeRun(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express run runs without -short or -race")
	}
	p := readSharedExpressProject(t)
	s, err := NewFleetWithOrderContract(p.Network, p.Fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	p.configure(t, s)
	if s.envelopes == nil || s.shapes == nil {
		t.Fatal("the preset has no compiled envelope reach or no shape table")
	}
	reference := newEnvelopeReference(s)
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
	fault, emergency, mixed, platooned := "", "", false, false
	linked := 0
	faulted := ""
	for s.tick < sharedExpressRunTicks {
		tick := s.tick
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
		if s.platoonLinks > 0 {
			linked++
		}
		if s.tick%TicksPerSecond != 0 {
			continue
		}
		if !platooned && s.platoonLinks > 0 {
			platooned = true
			sharedExpressRestore(t, p, s, "platoon links", &counts)
		}
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if v.Pod.Class != ExpressClass || !sharedExpressLinkLanes[v.Pod.LaneID] {
				continue
			}
			switch {
			case !mixed && counts.mixed > 0:
				mixed = true
				sharedExpressRestore(t, p, s, "mixed traffic on link 100", &counts)
			case fault == "" && v.PassengersAboard() > 12 && v.Pod.Speed >= 10:
				fault, err = s.Fault(FaultRequest{PodID: v.Pod.ID, DurationSeconds: new(int64(60))})
				if err != nil {
					t.Fatal(err)
				}
				faulted = v.Pod.ID
				t.Logf("tick %d: fault %s on %s on %s at %.2f m/s with %d riders", s.tick, fault, v.Pod.ID, v.Pod.LaneID, v.Pod.Speed, v.PassengersAboard())
				sharedExpressRestore(t, p, s, "fault on link 100", &counts)
			case fault != "" && emergency == "" && v.Pod.ID != faulted && v.PassengersAboard() > 0 && v.Pod.Speed > 0:
				emergency, err = s.Emergency(v.Pod.ID, 0)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("tick %d: emergency %s on %s on %s at %.2f m/s with %d riders", s.tick, emergency, v.Pod.ID, v.Pod.LaneID, v.Pod.Speed, v.PassengersAboard())
				sharedExpressRestore(t, p, s, "emergency on link 100", &counts)
			default:
				continue
			}
			break
		}
	}
	t.Logf("%d ticks with a platoon link", linked)
	t.Logf("%d ticks, %d envelope comparisons, %d ticks with two pods on merge or shape pair lanes, %d of them with an Express pod among them, %d ticks with Express and Compact pods on link 100, %d ticks with a fault, %d ticks with an emergency, %d restores",
		counts.ticks, counts.comparisons, counts.lateral, counts.lateralExpress, counts.mixed, counts.fault, counts.emergency, counts.restores)
	if fault == "" || emergency == "" || !mixed || counts.lateralExpress == 0 || counts.fault == 0 || counts.emergency == 0 || counts.restores != 4 || counts.policiesOff == 0 || counts.control == 0 {
		t.Fatalf("coverage: fault %q, emergency %q, mixed %v, counts %+v", fault, emergency, mixed, counts)
	}
}

// sharedExpressSweepPods returns a simulation of the preset with two
// Express pods and two Compact pods, and the indexes of its pods by class.
func sharedExpressSweepPods(t *testing.T, p sharedExpressProject) (*Simulation, map[VehicleClass][]int) {
	t.Helper()
	var fleet []Placement
	express, compact := 0, 0
	for _, placement := range p.Fleet {
		switch {
		case placement.Class == ExpressClass && express < 2:
			express++
		case placement.Class == CompactClass && compact < 2:
			compact++
		default:
			continue
		}
		fleet = append(fleet, placement)
	}
	s, err := NewFleetWithOrderContract(p.Network, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	pods := map[VehicleClass][]int{}
	for i := range s.vehicles {
		pods[s.vehicles[i].Pod.Class] = append(pods[s.vehicles[i].Pod.Class], i)
	}
	return s, pods
}

// sharedExpressCertifiedParts returns the parts of shape pair a, b that the
// certification sweeps, in the order of the two lanes. It calls
// (bankAudit).planes, as shapePairs does, with the lane paths of the
// audit: the parts at which the envelope reach of the two lanes is not
// separated, or the whole lanes when their planes are not separated.
func sharedExpressCertifiedParts(t *testing.T, s *Simulation, audit bankAudit, a, b int) []pairRange {
	t.Helper()
	path := func(lane int) bankAuditPath {
		l := s.network.Lanes[lane]
		location := SafetyLocation{SeparationGroup: l.SeparationGroup, From: l.From, To: l.To}
		return bankAuditPath{id: l.ID, location: location, locations: []SafetyLocation{location}, large: largeClassSet(l.VehicleClasses), key: laneKey(lane)}
	}
	planes := audit.planes(path(a), path(b))
	if planes.exempt {
		t.Fatalf("shape pair %s and %s is exempt", s.network.Lanes[a].ID, s.network.Lanes[b].ID)
	}
	if planes.ranges == nil {
		return []pairRange{{first: [2]float64{0, s.graph.lengths[a]}, second: [2]float64{0, s.graph.lengths[b]}}}
	}
	return planes.ranges
}

// sharedExpressSweep holds the counts of TestSharedExpressShapePairSweep.
type sharedExpressSweep struct {
	pairs, placements int
	// joins counts the placements with a pod at a join pose: a pose that
	// turns between two headings at a polyline vertex or a lane end.
	// bounds counts the placements with a pod at, or 1 mm before or after,
	// a bound of its junction extent.
	joins, bounds int
	classes       map[[2]VehicleClass]int
	// nearest holds the smallest distance between the reference points of
	// two tested pods, by class pairing, and margin the least distance less
	// the certified clearance of the pair.
	nearest, margin map[[2]VehicleClass]float64
}

// TestSharedExpressShapePairSweep runs the shape pair, merge pair, and
// split pair cases of SC6 on the preset (row SX2). Each pair certifies the
// 6 m by 2.5 m class body. For each pairing of the classes that the two
// lanes admit, in both class orders, it places two pods at each pair of
// positions whose reference points are less than 12 m apart, and requires the literal bodies of the oracle
// (oracleDisjoint) to be disjoint at each pose, with no plane exemption:
//   - on a shape pair, at the parts that the certification sweeps, where
//     the envelope reach of the two lanes is not separated, and the
//     reference points keep the clearance of the shape table;
//   - on a merge or split pair with a lane that admits Express, at each
//     pair of positions with a pod outside its junction extent.
//
// The positions of a lane are each 0.5 m of a part, the ends of the part,
// each polyline vertex in the part, where the oracle turns the body
// between the headings of a join, and, on a merge or split pair, each
// bound of the junction extent and the positions 1 mm before and after it.
func TestSharedExpressShapePairSweep(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express sweep runs without -short or -race")
	}
	p := readSharedExpressProject(t)
	s, pods := sharedExpressSweepPods(t, p)
	if !s.shapes.any() || !s.merges.any() {
		t.Fatal("the preset has no shape pair or no merge pair")
	}
	audit := bankAudit{tails: s.tails, reach: &lazyPlaneReach{network: s.network}, envelope: newLazyEnvelopeReach(s.network, s.tails)}
	if audit.envelope.get() == nil {
		t.Fatal("the preset has no envelope reach")
	}
	const near, step, beside = 12.0, 0.5, 1e-3
	// positions returns the positions of a lane in a part, with the
	// bounds of its junction extent.
	positions := func(lane int, part [2]float64, bounds ...float64) []float64 {
		var at []float64
		for along := part[0]; along < part[1]; along += step {
			at = append(at, along)
		}
		at = append(at, part[1])
		points := s.network.Polyline(s.network.Lanes[lane])
		length := 0.0
		for k := 1; k < len(points); k++ {
			length += pointDistance(points[k-1], points[k])
			at = append(at, length)
		}
		for _, bound := range bounds {
			at = append(at, bound-beside, bound, bound+beside)
		}
		at = slices.DeleteFunc(at, func(along float64) bool { return along < part[0] || along > part[1] })
		slices.Sort(at)
		return slices.Compact(at)
	}
	// place puts pod i at along on lane a and pod j at other on lane b,
	// and returns the distance of their reference points.
	place := func(i, j, a, b int, along, other float64) float64 {
		oraclePlace(s, i, s.network.Lanes[a].ID, along)
		oraclePlace(s, j, s.network.Lanes[b].ID, other)
		return pointDistance(s.vehicles[i].Pod.Position, s.vehicles[j].Pod.Position)
	}
	// sweep runs the class pairings of lanes a and b over the parts. skip
	// reports a pair of positions that the pair does not certify, and
	// clearance is the certified distance of the reference points, or 0.
	sweep := func(name string, a, b int, parts []pairRange, bounds [2][]float64, skip func(along, other float64) bool, clearance float64, counts *sharedExpressSweep) {
		first, second := s.network.Lanes[a], s.network.Lanes[b]
		counts.pairs++
		for _, classes := range shapeProbeClasses {
			if !first.VehicleClasses.Allows(string(classes[0])) || !second.VehicleClasses.Allows(string(classes[1])) {
				continue
			}
			counts.classes[classes]++
			i, j := pods[classes[0]][0], pods[classes[1]][len(pods[classes[1]])-1]
			atBound := func(lane int, along float64) bool {
				return slices.ContainsFunc(bounds[lane], func(bound float64) bool { return math.Abs(along-bound) <= beside })
			}
			for _, part := range parts {
				pointsB := s.network.Polyline(second)
				others := positions(b, part.second, bounds[1]...)
				for _, along := range positions(a, part.first, bounds[0]...) {
					oraclePlace(s, i, first.ID, along)
					for _, other := range others {
						if pointDistance(s.vehicles[i].Pod.Position, pointAlong(pointsB, other)) >= near || skip != nil && skip(along, other) {
							continue
						}
						gap := place(i, j, a, b, along, other)
						counts.placements++
						podA, podB := s.newLateralPod(&s.vehicles[i], a), s.newLateralPod(&s.vehicles[j], b)
						if len(podA.poses) > 1 || len(podB.poses) > 1 {
							counts.joins++
						}
						if atBound(0, along) || atBound(1, other) {
							counts.bounds++
						}
						if !oracleDisjoint(podA, podB) {
							t.Fatalf("%s %s and %s, %s at %g m and %s at %g m: the class bodies are not disjoint, %g m apart", name, first.ID, second.ID, classes[0], along, classes[1], other, gap)
						}
						if clearance > 0 && gap < clearance {
							t.Fatalf("%s %s and %s, %s at %g m and %s at %g m: %g m apart, under the certified clearance %g m", name, first.ID, second.ID, classes[0], along, classes[1], other, gap, clearance)
						}
						if nearest, ok := counts.nearest[classes]; !ok || gap < nearest {
							counts.nearest[classes] = gap
						}
						if margin, ok := counts.margin[classes]; clearance > 0 && (!ok || gap-clearance < margin) {
							counts.margin[classes] = gap - clearance
						}
					}
				}
			}
		}
	}
	newCounts := func() *sharedExpressSweep {
		return &sharedExpressSweep{classes: map[[2]VehicleClass]int{}, nearest: map[[2]VehicleClass]float64{}, margin: map[[2]VehicleClass]float64{}}
	}
	shapes, merges := newCounts(), newCounts()
	for a, neighbors := range s.shapes.neighbors {
		for _, b := range neighbors {
			if b <= a {
				continue
			}
			clearance, _ := s.shapes.clearance(laneKey(a), laneKey(b))
			sweep("shape pair", a, b, sharedExpressCertifiedParts(t, s, audit, a, b), [2][]float64{}, nil, clearance, shapes)
		}
	}
	for a, neighbors := range s.merges.neighbors {
		for _, b := range neighbors {
			if b <= a || !largeClassSet(s.network.Lanes[a].VehicleClasses) && !largeClassSet(s.network.Lanes[b].VehicleClasses) {
				continue
			}
			startA, endA, _ := s.merges.mergeExtent(a, b)
			startB, endB, _ := s.merges.mergeExtent(b, a)
			whole := []pairRange{{first: [2]float64{0, s.graph.lengths[a]}, second: [2]float64{0, s.graph.lengths[b]}}}
			// Both pods in their extents is the case of the junction.
			inside := func(along, other float64) bool {
				return along >= startA && along <= endA && other >= startB && other <= endB
			}
			sweep("merge or split pair", a, b, whole, [2][]float64{{startA, endA}, {startB, endB}}, inside, 0, merges)
		}
	}
	for _, c := range []struct {
		name   string
		counts *sharedExpressSweep
	}{{"shape pairs", shapes}, {"merge and split pairs with an Express lane", merges}} {
		t.Logf("%s: %d pairs, class pairs %v, %d placements, %d with a join pose, %d at an extent bound, nearest reference points %v, least margin over the certified clearance %v",
			c.name, c.counts.pairs, c.counts.classes, c.counts.placements, c.counts.joins, c.counts.bounds, c.counts.nearest, c.counts.margin)
	}
	express := [2]VehicleClass{ExpressClass, CompactClass}
	if shapes.pairs != 47 || shapes.classes[express] == 0 || shapes.placements == 0 || shapes.joins == 0 || merges.pairs == 0 || merges.placements == 0 || merges.joins == 0 || merges.bounds == 0 {
		t.Fatal("the sweep covers no shape pair or no merge pair of Express and Compact pods")
	}
}

// sharedExpressDwellFleet returns a simulation of the preset with one
// Express pod at a Waterloo berth, and its network and fleet.
func sharedExpressDwellFleet(t *testing.T, p sharedExpressProject) (*Simulation, []Placement) {
	t.Helper()
	fleet := []Placement{{ID: "express-dwell", Class: ExpressClass, StationID: sharedExpressWaterloo, BerthID: sharedExpressWaterloo + "-01"}}
	s, err := NewFleetWithOrderContract(p.Network, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	return s, fleet
}

// sharedExpressBoarding submits a party from Waterloo to Bank, and it
// fails the test when the pod does not board it.
func sharedExpressBoarding(t *testing.T, s *Simulation, consent SharingConsent, party int) *vehicle {
	t.Helper()
	if _, err := s.SubmitTripOptions(TripOptions{From: sharedExpressWaterloo, To: sharedExpressBank, PartySize: party, SharingConsent: consent}); err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	if v.Pod.Activity != Boarding {
		t.Fatalf("the pod does not board: %+v", v.Pod)
	}
	return v
}

// TestSharedExpressDwell runs the cases of the Dwell row of section 7.2
// of the standee contract on the preset (row SX9), as the tests of
// express_dwell_test.go do on their fixture: the boarding and unloading
// phases of 1, 13, and 20 passengers between Waterloo and Bank, a join
// during boarding that adds to the phase, a join at the bound, a join that
// ends a wait for track, an emergency unload, and the restore bound of the
// boarding and the unloading phase at the largest value and one tick
// above it. Only Waterloo and Bank are passenger stations that admit
// Express, so the preset has no occupied pickup and no multi-stop unload of
// an Express pod. The test pins that fact.
func TestSharedExpressDwell(t *testing.T) {
	// The cases share one base simulation, so they run one after another.
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express dwell runs without -short or -race")
	}
	p := readSharedExpressProject(t)
	base, fleet := sharedExpressDwellFleet(t, p)
	var express []string
	for _, station := range p.Network.Stations {
		if !station.ParkingOnly && slices.ContainsFunc(station.Berths, func(b Berth) bool { return b.VehicleClasses.Allows(string(ExpressClass)) }) {
			express = append(express, station.ID)
		}
	}
	slices.Sort(express)
	if !slices.Equal(express, []string{sharedExpressBank, sharedExpressWaterloo}) {
		t.Fatalf("passenger stations with Express berths %v, want Bank and Waterloo", express)
	}
	t.Run("journey", func(t *testing.T) {
		for _, test := range []struct{ party, boarding, unloading int }{{1, 3, 2}, {13, 15, 14}, {20, 22, 21}} {
			s := base.Clone()
			v := sharedExpressBoarding(t, s, PrivateConsent, test.party)
			start := s.tick
			for v.Pod.Activity == Boarding && s.tick < 60*TicksPerSecond {
				s.Step()
			}
			if got, want := s.tick-start, int64(test.boarding*TicksPerSecond); got != want {
				t.Errorf("party %d boards for %d ticks, want %d", test.party, got, want)
			}
			for v.Pod.Activity != Unloading && s.tick < 900*TicksPerSecond {
				s.Step()
			}
			if v.Pod.StationID != sharedExpressBank {
				t.Fatalf("party %d: the pod unloads at %q", test.party, v.Pod.StationID)
			}
			t.Logf("party %d: the pod unloads at Bank at tick %d", test.party, s.tick)
			if got, want := v.phaseTicks, test.unloading*TicksPerSecond; got != want {
				t.Errorf("party %d unloads for %d ticks, want %d", test.party, got, want)
			}
			if err := s.CheckContract(); err != nil {
				t.Fatal(err)
			}
		}
	})
	joined := func(t *testing.T) (*Simulation, *vehicle) {
		t.Helper()
		s := base.Clone()
		if err := s.SetSharedRidePartyLimit(3); err != nil {
			t.Fatal(err)
		}
		return s, sharedExpressBoarding(t, s, SharedConsent, 4)
	}
	t.Run("join during boarding", func(t *testing.T) {
		s, v := joined(t)
		if want := 6 * TicksPerSecond; v.phaseTicks != want {
			t.Fatalf("first party: phase %d, want %d", v.phaseTicks, want)
		}
		advance(s, TicksPerSecond)
		left := v.phaseTicks
		if _, err := s.SubmitTripOptions(TripOptions{From: sharedExpressWaterloo, To: sharedExpressBank, PartySize: 3, SharingConsent: SharedConsent}); err != nil {
			t.Fatal(err)
		}
		if want := left + 3*TicksPerSecond; len(v.Riders) != 2 || v.phaseTicks != want {
			t.Fatalf("after the join: %d riders, phase %d, want %d", len(v.Riders), v.phaseTicks, want)
		}
	})
	t.Run("join at the bound", func(t *testing.T) {
		s, v := joined(t)
		v.phaseTicks = 21 * TicksPerSecond
		if _, err := s.SubmitTripOptions(TripOptions{From: sharedExpressWaterloo, To: sharedExpressBank, PartySize: 3, SharingConsent: SharedConsent}); err != nil {
			t.Fatal(err)
		}
		if want := 22 * TicksPerSecond; v.phaseTicks != want {
			t.Fatalf("phase %d, want the bound %d", v.phaseTicks, want)
		}
	})
	t.Run("join ends a wait for track", func(t *testing.T) {
		s, v := joined(t)
		v.phaseTicks, v.pending, v.waitSince = 0, 0, s.tick
		v.Pod.WaitReason, v.Pod.BlockedBy = TrackOccupied, "other"
		if _, err := s.SubmitTripOptions(TripOptions{From: sharedExpressWaterloo, To: sharedExpressBank, PartySize: 2, SharingConsent: SharedConsent}); err != nil {
			t.Fatal(err)
		}
		if want := 2 * TicksPerSecond; v.phaseTicks != want || v.pending != -1 || v.Pod.WaitReason != NoWait || v.Pod.BlockedBy != "" {
			t.Fatalf("phase %d, want %d, pending %d, wait %v by %q", v.phaseTicks, want, v.pending, v.Pod.WaitReason, v.Pod.BlockedBy)
		}
		if err := s.CheckContract(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("whole-party admission at the capacity", func(t *testing.T) {
		s := base.Clone()
		if err := s.SetSharedRidePartyLimit(3); err != nil {
			t.Fatal(err)
		}
		v := sharedExpressBoarding(t, s, SharedConsent, 13)
		submit := func(party int) {
			t.Helper()
			if _, err := s.SubmitTripOptions(TripOptions{From: sharedExpressWaterloo, To: sharedExpressBank, PartySize: party, SharingConsent: SharedConsent}); err != nil {
				t.Fatal(err)
			}
		}
		// 13 and 8 passengers are 21, one above the capacity of 20, so the
		// party of 8 waits. 13 and 7 are 20, so the party of 7 joins.
		submit(8)
		if len(v.Riders) != 1 || v.PassengersAboard() != 13 {
			t.Fatalf("a party of 8 joined 13 passengers: %d riders, %d aboard", len(v.Riders), v.PassengersAboard())
		}
		submit(7)
		if len(v.Riders) != 2 || v.PassengersAboard() != 20 {
			t.Fatalf("a party of 7 did not join 13 passengers: %d riders, %d aboard", len(v.Riders), v.PassengersAboard())
		}
	})
	t.Run("emergency unload", func(t *testing.T) {
		s := base.Clone()
		v := sharedExpressBoarding(t, s, PrivateConsent, 13)
		if err := s.withdrawService(v, emergencyHold); err != nil {
			t.Fatal(err)
		}
		if err := s.startOperationalUnload(v, emergencyHold, 0); err != nil {
			t.Fatal(err)
		}
		if want := 14 * TicksPerSecond; v.phaseTicks != want {
			t.Fatalf("phase %d, want %d", v.phaseTicks, want)
		}
		for v.Pod.Activity == Unloading && s.tick < 60*TicksPerSecond {
			s.Step()
		}
		if v.Pod.Activity != Idle {
			t.Fatalf("the unload did not end: %+v", v.Pod)
		}
	})
	for _, test := range []struct {
		activity Activity
		seconds  int
	}{{Boarding, 22}, {Unloading, 21}} {
		t.Run("restore bound, "+string(test.activity), func(t *testing.T) {
			s := base.Clone()
			v := sharedExpressBoarding(t, s, PrivateConsent, 20)
			for v.Pod.Activity != test.activity {
				if s.tick > 900*TicksPerSecond {
					t.Fatalf("the pod did not reach %s: %+v", test.activity, v.Pod)
				}
				s.Step()
			}
			largest := test.seconds * TicksPerSecond
			if maxPhaseTicks(test.activity, ExpressClass) != largest {
				t.Fatalf("bound %d, want %d", maxPhaseTicks(test.activity, ExpressClass), largest)
			}
			restore := func(phase int) (*Simulation, RestoreResult, error) {
				state := roundTripState(t, s.ExportState())
				state.Pods[0].PhaseTicks = phase
				return RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: p.Network, Fleet: fleet, State: state})
			}
			restored, receipt, err := restore(largest)
			if err != nil || !cleanRestore(receipt) {
				t.Fatalf("the largest phase: %+v, %v", receipt, err)
			}
			if got := restored.vehicles[0].phaseTicks; got != largest {
				t.Fatalf("restored phase %d, want %d", got, largest)
			}
			_, _, err = restore(largest + 1)
			if err == nil || !strings.Contains(err.Error(), "phase") || !strings.Contains(err.Error(), "is out of range") {
				t.Fatalf("one tick above the largest phase: %v", err)
			}
		})
	}
}

// TestSharedExpressOccupiedPickupPolicy is the occupied pickup part of the
// disabled policies of SX4 on the preset. The preset has occupied pickups
// off, so the test turns them on with drop-offs sharing for one Compact
// pod at Chancery Lane. The pod departs with a party for Bank and a party
// for St Paul's, the stop between them. The simulation then saves and
// restores, and a party orders a trip from St Paul's to Bank. With
// occupied pickups on, the pod takes that party aboard at St Paul's with
// the party for Bank aboard. With occupied pickups off after the restore,
// it does not, and it serves the party when it is empty. In both copies
// each accepted order completes.
func TestSharedExpressOccupiedPickupPolicy(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express occupied pickup runs without -short or -race")
	}
	const from, via, to = "940GZZLUCHL", "940GZZLUSPU", sharedExpressBank
	p := readSharedExpressProject(t)
	fleet := []Placement{{ID: "compact-pickup", Class: CompactClass, StationID: from, BerthID: from + "-01"}}
	s, err := NewFleetWithOrderContract(p.Network, fleet, ExpressOrderContract)
	if err != nil {
		t.Fatal(err)
	}
	// policies sets the sharing settings, and the occupied pickup policy
	// unless the restore input sets it (restored).
	policies := func(s *Simulation, pickups, restored bool) {
		t.Helper()
		if err := s.SetSharedRidePartyLimit(4); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSharedRideMode(SharedRideDropOffs, 3); err != nil {
			t.Fatal(err)
		}
		if restored {
			return
		}
		if err := s.SetOnboardPickups(pickups); err != nil {
			t.Fatal(err)
		}
	}
	policies(s, true, false)
	submit := func(s *Simulation, from, to string, party int) {
		t.Helper()
		if _, err := s.SubmitTripOptions(TripOptions{From: from, To: to, PartySize: party, SharingConsent: SharedConsent}); err != nil {
			t.Fatal(err)
		}
	}
	submit(s, from, to, 2)
	submit(s, from, via, 1)
	v := &s.vehicles[0]
	for v.Pod.Activity != Traveling || v.PassengersAboard() != 3 {
		if s.tick > 300*TicksPerSecond {
			t.Fatalf("the pod did not depart with both parties: %+v", v.Pod)
		}
		s.Step()
	}
	restore := func(pickups bool) *Simulation {
		t.Helper()
		restored, receipt, err := RestoreState(RestoreStateInput{OrderContract: ExpressOrderContract, Network: p.Network, Fleet: fleet, State: roundTripState(t, s.ExportState()), OnboardPickups: pickups})
		if err != nil || !cleanRestore(receipt) {
			t.Fatalf("restore: %+v, %v", receipt, err)
		}
		policies(restored, pickups, true)
		submit(restored, via, to, 1)
		return restored
	}
	for _, pickups := range []bool{true, false} {
		c := restore(pickups)
		v := &c.vehicles[0]
		occupied := false
		for c.completed < 3 {
			if c.tick > s.tick+30*60*TicksPerSecond {
				t.Fatalf("occupied pickups %v: %d of 3 orders complete after 30 minutes", pickups, c.completed)
			}
			c.Step()
			if _, err := c.SafetyObservation().Check(); err != nil {
				t.Fatal(err)
			}
			if v.Pod.Activity == Boarding && v.Pod.Occupied {
				occupied = true
				if v.Pod.StationID != via || !pickups {
					t.Fatalf("occupied pickups %v: an occupied pickup at %s", pickups, v.Pod.StationID)
				}
			}
		}
		if occupied != pickups {
			t.Fatalf("occupied pickups %v: occupied pickup %v", pickups, occupied)
		}
		if err := c.CheckContract(); err != nil {
			t.Fatal(err)
		}
		t.Logf("occupied pickups %v: occupied pickup at %s %v, 3 orders complete at tick %d", pickups, via, occupied, c.tick)
	}
}
