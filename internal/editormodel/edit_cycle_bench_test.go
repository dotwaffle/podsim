package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// cyclePhases are the worker operations of one edit cycle, in the order in
// which the browser sends them.
var cyclePhases = []string{"sync-empty", "edit", "sync-network", "history-prepare", "history-accept", "validate", "checks"}

// cycleReply holds the members of a worker reply that the cycle reads.
type cycleReply struct {
	Valid  bool   `json:"valid"`
	Error  string `json:"error"`
	Change struct {
		Patch map[string]jsontext.Value `json:"patch"`
	} `json:"change"`
	History struct {
		Head     string `json:"head"`
		Revision string `json:"revision"`
		Proposal string `json:"proposal"`
	} `json:"history"`
	Checks *struct {
		Errors []check `json:"errors"`
	} `json:"checks"`
}

// editCycle sends the requests of the browser worker for one edit to one
// engine: the edit, the history commit of its reply, and the validation.
// The network bytes of each edit reply go to the next synchronization
// unchanged, as the browser sends them.
type editCycle struct {
	b        *testing.B
	call     func(string) string
	keys     string
	head     string
	revision string
	spent    map[string]time.Duration
}

// newEditCycle returns an engine that holds config with a new history.
func newEditCycle(b *testing.B, config project.Config) *editCycle {
	b.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		b.Fatal(err)
	}
	var fields map[string]jsontext.Value
	if err = json.Unmarshal(data, &fields); err != nil {
		b.Fatal(err)
	}
	keys, err := json.Marshal(slices.Sorted(maps.Keys(fields)))
	if err != nil {
		b.Fatal(err)
	}
	c := &editCycle{b: b, call: NewCall(), keys: string(keys), revision: "0", spent: make(map[string]time.Duration)}
	c.send("", `{"op":"sync","keys":`+c.keys+`,"patch":`+string(data)+`}`)
	prepared := c.send("", `{"op":"history","history":{"action":"prepare","kind":"reset","revision":"0","background":null}}`)
	c.accept("", prepared)
	return c
}

// send sends one request and adds its time to phase. An empty phase has no
// time. A reply with an error stops the benchmark.
func (c *editCycle) send(phase, request string) cycleReply {
	c.b.Helper()
	reply, text := c.try(phase, request)
	if reply.Error != "" {
		c.b.Fatalf("%s: %s", phase, text)
	}
	return reply
}

// try is send for a request that the engine can refuse.
func (c *editCycle) try(phase, request string) (cycleReply, string) {
	c.b.Helper()
	start := time.Now()
	text := c.call(request)
	if phase != "" {
		c.spent[phase] += time.Since(start)
	}
	var reply cycleReply
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		c.b.Fatal(err)
	}
	return reply, text
}

func (c *editCycle) accept(phase string, prepared cycleReply) {
	c.b.Helper()
	accepted := c.send(phase, `{"op":"history","history":{"action":"accept","token":"`+prepared.History.Proposal+`"}}`)
	c.head, c.revision = accepted.History.Head, accepted.History.Revision
}

func (c *editCycle) syncEmpty() {
	c.b.Helper()
	c.send("sync-empty", `{"op":"sync","keys":`+c.keys+`,"patch":{}}`)
}

// run does one cycle with a geometry edit. It returns false, with the engine
// unchanged, when the engine refuses the edit. A draft that is not valid
// after the edit stops the benchmark. The second result is the network of
// the edit reply.
func (c *editCycle) run(edit string) (jsontext.Value, bool) {
	c.b.Helper()
	c.syncEmpty()
	changed, _ := c.try("edit", `{"op":"edit","edit":{"field":"geometry","value":`+edit+`}}`)
	network := changed.Change.Patch["network"]
	if changed.Error != "" || len(network) == 0 {
		return nil, false
	}
	c.send("sync-network", `{"op":"sync","keys":`+c.keys+`,"patch":{"network":`+string(network)+`}}`)
	prepared := c.send("history-prepare", `{"op":"history","history":{"action":"prepare","kind":"commitFrom","revision":"`+c.revision+`","before":"`+c.head+`","background":null}}`)
	c.accept("history-accept", prepared)
	c.syncEmpty()
	if !c.send("validate", `{"op":"validate"}`).Valid {
		c.b.Fatalf("the draft is not valid after %s", edit)
	}
	if checked := c.send("checks", `{"op":"checks"}`); checked.Checks == nil || len(checked.Checks.Errors) != 0 {
		c.b.Fatalf("the checks have an error after %s", edit)
	}
	return network, true
}

