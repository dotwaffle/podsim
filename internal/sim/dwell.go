package sim

import "slices"

// dwellTicksPerPassenger is the door time of one passenger of a class with
// standees: two door streams of about 2 s for each passenger.
const dwellTicksPerPassenger = 1 * TicksPerSecond

// loadedPhase returns the phase of a pod of a class that has passengers at the
// door. A seat-only class keeps its base for each load. Express adds the door
// time of each passenger after the first, up to the capacity of the class.
func loadedPhase(base int, class VehicleClass, passengers int) int {
	if class != ExpressClass {
		return base
	}
	profile, _ := LookupVehicleClass(class)
	return base + (min(max(passengers, 1), profile.Capacity())-1)*dwellTicksPerPassenger
}

// boardingPhase returns the boarding phase of a party that starts a phase.
func boardingPhase(class VehicleClass, passengers int) int {
	return loadedPhase(boardingTicks, class, passengers)
}

// unloadingPhase returns the unloading phase for the alighting passengers.
func unloadingPhase(class VehicleClass, passengers int) int {
	return loadedPhase(unloadingTicks, class, passengers)
}

// maxBoardingPhase returns the largest boarding phase of a class.
func maxBoardingPhase(class VehicleClass) int {
	return boardingPhase(class, MaxExpressParties)
}

// maxUnloadingPhase returns the largest unloading phase of a class.
func maxUnloadingPhase(class VehicleClass) int {
	return unloadingPhase(class, MaxExpressParties)
}

// addJoinDwell adds the door time of a party that joins a boarding pod. The
// phase does not restart, and it stays inside the bound of the class. A pod
// that waited for track at phase zero boards again, so its wait ends. A
// seat-only class keeps its phase.
func (v *vehicle) addJoinDwell(passengers int) {
	if v.Pod.Class != ExpressClass {
		return
	}
	v.phaseTicks = min(v.phaseTicks+passengers*dwellTicksPerPassenger, maxBoardingPhase(v.Pod.Class))
	v.pending = -1
	v.Pod.WaitReason, v.Pod.BlockedBy = NoWait, ""
}

// alightingPassengers returns the passengers that leave a pod at its berth:
// each active party in an emergency unload, or else each active party with no
// later stop, as alight decides.
func (v *vehicle) alightingPassengers() int {
	if v.op.purpose == opEmergencyUnload {
		return v.PassengersAboard()
	}
	passengers := 0
	for index := range v.Riders {
		rider := &v.Riders[index]
		if !rider.Completed && !slices.Contains(v.Stops, rider.To) {
			passengers += rider.PartySize
		}
	}
	return passengers
}

// stopUnloadingPhase estimates the unloading phase of a pod at a later stop,
// from the active parties that go to the stop.
func (v *vehicle) stopUnloadingPhase(stop string) int {
	if v.Pod.Class != ExpressClass {
		return unloadingTicks
	}
	passengers := 0
	for index := range v.Riders {
		rider := &v.Riders[index]
		if !rider.Completed && rider.To == stop {
			passengers += rider.PartySize
		}
	}
	return unloadingPhase(v.Pod.Class, passengers)
}
