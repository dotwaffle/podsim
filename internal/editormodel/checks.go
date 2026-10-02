package editormodel

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

type checkTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type check struct {
	Text   string       `json:"text"`
	Target *checkTarget `json:"target"`
}

type checkReport struct {
	Errors   []check `json:"errors"`
	Warnings []check `json:"warnings"`
}

type checkList struct {
	items []check
	seen  map[string]int
}

func (l *checkList) add(text string, target *checkTarget) {
	if l.seen == nil {
		l.seen = make(map[string]int)
	}
	if index, exists := l.seen[text]; exists {
		if l.items[index].Target == nil {
			l.items[index].Target = target
		}
		return
	}
	l.seen[text] = len(l.items)
	l.items = append(l.items, check{Text: text, Target: target})
}

func target(kind string, value any) *checkTarget {
	id, ok := value.(string)
	if kind == "" || !ok || id == "" {
		return nil
	}
	return &checkTarget{Type: kind, ID: id}
}

func object(value any) map[string]any  { result, _ := value.(map[string]any); return result }
func items(value any) []any            { result, _ := value.([]any); return result }
func text(value any) string            { result, _ := value.(string); return result }
func member(value any, key string) any { return object(value)[key] }

func number(value any) float64 {
	result, ok := value.(float64)
	if !ok {
		return math.NaN()
	}
	return result
}

func finite(value any) bool {
	x := number(value)
	return !math.IsNaN(x) && !math.IsInf(x, 0)
}

func integer(value any) bool { x := number(value); return finite(value) && x == math.Trunc(x) }
func validID(value any) bool {
	id, ok := value.(string)
	return ok && strings.TrimSpace(id) != "" && len(id) <= 64
}
func has(value any, key string) bool { _, ok := object(value)[key]; return ok }

func label(value any) string {
	if id := text(value); id != "" {
		return id
	}
	return "?"
}

// draftChecks also accepts incomplete or incorrectly typed drafts. Only the
// native project validator decides whether a draft can start a simulation.
func draftChecks(value any) checkReport {
	return preparedDraftChecks(value, nil)
}

type preparedChecks struct {
	network                 *draftNetwork
	networkErrors, profiles []check
	profilesReady           bool
	services                []check
	servicesReady           bool
}

func preparedDraftChecks(value any, prepared *preparedChecks) checkReport {
	var errors, warnings checkList
	if object(value) == nil {
		errors.add("The scenario must be a JSON object.", nil)
		return checkReport{Errors: errors.items}
	}
	version := number(member(value, "version"))
	banked := hasBanks(member(value, "network"))
	if version != 1 && version != 2 && version != 3 {
		errors.add("The scenario version must be 1, 2, or 3.", nil)
	} else if version == 1 && banked || version == 2 && !banked {
		errors.add("The scenario version does not match its station banks.", nil)
	}
	if prepared == nil {
		checkServiceMetadata(value, &errors)
	} else {
		if !prepared.servicesReady {
			var serviceErrors checkList
			checkServiceMetadata(value, &serviceErrors)
			prepared.services, prepared.servicesReady = serviceErrors.items, true
		}
		for _, item := range prepared.services {
			errors.add(item.Text, item.Target)
		}
	}
	name := member(value, "name")
	if strings.TrimSpace(text(name)) == "" {
		errors.add("The scenario needs a name.", nil)
	}
	if len(text(name)) > 80 {
		errors.add("The scenario name exceeds 80 bytes.", nil)
	}
	network := member(value, "network")
	if object(network) == nil || items(member(network, "Nodes")) == nil || items(member(network, "Lanes")) == nil || items(member(network, "Stations")) == nil {
		errors.add("The scenario needs Nodes, Lanes, and Stations arrays.", nil)
		return checkReport{Errors: errors.items}
	}
	var g *draftNetwork
	if prepared == nil {
		g = checkNetwork(network, &errors)
	} else {
		g = prepared.network
		for _, item := range prepared.networkErrors {
			errors.add(item.Text, item.Target)
		}
	}
	checkFleet(value, g, &errors)
	passengerIDs := make(map[string]bool)
	for _, station := range g.passenger {
		passengerIDs[text(member(station, "ID"))] = true
	}
	checkDemandRate(value, &errors)
	if prepared == nil {
		checkProfiles(value, passengerIDs, &errors)
	} else {
		for _, item := range prepared.profiles {
			errors.add(item.Text, item.Target)
		}
	}
	checkRailPlans(value, passengerIDs, &errors)
	checkGeo(value, &errors)
	checkDemand(value, passengerIDs, &errors)
	checkSettings(value, &errors)
	g.warnings(&warnings)
	return checkReport{Errors: errors.items, Warnings: warnings.items}
}

