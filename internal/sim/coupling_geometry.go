package sim

import (
	"fmt"
	"math"
)

// CouplingSite declares one protected assembly or split interval on a lane.
// Positions use meters from the lane's directed start. A site is not a berth.
type CouplingSite struct {
	ID                 string
	LaneID             string
	StartMeters        float64
	EndMeters          float64
	FrontStagingMeters float64
	RearStagingMeters  float64
}

// CouplingCorridor declares an ordered directed path between two protected sites.
type CouplingCorridor struct {
	ID             string
	AssemblySiteID string
	SplitSiteID    string
	LaneIDs        []string
}

// CouplingGeometryInput keeps the marker independent from the order contract.
type CouplingGeometryInput struct {
	Contract  CouplingContract
	Network   Network
	Sites     []CouplingSite
	Corridors []CouplingCorridor
}

// ValidateCouplingGeometry checks bounded authored geometry without mutation.
// All positions use the network's single XY plane. No height or grade is implied.
// Passing this check does not prove crossing, junction, or foreign-traffic safety.
// Runtime admission must reserve the complete path, both sites, and exit holds.
// A known marker with both registries empty permits a geometry-free opt-in.
// It supplies no formation path. One-sided or unreferenced records reject.
func ValidateCouplingGeometry(input CouplingGeometryInput) error {
	if input.Contract == "" && len(input.Sites) == 0 && len(input.Corridors) == 0 {
		return nil
	}
	if _, ok := LookupCouplingProfile(input.Contract); !ok {
		return ErrUnknownCouplingContract
	}
	if len(input.Sites) == 0 && len(input.Corridors) == 0 {
		return nil
	}
	if err := couplingGeometryBounds(input); err != nil {
		return err
	}
	room, err := CouplingSiteRoom(input.Contract)
	if err != nil {
		return err
	}
	lanes := prepareCouplingLanes(input.Network)
	sites, err := couplingSites(input, lanes, room)
	if err != nil {
		return err
	}
	return couplingCorridors(input, lanes, sites)
}

type couplingLaneGeometry struct {
	lane      Lane
	from, to  Point
	direction Point
	length    float64
}

// Index coordinates once. Every corridor reuses the same immutable records.
func prepareCouplingLanes(network Network) map[string]couplingLaneGeometry {
	nodes := make(map[string]Point, len(network.Nodes))
	for _, node := range network.Nodes {
		nodes[node.ID] = node.Position
	}
	lanes := make(map[string]couplingLaneGeometry, len(network.Lanes))
	for _, lane := range network.Lanes {
		from, to := nodes[lane.From], nodes[lane.To]
		length := pointDistance(from, to)
		var direction Point
		if length > 0 {
			direction = Point{X: (to.X - from.X) / length, Y: (to.Y - from.Y) / length}
		}
		lanes[lane.ID] = couplingLaneGeometry{lane: lane, from: from, to: to, length: length, direction: direction}
	}
	return lanes
}

func couplingGeometryBounds(input CouplingGeometryInput) error {
	n := input.Network
	if len(input.Sites) < 2 || len(input.Sites) > MaxCouplingSites || len(input.Corridors) < 1 || len(input.Corridors) > MaxCouplingCorridors ||
		len(n.Nodes) < 1 || len(n.Nodes) > expressMaxNodes || len(n.Lanes) < 1 || len(n.Lanes) > expressMaxLanes || len(n.Stations) > expressMaxStations {
		return fmt.Errorf("coupling collections exceed bounds: %w", ErrInvalidCouplingGeometry)
	}
	if err := validateContractNetworkRecords(n); err != nil {
		return fmt.Errorf("coupling network records: %w: %w", err, ErrInvalidCouplingGeometry)
	}
	if err := validateContractGeometryBudget(n); err != nil {
		return fmt.Errorf("coupling network budget: %w: %w", err, ErrInvalidCouplingGeometry)
	}
	if err := n.validate(); err != nil {
		return fmt.Errorf("coupling network: %w: %w", err, ErrInvalidCouplingGeometry)
	}
	return nil
}

func couplingSites(input CouplingGeometryInput, lanes map[string]couplingLaneGeometry, room CouplingRoom) (map[string]CouplingSite, error) {
	sites := make(map[string]CouplingSite, len(input.Sites))
	for _, site := range input.Sites {
		if !boundedContractID(site.ID) || !boundedContractID(site.LaneID) {
			return nil, fmt.Errorf("invalid site or lane ID: %w", ErrInvalidCouplingGeometry)
		}
		if _, duplicate := sites[site.ID]; duplicate {
			return nil, fmt.Errorf("invalid or duplicate site ID: %w", ErrInvalidCouplingGeometry)
		}
		lane, exists := lanes[site.LaneID]
		if !exists {
			return nil, fmt.Errorf("unknown or invalid site lane ID: %w", ErrInvalidCouplingGeometry)
		}
		if err := couplingLane(lane.lane); err != nil {
			return nil, err
		}
		if err := couplingSiteFits(lane.length, site, room); err != nil {
			return nil, err
		}
		for _, previous := range sites {
			if site.LaneID == previous.LaneID && site.StartMeters < previous.EndMeters && previous.StartMeters < site.EndMeters {
				return nil, fmt.Errorf("protected sites overlap on %q: %w", site.LaneID, ErrInvalidCouplingGeometry)
			}
		}
		sites[site.ID] = site
	}
	return sites, nil
}

