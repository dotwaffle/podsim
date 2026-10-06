package sim

import (
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// The tailored-network probe of natural multi-pair coupling. Ordinary
// balanced demand runs on a generated network. No test code places a
// trip, a group, or an approach.
// docs/measurements/coupling-natural-multi-pair-probe.json gives the
// network design and the measured runs.
const (
	couplingProbeSeed = 4
	// couplingProbeRate is the number of orders in each simulated minute.
	couplingProbeRate = 8
	couplingProbePods = 70
	// couplingProbeTicks is 20 simulated minutes. In the pinned run, the
	// last train retires at tick 70750.
	couplingProbeTicks = 20 * 60 * TicksPerSecond
	// couplingProbeHalf is half the side of the square ring in meters.
	couplingProbeHalf = 600
	// couplingProbeJoinSpeed is the speed limit in m/s of the return road
	// that crosses each spoke at its merge. The other lanes have 14 m/s.
	couplingProbeJoinSpeed = 3
	couplingProbeBerths    = 10
)

type couplingProbeBuilder struct {
	network Network
}

func (b *couplingProbeBuilder) node(id string, position Point) {
	b.network.Nodes = append(b.network.Nodes, Node{ID: id, Position: position})
}

func (b *couplingProbeBuilder) lane(id, from, to string, speed float64, role StationLaneRole, station string) {
	b.network.Lanes = append(b.network.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: speed, StationRole: role, StationID: station})
}

// station adds a station on a side loop from trunk node d to trunk node m.
// at maps an (along, offset) pair to a point. The entry is at (60, 60)
// from along 260, the exit is at along 460, and the berth rows are 40 m
// apart beyond them.
func (b *couplingProbeBuilder) station(id, d, m string, at func(along, offset float64) Point) {
	const speed, entryAlong, exitAlong, offset = 14.0, 260.0, 460.0, 60.0
	entry, exit := id+"-entry", id+"-exit"
	b.node(entry, at(entryAlong, offset))
	b.node(exit, at(exitAlong, offset))
	b.lane(id+"-in", d, entry, speed, StationEntryRole, id)
	b.lane(id+"-through", entry, exit, speed, StationThroughRole, id)
	b.lane(id+"-out", exit, m, speed, StationExitRole, id)
	station := Station{ID: id, Name: id, Entry: entry, Exit: exit}
	arrival, departure := entry, exit
	for k := 1; k <= couplingProbeBerths; k++ {
		arr, dep, berth := fmt.Sprintf("%s-arr-%d", id, k), fmt.Sprintf("%s-dep-%d", id, k), fmt.Sprintf("%s-b-%d", id, k)
		row := offset + 40*float64(k)
		b.node(arr, at(entryAlong, row))
		b.node(berth, at((entryAlong+exitAlong)/2, row))
		b.node(dep, at(exitAlong, row))
		b.lane(fmt.Sprintf("%s-arrival-link-%d", id, k), arrival, arr, speed, StationBerthAccessRole, id)
		b.lane(fmt.Sprintf("%s-departure-link-%d", id, k), dep, departure, speed, StationDepartureRole, id)
		b.lane(fmt.Sprintf("%s-in-%d", id, k), arr, berth, speed, StationBerthAccessRole, id)
		b.lane(fmt.Sprintf("%s-out-%d", id, k), berth, dep, speed, StationDepartureRole, id)
		station.Berths = append(station.Berths, Berth{ID: fmt.Sprintf("%s-%d", id, k), Node: berth})
		arrival, departure = arr, dep
	}
	b.network.Stations = append(b.network.Stations, station)
}

