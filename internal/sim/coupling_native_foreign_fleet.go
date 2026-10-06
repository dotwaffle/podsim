package sim

import (
	"maps"
	"math"
)

// This immutable cache binds actual fleet identities and immutable route storage.
// A route or berth change requires new preparation, including saturated versions.
type nativeForeignFleet struct {
	source        *Simulation
	context       *couplingMotionContext
	network       *couplingReservationNetwork
	orderContract OrderContract
	entries       []nativeForeignEntry
	pairs         map[*couplingMotionContext][2]int
}

type nativeForeignEntry struct {
	id           string
	class        VehicleClass
	route        []Lane
	routeVersion uint64
	path         *couplingForeignPath
	parked       *couplingForeignPath
	maxTail      float64
}

// One frame captures the complete fleet and ledger before any member moves.
// Step captures this frame after admission and before movement.
type nativeForeignTick struct {
	fleet          *nativeForeignFleet
	work           *couplingTickWork
	tick           int64
	facts          []nativeForeignFact
	owners         map[resource]resourceOwner
	proofs         map[string]*nativeForeignProof
	pairMembers    map[int]nativeForeignPairMember
	approachFronts map[int]*nativeApproachProof
}

type nativeForeignFact struct {
	pod                             Pod
	cabin                           couplingCabinMotion
	distance                        float64
	blockIndex, through, phaseTicks int
	origin, destination             Berth
	retained                        map[resource]float64
	link                            platoonLink
	follower                        int
	cap                             float64
	compact                         compactBufferMotion
	// faulted and faultCap are the fault members of the pod. A faulted
	// pod moves with faultMoveStep, in move and in each proof.
	faulted  bool
	faultCap float64
	// restoredPose is vehicle.restoredPose.
	restoredPose bool
}

func prepareNativeForeignFleetBound(s *Simulation, n *couplingReservationNetwork, contract OrderContract, pairs ...*couplingMotionContext) (*nativeForeignFleet, error) {
	if s == nil || n == nil || len(s.vehicles) < 2 || len(s.vehicles) > expressMaxPods || s.orderContract != contract || !nativeForeignPreparedIdentity(s, n.prepared) {
		return nil, couplingMotionInvariant("native fleet or contract is invalid")
	}
	f := &nativeForeignFleet{source: s, network: n, orderContract: contract}
	seen := make(map[string]bool, len(s.vehicles))
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if !boundedContractID(v.Pod.ID) || seen[v.Pod.ID] {
			return nil, couplingMotionInvariant("native fleet identities are invalid")
		}
		seen[v.Pod.ID] = true
		entry := nativeForeignEntry{id: v.Pod.ID, class: v.Pod.Class, route: v.Route, routeVersion: v.routeVersion}
		var err error
		if len(v.Route) != 0 {
			entry.path, err = prepareCouplingForeignPath(n, contract, v.Pod.ID, v.Pod.Class, v.Route)
			if err != nil {
				return nil, err
			}
			if !nativeForeignRouteCells(v, &entry.path.blocks) {
				return nil, couplingMotionInvariant("native route cells differ from prepared geometry")
			}
			for _, lane := range entry.path.blocks.lanes {
				if lane.cells != nil {
					entry.maxTail = max(entry.maxTail, Clearance, lane.cells.tail, lane.cells.fromTail)
				}
			}
		}
		if v.Pod.BerthID != "" {
			entry.parked, err = prepareCouplingParkedForeign(n, contract, v.Pod.ID, v.Pod.Class, v.Pod.StationID, v.Pod.BerthID)
			if err != nil {
				return nil, err
			}
		}
		f.entries = append(f.entries, entry)
	}
	if err := f.preparePairRoutes(pairs); err != nil {
		return nil, err
	}
	return f, nil
}

