package editormodel

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
)

type berthRow struct {
	berth                                     map[string]any
	arrival, departure                        string
	arrivalLink, departureLink, inlet, outlet map[string]any
}

func (g geometryDraft) berthChain(station any) []berthRow {
	core := stationCoreNodes(station)
	from, to := map[string][]map[string]any{}, map[string][]map[string]any{}
	for _, item := range items(g.network["lanes"]) {
		lane := object(item)
		if lane == nil {
			continue
		}
		a, b := text(lane["from"]), text(lane["to"])
		from[a], to[b] = append(from[a], lane), append(to[b], lane)
	}
	link := func(a, b string) map[string]any {
		for _, lane := range from[a] {
			if lane["to"] == b {
				return lane
			}
		}
		return nil
	}
	rows := []berthRow{}
	arrival, departure := text(member(station, "entry")), text(member(station, "exit"))
	for _, item := range items(member(station, "berths")) {
		berth := object(item)
		if berth == nil {
			return nil
		}
		node := text(berth["node"])
		if len(to[node]) != 1 || len(from[node]) != 1 {
			return nil
		}
		row := berthRow{berth: berth, arrival: text(to[node][0]["from"]), departure: text(from[node][0]["to"]), inlet: to[node][0], outlet: from[node][0]}
		if row.arrival == "" || row.departure == "" || row.arrival == row.departure || core[row.arrival] || core[row.departure] {
			return nil
		}
		row.arrivalLink, row.departureLink = link(arrival, row.arrival), link(row.departure, departure)
		if row.arrivalLink == nil || row.departureLink == nil {
			return nil
		}
		rows = append(rows, row)
		arrival, departure = row.arrival, row.departure
	}
	return rows
}

func (g geometryDraft) addBerth(id string) error {
	station, err := g.find("stations", id)
	if err != nil {
		return err
	}
	if has(station, "banks") {
		return errors.New("select a bank to add a berth")
	}
	if len(items(station["berths"])) >= project.MaxBerths {
		return fmt.Errorf("the station already has %d berths", project.MaxBerths)
	}
	oldCounts, _ := g.nodeLaneCounts()
	var newLanes []string
	if rows := g.berthChain(station); len(rows) != 0 {
		newLanes, err = g.addChainBerth(station, rows)
	} else {
		err = g.addStarBerth(station)
	}
	if err != nil {
		return err
	}
	counts, order := g.nodeLaneCounts()
	for _, node := range order {
		if counts[node] > project.MaxNodeLanes && counts[node] > oldCounts[node] {
			return fmt.Errorf("node %s would exceed the %d-lane limit", node, project.MaxNodeLanes)
		}
	}
	if conflict := g.laneConflict(newLanes, false); conflict != nil {
		return fmt.Errorf("new lane %s would come within %g meters of lane %s", conflict.lane, conflict.minimum, conflict.other)
	}
	return nil
}

func (g geometryDraft) nodeLaneCounts() (map[string]int, []string) {
	counts, order := make(map[string]int), []string{}
	for _, lane := range items(g.network["lanes"]) {
		for _, id := range []string{text(member(lane, "from")), text(member(lane, "to"))} {
			if counts[id] == 0 {
				order = append(order, id)
			}
			counts[id]++
		}
	}
	return counts, order
}

func (g geometryDraft) addStarBerth(station map[string]any) error {
	entry, err := g.point(text(station["entry"]))
	if err != nil {
		return err
	}
	exit, err := g.point(text(station["exit"]))
	if err != nil {
		return err
	}
	origin, _, across := geometryAxes(entry, exit)
	depth := func(p geometryPoint) float64 { return (p.X-origin.X)*across.X + (p.Y-origin.Y)*across.Y }
	berths, mean := []geometryPoint{}, 0.0
	for _, berth := range items(station["berths"]) {
		p, pointErr := g.point(text(member(berth, "node")))
		if pointErr == nil {
			berths = append(berths, p)
			mean += depth(p)
		}
	}
	side, start, furthest := 1.0, origin, math.Inf(-1)
	if mean < 0 {
		side = -1
	}
	for _, p := range berths {
		if d := depth(p) * side; d > furthest {
			start, furthest = p, d
		}
	}
	id := text(station["id"])
	berthID := g.nextID(id + "-berth")
	nodeID, err := g.addNode(id+"-berth-node", geometryPoint{start.X + across.X*side*30, start.Y + across.Y*side*30})
	if err != nil {
		return err
	}
	station["berths"] = append(items(station["berths"]), map[string]any{"id": berthID, "node": nodeID})
	if err := g.addLane(text(station["entry"]), nodeID, id, "berth-access", nil); err != nil {
		return err
	}
	return g.addLane(nodeID, text(station["exit"]), id, "departure", nil)
}

func geometryAxes(entry, exit geometryPoint) (origin, along, across geometryPoint) {
	x, y := exit.X-entry.X, exit.Y-entry.Y
	length := math.Hypot(x, y)
	along = geometryPoint{1, 0}
	if length != 0 {
		along = geometryPoint{x / length, y / length}
	}
	return geometryPoint{(entry.X + exit.X) / 2, (entry.Y + exit.Y) / 2}, along, geometryPoint{-along.Y, along.X}
}

