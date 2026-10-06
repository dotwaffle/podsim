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

// draftInt returns value as an int when it is a whole number that an int
// holds on every platform. Thus a project predicate can check a draft
// value without a rounded or wrapped conversion.
func draftInt(value any) (int, bool) {
	if x := number(value); integer(value) && math.Abs(x) <= math.MaxInt32 {
		return int(x), true
	}
	return 0, false
}

func draftPlatoonLimit(value any) bool {
	limit, ok := draftInt(value)
	return ok && project.ValidPlatoonLimit(limit)
}

func validID(value any) bool {
	id, ok := value.(string)
	return ok && strings.TrimSpace(id) != "" && len(id) <= project.MaxIDLength
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
	if problem := draftVersionError(value); problem != "" {
		errors.add(problem, nil)
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
	if len(text(name)) > project.MaxNameLength {
		errors.add("The scenario name exceeds 80 bytes.", nil)
	}
	network := member(value, "network")
	if object(network) == nil || items(member(network, "nodes")) == nil || items(member(network, "lanes")) == nil || items(member(network, "stations")) == nil {
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
		passengerIDs[text(member(station, "id"))] = true
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
	previousErrors := len(errors.items)
	g := &draftNetwork{
		nodes: items(member(value, "nodes")), lanes: items(member(value, "lanes")), stations: items(member(value, "stations")),
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
	if hasBanks(value) || hasLargeGeometry(value) && draftGeometryClassesValid(value) && len(errors.items) == previousErrors {
		if err := validateBankDraft(value); err != nil {
			errors.add(err.Error(), nil)
		}
	}
	return g
}

type draftIDs map[string]map[string]bool

func (ids draftIDs) add(value any, kind, targetKind string, errors *checkList) {
	id := text(value)
	if len(id) > project.MaxIDLength {
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
		id, position := member(node, "id"), member(node, "position")
		ids.add(id, "A node", "node", errors)
		if name, ok := id.(string); ok && object(node) != nil {
			g.nodeIDs[name] = true
			if _, exists := g.positions[name]; !exists {
				g.positions[name] = position
			}
		}
		if !finite(member(position, "x")) || !finite(member(position, "y")) {
			errors.add("Node "+label(id)+" has an invalid position.", nil)
		}
	}
}

func (g *draftNetwork) checkLanes(ids draftIDs, errors *checkList) map[[2]string]bool {
	pairs, paths := make(map[[2]string]bool), make(map[string]string)
	incident := make(map[string]int)
	var nodeOrder []string
	for _, lane := range g.lanes {
		id := member(lane, "id")
		ids.add(id, "A lane", "lane", errors)
		from, to := text(member(lane, "from")), text(member(lane, "to"))
		at := target("lane", id)
		if object(lane) == nil || !g.nodeIDs[from] || !g.nodeIDs[to] || from == to {
			errors.add("Lane "+label(id)+" has invalid endpoints.", at)
		}
		if !finite(member(lane, "speedLimit")) || number(member(lane, "speedLimit")) <= 0 {
			errors.add("Lane "+label(id)+" needs a positive speed limit.", at)
		}
		control := member(lane, "control")
		if control != nil && (!finite(member(control, "x")) || !finite(member(control, "y"))) {
			errors.add("Lane "+label(id)+" has an invalid control point.", at)
		}
		if object(lane) == nil {
			continue
		}
		minimum := draftLaneMinimum(lane)
		if g.nodeIDs[from] && g.nodeIDs[to] && draftLaneLength(g.positions[from], g.positions[to], control) < minimum {
			errors.add(fmt.Sprintf("Lane %s is shorter than %g m.", label(id), minimum), at)
		}
		pair := [2]string{from, to}
		path := from + "\x00" + to
		if object(control) != nil {
			path += fmt.Sprintf("\x00%v\x00%v", member(control, "x"), member(control, "y"))
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
		id := member(station, "id")
		ids.add(id, "A station", "station", errors)
		if object(station) == nil {
			errors.add("A station has an invalid value.", nil)
			continue
		}
		name, entry, exit := text(id), text(member(station, "entry")), text(member(station, "exit"))
		g.stationIDs[name] = true
		at := target("station", id)
		if has(station, "parkingOnly") {
			if _, ok := member(station, "parkingOnly").(bool); !ok {
				errors.add("Station "+label(id)+" has an invalid parking setting.", at)
			}
		}
		if len(text(member(station, "name"))) > project.MaxNameLength {
			errors.add("Station "+label(id)+" name exceeds 80 bytes.", at)
		}
		berths := items(member(station, "berths"))
		if len(berths) > project.MaxBerths {
			errors.add("Station "+label(id)+" exceeds 200 berths.", at)
		}
		if strings.TrimSpace(text(member(station, "name"))) == "" {
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
			berthID := member(berth, "id")
			ids.add(berthID, "A berth", "berth", errors)
			if object(berth) == nil {
				errors.add("Station "+label(id)+" has an invalid berth.", at)
				continue
			}
			node := text(member(berth, "node"))
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
		if member(station, "parkingOnly") != true {
			g.passenger = append(g.passenger, station)
		}
	}
}

func (g *draftNetwork) checkStationRoles(errors *checkList) {
	for _, lane := range g.lanes {
		if object(lane) == nil {
			continue
		}
		id, station, role := member(lane, "id"), text(member(lane, "stationID")), text(member(lane, "stationRole"))
		roles := []string{string(sim.StationApproachRole), string(sim.StationEntryRole), string(sim.StationBerthAccessRole), string(sim.StationThroughRole), string(sim.StationDepartureRole), string(sim.StationExitRole)}
		if (station != "") != (role != "") || role != "" && !slices.Contains(roles, role) {
			errors.add("Lane "+label(id)+" has an invalid station role.", target("lane", id))
		} else if station != "" && !g.stationIDs[station] {
			errors.add("Lane "+label(id)+" refers to an unknown station.", target("lane", id))
		}
	}
}

func draftLaneLength(from, to, control any) float64 {
	start := sim.Point{X: number(member(from, "x")), Y: number(member(from, "y"))}
	end := sim.Point{X: number(member(to, "x")), Y: number(member(to, "y"))}
	if from == nil || to == nil {
		return 0
	}
	if control == nil {
		return math.Hypot(end.X-start.X, end.Y-start.Y)
	}
	curve := sim.Point{X: number(member(control, "x")), Y: number(member(control, "y"))}
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
		for _, berth := range items(member(station, "berths")) {
			if object(berth) == nil {
				continue
			}
			node, id := text(member(berth, "node")), member(berth, "id")
			gates := station
			if has(station, "banks") {
				gates = bankForBerth(station, text(id))
			}
			entry := draftRoute(g.directed, text(member(gates, "entry")), node, g.stationNodes)
			exit := draftRoute(g.directed, node, text(member(gates, "exit")), g.stationNodes)
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
		id := member(pod, "id")
		if len(text(id)) > project.MaxIDLength {
			errors.add("A pod ID exceeds 64 bytes.", nil)
		}
		if strings.TrimSpace(text(id)) == "" {
			errors.add("A pod has no ID.", nil)
		} else if ids[text(id)] {
			errors.add("ID "+text(id)+" is used more than once.", nil)
		}
		ids[text(id)] = true
		stationID, berthID := member(pod, "stationID"), text(member(pod, "berthID"))
		found := false
		for _, station := range g.stations {
			if text(member(station, "id")) == text(stationID) && object(station) != nil {
				for _, berth := range items(member(station, "berths")) {
					found = found || object(berth) != nil && text(member(berth, "id")) == berthID
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