func nativeForeignPreparedIdentity(s *Simulation, p *PreparedNetwork) bool {
	if p == nil || len(p.network.Nodes) == 0 || len(p.network.Lanes) == 0 || len(s.network.Nodes) != len(p.network.Nodes) || len(s.network.Lanes) != len(p.network.Lanes) || len(s.network.Stations) != len(p.network.Stations) {
		return false
	}
	if &s.network.Nodes[0] != &p.network.Nodes[0] || &s.network.Lanes[0] != &p.network.Lanes[0] {
		return false
	}
	return len(s.network.Stations) == 0 || &s.network.Stations[0] == &p.network.Stations[0]
}

func nativeForeignRouteCells(v *vehicle, blocks *blockList) bool {
	if v.blocks.len() != blocks.len() || len(v.blocks.lanes) != len(blocks.lanes) {
		return false
	}
	for i, lane := range blocks.lanes {
		actual := v.blocks.lanes[i]
		if actual.cells != lane.cells || actual.geometry != lane.geometry || actual.first != lane.first || i < len(blocks.route) && (actual.start != lane.start || actual.length != lane.length) {
			return false
		}
	}
	return true
}

func nativeForeignSameRoute(a, b []Lane) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// buildNativeForeignApproachTick captures the frame of the tick. With a nil
// work, the frame owns new copies. With work, the frame copies into the
// buffers of work, and the next build with the same work overwrites it.
func buildNativeForeignApproachTick(s *Simulation, f *nativeForeignFleet, work *couplingTickWork, approaches []couplingApproachTransition, pairs ...couplingNativeForeignPair) (*nativeForeignTick, error) {
	if s == nil || f == nil || f.source != s || f.network == nil || s.tick <= 0 || len(s.vehicles) != len(f.entries) || s.orderContract != f.orderContract || !nativeForeignPreparedIdentity(s, f.network.prepared) {
		return nil, couplingMotionInvariant("native frame has a stale fleet, geometry, or tick")
	}
	frame := &nativeForeignTick{fleet: f, work: work, tick: s.tick}
	if work == nil {
		frame.owners, frame.proofs = maps.Clone(s.owners), make(map[string]*nativeForeignProof, len(f.entries))
		frame.facts = make([]nativeForeignFact, len(s.vehicles))
	} else {
		clear(work.owners)
		clear(work.proofs)
		maps.Copy(work.owners, s.owners)
		frame.owners, frame.proofs = work.owners, work.proofs
		if cap(work.facts) < len(s.vehicles) {
			work.facts = make([]nativeForeignFact, len(s.vehicles))
		}
		work.facts = work.facts[:len(s.vehicles)]
		if len(work.retained) < len(s.vehicles) {
			work.retained = append(work.retained, make([]map[resource]float64, len(s.vehicles)-len(work.retained))...)
			work.views = append(work.views, make([]map[resource]resourceOwner, len(s.vehicles)-len(work.views))...)
			work.claims = append(work.claims, make([][]couplingClaim, len(s.vehicles)-len(work.claims))...)
		}
		frame.facts = work.facts
	}
	for i := range s.vehicles {
		v := &s.vehicles[i]
		entry := &f.entries[i]
		if v.Pod.ID != entry.id || v.Pod.Class != entry.class || v.routeVersion != entry.routeVersion || !nativeForeignSameRoute(v.Route, entry.route) {
			return nil, couplingMotionInvariant("native frame requires new immutable route preparation")
		}
		fact := nativeForeignFact{pod: v.Pod, cabin: nativeForeignCabinInto(v, frame.facts[i].cabin), distance: v.distance, blockIndex: v.blockIndex, through: v.reservedThrough, phaseTicks: v.phaseTicks, origin: v.origin, destination: v.destination, retained: nativeForeignRetained(v, work, i), link: v.link, follower: v.follower, cap: v.platoonCap, faulted: v.faulted, faultCap: v.faultCap, restoredPose: v.restoredPose}
		if len(s.compactMotions) > 0 {
			if len(s.compactMotions) != len(s.vehicles) {
				return nil, couplingMotionInvariant("native compact plan omits fleet members")
			}
			fact.compact = s.compactMotions[i]
		}
		if !finite(fact.distance) || !finite(fact.pod.Speed) || fact.distance < 0 || fact.pod.Speed < 0 || !finite(fact.pod.Position.X) || !finite(fact.pod.Position.Y) {
			return nil, couplingMotionInvariant("native previous pose is invalid")
		}
		frame.facts[i] = fact
	}
	if err := frame.preparePairs(pairs); err != nil {
		return nil, err
	}
	compact, err := frame.compactCertificates(s.compactGroups, s.compactNextGroups)
	if err != nil {
		return nil, err
	}
	if err := frame.prepareApproaches(approaches); err != nil {
		return nil, err
	}
	for i := range frame.facts {
		if f.context != nil && !couplingDeclaredForeign(f.context.foreignIDs, frame.facts[i].pod.ID) {
			continue
		}
		proof, proofErr := frame.prepareProof(i, compact[i])
		if proofErr != nil {
			return nil, proofErr
		}
		proof.faulted, proof.faultCap = frame.facts[i].faulted, frame.facts[i].faultCap
		frame.proofs[proof.raw.Path.id] = proof
	}
	return frame, nil
}