func chainID(ids map[string]bool, old, prefix string) string {
	end := len(old)
	for end > 0 && (old[end-1] < '0' || old[end-1] > '9') {
		end--
	}
	start := end
	for start > 0 && old[start-1] >= '0' && old[start-1] <= '9' {
		start--
	}
	id := ""
	if start < end {
		value, _ := strconv.ParseFloat(old[start:end], 64)
		format := byte('f')
		if value+1 >= 1e21 {
			format = 'e'
		}
		number := strconv.FormatFloat(value+1, format, -1, 64)
		id = old[:start] + strings.Repeat("0", max(0, end-start-len(number))) + number + old[end:]
	}
	for next := 1; id == "" || ids[id]; next++ {
		id = prefix + "-" + strconv.Itoa(next)
	}
	ids[id] = true
	return id
}

func (g geometryDraft) addChainBerth(station map[string]any, rows []berthRow) ([]string, error) {
	last := rows[len(rows)-1]
	end, err := g.point(text(last.berth["node"]))
	if err != nil {
		return nil, err
	}
	var start geometryPoint
	if len(rows) > 1 {
		start, err = g.point(text(rows[len(rows)-2].berth["node"]))
	} else {
		entry, entryErr := g.point(text(station["entry"]))
		if entryErr != nil {
			return nil, entryErr
		}
		exit, exitErr := g.point(text(station["exit"]))
		if exitErr != nil {
			return nil, exitErr
		}
		start, _, _ = geometryAxes(entry, exit)
	}
	if err != nil {
		return nil, err
	}
	delta := geometryPoint{end.X - start.X, end.Y - start.Y}
	shift := func(p geometryPoint) (map[string]any, error) {
		return geometryPosition(geometryPoint{p.X + delta.X, p.Y + delta.Y})
	}
	ids, stationID := g.allIDs(), text(station["id"])
	nodes := []string{}
	for index, id := range []string{last.arrival, text(last.berth["node"]), last.departure} {
		old, err := g.find("nodes", id)
		if err != nil {
			return nil, err
		}
		point, err := g.point(id)
		if err != nil {
			return nil, err
		}
		position, err := shift(point)
		if err != nil {
			return nil, err
		}
		prefix := "node"
		if index == 1 {
			prefix = stationID + "-berth-node"
		}
		owned := object(cloneEditValue(old))
		owned["id"], owned["position"] = chainID(ids, id, prefix), position
		g.network["nodes"] = append(items(g.network["nodes"]), owned)
		nodes = append(nodes, text(owned["id"]))
	}
	lanes := []struct {
		old            map[string]any
		from, to, role string
	}{
		{last.arrivalLink, last.arrival, nodes[0], "berth-access"}, {last.departureLink, nodes[2], last.departure, "departure"},
		{last.inlet, nodes[0], nodes[1], "berth-access"}, {last.outlet, nodes[1], nodes[2], "departure"},
	}
	created := []string{}
	for _, lane := range lanes {
		owned := object(cloneEditValue(lane.old))
		id := chainID(ids, text(owned["id"]), "lane")
		owned["id"], owned["from"], owned["to"], owned["stationID"], owned["stationRole"] = id, lane.from, lane.to, stationID, lane.role
		if control := owned["control"]; control != nil {
			if !finite(member(control, "x")) || !finite(member(control, "y")) {
				return nil, errors.New("the lane needs a finite control point")
			}
			position, err := shift(geometryPoint{number(member(control, "x")), number(member(control, "y"))})
			if err != nil {
				return nil, err
			}
			owned["control"] = position
		}
		g.network["lanes"] = append(items(g.network["lanes"]), owned)
		created = append(created, id)
	}
	berth := object(cloneEditValue(last.berth))
	berth["id"], berth["node"] = chainID(ids, text(berth["id"]), stationID+"-berth"), nodes[1]
	station["berths"] = append(items(station["berths"]), berth)
	return created, nil
}

func (g geometryDraft) removeBerth(stationID, berthID string) error {
	station, err := g.find("stations", stationID)
	if err != nil {
		return err
	}
	if berthID == "" {
		return errors.New("a berth edit needs a berth ID")
	}
	berths := items(station["berths"])
	index := slices.IndexFunc(berths, func(berth any) bool { return member(berth, "id") == berthID })
	if index < 0 {
		return nil
	}
	if len(berths) <= 1 {
		if has(station, "banks") {
			return fmt.Errorf("station %s must retain one berth", stationID)
		}
		return nil
	}
	node := text(member(berths[index], "node"))
	if nodeErr := g.checkBerthNode(stationID, node); nodeErr != nil {
		return nodeErr
	}
	gone := berthRemoval{nodes: map[string]bool{node: true}, lanes: map[string]bool{}}
	chainStation, owningBank, err := berthOwner(station, berthID)
	if err != nil {
		return err
	}
	if err := g.markLastBerthRow(chainStation, berthID, gone); err != nil {
		return err
	}
	if owningBank != nil {
		if err := g.removeBankBerth(station, owningBank, berthID, gone); err != nil {
			return err
		}
	}
	station["berths"] = slices.Delete(berths, index, index+1)
	g.network["nodes"] = slices.DeleteFunc(items(g.network["nodes"]), func(item any) bool { return gone.nodes[text(member(item, "id"))] })
	g.network["lanes"] = slices.DeleteFunc(items(g.network["lanes"]), func(item any) bool {
		return member(item, "from") == node || member(item, "to") == node || gone.lanes[text(member(item, "id"))]
	})
	g.removeBerthPods(berthID)
	return nil
}

