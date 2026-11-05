package editormodel

import (
	"fmt"
	"math"
	"testing"
)

// BenchmarkHubChecks measures the checks of a draft with 2,000 lanes at one
// hub, which reports the limit of the lanes at a node.
func BenchmarkHubChecks(b *testing.B) {
	for name, classes := range map[string][]any{"small": {"legacy"}, "large": {"legacy", "group"}} {
		for groupName, group := range map[string]bool{"blank": false, "distinct": true} {
			b.Run(name+"/"+groupName, func(b *testing.B) {
				nodes := []any{map[string]any{"id": "hub", "position": map[string]any{"x": 0.0, "y": 0.0}}}
				lanes := []any{}
				for spoke := range 2000 {
					angle := 2 * math.Pi * float64(spoke) / 2000
					id := fmt.Sprintf("end-%d", spoke)
					nodes = append(nodes, map[string]any{"id": id, "position": map[string]any{"x": 200 * math.Cos(angle), "y": 200 * math.Sin(angle)}})
					lane := map[string]any{"id": fmt.Sprintf("lane-%d", spoke), "from": "hub", "to": id, "speedLimit": 14.0, "vehicleClasses": classes}
					if group {
						lane["separationGroup"] = fmt.Sprintf("plane-%d", spoke)
					}
					lanes = append(lanes, lane)
				}
				draft := map[string]any{"name": "hub", "network": map[string]any{"nodes": nodes, "lanes": lanes, "stations": []any{}}}
				b.ReportAllocs()
				for b.Loop() {
					draftChecks(draft)
				}
			})
		}
	}
}
