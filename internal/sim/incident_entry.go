package sim

import (
	"errors"
	"fmt"
)

// IncidentTestOperation is one stage 1 operation of IncidentForTest.
type IncidentTestOperation struct {
	// Kind is "withdraw", "restore", "destination", "unload", "resume", or
	// "evacuate".
	Kind string
	// Hold is the hold of withdraw and restore, and the owner of
	// destination and unload: 1 for a fault, 2 for an emergency.
	Hold uint8
	// Purpose, Station, and Berth are the target of destination: 1 for an
	// emergency unload, 2 for a refuge, 3 for an empty recovery.
	Purpose        uint8
	Station, Berth string
	// Interrupt marks the riders of destination and unload, by index in
	// Riders.
	Interrupt uint32
}

// IncidentForTest runs one stage 1 operation on pod podID, and then the
// monitor once: withdrawService, restoreService,
// setOperationalDestination, startOperationalUnload, resumeFromRefuge, or
// evacuate. Each operation checks its preconditions, and a refusal changes
// nothing.
//
// It is a test entry (incident contract, section 13). The session tests
// use it to save each stage 1 state. No production code calls it. It
// refuses a call during a dispatch pass.
func (s *Simulation) IncidentForTest(podID string, operation IncidentTestOperation) error {
	if s.pass != nil && s.pass.active {
		return errors.New("an incident operation during a dispatch pass")
	}
	v := s.findVehicle(podID)
	if v == nil {
		return fmt.Errorf("pod %s does not exist", podID)
	}
	hold := serviceHold(operation.Hold)
	var err error
	switch operation.Kind {
	case "withdraw":
		err = s.withdrawService(v, hold)
	case "restore":
		err = s.restoreService(v, hold)
	case "destination":
		err = s.setOperationalDestination(v, operationalTarget{
			purpose: opPurpose(operation.Purpose), owner: hold, interrupt: operation.Interrupt,
			station: operation.Station, berth: operation.Berth,
		})
	case "unload":
		err = s.startOperationalUnload(v, hold, operation.Interrupt)
	case "resume":
		err = s.resumeFromRefuge(v)
	case "evacuate":
		err = s.evacuate(v)
	default:
		return fmt.Errorf("unknown incident operation %q", operation.Kind)
	}
	if err != nil {
		return err
	}
	s.observe()
	return nil
}

// StopIncidentSamplersForTest sets the next tick of each live sampler to
// MaxCounter, so no new event starts. The samplers stay live, so their gated
// counters keep moving.
//
// It is a test entry for the soak of section 5.1.9 of the incident stage 7
// contract. No production code calls it.
func (s *Simulation) StopIncidentSamplersForTest() {
	if s.faultSampler.live {
		s.faultSampler.nextTick = MaxCounter
	}
	if s.emergencySampler.live {
		s.emergencySampler.nextTick = MaxCounter
	}
}

// CheckIncidentRecordsForTest runs the part of CheckContract that reads no
// export: the fault records and the emergency records.
//
// It is a test entry for the soak of section 5.1.9 of the incident stage 7
// contract. No production code calls it.
func (s *Simulation) CheckIncidentRecordsForTest() error {
	return s.checkIncidentRecords()
}

// CheckedExportForTest runs the part of CheckContract that checks the saved
// state, and it returns that state, so the soak can read it with no second
// export.
//
// It is a test entry for the soak of section 5.1.9 of the incident stage 7
// contract. No production code calls it.
func (s *Simulation) CheckedExportForTest() (SavedState, error) {
	return s.checkedExport()
}

// IncidentEvents holds the numbers that change at an incident event: a start
// or an end of a fault or an emergency, a sampler event, an evacuation, or a
// reroute. It leaves out the counters that move at each tick of a wait or a
// record. Two equal values mean that no such event came between them.
type IncidentEvents struct {
	Serial                                                  uint64
	Faults, Emergencies                                     int
	FaultStarted, FaultCleared, Evacuations, Reroutes       int64
	FaultSampled, FaultSkipped, FaultClearedTicks           int64
	EmergencyStarted, EmergencyEnded                        int64
	EmergencySampled, EmergencySkipped, EmergencyEndedTicks int64
	EmergencyMaxTicks                                       int64
}

// IncidentEventsForTest returns the numbers that change at an incident event.
// It does not allocate.
//
// It is a test entry for the check stride of amendment A10 of the incident
// stage 7 contract. No production code calls it.
func (s *Simulation) IncidentEventsForTest() IncidentEvents {
	f, e := &s.faultCounters, &s.emergencyCounters
	return IncidentEvents{
		Serial: s.incidentSerial, Faults: len(s.faults), Emergencies: len(s.emergencies),
		FaultStarted: f.started, FaultCleared: f.cleared, Evacuations: f.evacuations, Reroutes: f.reroutes,
		FaultSampled: f.sampled, FaultSkipped: f.skipped, FaultClearedTicks: f.clearedTicks,
		EmergencyStarted: e.started, EmergencyEnded: e.ended,
		EmergencySampled: e.sampled, EmergencySkipped: e.skipped, EmergencyEndedTicks: e.endedTicks,
		EmergencyMaxTicks: e.maxTicks,
	}
}

