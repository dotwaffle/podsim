package project

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// twoPairProject returns the default project with two independent pairs of
// small lanes 3.0 m apart that share no node. Each pair is a refusal of the
// all-pairs audit. Lane b of a pair is 200 m long, and lane a is the middle
// 100 m beside it.
func twoPairProject() Config {
	config := Clone(Default())
	for _, pair := range []struct {
		name string
		x    float64
	}{{"pair-1", 9000}, {"pair-2", 10000}} {
		for _, row := range []struct {
			lane        string
			from, to, y float64
		}{{"a", 0, 100, 0}, {"b", -50, 150, 3}} {
			from, to := pair.name+"-"+row.lane+"-from", pair.name+"-"+row.lane+"-to"
			config.Network.Nodes = append(config.Network.Nodes, sim.Node{ID: from, Position: sim.Point{X: pair.x + row.from, Y: row.y}}, sim.Node{ID: to, Position: sim.Point{X: pair.x + row.to, Y: row.y}})
			config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: pair.name + "-" + row.lane, From: from, To: to, SpeedLimit: 14})
		}
	}
	return config
}

// moveJSONNodes moves the "to" and "from" nodes of a lane in the project
// JSON from Y 3 to Y 20, and decodes the project again.
func moveJSONNodes(t *testing.T, data []byte, lane string, x float64) ([]byte, Config) {
	t.Helper()
	for _, node := range []struct {
		suffix string
		x      float64
	}{{"-from", x - 50}, {"-to", x + 150}} {
		old := fmt.Appendf(nil, `{"id":%q,"position":{"x":%g,"y":3}}`, lane+node.suffix, node.x)
		if bytes.Count(data, old) != 1 {
			t.Fatalf("the project JSON does not have %s once", old)
		}
		data = bytes.Replace(data, old, fmt.Appendf(nil, `{"id":%q,"position":{"x":%g,"y":20}}`, lane+node.suffix, node.x), 1)
	}
	var config Config
	if err := jsonv2.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return data, config
}

// TestRepairTwoRefusedPairsInJSON pins the repair route of a project with
// two refused pairs: a change of the project JSON for each pair, with a
// validation after each change.
func TestRepairTwoRefusedPairsInJSON(t *testing.T) {
	t.Parallel()
	config := twoPairProject()
	const first = `invalid project scenario: nonincident paths "pair-1-b" and "pair-1-a" are less than 12 meters apart`
	if err := Validate(config); err == nil || err.Error() != first {
		t.Fatalf("refusal = %v, want %s", err, first)
	}
	data, err := jsonv2.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	data, config = moveJSONNodes(t, data, "pair-1-b", 9000)
	const second = `invalid project scenario: nonincident paths "pair-2-b" and "pair-2-a" are less than 12 meters apart`
	if err := Validate(config); err == nil || err.Error() != second {
		t.Fatalf("refusal after the first change = %v, want %s", err, second)
	}
	_, config = moveJSONNodes(t, data, "pair-2-b", 10000)
	if err := Validate(config); err != nil {
		t.Fatalf("refusal after the second change = %v", err)
	}
}
