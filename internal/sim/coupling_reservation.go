package sim

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
)

type couplingMemberSnapshot struct {
	Vehicle                                                       Vehicle
	RouteVersion                                                  uint64
	Distance                                                      float64
	BlockIndex, ReservedThrough                                   int
	Origin, Destination                                           Berth
	DestinationStation                                            string
	Retained                                                      map[resource]float64
	VirtualLeader, VirtualFollower, CompactQueue, StationManeuver bool
	nativeRiddenBase                                              *float64
}

type couplingReservationInput struct {
	Network       *couplingReservationNetwork
	Prepared      *PreparedNetwork
	CorridorID    string
	OrderContract OrderContract
	Tick          int64
	Members       [2]couplingMemberSnapshot
	Waiting       []Request
	Owners        map[resource]resourceOwner
}

type couplingClaim struct {
	Resource resource
	Expected resourceOwner
}

type couplingExitHolding struct {
	Distance     float64
	Through      int
	SplitCleared bool
}

type couplingReservationPlan struct {
	network                  *couplingReservationNetwork
	corridorID               string
	orderContract            OrderContract
	tick                     int64
	members                  [2]couplingMemberSnapshot
	waiting                  []Request
	routes                   [2]blockList
	axisOrigins              [2]float64
	ClosingStops             [2]float64
	SplitStops               [2]float64
	OpeningStops             [2]float64
	LatchTicks, UnlatchTicks int
	Claims                   []couplingClaim
	PreservedClaims          []couplingClaim
	Dependencies             []couplingDependency
	Exits                    [2]couplingExitHolding
	DrainageProved           bool
}

func cloneCouplingMember(member couplingMemberSnapshot) couplingMemberSnapshot {
	member.Vehicle.Route = cloneLanes(member.Vehicle.Route)
	member.Vehicle.Riders = slices.Clone(member.Vehicle.Riders)
	member.Vehicle.Stops = slices.Clone(member.Vehicle.Stops)
	member.Vehicle.Boardings = slices.Clone(member.Vehicle.Boardings)
	member.Retained = maps.Clone(member.Retained)
	if member.nativeRiddenBase != nil {
		member.nativeRiddenBase = new(*member.nativeRiddenBase)
	}
	return member
}

// Native cabins keep the existing mileage clock's operation order.
func (member couplingMemberSnapshot) cabinMeters(distance float64) float64 {
	v := member.Vehicle
	if member.nativeRiddenBase != nil {
		if v.PassengersAboard() > 0 {
			return *member.nativeRiddenBase + distance
		}
		if len(v.Boardings) > 0 {
			return *member.nativeRiddenBase
		}
		return 0
	}
	meters := v.RiddenMeters
	if v.PassengersAboard() > 0 {
		meters += distance - member.Distance
	}
	return meters
}

