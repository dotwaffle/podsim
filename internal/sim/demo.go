package sim

import (
	"errors"
	"fmt"
	"slices"
)

type demoRun struct{ secondSent, followupsSent bool }

const demoJourneys = 8

// StartDemo adds two parked pods for a fixed eight-journey experiment. Reset restores the original fleet.
func (s *Simulation) StartDemo() error {
	if len(s.initial) != 2 || !demoPlacement(s.initial[0], Placement{ID: "01", StationID: "harbor", BerthID: "harbor-1"}) || !demoPlacement(s.initial[1], Placement{ID: "02", StationID: "garden", BerthID: "garden-1"}) {
		return errors.New("the traffic demo needs pod 01 at Harbor and pod 02 at Garden")
	}
	if _, ok := s.network.Station("market"); !ok {
		return errors.New("the traffic demo needs Market")
	}
	if err := s.validateDemo(); err != nil {
		return err
	}
	parking, _ := s.network.Station("parking")
	if len(parking.Berths) < 2 {
		return errors.New("the traffic demo needs two parking berths")
	}
	placements := append(slices.Clone(s.initial), Placement{ID: "03", StationID: "parking", BerthID: parking.Berths[0].ID}, Placement{ID: "04", StationID: "parking", BerthID: parking.Berths[1].ID})
	candidate, err := NewFleet(s.network, placements)
	if err != nil {
		return fmt.Errorf("create demo fleet: %w", err)
	}
	if err := candidate.RequestJourney("01", "market"); err != nil {
		return err
	}
	candidate.initial = s.initial
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
		for _, trip := range [][2]string{{"harbor", "garden"}, {"garden", "harbor"}} {
			if err := s.RequestTrip(trip[0], trip[1]); err != nil {
				s.failDemo(err)
				return
			}
		}
	}
	if !d.followupsSent && s.completed >= 2 {
		for _, trip := range [][2]string{{"market", "harbor"}, {"market", "garden"}, {"harbor", "market"}, {"garden", "market"}} {
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
		from, _ := s.network.Station(pair[0])
		to, ok := s.network.Station(pair[1])
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

func demoPlacement(actual, expected Placement) bool {
	return actual.ID == expected.ID && actual.StationID == expected.StationID && (actual.BerthID == "" || actual.BerthID == expected.BerthID)
}
