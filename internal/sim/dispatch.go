package sim

import (
	"math"
	"slices"
	"strings"
)

type waitingTrip struct {
	request     Request
	route       []Lane
	destination Berth
	deferUntil  int64
	deferCheck  int64
	deferPodID  string
	// excludedPod is the pod that the trip must not get. releasePickups
	// sets it on a trip that never boarded, and a later release from
	// another pod replaces it. It ends when the trip leaves the queue. No
	// path binds the trip to the excluded pod or holds the trip for it.
	excludedPod string
	// boarded is false for a new order. It is true for a rider that a
	// restore queues again. The wait of such a trip is already recorded,
	// and its request keeps its BoardedTick.
	boarded bool
	// fullPodRefused is true after the seat screen counted a refusal of
	// the trip. See refusedByFullPod.
	fullPodRefused bool
	// joinEligibleAssigned and joinEligibleExistingStop are true after the
	// join census counted the trip in the member of the same name. See
	// recordJoinEligible.
	joinEligibleAssigned, joinEligibleExistingStop bool
}

// RequestTrip queues a passenger journey between stations and assigns an available pod when possible.
func (s *Simulation) RequestTrip(origin, destination string) error {
	_, err := s.SubmitTrip(origin, destination)
	return err
}

// SubmitTrip queues a journey and returns its request ID. An error returns zero.
func (s *Simulation) SubmitTrip(origin, destination string) (int, error) {
	return s.SubmitTripOptions(TripOptions{From: origin, To: destination})
}

// SubmitTripOptions accepts a whole party after checking certified capacity and paths.
func (s *Simulation) SubmitTripOptions(options TripOptions) (int, error) {
	defer s.observe()
	options, err := s.validateTripOptions(options)
	if err != nil {
		return 0, err
	}
	s.requestID++
	s.waiting = append(s.waiting, waitingTrip{request: requestFromOptions(options, s.requestID, s.tick)})
	s.dispatch()
	return s.requestID, nil
}

// dispatch considers requests in submission order. Unavailable pickups do not block other stations.
//
// Many waiting trips can start at the same station. The pass keeps the
// result of pickupPod for each station, so these trips do not repeat the
// same work. The pass also keeps the free pods and the pickup candidates,
// so localPickup, pickupAvailable and pickupPod do not read the full fleet
// for each trip. pickupPod reads the pods, the berth owners, the waiting
// trips, and assigned. The free pods depend only on the pods, and the
// pickup candidates depend only on the pods and assigned. After a change to
// one of them, the loop resets the pass before it reads the pass again.
// Thus each result is equal to the result of a new call. A dispatch reason
// and a deferral do not change what pickupPod reads. pickupPod also fills
// the route caches, but a cached route is equal to a new route. A trip that waitForFinishingPod holds until its
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
//
// With the join policy SharedRideJoinReassignExisting, a trip with an
// empty pod on its way can also join a boarding pod at its origin. The pod
// must already stop at the destination of the trip. Dispatch then releases
// the pod of the trip, as for a local pickup. Promotion runs first, so a
// trip that promotion gives an idle pod at the origin boards that pod. A
// trip whose pod is idle at the origin cannot join, because that pod is not
// releasable.
//
// While the seat screen is on, the pass also counts the trips with a pod on
// its way that a boarding pod at the origin could take. The count reads the
// pods and changes no decision. It runs before the join, so it also counts
// the trips that join. See recordJoinEligible.
func (s *Simulation) dispatch() {
	if s.pass == nil {
		s.pass = new(dispatchPass)
	}
	pass := s.pass
	pass.active = true
	defer pass.end()
	defer s.clearUnboundWaitingRoutes()
	pass.begin(s.waiting)
	for i := 0; i < len(s.waiting); {
		i = s.dispatchTrip(i, pass)
	}
	s.parkUnclaimedReleased()
}

// dispatchTrip dispatches the waiting trip at index i. It returns the index
// of the next trip: i when the trip leaves the queue, and i+1 when the trip
// stays.
func (s *Simulation) dispatchTrip(i int, pass *dispatchPass) int {
	if s.mayBeIdle(pass, s.waiting[i].request.legOrigin()) && s.promoteReadyPickup(i) {
		pass.reset()
	}
	trip := &s.waiting[i]
	previousReason := trip.request.DispatchReason
	trip.request.DispatchReason = ""
	v := s.keepTripPod(trip, pass)
	if v != nil && s.screensSeats() {
		s.recordJoinEligible(trip, v, pass)
	}
	if s.joinTrip(trip, v, pass) {
		s.waiting = slices.Delete(s.waiting, i, i+1)
		return i
	}
	v = s.takeLocalPickup(trip, v, pass)
	if v == nil {
		if s.keepHold(trip, pass) {
			return i + 1
		}
		var ok bool
		if v, ok = s.assignPickupPod(i, trip, pass); !ok {
			return i + 1
		}
	}
	return s.boardOrWait(i, trip, v, previousReason, pass)
}

