package scenarios

import (
	"fmt"
	"math"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestLondonMirrorRemovesAccessCrossover(t *testing.T) {
	t.Parallel()
	for _, heading := range []float64{0, math.Pi / 2, math.Pi, 3 * math.Pi / 2} {
		t.Run(fmt.Sprintf("heading_%g", heading), func(t *testing.T) {
			t.Parallel()
			site := londonSite{berths: 2, pitch: 75}
			frame := site.shape(heading).frame()
			site.arrivals = []sim.Point{frame.position(30, 0)}
			site.departures = []sim.Point{frame.position(-30, 0)}
			input := londonHeadingInput{sites: []londonSite{site}, centers: []sim.Point{{}}}
			before := site.shapeFootprint(site.shape(heading))
			shapes := mirrorLondonStations(input, []float64{heading})
			after := site.shapeFootprint(shapes[0])
			if !shapes[0].mirrored || !before.roads[0].crosses(before.roads[1]) || after.roads[0].crosses(after.roads[1]) {
				t.Fatal("station did not eliminate the entry/exit crossover")
			}
			oldNodes, newNodes := site.shape(heading).nodes(), shapes[0].nodes()
			if oldNodes.entry != newNodes.exit || oldNodes.diverge != newNodes.merge {
				t.Fatal("mirror did not reverse the station throat")
			}
			for i, berth := range oldNodes.berths {
				if berth.berth != newNodes.berths[i].berth || berth.arrival != newNodes.berths[i].departure {
					t.Fatal("mirror moved a berth or failed to reverse its access")
				}
			}
		})
	}
}

func TestLondonMirrorKeepsUncrossedLayout(t *testing.T) {
	t.Parallel()
	site := londonSite{berths: 2, pitch: 75, arrivals: []sim.Point{{X: 0, Y: -30}}, departures: []sim.Point{{X: 0, Y: 30}}}
	input := londonHeadingInput{sites: []londonSite{site}, centers: []sim.Point{{}}}
	if mirrorLondonStations(input, []float64{0})[0].mirrored {
		t.Fatal("mirrored an uncrossed layout")
	}
}

func TestLondonMirrorRejectsShortAccessRoad(t *testing.T) {
	t.Parallel()
	site := londonSite{berths: 2, pitch: 75, arrivals: []sim.Point{{X: 80, Y: 29}}, departures: []sim.Point{{X: 0, Y: -30}}}
	input := londonHeadingInput{sites: []londonSite{site}, centers: []sim.Point{{}}}
	if mirrorLondonStations(input, []float64{0})[0].mirrored {
		t.Fatal("mirror created a one-meter approach road")
	}
}