// BoardingLegBlockedForTest reports whether idle pod podID has a waiting trip
// whose leg route fails now and succeeds with an empty blocked set. It
// searches on a clone, so it changes no state, memo, counter, or route cache
// of s, and the clone starts with no cached route.
//
// It is a test entry for the Progress row of amendment A8 of the incident
// stage 7 contract, because the dispatch reason of a failed leg does not name
// the cause. No production code calls it.
func (s *Simulation) BoardingLegBlockedForTest(podID string) bool {
	v := s.findVehicle(podID)
	if v == nil || v.Pod.Activity != Idle {
		return false
	}
	next, ok := s.boardingLeg(v)
	if !ok {
		return false
	}
	c := s.Clone()
	v = c.findVehicle(podID)
	if _, err := c.legRoute(v, next); err == nil {
		return false
	}
	c.setBlocked(nil)
	_, err := c.legRoute(v, next)
	return err == nil
}

// FaultTransitionForTest returns a check of the transition rules of section
// 10 of the incident suspension contract. Each call compares each pod that is
// faulted before and after the time since the last call.
//
// It is a test entry for the soak of section 5.1.9 of the incident stage 7
// contract, because the session tests cannot read the cap or the grants. No
// production code calls it.
func (s *Simulation) FaultTransitionForTest() func() error {
	return s.faultTransitionCheck(func() []faultPose { return faultPoses(s) })
}

// RestoredFaultTransitionForTest is FaultTransitionForTest for a simulation
// that a physical restore made and that has not stepped. A restored pod keeps
// its saved lane distance, and its route distance is rebuilt on a shorter route,
// so the first publish can change the lane distance and the position by a
// rounding error. So until the first tick, the check reads the pose that the
// first publish gives. Each compare stays exact, and a change of the route
// distance in the first tick still fails.
//
// It is a test entry for the soak of section 5.1.9 of the incident stage 7
// contract. No production code calls it.
func (s *Simulation) RestoredFaultTransitionForTest() func() error {
	restored := s.tick
	return s.faultTransitionCheck(func() []faultPose {
		poses := faultPoses(s)
		if s.tick == restored {
			s.publishedPoses(poses)
		}
		return poses
	})
}

// faultTransitionCheck returns a check that compares the poses of each call
// with the poses of the call before it.
func (s *Simulation) faultTransitionCheck(read func() []faultPose) func() error {
	previous := read()
	return func() error {
		next := read()
		for index := range next {
			if previous[index].faulted && next[index].faulted {
				if err := faultTransition(previous[index], next[index]); err != nil {
					return fmt.Errorf("pod %s: %w: %+v, then %+v", s.vehicles[index].Pod.ID, err, previous[index], next[index])
				}
			}
		}
		previous = next
		return nil
	}
}

// publishedPoses gives each traveling pod with a lane the lane distance and the
// position that publishVehicleTravel computes from its route distance.
func (s *Simulation) publishedPoses(poses []faultPose) {
	for index := range poses {
		v := &s.vehicles[index]
		if v.Pod.Activity != Traveling || v.Pod.LaneID == "" {
			continue
		}
		blocks := &v.blocks
		lane := 0
		for lane+1 < len(blocks.route) && blocks.laneFirst(lane+1) <= v.blockIndex {
			lane++
		}
		poses[index].laneDistance = v.distance - blocks.lanes[lane].start
		poses[index].position = s.lanePosition(blocks.lanes[lane].geometry, &blocks.route[lane], poses[index].laneDistance)
	}
}

// faultPose is the state of a pod that the transition rules of section 10
// of the incident suspension contract compare.
type faultPose struct {
	faulted, traveling     bool
	faultCap, distance     float64
	speed                  float64
	lane, berth            string
	laneDistance           float64
	position               Point
	routeVersion           uint64
	reservedThrough        int
	riders, completedRider int
}

// faultPoses returns the pose of each pod.
func faultPoses(s *Simulation) []faultPose {
	poses := make([]faultPose, len(s.vehicles))
	for index := range s.vehicles {
		v := &s.vehicles[index]
		completed := 0
		for _, rider := range v.Riders {
			if rider.Completed {
				completed++
			}
		}
		poses[index] = faultPose{
			faulted: v.faulted, traveling: v.Pod.Activity == Traveling, faultCap: v.faultCap, distance: v.distance, speed: v.Pod.Speed,
			lane: v.Pod.LaneID, berth: v.Pod.BerthID, laneDistance: v.Pod.LaneDistance, position: v.Pod.Position,
			routeVersion: v.routeVersion, reservedThrough: v.reservedThrough, riders: len(v.Riders), completedRider: completed,
		}
	}
	return poses
}

// faultTransition checks the transition rules of a pod that is faulted
// before and after one tick or command (F3 and F4). The speed never rises,
// and the cap does not change. The grants do not grow. While the pod
// brakes, its distance does not fall and stays at most the cap. A pod at
// rest keeps its physical pose and speed 0, and its distance while its
// route stays. No rider completes.
func faultTransition(before, after faultPose) error {
	switch {
	case after.speed > before.speed:
		return errors.New("the speed rises")
	case after.faultCap != before.faultCap:
		return errors.New("the cap changes")
	case after.reservedThrough > before.reservedThrough:
		return errors.New("the grants grow")
	case after.completedRider > before.completedRider:
		return errors.New("a rider completes")
	}
	sameRoute := after.routeVersion == before.routeVersion
	if before.speed > 0 {
		if sameRoute && (after.distance < before.distance || after.distance > after.faultCap) {
			return errors.New("the braking pod leaves its distance range")
		}
		return nil
	}
	if after.speed != 0 || after.lane != before.lane || after.laneDistance != before.laneDistance || after.berth != before.berth || after.position != before.position {
		return errors.New("the pod at rest moves")
	}
	if sameRoute && after.distance != before.distance {
		return errors.New("the distance of the pod at rest changes")
	}
	return nil
}