// This function prepares a certificate. It never changes the input or owners.
func planCouplingReservation(input couplingReservationInput) (couplingReservationPlan, error) {
	if input.Network == nil || input.Prepared != input.Network.prepared {
		return couplingReservationPlan{}, couplingDenied("prepared network identity changed")
	}
	n := input.Network
	corridor, exists := n.corridors[input.CorridorID]
	if !exists || n.contract != CompactPairV1CouplingContract || ValidateOrderContract(input.OrderContract) != nil || input.Tick < 0 {
		return couplingReservationPlan{}, couplingDenied("unknown corridor, contract, or tick")
	}
	if err := couplingMemberEligibility(input); err != nil {
		return couplingReservationPlan{}, err
	}
	plan := couplingReservationPlan{network: n, corridorID: input.CorridorID, orderContract: input.OrderContract, tick: input.Tick, waiting: slices.Clone(input.Waiting)}
	profile, _ := LookupCouplingProfile(n.contract)
	plan.LatchTicks, plan.UnlatchTicks = profile.LatchTicks, profile.UnlatchTicks
	dependencies := make(map[resource]couplingDependency)
	for i, member := range &input.Members {
		plan.members[i] = cloneCouplingMember(member)
		if err := plan.prepareMember(i, corridor); err != nil {
			return couplingReservationPlan{}, err
		}
		if err := plan.collectMember(i, corridor, dependencies, input.Owners); err != nil {
			return couplingReservationPlan{}, err
		}
	}
	if err := plan.checkExitSeparation(); err != nil {
		return couplingReservationPlan{}, err
	}
	for _, siteID := range []string{corridor.AssemblySiteID, corridor.SplitSiteID} {
		r := resource{kind: couplingSiteResourceKind(), id: siteID}
		site := n.sites[siteID]
		room, _ := CouplingSiteRoom(n.contract)
		dependency := couplingDependency{Resource: r, Site: true, NotBefore: couplingConnected}
		if siteID == corridor.SplitSiteID {
			dependency.NotBefore = couplingDraining
		}
		for i := range plan.routes {
			lane := slices.IndexFunc(plan.routes[i].route, func(lane Lane) bool { return lane.ID == site.LaneID })
			dependency.MemberUse[i] = true
			dependency.MemberRelease[i] = plan.routes[i].lanes[lane].start + site.EndMeters + room.BoundaryMarginMeters
		}
		dependencies[r] = dependency
	}
	if err := plan.preserveReceivingClaims(input, dependencies); err != nil {
		return couplingReservationPlan{}, err
	}
	for r, dependency := range dependencies {
		owner := input.Owners[r]
		if !couplingClaimOwner(owner, r, input.Members) {
			return couplingReservationPlan{}, couplingDenied(fmt.Sprintf("foreign owner at resource %+v", r))
		}
		plan.Claims = append(plan.Claims, couplingClaim{Resource: r, Expected: owner})
		plan.Dependencies = append(plan.Dependencies, dependency)
	}
	slices.SortFunc(plan.Claims, func(a, b couplingClaim) int { return compareCouplingResource(a.Resource, b.Resource) })
	slices.SortFunc(plan.PreservedClaims, func(a, b couplingClaim) int { return compareCouplingResource(a.Resource, b.Resource) })
	slices.SortFunc(plan.Dependencies, func(a, b couplingDependency) int { return compareCouplingResource(a.Resource, b.Resource) })
	return plan, nil
}

func couplingClaimOwner(owner resourceOwner, r resource, members [2]couplingMemberSnapshot) bool {
	if owner.isZero() {
		return true
	}
	return r.kind != couplingSiteResourceKind() && (owner.isPod(members[0].Vehicle.Pod.ID) || owner.isPod(members[1].Vehicle.Pod.ID))
}

func compareCouplingResource(a, b resource) int {
	if a.kind < b.kind {
		return -1
	}
	if a.kind > b.kind {
		return 1
	}
	if a.id < b.id {
		return -1
	}
	if a.id > b.id {
		return 1
	}
	return a.cell - b.cell
}

func couplingMemberEligibility(input couplingReservationInput) error {
	front, rear := input.Members[0].Vehicle.Pod.ID, input.Members[1].Vehicle.Pod.ID
	if !boundedContractID(front) || !boundedContractID(rear) || front == rear {
		return couplingDenied("invalid ordered member identities")
	}
	if len(input.Waiting) > MaxWaitingTripsForOrderContract(input.OrderContract) {
		return couplingDenied("pending facts exceed existing bound")
	}
	partyIDs := make(map[int]bool)
	occupied := input.Members[0].Vehicle.PassengersAboard() > 0
	for _, member := range &input.Members {
		v := member.Vehicle
		if v.Pod.Class != CompactClass || v.Presentation != nil || member.VirtualLeader || member.VirtualFollower || member.CompactQueue || member.StationManeuver ||
			v.PlatoonID != "" || v.PlatoonIndex != 0 || v.Pod.StationPhase != "" || v.Pod.ManeuverStationID != "" {
			return couplingDenied("member has incompatible class or maneuver")
		}
		if v.Pod.Speed != 0 || v.Pod.BerthID != "" || v.Pod.StationID != "" || !finite(member.Distance) || member.Distance < 0 || v.Pod.Activity != Traveling {
			return couplingDenied("member is not stopped on an ordinary route")
		}
		if (v.PassengersAboard() > 0) != occupied || v.Pod.Occupied != occupied {
			return couplingDenied("mixed or inconsistent occupancy")
		}
		if member.nativeRiddenBase != nil && (!finite(*member.nativeRiddenBase) || member.cabinMeters(member.Distance) != v.RiddenMeters) {
			return couplingDenied("native cabin mileage differs from its history baseline")
		}
		for _, request := range input.Waiting {
			if request.PodID == v.Pod.ID {
				return couplingDenied("member has a pending pickup binding")
			}
		}
		if err := couplingCabinFacts(v, input.OrderContract, input.Tick, input.Network.prepared.network); err != nil {
			return err
		}
		for _, rider := range v.Riders {
			if partyIDs[rider.ID] {
				return couplingDenied("party identity appears in both cabins")
			}
			partyIDs[rider.ID] = true
		}
	}
	return nil
}

