package sim

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// planeReachHubs returns hubs overlapping stars of lanes spokes each, the
// shape that costs the reach test the most: the lanes of different hubs
// share no node, and their boxes overlap. group gives the separation group
// of a spoke.
func planeReachHubs(classes ClassSet, hubs, spokes int, group func(hub, spoke int) string) Network {
	var n Network
	for hub := range hubs {
		center := Point{X: float64(hub) * 50}
		hubID := fmt.Sprintf("hub-%d", hub)
		n.Nodes = append(n.Nodes, Node{ID: hubID, Position: center})
		for spoke := range spokes {
			angle := 2 * math.Pi * float64(spoke) / float64(spokes)
			end := fmt.Sprintf("end-%d-%d", hub, spoke)
			n.Nodes = append(n.Nodes, Node{ID: end, Position: Point{X: center.X + 200*math.Cos(angle), Y: 200 * math.Sin(angle)}})
			n.Lanes = append(n.Lanes, Lane{ID: fmt.Sprintf("lane-%d-%d", hub, spoke), From: hubID, To: end, SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: group(hub, spoke)})
		}
	}
	return n
}

func planeReachGroup(name string) func(hub, spoke int) string {
	switch name {
	case "planes":
		return func(hub, _ int) string { return fmt.Sprintf("plane-%d", hub) }
	case "distinct":
		return func(hub, spoke int) string { return fmt.Sprintf("plane-%d-%d", hub, spoke) }
	}
	return func(int, int) string { return "" }
}

// BenchmarkPlaneReach measures the index of the editor and the bank audit
// on one hub of 2,000 lanes, and on two hubs with the same total, where
// every lane pair of different hubs needs the reach test.
func BenchmarkPlaneReach(b *testing.B) {
	classes := map[string][]string{"small": {"legacy", "compact"}, "large": {"legacy", "compact", "group"}}
	for _, shape := range []struct {
		name          string
		hubs, spokes  int
		class, groups string
	}{
		{"hub/small/blank", 1, 2000, "small", ""},
		{"hub/small/distinct", 1, 2000, "small", "distinct"},
		{"hub/large/blank", 1, 2000, "large", ""},
		{"hub/large/distinct", 1, 2000, "large", "distinct"},
		{"hubcross/large/distinct", 1, 2000, "large", "distinct"},
		{"twohubs200/large/planes", 2, 100, "large", "planes"},
		{"twohubs200/large/distinct", 2, 100, "large", "distinct"},
		{"twohubs2000/large/planes", 2, 1000, "large", "planes"},
		{"twohubs2000/large/distinct", 2, 1000, "large", "distinct"},
	} {
		set, err := NewClassSet(classes[shape.class]...)
		if err != nil {
			b.Fatal(err)
		}
		network := planeReachHubs(set, shape.hubs, shape.spokes, planeReachGroup(shape.groups))
		cross := network.Lanes[shape.spokes:]
		if strings.HasPrefix(shape.name, "hubcross") {
			// One lane on its own plane passes through every spoke, so that
			// each lane of the hub needs the reach test.
			network.Nodes = append(network.Nodes, Node{ID: "west", Position: Point{X: -300, Y: 1}}, Node{ID: "east", Position: Point{X: 300, Y: 1}})
			network.Lanes = append(network.Lanes, Lane{ID: "cross", From: "west", To: "east", SpeedLimit: 14, VehicleClasses: set, SeparationGroup: "cross"})
			cross = network.Lanes[shape.spokes:]
		}
		b.Run(shape.name+"/clearance", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				clearance := NewLaneClearance(network)
				for _, first := range network.Lanes[:shape.spokes] {
					for _, second := range cross {
						clearance.PlanesSeparated(first, second)
					}
				}
			}
		})
		b.Run(shape.name+"/audit", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := network.ValidateBankGeometry(); err != nil && shape.groups != "" {
					b.Fatal(err)
				}
			}
		})
	}
}
