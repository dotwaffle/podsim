package sim

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
)

// PreparedNetwork owns validated network geometry and indexes that do not change.
// Its methods can be called concurrently. Each returned simulation owns its
// mutable state and must be used by only one goroutine at a time.
// The zero value is not prepared. Use PrepareNetwork to create a handle.
type PreparedNetwork struct {
	networkIndexes
}

// networkIndexes holds a network and its indexes. PreparedNetwork and each
// simulation from it share one value. PrepareNetwork builds it, and no code
// writes to it after that.
type networkIndexes struct {
	network           Network
	graph             routeGraph
	stationIndexes    map[string]int
	stationForbidden  map[string]bool
	geometry          map[string]*laneGeometry
	junctionConflicts map[string][]laneConflict
	// berthResources holds the berth resources at each node.
	berthResources map[string][]resource
	// laneCells holds the cells of each network lane, which the blocks of
	// each route share. It is built from the geometry, junction and berth
	// indexes.
	laneCells map[string]*laneCells
	// resourceLanes holds the lanes whose cells hold each resource that is
	// not a track resource. It is built from laneCells.
	resourceLanes map[resource][]int32
	laneSafety    map[string]SafetyLocation
	berthSafety   map[string]SafetyLocation
	// berthNodes holds the node of each station berth. nodeSafety holds
	// the safety locations of the lanes that start or end at each node, in
	// network lane order, one entry for each lane. The safety envelopes
	// read both.
	berthNodes map[string]string
	nodeSafety map[string][]SafetyLocation
	// tails holds the one tail index of the network. The lane cells and the
	// retention index read it.
	tails map[string]geometryTails
	// retention holds the reduced retention of the qualifying lanes and
	// their windows. The lane cells and the safety observation read it.
	retention *laneRetention
	// maneuvers holds the maneuver groups, or nil when the network has
	// none. certified holds the pairs that the maneuver certification
	// clears. The bank audit and the safety observation skip them.
	maneuvers *maneuverIndex
	certified certifiedPairs
	// depots holds the depots and their lane paths, or nil when the
	// network has none.
	depots *depotIndex
	// berthKeys holds the separation key index of each berth (see
	// indexBerthKeys).
	berthKeys map[string]int
	// lateral holds the lateral pairs of the depot lanes. The bank audit,
	// the safety observation, and the debris footprint read it.
	lateral *depotLateral
	// platoonData holds the network data that links read.
	platoonData platoonIndexData
}

// PrepareNetwork validates and copies network, then builds its immutable indexes.
// Later changes to the caller's network do not change the prepared network.
func PrepareNetwork(network Network) (*PreparedNetwork, error) {
	prepared, err := prepareNetwork(network)
	if err != nil {
		return nil, err
	}
	return newPreparedNetwork(prepared), nil
}

// ValidateNetwork returns the error of PrepareNetwork for network. It does
// not build the indexes that a prepared network keeps for a simulation,
// because they cannot refuse a network.
func ValidateNetwork(network Network) error {
	_, err := prepareNetwork(network)
	return err
}

// ValidateNetworkClearance is ValidateNetwork with the tails of an index of
// the same network, so that one editor check builds one tail index. The zero
// index has no tails, and the validation then builds them.
func ValidateNetworkClearance(network Network, clearance LaneClearance) error {
	_, err := checkNetworkTails(network, clearance.tails)
	return err
}

// networkPreparation holds a validated copy of a network, its route graph
// and its retention index.
type networkPreparation struct {
	network   Network
	graph     routeGraph
	tails     map[string]geometryTails
	retention *laneRetention
	maneuvers *maneuverIndex
	certified certifiedPairs
	depots    *depotIndex
	berthKeys map[string]int
	lateral   *depotLateral
	// reach is the refusal of the bank audit that the checks of the network
	// held until the last of them passed.
	reach *PlaneReachError
}

// prepareNetwork validates network and returns an owned copy. It builds the
// retention index one time, before the bank audit and the length checks,
// which read it. The index does not refuse a network, so the order of the
// refusals does not change.
func prepareNetwork(network Network) (networkPreparation, error) {
	return checkNetworkTails(network, nil)
}

// checkNetworkTails is prepareNetwork with the tail index of the network, or
// nil to build it.
func checkNetworkTails(network Network, tails map[string]geometryTails) (networkPreparation, error) {
	prepared, err := prepareNetworkTails(network, tails)
	if err != nil {
		return networkPreparation{}, err
	}
	if prepared.reach != nil {
		return networkPreparation{}, prepared.reach
	}
	return prepared, nil
}

