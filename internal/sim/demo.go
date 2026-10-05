package sim

import (
	"errors"
	"fmt"
	"slices"
)

type demoRun struct{ secondSent, followupsSent bool }

const demoJourneys = 8

// demoParkedPods are the pods that StartDemo adds. They stay after the demo
// ends, until Reset.
var demoParkedPods = [...]string{"03", "04"}

// demoMergeTrips are the trips that the demo orders with the journey of pod
// 02. demoFollowupTrips are the trips that it orders after two completions.
var (
	demoMergeTrips    = [...][2]string{{"harbor", "garden"}, {"garden", "harbor"}}
	demoFollowupTrips = [...][2]string{{"market", "harbor"}, {"market", "garden"}, {"harbor", "market"}, {"garden", "market"}}
)

// savedDemoOrders returns the number of orders that a saved demo still
// submits. stepDemo submits them without a queue check, so a restore must
// keep room for them.
func savedDemoOrders(demo *SavedDemo) int {
	if demo == nil {
		return 0
	}
	orders := 0
	if !demo.SecondSent {
		orders += 1 + len(demoMergeTrips)
	}
	if !demo.FollowupsSent {
		orders += len(demoFollowupTrips)
	}
	return orders
}

// StartDemo adds two parked pods for a fixed eight-journey experiment. Reset restores the original fleet.
func (s *Simulation) StartDemo() error {
	defer s.observe()
	if !isDemoFleet(s.initial) {
		return errors.New("the traffic demo needs pod 01 at Harbor and pod 02 at Garden")
	}
	if _, ok := s.station("market"); !ok {
		return errors.New("the traffic demo needs Market")
	}
	if err := s.validateDemo(); err != nil {
		return err
	}
	parking, _ := s.station("parking")
	if len(parking.Berths) < 2 {
		return errors.New("the traffic demo needs two parking berths")
	}
	placements := append(slices.Clone(s.initial), Placement{ID: demoParkedPods[0], StationID: "parking", BerthID: parking.Berths[0].ID}, Placement{ID: demoParkedPods[1], StationID: "parking", BerthID: parking.Berths[1].ID})
	candidate, err := NewFleet(s.network, placements)
	if err != nil {
		return fmt.Errorf("create demo fleet: %w", err)
	}
	if err := candidate.RequestJourney("01", "market"); err != nil {
		return err
	}
	candidate.initial, candidate.monitor = s.initial, s.monitor
	// The topology of the project keeps the incident marker, so the demo
	// fleet keeps it.
	candidate.incidentContract = s.incidentContract
	*s = *candidate
	s.demo = &demoRun{}
	return nil
}

func (s *Simulation) stepDemo() {
	if s.completed >= demoJourneys && !slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.RelocatingTo != "" }) {
		s.demo = nil
	}
	if s.demo == nil {
		return
	}
	d := s.demo
	first := s.findVehicle("01")
	// This supplied trigger brings both approaches to the merge together.
	if !d.secondSent && first.Pod.LaneID == "bypass-in" && first.Pod.LaneDistance >= 50 {
		if err := s.RequestJourney("02", "market"); err != nil {
			s.failDemo(err)
			return
		}
		d.secondSent = true
		for _, trip := range demoMergeTrips {
			if err := s.RequestTrip(trip[0], trip[1]); err != nil {
				s.failDemo(err)
				return
			}
		}
	}
	if !d.followupsSent && s.completed >= 2 {
		for _, trip := range demoFollowupTrips {
			if err := s.RequestTrip(trip[0], trip[1]); err != nil {
				s.failDemo(err)
				return
			}
		}
		d.followupsSent = true
	}
}

func (s *Simulation) validateDemo() error {
	for _, pair := range [][2]string{{"harbor", "market"}, {"garden", "market"}, {"market", "parking"}, {"parking", "harbor"}, {"parking", "garden"}, {"harbor", "garden"}, {"garden", "harbor"}, {"market", "harbor"}, {"market", "garden"}} {
		from, _ := s.station(pair[0])
		to, ok := s.station(pair[1])
		if !ok {
			return fmt.Errorf("traffic demo needs station %s", pair[1])
		}
		route, err := s.route(from.Berths[0].Node, to.Berths[0].Node)
		if err != nil {
			return fmt.Errorf("traffic demo route %s to %s: %w", pair[0], pair[1], err)
		}
		if pair == ([2]string{"harbor", "market"}) && !slices.ContainsFunc(route, func(lane Lane) bool { return lane.ID == "bypass-in" && s.network.Length(lane) > 50 }) {
			return errors.New("the traffic demo needs its bypass departure trigger")
		}
	}
	return nil
}

func (s *Simulation) failDemo(err error) {
	s.demoError = fmt.Sprintf("Traffic demo stopped: %v", err)
	s.demo = nil
}

// isDemoFleet reports whether a fleet sorted by pod ID is the fleet that the
// traffic demo needs.
func isDemoFleet(fleet []Placement) bool {
	return len(fleet) == 2 && demoPlacement(fleet[0], Placement{ID: "01", StationID: "harbor", BerthID: "harbor-1"}) &&
		demoPlacement(fleet[1], Placement{ID: "02", StationID: "garden", BerthID: "garden-1"})
}

func demoPlacement(actual, expected Placement) bool {
	return actual.ID == expected.ID && actual.StationID == expected.StationID && (actual.BerthID == "" || actual.BerthID == expected.BerthID)
}
