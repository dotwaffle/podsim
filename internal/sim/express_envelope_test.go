package sim

import (
	"math"
	"testing"
)

// The rates of the Express ledger. A test states them, so that it does not
// read the production envelope.
const (
	expressAcceleration   = 1.3
	expressPlannedBraking = 0.8
	expressMaximumBraking = 1.15
)

// TestPlanCapBindsTowardRouteEnd checks the ordinary step with each rate apart.
func TestPlanCapBindsTowardRouteEnd(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{100}, []float64{14})
	dt := 1.0 / TicksPerSecond
	p, m := distinctEnvelope.plannedBraking, distinctEnvelope.maximumBraking
	const speed = 8.0
	end := v.blocks.end(v.blocks.len() - 1)
	command := func(distance float64) float64 {
		return ordinaryMoveStep(&v.blocks, distinctEnvelope, 0, distance, speed, end).commandedSpeed
	}
	requireSameBits(t, "far from the end", command(end-60), speed+distinctEnvelope.acceleration*dt)
	const near = 32.0
	root := math.Sqrt(p*p*dt*dt+2*p*near) - p*dt
	if root >= speed || root <= speed-m*dt {
		t.Fatalf("the fixture does not bind the root: %g", root)
	}
	requireSameBits(t, "planned root", command(end-near), root)
	requireSameBits(t, "floor at the maximum rate", command(end-10), speed-m*dt)
	// The reservation limit stays hard under the floor.
	const hard = 2.0
	safe := math.Sqrt(m*m*dt*dt+2*m*hard) - m*dt
	requireSameBits(t, "limit under the floor", ordinaryMoveStep(&v.blocks, distinctEnvelope, 0, end-10, speed, end-10+hard).commandedSpeed, safe)
}

// TestPlanCapSkipsFaultStep checks that a fault stop is the maximum rate up to
// its cap, also near the route end.
func TestPlanCapSkipsFaultStep(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{100}, []float64{14})
	end := v.blocks.end(v.blocks.len() - 1)
	const speed = 8.0
	requireSameBits(t, "fault step", faultMoveStep(&v.blocks, distinctEnvelope, 0, end-10, speed, end, end).commandedSpeed, speed)
}

// TestPlannedFloorBindsLaneScan checks that a lower limit nearer than the
// planned reach brakes the pod at the maximum rate.
func TestPlannedFloorBindsLaneScan(t *testing.T) {
	t.Parallel()
	_, v := laneSpeedFixture([]float64{100, 900}, []float64{14, 2})
	dt := 1.0 / TicksPerSecond
	const speed = 12.0
	floor := speed - distinctEnvelope.maximumBraking*dt
	requireSameBits(t, "ordinary", ordinaryMoveStep(&v.blocks, distinctEnvelope, 0, 90, speed, 1000).commandedSpeed, floor)
	requireSameBits(t, "fault", faultMoveStep(&v.blocks, distinctEnvelope, 0, 90, speed, 1000, 1000).commandedSpeed, floor)
}

// expressLedger records the rates of one pod over a run. It reads speeds from
// the state of the pod, and it does not read the envelope.
type expressLedger struct {
	rise, drop float64
	// plannedDrop excludes a drop to zero, which is the endpoint snap.
	plannedDrop float64
}

func (l *expressLedger) add(previous, speed float64) {
	l.rise = max(l.rise, speed-previous)
	l.drop = max(l.drop, previous-speed)
	if speed > 0 {
		l.plannedDrop = max(l.plannedDrop, previous-speed)
	}
}

// TestExpressClearApproachBrakesAtPlannedRate runs a journey with no other
// pod: to a berth, and to a berth past a lower limit.
func TestExpressClearApproachBrakesAtPlannedRate(t *testing.T) {
	t.Parallel()
	dt := 1.0 / TicksPerSecond
	for name, limit := range map[string]float64{"same limit": 14, "lower limit": 2.5} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			network := largeMotionNetwork(false)
			for index := range network.Lanes {
				if network.Lanes[index].ID == "a-link" {
					network.Lanes[index].SpeedLimit = limit
				}
			}
			s, err := expressPhysicalFleet(network, []Placement{{ID: "pod", Class: ExpressClass, StationID: "a", BerthID: "a-1"}})
			if err != nil {
				t.Fatal(err)
			}
			expressMotionRequest(t, s, "pod", "b")
			var ledger expressLedger
			var previous motionPodBuffer
			for range 300 * TicksPerSecond {
				before := previous.capture(s)
				s.Step()
				checkExpressMotionTick(t, s, before)
				ledger.add(before[0].Speed, s.vehicles[0].Pod.Speed)
				if s.completed == 1 {
					break
				}
			}
			if s.completed != 1 {
				t.Fatal("the journey did not finish")
			}
			if math.Abs(ledger.rise-expressAcceleration*dt) > 1e-9 {
				t.Fatalf("largest rise %g, want %g", ledger.rise, expressAcceleration*dt)
			}
			if ledger.plannedDrop > expressPlannedBraking*dt+1e-9 || ledger.plannedDrop < 0.9*expressPlannedBraking*dt {
				t.Fatalf("largest planned drop %g, want about %g", ledger.plannedDrop, expressPlannedBraking*dt)
			}
		})
	}
}