func couplingCabinFacts(v Vehicle, contract OrderContract, tick int64, network Network) error {
	if len(v.Riders) > MaxStoredRidersForOrderContract(v.Pod.Class, contract) || len(v.Stops) > MaxSharedRideParties {
		return couplingDenied("stored cabin facts exceed existing bounds")
	}
	var active []SavedRequest
	ids := make(map[int]bool, len(v.Riders))
	for _, rider := range v.Riders {
		if rider.ID <= 0 || ids[rider.ID] || rider.PodID != v.Pod.ID || rider.PartySize < 1 || rider.PartySize > 4 || !validSavedOptionsWithOrderContract(SavedRequest(rider), contract) {
			return couplingDenied("invalid immutable party facts")
		}
		if rider.From == rider.To || rider.LegFrom == rider.To || rider.RequestedTick < 0 || rider.RequestedTick > tick || rider.BoardedTick < rider.RequestedTick || rider.BoardedTick > tick {
			return couplingDenied("invalid party chronology or endpoints")
		}
		ends := []string{rider.From, rider.To}
		if rider.LegFrom != "" {
			ends = append(ends, rider.LegFrom)
		}
		for _, id := range ends {
			station, ok := network.Station(id)
			if !ok || station.ParkingOnly || !station.VehicleClasses.Allows(string(CompactClass)) {
				return couplingDenied("party station is incompatible")
			}
		}
		ids[rider.ID] = true
		if !rider.Completed {
			if !slices.Contains(v.Stops, rider.To) {
				return couplingDenied("active party destination is absent from stops")
			}
			active = append(active, SavedRequest(rider))
		}
	}
	seenStops := make(map[string]bool, len(v.Stops))
	for _, stop := range v.Stops {
		if seenStops[stop] || !slices.ContainsFunc(active, func(rider SavedRequest) bool { return rider.To == stop }) {
			return couplingDenied("stop has no distinct active party target")
		}
		seenStops[stop] = true
	}
	if v.PassengersAboard() > 4 {
		return couplingDenied("party capacity is not per cabin")
	}
	if len(v.Boardings) > 0 && len(v.Boardings) != len(v.Riders) {
		return couplingDenied("boarding records differ from rider identities")
	}
	if err := checkSavedAdmissionWithOrderContract(SavedPod{Class: CompactClass}, active, contract); err != nil {
		return fmt.Errorf("cabin admission: %w", err)
	}
	stored := make([]SavedRequest, len(v.Riders))
	for i, rider := range v.Riders {
		stored[i] = SavedRequest(rider)
	}
	pod := SavedPod{Class: CompactClass, Activity: activityCode(v.Pod.Activity), Occupied: v.Pod.Occupied, Riders: stored, Boardings: v.Boardings, RiddenMeters: v.RiddenMeters}
	if err := checkSavedBoardingsWithOrderContract(pod, contract); err != nil {
		return fmt.Errorf("cabin boarding facts: %w", err)
	}
	if err := checkBoardingBerths(network, pod); err != nil {
		return fmt.Errorf("cabin boarding origins: %w", err)
	}
	return nil
}

