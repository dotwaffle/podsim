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
	// parties is 0 for a new order. A trip that a restore queues again holds
	// the party count of the pod that carried it. Its boarding is already
	// recorded.
	parties int
}

// partyCount returns the number of parties that the trip carries.
func (trip waitingTrip) partyCount() int { return max(1, trip.parties) }

// RequestTrip queues a passenger journey between stations and assigns an available pod when possible.
func (s *Simulation) RequestTrip(origin, destination string) error {
	from, ok := s.station(origin)
	if !ok || from.ParkingOnly {
		return errors.New("choose a passenger pickup station")
	}
	to, ok := s.station(destination)
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
//
// Many waiting trips can start at the same station. The pass keeps the
// result of pickupPod for each station, so these trips do not repeat the
// same work. The pass also keeps the free pods, so localPickup and
// pickupAvailable do not read the full fleet for each trip. pickupPod reads
// the pods, the berth owners, the waiting trips, and assigned. The free pods
// depend only on the pods. After a change to one of them, the loop resets
// the pass before it reads the pass again. Thus each result is equal to the
// result of a new call. A dispatch reason and a deferral do not change what
// pickupPod reads. pickupPod also fills the route caches, but a cached route
// is equal to a new route. A trip that waitForFinishingPod holds until its
// next check does not need pickupPod. See keepHold.
//
// Most waiting trips have a pod on its way to the pickup. For each such
// trip, promoteReadyPickup looks for a later trip with a pod that is idle at
// the pickup station, and localPickup looks for an idle pod at the pickup
// station. At a station with no idle pod, both scans find nothing and
// change nothing. The pass keeps a filter over the stations with an idle
// pod, and the loop does not do the scans at a station that the filter
// excludes. mayBeIdle computes the filter again at the first use after a
// reset of the pass. Thus the filter includes each station with an idle pod.
//
// When a local idle pod takes a trip from a pod on its way to the pickup,
// dispatch releases the other pod. The released pod can divert at once, so
// a later trip in the same pass can take it. After the pass, each released
// pod that holds no claim on its destination berth goes to the nearest free
// berth. This includes a pod that a restore released. See
// parkUnclaimedReleased.
func (s *Simulation) dispatch() {
	assigned := make(map[string]bool, len(s.waiting))
	for _, trip := range s.waiting {
		if trip.request.PodID != "" {
			assigned[trip.request.PodID] = true
		}
	}
	pass := dispatchPass{assigned: assigned, pickups: make(map[string]*vehicle)}
	for i := 0; i < len(s.waiting); {
		if s.mayBeIdle(&pass, s.waiting[i].request.From) && s.promoteReadyPickup(i) {
			pass.reset()
		}
		trip := &s.waiting[i]
		trip.request.DispatchReason = ""
		if trip.request.PodID == "" && s.joinSharedRide(*trip) {
			pass.reset()
			s.waiting = slices.Delete(s.waiting, i, i+1)
			continue
		}
		v := s.findVehicle(trip.request.PodID)
		if v != nil && (v.Pod.Activity != Idle || v.Pod.StationID != trip.request.From) && s.mayBeIdle(&pass, trip.request.From) {
			if local := s.localPickup(trip.request.From, &pass); local != nil {
				pass.reset()
				delete(assigned, v.Pod.ID)
				s.releasePickup(v)
				trip.request.PodID = local.Pod.ID
				trip.route, trip.destination = nil, Berth{}
				assigned[local.Pod.ID] = true
				v = local
			}
		}
		if v == nil && s.keepHold(trip, &pass) {
			i++
			continue
		}
		if v == nil {
			var known bool
			if v, known = pass.pickups[trip.request.From]; !known {
				v = s.pickupPod(trip.request.From, assigned)
				pass.pickups[trip.request.From] = v
			}
			if v == nil {
				trip.request.DispatchReason = "Waiting for an available pod"
				i++
				continue
			}
			away := v.Pod.StationID != trip.request.From || v.Pod.Activity != Idle
			if away && s.waitForFinishingPod(trip, v, assigned) {
				i++
				continue
			}
			pass.reset()
			if away {
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
			pass.reset()
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
	s.parkUnclaimedReleased()
}

// stationFilterBits is the number of bits in a stationFilter.
const stationFilterBits = 256

// stationFilter is a Bloom filter with one hash over a set of stations.
// mayHave reports true for each station in the set. It can also report true
// for a station that is not in the set.
type stationFilter [stationFilterBits / 64]uint64

func (f *stationFilter) add(stationID string) {
	bit := stationBit(stationID)
	f[bit/64] |= 1 << (bit % 64)
}

func (f *stationFilter) mayHave(stationID string) bool {
	bit := stationBit(stationID)
	return f[bit/64]&(1<<(bit%64)) != 0
}

// stationBit folds the 32-bit FNV-1a hash of a station ID to a bit of
// stationFilter.
func stationBit(stationID string) uint {
	hash := uint32(2166136261)
	for i := range len(stationID) {
		hash = (hash ^ uint32(stationID[i])) * 16777619
	}
	return uint(hash^hash>>8^hash>>16^hash>>24) % stationFilterBits
}

// idleStations returns a filter over the stations with an idle pod.
func (s *Simulation) idleStations() stationFilter {
	var idle stationFilter
	for i := range s.vehicles {
		if pod := &s.vehicles[i].Pod; pod.Activity == Idle {
			idle.add(pod.StationID)
		}
	}
	return idle
}

func (s *Simulation) assigned(podID string) bool {
	return slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.request.PodID == podID })
}

// dispatchPass keeps the results of reads that dispatch repeats for the
// waiting trips of one pass. After dispatch changes the pods, the berth
// owners, the waiting trips, or assigned, it calls reset before it reads
// the pass again.
type dispatchPass struct {
	// assigned holds the pods of the waiting trips. dispatch changes it
	// during the pass.
	assigned map[string]bool
	// pickups keeps the result of pickupPod for each station.
	pickups map[string]*vehicle
	// free holds the free pods in fleet order when freeKnown is true. See
	// freePods.
	free      []*vehicle
	freeKnown bool
	// idle is a filter over the stations with an idle pod when idleKnown is
	// true. See mayBeIdle.
	idle      stationFilter
	idleKnown bool
}

// reset removes the results of the pass.
func (pass *dispatchPass) reset() {
	clear(pass.pickups)
	pass.free, pass.freeKnown = pass.free[:0], false
	pass.idleKnown = false
}

// mayBeIdle reports false only when no pod is idle at the station. It
// computes the filter at the first call after a reset of the pass.
func (s *Simulation) mayBeIdle(pass *dispatchPass, stationID string) bool {
	if !pass.idleKnown {
		pass.idle, pass.idleKnown = s.idleStations(), true
	}
	return pass.idle.mayHave(stationID)
}

// freePods returns each pod that is idle or not occupied, in fleet order.
// Each other pod is not a local pickup, and pickupRouteWithAssignments
// rejects it. The free pods do not depend on assigned, so the callers read
// assigned for each pod. freePods finds the pods at the first call after a
// reset of the pass.
func (s *Simulation) freePods(pass *dispatchPass) []*vehicle {
	if !pass.freeKnown {
		for i := range s.vehicles {
			if v := &s.vehicles[i]; v.Pod.Activity == Idle || !v.Pod.Occupied {
				pass.free = append(pass.free, v)
			}
		}
		pass.freeKnown = true
	}
	return pass.free
}

// localPickup returns the first idle pod at the station that is not in
// assigned.
func (s *Simulation) localPickup(stationID string, pass *dispatchPass) *vehicle {
	for _, v := range s.freePods(pass) {
		if v.Pod.Activity == Idle && v.Pod.StationID == stationID && !pass.assigned[v.Pod.ID] {
			return v
		}
	}
	return nil
}

// pickupPod chooses the fastest available idle or divertible parking pod, with pod ID breaking ties.
// An idle pod at the pickup station has an estimate of zero, so it wins
// over each pod that must travel.
// It does not change the pods, the berth owners, or the waiting trips, so it
// computes each berth load one time for all pods.
func (s *Simulation) pickupPod(stationID string, assigned map[string]bool) *vehicle {
	var best *vehicle
	bestTime := math.Inf(1)
	load := s.berthLoads()
	for i := range s.vehicles {
		v := &s.vehicles[i]
		route, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: v, station: stationID, assigned: assigned, load: load})
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
	from, _ := s.station(trip.request.From)
	origin, _ := from.berth(v.Pod.BerthID)
	route, err := s.stationApproachRoute(origin.Node, trip.request.To)
	if err != nil {
		return err
	}
	trip.route, trip.destination = route, Berth{}
	request := trip.request
	request.DispatchReason = ""
	if trip.parties == 0 {
		s.recordBoarding(request)
	}
	request.PodID = v.Pod.ID
	v.Request = &request
	v.Parties = trip.partyCount()
	v.origin, v.destination, v.destinationStation = origin, trip.destination, trip.request.To
	s.setVehicleRoute(v, trip.route)
	v.Pod.Activity, v.Pod.WaitReason, v.Pod.BlockedBy = Boarding, NoWait, ""
	v.phaseTicks, v.blockIndex, v.reservedThrough = boardingTicks, 0, -1
	v.originReleased = false
	v.distance, v.pending = 0, -1
	return nil
}

// joinSharedRide adds the parties of a trip to a boarding pod with the same
// origin and destination. The pod must have room for all of them.
func (s *Simulation) joinSharedRide(trip waitingTrip) bool {
	if s.sharedRidePartyLimit <= 1 {
		return false
	}
	request, parties := trip.request, trip.partyCount()
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.Pod.Activity != Boarding || v.Pod.StationID != request.From || v.destinationStation != request.To ||
			v.Request == nil || v.Parties+parties > s.sharedRidePartyLimit {
			continue
		}
		if trip.parties == 0 {
			s.recordBoarding(request)
			s.sharedParties++
		}
		v.Parties += parties
		v.Request.PartySize += request.PartySize
		return true
	}
	return false
}

func (s *Simulation) recordBoarding(request Request) {
	wait := s.tick - request.RequestedTick
	s.boarded++
	s.totalWaitTicks += wait
	s.maxWaitTicks = max(s.maxWaitTicks, wait)
}

// promoteReadyPickup serves the oldest passenger first when pickup pods arrive out of order.
// Both pods retain the same pickup station; only their unboarded passenger orders swap.
// It reports whether it swapped the pods of two trips.
func (s *Simulation) promoteReadyPickup(index int) bool {
	trip := &s.waiting[index]
	current := s.findVehicle(trip.request.PodID)
	if current != nil && current.Pod.Activity == Idle && current.Pod.StationID == trip.request.From {
		return false
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
			return true
		}
	}
	return false
}
