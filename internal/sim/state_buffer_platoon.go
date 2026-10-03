package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

var errBufferCertificate = errors.New("invalid fixed buffer certificate")

func hasBufferCertificate(state SavedState) bool {
	return hasCompactCertificate(state) || slices.ContainsFunc(state.Pods, func(p SavedPod) bool {
		return p.Platoon != nil && p.Platoon.Kind == "buffer"
	})
}

// checkRestoredBufferMembers rejects partial physical recovery of a certificate.
func checkRestoredBufferMembers(state SavedState, result RestoreResult) error {
	for _, pod := range state.Pods {
		if pod.CompactQueue != nil {
			for _, id := range pod.CompactQueue.Members {
				if slices.Contains(result.Demoted, id) {
					return fmt.Errorf("compact member %s lost its physical placement", id)
				}
			}
		}
		if pod.Platoon == nil || pod.Platoon.Kind != "buffer" {
			continue
		}
		if slices.Contains(result.Demoted, pod.ID) || slices.Contains(result.Demoted, pod.Platoon.Leader) {
			return fmt.Errorf("buffer member %s or its predecessor lost its physical placement", pod.ID)
		}
	}
	return nil
}

// checkBufferLinkFields preserves the earlier restore contracts.
func checkBufferLinkFields(input RestoreStateInput) error {
	for _, pod := range input.State.Pods {
		link := pod.Platoon
		if link == nil {
			continue
		}
		if link.Kind == "compact-buffer-v1" {
			continue
		}
		if link.Kind == "" && link.TerminalCell == nil {
			continue
		}
		if !input.BufferPlatoons || !input.StationBuffers || link.Kind != "buffer" || link.TerminalCell == nil || *link.TerminalCell < 0 || link.Lanes != 1 {
			return fmt.Errorf("pod %s: %w", pod.ID, errBufferCertificate)
		}
	}
	return nil
}

// checkSavedBufferLink validates the fixed endpoint without planning a new link.
func (r *physicalRestore) checkSavedBufferLink(index int) error {
	v, leader := &r.s.vehicles[index], &r.s.vehicles[r.leaders[index]-1]
	saved := r.state.Pods[index]
	link := saved.Platoon
	if !r.bufferPlatoons || !r.stationBuffers || !v.buffered || v.Route == nil || leader.Route == nil ||
		link.Lane < 0 || link.Lane != len(v.Route)-1 || link.LeaderLane < 0 || link.LeaderLane >= len(leader.Route) ||
		v.Route[link.Lane].ID != leader.Route[link.LeaderLane].ID || v.destinationStation != leader.destinationStation {
		return errors.New("the fixed buffer run is not on both member routes")
	}
	if err := r.checkBufferPose(index); err != nil {
		return err
	}
	if err := r.checkBufferPose(r.leaders[index] - 1); err != nil {
		return err
	}
	plan, ok := r.s.bufferPlan(v)
	if !ok || *link.TerminalCell != plan.frontier-plan.first {
		return errors.New("the terminal cell is not the buffer stopping frontier")
	}
	if math.IsNaN(link.Turn) || link.Turn < 0 || link.Turn > platoonMaxTurn ||
		r.s.runTurn(v.Route, link.Lane, link.Lane) > link.Turn+platoonTurnSlack {
		return errors.New("the fixed buffer turn is not valid")
	}
	if leader.buffered {
		if link.LeaderLane != len(leader.Route)-1 || r.state.Pods[r.leaders[index]-1].RouteIndex != link.LeaderLane {
			return errors.New("the buffered predecessor is not on its final certified entry")
		}
	} else {
		if !link.Draining || leader.destination.ID == "" || link.LeaderLane+1 >= len(leader.Route) {
			return errors.New("the discharged predecessor has no draining exclusive suffix")
		}
		prefix := *leader
		r.s.setVehicleRoute(&prefix, leader.Route[:link.LeaderLane+1])
		if !r.s.bufferSuffixValid(&prefix, leader.Route[link.LeaderLane+1:], leader.destination) {
			return errors.New("the discharged predecessor changes its certified prefix")
		}
	}
	if saved.RouteIndex != link.Lane || r.state.Pods[r.leaders[index]-1].RouteIndex < link.LeaderLane {
		return errors.New("a fixed buffer member is before its certified entry")
	}
	limit := v.Route[link.Lane].SpeedLimit
	if !r.s.oneSpeedLimit(v, link.Lane, limit) || !r.s.oneSpeedLimit(leader, link.LeaderLane, limit) {
		return errors.New("the fixed buffer routes have different speed limits")
	}
	certificate := platoonLink{buffer: true, terminalCell: *link.TerminalCell, first: plan.entryStop + 1, lane: link.Lane, lanes: 1}
	_, end := linkEnds(&v.blocks, certificate)
	if end < certificate.first {
		return errors.New("the fixed buffer run has no complete shareable cell")
	}
	if !r.s.bufferHasDischarge(v) {
		return errors.New("the fixed buffer follower has no exclusive discharge path")
	}
	return nil
}

// checkBufferPose rejects repairs that could discard certified physical state.
func (r *physicalRestore) checkBufferPose(index int) error {
	v, saved := &r.s.vehicles[index], r.state.Pods[index]
	if saved.RouteIndex < 0 || saved.RouteIndex >= len(v.Route) ||
		math.IsNaN(saved.LaneDistance) || math.IsInf(saved.LaneDistance, 0) ||
		math.IsNaN(saved.Distance) || math.IsInf(saved.Distance, 0) {
		return fmt.Errorf("buffer member %s has an invalid route position", saved.ID)
	}
	lane := v.Route[saved.RouteIndex]
	expected := v.blocks.lanes[saved.RouteIndex].start + saved.LaneDistance
	if saved.LaneDistance < -restoreTolerance || saved.LaneDistance > r.s.laneLength(lane)+restoreTolerance ||
		math.Abs(expected-saved.Distance) > restoreTolerance {
		return fmt.Errorf("buffer member %s has inconsistent lane and route distances", saved.ID)
	}
	return nil
}
