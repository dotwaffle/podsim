package editormodel

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
	"math"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// editTimeSamples is the number of timed edits of each network, mode, and
// verdict.
const editTimeSamples = 20

// refusedEditGap is the gap of a refused edit between a moved lane and
// the nearest lane that shares no node with it.
const refusedEditGap = 3.0

// editTimeNetworks are the networks of the edit time measurement, in the
// order of section 6.1 of the shape clearance always-on proposal.
var editTimeNetworks = []struct {
	name   string
	config func() (project.Config, error)
}{
	{"example", func() (project.Config, error) { return project.Default(), nil }},
	{"small", preset(scenarios.Small)},
	{"busy", preset(scenarios.Busy)},
	{"parking-constrained", preset(scenarios.ParkingConstrained)},
	{"rail-hub", preset(scenarios.RailHub)},
	{"rail-hub-incidents", preset(scenarios.RailHubIncidents)},
	{"scale100", preset(scenarios.Scale100)},
	{"independent-banks", func() (project.Config, error) {
		config := project.Default()
		config.Name, config.Network = "Independent station banks", sim.BankExample()
		config.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}, {ID: "02", StationID: "hub", BerthID: "bank-a-1"}, {ID: "03", StationID: "parking", BerthID: "parking-1"}}
		return config, nil
	}},
	{"compact-station", scenarios.CompactStation},
	{"london-central", scenarios.GenerateLondonCentral},
	{"london-full", scenarios.GenerateLondonFull},
	{"london-full-express", scenarios.GenerateLondonFullExpress},
	{"london-full-shared-express", scenarios.GenerateLondonFullSharedExpress},
	{"limits", func() (project.Config, error) { return limitsProject(), nil }},
}

func preset(config func() project.Config) func() (project.Config, error) {
	return func() (project.Config, error) { return config(), nil }
}

// limitsProject returns the plain project of TestValidateAcceptsNetworkLimits
// in package project: the example, and MaxNodes nodes and MaxLanes lanes
// in all on two circles far from the stations.
func limitsProject() project.Config {
	config := project.Default()
	half := (project.MaxNodes - len(config.Network.Nodes)) / 2
	inner := 40 * float64(half) / (2 * math.Pi)
	for ring := range 2 {
		for index := range half {
			angle := 2 * math.Pi * float64(index) / float64(half)
			radius := inner + 40*float64(ring)
			config.Network.Nodes = append(config.Network.Nodes, sim.Node{
				ID: fmt.Sprintf("far-%d", ring*half+index), Position: sim.Point{X: radius * math.Cos(angle), Y: radius * math.Sin(angle)},
			})
		}
	}
	lane := func(from, to int) {
		config.Network.Lanes = append(config.Network.Lanes, sim.Lane{
			ID: fmt.Sprintf("far-%d", len(config.Network.Lanes)), From: fmt.Sprintf("far-%d", from), To: fmt.Sprintf("far-%d", to), SpeedLimit: 12,
		})
	}
	for index := range half {
		lane(index, (index+1)%half)
		lane(half+index, half+(index+1)%half)
		lane(index, half+index)
	}
	for index := 0; len(config.Network.Lanes) < project.MaxLanes; index++ {
		lane(half+index, index)
	}
	return config
}

// editTimer sends the requests of the browser worker to one engine.
type editTimer struct {
	t    *testing.T
	call func(string) string
	keys string
}