type draftNetwork struct {
	nodes, lanes, stations, passenger []any
	nodeIDs, stationIDs, berthIDs     map[string]bool
	positions                         map[string]any
	directed                          map[string][]string
	stationNodes, brokenBerths        map[string]bool
	passengerChecks                   []check
	passengerChecked                  bool
}

func checkNetwork(value any, errors *checkList) *draftNetwork {
	g := &draftNetwork{
		nodes: items(member(value, "Nodes")), lanes: items(member(value, "Lanes")), stations: items(member(value, "Stations")),
		nodeIDs: make(map[string]bool), stationIDs: make(map[string]bool), berthIDs: make(map[string]bool),
		positions: make(map[string]any), directed: make(map[string][]string),
		stationNodes: make(map[string]bool), brokenBerths: make(map[string]bool),
	}
	ids := make(draftIDs)
	g.checkNodes(ids, errors)
	pairs := g.checkLanes(ids, errors)
	g.checkStations(ids, pairs, errors)
	g.checkBerthRoutes(errors)
	g.checkStationRoles(errors)
	if hasBanks(value) {
		if err := validateBankDraft(value); err != nil {
			errors.add(err.Error(), nil)
		}
	}
	return g
}

type draftIDs map[string]map[string]bool

func (ids draftIDs) add(value any, kind, targetKind string, errors *checkList) {
	id := text(value)
	if len(id) > 64 {
		errors.add(kind+" ID exceeds 64 bytes.", target(targetKind, value))
	}
	if strings.TrimSpace(id) == "" {
		errors.add(kind+" has no ID.", nil)
		return
	}
	if ids[kind] == nil {
		ids[kind] = make(map[string]bool)
	}
	if ids[kind][id] {
		errors.add("ID "+id+" is used more than once.", target(targetKind, value))
	}
	ids[kind][id] = true
}

func (g *draftNetwork) checkNodes(ids draftIDs, errors *checkList) {
	for _, node := range g.nodes {
		id, position := member(node, "ID"), member(node, "Position")
		ids.add(id, "A node", "node", errors)
		if name, ok := id.(string); ok && object(node) != nil {
			g.nodeIDs[name] = true
			if _, exists := g.positions[name]; !exists {
				g.positions[name] = position
			}
		}
		if !finite(member(position, "X")) || !finite(member(position, "Y")) {
			errors.add("Node "+label(id)+" has an invalid position.", nil)
		}
	}
}

func (g *draftNetwork) checkLanes(ids draftIDs, errors *checkList) map[[2]string]bool {
	pairs, paths := make(map[[2]string]bool), make(map[string]string)
	incident := make(map[string]int)
	var nodeOrder []string
	for _, lane := range g.lanes {
		id := member(lane, "ID")
		ids.add(id, "A lane", "lane", errors)
		from, to := text(member(lane, "From")), text(member(lane, "To"))
		at := target("lane", id)
		if object(lane) == nil || !g.nodeIDs[from] || !g.nodeIDs[to] || from == to {
			errors.add("Lane "+label(id)+" has invalid endpoints.", at)
		}
		if !finite(member(lane, "SpeedLimit")) || number(member(lane, "SpeedLimit")) <= 0 {
			errors.add("Lane "+label(id)+" needs a positive speed limit.", at)
		}
		control := member(lane, "Control")
		if control != nil && (!finite(member(control, "X")) || !finite(member(control, "Y"))) {
			errors.add("Lane "+label(id)+" has an invalid control point.", at)
		}
		if object(lane) == nil {
			continue
		}
		if g.nodeIDs[from] && g.nodeIDs[to] && draftLaneLength(g.positions[from], g.positions[to], control) < 24 {
			errors.add("Lane "+label(id)+" is shorter than 24 m.", at)
		}
		pair := [2]string{from, to}
		path := from + "\x00" + to
		if object(control) != nil {
			path += fmt.Sprintf("\x00%v\x00%v", member(control, "X"), member(control, "Y"))
		}
		if previous, exists := paths[path]; exists {
			errors.add("Lanes "+previous+" and "+label(id)+" have the same nodes and path.", at)
		} else {
			paths[path] = label(id)
		}
		pairs[pair] = true
		for _, node := range pair {
			if incident[node] == 0 {
				nodeOrder = append(nodeOrder, node)
			}
			incident[node]++
		}
		g.directed[from] = append(g.directed[from], to)
	}
	for _, node := range nodeOrder {
		if g.nodeIDs[node] && incident[node] > project.MaxNodeLanes {
			errors.add(fmt.Sprintf("Node %s has %d lanes, more than %d.", node, incident[node], project.MaxNodeLanes), target("node", node))
		}
	}
	return pairs
}