// berthRemoval holds the IDs of the nodes and the lanes that a berth removal
// deletes.
type berthRemoval struct {
	nodes, lanes map[string]bool
}

// checkBerthNode refuses a berth node that does not exist, or that has a
// lane of a different station.
func (g geometryDraft) checkBerthNode(stationID, node string) error {
	if _, err := g.point(node); err != nil {
		return err
	}
	for _, lane := range items(g.network["lanes"]) {
		if (member(lane, "from") == node || member(lane, "to") == node) && member(lane, "stationID") != stationID {
			return fmt.Errorf("delete nonstation lane %s at berth node %s of station %s first", text(member(lane, "id")), node, stationID)
		}
	}
	return nil
}

// berthOwner returns the station form that holds the berth chain of berthID,
// and the bank that owns the berth. A station without banks has no owning
// bank, and it holds its own chain.
func berthOwner(station map[string]any, berthID string) (map[string]any, map[string]any, error) {
	if !has(station, "banks") {
		return station, nil, nil
	}
	for _, bank := range items(station["banks"]) {
		if slices.ContainsFunc(items(member(bank, "berthIDs")), func(id any) bool { return id == berthID }) {
			return bankStation(station, object(bank)), object(bank), nil
		}
	}
	return nil, nil, fmt.Errorf("berth %s of station %s has no owning bank", berthID, text(station["id"]))
}

// markLastBerthRow adds the nodes and the lanes of the last row of the
// berth chain to gone when that row holds berthID. It refuses a row whose
// nodes have other lanes, and names the first such lane and its row node.
func (g geometryDraft) markLastBerthRow(chainStation map[string]any, berthID string, gone berthRemoval) error {
	rows := g.berthChain(chainStation)
	if len(rows) == 0 {
		return nil
	}
	last := rows[len(rows)-1]
	if last.berth["id"] != berthID {
		return nil
	}
	gone.nodes[last.arrival], gone.nodes[last.departure] = true, true
	for _, lane := range []map[string]any{last.arrivalLink, last.departureLink, last.inlet, last.outlet} {
		gone.lanes[text(lane["id"])] = true
	}
	for _, lane := range items(g.network["lanes"]) {
		if !gone.lanes[text(member(lane, "id"))] && (member(lane, "from") == last.arrival || member(lane, "to") == last.arrival || member(lane, "from") == last.departure || member(lane, "to") == last.departure) {
			node := last.departure
			if member(lane, "from") == last.arrival || member(lane, "to") == last.arrival {
				node = last.arrival
			}
			return fmt.Errorf("delete shared lane %s at berth row node %s of station %s first", text(member(lane, "id")), node, text(chainStation["id"]))
		}
	}
	return nil
}

// removeBankBerth removes berthID from bank. A bank with no berths left goes
// from station, and its gate nodes and their lanes go to gone. It refuses a
// gate node with a lane of a different station.
func (g geometryDraft) removeBankBerth(station, bank map[string]any, berthID string, gone berthRemoval) error {
	bank["berthIDs"] = slices.DeleteFunc(items(bank["berthIDs"]), func(id any) bool { return id == berthID })
	if len(items(bank["berthIDs"])) != 0 {
		return nil
	}
	for _, key := range []string{"entry", "exit"} {
		gone.nodes[text(bank[key])] = true
	}
	for _, lane := range items(g.network["lanes"]) {
		from, to := text(member(lane, "from")), text(member(lane, "to"))
		if gone.nodes[from] || gone.nodes[to] {
			if member(lane, "stationID") != member(station, "id") {
				node := to
				if gone.nodes[from] {
					node = from
				}
				return fmt.Errorf("delete nonstation lane %s at node %s of bank %s of station %s first", text(member(lane, "id")), node, text(bank["id"]), text(station["id"]))
			}
			gone.lanes[text(member(lane, "id"))] = true
		}
	}
	station["banks"] = slices.DeleteFunc(items(station["banks"]), func(item any) bool { return member(item, "id") == bank["id"] })
	refreshBankAliases(station)
	return nil
}

// removeBerthPods removes the pods of the fleet at berthID. The patch gets
// the fleet only when a pod goes.
func (g geometryDraft) removeBerthPods(berthID string) {
	fleet := items(cloneEditValue(member(g.draft, "fleet")))
	fleet = slices.DeleteFunc(fleet, func(pod any) bool { return member(pod, "berthID") == berthID })
	if !reflect.DeepEqual(items(member(g.draft, "fleet")), fleet) {
		g.patch["fleet"] = fleet
	}
}
