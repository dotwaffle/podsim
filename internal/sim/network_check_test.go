package sim

import (
	"errors"
	"slices"
	"testing"
)

// reachIsland adds three lanes to network that give a plane reach refusal:
// two paths on different planes that are less than 20 meters apart.
func reachIsland(t *testing.T, network Network) Network {
	t.Helper()
	classes, err := NewClassSet("group")
	if err != nil {
		t.Fatal(err)
	}
	network.Nodes, network.Lanes = slices.Clone(network.Nodes), slices.Clone(network.Lanes)
	network.Nodes = append(network.Nodes, []Node{
		{ID: "island-mid", Position: Point{X: 5000, Y: 100}}, {ID: "island-join", Position: Point{X: 5000}}, {ID: "island-east", Position: Point{X: 5400}},
		{ID: "island-cross-east", Position: Point{X: 5140, Y: 24}}, {ID: "island-cross-west", Position: Point{X: 4860, Y: 24}},
	}...)
	for _, row := range [][4]string{{"island-near", "island-mid", "island-join", "A"}, {"island-main", "island-join", "island-east", "Q"}, {"island-cross", "island-cross-east", "island-cross-west", "Q"}} {
		network.Lanes = append(network.Lanes, Lane{ID: row[0], From: row[1], To: row[2], SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: row[3]})
	}
	return network
}

func checkErrorText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// TestNetworkCheckMatchesValidation pins that a NetworkCheck gives the
// errors of the functions that it replaces, for valid networks and for
// networks with one and with two refusals, with and without the tails of a
// clearance index, and when a caller reads it more than one time.
func TestNetworkCheckMatchesValidation(t *testing.T) {
	t.Parallel()
	base := BankExample()
	short := base
	short.Nodes = append(slices.Clone(base.Nodes), Node{ID: "short-a", Position: Point{X: 9000}}, Node{ID: "short-b", Position: Point{X: 9003}})
	short.Lanes = append(slices.Clone(base.Lanes), Lane{ID: "short", From: "short-a", To: "short-b", SpeedLimit: 14})
	var bank StationBank
	for _, station := range base.Stations {
		if len(station.Banks) != 0 {
			bank = station.Banks[0]
		}
	}
	noThrough := base
	noThrough.Lanes = slices.DeleteFunc(slices.Clone(base.Lanes), func(lane Lane) bool { return lane.From == bank.Entry && lane.To == bank.Exit })
	fleet := []Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
	fleets := map[string][]Placement{"valid": fleet, "none": nil, "unknown station": {{ID: "01", StationID: "nowhere"}}}
	networks := map[string]Network{"valid": base, "short lane": short, "no through lane": noThrough, "reach": reachIsland(t, base), "reach and short lane": reachIsland(t, short), "empty": {}}
	var reach *PlaneReachError
	if err := ValidateNetwork(networks["reach"]); !errors.As(err, &reach) {
		t.Fatalf("the island gives %v, want a plane reach refusal", err)
	}
	if err := ValidateNetwork(short); err == nil {
		t.Fatal("the short lane has no refusal")
	}
	if err := noThrough.ValidateStationBanks(); err == nil {
		t.Fatal("the bank with no through lane has no refusal")
	}
	for name, network := range networks {
		for index, clearance := range []LaneClearance{{}, NewLaneClearance(network)} {
			check := NewNetworkCheck(network, clearance)
			if check.checked {
				t.Fatalf("%s: a new check is prepared", name)
			}
			for range 2 {
				if got, want := checkErrorText(check.StationBanksErr()), checkErrorText(network.ValidateStationBanks()); got != want {
					t.Errorf("%s, clearance %d: bank error = %s, want %s", name, index, got, want)
				}
				if got, want := checkErrorText(check.Err()), checkErrorText(ValidateNetworkClearance(network, clearance)); got != want {
					t.Errorf("%s, clearance %d: error = %s, want %s", name, index, got, want)
				}
				if got, want := checkErrorText(check.Err()), checkErrorText(ValidateNetwork(network)); got != want {
					t.Errorf("%s, clearance %d: error = %s, want the error %s of ValidateNetwork", name, index, got, want)
				}
				for fleetName, placements := range fleets {
					for _, contract := range []OrderContract{"", ExpressOrderContract, "unknown"} {
						if got, want := checkErrorText(check.ValidateFleet(placements, contract)), checkErrorText(ValidateFleetWithOrderContract(network, placements, contract)); got != want {
							t.Errorf("%s, clearance %d, fleet %s, contract %q: error = %s, want %s", name, index, fleetName, contract, got, want)
						}
					}
				}
			}
			if !check.checked {
				t.Fatalf("%s: a check that was read is not prepared", name)
			}
		}
	}
	// The refusal of a fleet comes before the plane reach refusal, and the
	// refusal of the network before the two.
	if got := checkErrorText(NewNetworkCheck(networks["reach"], LaneClearance{}).ValidateFleet(fleets["unknown station"], "")); got != `unknown start station "nowhere"` {
		t.Errorf("fleet and reach: error = %s", got)
	}
	if got := checkErrorText(NewNetworkCheck(networks["reach and short lane"], LaneClearance{}).ValidateFleet(fleets["unknown station"], "")); got != checkErrorText(ValidateNetwork(short)) {
		t.Errorf("short lane, fleet and reach: error = %s", got)
	}
}
