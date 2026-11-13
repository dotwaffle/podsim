package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// edit sends a geometry edit to the engine. When the engine accepts it, edit
// synchronizes the bytes of the reply, as the browser does, and commits them
// to the history. It reports whether the engine accepted the edit.
func (k *keptEngine) edit(geometry string) bool {
	k.t.Helper()
	return k.change(`{"field":"geometry","value":` + geometry + `}`)
}

// change is edit for any project edit. The reply of the engine must be the
// reply of the stateless handler, which keeps no result, for the project of
// the engine.
func (k *keptEngine) change(edit string) bool {
	k.t.Helper()
	stateless := Call(`{"op":"edit","project":` + k.project() + `,"edit":` + edit + `}`)
	text := ask(k.model, `{"op":"edit","edit":`+edit+`}`)
	var got, want any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		k.t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(stateless), &want); err != nil {
		k.t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		k.t.Fatalf("the edit %s differs from the stateless handler:\n got %.400s\nwant %.400s", edit, text, stateless)
	}
	var reply cycleReply
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		k.t.Fatal(err)
	}
	if reply.Error != "" || len(reply.Change.Patch) == 0 {
		return false
	}
	raw, err := json.Marshal(reply.Change.Patch)
	if err != nil {
		k.t.Fatal(err)
	}
	k.patchRaw(raw)
	k.history(`"kind":"replace","background":null`)
	return true
}

// history prepares and accepts one history command.
func (k *keptEngine) history(command string) {
	k.t.Helper()
	revision := "0"
	if k.model.timeline != nil {
		revision = strconv.FormatUint(k.model.timeline.revision, 10)
	}
	var prepared cycleReply
	if err := json.Unmarshal([]byte(ask(k.model, `{"op":"history","history":{"action":"prepare","revision":"`+revision+`",`+command+`}}`)), &prepared); err != nil || prepared.Error != "" {
		k.t.Fatalf("history %s: %v %s", command, err, prepared.Error)
	}
	if reply := ask(k.model, `{"op":"history","history":{"action":"accept","token":"`+prepared.History.Proposal+`"}}`); strings.Contains(reply, `"error"`) {
		k.t.Fatalf("history accept: %s", reply)
	}
}

// network returns the network of the engine as the project decoder reads it.
func (k *keptEngine) network() sim.Network {
	return k.model.config.Network
}

// keptFixtures returns the projects of the differential test: the default
// project with a profile, the bank example, a project with depots, the
// compact station, and, without -short, the LondonFull Express preset.
func keptFixtures(t *testing.T) map[string]project.Config {
	t.Helper()
	small := project.Default()
	profile, demand, err := makeParkRide(small, validPlan())
	if err != nil {
		t.Fatal(err)
	}
	small.DemandProfiles, small.Demand = []project.DemandProfile{profile}, demand
	depot, err := scenarios.PresetWith("small", func(parameters *scenarios.Parameters) {
		parameters.ParkingLanes, parameters.ParkingBerths, parameters.InitialParkingPods = 2, 3, 4
	})
	if err != nil {
		t.Fatal(err)
	}
	compact, err := scenarios.CompactStation()
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]project.Config{"small": small, "banks": bankEditorConfig(), "depot": depot, "compact": compact}
	if !testing.Short() {
		fixtures["london-full-express"] = scenarios.LondonFullExpress()
	}
	return fixtures
}

// plainNodes returns the IDs of the nodes of network that no station, berth
// or station lane reads, and the largest X coordinate of the network.
func plainNodes(network sim.Network) (ids []string, right float64) {
	fixed := make(map[string]bool)
	for _, station := range network.Stations {
		fixed[station.Entry], fixed[station.Exit] = true, true
		for _, berth := range station.Berths {
			fixed[berth.Node] = true
		}
	}
	for _, lane := range network.Lanes {
		if lane.StationID != "" {
			fixed[lane.From], fixed[lane.To] = true, true
		}
	}
	for _, node := range network.Nodes {
		right = max(right, node.Position.X)
		if !fixed[node.ID] {
			ids = append(ids, node.ID)
		}
	}
	return ids, right
}

// keptSequence is the state of one run of the differential test.
type keptSequence struct {
	*keptEngine
	steps    int
	accepted map[string]bool
	texts    map[string]bool
}

// check compares the engine with a new engine. The order of the two
// operations alternates. It records the kinds of the replies.
func (s *keptSequence) check(step string) {
	s.t.Helper()
	operations := []string{"validate", "checks"}
	if s.steps%2 == 1 {
		operations = []string{"checks", "validate"}
	}
	s.steps++
	for _, reply := range s.compare(step, operations...) {
		for _, marker := range []string{"parking routes:", "validate large geometry:", "validate station banks:", "Demand profile", `"valid":true`} {
			if strings.Contains(reply, marker) {
				s.texts[marker] = true
			}
		}
	}
}

// try applies a geometry edit, and compares when the engine accepts it.
func (s *keptSequence) try(action, geometry string) bool {
	s.t.Helper()
	if !s.edit(geometry) {
		return false
	}
	s.accepted[action] = true
	s.check(action)
	return true
}

