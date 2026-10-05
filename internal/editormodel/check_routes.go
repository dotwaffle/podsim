package editormodel

import (
	"fmt"
	"slices"
	"strings"
)

func (g *draftNetwork) checkPassengerRoutes(errors *checkList) {
	if !g.passengerChecked {
		var found checkList
		g.findPassengerRoutes(&found)
		g.passengerChecks, g.passengerChecked = found.items, true
	}
	for _, item := range g.passengerChecks {
		errors.add(item.Text, item.Target)
	}
}

func (g *draftNetwork) findPassengerRoutes(errors *checkList) {
	berths := make([][]string, len(g.passenger))
	for index, station := range g.passenger {
		for _, berth := range items(member(station, "berths")) {
			if object(berth) != nil {
				if node := text(member(berth, "node")); !g.brokenBerths[node] {
					berths[index] = append(berths[index], node)
				}
			}
		}
	}
	if g.allBerthsConnected(berths) {
		return
	}
	reach := draftStationReach(g.directed, berths)
	checked := make([]bool, len(berths))
	for from, origins := range berths {
		checked[from] = len(origins) != 0
	}
	for _, item := range cutOff(reach, checked) {
		station := g.passenger[item.index]
		parts := make([]string, 0, 2)
		list := func(indexes []int) string {
			if len(indexes) == 1 {
				return text(member(g.passenger[indexes[0]], "name"))
			}
			return fmt.Sprintf("%d passenger stations", len(indexes))
		}
		name := text(member(station, "name"))
		if len(item.out) != 0 {
			parts = append(parts, name+" cannot reach "+list(item.out))
		}
		if len(item.in) != 0 {
			parts = append(parts, list(item.in)+" cannot reach "+name)
		}
		errors.add(strings.Join(parts, ", and ")+".", target("station", member(station, "id")))
	}
}

// draftStationReach searches each distinct berth node once with indexed
// adjacency and reusable marks. Repeated berth nodes cannot add work per row.
func draftStationReach(graph map[string][]string, berths [][]string) [][]bool {
	indexes := make(map[string]int)
	indexOf := func(id string) int {
		if index, exists := indexes[id]; exists {
			return index
		}
		index := len(indexes)
		indexes[id] = index
		return index
	}
	for from, destinations := range graph {
		indexOf(from)
		for _, to := range destinations {
			indexOf(to)
		}
	}
	nodes := make([][]int, len(berths))
	for station, entries := range berths {
		for _, id := range entries {
			nodes[station] = append(nodes[station], indexOf(id))
		}
		slices.Sort(nodes[station])
		nodes[station] = slices.Compact(nodes[station])
	}
	adjacent := make([][]int, len(indexes))
	for from, destinations := range graph {
		for _, to := range destinations {
			adjacent[indexes[from]] = append(adjacent[indexes[from]], indexes[to])
		}
	}
	seen := make([]uint32, len(indexes))
	queue := make([]int, 0, len(indexes))
	search := uint32(0)
	computed := make(map[int][]bool)
	reach := make([][]bool, len(berths))
	for station, origins := range nodes {
		reach[station] = make([]bool, len(berths))
		for to := range berths {
			reach[station][to] = true
		}
		for _, origin := range origins {
			row, exists := computed[origin]
			if !exists {
				search++
				seen[origin] = search
				queue = append(queue[:0], origin)
				for head := 0; head < len(queue); head++ {
					for _, next := range adjacent[queue[head]] {
						if seen[next] != search {
							seen[next] = search
							queue = append(queue, next)
						}
					}
				}
				row = make([]bool, len(berths))
				for to, destinations := range nodes {
					row[to] = !slices.ContainsFunc(destinations, func(node int) bool { return seen[node] != search })
				}
				computed[origin] = row
			}
			for to, reachable := range row {
				reach[station][to] = reach[station][to] && reachable
			}
		}
	}
	return reach
}

// Two searches from one berth prove all passenger berth pairs when each
// berth reaches that root and the root reaches each berth.
func (g *draftNetwork) allBerthsConnected(berths [][]string) bool {
	root := ""
	found := false
	for _, station := range berths {
		if len(station) != 0 {
			root, found = station[0], true
			break
		}
	}
	if !found {
		return true
	}
	reverse := make(map[string][]string, len(g.directed))
	for from, destinations := range g.directed {
		for _, to := range destinations {
			reverse[to] = append(reverse[to], from)
		}
	}
	fromRoot, toRoot := draftReach(g.directed, root), draftReach(reverse, root)
	for _, station := range berths {
		for _, berth := range station {
			if !fromRoot[berth] || !toRoot[berth] {
				return false
			}
		}
	}
	return true
}

type cutOffStation struct {
	index   int
	out, in []int
}

func cutOff(reach [][]bool, checked []bool) []cutOffStation {
	var stations []int
	for index, hasBerths := range checked {
		if hasBerths {
			stations = append(stations, index)
		}
	}
	links := func(index int) cutOffStation {
		item := cutOffStation{index: index}
		for _, other := range stations {
			if other != index {
				if !reach[index][other] {
					item.out = append(item.out, other)
				}
				if !reach[other][index] {
					item.in = append(item.in, other)
				}
			}
		}
		return item
	}
	group := make(map[int]int)
	type stationGroup struct{ size, misses int }
	var groups []stationGroup
	main := -1
	for _, seed := range stations {
		if _, exists := group[seed]; exists {
			continue
		}
		item := stationGroup{}
		for _, index := range stations {
			if index == seed || reach[seed][index] && reach[index][seed] {
				group[index] = len(groups)
				item.size++
			}
		}
		missing := links(seed)
		item.misses = len(missing.out) + len(missing.in)
		if main < 0 || item.size > groups[main].size || item.size == groups[main].size && item.misses < groups[main].misses {
			main = len(groups)
		}
		groups = append(groups, item)
	}
	var result []cutOffStation
	for _, index := range stations {
		if group[index] != main {
			result = append(result, links(index))
		}
	}
	return result
}

func (g *draftNetwork) warnings(warnings *checkList) {
	undirected := make(map[string][]string)
	for _, lane := range g.lanes {
		if object(lane) == nil {
			continue
		}
		from, to := text(member(lane, "from")), text(member(lane, "to"))
		undirected[from] = append(undirected[from], to)
		undirected[to] = append(undirected[to], from)
	}
	var sections []string
	for _, node := range g.nodes {
		if object(node) == nil {
			continue
		}
		id := text(member(node, "id"))
		if g.stationNodes[id] || len(undirected[id]) != 0 {
			sections = append(sections, id)
		} else {
			warnings.add("Junction "+id+" is disconnected.", target("node", member(node, "id")))
		}
	}
	if len(sections) != 0 {
		seen := draftReach(undirected, sections[0])
		if slices.ContainsFunc(sections, func(id string) bool { return !seen[id] }) {
			warnings.add("The network has disconnected sections.", nil)
		}
	}
}
