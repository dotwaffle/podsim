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
		return errors.New("the station already has 200 berths")
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
			return fmt.Errorf("node %s would exceed the 64-lane limit", node)
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
			return errors.New("the station must retain one berth")
		}
		return nil
	}
	node := text(member(berths[index], "node"))
	if _, nodeErr := g.point(node); nodeErr != nil {
		return nodeErr
	}
	for _, lane := range items(g.network["lanes"]) {
		if (member(lane, "from") == node || member(lane, "to") == node) && member(lane, "stationID") != stationID {
			return errors.New("delete nonstation lanes at the berth node first")
		}
	}
	goneNodes, goneLanes := map[string]bool{node: true}, map[string]bool{}
	chainStation := station
	var owningBank map[string]any
	if has(station, "banks") {
		for _, bank := range items(station["banks"]) {
			if slices.ContainsFunc(items(member(bank, "berthIDs")), func(id any) bool { return id == berthID }) {
				owningBank = object(bank)
				chainStation = bankStation(station, owningBank)
				break
			}
		}
		if owningBank == nil {
			return errors.New("the berth has no owning bank")
		}
	}
	if rows := g.berthChain(chainStation); len(rows) != 0 {
		last := rows[len(rows)-1]
		if last.berth["id"] == berthID {
			goneNodes[last.arrival], goneNodes[last.departure] = true, true
			for _, lane := range []map[string]any{last.arrivalLink, last.departureLink, last.inlet, last.outlet} {
				goneLanes[text(lane["id"])] = true
			}
			for _, lane := range items(g.network["lanes"]) {
				if !goneLanes[text(member(lane, "id"))] && (member(lane, "from") == last.arrival || member(lane, "to") == last.arrival || member(lane, "from") == last.departure || member(lane, "to") == last.departure) {
					return errors.New("delete shared lanes at the berth row nodes first")
				}
			}
		}
	}
	if owningBank != nil {
		owningBank["berthIDs"] = slices.DeleteFunc(items(owningBank["berthIDs"]), func(id any) bool { return id == berthID })
		if len(items(owningBank["berthIDs"])) == 0 {
			for _, key := range []string{"entry", "exit"} {
				goneNodes[text(owningBank[key])] = true
			}
			for _, lane := range items(g.network["lanes"]) {
				if goneNodes[text(member(lane, "from"))] || goneNodes[text(member(lane, "to"))] {
					if member(lane, "stationID") != stationID {
						return errors.New("delete nonstation lanes at the bank gates first")
					}
					goneLanes[text(member(lane, "id"))] = true
				}
			}
			station["banks"] = slices.DeleteFunc(items(station["banks"]), func(bank any) bool { return member(bank, "id") == owningBank["id"] })
			refreshBankAliases(station)
		}
	}
	station["berths"] = slices.Delete(berths, index, index+1)
	g.network["nodes"] = slices.DeleteFunc(items(g.network["nodes"]), func(item any) bool { return goneNodes[text(member(item, "id"))] })
	g.network["lanes"] = slices.DeleteFunc(items(g.network["lanes"]), func(item any) bool {
		return member(item, "from") == node || member(item, "to") == node || goneLanes[text(member(item, "id"))]
	})
	fleet := items(cloneEditValue(member(g.draft, "fleet")))
	fleet = slices.DeleteFunc(fleet, func(pod any) bool { return member(pod, "berthID") == berthID })
	if !reflect.DeepEqual(items(member(g.draft, "fleet")), fleet) {
		g.patch["fleet"] = fleet
	}
	return nil
}
