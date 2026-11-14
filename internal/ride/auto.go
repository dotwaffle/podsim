package ride

import (
	"cmp"
	"math"

	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// NextPod returns the pod that auto mode rides after the pod current. It is
// the nearest other pod that carries riders. If there is no such pod, it is
// the nearest pod that a pending trip waits for. Ties go to the lowest pod
// ID. NextPod returns an empty string when there is no such pod, or when
// state has no pod current.
func NextPod(state session.State, current string) string {
	vehicles := state.Simulation.Vehicles
	var origin sim.Point
	found := false
	for _, vehicle := range vehicles {
		if vehicle.Pod.ID == current {
			origin, found = vehicle.Pod.Position, true
			break
		}
	}
	if !found {
		return ""
	}
	pickups := make(map[string]bool)
	for _, request := range state.Simulation.Pending {
		if request.PodID != "" {
			pickups[request.PodID] = true
		}
	}
	type choice struct {
		id       string
		distance float64
	}
	var loaded, pickup *choice
	better := func(best *choice, next choice) bool {
		return best == nil || next.distance < best.distance || next.distance == best.distance && cmp.Less(next.id, best.id)
	}
	for _, vehicle := range vehicles {
		pod := vehicle.Pod
		if pod.ID == current {
			continue
		}
		next := choice{pod.ID, math.Hypot(pod.Position.X-origin.X, pod.Position.Y-origin.Y)}
		switch {
		case carriesRiders(vehicle):
			if better(loaded, next) {
				loaded = &next
			}
		case pickups[pod.ID]:
			if better(pickup, next) {
				pickup = &next
			}
		}
	}
	if loaded != nil {
		return loaded.id
	}
	if pickup != nil {
		return pickup.id
	}
	return ""
}

func carriesRiders(vehicle sim.Vehicle) bool {
	for _, rider := range vehicle.Riders {
		if !rider.Completed {
			return true
		}
	}
	return false
}