// TestGroupKeepsFullRates checks that the rates of Express do not reach a
// seat-only large class.
func TestGroupKeepsFullRates(t *testing.T) {
	t.Parallel()
	dt := 1.0 / TicksPerSecond
	s, err := expressPhysicalFleet(largeMotionNetwork(false), []Placement{{ID: "pod", Class: GroupClass, StationID: "a", BerthID: "a-1"}})
	if err != nil {
		t.Fatal(err)
	}
	expressMotionRequest(t, s, "pod", "b")
	var ledger expressLedger
	var previous motionPodBuffer
	for range 300 * TicksPerSecond {
		before := previous.capture(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
		ledger.add(before[0].Speed, s.vehicles[0].Pod.Speed)
		if s.completed == 1 {
			break
		}
	}
	if s.completed != 1 || math.Abs(ledger.rise-2*dt) > 1e-9 || ledger.plannedDrop < 1.9*dt {
		t.Fatalf("completed %d, largest rise %g, largest drop %g", s.completed, ledger.rise, ledger.plannedDrop)
	}
}

// TestExpressBlockedStopUsesMaximumRate stops a pod at speed behind a pod that
// holds the next cells. It checks that the unplanned stop uses the maximum
// rate and stays inside the reservation.
func TestExpressBlockedStopUsesMaximumRate(t *testing.T) {
	t.Parallel()
	dt := 1.0 / TicksPerSecond
	fleet := []Placement{{ID: "lead", Class: ExpressClass, StationID: "a", BerthID: "a-1"}, {ID: "follow", Class: ExpressClass, StationID: "a", BerthID: "a-2"}}
	s, err := expressPhysicalFleet(largeMotionNetwork(false), fleet)
	if err != nil {
		t.Fatal(err)
	}
	s.faultsOn = true
	s.faultSettings.evacuationSeconds = 3000
	expressMotionRequest(t, s, "lead", "b")
	lead, follow := s.findVehicle("lead"), s.findVehicle("follow")
	var previous motionPodBuffer
	var ledger expressLedger
	faulted := false
	for tick := range 240 * TicksPerSecond {
		if tick == 2*TicksPerSecond {
			expressMotionRequest(t, s, "follow", "b")
		}
		if !faulted && lead.Pod.LaneID == "a-link" && lead.Pod.LaneDistance > 180 {
			startFault(t, s, lead, 0)
			faulted = true
		}
		before := previous.capture(s)
		speed := follow.Pod.Speed
		s.Step()
		checkExpressMotionTick(t, s, before)
		if faulted && speed > 0 {
			ledger.add(speed, follow.Pod.Speed)
		}
		if faulted && lead.Pod.Speed == 0 && follow.Pod.Speed == 0 && follow.Pod.Activity == Traveling {
			break
		}
	}
	if !faulted || follow.Pod.Speed != 0 || follow.Pod.Activity != Traveling {
		t.Fatalf("the follower did not stop on the track: faulted %t, %+v", faulted, follow.Pod)
	}
	if ledger.plannedDrop > expressMaximumBraking*dt+1e-6 || ledger.plannedDrop < 1.1*expressPlannedBraking*dt {
		t.Fatalf("largest drop %g, want above the planned %g, up to the maximum %g", ledger.plannedDrop, expressPlannedBraking*dt, expressMaximumBraking*dt)
	}
}

// TestExpressFaultStopsAtMaximumRate checks the fault cap and the rate of a
// fault on Express at speed.
func TestExpressFaultStopsAtMaximumRate(t *testing.T) {
	t.Parallel()
	dt := 1.0 / TicksPerSecond
	s, err := expressPhysicalFleet(largeMotionNetwork(false), []Placement{{ID: "pod", Class: ExpressClass, StationID: "a", BerthID: "a-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.faultsOn = true
	s.faultSettings.evacuationSeconds = 3000
	expressMotionRequest(t, s, "pod", "c")
	v := s.findVehicle("pod")
	for range 120 * TicksPerSecond {
		s.Step()
		if v.Pod.LaneID == "a-link" && v.Pod.Speed == 14 {
			break
		}
	}
	if v.Pod.Speed != 14 {
		t.Fatalf("the pod did not reach its speed: %+v", v.Pod)
	}
	startFault(t, s, v, 0)
	want := v.distance + 14*14/(2*expressMaximumBraking)
	requireSameBits(t, "fault cap", v.faultCap, want)
	speed, largest := v.Pod.Speed, 0.0
	for tick := 0; v.Pod.Speed > 0; tick++ {
		if tick > 20*TicksPerSecond {
			t.Fatal("the pod does not come to rest")
		}
		s.Step()
		if v.Pod.Speed > 0 {
			largest = max(largest, speed-v.Pod.Speed)
		}
		speed = v.Pod.Speed
	}
	if largest > expressMaximumBraking*dt+1e-9 || largest < 0.95*expressMaximumBraking*dt {
		t.Fatalf("largest drop %g, want about %g", largest, expressMaximumBraking*dt)
	}
	if math.Abs(v.distance-want) > 1e-5 {
		t.Fatalf("the pod rests at %g, want %g", v.distance, want)
	}
}

// TestExpressDiversionStaysWithinMaximumRate diverts an empty moving pod to
// berths behind a lower limit, nearer than the planned rate can reach. The
// ledger of checkExpressMotionTick holds for each tick.
func TestExpressDiversionStaysWithinMaximumRate(t *testing.T) {
	t.Parallel()
	network := largeMotionNetwork(false)
	for index := range network.Lanes {
		switch network.Lanes[index].ID {
		case "b-1-in", "b-2-in":
			network.Lanes[index].SpeedLimit = 2.5
		}
	}
	s, err := expressPhysicalFleet(network, []Placement{{ID: "pod", Class: ExpressClass, StationID: "a", BerthID: "a-1"}})
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("pod")
	if !s.park(v) {
		t.Fatal("could not start the parking move")
	}
	var previous motionPodBuffer
	diverted := false
	for range 240 * TicksPerSecond {
		if !diverted && v.Pod.LaneID == "a-link" && v.Pod.Speed == 14 {
			laneEnd := v.distance - v.Pod.LaneDistance + s.laneLength(*v.blocks.currentLane(v.blockIndex))
			reservedEnd := v.blocks.end(v.reservedThrough)
			// The lane scan brakes for 2.5 m/s at the planned rate from
			// (14² - 2.5²)/(2 × 0.8) = 118.6 m.
			if reservedEnd <= laneEnd && laneEnd-v.distance < 118 {
				if err := s.RequestTrip("b", "c"); err != nil {
					t.Fatal(err)
				}
				if v.RelocatingTo != "b" || v.Route[len(v.Route)-1].To == "parking-1" {
					t.Fatalf("the pod did not divert: %q", v.RelocatingTo)
				}
				diverted = true
			}
		}
		before := previous.capture(s)
		s.Step()
		checkExpressMotionTick(t, s, before)
		if s.completed == 1 {
			break
		}
	}
	if !diverted || s.completed != 1 {
		t.Fatalf("diverted %t, completed %d", diverted, s.completed)
	}
}

// TestExpressEstimatesUseClass checks each estimate site that reads the class
// of a pod. The seat-only values on the same route differ.
func TestExpressEstimatesUseClass(t *testing.T) {
	t.Parallel()
	express := motionEnvelope{acceleration: expressAcceleration, plannedBraking: expressPlannedBraking, maximumBraking: expressMaximumBraking}
	s, err := expressPhysicalFleet(largeMotionNetwork(false), []Placement{{ID: "pod", Class: ExpressClass, StationID: "a", BerthID: "a-1"}})
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("pod")
	route, err := s.routeForClass("a-1", "b-1", ExpressClass)
	if err != nil {
		t.Fatal(err)
	}
	want := s.routeSeconds(express, route, motionEstimate{})
	if want == s.routeSeconds(seatEnvelope, route, motionEstimate{}) {
		t.Fatal("the fixture does not tell the classes apart")
	}
	requireSameBits(t, "cached route", s.withSeconds(routeResult{lanes: route}, ExpressClass).seconds, want)
	requireSameBits(t, "pickup", s.pickupSeconds(v, route), want)

	expressMotionRequest(t, s, "pod", "b")
	for range 60 * TicksPerSecond {
		s.Step()
		if v.Pod.Activity == Traveling && v.Pod.Speed > 5 && v.destination.Node != "" {
			break
		}
	}
	if v.Pod.Activity != Traveling || v.destination.Node == "" {
		t.Fatalf("the pod is not traveling: %+v", v.Pod)
	}
	_, got, ok := s.finishEstimate(v)
	if !ok {
		t.Fatal("no estimate for the traveling pod")
	}
	travel := s.routeSecondsWith(express, v.Route, v.routeLengths, motionEstimate{distance: v.distance, speed: v.Pod.Speed})
	requireSameBits(t, "finish estimate", got, travel+21)
}

// TestExpressLaterStopEstimateUsesClass checks the legs to later stops.
func TestExpressLaterStopEstimateUsesClass(t *testing.T) {
	t.Parallel()
	express := motionEnvelope{acceleration: expressAcceleration, plannedBraking: expressPlannedBraking, maximumBraking: expressMaximumBraking}
	s, err := expressPhysicalFleet(largeMotionNetwork(false), []Placement{{ID: "pod", Class: ExpressClass, StationID: "a", BerthID: "a-1"}})
	if err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("pod")
	v.Pod.Activity, v.Pod.Occupied = Unloading, true
	v.Stops = []string{"b"}
	_, got, ok := s.laterStops(v, "a-1", 0, v.Stops)
	if !ok {
		t.Fatal("no estimate")
	}
	route, err := s.stationApproachForStops("a-1", v.Stops, ExpressClass)
	if err != nil {
		t.Fatal(err)
	}
	station, _ := s.station("b")
	suffix, _, err := s.stationRouteByLoad(stationRouteInput{class: ExpressClass, from: station.routeEntry(route, Berth{}), station: "b", load: noBerthLoad, accept: s.berthFilterForStops(ExpressClass, nil)})
	if err != nil {
		t.Fatal(err)
	}
	want := s.routeSeconds(express, route, motionEstimate{}) + s.routeSeconds(express, suffix, motionEstimate{}) + float64(unloadingTicks)/TicksPerSecond
	requireSameBits(t, "later stop", got, want)
}

// TestExpressHoldUsesClass checks the estimate of the leg of a held pod. The
// hold decision differs between the rates of Express and the seat-only rates.
func TestExpressHoldUsesClass(t *testing.T) {
	t.Parallel()
	express := motionEnvelope{acceleration: expressAcceleration, plannedBraking: expressPlannedBraking, maximumBraking: expressMaximumBraking}
	fleet := []Placement{{ID: "idle", Class: ExpressClass, StationID: "parking", BerthID: "parking-1"}, {ID: "busy", Class: ExpressClass, StationID: "a", BerthID: "a-1"}}
	s, err := expressPhysicalFleet(largeMotionNetwork(false), fleet)
	if err != nil {
		t.Fatal(err)
	}
	idle, busy := s.findVehicle("idle"), s.findVehicle("busy")
	busy.Pod.Activity, busy.Pod.Occupied = Unloading, true
	busy.Riders = []Request{{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "c", To: "a", PartySize: 1, PodID: "busy"}}
	s.requestID = 1
	route, _, ok := s.pickupRouteWithAssignments(pickupRouteInput{pod: idle, station: "b", assigned: map[string]bool{}, accept: s.pickupBerthFilter(idle, Request{To: "c", From: "b", PartySize: 1})})
	if !ok {
		t.Fatal("no pickup route for the idle pod")
	}
	leg, _, err := s.stationRouteByLoad(stationRouteInput{class: ExpressClass, from: "a-1", station: "b", load: noBerthLoad, accept: s.pickupBerthFilter(busy, Request{To: "c", From: "b", PartySize: 1})})
	if err != nil {
		t.Fatal(err)
	}
	expressLeg, seatLeg := s.routeSeconds(express, leg, motionEstimate{}), s.routeSeconds(seatEnvelope, leg, motionEstimate{})
	best := s.pickupSeconds(idle, route) - pickupAdvantageSeconds
	// The busy pod wins with the seat-only leg and loses with its own.
	remaining := best - (expressLeg+seatLeg)/2
	if remaining <= 0 {
		t.Fatalf("the fixture has no room for the phase: %g", remaining)
	}
	busy.phaseTicks = int(remaining * TicksPerSecond)
	if err := s.RequestTrip("b", "c"); err != nil {
		t.Fatal(err)
	}
	if pending := s.Snapshot().Pending[0]; pending.PodID != "idle" {
		t.Fatalf("the trip held for a pod with a longer estimate: %+v", pending)
	}
}
