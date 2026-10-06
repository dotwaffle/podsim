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
// simulation from it share one value. No code writes to the fields in
// place. PrepareNetwork builds them, and ensureNetworkIndexes builds a new
// value when the network changes.
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
}

// PrepareNetwork validates and copies network, then builds its immutable indexes.
// Later changes to the caller's network do not change the prepared network.
func PrepareNetwork(network Network) (*PreparedNetwork, error) {
	owned, graph, err := prepareNetwork(network)
	if err != nil {
		return nil, err
	}
	return newPreparedNetwork(owned, graph), nil
}

func prepareNetwork(network Network) (Network, routeGraph, error) {
	if err := network.validate(); err != nil {
		return Network{}, routeGraph{}, err
	}
	if err := network.ValidateBankGeometry(); err != nil {
		return Network{}, routeGraph{}, err
	}
	owned := network.clone()
	inferStationLaneRoles(&owned)
	graph := newRouteGraph(owned)
	tails := indexGeometryTails(owned)
	for index, lane := range owned.Lanes {
		if bounds := tails[lane.ID]; !finite(bounds.tail) || !finite(bounds.fromTail) {
			return Network{}, routeGraph{}, fmt.Errorf("lane %q has an unsupported large-vehicle path shape", lane.ID)
		}
		length, minimum := graph.lengths[index], LaneMinimumLength(lane)
		if length < minimum {
			return Network{}, routeGraph{}, fmt.Errorf("lane %q must be at least %.0f meters long", lane.ID, minimum)
		}
		if largeClassSet(lane.VehicleClasses) {
			if err := validateLargeLaneCells(lane.ID, length, laneBlockCount(length)); err != nil {
				return Network{}, routeGraph{}, err
			}
		}
	}
	return owned, graph, nil
}

func validateLargeLaneCells(id string, length float64, count int) error {
	for cell := range count {
		start, end := cellOffset(cell, count, length), cellOffset(cell+1, count, length)
		if end-start < largeClearance {
			return fmt.Errorf("lane %q needs actual track cells of at least %.0f meters", id, largeClearance)
		}
	}
	return nil
}

func newPreparedNetwork(owned Network, graph routeGraph) *PreparedNetwork {
	p := &PreparedNetwork{
		network: owned, graph: graph,
		stationIndexes: indexStations(owned), stationForbidden: owned.stationForbidden(),
		geometry: buildLaneGeometry(owned), junctionConflicts: buildJunctionConflicts(owned),
		berthResources: indexBerthResources(owned),
		laneSafety:     make(map[string]SafetyLocation, len(owned.Lanes)),
		berthSafety:    make(map[string]SafetyLocation),
	}
	for _, lane := range owned.Lanes {
		p.laneSafety[lane.ID] = SafetyLocation{SeparationGroup: lane.SeparationGroup, From: lane.From, To: lane.To}
	}
	for _, station := range owned.Stations {
		for _, berth := range station.Berths {
			p.berthSafety[berth.ID] = SafetyLocation{SeparationGroup: berth.SeparationGroup, From: berth.Node, To: berth.Node}
		}
	}
	p.laneCells = indexLaneCells(laneCellsIndexInput{network: owned, geometry: p.geometry, conflicts: p.junctionConflicts, berths: p.berthResources})
	p.resourceLanes = indexResourceLanes(owned, p.laneCells)
	return p
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
		sharedRideMaxStops: DefaultSharedRideMaxStops, sharedRideJoin: DefaultSharedRideJoin,
		platoonLimit: MaxPlatoonLimit, reservationLookaheadSeconds: defaultReservationLookaheadSeconds,
	}
	s.Reset()
	return s
}

// PreparedRestoreInput supplies the fleet and saved state for a prepared network.
// The network must be the one that the saved simulation used.
type PreparedRestoreInput struct {
	OrderContract     OrderContract
	CouplingContract  CouplingContract
	IncidentContract  IncidentContract
	CouplingEnabled   bool
	CouplingSites     []CouplingSite
	CouplingCorridors []CouplingCorridor
	// OnboardPickups enables new occupied pickups after restoration.
	OnboardPickups      bool
	ExpressServices     []ExpressService
	Fleet               []Placement
	State               SavedState
	LogicalOnly         bool
	StationQueueSpacing StationQueueSpacing
	PlatoonLimit        int
}

// RestoreState rebuilds a simulation with this network's immutable geometry.
// Each tier starts with fresh mutable state and performs the same validation as
// RestoreState. A failed physical tier cannot change the logical fallback.
func (p *PreparedNetwork) RestoreState(input PreparedRestoreInput) (*Simulation, RestoreResult, error) {
	if err := p.check(); err != nil {
		return nil, RestoreResult{}, err
	}
	stateInput := RestoreStateInput{
		OrderContract: input.OrderContract, CouplingContract: input.CouplingContract, IncidentContract: input.IncidentContract,
		CouplingEnabled: input.CouplingEnabled, CouplingSites: input.CouplingSites, CouplingCorridors: input.CouplingCorridors,
		OnboardPickups: input.OnboardPickups, ExpressServices: input.ExpressServices,
		Network: p.network, Fleet: input.Fleet, State: input.State, LogicalOnly: input.LogicalOnly,
		StationQueueSpacing: input.StationQueueSpacing, PlatoonLimit: input.PlatoonLimit,
	}
	return restoreState(stateInput, func() (*Simulation, error) {
		return p.NewFleetWithContracts(input.Fleet, stateInput.fleetContracts())
	})
}