// report gives the time of each phase and of the cycle, in milliseconds for
// each cycle.
func (c *editCycle) report(cycles int) {
	var sum time.Duration
	for _, phase := range cyclePhases {
		sum += c.spent[phase]
		c.b.ReportMetric(float64(c.spent[phase].Microseconds())/1000/float64(cycles), phase+"-ms/cycle")
	}
	c.b.ReportMetric(float64(sum.Microseconds())/1000/float64(cycles), "cycle-ms/cycle")
}

// BenchmarkEditCycle measures the worker time of one geometry edit of the
// LondonFull Express preset, by worker operation. Each cycle changes the
// network, so no cycle reads a result of the cycle before it for the same
// network. The moveNode case keeps the topology. The lane case adds a lane
// in one cycle and deletes it in the next.
//
// Run it with -benchtime=5x or more, and compare the medians of 7 runs.
func BenchmarkEditCycle(b *testing.B) {
	if testing.Short() {
		b.Skip("the cycle of the LondonFull Express preset is slow")
	}
	config := scenarios.LondonFullExpress()
	// The plain nodes: no station, berth, maneuver group or depot reads them.
	fixed := make(map[string]bool)
	for _, station := range config.Network.Stations {
		fixed[station.Entry], fixed[station.Exit] = true, true
		for _, berth := range station.Berths {
			fixed[berth.Node] = true
		}
	}
	stationLane := make(map[string]bool)
	for _, lane := range config.Network.Lanes {
		if lane.StationID != "" {
			stationLane[lane.From], stationLane[lane.To] = true, true
		}
	}
	var plain []int
	for index, node := range config.Network.Nodes {
		if !fixed[node.ID] && !stationLane[node.ID] {
			plain = append(plain, index)
		}
	}
	b.Run("moveNode", func(b *testing.B) {
		cycle := newEditCycle(b, config)
		move := func(index, step int) string {
			node := config.Network.Nodes[index]
			return fmt.Sprintf(`{"action":"moveNode","id":%s,"point":{"x":%s,"y":%s}}`, strconv.Quote(node.ID),
				strconv.FormatFloat(node.Position.X+0.01*float64(1+step%50), 'f', -1, 64), strconv.FormatFloat(node.Position.Y, 'f', -1, 64))
		}
		// Find a node that the engine lets move.
		chosen := -1
		for _, index := range plain[:min(len(plain), 20)] {
			if _, ok := cycle.run(move(index, 49)); ok {
				chosen = index
				break
			}
		}
		if chosen < 0 {
			b.Fatal("no plain node can move")
		}
		clear(cycle.spent)
		step := 0
		for b.Loop() {
			if _, ok := cycle.run(move(chosen, step)); !ok {
				b.Fatal("the engine refused the move")
			}
			step++
		}
		cycle.report(step)
	})
	b.Run("addDeleteLane", func(b *testing.B) {
		cycle := newEditCycle(b, config)
		add := func(from, to int) string {
			return fmt.Sprintf(`{"action":"addLane","id":%s,"to":%s}`, strconv.Quote(config.Network.Nodes[from].ID), strconv.Quote(config.Network.Nodes[to].ID))
		}
		// remove returns the edit that deletes the last lane of network.
		remove := func(network jsontext.Value) string {
			var added struct {
				Lanes []struct {
					ID string `json:"id"`
				} `json:"lanes"`
			}
			if err := json.Unmarshal(network, &added); err != nil || len(added.Lanes) == 0 {
				b.Fatal("the edit reply has no lane")
			}
			return `{"action":"deleteLane","id":` + strconv.Quote(added.Lanes[len(added.Lanes)-1].ID) + `}`
		}
		// Find two plain nodes that the engine lets a new lane join.
		from, to := -1, -1
		for i := 0; i+1 < min(len(plain), 40) && from < 0; i += 2 {
			if network, ok := cycle.run(add(plain[i], plain[i+1])); ok {
				from, to = plain[i], plain[i+1]
				cycle.run(remove(network))
			}
		}
		if from < 0 {
			b.Fatal("no two plain nodes accept a lane")
		}
		clear(cycle.spent)
		cycles := 0
		var network jsontext.Value
		for b.Loop() {
			edit := add(from, to)
			if cycles%2 == 1 {
				edit = remove(network)
			}
			var ok bool
			if network, ok = cycle.run(edit); !ok {
				b.Fatal("the engine refused the lane edit")
			}
			cycles++
		}
		cycle.report(cycles)
	})
}