// prepareNetworkTails is checkNetworkTails without the plane reach refusal,
// which it returns in the preparation. A caller with later checks reports the
// refusal after them.
func prepareNetworkTails(network Network, tails map[string]geometryTails) (networkPreparation, error) {
	if err := network.validate(); err != nil {
		return networkPreparation{}, err
	}
	owned := network.clone()
	inferStationLaneRoles(&owned)
	graph := newRouteGraph(owned)
	if tails == nil {
		tails = indexGeometryTails(owned)
	}
	retention := newLaneRetention(owned, tails)
	// validate checked the groups, so the index has no error.
	maneuvers, _ := newManeuverIndex(owned, indexNodePositions(owned.Nodes))
	berthKeys := indexBerthKeys(owned)
	certified, err := certifyManeuvers(owned, maneuvers, berthKeys)
	if err != nil {
		return networkPreparation{}, err
	}
	// validate checked the depots, so the index has no error.
	depots, _ := newDepotIndex(owned, indexNodePositions(owned.Nodes))
	lateral := newDepotLateral(owned, depots, tails, berthKeys)
	var reach *PlaneReachError
	if err := network.validateBankGeometry(bankAudit{retention: retention, maneuvers: maneuvers, certified: certified, depots: depots, lateral: lateral}); err != nil && !errors.As(err, &reach) {
		return networkPreparation{}, err
	}
	for index, lane := range owned.Lanes {
		if bounds := tails[lane.ID]; !finite(bounds.tail) || !finite(bounds.fromTail) {
			return networkPreparation{}, fmt.Errorf("lane %q has an unsupported large-vehicle path shape", lane.ID)
		}
		// A lane of a maneuver group needs only a positive length (see
		// invalidLane): one pod at a time uses the group.
		if maneuvers.groupLane(lane.ID) {
			continue
		}
		length, minimum := graph.lengths[index], depots.chainMinimum(lane, retention)
		if length < minimum {
			return networkPreparation{}, fmt.Errorf("lane %q must be at least %g meters long", lane.ID, minimum)
		}
		if largeClassSet(lane.VehicleClasses) {
			if err := validateLargeLaneCells(lane.ID, length, laneBlockCount(length)); err != nil {
				return networkPreparation{}, err
			}
		}
	}
	return networkPreparation{network: owned, graph: graph, tails: tails, retention: retention, maneuvers: maneuvers, certified: certified, depots: depots, berthKeys: berthKeys, lateral: lateral, reach: reach}, nil
}

func validateLargeLaneCells(id string, length float64, count int) error {
	for cell := range count {
		start, end := cellOffset(cell, count, length), cellOffset(cell+1, count, length)
		if end-start < largeClearance {
			return fmt.Errorf("lane %q needs actual track cells of at least %g meters", id, largeClearance)
		}
	}
	return nil
}

func newPreparedNetwork(prepared networkPreparation) *PreparedNetwork {
	owned := prepared.network
	p := &PreparedNetwork{
		network: owned, graph: prepared.graph, tails: prepared.tails, retention: prepared.retention, maneuvers: prepared.maneuvers, certified: prepared.certified, depots: prepared.depots,
		berthKeys: prepared.berthKeys, lateral: prepared.lateral,
		stationIndexes: indexStations(owned), stationForbidden: owned.stationForbidden(),
		geometry: buildLaneGeometry(owned), junctionConflicts: buildJunctionConflicts(owned),
		berthResources: indexBerthResources(owned),
	}
	p.indexSafety()
	p.laneCells = indexLaneCells(laneCellsIndexInput{network: owned, tails: prepared.tails, geometry: p.geometry, conflicts: p.junctionConflicts, berths: p.berthResources, retention: p.retention, maneuvers: p.maneuvers, depots: p.depots})
	p.resourceLanes = indexResourceLanes(owned, p.laneCells)
	p.platoonData = newPlatoonIndexData(owned)
	return p
}

// indexSafety builds the safety locations of the lanes and the berths of
// the network, and the berth and node indexes of the safety envelopes.
func (n *networkIndexes) indexSafety() {
	n.laneSafety = make(map[string]SafetyLocation, len(n.network.Lanes))
	n.berthSafety = make(map[string]SafetyLocation)
	n.berthNodes = make(map[string]string)
	n.nodeSafety = make(map[string][]SafetyLocation)
	for _, lane := range n.network.Lanes {
		location := SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}
		n.laneSafety[lane.ID] = location
		n.nodeSafety[lane.From] = append(n.nodeSafety[lane.From], location)
		if lane.To != lane.From {
			n.nodeSafety[lane.To] = append(n.nodeSafety[lane.To], location)
		}
	}
	for _, station := range n.network.Stations {
		for _, berth := range station.Berths {
			n.berthSafety[berth.ID] = SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}
			n.berthNodes[berth.ID] = berth.Node
		}
	}
}

func (p *PreparedNetwork) check() error {
	if p == nil || len(p.network.Nodes) == 0 {
		return errors.New("network is not prepared")
	}
	return nil
}

// Network returns a detached copy of the prepared network.
func (p *PreparedNetwork) Network() Network {
	if p == nil {
		return Network{}
	}
	return p.network.clone()
}

// NewFleet validates and copies placements and creates fresh simulation state.
// An omitted berth ID selects the first berth, as in NewFleet.
func (p *PreparedNetwork) NewFleet(placements []Placement) (*Simulation, error) {
	if err := p.check(); err != nil {
		return nil, err
	}
	if err := validatePlacements(p.network, placements); err != nil {
		return nil, err
	}
	return p.newFleet(placements), nil
}

