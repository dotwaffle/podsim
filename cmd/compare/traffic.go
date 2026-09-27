package main

import "github.com/dotwaffle/podsim/internal/sim"

// stoppedSpeed is the speed in m/s below which a pod with a wait reason
// counts as stopped. Dispatch uses the same limit.
const stoppedSpeed = 0.1

// trafficWaits holds the pod-seconds that pods spent stopped.
type trafficWaits struct {
	// stopped counts each stopped pod, for any wait reason.
	stopped int
	// junction and track count the stopped pods that wait for junction
	// traffic and for a pod ahead. The other stopped pods wait for a
	// berth or for parking.
	junction, track int
}

// sampleWaits adds the stopped pods of a snapshot, as one second each.
func (waits *trafficWaits) sampleWaits(vehicles []sim.Vehicle) {
	for _, vehicle := range vehicles {
		pod := vehicle.Pod
		if pod.WaitReason == sim.NoWait || pod.Speed >= stoppedSpeed {
			continue
		}
		waits.stopped++
		switch pod.WaitReason {
		case sim.JunctionOccupied:
			waits.junction++
		case sim.TrackOccupied:
			waits.track++
		case sim.NoWait, sim.BerthOccupied, sim.ParkingUnavailable:
		}
	}
}