// couplingProbeNetwork returns a one-way square ring with a spoke at each
// corner, and the assembly and split lanes of each spoke.
//
// A spoke continues the incoming side of the ring straight on, and the
// ring turns left at the corner. The spoke runs 280 m to the assembly lane
// p-M, 120 m to the merge M, and 120 m on the split lane to the fork F.
// At F, a left and a right branch each lead to one station. Thus only the
// orders for these two stations use the spoke, and two members with
// different stations separate at F. The left branch returns to the next
// side of the ring. The right branch returns past the spoke and joins M at
// about 20 degrees at couplingProbeJoinSpeed. It crosses the spoke there to
// the next side. While a pod crosses, the junction at M stops the spoke
// pods at the start of its conflict zone, which is the assembly point.
func couplingProbeNetwork() (Network, [][2]string) {
	const speed, h = 14.0, couplingProbeHalf
	var b couplingProbeBuilder
	var corridors [][2]string
	corners := []Point{{X: -h, Y: -h}, {X: h, Y: -h}, {X: h, Y: h}, {X: -h, Y: h}}
	for k := range corners {
		b.node(fmt.Sprintf("c%d", k), corners[k])
	}
	for k, c := range corners {
		previous := corners[(k+3)%4]
		u := Point{X: (c.X - previous.X) / (2 * h), Y: (c.Y - previous.Y) / (2 * h)}
		// S gives a point at along meters past the corner on the spoke
		// axis, and lateral meters to its right.
		S := func(along, lateral float64) Point {
			return Point{X: c.X + along*u.X + lateral*u.Y, Y: c.Y + along*u.Y - lateral*u.X}
		}
		id := func(name string) string { return fmt.Sprintf("k%d-%s", k, name) }
		corner, next := fmt.Sprintf("c%d", k), fmt.Sprintf("c%d", (k+1)%4)
		// The next side of the ring leaves the corner to the left of the
		// spoke. The returns join it at 300 m and 600 m.
		b.node(id("pr"), S(0, -300))
		b.node(id("lr"), S(0, -600))
		b.lane(id("ring-pr"), corner, id("pr"), speed, "", "")
		b.lane(id("ring-lr"), id("pr"), id("lr"), speed, "", "")
		b.lane(id("ring-end"), id("lr"), next, speed, "", "")
		b.node(id("p"), S(280, 0))
		b.node(id("M"), S(400, 0))
		b.node(id("F"), S(520, 0))
		b.lane(id("approach"), corner, id("p"), speed, "", "")
		b.lane(id("asm"), id("p"), id("M"), speed, "", "")
		b.lane(id("split"), id("M"), id("F"), speed, "", "")
		corridors = append(corridors, [2]string{id("asm"), id("split")})
		for _, branch := range []struct {
			name string
			side float64
		}{{"L", -1}, {"R", 1}} {
			name, side := branch.name, branch.side
			g0, gd, gm := id(name+"-g0"), id(name+"-gd"), id(name+"-gm")
			b.node(g0, S(620, side*100))
			b.node(gd, S(620, side*200))
			b.node(gm, S(620, side*520))
			b.lane(id(name+"-fork"), id("F"), g0, speed, "", "")
			b.lane(id(name+"-g1"), g0, gd, speed, "", "")
			b.lane(id(name+"-g2"), gd, gm, speed, "", "")
			b.station(id(name), gd, gm, func(along, offset float64) Point { return S(620+offset, side*along) })
		}
		b.lane(id("L-return"), id("L-gm"), id("lr"), speed, "", "")
		b.node(id("r1"), S(320, 520))
		b.node(id("r2"), S(320, 30))
		b.node(id("x1"), S(480, -30))
		b.node(id("x2"), S(480, -150))
		b.lane(id("R-return"), id("R-gm"), id("r1"), speed, "", "")
		b.lane(id("R-return2"), id("r1"), id("r2"), speed, "", "")
		b.lane(id("R-join"), id("r2"), id("M"), couplingProbeJoinSpeed, "", "")
		b.lane(id("cross"), id("M"), id("x1"), speed, "", "")
		b.lane(id("cross2"), id("x1"), id("x2"), speed, "", "")
		b.lane(id("cross-return"), id("x2"), id("pr"), speed, "", "")
	}
	return b.network, corridors
}