// point returns the position of a node, moved by dx, as a JSON point.
func (s *keptSequence) point(id string, dx float64) string {
	node, _ := s.network().Node(id)
	return fmt.Sprintf(`{"x":%g,"y":%g}`, node.Position.X+dx, node.Position.Y)
}

// edits applies the edits of the sequence: two moves with no validation
// between them, edits of metadata, and lane, station and berth edits.
func (s *keptSequence) edits(config project.Config) {
	s.t.Helper()
	plain, right := plainNodes(config.Network)
	for _, id := range plain {
		if s.edit(`{"action":"moveNode","id":"`+id+`","point":`+s.point(id, 0.25)+`}`) &&
			s.try("moveNode", `{"action":"moveNode","id":"`+id+`","point":`+s.point(id, 0.25)+`}`) {
			break
		}
	}
	station, lane := config.Network.Stations[0].ID, config.Network.Lanes[0].ID
	s.try("stationName", `{"action":"stationName","id":"`+station+`","value":"Renamed"}`)
	if s.change(`{"field":"name","value":"Other name"}`) {
		s.accepted["name"] = true
		s.check("name")
	}
	s.try("laneSpeed", `{"action":"laneSpeed","id":"`+lane+`","value":9}`)
	s.try("toggleCurve", `{"action":"toggleCurve","id":"`+lane+`"}`)
	lastNode := func() string { nodes := s.network().Nodes; return nodes[len(nodes)-1].ID }
	if s.try("addNode", fmt.Sprintf(`{"action":"addNode","point":{"x":%g,"y":0}}`, right+400)) {
		first := lastNode()
		if s.try("addNode", fmt.Sprintf(`{"action":"addNode","point":{"x":%g,"y":0}}`, right+500)) &&
			s.try("addLane", `{"action":"addLane","id":"`+first+`","to":"`+lastNode()+`"}`) {
			lanes := s.network().Lanes
			s.try("deleteLane", `{"action":"deleteLane","id":"`+lanes[len(lanes)-1].ID+`"}`)
		}
	}
	if s.try("addStation", fmt.Sprintf(`{"action":"addStation","point":{"x":%g,"y":0}}`, right+900)) {
		stations := s.network().Stations
		added := stations[len(stations)-1].ID
		if s.try("addBerth", `{"action":"addBerth","id":"`+added+`"}`) {
			berths := s.network().Stations[len(stations)-1].Berths
			s.try("removeBerth", `{"action":"removeBerth","id":"`+added+`","value":"`+berths[len(berths)-1].ID+`"}`)
		}
		s.try("stationParking", `{"action":"stationParking","id":"`+added+`","value":true}`)
		s.try("stationParking", `{"action":"stationParking","id":"`+added+`","value":false}`)
		s.try("deleteStation", `{"action":"deleteStation","id":"`+added+`"}`)
	}
}

// moves applies undo and redo moves of the history.
func (s *keptSequence) moves() {
	s.t.Helper()
	for _, kind := range []string{"undo", "undo", "undo", "redo", "redo", "undo", "redo", "redo"} {
		if state := s.model.timeline.state; kind == "undo" && len(state.past) == 0 || kind == "redo" && len(state.future) == 0 {
			continue
		}
		s.history(`"kind":"` + kind + `"`)
		s.accepted[kind] = true
		s.check(kind)
	}
}

// patches replaces the network with networks that are not valid: a parking
// gap, a plane reach refusal alone and after another refusal, a bank error,
// and a member that the project decoder refuses. After each of them it
// synchronizes the bytes of start again. Last, it gives the profiles an
// error and moves a node.
func (s *keptSequence) patches(config project.Config, start jsontext.Value) {
	s.t.Helper()
	back := func(step string) {
		s.t.Helper()
		s.patchRaw(jsontext.Value(`{"network":` + string(start) + `}`))
		s.check(step + ", back")
	}
	// A parking station that no lane enters has a parking gap.
	gap := config.Network
	for _, parking := range gap.Stations {
		if parking.ParkingOnly {
			gap.Lanes = slices.DeleteFunc(slices.Clone(gap.Lanes), func(lane sim.Lane) bool { return lane.To == parking.Entry })
		}
	}
	s.patch(map[string]any{"network": gap})
	s.check("parking gap")
	moved := gap
	moved.Nodes = slices.Clone(gap.Nodes)
	moved.Nodes[0].Position.X += 0.125
	s.patch(map[string]any{"network": moved})
	s.check("parking gap, moved")
	back("parking gap")
	reach := planeJoinDraft(24, []any{"legacy", "group"}, nil, "Q")
	s.patch(map[string]any{"network": reach})
	s.check("plane reach")
	reach["nodes"] = append(items(reach["nodes"]), map[string]any{"id": "short-a", "position": map[string]any{"x": 2000.0, "y": 0.0}}, map[string]any{"id": "short-b", "position": map[string]any{"x": 2010.0, "y": 0.0}})
	reach["lanes"] = append(items(reach["lanes"]), map[string]any{"id": "short", "from": "short-a", "to": "short-b", "speedLimit": float64(14)})
	s.patch(map[string]any{"network": reach})
	s.check("plane reach and short lane")
	back("plane reach")
	// A bank with no through lane.
	banked := config.Network
	for _, station := range banked.Stations {
		for _, bank := range station.Banks {
			banked.Lanes = slices.DeleteFunc(slices.Clone(banked.Lanes), func(lane sim.Lane) bool { return lane.From == bank.Entry && lane.To == bank.Exit })
		}
	}
	s.patch(map[string]any{"network": banked})
	s.check("bank error")
	back("bank error")
	unknown := configDraft(s.t, config)["network"]
	object(items(member(unknown, "lanes"))[0])["unknown"] = true
	s.patch(map[string]any{"network": unknown})
	s.check("unknown member")
	back("unknown member")
	if len(config.DemandProfiles) != 0 {
		profiles := configDraft(s.t, config)["demandProfiles"]
		object(items(profiles)[0])["bands"] = []any{}
		s.patch(map[string]any{"demandProfiles": profiles})
		s.check("profile error")
		id := config.Network.Nodes[0].ID
		s.edit(`{"action":"moveNode","id":"` + id + `","point":` + s.point(id, 0.5) + `}`)
		s.check("profile error, moved")
	}
}

