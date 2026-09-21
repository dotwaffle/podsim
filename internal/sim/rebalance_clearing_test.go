package sim

import "testing"

func TestRebalancingArrivalClearsIdleBerth(t *testing.T) {
	t.Parallel()
	simulation, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "garden"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	arrival := simulation.findVehicle("01")
	station, _ := simulation.network.Station("market")
	// A passenger reached the berth after this empty move yielded its remote claim.
	if err := simulation.startEmptyMove(arrival, emptyDestination{station: station.ID, berth: station.Berths[0], rebalance: true}); err != nil {
		t.Fatal(err)
	}
	for range 300 * TicksPerSecond {
		simulation.Step()
		checkTraffic(t, simulation.Snapshot())
		if arrival.Pod.Activity == Idle && arrival.Pod.StationID == "market" && simulation.findVehicle("02").Pod.StationID == "parking" {
			if simulation.completed != 0 || simulation.requestID != 0 {
				t.Fatal("empty moves changed passenger counts")
			}
			return
		}
	}
	t.Fatal("rebalancing arrival did not clear the idle blocker")
}
