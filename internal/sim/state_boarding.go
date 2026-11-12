package sim

import (
	"errors"
	"fmt"
)

// checkBoardingFields checks records before either restore tier can use them.
func checkBoardingFields(input RestoreStateInput) error {
	for _, pod := range input.State.Pods {
		if pod.Boardings == nil {
			if err := checkSavedBoardingsWithOrderContract(pod, input.OrderContract); err != nil {
				return fmt.Errorf("pod %s boarding records: %w", pod.ID, err)
			}
			continue
		}
		if err := input.State.checkPod(pod, input.Network); err != nil {
			return fmt.Errorf("pod %s boarding records: %w", pod.ID, err)
		}
		if err := checkBoardingBerths(input.Network, pod); err != nil {
			return fmt.Errorf("pod %s boarding records: %w", pod.ID, err)
		}
	}
	return nil
}

// checkSavedBoardingsWithOrderContract checks the aligned records and their
// passenger distance. A pod with riders needs one record for each rider.
func checkSavedBoardingsWithOrderContract(pod SavedPod, contract OrderContract) error {
	errMismatch := errors.New("boarding records do not match the stored riders")
	if pod.Boardings == nil {
		if pod.Activity == activityCode(Boarding) && pod.Occupied {
			return errors.New("occupied boarding needs boarding records")
		}
		if len(pod.Riders) > 0 {
			return errMismatch
		}
		return nil
	}
	if len(pod.Boardings) == 0 || len(pod.Boardings) > MaxStoredRidersForOrderContract(pod.Class, contract) || len(pod.Boardings) != len(pod.Riders) {
		return errMismatch
	}
	cumulative := pod.RiddenMeters
	if !finite(cumulative) || cumulative < 0 {
		return errors.New("passenger distance is negative or not finite")
	}
	active, _ := savedRiders(pod)
	for _, rider := range active {
		if len(pod.Riders) > 1 && rider.SharingConsent != SharedConsent {
			return errors.New("an active recorded rider needs shared consent")
		}
	}
	if len(active) > 0 && pod.Activity == activityCode(Traveling) && pod.Occupied {
		if !finite(pod.Distance) || pod.Distance < 0 {
			return errors.New("occupied route distance is negative or not finite")
		}
		cumulative += pod.Distance
		if !finite(cumulative) {
			return errors.New("cumulative passenger distance is not finite")
		}
	}
	for _, boarding := range pod.Boardings {
		if boarding.BerthID == "" || !finite(boarding.MetersAtBoarding) || boarding.MetersAtBoarding < 0 || boarding.MetersAtBoarding > cumulative {
			return errors.New("a boarding origin or distance is not valid")
		}
	}
	return nil
}

// checkBoardingBerths binds each record to its rider's passenger station.
func checkBoardingBerths(network Network, pod SavedPod) error {
	for index, boarding := range pod.Boardings {
		rider := pod.Riders[index]
		station, ok := network.Station(rider.legOrigin())
		if !ok || station.ParkingOnly {
			return fmt.Errorf("rider %d has no passenger boarding station", rider.ID)
		}
		berth, ok := station.berth(boarding.BerthID)
		if !ok || !berthAllows(station, berth, pod.Class) {
			return fmt.Errorf("rider %d has no compatible boarding berth at its origin", rider.ID)
		}
	}
	phase, err := phaseOf(pod)
	if err != nil {
		return err
	}
	rule := ruleForPod(pod, phase, network)
	if !rule.atBerth {
		return nil
	}
	station, ok := network.Station(pod.StationID)
	if !ok {
		return errors.New("the recorded pod has no station")
	}
	berth, ok := station.berth(pod.BerthID)
	if !ok || !berthAllows(station, berth, pod.Class) || rule.active && station.ParkingOnly && !refugePhase(phase) {
		return errors.New("the recorded pod is not at a compatible berth")
	}
	return nil
}

// refugePhase reports whether a pod with riders can be in phase at a berth
// of a parking station. A refuge can be a parking station: the pod holds
// there, and after a resume it continues from there. Each recorded rider
// still boarded at a passenger station.
func refugePhase(phase podPhase) bool {
	return phase == phaseRefugeHolding || phase == phaseContinuing
}
