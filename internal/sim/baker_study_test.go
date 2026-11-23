package sim

import (
	"math"
	"os"
	"slices"
	"strings"
	"testing"
)

// This file is the Baker Street study of HB-D15 in
// docs/design/london-hubs-d8-d9.md (section 10). It uses the input and the
// environment variables of the hub pair study, and it skips without them.

const studyBaker = "940GZZLUBST"

// bakerLinks are the links at Baker Street.
var bakerLinks = []string{"090", "108", "109", "110", "111", "112", "113"}

// junctionChange holds a change at a station: the station turns about its
// junction center, the two portals of each link turn about the same center,
// in degrees, and each road gets a curve. A link is its number and its end
// at the station, such as "112a". A road is its ID without the station.
type junctionChange struct {
	station string
	turn    float64
	links   map[string]float64
	curves  map[string]float64
	// remove holds movement lanes, such as "04-04".
	remove []string
}

// bakerFix is the change that the study selects at Baker Street.
func bakerFix() junctionChange {
	return junctionChange{
		station: studyBaker, turn: -10,
		links:  map[string]float64{"112a": 40, "111a": 30, "090b": 20},
		curves: map[string]float64{"road-out-04": -20},
	}
}

// apply also makes the roads of the station straight at 14 m/s, as
// straightStudyHubs does at a hub, because the fillets of the generator are
// for the portals of the generator.
func (c junctionChange) apply(t *testing.T, base Network) Network {
	t.Helper()
	station, _ := base.Station(c.station)
	center := sharedTrackStationFrame(t, indexNodePositions(base.Nodes), station).center
	change := sharedTrackChange{hubs: []string{c.station}, turn: map[string]float64{c.station: c.turn}}
	for _, id := range c.remove {
		change.remove = append(change.remove, c.station+"-move-"+id)
	}
	n := change.apply(t, straightStudyHubs(base, c.station))
	index := make(map[string]int, len(n.Nodes))
	for i, node := range n.Nodes {
		index[node.ID] = i
	}
	for id, degrees := range c.links {
		link, end := id[:3], id[3:]
		cos, sin := math.Cos(degrees*math.Pi/180), math.Sin(degrees*math.Pi/180)
		prefix := "london-link-" + link + "-"
		for _, portal := range []string{"-arrival", "-departure"} {
			p := &n.Nodes[index[prefix+end+portal]].Position
			dx, dy := p.X-center.X, p.Y-center.Y
			*p = Point{X: center.X + dx*cos - dy*sin, Y: center.Y + dx*sin + dy*cos}
		}
		// The middle node of each carriageway stays halfway.
		for _, way := range [][3]string{{"a-departure", "a", "b-arrival"}, {"b-departure", "b", "a-arrival"}} {
			from, to := n.Nodes[index[prefix+way[0]]].Position, n.Nodes[index[prefix+way[2]]].Position
			n.Nodes[index["london-mid-"+link+"-"+way[1]]].Position = Point{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
		}
	}
	for road, offset := range c.curves {
		// The curve is after the turns, so it is on the final chord.
		curve := sharedTrackChange{hubs: []string{c.station}, curve: c.station + "-" + road, offset: offset}
		n = curve.apply(t, n)
	}
	junctionMovements(&n, c.station, center)
	return n
}

// junctionMovements gives each movement lane of the station the control of
// londonMovementControl of internal/scenarios.
func junctionMovements(n *Network, station string, center Point) {
	positions := indexNodePositions(n.Nodes)
	for i := range n.Lanes {
		lane := &n.Lanes[i]
		if !strings.HasPrefix(lane.ID, station+"-move-") {
			continue
		}
		lane.Control = nil
		from, to := positions[lane.From], positions[lane.To]
		dx, dy := to.X-from.X, to.Y-from.Y
		if math.Hypot(dx, dy) >= 2*Clearance {
			continue
		}
		control, chord := center, dx*dx+dy*dy
		if projection := (control.X-from.X)*dx + (control.Y-from.Y)*dy; chord > 0 && (projection <= 0 || projection >= chord) {
			shift := 0.5 - projection/chord
			control.X += shift * dx
			control.Y += shift * dy
		}
		lane.Control = &control
	}
}

// bakerRefusals counts the refusals with a lane of Baker Street or of one
// of its links.
func bakerRefusals(result sharedTrackAuditResult) int {
	at := func(id string) bool {
		link, ok := strings.CutPrefix(id, "london-link-")
		return strings.HasPrefix(id, studyBaker+"-") || ok && slices.Contains(bakerLinks, link[:3])
	}
	count := 0
	for _, refusal := range result.refusals {
		if at(refusal.first) || at(refusal.second) {
			count++
		}
	}
	return count
}

// TestBakerStudyMovements checks that junctionMovements and the junction
// center give the controls of the generator on the preset.
func TestBakerStudyMovements(t *testing.T) {
	preset := hubPairLoad(t)
	n := preset.clone()
	for _, change := range pairTwoArms()[1].changes {
		station, _ := preset.Station(change.station)
		junctionMovements(&n, change.station, sharedTrackStationFrame(t, indexNodePositions(n.Nodes), station).center)
	}
	curved := 0
	for i, lane := range preset.Lanes {
		want, got := lane.Control, n.Lanes[i].Control
		if (want == nil) != (got == nil) || want != nil && math.Hypot(want.X-got.X, want.Y-got.Y) > 1e-6 {
			t.Fatalf("%s: control %v, want %v", lane.ID, got, want)
		}
		if strings.HasPrefix(lane.ID, studyBaker+"-move-") && want != nil {
			curved++
		}
	}
	if curved == 0 {
		t.Fatal("no curved movement lane at Baker Street")
	}
}

// TestBakerStudyChange pins the refusals at Baker Street, and all refusals,
// on Paddington and Liverpool Street with the selected change, with each
// part of it left out, and with the straight roads only. It also pins the
// two other pairs through Baker Street with the selected change.
func TestBakerStudyChange(t *testing.T) {
	preset := hubPairLoad(t)
	without := func(link string) junctionChange {
		c := bakerFix()
		delete(c.links, link)
		return c
	}
	noTurn, noCurve := bakerFix(), bakerFix()
	noTurn.turn, noCurve.curves = 0, nil
	main := [2]string{studyPaddington, studyLiverpool}
	for _, test := range []struct {
		name   string
		pair   [2]string
		change junctionChange
		want   [2]int
	}{
		{"straight roads only", main, junctionChange{station: studyBaker}, [2]int{25, 35}},
		{"selected", main, bakerFix(), [2]int{0, 10}},
		{"no turn", main, noTurn, [2]int{2, 12}},
		{"no curve", main, noCurve, [2]int{1, 11}},
		{"no 090", main, without("090b"), [2]int{0, 10}},
		{"no 111", main, without("111a"), [2]int{4, 14}},
		{"no 112", main, without("112a"), [2]int{21, 31}},
		{"selected", [2]string{studyKingsCross, studyPaddington}, bakerFix(), [2]int{0, 7}},
		{"selected", [2]string{studyPaddington, studyTottenham}, bakerFix(), [2]int{0, 7}},
		{"no 090", [2]string{studyPaddington, studyTottenham}, without("090b"), [2]int{9, 16}},
	} {
		n := test.change.apply(t, hubPairShape(t, hubPairBase(t, preset, test.pair), test.pair, sharedTrackChange{}))
		result := hubPairAudit(t, n)
		got := [2]int{bakerRefusals(result), len(result.refusals)}
		t.Logf("%s %v: %d refusals at Baker Street, %d in all, %d shape pairs", test.name, test.pair, got[0], got[1], result.shapes)
		if got != test.want {
			t.Errorf("%s %v: %v, want %v", test.name, test.pair, got, test.want)
		}
		if _, err := prepareNetworkTails(n, nil); err != nil {
			t.Errorf("%s: preparation: %v", test.name, err)
		}
	}
}

// TestBakerStudyCosts logs the free-flow cost of the selected change to the
// station routes of the preset, against the preset and against the preset
// with the straight roads of Baker Street. It writes the changed preset for
// the layout audit.
func TestBakerStudyCosts(t *testing.T) {
	preset := hubPairLoad(t)
	changed := bakerFix().apply(t, preset)
	if err := changed.ValidateBankGeometry(); err != nil {
		t.Fatal(err)
	}
	for _, base := range []struct {
		name    string
		network Network
	}{{"preset", preset}, {"straight roads", junctionChange{station: studyBaker}.apply(t, preset)}} {
		logStudyCosts(t, base.name, newSharedTrackCosts(base.network), newSharedTrackCosts(changed))
	}
	if dir := os.Getenv(hubPairOutEnv); dir != "" {
		writeStudyNetwork(t, dir, "baker", changed)
	}
}

// logStudyCosts logs the change of the free-flow station routes, and fails
// when a route is lost.
func logStudyCosts(t *testing.T, name string, before, after sharedTrackCosts) {
	t.Helper()
	longer, shorter, lost, slow, worst, total, gain := 0, 0, 0, 0, 0.0, 0.0, 0.0
	for i, row := range after.cost {
		for j, cost := range row {
			delta := cost - before.cost[i][j]
			switch {
			case i == j || math.IsInf(before.cost[i][j], 1):
			case math.IsInf(cost, 1):
				lost++
			case delta > 1e-9:
				longer, worst, total = longer+1, max(worst, delta), total+delta
				if delta > 10 {
					slow++
				}
			case delta < -1e-9:
				shorter, gain = shorter+1, gain-delta
			}
		}
	}
	t.Logf("against the %s: %d routes longer (%d by more than 10 s, worst +%.1f s, total +%.0f s), %d shorter (total -%.0f s), %d lost", name, longer, slow, worst, total, shorter, gain, lost)
	if lost != 0 {
		t.Errorf("%d routes lost", lost)
	}
}

// pairTwoArm is a set of changes at the five stations of Paddington and
// Liverpool Street (section 11 of the design). The arms differ at King's
// Cross St. Pancras.
type pairTwoArm struct {
	name    string
	changes []junctionChange
	// others holds the refusals of each pair of hubPairList.
	others []int
}

// pairTwoArms returns the arm that removes movement lanes and the arm that
// turns the two ends of link 194, which also changes Euston.
func pairTwoArms() []pairTwoArm {
	arm := func(name string, others []int, kings ...junctionChange) pairTwoArm {
		changes := []junctionChange{
			bakerFix(),
			{station: studyPaddington, links: map[string]float64{"190b": -30}},
			{station: studyMoorgate, turn: -15, links: map[string]float64{"284b": 25}},
			{station: studyLiverpool, links: map[string]float64{"096b": -10}},
		}
		return pairTwoArm{name: name, changes: append(changes, kings...), others: others}
	}
	return []pairTwoArm{
		arm("removal", []int{1, 1, 0, 0, 7, 7, 4, 11, 14, 18, 23, 31, 1, 0},
			junctionChange{station: studyKingsCross, remove: []string{"03-04", "04-04", "04-07"}}),
		arm("link", []int{1, 1, 0, 2, 8, 6, 4, 11, 18, 18, 27, 31, 2, 0},
			junctionChange{station: studyKingsCross, links: map[string]float64{"194b": 40}},
			junctionChange{station: studyEuston, links: map[string]float64{"194a": 10}}),
	}
}

// TestPairTwoStudy pins the refusals of Paddington and Liverpool Street with
// each arm, and with the change at each station left out. With a whole arm,
// ValidateBankGeometry must accept the network.
func TestPairTwoStudy(t *testing.T) {
	preset := hubPairLoad(t)
	pair := [2]string{studyPaddington, studyLiverpool}
	base := hubPairShape(t, hubPairBase(t, preset, pair), pair, sharedTrackChange{})
	want := map[string]int{studyBaker: 25, studyPaddington: 3, studyKingsCross: 3, studyMoorgate: 3, studyLiverpool: 1}
	for _, arm := range pairTwoArms() {
		apply := func(n Network, without string) Network {
			for _, change := range arm.changes {
				if change.station != without {
					n = change.apply(t, n)
				}
			}
			return n
		}
		for _, change := range append([]junctionChange{{}}, arm.changes...) {
			n := apply(base, change.station)
			result := hubPairAudit(t, n)
			t.Logf("%s without %q: %d refusals, %d shape pairs", arm.name, change.station, len(result.refusals), result.shapes)
			if len(result.refusals) != want[change.station] {
				t.Errorf("%s without %q: %d refusals, want %d", arm.name, change.station, len(result.refusals), want[change.station])
			}
			if _, err := prepareNetworkTails(n, nil); err != nil {
				t.Errorf("%s without %q: preparation: %v", arm.name, change.station, err)
			}
		}
		n, plain, straight := apply(base, ""), apply(preset, ""), base
		for _, network := range []Network{n, plain} {
			if err := network.ValidateBankGeometry(); err != nil {
				t.Error(err)
			}
		}
		for _, change := range arm.changes {
			straight = junctionChange{station: change.station}.apply(t, straight)
		}
		after := newSharedTrackCosts(n)
		logStudyCosts(t, arm.name+": hub shape", newSharedTrackCosts(base), after)
		logStudyCosts(t, arm.name+": hub shape with straight roads", newSharedTrackCosts(straight), after)
		if dir := os.Getenv(hubPairOutEnv); dir != "" {
			writeStudyNetwork(t, dir, "pair-two-"+arm.name, n)
			writeStudyNetwork(t, dir, "pair-two-"+arm.name+"-preset", plain)
		}
	}
}

// TestPairTwoStudyOthers pins the refusals of each pair of the hub pair
// study with each arm, for the effect on the other pairs.
func TestPairTwoStudyOthers(t *testing.T) {
	preset := hubPairLoad(t)
	for _, arm := range pairTwoArms() {
		for i, pair := range hubPairList() {
			n := hubPairShape(t, hubPairBase(t, preset, pair), pair, sharedTrackChange{})
			for _, change := range arm.changes {
				n = change.apply(t, n)
			}
			if got := len(hubPairAudit(t, n).refusals); got != arm.others[i] {
				t.Errorf("%s %v: %d refusals, want %d", arm.name, pair, got, arm.others[i])
			}
		}
	}
}
