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
	for _, item := range items(g.network["Lanes"]) {
		lane := object(item)
		if lane == nil {
			continue
		}
		a, b := text(lane["From"]), text(lane["To"])
		from[a], to[b] = append(from[a], lane), append(to[b], lane)
	}
	link := func(a, b string) map[string]any {
		for _, lane := range from[a] {
			if lane["To"] == b {
				return lane
			}
		}
		return nil
	}
	rows := []berthRow{}
	arrival, departure := text(member(station, "Entry")), text(member(station, "Exit"))
	for _, item := range items(member(station, "Berths")) {
		berth := object(item)
		if berth == nil {
			return nil
		}
		node := text(berth["Node"])
		if len(to[node]) != 1 || len(from[node]) != 1 {
			return nil
		}
		row := berthRow{berth: berth, arrival: text(to[node][0]["From"]), departure: text(from[node][0]["To"]), inlet: to[node][0], outlet: from[node][0]}
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
	station, err := g.find("Stations", id)
	if err != nil {
		return err
	}
	if has(station, "Banks") {
		return errors.New("select a bank to add a berth")
	}
	if len(items(station["Berths"])) >= project.MaxBerths {
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
		return fmt.Errorf("new lane %s would come within 12 meters of lane %s", conflict.lane, conflict.other)
	}
	return nil
}

func (g geometryDraft) nodeLaneCounts() (map[string]int, []string) {
	counts, order := make(map[string]int), []string{}
	for _, lane := range items(g.network["Lanes"]) {
		for _, id := range []string{text(member(lane, "From")), text(member(lane, "To"))} {
			if counts[id] == 0 {
				order = append(order, id)
			}
			counts[id]++
		}
	}
	return counts, order
}

