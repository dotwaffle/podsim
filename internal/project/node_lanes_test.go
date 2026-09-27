package project

import (
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// ringOfHubs returns the example project with a ring of added nodes. Each
// added node has a straight lane to each of the next MaxNodeLanes/2 nodes,
// so it has MaxNodeLanes lanes. The ring fills the lane limit.
func ringOfHubs() Config {
	config := Default()
	reach := MaxNodeLanes / 2
	count := (MaxLanes - len(config.Network.Lanes)) / reach
	radius := 40 * float64(count) / (2 * math.Pi)
	for index := range count {
		angle := 2 * math.Pi * float64(index) / float64(count)
		config.Network.Nodes = append(config.Network.Nodes, sim.Node{
			ID: fmt.Sprintf("hub-%d", index), Position: sim.Point{X: 20_000 + radius*math.Cos(angle), Y: 20_000 + radius*math.Sin(angle)},
		})
	}
	for index := range count {
		for step := 1; step <= reach; step++ {
			config.Network.Lanes = append(config.Network.Lanes, sim.Lane{
				ID: fmt.Sprintf("hub-%d-%d", index, step), From: fmt.Sprintf("hub-%d", index), To: fmt.Sprintf("hub-%d", (index+step)%count), SpeedLimit: 12,
			})
		}
	}
	return config
}

// bundles returns the example project with added pairs of nodes 30 meters
// apart. Each pair has MaxNodeLanes lanes, with a different curve for each
// lane. Lanes that stay near each other make each comparison slow, so this
// is a slow layout for the lane limits.
func bundles() Config {
	config := Default()
	for bundle := 0; len(config.Network.Lanes)+MaxNodeLanes <= MaxLanes && len(config.Network.Nodes)+2 <= MaxNodes; bundle++ {
		x, y := 20_000+float64(bundle%30)*500, 20_000+float64(bundle/30)*1_000
		from, to := fmt.Sprintf("bundle-%d-a", bundle), fmt.Sprintf("bundle-%d-b", bundle)
		config.Network.Nodes = append(config.Network.Nodes,
			sim.Node{ID: from, Position: sim.Point{X: x, Y: y}}, sim.Node{ID: to, Position: sim.Point{X: x + 30, Y: y}})
		for index := range MaxNodeLanes {
			config.Network.Lanes = append(config.Network.Lanes, sim.Lane{
				ID: fmt.Sprintf("bundle-%d-%d", bundle, index), From: from, To: to, SpeedLimit: 12,
				Control: &sim.Point{X: x + 15, Y: y + 5*float64(index)},
			})
		}
	}
	return config
}

func TestValidateNodeLanes(t *testing.T) {
	t.Parallel()
	base := Default()
	base.Network.Nodes = append(base.Network.Nodes, sim.Node{ID: "hub", Position: sim.Point{X: 20_000, Y: 20_000}})
	for index := range MaxNodeLanes {
		angle := 2 * math.Pi * float64(index) / MaxNodeLanes
		id := fmt.Sprintf("spoke-%d", index)
		base.Network.Nodes = append(base.Network.Nodes, sim.Node{ID: id, Position: sim.Point{X: 20_000 + 500*math.Cos(angle), Y: 20_000 + 500*math.Sin(angle)}})
		base.Network.Lanes = append(base.Network.Lanes, sim.Lane{ID: id, From: "hub", To: id, SpeedLimit: 12})
	}
	if err := Validate(base); err != nil {
		t.Fatalf("a node with %d lanes: %v", MaxNodeLanes, err)
	}
	curved := Clone(base)
	curved.Network.Lanes = append(curved.Network.Lanes, sim.Lane{ID: "curved", From: "spoke-0", To: "hub", SpeedLimit: 12, Control: &sim.Point{X: 20_250, Y: 100}})
	loop := Clone(base)
	loop.Network.Lanes[len(loop.Network.Lanes)-1] = sim.Lane{ID: "loop", From: "hub", To: "hub", SpeedLimit: 12, Control: &sim.Point{X: 20_000, Y: 20_100}}
	duplicate := Clone(base)
	duplicate.Network.Lanes[len(duplicate.Network.Lanes)-1].To = "spoke-0"
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{"one lane more", curved, fmt.Sprintf(`node "hub" has %d lanes, more than %d`, MaxNodeLanes+1, MaxNodeLanes)},
		{"a loop counts twice", loop, fmt.Sprintf(`node "hub" has %d lanes, more than %d`, MaxNodeLanes+1, MaxNodeLanes)},
		{"a lane with the path of another lane", duplicate, fmt.Sprintf(`lanes "spoke-0" and "spoke-%d" have the same nodes and path`, MaxNodeLanes-1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(test.config); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate = %v, want %q", err, test.want)
			}
		})
	}
}

// TestNodeLaneLimitBoundsFleetMemory starts a fleet on a network in which
// each added node has MaxNodeLanes lanes, and the network has MaxLanes
// lanes. The simulator keeps an entry for each pair of lanes at a node, so
// the lane limits bound the memory that the fleet keeps. It does not run in
// parallel, because it measures the heap.
func TestNodeLaneLimitBoundsFleetMemory(t *testing.T) {
	config := ringOfHubs()
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fleet, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	// Each entry has about 32 bytes. The bound doubles that for the slices
	// and the maps.
	bound := int64(2*MaxLanes*MaxNodeLanes) * 64
	t.Logf("a fleet on %d lanes with %d lanes at each added node retains %d bytes, bound %d", len(config.Network.Lanes), MaxNodeLanes, retained, bound)
	if retained > bound {
		t.Fatalf("the fleet retains %d bytes, more than %d", retained, bound)
	}
	runtime.KeepAlive(fleet)
}

// BenchmarkFleetAtNodeLaneLimit starts a fleet on the slow layout of
// bundles. Run it with -benchtime=1x.
func BenchmarkFleetAtNodeLaneLimit(b *testing.B) {
	config := bundles()
	if err := Validate(config); err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		if _, err := sim.NewFleet(config.Network, config.Fleet); err != nil {
			b.Fatal(err)
		}
	}
}
