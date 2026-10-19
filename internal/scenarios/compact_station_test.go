package scenarios

import (
	"encoding/json/v2"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestCompactStationTable checks the generated geometry against the
// values of the design table, to their printed precision.
func TestCompactStationTable(t *testing.T) {
	t.Parallel()
	network := compactStationNetwork()
	near := func(name string, got, want, tolerance float64) {
		t.Helper()
		if math.Abs(got-want) > tolerance {
			t.Errorf("%s is %.9f, want %.9f", name, got, want)
		}
	}
	point := func(name string, got, want sim.Point) {
		t.Helper()
		near(name+" x", got.X, want.X, 5e-7)
		near(name+" y", got.Y, want.Y, 5e-7)
	}
	node := func(id string) sim.Point {
		t.Helper()
		found, ok := network.Node(id)
		if !ok {
			t.Fatalf("no node %q", id)
		}
		return found.Position
	}
	lane := func(id string) sim.Lane {
		t.Helper()
		for _, lane := range network.Lanes {
			if lane.ID == id {
				return lane
			}
		}
		t.Fatalf("no lane %q", id)
		return sim.Lane{}
	}
	y := compactAisleY
	for _, radius := range []struct {
		arc  compactArc
		want float64
	}{
		{compactBays[0][0], 5.0988}, {compactBays[0][1], 5.0200}, {compactBays[1][0], 5.0280}, {compactBays[1][1], 5.0013},
	} {
		near(fmt.Sprint("radius of the arc of ", radius.arc.sweep), compactArcRadius(radius.arc), radius.want, 5e-5)
	}
	point("b1", node("branch-1"), sim.Point{X: -7.130146, Y: y})
	bay1 := []struct{ node, control *sim.Point }{
		{&sim.Point{X: -5.180584, Y: -3.620733}, &sim.Point{X: -6.116867, Y: y}},
		{&sim.Point{X: -3.527301, Y: -2.517305}, &sim.Point{X: -4.244301, Y: -3.233297}},
		{&sim.Point{X: -2.513211, Y: -1.504641}, nil},
		{&sim.Point{X: -1.843937, Y: -0.943654}, &sim.Point{X: -2.203065, Y: -1.194929}},
		{nil, &sim.Point{X: -1.484809, Y: -0.692378}},
	}
	bay2 := []struct{ node, control *sim.Point }{
		{&sim.Point{X: -3.058138, Y: -3.900422}, &sim.Point{X: -3.570193, Y: y}},
		{&sim.Point{X: -2.067188, Y: -3.581799}, &sim.Point{X: -2.546083, Y: -3.792675}},
		{&sim.Point{X: 4.814302, Y: -0.551620}, nil},
		{nil, &sim.Point{X: 4.863606, Y: -0.529909}},
	}
	lengths := [][]float64{{2.0007, 2.0007, 1.4331, 0.8744, 0.8744}, {1.0428, 1.0428, 7.5191, 0.1077}}
	for bay := 1; bay <= 4; bay++ {
		shift, pieces := compactPitch*float64(bay-2), bay2
		if bay == 1 {
			shift, pieces = 0, bay1
		} else {
			point(fmt.Sprint("b", bay), node(fmt.Sprint("branch-", bay)), sim.Point{X: -4.093461 + shift, Y: y})
		}
		point(fmt.Sprint("B", bay), node(fmt.Sprint("berth-", bay)), sim.Point{X: -1.087569 + compactPitch*float64(bay-1), Y: -0.507142})
		for k, piece := range pieces {
			id := fmt.Sprintf("bay-%d-in-%d", bay, k+1)
			in, out := lane(id), lane(fmt.Sprintf("bay-%d-out-%d", bay, k+1))
			if piece.node != nil {
				point(id+" end", node(in.To), sim.Point{X: piece.node.X + shift, Y: piece.node.Y})
			}
			if (in.Control == nil) != (piece.control == nil) || (out.Control == nil) != (piece.control == nil) {
				t.Fatalf("lane %s has control %v, want %v", id, in.Control, piece.control)
			}
			if piece.control != nil {
				point(id+" control", *in.Control, sim.Point{X: piece.control.X + shift, Y: piece.control.Y})
				if *out.Control != *in.Control {
					t.Errorf("the reverse lane of %s has control %v", id, *out.Control)
				}
			}
			if out.From != in.To || out.To != in.From {
				t.Errorf("the reverse lane of %s runs from %s to %s", id, out.From, out.To)
			}
			near(id+" length", network.Length(in), lengths[min(bay-1, 1)][k], 5e-5)
		}
	}
	for _, aisle := range []struct {
		id   string
		want float64
	}{
		{"aisle-1", 12.009854}, {"aisle-2", 3.036685}, {"aisle-3", 6}, {"aisle-4", 6}, {"aisle-out", 20.493461},
	} {
		near(aisle.id+" length", network.Length(lane(aisle.id)), aisle.want, 5e-7)
	}
	point("H", node("hold"), sim.Point{X: -19.14, Y: y})
	point("R", node("receive"), sim.Point{X: 28.40, Y: y})
	point("E", node("entry"), sim.Point{X: -49.14, Y: y})
	point("X", node("exit"), sim.Point{X: 58.40, Y: y})
	point("bypass control", *lane("bypass").Control, sim.Point{X: 4.63, Y: y - 32})
}

// compactFixture is the part of a station-layout21 fixture that the
// generator test reads.
type compactFixture struct {
	Start    compactFixturePose `json:"start"`
	Commands []struct {
		Kind      string  `json:"kind"`
		Direction float64 `json:"direction"`
		Length    float64 `json:"length"`
		Radius    float64 `json:"radius"`
		Sweep     float64 `json:"sweep"`
	} `json:"commands"`
	Obstacles []struct {
		Rectangle compactFixturePose `json:"rectangle"`
	} `json:"obstacles"`
}

// compactFixturePose is a fixture pose, or the center and heading of a
// fixture rectangle.
type compactFixturePose struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Heading float64 `json:"heading"`
}