func (g *draftNetwork) checkStations(ids draftIDs, pairs map[[2]string]bool, errors *checkList) {
	berthNodes := make(map[string]bool)
	for _, station := range g.stations {
		id := member(station, "ID")
		ids.add(id, "A station", "station", errors)
		if object(station) == nil {
			errors.add("A station has an invalid value.", nil)
			continue
		}
		name, entry, exit := text(id), text(member(station, "Entry")), text(member(station, "Exit"))
		g.stationIDs[name] = true
		at := target("station", id)
		if has(station, "ParkingOnly") {
			if _, ok := member(station, "ParkingOnly").(bool); !ok {
				errors.add("Station "+label(id)+" has an invalid parking setting.", at)
			}
		}
		if len(text(member(station, "Name"))) > 80 {
			errors.add("Station "+label(id)+" name exceeds 80 bytes.", at)
		}
		berths := items(member(station, "Berths"))
		if len(berths) > project.MaxBerths {
			errors.add("Station "+label(id)+" exceeds 200 berths.", at)
		}
		if strings.TrimSpace(text(member(station, "Name"))) == "" {
			errors.add("Station "+label(id)+" needs a name.", at)
		}
		if !g.nodeIDs[entry] || !g.nodeIDs[exit] || entry == exit {
			errors.add("Station "+label(id)+" has invalid entry or exit nodes.", at)
		}
		g.stationNodes[entry], g.stationNodes[exit] = true, true
		for node := range stationCoreNodes(station) {
			g.stationNodes[node] = true
		}
		if len(berths) == 0 {
			errors.add("Station "+label(id)+" needs at least one berth.", at)
		}
		for _, berth := range berths {
			berthID := member(berth, "ID")
			ids.add(berthID, "A berth", "berth", errors)
			if object(berth) == nil {
				errors.add("Station "+label(id)+" has an invalid berth.", at)
				continue
			}
			node := text(member(berth, "Node"))
			g.berthIDs[text(berthID)], g.stationNodes[node] = true, true
			if !g.nodeIDs[node] || node == entry || node == exit {
				errors.add("Berth "+label(berthID)+" has an invalid node.", target("berth", berthID))
			}
			if berthNodes[node] {
				errors.add("Berth node "+node+" is used more than once.", target("berth", berthID))
			}
			berthNodes[node] = true
		}
		if !pairs[[2]string{entry, exit}] {
			errors.add("Station "+label(id)+" needs a through lane.", at)
		}
		if member(station, "ParkingOnly") != true {
			g.passenger = append(g.passenger, station)
		}
	}
}

func (g *draftNetwork) checkStationRoles(errors *checkList) {
	for _, lane := range g.lanes {
		if object(lane) == nil {
			continue
		}
		id, station, role := member(lane, "ID"), text(member(lane, "StationID")), text(member(lane, "StationRole"))
		roles := []string{string(sim.StationApproachRole), string(sim.StationEntryRole), string(sim.StationBerthAccessRole), string(sim.StationThroughRole), string(sim.StationDepartureRole), string(sim.StationExitRole)}
		if (station != "") != (role != "") || role != "" && !slices.Contains(roles, role) {
			errors.add("Lane "+label(id)+" has an invalid station role.", target("lane", id))
		} else if station != "" && !g.stationIDs[station] {
			errors.add("Lane "+label(id)+" refers to an unknown station.", target("lane", id))
		}
	}
}