// Body sweep geometry is unchanged by a published ordinary endpoint snap.
func nativeForeignLimit(fact nativeForeignFact, blocks *blockList) (float64, error) {
	if fact.through < 0 || fact.through >= blocks.len() || fact.blockIndex < 0 || fact.blockIndex > fact.through || fact.distance > blocks.at(fact.through).end {
		return 0, couplingMotionInvariant("native ordinary grants are invalid")
	}
	limit := blocks.at(fact.through).end
	if fact.link.leader != 0 && fact.cap < limit {
		limit = fact.cap
	}
	if !finite(limit) || limit < fact.distance {
		return 0, couplingMotionInvariant("native ordinary stopping cap is behind its pose")
	}
	return limit, nil
}

// nativeForeignStep is the motion step of a fact: faultMoveStep with the
// fault cap of a faulted pod, and ordinaryMoveStep otherwise. move makes
// the same choice from the same values.
func nativeForeignStep(fact *nativeForeignFact, blocks *blockList, lane int, limit float64) ordinaryMoveResult {
	if fact.faulted {
		return faultMoveStep(blocks, lane, fact.distance, fact.pod.Speed, limit, fact.faultCap)
	}
	return ordinaryMoveStep(blocks, lane, fact.distance, fact.pod.Speed, limit)
}

func nativeForeignSameFloat(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

func nativeForeignCabin(v *vehicle) couplingCabinMotion {
	return nativeForeignCabinInto(v, couplingCabinMotion{})
}

// nativeForeignCabinInto is nativeForeignCabin. It copies the slices of v
// into the arrays of old, which no other reader holds.
func nativeForeignCabinInto(v *vehicle, old couplingCabinMotion) couplingCabinMotion {
	ridden := 0.0
	if v.RidersAboard() > 0 || len(v.Boardings) > 0 {
		ridden = v.riddenMeters()
	}
	return couplingCabinMotion{Pod: v.Pod, Riders: append(old.Riders[:0], v.Riders...), Stops: append(old.Stops[:0], v.Stops...), Boardings: append(old.Boardings[:0], v.Boardings...), RiddenMeters: ridden, RelocatingTo: v.RelocatingTo, RouteVersion: v.routeVersion}
}

// nativeForeignRetained copies the retention ledger of v, the pod at index.
// With work, it uses the map of the index again.
func nativeForeignRetained(v *vehicle, work *couplingTickWork, index int) map[resource]float64 {
	if work == nil || v.routeReleases == nil {
		return maps.Clone(v.routeReleases)
	}
	retained := work.retained[index]
	if retained == nil {
		retained = make(map[resource]float64, len(v.routeReleases))
		work.retained[index] = retained
	}
	clear(retained)
	maps.Copy(retained, v.routeReleases)
	return retained
}
