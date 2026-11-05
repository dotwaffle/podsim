package sim

import "math"

const (
	// queueHeadwaySeconds is the time that each stopped pod in a queue adds
	// to the time that the queue needs to clear.
	queueHeadwaySeconds = 3.0
	// queueStoppedSpeed is the speed in m/s below which a waiting pod is in
	// a queue.
	queueStoppedSpeed = 0.1
)

// queueDischarge returns the time in seconds that the queue on each lane
// needs to clear, by lane index. A pod is in a queue when it travels with a
// wait reason and a speed below queueStoppedSpeed. Pod v does not count. It
// returns nil when no lane has a queue.
func (s *Simulation) queueDischarge(v *vehicle) []float64 {
	var discharge []float64
	for index := range s.vehicles {
		other := &s.vehicles[index]
		pod := &other.Pod
		if other == v || pod.Activity != Traveling || pod.WaitReason == NoWait || pod.Speed >= queueStoppedSpeed {
			continue
		}
		lane, ok := s.graph.lanes[pod.LaneID]
		if !ok {
			continue
		}
		if discharge == nil {
			discharge = make([]float64, len(s.network.Lanes))
		}
		discharge[lane] += queueHeadwaySeconds
	}
	return discharge
}

// queueDelay returns the part of the discharge time of a queue that remains
// when a pod gets to the start of its lane at time at.
func queueDelay(discharge, at float64) float64 {
	return math.Max(0, discharge-at)
}