func draftLaneLength(from, to, control any) float64 {
	start := sim.Point{X: number(member(from, "X")), Y: number(member(from, "Y"))}
	end := sim.Point{X: number(member(to, "X")), Y: number(member(to, "Y"))}
	if from == nil || to == nil {
		return 0
	}
	if control == nil {
		return math.Hypot(end.X-start.X, end.Y-start.Y)
	}
	curve := sim.Point{X: number(member(control, "X")), Y: number(member(control, "Y"))}
	length, previous := 0.0, start
	for i := 1; i <= 16; i++ {
		t := float64(i) / 16
		u := 1 - t
		next := sim.Point{X: u*u*start.X + 2*u*t*curve.X + t*t*end.X, Y: u*u*start.Y + 2*u*t*curve.Y + t*t*end.Y}
		length += math.Hypot(next.X-previous.X, next.Y-previous.Y)
		previous = next
	}
	return length
}

func (g *draftNetwork) checkBerthRoutes(errors *checkList) {
	for _, station := range g.stations {
		for _, berth := range items(member(station, "Berths")) {
			if object(berth) == nil {
				continue
			}
			node, id := text(member(berth, "Node")), member(berth, "ID")
			gates := station
			if has(station, "Banks") {
				gates = bankForBerth(station, text(id))
			}
			entry := draftRoute(g.directed, text(member(gates, "Entry")), node, g.stationNodes)
			exit := draftRoute(g.directed, node, text(member(gates, "Exit")), g.stationNodes)
			if !entry {
				errors.add("Berth "+label(id)+" needs an entry lane.", target("berth", id))
			}
			if !exit {
				errors.add("Berth "+label(id)+" needs an exit lane.", target("berth", id))
			}
			if !entry || !exit {
				g.brokenBerths[node] = true
			}
		}
	}
}

func draftRoute(graph map[string][]string, from, to string, blocked map[string]bool) bool {
	seen, queue := map[string]bool{from: true}, []string{from}
	for i := 0; i < len(queue); i++ {
		for _, next := range graph[queue[i]] {
			if next == to {
				return true
			}
			if !seen[next] && !blocked[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}

func draftReach(graph map[string][]string, from string) map[string]bool {
	seen, queue := map[string]bool{from: true}, []string{from}
	for i := 0; i < len(queue); i++ {
		for _, next := range graph[queue[i]] {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}

func checkFleet(value any, g *draftNetwork, errors *checkList) {
	if items(member(value, "fleet")) == nil {
		errors.add("The scenario needs a fleet array.", nil)
	}
	if _, ok := member(value, "redistribution").(bool); !ok {
		errors.add("The redistribution setting must be true or false.", nil)
	}
	fleet := items(member(value, "fleet"))
	occupied, ids := make(map[string]bool), make(map[string]bool)
	for _, pod := range fleet {
		id := member(pod, "ID")
		if len(text(id)) > 64 {
			errors.add("A pod ID exceeds 64 bytes.", nil)
		}
		if strings.TrimSpace(text(id)) == "" {
			errors.add("A pod has no ID.", nil)
		} else if ids[text(id)] {
			errors.add("ID "+text(id)+" is used more than once.", nil)
		}
		ids[text(id)] = true
		stationID, berthID := member(pod, "StationID"), text(member(pod, "BerthID"))
		found := false
		for _, station := range g.stations {
			if text(member(station, "ID")) == text(stationID) && object(station) != nil {
				for _, berth := range items(member(station, "Berths")) {
					found = found || object(berth) != nil && text(member(berth, "ID")) == berthID
				}
				break
			}
		}
		if object(pod) == nil || !found || !g.berthIDs[berthID] {
			errors.add("Pod "+label(id)+" has an invalid station or berth.", target("station", stationID))
		}
		if object(pod) != nil {
			if occupied[berthID] {
				errors.add("Berth "+berthID+" has more than one pod.", target("berth", berthID))
			}
			occupied[berthID] = true
		}
	}
	if len(g.passenger) < 2 {
		errors.add("The network needs at least two passenger stations.", nil)
	}
	if len(g.stations) > project.MaxStations || len(g.nodes) > project.MaxNodes || len(g.lanes) > project.MaxLanes {
		errors.add("The network exceeds the supported size.", nil)
	}
	if len(fleet) < 1 || len(fleet) > project.MaxPods {
		errors.add("The fleet must contain 1 to 300 pods.", nil)
	}
	g.checkPassengerRoutes(errors)
}
