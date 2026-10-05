package sim

import (
	"errors"
	"strings"
	"testing"
)

// checkTickMotion accepts the scheduled connected tick and refuses each
// fabricated next state. The schedule always passes this check, so only a
// fabricated state can show that the check is live.
func TestCouplingTickMotionRefusesFabricatedStep(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(c *couplingMotionContext, previous couplingMotionState, next *couplingMotionState)
		want   string
	}{
		{"scheduled", func(*couplingMotionContext, couplingMotionState, *couplingMotionState) {}, ""},
		{"euler", func(_ *couplingMotionContext, _ couplingMotionState, next *couplingMotionState) {
			next.Distances[0] += 0.01
		}, "strict Euler"},
		{"acceleration", func(_ *couplingMotionContext, previous couplingMotionState, next *couplingMotionState) {
			next.Speeds[1] = previous.Speeds[1] + 1
			next.Distances[1] = previous.Distances[1] + next.Speeds[1]/TicksPerSecond
		}, "strict Euler or acceleration"},
		{"lane_limit", func(c *couplingMotionContext, _ couplingMotionState, next *couplingMotionState) {
			blocks := &c.reservation.routes[0]
			lane := blocks.routeLane(next.Cells[0])
			blocks.route = append([]Lane(nil), blocks.route...)
			blocks.route[lane].SpeedLimit = next.Speeds[0] / 2
		}, "lane speed exceeded"},
		{"connector_geometry", func(_ *couplingMotionContext, _ couplingMotionState, next *couplingMotionState) {
			next.Positions[1].Y += 0.5
		}, "body and connector geometry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			c, previous, next := couplingConnectorContext(t, "pair")
			if next.Speeds[0] <= 0 || next.Speeds[1] <= 0 {
				t.Fatal("connected fixture tick does not move both members")
			}
			test.change(c, previous, &next)
			err := c.checkTickMotion(previous, next)
			if test.want == "" {
				if err != nil {
					t.Fatal("scheduled connected tick refused", err)
				}
				return
			}
			if !errors.Is(err, errCouplingMotionInvariant) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("fabricated %s tick accepted or refused for another reason: %v", test.name, err)
			}
		})
	}
}