// authoredPath samples the forward path of a fixture, up to its first
// stop, every millimeter. It returns the points and the end heading.
func (fixture compactFixture) authoredPath() ([]sim.Point, float64) {
	p, heading := sim.Point{X: fixture.Start.X, Y: fixture.Start.Y}, fixture.Start.Heading
	points := []sim.Point{p}
	for _, command := range fixture.Commands {
		if command.Kind == "stop" {
			break
		}
		length := command.Length
		if command.Kind == "arc" {
			length = command.Radius * math.Abs(command.Sweep)
		}
		steps := max(1, int(math.Ceil(length/0.001)))
		start, startHeading := p, heading
		for i := 1; i <= steps; i++ {
			f := float64(i) / float64(steps)
			if command.Kind == "line" {
				p = sim.Point{X: start.X + f*length*math.Cos(startHeading), Y: start.Y + f*length*math.Sin(startHeading)}
			} else {
				sweep := command.Sweep * f
				chord := 2 * command.Radius * math.Sin(math.Abs(sweep)/2)
				p = sim.Point{X: start.X + chord*math.Cos(startHeading+sweep/2), Y: start.Y + chord*math.Sin(startHeading+sweep/2)}
				heading = startHeading + sweep
			}
			points = append(points, p)
		}
	}
	return points, heading
}

// segmentDistance returns the distance from p to the segment ab.
func segmentDistance(p, a, b sim.Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	t := 0.0
	if length := dx*dx + dy*dy; length > 0 {
		t = max(0, min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/length))
	}
	return math.Hypot(p.X-a.X-t*dx, p.Y-a.Y-t*dy)
}