func validatePlacements(network Network, placements []Placement) error {
	return validatePlacementsWithOrderContract(network, placements, "")
}

func validatePlacementsWithOrderContract(network Network, placements []Placement, contract OrderContract) error {
	if err := validateContractFleetBounds(network, placements, contract); err != nil {
		return err
	}
	if len(placements) == 0 {
		return errors.New("the fleet needs at least one pod")
	}
	ids, berths := make(map[string]bool), make(map[string]bool)
	geometry := make([]initialPlacementGeometry, 0, len(placements))
	incident := incidentLanes(network)
	for _, placement := range placements {
		if err := ValidateVehicleClassProfileWithOrderContract(placement.Class, contract); err != nil {
			return err
		}
		if placement.ID == "" || ids[placement.ID] {
			return fmt.Errorf("invalid or duplicate pod %q", placement.ID)
		}
		station, ok := network.Station(placement.StationID)
		if !ok {
			return fmt.Errorf("unknown start station %q", placement.StationID)
		}
		berth, ok := station.berth(placement.BerthID)
		if !ok || berths[berth.ID] {
			return fmt.Errorf("invalid or occupied initial berth at %q", placement.StationID)
		}
		if !station.VehicleClasses.Allows(string(placement.Class)) || !berth.VehicleClasses.Allows(string(placement.Class)) {
			return fmt.Errorf("pod %s class is incompatible with its initial berth", placement.ID)
		}
		ids[placement.ID], berths[berth.ID] = true, true
		node, _ := network.Node(berth.Node)
		geometry = append(geometry, initialPlacementGeometry{id: placement.ID, class: placement.Class, position: node.Position,
			locations: berthGeometryLocations(berth, incident[berth.Node])})
	}
	return validateInitialSeparation(geometry)
}

type initialPlacementGeometry struct {
	id        string
	class     VehicleClass
	position  Point
	locations []SafetyLocation
}

func berthGeometryLocations(berth Berth, incident []Lane) []SafetyLocation {
	locations := make([]SafetyLocation, 0, 1+len(incident))
	locations = append(locations, SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node})
	for _, lane := range incident {
		locations = append(locations, SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To})
	}
	return locations
}

func validateInitialSeparation(placements []initialPlacementGeometry) error {
	for i, first := range placements {
		for _, second := range placements[i+1:] {
			if !largeVehicleClass(first.class) && !largeVehicleClass(second.class) || envelopeLocationsSeparated(first.locations, second.locations) {
				continue
			}
			clearance := classPairClearance(first.class, second.class)
			if pointDistance(first.position, second.position) < clearance-separationTolerance {
				return fmt.Errorf("initial pods %q and %q need at least %.0f meters of separation", first.id, second.id, clearance)
			}
		}
	}
	return nil
}

func (p *PreparedNetwork) newFleet(placements []Placement) *Simulation {
	initial := slices.Clone(placements)
	slices.SortFunc(initial, func(a, b Placement) int { return cmp.Compare(a.ID, b.ID) })
	s := &Simulation{
		networkIndexes: &p.networkIndexes, initial: initial,
		sharedRidePartyLimit: 1, sharedRideMode: DefaultSharedRideMode,
		sharedRideMaxStops: DefaultSharedRideMaxStops,
		platoonLimit:       MaxPlatoonLimit,
	}
	s.Reset()
	return s
}

// PreparedRestoreInput supplies the fleet and saved state for a prepared network.
// The network must be the one that the saved simulation used.
type PreparedRestoreInput struct {
	OrderContract OrderContract
	// FaultsOn, Faults, EmergenciesOn, and Rates are the members of the same
	// name of RestoreStateInput.
	FaultsOn      bool
	Faults        FaultSettings
	EmergenciesOn bool
	Rates         IncidentRates
	// OnboardPickups enables new occupied pickups after restoration.
	OnboardPickups  bool
	ExpressServices []ExpressService
	Fleet           []Placement
	State           SavedState
	LogicalOnly     bool
}

// RestoreState rebuilds a simulation with this network's immutable geometry.
// Each tier starts with fresh mutable state and performs the same validation as
// RestoreState. A failed physical tier cannot change the logical fallback.
func (p *PreparedNetwork) RestoreState(input PreparedRestoreInput) (*Simulation, RestoreResult, error) {
	if err := p.check(); err != nil {
		return nil, RestoreResult{}, err
	}
	stateInput := RestoreStateInput{
		OrderContract: input.OrderContract,
		FaultsOn:      input.FaultsOn, Faults: input.Faults, EmergenciesOn: input.EmergenciesOn, Rates: input.Rates,
		OnboardPickups: input.OnboardPickups, ExpressServices: input.ExpressServices,
		Network: p.network, Fleet: input.Fleet, State: input.State, LogicalOnly: input.LogicalOnly,
	}
	return restoreState(stateInput, func() (*Simulation, error) {
		return p.NewFleetWithOrderContract(input.Fleet, input.OrderContract)
	})
}
