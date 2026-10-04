package sim

import (
	"errors"
	"fmt"
)

var errCouplingCheckpointWork = errors.New("coupling checkpoint routes exceed physical restore bounds")

func couplingWorkDenied(reason string) error {
	return fmt.Errorf("%s: %w: %w", reason, errCouplingCheckpointWork, errCouplingReservationDenied)
}

// Check full member routes before formation makes their original prefixes permanent.
func (s *Simulation) checkCouplingCheckpointWork(members [2]string) error {
	return s.checkCouplingCheckpointBatchWork([][2]string{members})
}

// Count every proposed pair's full routes in the same checkpoint budget.
func (s *Simulation) checkCouplingCheckpointBatchWork(pairs [][2]string) error {
	members := make(map[string]bool, 2*len(pairs))
	for _, pair := range pairs {
		for _, id := range pair {
			if id == "" || members[id] {
				return couplingWorkDenied("invalid proposed checkpoint members")
			}
			if v := s.findVehicle(id); v == nil || v.Pod.Activity != Traveling {
				return couplingWorkDenied("proposed checkpoint member is not traveling")
			}
			members[id] = true
		}
	}
	laneBlocks, budget := physicalBlockBudget(s)
	limits := newRouteLimits(s.network)
	cost := 0
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if !routed(v.Pod.Activity) {
			continue
		}
		route, err := s.couplingCheckpointRoute(v, members)
		if err != nil {
			return err
		}
		if len(route) == 0 || len(route) > limits.pod {
			return couplingWorkDenied("checkpoint pod route exceeds its lane limit")
		}
		for _, lane := range route {
			index, ok := s.graph.lanes[lane.ID]
			if !ok {
				return couplingWorkDenied("checkpoint route has an unknown lane")
			}
			if laneBlocks[index] > budget-cost {
				return couplingWorkDenied("checkpoint route blocks exceed the work budget")
			}
			cost += laneBlocks[index]
		}
	}
	for _, trip := range s.waiting {
		if len(trip.route) > limits.trip {
			return couplingWorkDenied("checkpoint waiting route exceeds its lane limit")
		}
		for _, lane := range trip.route {
			if _, ok := s.graph.lanes[lane.ID]; !ok {
				return couplingWorkDenied("checkpoint waiting route has an unknown lane")
			}
		}
		if len(trip.route) > budget-cost {
			return couplingWorkDenied("checkpoint waiting routes exceed the work budget")
		}
		cost += len(trip.route)
	}
	return nil
}

func (s *Simulation) couplingCheckpointRoute(v *vehicle, members map[string]bool) ([]Lane, error) {
	if v.Pod.Activity != Traveling {
		return v.Route, nil
	}
	if v.blocks.len() == 0 {
		return nil, couplingWorkDenied("traveling checkpoint pod has no route blocks")
	}
	if members[v.Pod.ID] || s.couplingMember(v.Pod.ID) {
		return v.Route, nil
	}
	start, _, _ := s.savedStart(v)
	if start < 0 || start >= len(v.Route) {
		return nil, couplingWorkDenied("checkpoint route has an invalid retained prefix")
	}
	return v.Route[start:], nil
}

// Committed saves cannot discard routes to make restoration fit its work budget.
func (r *physicalRestore) checkCouplingRestoreWork() error {
	if len(r.state.CouplingGroups) == 0 {
		return nil
	}
	limits := newRouteLimits(r.s.network)
	cost := 0
	for _, pod := range r.state.Pods {
		activity, ok := activityOfCode(pod.Activity)
		if !ok || !routed(activity) {
			continue
		}
		if len(pod.Route) == 0 || len(pod.Route) > limits.pod {
			return couplingWorkDenied("committed checkpoint pod route exceeds its lane limit")
		}
		for _, lane := range pod.Route {
			if lane < 0 || lane >= len(r.laneBlocks) {
				return couplingWorkDenied("committed checkpoint route has an unknown lane")
			}
			if r.laneBlocks[lane] > r.budget-cost {
				return couplingWorkDenied("committed checkpoint route blocks exceed the work budget")
			}
			cost += r.laneBlocks[lane]
		}
	}
	for _, trip := range r.state.Waiting {
		if len(trip.Route) > limits.trip {
			return couplingWorkDenied("committed checkpoint waiting route exceeds its lane limit")
		}
		for _, lane := range trip.Route {
			if lane < 0 || lane >= len(r.laneBlocks) {
				return couplingWorkDenied("committed checkpoint waiting route has an unknown lane")
			}
		}
		if len(trip.Route) > r.budget-cost {
			return couplingWorkDenied("committed checkpoint waiting routes exceed the work budget")
		}
		cost += len(trip.Route)
	}
	return nil
}
