package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

type waitingTrip struct {
	request     Request
	route       []Lane
	destination Berth
	deferUntil  int64
	deferCheck  int64
	deferPodID  string
}

// RequestTrip queues a passenger journey between stations and assigns an available pod when possible.
func (s *Simulation) RequestTrip(origin, destination string) error {
	from, ok := s.network.Station(origin)
	if !ok || from.ParkingOnly {
		return errors.New("choose a passenger pickup station")
	}
	to, ok := s.network.Station(destination)
	if !ok || to.ParkingOnly {
		return errors.New("choose a passenger destination")
	}
	if from.ID == to.ID {
		return ErrSameStation
	}
	if !s.stationsConnected(from, to) {
		return fmt.Errorf("passenger route %s to %s: %w", origin, destination, ErrUnreachable)
	}
	s.requestID++
	s.waiting = append(s.waiting, waitingTrip{request: Request{ID: s.requestID, From: origin, To: destination, PartySize: 1, RequestedTick: s.tick}})
	s.dispatch()
	return nil
}

// dispatch considers requests in submission order. Unavailable pickups do not block other stations.
func (s *Simulation) dispatch() {
	assigned := make(map[string]bool, len(s.waiting))
	for _, trip := range s.waiting {
		if trip.request.PodID != "" {
			assigned[trip.request.PodID] = true
		}
	}
	for i := 0; i < len(s.waiting); {
		s.promoteReadyPickup(i)
		trip := &s.waiting[i]
		trip.request.DispatchReason = ""
		v := s.findVehicle(trip.request.PodID)
		if v != nil && (v.Pod.Activity != Idle || v.Pod.StationID != trip.request.From) {
			if local := s.localPickup(trip.request.From, assigned); local != nil {
				delete(assigned, v.Pod.ID)
				trip.request.PodID = local.Pod.ID
				trip.route, trip.destination = nil, Berth{}
				assigned[local.Pod.ID] = true
				v = local
			}
		}
		if v == nil {
			v = s.pickupPod(trip.request.From, assigned)
			if v == nil {
				trip.request.DispatchReason = "Waiting for an available pod"
				i++
				continue
			}
			if v.Pod.StationID != trip.request.From || v.Pod.Activity != Idle {
				if s.waitForFinishingPod(trip, v, assigned) {
					i++
					continue
				}
				if err := s.sendPickup(v, trip.request.From); err != nil {
					trip.request.DispatchReason = "Waiting for pickup access"
					i++
					continue
				}
				trip.route, _ = s.stationApproachRoute(v.destination.Node, trip.request.To)
				trip.destination = Berth{}
			}
			trip.request.PodID = v.Pod.ID
			assigned[v.Pod.ID] = true
		}
		if v.Pod.Activity == Idle && v.Pod.StationID == trip.request.From {
			if err := s.board(v, *trip); err != nil {
				trip.request.DispatchReason = "Waiting for destination access"
				i++
				continue
			}
			s.waiting = slices.Delete(s.waiting, i, i+1)
			continue
		}
		if v.RelocatingTo != "" {
			trip.request.DispatchReason = "Pod " + v.Pod.ID + " traveling to pickup"
			if v.Pod.WaitReason != NoWait && v.Pod.Speed < 0.1 {
				trip.request.DispatchReason = "Pod " + v.Pod.ID + " waiting in traffic"
			}
		} else {
			trip.request.DispatchReason = "Waiting for destination access"
		}
		i++
	}
}

func (s *Simulation) assigned(podID string) bool {
	return slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.PodID == podID })
}

func (s *Simulation) localPickup(stationID string, assigned map[string]bool) *vehicle {
	for i := range s.vehicles {
		v := &s.vehicles[i]
		if v.Pod.Activity == Idle && v.Pod.StationID == stationID && !assigned[v.Pod.ID] {
			return v
		}
	}
	return nil
}

// pickupPod chooses the fastest available idle or divertible parking pod, with pod ID breaking ties.
func (s *Simulation) pickupPod(stationID string, assigned map[string]bool) *vehicle {
	var best *vehicle
	bestTime := math.Inf(1)
	for i := range s.vehicles {
		v := &s.vehicles[i]
		route, _, ok := s.pickupRouteWithAssignments(v, stationID, assigned)
		if !ok {
			continue
		}
		travelTime := s.pickupSeconds(v, route)
		if travelTime < bestTime {
			best, bestTime = v, travelTime
		}
	}
	return best
}

func (s *Simulation) board(v *vehicle, trip waitingTrip) error {
	from, _ := s.network.Station(trip.request.From)
	origin, _ := from.berth(v.Pod.BerthID)
	route, err := s.stationApproachRoute(origin.Node, trip.request.To)
	if err != nil {
		return err
	}
	trip.route, trip.destination = route, Berth{}
	request := trip.request
	request.DispatchReason = ""
	wait := s.tick - request.RequestedTick
	s.boarded++
	s.totalWaitTicks += wait
	s.maxWaitTicks = max(s.maxWaitTicks, wait)
	request.PodID = v.Pod.ID
	v.Request = &request
	v.origin, v.destination, v.destinationStation = origin, trip.destination, trip.request.To
	v.Route, v.blocks = trip.route, s.routeBlocks(trip.route)
	v.Pod.Activity, v.Pod.WaitReason, v.Pod.BlockedBy = Boarding, NoWait, ""
	v.phaseTicks, v.blockIndex, v.reservedThrough = boardingTicks, 0, -1
	v.originReleased = false
	v.distance, v.pending = 0, -1
	return nil
}

// promoteReadyPickup serves the oldest passenger first when pickup pods arrive out of order.
// Both pods retain the same pickup station; only their unboarded passenger orders swap.
func (s *Simulation) promoteReadyPickup(index int) {
	trip := &s.waiting[index]
	current := s.findVehicle(trip.request.PodID)
	if current != nil && current.Pod.Activity == Idle && current.Pod.StationID == trip.request.From {
		return
	}
	for j := index + 1; j < len(s.waiting); j++ {
		later := &s.waiting[j]
		if later.request.From != trip.request.From {
			continue
		}
		ready := s.findVehicle(later.request.PodID)
		if ready != nil && ready.Pod.Activity == Idle && ready.Pod.StationID == trip.request.From {
			trip.request.PodID, later.request.PodID = later.request.PodID, trip.request.PodID
			trip.route, later.route = nil, nil
			trip.destination, later.destination = Berth{}, Berth{}
			return
		}
	}
}
