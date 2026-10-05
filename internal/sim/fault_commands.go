package sim

import "math"

// FaultRequest is the target of a fault command. PodID names the pod of a
// pod fault. LaneID names the lane of debris on the segment from
// FromMeters to ToMeters. Exactly one of PodID and LaneID is set, and the
// segment goes only with a lane. DurationSeconds is nil for a fault that
// lasts until a clear. Otherwise it is from 1 to 86,400 seconds.
type FaultRequest struct {
	PodID, LaneID        string
	FromMeters, ToMeters *float64
	DurationSeconds      *int64
}

// Fault starts the fault of request at a command boundary, and then runs
// the monitor once. It returns the fault ID. A refusal changes nothing,
// and its error text is the message of the fault command.
//
// The checks run in the order of the incident suspension contract: faults
// on, then the duration, then the target shape, then the preconditions of
// the pod fault or the debris. A request with both a pod and a lane, with
// neither, or with a segment for a pod is a target that is not supported.
// A lane request with no FromMeters or no ToMeters has an invalid
// segment. An unknown lane ID gets the lane error of the debris start.
func (s *Simulation) Fault(request FaultRequest) (string, error) {
	if !s.faultsOn {
		return "", errFaultsOff
	}
	var duration int64
	if seconds := request.DurationSeconds; seconds != nil {
		if *seconds < 1 || *seconds > maxFaultSeconds {
			return "", errFaultDuration
		}
		duration = *seconds
	}
	pod, lane := request.PodID != "", request.LaneID != ""
	if pod == lane || pod && (request.FromMeters != nil || request.ToMeters != nil) {
		return "", errFaultTarget
	}
	var id string
	var err error
	if pod {
		id, err = s.startPodFault(s.findVehicle(request.PodID), duration)
	} else {
		s.ensureNetworkIndexes()
		index, known := s.graph.lanes[request.LaneID]
		if !known {
			index = -1
		}
		id, err = s.startDebris(index, meters(request.FromMeters), meters(request.ToMeters), duration)
	}
	if err != nil {
		return "", err
	}
	s.observe()
	return id, nil
}

// meters returns the value of a segment bound, or NaN for a bound that
// the request does not have. The segment check refuses NaN.
func meters(bound *float64) float64 {
	if bound == nil {
		return math.NaN()
	}
	return *bound
}

// ClearFault ends the active fault with the ID at a command boundary, and
// then runs the monitor once. A refusal changes nothing, and its error
// text is the message of the clear command.
func (s *Simulation) ClearFault(id string) error {
	if err := s.clearFault(id); err != nil {
		return err
	}
	s.observe()
	return nil
}