func (plan *couplingReservationPlan) prepareMember(index int, corridor CouplingCorridor) error {
	member := plan.members[index]
	blocks, err := plan.network.routeBlocks(member.Vehicle.Route)
	if err != nil {
		return err
	}
	first := slices.IndexFunc(blocks.route, func(lane Lane) bool { return lane.ID == corridor.LaneIDs[0] })
	if first < 0 || first+len(corridor.LaneIDs) > len(blocks.route) {
		return couplingDenied("route lacks complete corridor")
	}
	for i, id := range corridor.LaneIDs {
		if blocks.route[first+i].ID != id {
			return couplingDenied("routes diverge before split")
		}
	}
	assembly := plan.network.sites[corridor.AssemblySiteID]
	stage := assembly.FrontStagingMeters
	if index == 1 {
		stage = assembly.RearStagingMeters
	}
	origin := blocks.lanes[first].start
	if math.Abs(member.Distance-origin-stage) > conflictSlack || member.Vehicle.Pod.LaneID != assembly.LaneID || math.Abs(member.Vehicle.Pod.LaneDistance-stage) > conflictSlack {
		return couplingDenied("member is not at its authored staging coordinate")
	}
	position := couplingOffset(plan.network.lanes[assembly.LaneID].from, plan.network.lanes[assembly.LaneID].direction, stage)
	if pointDistance(position, member.Vehicle.Pod.Position) > conflictSlack {
		return couplingDenied("member position differs from route geometry")
	}
	if member.BlockIndex < 0 || member.ReservedThrough < member.BlockIndex || member.ReservedThrough >= blocks.len() {
		return couplingDenied("invalid current grants")
	}
	b := blocks.at(member.BlockIndex)
	if member.Distance < b.start || member.Distance > b.end || b.lane.ID != assembly.LaneID {
		return couplingDenied("current cell does not contain stopped member")
	}
	plan.routes[index], plan.axisOrigins[index] = blocks, origin
	plan.ClosingStops[index] = plan.network.closingStop(index, origin, assembly)
	return plan.prepareExit(index, first+len(corridor.LaneIDs)-1, corridor)
}

// closingStop returns the closing stop of member index (0 is the front),
// where origin is the route distance of the start of the assembly lane.
func (n *couplingReservationNetwork) closingStop(index int, origin float64, assembly CouplingSite) float64 {
	profile, _ := LookupCouplingProfile(n.contract)
	stop := origin + assembly.FrontStagingMeters
	if index == 1 {
		stop -= profile.CenterSpacingMeters
	}
	return stop
}

func (plan *couplingReservationPlan) prepareExit(index, last int, corridor CouplingCorridor) error {
	blocks := &plan.routes[index]
	member := plan.members[index]
	if err := plan.receivingIntent(member, blocks); err != nil {
		return err
	}
	split := plan.network.sites[corridor.SplitSiteID]
	room, _ := CouplingSiteRoom(plan.network.contract)
	splitOrigin := blocks.lanes[last].start
	profile, _ := LookupCouplingProfile(plan.network.contract)
	plan.SplitStops[index] = splitOrigin + split.FrontStagingMeters
	if index == 1 {
		plan.SplitStops[index] -= profile.CenterSpacingMeters
	}
	opening := splitOrigin + split.FrontStagingMeters + room.OpeningTravelMeters
	if index == 1 {
		opening = splitOrigin + split.FrontStagingMeters - profile.CenterSpacingMeters
	}
	hold := splitOrigin + split.EndMeters + room.BoundaryMarginMeters
	if index == 0 {
		hold += Clearance
	}
	total := blocks.lanes[len(blocks.route)].start
	cleared := hold+room.StoppingMeters <= total
	if !cleared {
		hold = opening
	}
	if hold+room.StoppingMeters > total {
		return couplingDenied("continuation lacks bounded exit stopping room")
	}
	if err := plan.stopsAfterSplit(member, blocks, splitOrigin+split.FrontStagingMeters+room.OpeningTravelMeters); err != nil {
		return err
	}
	plan.OpeningStops[index] = opening
	through := blocks.lanes[last+1].first - 1
	for through+1 < blocks.len() && blocks.at(through).end < hold+room.StoppingMeters {
		through++
	}
	through = reservationEnd(blocks, through)
	if through >= blocks.len() {
		return couplingDenied("downstream closure leaves actual continuation")
	}
	plan.Exits[index] = couplingExitHolding{Distance: hold, Through: through, SplitCleared: cleared}
	return nil
}

