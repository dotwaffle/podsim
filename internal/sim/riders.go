package sim

import "slices"

// carriesPassengers reports whether a pod carries parties that have not
// arrived. After a journey, a pod keeps its riders with Completed true, so
// the riders alone do not tell.
func (v *vehicle) carriesPassengers() bool {
	return (v.Pod.Activity == Boarding || v.Pod.Occupied) && v.RidersAboard() > 0
}

// RidersAboard returns the number of riders that have not left the pod.
func (v *Vehicle) RidersAboard() int {
	aboard := 0
	for index := range v.Riders {
		if !v.Riders[index].Completed {
			aboard++
		}
	}
	return aboard
}

// PassengersAboard returns the passengers of the riders that have not left
// the pod.
func (v *Vehicle) PassengersAboard() int {
	passengers := 0
	for index := range v.Riders {
		if !v.Riders[index].Completed {
			passengers += v.Riders[index].PartySize
		}
	}
	return passengers
}

// boardingStation returns the origin station of the riders aboard a pod.
// All riders of a pod board at one station. It returns "" for a pod with no
// rider aboard.
func (v *vehicle) boardingStation() string {
	for _, rider := range v.Riders {
		if !rider.Completed {
			return rider.From
		}
	}
	return ""
}

// alight completes each rider of a pod that unloaded at a stop. A rider
// leaves the pod when the pod has no later stop for it.
func (s *Simulation) alight(v *vehicle) {
	for index := range v.Riders {
		rider := &v.Riders[index]
		if rider.Completed || slices.Contains(v.Stops, rider.To) {
			continue
		}
		rider.Completed = true
		s.completed++
		if s.recordExperiments {
			s.requestCompletions = append(s.requestCompletions, requestCompletion{requestID: rider.ID, tick: s.tick, riddenMeters: v.distance})
		}
	}
}