// keepTripPod returns the pod of the trip, or nil when the trip has no pod.
// A pod that does not fit the trip, or that has no access to its pickup,
// loses the trip. A pod that fits the trip but has no access to its pickup
// loses it with no exclusion, and stays in service. The trip keeps its ID,
// queue position, deferral budget and exclusion.
func (s *Simulation) keepTripPod(trip *waitingTrip, pass *dispatchPass) *vehicle {
	v := s.findVehicle(trip.request.PodID)
	if v == nil {
		return nil
	}
	fits := s.podFitsRequest(v, trip.request)
	if fits && s.pickupAccess(v, trip.request) {
		return v
	}
	delete(pass.assigned, v.Pod.ID)
	s.releasePickup(v)
	trip.request.PodID = ""
	if fits || s.orderContract == ExpressOrderContract {
		trip.route = nil
		trip.destination = Berth{}
	}
	pass.reset()
	return nil
}

// joinTrip joins the trip to an onboard pickup or to a shared ride, and
// releases the pod v of the trip. Only a trip with no pod, or with a pod
// that the join policy can release, can join.
func (s *Simulation) joinTrip(trip *waitingTrip, v *vehicle, pass *dispatchPass) bool {
	if trip.request.PodID != "" && !s.reassigns(v) {
		return false
	}
	if !s.joinOnboardPickup(trip) && !s.joinSharedRide(trip, pass) {
		return false
	}
	pass.reset()
	if v != nil {
		delete(pass.assigned, v.Pod.ID)
		s.releasePickup(v)
	}
	return true
}

// takeLocalPickup gives the trip an idle pod at its pickup station in place
// of v, a pod on its way to the pickup, and releases v. It returns the pod
// of the trip.
func (s *Simulation) takeLocalPickup(trip *waitingTrip, v *vehicle, pass *dispatchPass) *vehicle {
	origin := trip.request.legOrigin()
	if v == nil || v.Pod.Activity == Idle && v.Pod.StationID == origin || !s.mayBeIdle(pass, origin) {
		return v
	}
	local := s.localPickupForRequest(trip.request, trip.excludedPod, pass)
	if local == nil {
		return v
	}
	pass.reset()
	delete(pass.assigned, v.Pod.ID)
	s.releasePickup(v)
	assignPickup(trip, local)
	trip.route, trip.destination = nil, Berth{}
	pass.assigned[local.Pod.ID] = true
	return local
}

// assignPickupPod assigns a pickup pod to a trip with no pod, and sends the
// pod to the pickup when it is away. It returns the pod of the trip, and
// false when the trip waits with no pod.
func (s *Simulation) assignPickupPod(i int, trip *waitingTrip, pass *dispatchPass) (*vehicle, bool) {
	v := s.cachedPickupPod(trip, pass)
	if v == nil {
		trip.request.DispatchReason = "Waiting for an available pod"
		if !s.hasFittingPod(trip.request) {
			trip.request.DispatchReason = "Waiting for a certified vehicle that fits this party and route"
		}
		return nil, false
	}
	if s.orderContract == ExpressOrderContract && trip.request.PodID != v.Pod.ID {
		trip.route, trip.destination = nil, Berth{}
	}
	away := v.Pod.StationID != trip.request.legOrigin() || v.Pod.Activity != Idle
	if away && s.waitForFinishingPod(trip, v, pass.assigned) {
		return nil, false
	}
	pass.reset()
	if away && !s.sendTripPickup(trip, v) {
		return nil, false
	}
	assignPickup(trip, v)
	pass.assigned[v.Pod.ID] = true
	if s.reassignPickup(i) {
		pass.begin(s.waiting)
		v = s.findVehicle(trip.request.PodID)
	}
	return v, true
}