func (g geometryDraft) addStarBerth(station map[string]any) error {
	entry, err := g.point(text(station["Entry"]))
	if err != nil {
		return err
	}
	exit, err := g.point(text(station["Exit"]))
	if err != nil {
		return err
	}
	origin, _, across := geometryAxes(entry, exit)
	depth := func(p geometryPoint) float64 { return (p.X-origin.X)*across.X + (p.Y-origin.Y)*across.Y }
	berths, mean := []geometryPoint{}, 0.0
	for _, berth := range items(station["Berths"]) {
		p, pointErr := g.point(text(member(berth, "Node")))
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
	id := text(station["ID"])
	berthID := g.nextID(id + "-berth")
	nodeID, err := g.addNode(id+"-berth-node", geometryPoint{start.X + across.X*side*30, start.Y + across.Y*side*30})
	if err != nil {
		return err
	}
	station["Berths"] = append(items(station["Berths"]), map[string]any{"ID": berthID, "Node": nodeID})
	if err := g.addLane(text(station["Entry"]), nodeID, id, "berth-access", nil); err != nil {
		return err
	}
	return g.addLane(nodeID, text(station["Exit"]), id, "departure", nil)
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
	end, err := g.point(text(last.berth["Node"]))
	if err != nil {
		return nil, err
	}
	var start geometryPoint
	if len(rows) > 1 {
		start, err = g.point(text(rows[len(rows)-2].berth["Node"]))
	} else {
		entry, entryErr := g.point(text(station["Entry"]))
		if entryErr != nil {
			return nil, entryErr
		}
		exit, exitErr := g.point(text(station["Exit"]))
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
	ids, stationID := g.allIDs(), text(station["ID"])
	nodes := []string{}
	for index, id := range []string{last.arrival, text(last.berth["Node"]), last.departure} {
		old, err := g.find("Nodes", id)
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
		owned["ID"], owned["Position"] = chainID(ids, id, prefix), position
		g.network["Nodes"] = append(items(g.network["Nodes"]), owned)
		nodes = append(nodes, text(owned["ID"]))
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
		id := chainID(ids, text(owned["ID"]), "lane")
		owned["ID"], owned["From"], owned["To"], owned["StationID"], owned["StationRole"] = id, lane.from, lane.to, stationID, lane.role
		if control := owned["Control"]; control != nil {
			if !finite(member(control, "X")) || !finite(member(control, "Y")) {
				return nil, errors.New("the lane needs a finite control point")
			}
			position, err := shift(geometryPoint{number(member(control, "X")), number(member(control, "Y"))})
			if err != nil {
				return nil, err
			}
			owned["Control"] = position
		}
		g.network["Lanes"] = append(items(g.network["Lanes"]), owned)
		created = append(created, id)
	}
	berth := object(cloneEditValue(last.berth))
	berth["ID"], berth["Node"] = chainID(ids, text(berth["ID"]), stationID+"-berth"), nodes[1]
	station["Berths"] = append(items(station["Berths"]), berth)
	return created, nil
}

func (g geometryDraft) removeBerth(stationID, berthID string) error {
	station, err := g.find("Stations", stationID)
	if err != nil {
		return err
	}
	if berthID == "" {
		return errors.New("a berth edit needs a berth ID")
	}
	berths := items(station["Berths"])
	index := slices.IndexFunc(berths, func(berth any) bool { return member(berth, "ID") == berthID })
	if index < 0 {
		return nil
	}
	if len(berths) <= 1 {
		if has(station, "Banks") {
			return errors.New("the station must retain one berth")
		}
		return nil
	}
	node := text(member(berths[index], "Node"))
	if _, nodeErr := g.point(node); nodeErr != nil {
		return nodeErr
	}
	for _, lane := range items(g.network["Lanes"]) {
		if (member(lane, "From") == node || member(lane, "To") == node) && member(lane, "StationID") != stationID {
			return errors.New("delete nonstation lanes at the berth node first")
		}
	}
	goneNodes, goneLanes := map[string]bool{node: true}, map[string]bool{}
	chainStation := station
	var owningBank map[string]any
	if has(station, "Banks") {
		for _, bank := range items(station["Banks"]) {
			if slices.ContainsFunc(items(member(bank, "BerthIDs")), func(id any) bool { return id == berthID }) {
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
		if last.berth["ID"] == berthID {
			goneNodes[last.arrival], goneNodes[last.departure] = true, true
			for _, lane := range []map[string]any{last.arrivalLink, last.departureLink, last.inlet, last.outlet} {
				goneLanes[text(lane["ID"])] = true
			}
			for _, lane := range items(g.network["Lanes"]) {
				if !goneLanes[text(member(lane, "ID"))] && (member(lane, "From") == last.arrival || member(lane, "To") == last.arrival || member(lane, "From") == last.departure || member(lane, "To") == last.departure) {
					return errors.New("delete shared lanes at the berth row nodes first")
				}
			}
		}
	}
	if owningBank != nil {
		owningBank["BerthIDs"] = slices.DeleteFunc(items(owningBank["BerthIDs"]), func(id any) bool { return id == berthID })
		if len(items(owningBank["BerthIDs"])) == 0 {
			for _, key := range []string{"Entry", "Exit"} {
				goneNodes[text(owningBank[key])] = true
			}
			for _, lane := range items(g.network["Lanes"]) {
				if goneNodes[text(member(lane, "From"))] || goneNodes[text(member(lane, "To"))] {
					if member(lane, "StationID") != stationID {
						return errors.New("delete nonstation lanes at the bank gates first")
					}
					goneLanes[text(member(lane, "ID"))] = true
				}
			}
			station["Banks"] = slices.DeleteFunc(items(station["Banks"]), func(bank any) bool { return member(bank, "ID") == owningBank["ID"] })
			refreshBankAliases(station)
		}
	}
	station["Berths"] = slices.Delete(berths, index, index+1)
	g.network["Nodes"] = slices.DeleteFunc(items(g.network["Nodes"]), func(item any) bool { return goneNodes[text(member(item, "ID"))] })
	g.network["Lanes"] = slices.DeleteFunc(items(g.network["Lanes"]), func(item any) bool {
		return member(item, "From") == node || member(item, "To") == node || goneLanes[text(member(item, "ID"))]
	})
	fleet := items(cloneEditValue(member(g.draft, "fleet")))
	fleet = slices.DeleteFunc(fleet, func(pod any) bool { return member(pod, "BerthID") == berthID })
	if !reflect.DeepEqual(items(member(g.draft, "fleet")), fleet) {
		g.patch["fleet"] = fleet
	}
	return nil
}