func couplingLane(lane Lane) error {
	if lane.Control != nil || lane.VehicleClasses != classBit(string(CompactClass)) {
		return fmt.Errorf("lane %q must be straight and explicitly Compact-only: %w", lane.ID, ErrInvalidCouplingGeometry)
	}
	return nil
}

func couplingSiteFits(length float64, site CouplingSite, room CouplingRoom) error {
	for _, value := range []float64{site.StartMeters, site.EndMeters, site.FrontStagingMeters, site.RearStagingMeters} {
		if !finite(value) {
			return fmt.Errorf("nonfinite site %q: %w", site.ID, ErrInvalidCouplingGeometry)
		}
	}
	if site.StartMeters < 0 || site.EndMeters > length || site.EndMeters-site.StartMeters < room.RequiredLengthMeters ||
		math.Abs(site.FrontStagingMeters-site.RearStagingMeters-room.StagingSpacingMeters) > conflictSlack ||
		site.RearStagingMeters-room.BoundaryMarginMeters < site.StartMeters ||
		site.FrontStagingMeters+room.OpeningTravelMeters+room.BoundaryMarginMeters > site.EndMeters {
		return fmt.Errorf("site %q lacks staging, maneuver, or stopping room: %w", site.ID, ErrInvalidCouplingGeometry)
	}
	return nil
}

func couplingCorridors(input CouplingGeometryInput, lanes map[string]couplingLaneGeometry, sites map[string]CouplingSite) error {
	seen := make(map[string]bool, len(input.Corridors))
	used := make(map[string]bool, len(sites))
	for _, corridor := range input.Corridors {
		if !boundedContractID(corridor.ID) || seen[corridor.ID] || len(corridor.LaneIDs) < 1 || len(corridor.LaneIDs) > expressMaxLanes {
			return fmt.Errorf("invalid corridor identity or path: %w", ErrInvalidCouplingGeometry)
		}
		seen[corridor.ID] = true
		if !boundedContractID(corridor.AssemblySiteID) || !boundedContractID(corridor.SplitSiteID) {
			return fmt.Errorf("invalid endpoint site ID: %w", ErrInvalidCouplingGeometry)
		}
		assembly, aOK := sites[corridor.AssemblySiteID]
		split, sOK := sites[corridor.SplitSiteID]
		if !aOK || !sOK || assembly.ID == split.ID || corridor.LaneIDs[0] != assembly.LaneID || corridor.LaneIDs[len(corridor.LaneIDs)-1] != split.LaneID {
			return fmt.Errorf("corridor %q does not bind distinct endpoint sites: %w", corridor.ID, ErrInvalidCouplingGeometry)
		}
		if len(corridor.LaneIDs) == 1 && assembly.EndMeters > split.StartMeters {
			return fmt.Errorf("corridor %q reverses its site order: %w", corridor.ID, ErrInvalidCouplingGeometry)
		}
		if err := couplingPath(corridor, lanes); err != nil {
			return err
		}
		used[assembly.ID], used[split.ID] = true, true
	}
	for id := range sites {
		if !used[id] {
			return fmt.Errorf("site %q has no corridor: %w", id, ErrInvalidCouplingGeometry)
		}
	}
	return nil
}

func couplingPath(corridor CouplingCorridor, lanes map[string]couplingLaneGeometry) error {
	var previous Lane
	var direction, origin Point
	seen := make(map[string]bool, len(corridor.LaneIDs))
	for index, id := range corridor.LaneIDs {
		if !boundedContractID(id) {
			return fmt.Errorf("invalid corridor lane ID: %w", ErrInvalidCouplingGeometry)
		}
		lane, exists := lanes[id]
		if !exists || seen[id] {
			return fmt.Errorf("invalid corridor lane ID: %w", ErrInvalidCouplingGeometry)
		}
		seen[id] = true
		if err := couplingLane(lane.lane); err != nil {
			return err
		}
		if !finite(lane.length) || lane.length <= 0 {
			return fmt.Errorf("zero or nonfinite corridor segment: %w", ErrInvalidCouplingGeometry)
		}
		if index == 0 {
			direction, origin = lane.direction, lane.from
		} else if previous.To != lane.lane.From || direction.X*lane.direction.X+direction.Y*lane.direction.Y <= 0 {
			return fmt.Errorf("corridor %q is disconnected or reversed: %w", corridor.ID, ErrInvalidCouplingGeometry)
		}
		if err := couplingAxisFits(origin, direction, lane); err != nil {
			return err
		}
		previous = lane.lane
	}
	return nil
}

func couplingAxisFits(origin, direction Point, lane couplingLaneGeometry) error {
	// The existing arithmetic slack is an absolute distance in meters here.
	// A small per-segment heading error cannot accumulate across a long path.
	for _, point := range []Point{lane.from, lane.to} {
		distance := math.Abs((point.X-origin.X)*direction.Y - (point.Y-origin.Y)*direction.X)
		if !finite(distance) || distance > conflictSlack {
			return fmt.Errorf("corridor endpoint leaves its fixed XY axis: %w", ErrInvalidCouplingGeometry)
		}
	}
	return nil
}