// cachedPickupPod returns the pickup pod for the trip. The pass keeps the
// result for each dispatch key.
func (s *Simulation) cachedPickupPod(trip *waitingTrip, pass *dispatchPass) *vehicle {
	key := dispatchKey{options: trip.request.dispatchOptions(), excluded: trip.excludedPod}
	if v, known := pass.optionPickups[key]; known {
		return v
	}
	v := s.pickupPodForRequest(trip.request, trip.excludedPod, pass)
	if pass.optionPickups == nil {
		pass.optionPickups = make(map[dispatchKey]*vehicle)
	}
	pass.optionPickups[key] = v
	return v
}

// sendTripPickup sends the pod v to the pickup of the trip. The route of the
// trip then starts at the destination of v. It reports false when v has no
// access to the pickup.
func (s *Simulation) sendTripPickup(trip *waitingTrip, v *vehicle) bool {
	if err := s.sendPickupForRequest(v, trip.request); err != nil {
		trip.request.DispatchReason = "Waiting for pickup access"
		return false
	}
	trip.route = nil
	if v.destination.Node != "" {
		trip.route, _ = s.stationApproachRouteForClass(v.destination.Node, trip.request.To, v.Pod.Class)
	}
	trip.destination = Berth{}
	return true
}

// boardOrWait boards the trip at index i when its pod v is idle at the
// pickup station. Otherwise it sets the dispatch reason of the trip. It
// returns the index of the next trip.
func (s *Simulation) boardOrWait(i int, trip *waitingTrip, v *vehicle, previousReason string, pass *dispatchPass) int {
	if v.Pod.Activity != Idle || v.Pod.StationID != trip.request.legOrigin() {
		trip.request.DispatchReason = pickupReason(v, previousReason)
		return i + 1
	}
	pass.reset()
	if err := s.board(v, *trip); err != nil {
		trip.request.DispatchReason = "Waiting for destination access"
		return i + 1
	}
	s.waiting = slices.Delete(s.waiting, i, i+1)
	return i
}

// pickupReason returns the dispatch reason of a trip whose pod v is not
// ready at the pickup. previous is the last reason of the trip.
func pickupReason(v *vehicle, previous string) string {
	if v.RelocatingTo == "" {
		return "Waiting for destination access"
	}
	suffix := " traveling to pickup"
	if v.Pod.WaitReason != NoWait && v.Pod.Speed < 0.1 {
		suffix = " waiting in traffic"
	}
	return podReason(previous, v.Pod.ID, suffix)
}

// podReason returns "Pod " + podID + suffix. When previous has the same
// bytes, it returns previous and makes no new string. Most trips keep their
// reason from one pass to the next.
func podReason(previous, podID, suffix string) string {
	rest, ok := strings.CutPrefix(previous, "Pod ")
	if ok {
		rest, ok = strings.CutPrefix(rest, podID)
	}
	if ok && rest == suffix {
		return previous
	}
	return "Pod " + podID + suffix
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
	// active is true while dispatch runs. Service withdrawal refuses to run
	// during a pass. See withdrawService.
	active bool
	// optionPickups keeps the result of pickupPodForRequest for each key.
	optionPickups map[dispatchKey]*vehicle
	// assigned holds the pods of the waiting trips. dispatch changes it
	// during the pass.
	assigned map[string]bool
	// pickups keeps the result of pickupPod for each station.
	pickups map[string]*vehicle
	// free holds the free pods in fleet order when freeKnown is true. See
	// freePods.
	free      []*vehicle
	freeKnown bool
	// candidates holds the pods that pickupCandidate accepts, in fleet
	// order, when candidatesKnown is true. See pickupCandidates.
	candidates      []*vehicle
	candidatesKnown bool
	// idle is a filter over the stations with an idle pod when idleKnown is
	// true. See mayBeIdle.
	idle      stationFilter
	idleKnown bool
	// boarding holds the boarding pods by station when boardingKnown is
	// true. See boardingPods.
	boarding      map[string][]*vehicle
	boardingKnown bool
}

// dispatchKey selects a cached pickup pod. The pickup station is the leg
// origin, so the key holds dispatchOptions. Two trips with the same options
// and different exclusions can get different pods, so the key includes the
// exclusion.
type dispatchKey struct {
	options  TripOptions
	excluded string
}

// begin starts a pass for the waiting trips. It reuses the buffers of the
// last pass. Only map lookups read assigned and pickups, and the slices
// are filled again after each reset, so a reused buffer gives the same
// results as a new one.
func (pass *dispatchPass) begin(waiting []waitingTrip) {
	if pass.assigned == nil {
		pass.assigned = make(map[string]bool, len(waiting))
	}
	if pass.pickups == nil {
		pass.pickups = make(map[string]*vehicle)
	}
	clear(pass.assigned)
	for _, trip := range waiting {
		if trip.request.PodID != "" {
			pass.assigned[trip.request.PodID] = true
		}
	}
	pass.reset()
}

