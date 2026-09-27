package sim

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// stationLaneRolesPath holds the station lane roles that
// inferStationLaneRoles gives for each network of stationLaneRoleCases.
// The editor test web/editor_test.cjs reads the file and compares the
// roles that the editor infers on import with it, so that the editor and
// the simulation give a legacy project the same station lanes.
const stationLaneRolesPath = "testdata/station_lane_roles.json"

// stationLaneRolesFile is the form of the file at stationLaneRolesPath.
type stationLaneRolesFile struct {
	Cases []stationLaneRolesCase `json:"cases"`
}

// stationLaneRolesCase holds a network without the inferred roles, and
// the station fields of each lane, in lane order, after the inference.
type stationLaneRolesCase struct {
	Name    string             `json:"name"`
	Network Network            `json:"network"`
	Lanes   []stationLaneRoles `json:"lanes"`
}

type stationLaneRoles struct {
	ID          string          `json:"ID"`
	StationID   string          `json:"StationID"`
	StationRole StationLaneRole `json:"StationRole"`
}

func roleNode(id string, x, y float64) Node { return Node{ID: id, Position: Point{X: x, Y: y}} }

func roleLane(id, from, to string) Lane { return Lane{ID: id, From: from, To: to, SpeedLimit: 14} }

// roleStation gives a station with a berth on each node in berthNodes.
func roleStation(id string, berthNodes ...string) Station {
	station := Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit"}
	for index, node := range berthNodes {
		station.Berths = append(station.Berths, Berth{ID: fmt.Sprintf("%s-berth-%d", id, index+1), Node: node})
	}
	return station
}