// couplingProbeContracts makes the corridor lanes Compact-only and returns
// the sites by the rule of the earlier natural multi-pair record. E is the
// start of the first cell of the assembly lane that holds the junction
// resource of M. The split site starts 1 m after the end of the first cell
// of the split lane, which holds the junction resource of M, and is 49 m
// long.
func couplingProbeContracts(t *testing.T, network *Network, corridors [][2]string) FleetContracts {
	t.Helper()
	compact := classBit(string(CompactClass))
	for i := range network.Lanes {
		for _, corridor := range corridors {
			if network.Lanes[i].ID == corridor[0] || network.Lanes[i].ID == corridor[1] {
				network.Lanes[i].VehicleClasses = compact
			}
		}
	}
	p, err := PrepareNetwork(*network)
	if err != nil {
		t.Fatal(err)
	}
	room, err := CouplingSiteRoom(CompactPairV1CouplingContract)
	if err != nil {
		t.Fatal(err)
	}
	contracts := FleetContracts{CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true}
	for i, corridor := range corridors {
		merge := network.Lanes[slices.IndexFunc(network.Lanes, func(lane Lane) bool { return lane.ID == corridor[0] })].To
		junction := func(cells *laneCells, cell int) bool {
			return slices.Contains(cells.cell(cell), resource{kind: junctionResource, id: merge})
		}
		cells, length := p.laneCells[corridor[0]], p.graph.lengths[p.graph.lanes[corridor[0]]]
		zone := 0
		for zone < cells.count() && !junction(cells, zone) {
			zone++
		}
		// The pinned run uses E at the start of cell 2 or later, so the
		// fixture keeps that geometry.
		if zone < 2 || zone == cells.count() {
			t.Fatalf("corridor %s has its merge zone at cell %d", corridor[0], zone)
		}
		e := cellOffset(zone, cells.count(), length)
		split, splitLength := p.laneCells[corridor[1]], p.graph.lengths[p.graph.lanes[corridor[1]]]
		if !junction(split, 0) {
			t.Fatalf("split lane %s does not start in the merge zone", corridor[1])
		}
		start := math.Ceil(splitLength/float64(split.count())) + 1
		id := fmt.Sprintf("c%02d", i+1)
		contracts.CouplingSites = append(contracts.CouplingSites,
			CouplingSite{ID: id + "-assembly", LaneID: corridor[0], StartMeters: e - 30, EndMeters: min(e+40, length), RearStagingMeters: e, FrontStagingMeters: e + 12},
			CouplingSite{ID: id + "-split", LaneID: corridor[1], StartMeters: start, EndMeters: start + 49,
				RearStagingMeters: start + room.BoundaryMarginMeters + 0.5, FrontStagingMeters: start + room.BoundaryMarginMeters + 12.5})
		contracts.CouplingCorridors = append(contracts.CouplingCorridors,
			CouplingCorridor{ID: id, AssemblySiteID: id + "-assembly", SplitSiteID: id + "-split", LaneIDs: []string{corridor[0], corridor[1]}})
	}
	if err := ValidateCouplingGeometry(CouplingGeometryInput{Contract: contracts.CouplingContract, Network: *network, Sites: contracts.CouplingSites, Corridors: contracts.CouplingCorridors}); err != nil {
		t.Fatal(err)
	}
	return contracts
}

// couplingProbeFleet places the pods round robin on the stations, one berth
// per station in each round.
func couplingProbeFleet(network Network) []Placement {
	var placements []Placement
	for round := 0; len(placements) < couplingProbePods; round++ {
		for _, station := range network.Stations {
			if len(placements) == couplingProbePods {
				break
			}
			placements = append(placements, Placement{ID: fmt.Sprintf("pod-%03d", len(placements)+1), Class: CompactClass, StationID: station.ID, BerthID: station.Berths[round].ID})
		}
	}
	return placements
}

type couplingProbeTrain struct {
	id, corridor          string
	members               [2]string
	formation, retirement int64
}