// end marks the end of the pass.
func (pass *dispatchPass) end() {
	pass.active = false
}

// reset removes the results of the pass.
func (pass *dispatchPass) reset() {
	clear(pass.pickups)
	clear(pass.optionPickups)
	pass.free, pass.freeKnown = pass.free[:0], false
	pass.candidates, pass.candidatesKnown = pass.candidates[:0], false
	pass.idleKnown = false
	pass.boardingKnown = false
}

// mayBeIdle reports false only when no pod is idle at the station. It
// computes the filter at the first call after a reset of the pass.
func (s *Simulation) mayBeIdle(pass *dispatchPass, stationID string) bool {
	if !pass.idleKnown {
		pass.idle, pass.idleKnown = s.idleStations(), true
	}
	return pass.idle.mayHave(stationID)
}

// freePods returns each pod in service that is idle or not occupied, in
// fleet order.
// Each other pod is not a local pickup, and pickupRouteWithAssignments
// rejects it. The free pods do not depend on assigned, so the callers read
// assigned for each pod. freePods finds the pods at the first call after a
// reset of the pass.
func (s *Simulation) freePods(pass *dispatchPass) []*vehicle {
	if !pass.freeKnown {
		for i := range s.vehicles {
			if v := &s.vehicles[i]; v.inService() && (v.Pod.Activity == Idle || !v.Pod.Occupied) {
				pass.free = append(pass.free, v)
			}
		}
		pass.freeKnown = true
	}
	return pass.free
}

