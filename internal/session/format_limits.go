package session

import (
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// contractMarkers are the orderContract and couplingContract markers of a
// saved state, a stream document or an HTTP state.
type contractMarkers struct {
	order    sim.OrderContract
	coupling sim.CouplingContract
}

// orderBounds returns the largest number of waiting or pending orders, and
// the largest number of stored riders and boarding records of one pod. The
// Express marker selects the Express bounds.
func (markers contractMarkers) orderBounds() (orders, riders int64) {
	if markers.order == sim.ExpressOrderContract {
		return sim.MaxExpressWaitingTrips, sim.MaxExpressParties
	}
	return maxSavedTrips, sim.MaxSharedRideParties
}

// savedLimits bound a state file with the given markers. Only the order
// marker changes a bound. The coupling paths are always present: without
// the coupling marker the order scan refuses each coupling member, so the
// paths only replace the general element limit with a smaller one.
func savedLimits(markers contractMarkers) jsonLimits {
	limits := boardingStateLimits(compactStateLimits(stateJSONLimits))
	orders, riders := markers.orderBounds()
	limits.arrays["/simulation/waiting"] = orders
	limits.arrays["/simulation/pods/*/riders"] = riders
	limits.arrays["/simulation/pods/*/boardings"] = riders
	// The operational destination of a pod is a tuple of 2 or 3 numbers
	// (incident contract, section 11.6).
	limits.arrays["/simulation/pods/*/operational"] = 3
	// The fault records and the debris tuple (incident suspension
	// contract, section 13.5).
	limits.arrays["/simulation/faults/records"] = maxFaultRecords
	limits.arrays["/simulation/faults/records/*"] = debrisTupleLength
	limits.arrays["/project/expressServices"] = project.MaxExpressServices
	for _, path := range []string{
		"/project/network/lanes/*/vehicleClasses",
		"/project/network/stations/*/vehicleClasses",
		"/project/network/stations/*/berths/*/vehicleClasses",
	} {
		limits.arrays[path] = 4
	}
	limits.arrays["/simulation/couplingGroups"] = project.MaxPods / 2
	limits.arrays["/simulation/couplingGroups/*/members"] = 2
	limits.arrays["/project/couplingSites"] = sim.MaxCouplingSites
	limits.arrays["/project/couplingCorridors"] = sim.MaxCouplingCorridors
	limits.arrays["/project/couplingCorridors/*/laneIds"] = project.MaxLanes
	return limits
}

// streamLimits bound a stream document or an HTTP state with the given
// markers. Full frames are under "/full", HTTP frames under "/frame", and
// the topology of an HTTP state under "/topology". As in savedLimits, only
// the order marker changes a bound, and the coupling paths are always
// present. The paths without a prefix bound a coupling delta group that
// scanCouplingReplacement reads alone.
func streamLimits(markers contractMarkers) jsonLimits {
	orders, riders := markers.orderBounds()
	limits := jsonLimits{depth: 64, elements: 65536, members: 256, arrays: map[string]int64{}}
	for _, prefix := range []string{"/full", "/frame"} {
		limits.arrays[prefix+"/routes"] = project.MaxPods
		limits.arrays[prefix+"/routes/*/display"] = project.MaxLanes
		limits.arrays[prefix+"/routes/*/lanes"] = sim.MotionRouteLimit
		limits.arrays[prefix+"/state/checkpoints"] = checkpointLimit
		limits.arrays[prefix+"/state/simulation/vehicles"] = project.MaxPods
		limits.arrays[prefix+"/state/simulation/berths"] = project.MaxNodes
		limits.arrays[prefix+"/state/simulation/pending"] = orders
		limits.arrays[prefix+"/state/simulation/vehicles/*/riders"] = riders
		limits.arrays[prefix+"/state/simulation/vehicles/*/boardings"] = riders
		limits.arrays[prefix+"/state/simulation/vehicles/*/stops"] = sim.MaxSharedRideParties
		limits.arrays[prefix+"/state/simulation/vehicles/*/routeLaneIDs"] = 0
	}
	limits.arrays["/delta/vehicles"] = project.MaxPods
	limits.arrays["/delta/berths"] = project.MaxNodes
	limits.arrays["/delta/groups/checkpoints"] = checkpointLimit
	limits.arrays["/delta/groups/pending"] = orders
	limits.arrays["/delta/vehicles/*/riders/value"] = riders
	limits.arrays["/delta/vehicles/*/boardings/value"] = riders
	limits.arrays["/delta/vehicles/*/stops/value"] = sim.MaxSharedRideParties
	limits.arrays["/delta/vehicles/*/route/value/display"] = project.MaxLanes
	limits.arrays["/delta/vehicles/*/route/value/lanes"] = sim.MotionRouteLimit
	// The active faults: a full frame, an HTTP frame, the delta group,
	// and the group alone (incident suspension contract, section 13.5).
	for _, path := range []string{"/full/state/simulation/faults/active", "/frame/state/simulation/faults/active", "/delta/groups/faults/active", "/active"} {
		limits.arrays[path] = maxFaultRecords
	}
	for path, bound := range topologyJSONLimits.arrays {
		limits.arrays["/topology"+path] = bound
	}
	for _, prefix := range []string{"/full/state/simulation", "/frame/state/simulation", "/delta/groups/coupling", ""} {
		path := prefix + "/couplingGroups"
		limits.arrays[path] = project.MaxPods / 2
		limits.arrays[path+"/*/members"] = 2
		limits.arrays[path+"/*/bodies"] = 2
		for _, shape := range []string{"/bodies/*", "/connector", "/maneuverEnvelope"} {
			limits.arrays[path+"/*"+shape+"/corners"] = 4
		}
	}
	return limits
}