// newEditTimer returns an engine that holds the project data.
func newEditTimer(t *testing.T, data []byte, keys string) *editTimer {
	t.Helper()
	timer := &editTimer{t: t, call: NewCall(), keys: keys}
	var reply struct {
		Valid bool   `json:"valid"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(timer.call(`{"op":"sync","keys":`+keys+`,"patch":`+string(data)+`}`)), &reply); err != nil {
		t.Fatal(err)
	}
	if !reply.Valid {
		t.Fatalf("the project is not valid: %s", reply.Error)
	}
	return timer
}

// edit sends one geometry edit and returns its time, the network of the
// reply, and the error text of the reply.
func (e *editTimer) edit(edit string) (time.Duration, jsontext.Value, string) {
	e.t.Helper()
	request := `{"op":"edit","edit":{"field":"geometry","value":` + edit + `}}`
	start := time.Now()
	text := e.call(request)
	spent := time.Since(start)
	var reply cycleReply
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		e.t.Fatal(err)
	}
	return spent, reply.Change.Patch["network"], reply.Error
}

// accept sends an edit that the engine must accept, and synchronizes the
// network of the reply, as the browser does. It returns the time of the
// edit.
func (e *editTimer) accept(edit string) time.Duration {
	e.t.Helper()
	spent, network, refusal := e.edit(edit)
	if refusal != "" || len(network) == 0 {
		e.t.Fatalf("%s: refused: %s", edit, refusal)
	}
	var reply struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(e.call(`{"op":"sync","keys":`+e.keys+`,"patch":{"network":`+string(network)+`}}`)), &reply); err != nil {
		e.t.Fatal(err)
	}
	if reply.Error != "" {
		e.t.Fatalf("%s: synchronization: %s", edit, reply.Error)
	}
	return spent
}

// moveNodeEdit returns the geometry edit that moves node to point.
func moveNodeEdit(node string, point sim.Point) string {
	return fmt.Sprintf(`{"action":"moveNode","id":%s,"point":{"x":%s,"y":%s}}`, strconv.Quote(node),
		strconv.FormatFloat(point.X, 'f', -1, 64), strconv.FormatFloat(point.Y, 'f', -1, 64))
}

// editTimeStats returns the nearest-rank p95 and the maximum of samples.
func editTimeStats(samples []time.Duration) (p95, maximum time.Duration) {
	sorted := slices.Sorted(slices.Values(samples))
	rank := int(math.Ceil(0.95 * float64(len(sorted))))
	return sorted[rank-1], sorted[len(sorted)-1]
}

// isLaneGapRefusal reports whether text is the refusal of a lane that
// comes too close to another lane: the network audit of the server, or the
// local lane check of the editor.
func isLaneGapRefusal(text string) bool {
	return strings.Contains(text, "meters apart") || strings.Contains(text, "would come within")
}

// TestDraftEditTimes measures the time of one geometry edit in the editor
// engine, by the protocol of section 6.1 of the shape clearance always-on
// proposal. It skips unless PODSIM_EDIT_TIMES is 1. For each network, it
// finds a node that the engine lets move 0.5 m, and a move of a node that
// puts one of its lanes 3.0 m from a lane that shares no node with it. A
// cold sample is the edit call of a new engine, with no kept network. A
// cached sample is the edit call of one engine, after a first edit, and
// the engine synchronizes the network of each accepted edit. Each set has
// editTimeSamples samples, and the test logs the nearest-rank p95 and the
// maximum of each set. A refused edit must have a lane gap refusal. A
// draft with no server network check can accept it: the test logs the
// verdict of each set.
func TestDraftEditTimes(t *testing.T) {
	if os.Getenv("PODSIM_EDIT_TIMES") != "1" {
		t.Skip("set PODSIM_EDIT_TIMES=1 to measure the edit times")
	}
	t.Logf("GOOS %s, GOARCH %s, GOMAXPROCS %d, NumCPU %d, %s", runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), runtime.NumCPU(), runtime.Version())
	rows := []string{"| Network | Mode | Edit | Verdict | p95 ms | Max ms |", "| --- | --- | --- | --- | ---: | ---: |"}
	for _, network := range editTimeNetworks {
		if !t.Run(network.name, func(t *testing.T) {
			config, err := network.config()
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, measureEditTimes(t, network.name, config)...)
		}) {
			break
		}
	}
	t.Log("\n" + strings.Join(rows, "\n"))
}

// measureEditTimes measures the edit times of one network, and returns the
// rows of the result table.
func measureEditTimes(t *testing.T, name string, config project.Config) []string {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]jsontext.Value
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	keys, err := json.Marshal(slices.Sorted(maps.Keys(fields)))
	if err != nil {
		t.Fatal(err)
	}
	accepted, refused := editTimeMoves(t, newEditTimer(t, data, string(keys)), config.Network)
	t.Logf("%s: accepted edits move %s, refused edit %s", name, accepted.node, refused)
	var rows []string
	row := func(mode, edit string, samples []time.Duration, verdicts map[string]int) {
		p95, maximum := editTimeStats(samples)
		verdict := strings.Join(slices.Sorted(maps.Keys(verdicts)), ", ")
		ms := func(d time.Duration) string { return strconv.FormatFloat(float64(d.Microseconds())/1000, 'f', 1, 64) }
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %s | %s |", name, mode, edit, verdict, ms(p95), ms(maximum)))
		t.Logf("%s %s %s: %s, p95 %s ms, max %s ms", name, mode, edit, verdict, ms(p95), ms(maximum))
	}
	// refusedSample records the verdict of a refused edit.
	refusedSample := func(verdicts map[string]int, refusal string) {
		switch {
		case refusal == "":
			verdicts["accepted"]++
		case isLaneGapRefusal(refusal):
			verdicts["refused"]++
		default:
			t.Fatalf("the refused edit %s has another refusal: %s", refused, refusal)
		}
	}
	// Cold: a new engine for each edit.
	var samples []time.Duration
	for range editTimeSamples {
		spent, network, refusal := newEditTimer(t, data, string(keys)).edit(accepted.edit(1))
		if refusal != "" || len(network) == 0 {
			t.Fatalf("cold: %s: refused: %s", accepted.edit(1), refusal)
		}
		samples = append(samples, spent)
	}
	row("cold", "accepted", samples, map[string]int{"accepted": len(samples)})
	samples, verdicts := nil, map[string]int{}
	for range editTimeSamples {
		spent, _, refusal := newEditTimer(t, data, string(keys)).edit(refused)
		refusedSample(verdicts, refusal)
		samples = append(samples, spent)
	}
	row("cold", "refused", samples, verdicts)
	// Cached: one engine. The first edit moves the node and the second
	// moves it back, so the engine holds the server checks of the network
	// of the start.
	timer := newEditTimer(t, data, string(keys))
	timer.accept(accepted.edit(1))
	timer.accept(accepted.edit(0))
	samples, verdicts = nil, map[string]int{}
	for range editTimeSamples {
		spent, _, refusal := timer.edit(refused)
		refusedSample(verdicts, refusal)
		samples = append(samples, spent)
	}
	row("cached", "refused", samples, verdicts)
	samples = nil
	for index := range editTimeSamples {
		samples = append(samples, timer.accept(accepted.edit((index+1)%2)))
	}
	row("cached", "accepted", samples, map[string]int{"accepted": len(samples)})
	return rows
}

// acceptedMove is a node that the engine lets move 0.5 m in x.
type acceptedMove struct {
	node  string
	start sim.Point
}

// edit returns the edit that moves the node to its start plus step times
// 0.5 m in x.
func (m acceptedMove) edit(step int) string {
	return moveNodeEdit(m.node, sim.Point{X: m.start.X + 0.5*float64(step), Y: m.start.Y})
}

// editTimeMoves returns the accepted move and the refused edit of network.
// The candidate nodes come in order: plain nodes, which no station, berth,
// maneuver group, depot, separation group, or vehicle class reads, and no station
// lane has as an end; then the other nodes that are not a station entry or
// exit and that no maneuver group, depot, separation group, or vehicle
// class reads; then the other nodes. timer tests the first 20 candidates
// for the accepted move, and each refused edit: the engine must accept it
// or refuse it with a lane gap refusal.
func editTimeMoves(t *testing.T, timer *editTimer, network sim.Network) (acceptedMove, string) {
	t.Helper()
	ends, stationLane, restricted := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, station := range network.Stations {
		ends[station.Entry], ends[station.Exit] = true, true
		for _, berth := range station.Berths {
			stationLane[berth.Node] = true
		}
	}
	depots := map[string]bool{}
	for _, depot := range network.Depots {
		depots[depot.Station] = true
	}
	grouped := map[string]bool{}
	for _, group := range network.ManeuverGroups {
		for _, id := range slices.Concat(group.LaneIDs, group.ReverseLaneIDs) {
			grouped[id] = true
		}
	}
	positions := map[string]sim.Point{}
	for _, node := range network.Nodes {
		positions[node.ID] = node.Position
	}
	incident := map[string][]sim.Lane{}
	for _, lane := range network.Lanes {
		if grouped[lane.ID] || depots[lane.StationID] || lane.VehicleClasses != 0 || lane.SeparationGroup != "" {
			restricted[lane.From], restricted[lane.To] = true, true
		}
		if lane.StationID != "" {
			stationLane[lane.From], stationLane[lane.To] = true, true
		}
		incident[lane.From] = append(incident[lane.From], lane)
		if lane.To != lane.From {
			incident[lane.To] = append(incident[lane.To], lane)
		}
	}
	tiers := make([][]string, 3)
	for _, node := range network.Nodes {
		id := node.ID
		switch {
		case len(incident[id]) == 0:
		case !restricted[id] && !ends[id] && !stationLane[id]:
			tiers[0] = append(tiers[0], id)
		case !restricted[id] && !ends[id]:
			tiers[1] = append(tiers[1], id)
		default:
			tiers[2] = append(tiers[2], id)
		}
	}
	plain := slices.Concat(tiers...)
	accepted := acceptedMove{node: ""}
	for _, id := range plain[:min(len(plain), 20)] {
		move := acceptedMove{node: id, start: positions[id]}
		if _, network, refusal := timer.edit(move.edit(1)); refusal == "" && len(network) > 0 {
			accepted = move
			break
		}
	}
	if accepted.node == "" {
		t.Fatal("no candidate node can move 0.5 m")
	}
	for _, id := range plain {
		if edit, ok := refusedMove(network, positions, incident, id); ok {
			if _, _, refusal := timer.edit(edit); refusal == "" || isLaneGapRefusal(refusal) {
				return accepted, edit
			}
		}
	}
	t.Fatal("no candidate node can move 3.0 m from a lane")
	return acceptedMove{}, ""
}

// lanePolyline returns the polyline of lane with the node positions.
func lanePolyline(lane sim.Lane, positions map[string]sim.Point) []geometryPoint {
	from, to := positions[lane.From], positions[lane.To]
	var control *geometryPoint
	if lane.Control != nil {
		control = &geometryPoint{lane.Control.X, lane.Control.Y}
	}
	return geometryPolyline(geometryPoint{from.X, from.Y}, geometryPoint{to.X, to.Y}, control)
}

// refusedMove returns the edit that moves node so that a lane at node is
// refusedEditGap from a straight lane that shares no node with a lane at
// node. It tries the 10 nearest such lanes. The node moves on the normal of
// the target lane through the point of the lane nearest to the node, to
// the distance where the nearest lane at node is exactly that gap from the
// target lane. Each lane at node must stay 30 m long or more.
func refusedMove(network sim.Network, positions map[string]sim.Point, incident map[string][]sim.Lane, node string) (string, bool) {
	near := map[string]bool{node: true}
	for _, lane := range incident[node] {
		near[lane.From], near[lane.To] = true, true
	}
	at := positions[node]
	type candidate struct {
		index int
		gap   float64
	}
	var candidates []candidate
	for index, lane := range network.Lanes {
		if lane.Control != nil || near[lane.From] || near[lane.To] || lane.SeparationGroup != "" {
			continue
		}
		a, b := positions[lane.From], positions[lane.To]
		if gap := geometryPointGap(geometryPoint{at.X, at.Y}, geometryPoint{a.X, a.Y}, geometryPoint{b.X, b.Y}); gap > 15 {
			candidates = append(candidates, candidate{index, gap})
		}
	}
	slices.SortFunc(candidates, func(x, y candidate) int { return cmp.Compare(x.gap, y.gap) })
	for _, c := range candidates[:min(len(candidates), 10)] {
		target := network.Lanes[c.index]
		a, b := positions[target.From], positions[target.To]
		dx, dy := b.X-a.X, b.Y-a.Y
		length := math.Hypot(dx, dy)
		if length < 10 {
			continue
		}
		s := min(max(((at.X-a.X)*dx+(at.Y-a.Y)*dy)/(length*length), 4/length), 1-4/length)
		foot := sim.Point{X: a.X + s*dx, Y: a.Y + s*dy}
		nx, ny := -dy/length, dx/length
		if (at.X-foot.X)*nx+(at.Y-foot.Y)*ny < 0 {
			nx, ny = -nx, -ny
		}
		edited := maps.Clone(positions)
		// gap returns the gap of the nearest lane at node from the target
		// lane, with the node at distance d on the normal, or -1 when a lane
		// at node is shorter than 30 m.
		gap := func(d float64) float64 {
			edited[node] = sim.Point{X: foot.X + d*nx, Y: foot.Y + d*ny}
			minimum := math.Inf(1)
			for _, lane := range incident[node] {
				points := lanePolyline(lane, edited)
				size := 0.0
				for index := 1; index < len(points); index++ {
					size += math.Hypot(points[index].X-points[index-1].X, points[index].Y-points[index-1].Y)
				}
				if size < 30 {
					return -1
				}
				minimum = min(minimum, geometryPathGap(points, lanePolyline(target, edited)))
			}
			return minimum
		}
		low, high := refusedEditGap, c.gap
		if gap(low) < 0 || gap(high) < refusedEditGap {
			continue
		}
		for range 100 {
			middle := (low + high) / 2
			if gap(middle) < refusedEditGap {
				low = middle
			} else {
				high = middle
			}
		}
		if got := gap(high); got >= 0 && math.Abs(got-refusedEditGap) <= 1e-6 {
			return moveNodeEdit(node, edited[node]), true
		}
	}
	return "", false
}