// pickupCandidates returns each pod that pickupCandidate accepts with the
// assigned pods of the pass, in fleet order. Only these pods can be a
// pickup pod for a station. pickupCandidate reads only the pods, assigned
// and the fixed stations, so the result does not depend on the station.
// pickupCandidates finds the pods at the first call after a reset of the
// pass.
func (s *Simulation) pickupCandidates(pass *dispatchPass) []*vehicle {
	if !pass.candidatesKnown {
		for i := range s.vehicles {
			if v := &s.vehicles[i]; s.pickupCandidate(v, pass.assigned) {
				pass.candidates = append(pass.candidates, v)
			}
		}
		pass.candidatesKnown = true
	}
	return pass.candidates
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

// localPickupForRequest returns the first idle pod at the origin of the
// request that is not in assigned, fits the request, and has a route to its
// destination. It skips the pod excluded, which a released trip must not
// get.
func (s *Simulation) localPickupForRequest(request Request, excluded string, pass *dispatchPass) *vehicle {
	for _, v := range s.freePods(pass) {
		if v.Pod.Activity == Idle && v.Pod.StationID == request.legOrigin() && (excluded == "" || v.Pod.ID != excluded) && !pass.assigned[v.Pod.ID] && s.podFitsRequest(v, request) {
			station, _ := s.station(v.Pod.StationID)
			berth, _ := station.berth(v.Pod.BerthID)
			if _, err := s.stationApproachRouteForClass(berth.Node, request.To, v.Pod.Class); err != nil {
				continue
			}
			return v
		}
	}
	return nil
}

// pickupPod chooses the fastest available idle or divertible parking pod.
// Fleet order breaks ties.
// An idle pod at the pickup station has an estimate of zero, so it wins
// over each pod that must travel.
// It does not change the pods, the berth owners, or the waiting trips, so it
// computes each berth load one time for all pods.
//
// pickupRouteWithAssignments rejects each pod that pickupCandidate rejects,
// and it does not change the simulation when it does so. Thus pickupPod
// reads only the pickup candidates of the pass. They are in fleet order, so
// the full route search retains fleet-order ties. A reverse-search lower
// bound skips candidates that cannot improve the best pickup time. With
// no candidate, pickupPod does not compute the berth loads.
func (s *Simulation) pickupPod(stationID string, pass *dispatchPass) *vehicle {
	return s.pickupPodMatching(stationID, pass, nil, "")
}

// pickupPodForRequest chooses the pickup pod for a request as pickupPod
// does. It skips the pod excluded, which a released trip must not get.
func (s *Simulation) pickupPodForRequest(request Request, excluded string, pass *dispatchPass) *vehicle {
	return s.pickupPodMatching(request.legOrigin(), pass, &request, excluded)
}

func (s *Simulation) pickupPodMatching(stationID string, pass *dispatchPass, request *Request, excluded string) *vehicle {
	candidates := s.pickupCandidates(pass)
	if len(candidates) == 0 {
		return nil
	}
	var best *vehicle
	bestTime := math.Inf(1)
	load := s.berthLoads()
	var bounds []float64
	for _, v := range candidates {
		if excluded != "" && v.Pod.ID == excluded || request != nil && !s.podFitsRequest(v, *request) {
			continue
		}
		if best != nil {
			if bounds == nil {
				bounds = s.stationPickupBounds(stationID)
			}
			if pickupCannotImprove(s.pickupBound(v, bounds), bestTime) {
				continue
			}
		}
		var route []Lane
		var ok bool
		if request == nil {
			route, _, ok = s.candidateRoute(v, stationID, load)
		} else {
			route, _, ok = s.candidateRouteForRequest(v, *request, load)
		}
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

// board starts the boarding of a trip at pod v, which is idle at the origin.
// It refuses the pod that the trip excludes. The dispatch gates make that
// case unreachable, so this test is defensive.
func (s *Simulation) board(v *vehicle, trip waitingTrip) error {
	if trip.excludes(v.Pod.ID) || !s.podFitsRequest(v, trip.request) {
		return ErrPartyAdmission
	}
	from, _ := s.station(trip.request.legOrigin())
	origin, _ := from.berth(v.Pod.BerthID)
	route, err := s.legRoute(v, leg{origin: origin.Node, from: origin.Node, stops: []string{trip.request.To}})
	if err != nil {
		return err
	}
	trip.route, trip.destination = route, Berth{}
	v.Boardings, v.RiddenMeters = nil, 0
	v.Riders = []Request{s.boardingRider(trip, v, 0)}
	v.Stops = []string{trip.request.To}
	v.origin, v.destination, v.destinationStation = origin, trip.destination, trip.request.To
	v.journeyOrigin = origin
	s.setVehicleRoute(v, trip.route)
	v.Pod.Activity, v.Pod.WaitReason, v.Pod.BlockedBy = Boarding, NoWait, ""
	v.phaseTicks, v.blockIndex, v.reservedThrough = boardingTicks, 0, -1
	v.originReleased = false
	v.distance, v.riddenBase, v.pending = 0, 0, -1
	return nil
}

// boardingRider returns the rider of a trip that boards pod v now, and
// records the boarding of a new order. sharedWith is 0, or the ID of the
// first rider of the shared ride that the trip joins. A trip that a restore
// queued again keeps its recorded boarding.
func (s *Simulation) boardingRider(trip waitingTrip, v *vehicle, sharedWith int) Request {
	rider := trip.request
	rider.DispatchReason, rider.PodID = "", v.Pod.ID
	if !trip.boarded {
		rider.BoardedTick = s.tick
		s.recordBoarding(trip.request, sharedWith)
	}
	return rider
}

// joinSharedRide adds the party of a trip to the first pod in fleet order
// that boards at the origin of the trip and has room for one more party. In
// destination mode, the pod must go to the destination of the trip. In
// drop-offs mode, the pod must stop at the destination or be able to add
// it as a stop. See dropOffStops. A trip with a pod on its way can join
// only a pod that already stops at its destination, so the stops of the
// pod do not change. See reassigns.
//
// When a full pod could take the party and no pod takes it, the seat
// screen counts a refusal. See refusedByFullPod. The trip does not join the
// pod that it excludes.
func (s *Simulation) joinSharedRide(trip *waitingTrip, pass *dispatchPass) bool {
	if s.partyLimit(trip.request) <= 1 {
		return false
	}
	request := trip.request
	existingStop := request.PodID != ""
	refused := false
	for _, v := range s.boardingPods(pass)[request.legOrigin()] {
		if v.Pod.Occupied || len(v.Boardings) > 0 || trip.excludes(v.Pod.ID) {
			continue
		}
		if !s.canJoin(v, request) {
			refused = refused || s.refusedByFullPod(trip, v)
			continue
		}
		if s.sharedRideMode != SharedRideDropOffs {
			if v.destinationStation != request.To {
				continue
			}
		} else if existingStop {
			if !slices.Contains(v.Stops, request.To) {
				continue
			}
		} else if stops, ok := s.dropOffStops(v, request.To); !ok || !s.setBoardingStops(v, stops) {
			continue
		}
		if !trip.boarded {
			s.sharedParties++
		}
		v.Riders = append(v.Riders, s.boardingRider(*trip, v, v.Riders[0].ID))
		return true
	}
	if refused {
		trip.fullPodRefused = true
		s.seatScreen.FullPodRefusals++
	}
	return false
}

// reassigns reports whether a trip with the pod v on its way can join a
// boarding pod. This is so with the join policy
// SharedRideJoinReassignExisting when dispatch can release v.
func (s *Simulation) reassigns(v *vehicle) bool {
	return s.sharedRideJoin == SharedRideJoinReassignExisting && v != nil && releasable(v)
}

// boardingPods returns the pods in service that board a party, by station,
// in fleet order. It finds the pods at the first call after a reset of the
// pass. Only board makes a boarding pod during dispatch, and dispatch resets
// the pass after it.
func (s *Simulation) boardingPods(pass *dispatchPass) map[string][]*vehicle {
	if !pass.boardingKnown {
		clear(pass.boarding)
		if pass.boarding == nil {
			pass.boarding = make(map[string][]*vehicle)
		}
		for i := range s.vehicles {
			if v := &s.vehicles[i]; v.Pod.Activity == Boarding && len(v.Riders) > 0 && v.inService() {
				pass.boarding[v.Pod.StationID] = append(pass.boarding[v.Pod.StationID], v)
			}
		}
		pass.boardingKnown = true
	}
	return pass.boarding
}

// recordBoarding counts the wait of a request that boards now. sharedWith is
// 0, or the ID of the first request of the shared ride that it joins. A
// request with a pod ID that joins a shared ride had a pod on its way, so
// the seat screen counts a reassigned party.
func (s *Simulation) recordBoarding(request Request, sharedWith int) {
	wait := s.tick - request.RequestedTick
	s.boarded++
	s.totalWaitTicks += wait
	s.maxWaitTicks = max(s.maxWaitTicks, wait)
	if !s.recordExperiments {
		return
	}
	reassigned := sharedWith != 0 && request.PodID != ""
	if reassigned {
		s.seatScreen.ReassignedParties++
	}
	s.requestBoardings = append(s.requestBoardings, RequestTiming{
		RequestID: request.ID, RequestedTick: request.RequestedTick, BoardedTick: s.tick, SharedWith: sharedWith,
		Reassigned: reassigned,
	})
}

// promoteReadyPickup serves the oldest passenger first when pickup pods arrive out of order.
// Both pods retain the same pickup station; only their unboarded passenger orders swap.
// It reports whether it swapped the pods of two trips. Neither trip gets
// the pod that it excludes.
func (s *Simulation) promoteReadyPickup(index int) bool {
	trip := &s.waiting[index]
	current := s.findVehicle(trip.request.PodID)
	if current != nil && current.Pod.Activity == Idle && current.Pod.StationID == trip.request.legOrigin() {
		return false
	}
	for j := index + 1; j < len(s.waiting); j++ {
		later := &s.waiting[j]
		if later.request.legOrigin() != trip.request.legOrigin() {
			continue
		}
		ready := s.findVehicle(later.request.PodID)
		if ready == nil || !ready.inService() || trip.excludes(ready.Pod.ID) || ready.Pod.Activity != Idle || ready.Pod.StationID != trip.request.legOrigin() || !s.podFitsRequest(ready, trip.request) || !s.assignedPickupFitsRequest(ready, trip.request) {
			continue
		}
		if current != nil && (later.excludes(current.Pod.ID) || !s.podFitsRequest(current, later.request) || !s.assignedPickupFitsRequest(current, later.request)) {
			continue
		}
		previous := trip.request.PodID
		assignPickup(trip, ready)
		if current != nil {
			assignPickup(later, current)
		} else {
			// The trip had no pod, so the later trip has none now.
			later.request.PodID = previous
		}
		trip.route, later.route = nil, nil
		trip.destination, later.destination = Berth{}, Berth{}
		return true
	}
	return false
}

// assignPickup binds trip to v. Every write of a new pod binding goes
// through it. Its callers never pass the excluded pod of the trip. It keeps
// the exclusion and the deferral fields.
func assignPickup(trip *waitingTrip, v *vehicle) {
	trip.request.PodID = v.Pod.ID
}

// excludes reports whether the trip must not get the pod.
func (trip *waitingTrip) excludes(podID string) bool {
	return trip.excludedPod != "" && trip.excludedPod == podID
}