// TestKeptVerdictsMatchNewEngine is the differential test of the results
// that the engine keeps between operations. It applies a sequence of edits,
// history moves and patches to one engine. After each step, the validation
// and the checks of the engine must be the replies of a new engine with the
// same branch bytes, error text included.
func TestKeptVerdictsMatchNewEngine(t *testing.T) {
	t.Parallel()
	for name, config := range keptFixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s := &keptSequence{keptEngine: newKeptEngine(t, config), accepted: make(map[string]bool), texts: make(map[string]bool)}
			s.large = len(config.Network.Nodes) > 1000
			s.history(`"kind":"reset","background":null`)
			s.check("start")
			start := s.model.branches["network"].raw
			s.edits(config)
			s.moves()
			s.patches(config, start)
			t.Logf("%d comparisons, accepted %v, texts %v", s.steps, slices.Sorted(maps.Keys(s.accepted)), slices.Sorted(maps.Keys(s.texts)))
			for _, action := range []string{"moveNode", "stationName", "name", "addNode", "addLane", "deleteLane", "addStation", "addBerth", "removeBerth", "deleteStation", "undo", "redo"} {
				if !s.accepted[action] {
					t.Errorf("the sequence has no accepted %s", action)
				}
			}
			// Each fixture has a valid step, and the fixtures with the
			// members of a kept error have a step with that error.
			wanted := map[string][]string{
				"small":   {"Demand profile"},
				"banks":   {"validate station banks:"},
				"compact": {"parking routes:"},
			}
			for _, text := range append(wanted[name], `"valid":true`, "validate large geometry:") {
				if !s.texts[text] {
					t.Errorf("no step has a reply with %s", text)
				}
			}
		})
	}
}

// TestKeptParkingCostBound is the differential test of the cost bound of the
// kept parking route result. In the default project, the lanes return-start
// and return-to-parking are the only route from the market to parking. With
// very low speed limits, a move of the node return-east between them makes
// the cost of one lane, or the sum of the costs of the two, infinite, which
// removes the route. The engine must give the replies of a new engine and of
// the stateless handler after each move, in each direction.
func TestKeptParkingCostBound(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		start, toParking float64
		far              string
	}{
		// return-to-parking is 280 m long, then 380 m long.
		{"lane cost", 14, 2e-306, `{"x":1000,"y":460}`},
		// Each lane costs about 8e307 s, then 1.2e308 s and 8.5e307 s.
		{"route cost", 201 / 8e307, 280 / 8e307, `{"x":900,"y":560}`},
		// return-to-parking costs 4e307 s, in the bound of the kept
		// result, then 1280 m gives no finite cost. The engine keeps the
		// first result and must not keep the second.
		{"bound crossed", 14, 7e-306, `{"x":1900,"y":460}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := project.Default()
			network := config.Network
			network.Lanes = slices.Clone(network.Lanes)
			for index, lane := range network.Lanes {
				switch lane.ID {
				case "return-start":
					network.Lanes[index].SpeedLimit = test.start
				case "return-to-parking":
					network.Lanes[index].SpeedLimit = test.toParking
				}
			}
			k := newKeptEngine(t, config)
			k.history(`"kind":"reset","background":null`)
			k.patch(map[string]any{"network": network})
			want := []bool{false, true, false, true}
			for index, point := range []string{`{"x":900,"y":460}`, test.far, `{"x":900,"y":460}`, test.far} {
				if index > 0 && !k.edit(`{"action":"moveNode","id":"return-east","point":`+point+`}`) {
					t.Fatalf("step %d: the engine refused the move", index)
				}
				replies := k.compare(fmt.Sprintf("step %d", index), "validate", "checks")
				if gap := strings.Contains(replies[0], "parking routes:"); gap != want[index] {
					t.Fatalf("step %d: parking refusal %t, want %t: %.300s", index, gap, want[index], replies[0])
				}
			}
		})
	}
}