// stationLaneRoleCases gives the networks of the golden file. Each network
// has station lanes without StationID and StationRole, as a legacy or a
// hand-written import can give them.
func stationLaneRoleCases() []stationLaneRolesCase {
	// star has three berths with lanes directly from the entry and to the
	// exit. One lane is curved and one is slower. A road loop goes from the
	// exit back to the entry. A road lane also leaves the third berth. An
	// approach lane and a departure lane with a wrong explicit role keep
	// their explicit roles.
	approach := roleLane("s-approach", "j", "s-entry")
	approach.StationID, approach.StationRole = "s", StationApproachRole
	wrong := roleLane("s-out-3", "s-b3", "s-exit")
	wrong.StationID, wrong.StationRole = "s", StationExitRole
	curved := roleLane("s-in-2", "s-entry", "s-b2")
	curved.Control = &Point{X: 17.3, Y: 31.9}
	slow := roleLane("s-out-1", "s-b1", "s-exit")
	slow.SpeedLimit = 7.5
	star := Network{
		Nodes: []Node{
			roleNode("s-entry", 0, 0), roleNode("s-exit", 60, 0), roleNode("s-b1", 10, 20),
			roleNode("s-b2", 30, 20), roleNode("s-b3", 50, 20), roleNode("j", 30, -40),
		},
		Lanes: []Lane{
			roleLane("s-through", "s-entry", "s-exit"), roleLane("s-in-1", "s-entry", "s-b1"), slow,
			curved, roleLane("s-out-2", "s-b2", "s-exit"), roleLane("s-in-3", "s-entry", "s-b3"), wrong,
			roleLane("s-road", "s-exit", "j"), approach, roleLane("s-b3-road", "s-b3", "j"),
		},
		Stations: []Station{roleStation("s", "s-b1", "s-b2", "s-b3")},
	}

	// chain has two berth rows, as the generated London stations have.
	chain := Network{
		Nodes: []Node{
			roleNode("c-entry", 0, 0), roleNode("c-exit", 100, 0), roleNode("j", 50, -40),
			roleNode("c-01-arrival", 10, 20), roleNode("c-01-berth", 50, 20), roleNode("c-01-departure", 90, 20),
			roleNode("c-02-arrival", 10, 40), roleNode("c-02-berth", 50, 40), roleNode("c-02-departure", 90, 40),
		},
		Lanes: []Lane{
			roleLane("c-through", "c-entry", "c-exit"), roleLane("c-road-out", "c-exit", "j"), roleLane("c-road-in", "j", "c-entry"),
			roleLane("c-01-arrival-link", "c-entry", "c-01-arrival"), roleLane("c-01-departure-link", "c-01-departure", "c-exit"),
			roleLane("c-01-in", "c-01-arrival", "c-01-berth"), roleLane("c-01-out", "c-01-berth", "c-01-departure"),
			roleLane("c-02-arrival-link", "c-01-arrival", "c-02-arrival"), roleLane("c-02-departure-link", "c-02-departure", "c-01-departure"),
			roleLane("c-02-in", "c-02-arrival", "c-02-berth"), roleLane("c-02-out", "c-02-berth", "c-02-departure"),
		},
		Stations: []Station{roleStation("c", "c-01-berth", "c-02-berth")},
	}

	// ring has three stations on a road ring. At station r1, two routes
	// from the entry to the berth have the same cost. Node r1-k2 comes
	// before node r1-k1, so the route through r1-k2 wins, although the
	// lanes through r1-k1 come first.
	ring := Network{
		Nodes: []Node{
			roleNode("r1-entry", 0, 0), roleNode("r1-exit", 40, 0), roleNode("r1-k2", 13.7, 21.3),
			roleNode("r1-k1", -13.7, 21.3), roleNode("r1-b", 0, 42.6),
			roleNode("r2-entry", 200, 0), roleNode("r2-exit", 240, 0), roleNode("r2-b", 220, 20),
			roleNode("r3-entry", 140, 160), roleNode("r3-exit", 100, 160), roleNode("r3-b1", 130, 180), roleNode("r3-b2", 110, 180),
		},
		Lanes: []Lane{
			roleLane("r1-through", "r1-entry", "r1-exit"), roleLane("r1-in-a", "r1-entry", "r1-k1"), roleLane("r1-in-b", "r1-k1", "r1-b"),
			roleLane("r1-in-c", "r1-entry", "r1-k2"), roleLane("r1-in-d", "r1-k2", "r1-b"), roleLane("r1-out", "r1-b", "r1-exit"),
			roleLane("r2-through", "r2-entry", "r2-exit"), roleLane("r2-in", "r2-entry", "r2-b"), roleLane("r2-out", "r2-b", "r2-exit"),
			roleLane("r3-through", "r3-entry", "r3-exit"), roleLane("r3-in-1", "r3-entry", "r3-b1"), roleLane("r3-out-1", "r3-b1", "r3-exit"),
			roleLane("r3-in-2", "r3-entry", "r3-b2"), roleLane("r3-out-2", "r3-b2", "r3-exit"),
			roleLane("ring-1", "r1-exit", "r2-entry"), roleLane("ring-2", "r2-exit", "r3-entry"), roleLane("ring-3", "r3-exit", "r1-entry"),
		},
		Stations: []Station{roleStation("r1", "r1-b"), roleStation("r2", "r2-b"), roleStation("r3", "r3-b1", "r3-b2")},
	}

	// forbidden has two stations side by side. The shortest path from the
	// p entry to the p berth goes through the q berth node, so the route
	// takes the longer path through junction k. The lanes through the q
	// berth node stay road lanes.
	forbidden := Network{
		Nodes: []Node{
			roleNode("p-entry", 0, 0), roleNode("p-exit", 100, -30), roleNode("p-b", 100, 0), roleNode("k", 50, 40),
			roleNode("q-entry", 30, -30), roleNode("q-exit", 70, -30), roleNode("q-b", 50, 0),
		},
		Lanes: []Lane{
			roleLane("p-through", "p-entry", "p-exit"), roleLane("p-short-1", "p-entry", "q-b"), roleLane("p-short-2", "q-b", "p-b"),
			roleLane("p-in-1", "p-entry", "k"), roleLane("p-in-2", "k", "p-b"), roleLane("p-out", "p-b", "p-exit"),
			roleLane("q-through", "q-entry", "q-exit"), roleLane("q-in", "q-entry", "q-b"), roleLane("q-out", "q-b", "q-exit"),
		},
		Stations: []Station{roleStation("p", "p-b"), roleStation("q", "q-b")},
	}

	// shared has a lane on the departure route of the first berth and on
	// the arrival route of the second berth. The first route wins, so the
	// lane gets the departure role. The lanes of a shared junction in
	// front of the two stations go to the first station.
	shared := Network{
		Nodes: []Node{
			roleNode("w-entry", 0, 0), roleNode("w-exit", 100, 0), roleNode("w-b1", 20, 30), roleNode("w-b2", 80, 30),
			roleNode("m", 40, 50), roleNode("x", 60, 50),
			roleNode("v-entry", 0, 200), roleNode("v-exit", 100, 200), roleNode("u-entry", 0, 100), roleNode("u-exit", 100, 100),
			roleNode("u-b", 70, 130), roleNode("v-b", 70, 170), roleNode("g", 30, 150), roleNode("h", 50, 150),
		},
		Lanes: []Lane{
			roleLane("w-through", "w-entry", "w-exit"), roleLane("w-in-1", "w-entry", "w-b1"), roleLane("w-out-1", "w-b1", "m"),
			roleLane("w-mx", "m", "x"), roleLane("w-x-exit", "x", "w-exit"), roleLane("w-entry-m", "w-entry", "m"),
			roleLane("w-in-2", "x", "w-b2"), roleLane("w-out-2", "w-b2", "w-exit"),
			roleLane("v-through", "v-entry", "v-exit"), roleLane("u-through", "u-entry", "u-exit"),
			roleLane("v-g", "v-entry", "g"), roleLane("u-g", "u-entry", "g"), roleLane("gh", "g", "h"),
			roleLane("h-u", "h", "u-b"), roleLane("h-v", "h", "v-b"),
			roleLane("u-out", "u-b", "u-exit"), roleLane("v-out", "v-b", "v-exit"),
		},
		Stations: []Station{roleStation("w", "w-b1", "w-b2"), roleStation("u", "u-b"), roleStation("v", "v-b")},
	}

	// unreachable has a berth with no lane in. The network is not valid.
	// The inference still gives the departure lane a role.
	unreachable := Network{
		Nodes:    []Node{roleNode("n-entry", 0, 0), roleNode("n-exit", 40, 0), roleNode("n-b", 20, 20)},
		Lanes:    []Lane{roleLane("n-through", "n-entry", "n-exit"), roleLane("n-out", "n-b", "n-exit")},
		Stations: []Station{roleStation("n", "n-b")},
	}

	return []stationLaneRolesCase{
		{Name: "star", Network: star}, {Name: "chain", Network: chain}, {Name: "ring", Network: ring},
		{Name: "forbidden", Network: forbidden}, {Name: "shared", Network: shared}, {Name: "unreachable", Network: unreachable},
	}
}