func (plan *couplingReservationPlan) receivingIntent(member couplingMemberSnapshot, blocks *blockList) error {
	n := plan.network.prepared.network
	v := member.Vehicle
	stationID := member.DestinationStation
	if v.PassengersAboard() > 0 {
		if len(v.Stops) == 0 || stationID != v.Stops[0] {
			return couplingDenied("passenger stop does not bind current continuation")
		}
		for _, id := range v.Stops {
			station, exists := n.Station(id)
			if !exists || station.ParkingOnly || !station.VehicleClasses.Allows(string(CompactClass)) {
				return couplingDenied("invalid passenger stop")
			}
		}
	} else if v.RelocatingTo == "" || v.RelocatingTo != stationID {
		return couplingDenied("empty member lacks its existing receiving intent")
	}
	station, exists := n.Station(stationID)
	if !exists || !station.VehicleClasses.Allows(string(CompactClass)) {
		return couplingDenied("receiving station is incompatible")
	}
	end := blocks.route[len(blocks.route)-1].To
	if member.Destination.ID != "" {
		berth, ok := station.berth(member.Destination.ID)
		if !ok || berth != member.Destination || !berth.VehicleClasses.Allows(string(CompactClass)) || end != berth.Node {
			return couplingDenied("receiving berth differs from actual route")
		}
		return nil
	}
	if v.PassengersAboard() == 0 || !station.isEntry(end) {
		return couplingDenied("route does not end at its existing passenger approach")
	}
	for _, berth := range station.Berths {
		if berth.VehicleClasses.Allows(string(CompactClass)) {
			return nil
		}
	}
	return couplingDenied("passenger approach has no compatible receiving berth")
}

// An active stop boundary must follow the complete split opening interval.
func (plan *couplingReservationPlan) stopsAfterSplit(member couplingMemberSnapshot, blocks *blockList, openingEnd float64) error {
	for _, id := range member.Vehicle.Stops {
		station, _ := plan.network.prepared.network.Station(id)
		for i, lane := range blocks.route {
			end := blocks.lanes[i].start + blocks.lanes[i].length
			if end < member.Distance || end > openingEnd {
				continue
			}
			if station.isEntry(lane.To) || slices.ContainsFunc(station.Berths, func(berth Berth) bool { return berth.Node == lane.To }) {
				return couplingDenied("passenger stop precedes complete split opening")
			}
		}
	}
	return nil
}

func (plan *couplingReservationPlan) collectMember(index int, corridor CouplingCorridor, dependencies map[resource]couplingDependency, owners map[resource]resourceOwner) error {
	member := plan.members[index]
	blocks := &plan.routes[index]
	expected, err := plan.currentFootprint(index, dependencies, owners)
	if err != nil {
		return err
	}
	first := slices.IndexFunc(blocks.route, func(lane Lane) bool { return lane.ID == corridor.LaneIDs[0] })
	start, through := blocks.lanes[first].first, plan.Exits[index].Through
	if err := plan.network.proveInteractions(blocks, start, through); err != nil {
		return err
	}
	for _, b := range blocks.span(start, through+1) {
		for _, r := range b.resources {
			addCouplingDependency(dependencies, r, index, resourceReleaseDistance(b, r), plan.axisOrigins[index], b.lane.ID, corridor.LaneIDs)
		}
	}
	for r, releaseAt := range member.Retained {
		if !finite(releaseAt) || releaseAt < 0 || !owners[r].isPod(member.Vehicle.Pod.ID) || math.Abs(releaseAt-expected[r]) > conflictSlack || expected[r] == 0 {
			return couplingDenied("invalid individual retained dependency")
		}
		addCouplingDependency(dependencies, r, index, releaseAt, plan.axisOrigins[index], "", nil)
	}
	return nil
}

func (plan *couplingReservationPlan) currentFootprint(index int, dependencies map[resource]couplingDependency, owners map[resource]resourceOwner) (map[resource]float64, error) {
	member := plan.members[index]
	blocks := &plan.routes[index]
	expected := make(map[resource]float64)
	for _, b := range blocks.span(0, member.ReservedThrough+1) {
		for _, r := range b.resources {
			releaseAt := resourceReleaseDistance(b, r)
			expected[r] = max(expected[r], releaseAt)
			if releaseAt <= member.Distance {
				continue
			}
			if !owners[r].isPod(member.Vehicle.Pod.ID) {
				return nil, couplingDenied("current individual footprint lacks its own pod tag")
			}
			if member.Retained[r] < releaseAt {
				return nil, couplingDenied("current footprint lacks its retention ledger")
			}
			addCouplingDependency(dependencies, r, index, releaseAt, plan.axisOrigins[index], "", nil)
		}
	}
	if member.Origin.ID != "" && member.Distance < max(Clearance, blocks.lanes[0].cells.fromTail) {
		for _, r := range berthResources(member.Origin) {
			expected[r] = max(expected[r], max(Clearance, blocks.lanes[0].cells.fromTail))
			if !owners[r].isPod(member.Vehicle.Pod.ID) {
				return nil, couplingDenied("current origin footprint lacks its pod tag")
			}
			addCouplingDependency(dependencies, r, index, max(Clearance, blocks.lanes[0].cells.fromTail), plan.axisOrigins[index], "", nil)
		}
	}
	return expected, nil
}

