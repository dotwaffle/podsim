package sim

import "testing"

// bankEntryCostNetwork returns a station with two banks. The way to bank a has 10
// seconds to another station, 30 seconds on the through lane of that station, and
// 8 seconds to its entry. The way to bank b has 20 seconds of external lanes. The
// way to bank a has the lower cost only if the through lane does not count.
func bankEntryCostNetwork() Network {
	n := Network{Nodes: []Node{
		{ID: "from", Position: Point{X: 0, Y: 0}},
		{ID: "x-entry", Position: Point{X: 100, Y: 0}},
		{ID: "x-exit", Position: Point{X: 400, Y: 0}},
		{ID: "x-arrival", Position: Point{X: 150, Y: -80}},
		{ID: "x-1", Position: Point{X: 250, Y: -120}},
		{ID: "x-departure", Position: Point{X: 350, Y: -80}},
		{ID: "mid", Position: Point{X: 440, Y: 0}},
		{ID: "a-entry", Position: Point{X: 480, Y: 0}},
		{ID: "a-arrival", Position: Point{X: 530, Y: 80}},
		{ID: "a-1", Position: Point{X: 630, Y: 120}},
		{ID: "a-departure", Position: Point{X: 730, Y: 80}},
		{ID: "a-exit", Position: Point{X: 780, Y: 0}},
		{ID: "b-entry", Position: Point{X: 0, Y: 200}},
		{ID: "b-arrival", Position: Point{X: 50, Y: 280}},
		{ID: "b-1", Position: Point{X: 150, Y: 320}},
		{ID: "b-departure", Position: Point{X: 250, Y: 280}},
		{ID: "b-exit", Position: Point{X: 300, Y: 200}},
	}}
	lane := func(id, from, to, station string, role StationLaneRole) {
		n.Lanes = append(n.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 10, StationID: station, StationRole: role})
	}
	lane("from-x", "from", "x-entry", "x", StationEntryRole)
	lane("from-b", "from", "b-entry", "t", StationEntryRole)
	lane("x-through", "x-entry", "x-exit", "x", StationThroughRole)
	lane("x-arrive", "x-entry", "x-arrival", "x", StationBerthAccessRole)
	lane("x-in", "x-arrival", "x-1", "x", StationBerthAccessRole)
	lane("x-out", "x-1", "x-departure", "x", StationDepartureRole)
	lane("x-depart", "x-departure", "x-exit", "x", StationDepartureRole)
	lane("x-mid", "x-exit", "mid", "x", StationExitRole)
	lane("mid-a", "mid", "a-entry", "t", StationEntryRole)
	for _, id := range []string{"a", "b"} {
		lane(id+"-through", id+"-entry", id+"-exit", "t", StationThroughRole)
		lane(id+"-arrive", id+"-entry", id+"-arrival", "t", StationBerthAccessRole)
		lane(id+"-in", id+"-arrival", id+"-1", "t", StationBerthAccessRole)
		lane(id+"-out", id+"-1", id+"-departure", "t", StationDepartureRole)
		lane(id+"-depart", id+"-departure", id+"-exit", "t", StationDepartureRole)
	}
	n.Stations = []Station{
		{ID: "x", Name: "x", Entry: "x-entry", Exit: "x-exit", Berths: []Berth{{ID: "x-1", Node: "x-1"}}, Banks: []StationBank{{ID: "x", Entry: "x-entry", Exit: "x-exit", BerthIDs: []string{"x-1"}}}},
		{ID: "t", Name: "t", Entry: "a-entry", Exit: "a-exit", Berths: []Berth{{ID: "a-1", Node: "a-1"}, {ID: "b-1", Node: "b-1"}}, Banks: []StationBank{
			{ID: "a", Entry: "a-entry", Exit: "a-exit", BerthIDs: []string{"a-1"}},
			{ID: "b", Entry: "b-entry", Exit: "b-exit", BerthIDs: []string{"b-1"}},
		}},
	}
	return n
}

// TestBankEntryCostCountsThroughLanes checks that only a departure lane of a bank
// leaves the approach cost. The through lane of another bank counts, and it
// reverses the choice between the banks.
func TestBankEntryCostCountsThroughLanes(t *testing.T) {
	t.Parallel()
	network := bankEntryCostNetwork()
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "x", BerthID: "x-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if owner := s.graph.banks.lanes[s.graph.lanes["x-through"]]; owner < 0 {
		t.Fatal("the through lane is not a lane of a bank")
	}
	station, _ := s.station("t")
	entry, err := s.stationBankEntryMatching("from", station, noBerthLoad, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if entry != "b-entry" {
		t.Fatalf("the entry is %s, want b-entry", entry)
	}
}