type couplingProbeResult struct {
	trains []*couplingProbeTrain
	// overlap is the first tick with two live trains, or -1.
	overlap              int64
	approaches           int
	completed, submitted int
	digests              [][32]byte
}

// runCouplingProbe runs the pinned demand for couplingProbeTicks. After
// each tick it checks the coupling fault, the motion of each member, the
// state contract, safety, the order count, and the train registry. It
// keeps a digest of the exported state every 1,000 ticks and at the end.
func runCouplingProbe(t *testing.T, network Network, contracts FleetContracts, enabled bool) couplingProbeResult {
	t.Helper()
	contracts.CouplingEnabled = enabled
	s, err := NewFleetWithContracts(network, couplingProbeFleet(network), contracts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatoonLimit(2); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	stations := make([]string, len(network.Stations))
	for i, station := range network.Stations {
		stations[i] = station.ID
	}
	// The session generator for the balanced pattern, with its queue limit.
	rng := rand.New(rand.NewPCG(couplingProbeSeed, ^uint64(couplingProbeSeed)))
	budget, accepted := 0, 0
	result := couplingProbeResult{overlap: -1}
	trains := make(map[string]*couplingProbeTrain)
	approaches := make(map[[2]string]int64)
	completed := make(map[int]bool)
	digest := func() {
		raw, err := json.Marshal(s.ExportState(), json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		result.digests = append(result.digests, sha256.Sum256(raw))
	}
	for s.tick < couplingProbeTicks {
		before := couplingMemberMotions(s)
		s.Step()
		if err := s.CouplingError(); err != nil {
			t.Fatalf("coupling failed at tick %d: %v", s.tick, err)
		}
		if err := checkCouplingMemberMotion(s, before); err != nil {
			t.Fatalf("member motion at tick %d: %v", s.tick, err)
		}
		if err := s.CheckContract(); err != nil {
			t.Fatalf("state contract at tick %d: %v", s.tick, err)
		}
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatalf("safety at tick %d: %v", s.tick, err)
		}
		for _, completion := range s.StepCompletions() {
			if completed[completion.RequestID] {
				t.Fatalf("completion %d replayed at tick %d", completion.RequestID, s.tick)
			}
			completed[completion.RequestID] = true
		}
		held := len(s.waiting)
		for i := range s.vehicles {
			for _, rider := range s.vehicles[i].Riders {
				if !rider.Completed {
					held++
				}
			}
		}
		if s.requestID != accepted || accepted != s.completed+s.interrupted+held || s.completed != len(completed) {
			t.Fatalf("orders at tick %d: accepted %d, submitted %d, completed %d (%d seen), interrupted %d, held %d",
				s.tick, accepted, s.requestID, s.completed, len(completed), s.interrupted, held)
		}
		if !enabled && (len(s.couplingApproaches) != 0 || len(s.couplingGroups) != 0) {
			t.Fatalf("disabled coupling recruited at tick %d", s.tick)
		}
		for _, a := range s.couplingApproaches {
			pair := [2]string{a.context.members[0].id, a.context.members[1].id}
			if _, seen := approaches[pair]; !seen || approaches[pair] != a.context.tick {
				approaches[pair] = a.context.tick
				result.approaches++
			}
		}
		live := make(map[string]bool, len(s.couplingGroups))
		members := make(map[string]string)
		for _, g := range s.couplingGroups {
			id := g.context.owner.id
			live[id] = true
			pair := [2]string{g.context.reservation.members[0].Vehicle.Pod.ID, g.context.reservation.members[1].Vehicle.Pod.ID}
			for _, pod := range pair {
				if other, ok := members[pod]; ok {
					t.Fatalf("pod %s is a member of live trains %s and %s at tick %d", pod, other, id, s.tick)
				}
				members[pod] = id
				if s.findVehicle(pod).couplingID != id {
					t.Fatalf("member %s does not name its train %s at tick %d", pod, id, s.tick)
				}
			}
			train := trains[id]
			if train == nil {
				train = &couplingProbeTrain{id: id, corridor: g.context.reservation.corridorID, members: pair, formation: g.formationTick, retirement: -1}
				if g.formationTick != s.tick {
					t.Fatalf("train %s appeared at tick %d with formation tick %d", id, s.tick, g.formationTick)
				}
				trains[id] = train
				result.trains = append(result.trains, train)
			}
			if train.corridor != g.context.reservation.corridorID || train.members != pair {
				t.Fatalf("train %s changed its corridor or members at tick %d", id, s.tick)
			}
		}
		for _, train := range result.trains {
			if train.retirement < 0 && !live[train.id] {
				train.retirement = s.tick
				for _, pod := range train.members {
					if s.findVehicle(pod).couplingID != "" {
						t.Fatalf("retired train %s left member %s bound", train.id, pod)
					}
				}
			}
		}
		if len(live) >= 2 && result.overlap < 0 {
			result.overlap = s.tick
		}
		if s.tick%1000 == 0 {
			digest()
		}
		budget += couplingProbeRate
		if budget >= 60*TicksPerSecond {
			budget -= 60 * TicksPerSecond
			from := rng.IntN(len(stations))
			to := rng.IntN(len(stations) - 1)
			if to >= from {
				to++
			}
			if s.PendingCount() < 200 {
				if err := s.RequestTrip(stations[from], stations[to]); err == nil {
					accepted++
				}
			}
		}
	}
	digest()
	result.completed, result.submitted = s.completed, s.requestID
	return result
}

// TestCouplingNaturalMultiPairProbe pins one run of ordinary demand on the
// tailored network. Two trains must be alive at once on different
// corridors, and one corridor must form a second train after its first
// train retires. A second enabled run must give the same state digests,
// and a disabled control with the same demand must form no train.
func TestCouplingNaturalMultiPairProbe(t *testing.T) { //nolint:tparallel // The group waits for its parallel runs before the comparisons.
	t.Parallel()
	skipLong(t)
	network, corridors := couplingProbeNetwork()
	contracts := couplingProbeContracts(t, &network, corridors)
	var runs [3]couplingProbeResult
	t.Run("runs", func(t *testing.T) {
		for i, name := range []string{"enabled", "twin", "disabled"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				runs[i] = runCouplingProbe(t, network, contracts, name != "disabled")
			})
		}
	})
	if t.Failed() {
		return
	}
	enabled, twin, disabled := runs[0], runs[1], runs[2]
	if !slices.Equal(enabled.digests, twin.digests) {
		t.Fatal("two enabled runs gave different state digests")
	}
	if len(disabled.trains) != 0 || disabled.approaches != 0 {
		t.Fatalf("disabled control formed %d trains and %d approaches", len(disabled.trains), disabled.approaches)
	}
	for _, train := range enabled.trains {
		t.Logf("train %s corridor %s members %s,%s formation %d retirement %d", train.id, train.corridor, train.members[0], train.members[1], train.formation, train.retirement)
	}
	t.Logf("trains=%d approaches=%d firstOverlap=%d completed=%d submitted=%d controlCompleted=%d digest=%x",
		len(enabled.trains), enabled.approaches, enabled.overlap, enabled.completed, enabled.submitted, disabled.completed, enabled.digests[len(enabled.digests)-1])
	overlap, sequential := false, false
	for i, a := range enabled.trains {
		for _, b := range enabled.trains[i+1:] {
			if a.retirement < 0 || b.retirement < 0 {
				t.Fatalf("train %s or %s did not retire by the horizon", a.id, b.id)
			}
			if a.corridor != b.corridor && b.formation < a.retirement {
				overlap = true
			}
			if a.corridor == b.corridor && b.formation > a.retirement {
				sequential = true
			}
		}
	}
	if !overlap || !sequential {
		t.Fatalf("pinned run missed its gate: two live trains on different corridors %t, two sequential trains on one corridor %t", overlap, sequential)
	}
}