// circumradius returns the radius of the circle through three points, or
// +Inf for collinear points.
func circumradius(a, b, c sim.Point) float64 {
	cross := (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
	if cross == 0 {
		return math.Inf(1)
	}
	return math.Hypot(b.X-a.X, b.Y-a.Y) * math.Hypot(c.X-b.X, c.Y-b.Y) * math.Hypot(a.X-c.X, a.Y-c.Y) / (2 * math.Abs(cross))
}

// TestCompactStationPaths checks each bay path against its authored
// station-layout21 fixture: the arcs keep the authored headings, the
// fixture ends at the berth pose with the parked envelope 1.2 meters
// ahead, every polyline point is within 1 cm of the authored path, and the
// polyline radius is at least 5.0009 meters.
func TestCompactStationPaths(t *testing.T) {
	t.Parallel()
	network := compactStationNetwork()
	lanes := make(map[string]sim.Lane)
	for _, lane := range network.Lanes {
		lanes[lane.ID] = lane
	}
	for index := range compactBays {
		bay := index + 1
		data, err := os.ReadFile(filepath.Join("testdata", "station-layout21", fmt.Sprintf("bay%d.json", bay)))
		if err != nil {
			t.Fatal(err)
		}
		var fixture compactFixture
		if err := json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
		var arcs []float64
		for _, command := range fixture.Commands[:4] {
			if command.Kind == "arc" {
				arcs = append(arcs, command.Sweep)
			}
		}
		if len(arcs) != 2 || arcs[0] != compactBays[index][0].sweep || arcs[1] != compactBays[index][1].sweep {
			t.Fatalf("bay %d: fixture arcs %v", bay, arcs)
		}
		authored, heading := fixture.authoredPath()
		berth := compactBerth(index)
		if end := authored[len(authored)-1]; math.Hypot(end.X-berth.X, end.Y-berth.Y) > 1e-6 || math.Abs(heading-compactBerthHeading) > 1e-12 {
			t.Fatalf("bay %d: fixture ends at %v, heading %g; berth %v", bay, end, heading, berth)
		}
		for k, obstacle := range fixture.Obstacles {
			other := compactBerth(int(math.Round(obstacle.Rectangle.X / compactPitch)))
			ahead := sim.Point{X: other.X + compactAxleOffset*math.Cos(compactBerthHeading), Y: other.Y + compactAxleOffset*math.Sin(compactBerthHeading)}
			if math.Hypot(ahead.X-obstacle.Rectangle.X, ahead.Y-obstacle.Rectangle.Y) > 1e-9 || obstacle.Rectangle.Heading != compactBerthHeading {
				t.Errorf("bay %d: obstacle %d %+v", bay, k, obstacle.Rectangle)
			}
		}
		var polyline []sim.Point
		for k := 1; ; k++ {
			lane, ok := lanes[fmt.Sprintf("bay-%d-in-%d", bay, k)]
			if !ok {
				break
			}
			points := network.Polyline(lane)
			if len(polyline) > 0 {
				points = points[1:]
			}
			polyline = append(polyline, points...)
		}
		deviation, radius := 0.0, math.Inf(1)
		for i, p := range polyline {
			nearest := math.Inf(1)
			for j := 1; j < len(authored); j++ {
				nearest = min(nearest, segmentDistance(p, authored[j-1], authored[j]))
			}
			deviation = max(deviation, nearest)
			if i >= 2 {
				radius = min(radius, circumradius(polyline[i-2], polyline[i-1], p))
			}
		}
		if deviation > 0.01 || radius < compactMinRadius+0.0009 {
			t.Errorf("bay %d: deviation %.4f m, minimum radius %.5f m", bay, deviation, radius)
		}
		t.Logf("bay %d: deviation %.4f m, minimum radius %.5f m", bay, deviation, radius)
	}
}

// TestCompactStationMargins checks that the holding node is more than 12
// meters from branch 1, the receiving node more than 12 meters from berth
// 4, and the bypass at least 12.38 meters from both.
func TestCompactStationMargins(t *testing.T) {
	t.Parallel()
	network := compactStationNetwork()
	position := func(id string) sim.Point {
		node, _ := network.Node(id)
		return node.Position
	}
	distance := func(a, b sim.Point) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }
	hold, receive := position("hold"), position("receive")
	if got := distance(hold, position("branch-1")); got < sim.Clearance || got > sim.Clearance+0.02 {
		t.Errorf("H is %g meters from b1", got)
	}
	if got := distance(receive, position("berth-4")); got < sim.Clearance || got > sim.Clearance+0.02 {
		t.Errorf("R is %g meters from B4", got)
	}
	for _, lane := range network.Lanes {
		if lane.ID != "bypass" {
			continue
		}
		points := network.Polyline(lane)
		for _, p := range []sim.Point{hold, receive} {
			nearest := math.Inf(1)
			for j := 1; j < len(points); j++ {
				nearest = min(nearest, segmentDistance(p, points[j-1], points[j]))
			}
			if nearest < 12.38 {
				t.Errorf("the bypass is %g meters from %v", nearest, p)
			}
		}
	}
}

// TestCompactStationPrepares checks that the preset validates, prepares,
// and starts with its six pods.
func TestCompactStationPrepares(t *testing.T) {
	t.Parallel()
	config, err := CompactStation()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sim.PrepareNetwork(config.Network); err != nil {
		t.Fatal(err)
	}
	s, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s.Snapshot().Vehicles); got != 6 {
		t.Fatalf("%d pods", got)
	}
	// R one centimeter closer to B4 fails the bank audit.
	network := compactStationNetwork()
	for i := range network.Nodes {
		if network.Nodes[i].ID == "receive" {
			network.Nodes[i].Position.X -= 0.01
		}
	}
	if _, err := sim.PrepareNetwork(network); err == nil {
		t.Fatal("R 1 cm closer to B4 passed")
	}
}