// inferredStationLaneRoles gives the station fields of each lane after
// inferStationLaneRoles.
func inferredStationLaneRoles(network Network) []stationLaneRoles {
	owned := network.clone()
	inferStationLaneRoles(&owned)
	roles := make([]stationLaneRoles, len(owned.Lanes))
	for index, lane := range owned.Lanes {
		roles[index] = stationLaneRoles{ID: lane.ID, StationID: lane.StationID, StationRole: lane.StationRole}
	}
	return roles
}

// TestStationLaneRolesGolden fixes the station lane roles that the editor
// test compares with. Run the test with -update to write the file again.
func TestStationLaneRolesGolden(t *testing.T) {
	t.Parallel()
	cases := stationLaneRoleCases()
	if *update {
		written := stationLaneRolesFile{Cases: cases}
		for index := range written.Cases {
			written.Cases[index].Lanes = inferredStationLaneRoles(written.Cases[index].Network)
		}
		data, err := json.Marshal(written, json.Deterministic(true), jsontext.WithIndent("  "))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stationLaneRolesPath, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(stationLaneRolesPath)
	if err != nil {
		t.Fatal(err)
	}
	var file stationLaneRolesFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) != len(cases) {
		t.Fatalf("the golden file has %d cases, want %d", len(file.Cases), len(cases))
	}
	for index, want := range cases {
		got := file.Cases[index]
		if got.Name != want.Name || !reflect.DeepEqual(got.Network, want.Network) {
			t.Fatalf("golden case %d differs from case %s: %+v", index, want.Name, got)
		}
		if roles := inferredStationLaneRoles(want.Network); !reflect.DeepEqual(got.Lanes, roles) {
			t.Fatalf("case %s: golden roles %+v, inferred %+v", want.Name, got.Lanes, roles)
		}
		// The inference keeps the validation result. Only the unreachable
		// case is not valid.
		inferred := want.Network.clone()
		inferStationLaneRoles(&inferred)
		before, after := fmt.Sprint(want.Network.validate()), fmt.Sprint(inferred.validate())
		if before != after || (before == "<nil>") == (want.Name == "unreachable") {
			t.Fatalf("case %s: validation %s before the inference and %s after it", want.Name, before, after)
		}
	}
}
