package sim

import (
	"math"
	"os"
	"strings"
	"testing"
)

// This file is the Baker Street study of HB-D15 in
// docs/design/london-hubs-d8-d9.md (section 10). It uses the input and the
// environment variables of the hub pair study, and it skips without them.

const studyBaker = "940GZZLUBST"

// bakerLinks are the links at Baker Street, with the end at the station.
var bakerLinks = map[string]string{"090": "b", "108": "a", "109": "a", "110": "a", "111": "a", "112": "a", "113": "a"}

// bakerChange holds a change at Baker Street. The road of link 110 gets a
// curve, the station turns about its junction center, and the two portals of
// each link turn about the same center, in degrees.
type bakerChange struct {
	turn, curve float64
	links       map[string]float64
}

// bakerFix is the change that the study selects.
func bakerFix() bakerChange {
	return bakerChange{turn: -10, curve: -20, links: map[string]float64{"112": 40, "111": 30, "090": 20}}
}

// apply also makes the roads of Baker Street straight at 14 m/s, as
// straightStudyHubs does at a hub, because the fillets of the generator are
// for the portals of the generator.
func (c bakerChange) apply(t *testing.T, base Network) Network {
	t.Helper()
	station, _ := base.Station(studyBaker)
	center := sharedTrackStationFrame(t, indexNodePositions(base.Nodes), station).center
	change := sharedTrackChange{hubs: []string{studyBaker}, turn: map[string]float64{studyBaker: c.turn}}
	if c.curve != 0 {
		change.curve, change.offset = studyBaker+"-road-out-04", c.curve
	}
	n := change.apply(t, straightStudyHubs(base, studyBaker))
	index := make(map[string]int, len(n.Nodes))
	for i, node := range n.Nodes {
		index[node.ID] = i
	}
	for link, degrees := range c.links {
		cos, sin := math.Cos(degrees*math.Pi/180), math.Sin(degrees*math.Pi/180)
		prefix := "london-link-" + link + "-"
		for _, portal := range []string{"-arrival", "-departure"} {
			p := &n.Nodes[index[prefix+bakerLinks[link]+portal]].Position
			dx, dy := p.X-center.X, p.Y-center.Y
			*p = Point{X: center.X + dx*cos - dy*sin, Y: center.Y + dx*sin + dy*cos}
		}
		// The middle node of each carriageway stays halfway.
		for _, way := range [][3]string{{"a-departure", "a", "b-arrival"}, {"b-departure", "b", "a-arrival"}} {
			from, to := n.Nodes[index[prefix+way[0]]].Position, n.Nodes[index[prefix+way[2]]].Position
			n.Nodes[index["london-mid-"+link+"-"+way[1]]].Position = Point{X: (from.X + to.X) / 2, Y: (from.Y + to.Y) / 2}
		}
	}
	bakerMovements(&n, center)
	return n
}

// bakerMovements gives each movement lane of Baker Street the control of
// londonMovementControl of internal/scenarios.
func bakerMovements(n *Network, center Point) {
	positions := indexNodePositions(n.Nodes)
	for i := range n.Lanes {
		lane := &n.Lanes[i]
		if !strings.HasPrefix(lane.ID, studyBaker+"-move-") {
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
		return strings.HasPrefix(id, studyBaker+"-") || ok && bakerLinks[link[:3]] != ""
	}
	count := 0
	for _, refusal := range result.refusals {
		if at(refusal.first) || at(refusal.second) {
			count++
		}
	}
	return count
}

// TestBakerStudyMovements checks that bakerMovements and the junction
// center give the controls of the generator on the preset.
func TestBakerStudyMovements(t *testing.T) {
	preset := hubPairLoad(t)
	station, _ := preset.Station(studyBaker)
	n := preset.clone()
	bakerMovements(&n, sharedTrackStationFrame(t, indexNodePositions(n.Nodes), station).center)
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
	without := func(link string) bakerChange {
		c := bakerFix()
		delete(c.links, link)
		return c
	}
	noTurn, noCurve := bakerFix(), bakerFix()
	noTurn.turn, noCurve.curve = 0, 0
	main := [2]string{studyPaddington, studyLiverpool}
	for _, test := range []struct {
		name   string
		pair   [2]string
		change bakerChange
		want   [2]int
	}{
		{"straight roads only", main, bakerChange{}, [2]int{25, 35}},
		{"selected", main, bakerFix(), [2]int{0, 10}},
		{"no turn", main, noTurn, [2]int{2, 12}},
		{"no curve", main, noCurve, [2]int{1, 11}},
		{"no 090", main, without("090"), [2]int{0, 10}},
		{"no 111", main, without("111"), [2]int{4, 14}},
		{"no 112", main, without("112"), [2]int{21, 31}},
		{"selected", [2]string{studyKingsCross, studyPaddington}, bakerFix(), [2]int{0, 7}},
		{"selected", [2]string{studyPaddington, studyTottenham}, bakerFix(), [2]int{0, 7}},
		{"no 090", [2]string{studyPaddington, studyTottenham}, without("090"), [2]int{9, 16}},
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
	after := newSharedTrackCosts(changed)
	for _, base := range []struct {
		name    string
		network Network
	}{{"preset", preset}, {"straight roads", bakerChange{}.apply(t, preset)}} {
		before := newSharedTrackCosts(base.network)
		longer, shorter, lost, worst, total, gain := 0, 0, 0, 0.0, 0.0, 0.0
		for i, row := range after.cost {
			for j, cost := range row {
				delta := cost - before.cost[i][j]
				switch {
				case i == j || math.IsInf(before.cost[i][j], 1):
				case math.IsInf(cost, 1):
					lost++
				case delta > 1e-9:
					longer, worst, total = longer+1, max(worst, delta), total+delta
				case delta < -1e-9:
					shorter, gain = shorter+1, gain-delta
				}
			}
		}
		t.Logf("against the %s: %d routes longer (worst +%.1f s, total +%.0f s), %d shorter (total -%.0f s), %d lost", base.name, longer, worst, total, shorter, gain, lost)
		if lost != 0 {
			t.Errorf("%d routes lost", lost)
		}
	}
	if dir := os.Getenv(hubPairOutEnv); dir != "" {
		writeStudyNetwork(t, dir, "baker", changed)
	}
}