func (plan *couplingReservationPlan) preserveReceivingClaims(input couplingReservationInput, dependencies map[resource]couplingDependency) error {
	preserved := make(map[resource]resourceOwner)
	for _, member := range &input.Members {
		if member.Destination.ID == "" {
			continue
		}
		claims := berthResources(member.Destination)
		if member.Vehicle.PassengersAboard() > 0 {
			berthHeld := input.Owners[claims[0]].isPod(member.Vehicle.Pod.ID)
			nodeHeld := input.Owners[claims[1]].isPod(member.Vehicle.Pod.ID)
			if !berthHeld && !nodeHeld {
				continue
			}
			if berthHeld != nodeHeld {
				return couplingDenied("passenger receiving claim is incomplete")
			}
		}
		for _, r := range claims {
			if !input.Owners[r].isPod(member.Vehicle.Pod.ID) {
				return couplingDenied("existing receiving claim lost its individual owner")
			}
			if _, adopted := dependencies[r]; !adopted {
				preserved[r] = input.Owners[r]
			}
		}
	}
	for r, owner := range input.Owners {
		if !owner.isPod(input.Members[0].Vehicle.Pod.ID) && !owner.isPod(input.Members[1].Vehicle.Pod.ID) {
			continue
		}
		if _, adopted := dependencies[r]; adopted {
			continue
		}
		if _, kept := preserved[r]; !kept {
			return couplingDenied("individual claim has no proved footprint or receiving role")
		}
	}
	for r, owner := range preserved {
		plan.PreservedClaims = append(plan.PreservedClaims, couplingClaim{Resource: r, Expected: owner})
	}
	return nil
}

func (plan *couplingReservationPlan) checkExitSeparation() error {
	front := couplingRoutePoint(&plan.routes[0], plan.Exits[0].Distance)
	rear := couplingRoutePoint(&plan.routes[1], plan.Exits[1].Distance)
	if pointDistance(front, rear) < Clearance-conflictSlack {
		return couplingDenied("exit holdings lack ordinary separation")
	}
	return nil
}

func couplingRoutePoint(blocks *blockList, distance float64) Point {
	for i, lane := range blocks.lanes[:len(blocks.route)] {
		if distance > lane.start+lane.length {
			continue
		}
		for _, segment := range lane.geometry.segments {
			local := distance - lane.start
			if local <= segment.end {
				return couplingSegmentPoint(segment, max(segment.start, local))
			}
		}
		return blocks.lanes[i].geometry.end
	}
	return blocks.lanes[len(blocks.route)-1].geometry.end
}

// Return a copied write set only after every mutable predicate passes again.
func revalidateCouplingReservation(plan couplingReservationPlan, input couplingReservationInput) ([]couplingClaim, error) {
	if input.Network != plan.network || input.CorridorID != plan.corridorID || input.OrderContract != plan.orderContract || input.Tick != plan.tick ||
		!reflect.DeepEqual(input.Members, plan.members) || !reflect.DeepEqual(input.Waiting, plan.waiting) {
		return nil, couplingDenied("member, route, or party stamp changed")
	}
	fresh, err := planCouplingReservation(input)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(fresh.Claims, plan.Claims) || !reflect.DeepEqual(fresh.PreservedClaims, plan.PreservedClaims) || !reflect.DeepEqual(fresh.Dependencies, plan.Dependencies) || fresh.Exits != plan.Exits || fresh.ClosingStops != plan.ClosingStops || fresh.SplitStops != plan.SplitStops || fresh.OpeningStops != plan.OpeningStops || fresh.LatchTicks != plan.LatchTicks || fresh.UnlatchTicks != plan.UnlatchTicks {
		return nil, couplingDenied("reservation certificate changed")
	}
	return slices.Clone(fresh.Claims), nil
}
