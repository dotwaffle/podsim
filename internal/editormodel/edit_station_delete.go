package editormodel

import (
	"reflect"
	"slices"
)

func (g geometryDraft) deleteStation(id string) error {
	station, err := g.find("stations", id)
	if err != nil {
		return err
	}
	nodes := g.stationNodes(station)
	berths := make(map[string]bool)
	for _, berth := range items(station["berths"]) {
		if berthID := text(member(berth, "id")); berthID != "" {
			berths[berthID] = true
		}
	}
	g.network["stations"] = slices.DeleteFunc(items(g.network["stations"]), func(item any) bool { return member(item, "id") == id })
	g.network["nodes"] = slices.DeleteFunc(items(g.network["nodes"]), func(node any) bool { return nodes[text(member(node, "id"))] })
	g.network["lanes"] = slices.DeleteFunc(items(g.network["lanes"]), func(lane any) bool {
		return nodes[text(member(lane, "from"))] || nodes[text(member(lane, "to"))]
	})
	for _, lane := range items(g.network["lanes"]) {
		if member(lane, "stationID") == id {
			delete(object(lane), "stationID")
			delete(object(lane), "stationRole")
		}
	}
	if fleet, ok := member(g.draft, "fleet").([]any); ok {
		owned := items(cloneEditValue(fleet))
		owned = slices.DeleteFunc(owned, func(pod any) bool {
			return member(pod, "stationID") == id || berths[text(member(pod, "berthID"))]
		})
		g.replaceBranch("fleet", owned)
	}
	if demand := object(member(g.draft, "demand")); demand != nil && demand["destination"] == id {
		owned := object(cloneEditValue(demand))
		owned["destination"] = ""
		g.replaceBranch("demand", owned)
	}
	g.removeStationFlows(id)
	g.removeStationRailReferences(id, "railArrivals", "destinations")
	g.removeStationRailReferences(id, "railDepartures", "origins")
	return nil
}

func (g geometryDraft) replaceBranch(key string, owned any) {
	if !reflect.DeepEqual(member(g.draft, key), owned) {
		g.patch[key] = owned
	}
}

// removeStationFlows removes the flows that start or end at the station from
// each profile.
func (g geometryDraft) removeStationFlows(id string) {
	profiles, ok := member(g.draft, "demandProfiles").([]any)
	if !ok {
		return
	}
	owned := items(cloneEditValue(profiles))
	for _, profile := range owned {
		stations, flows := items(member(profile, "stations")), items(member(profile, "flows"))
		if flows != nil && slices.ContainsFunc(flows, func(flow any) bool { return flowNamesStation(flow, stations, id) }) {
			object(profile)["stations"], object(profile)["flows"] = withoutStationFlows(stations, flows, id)
		}
	}
	g.replaceBranch("demandProfiles", owned)
}

// flowNamesStation reports whether a flow starts or ends at the station id.
func flowNamesStation(flow any, stations []any, id string) bool {
	ends, ok := draftFlowEnds(flow, len(stations))
	return ok && (stations[ends[0]] == id || stations[ends[1]] == id)
}

// withoutStationFlows returns the station list and the flows of a profile
// without the flows that name the station id. The list keeps the stations
// of the other flows in the order of first use, as the project encoding
// does, so that no listed station is unused. The flows get the new indexes.
// A flow that does not start with two valid station indexes stays the same.
func withoutStationFlows(stations, flows []any, id string) (list, kept []any) {
	list, kept = make([]any, 0, len(stations)), make([]any, 0, len(flows))
	renumbered := make(map[int]float64, len(stations))
	for _, flow := range flows {
		ends, ok := draftFlowEnds(flow, len(stations))
		if !ok {
			kept = append(kept, flow)
			continue
		}
		if stations[ends[0]] == id || stations[ends[1]] == id {
			continue
		}
		row := items(flow)
		for end, old := range ends {
			index, found := renumbered[old]
			if !found {
				index = float64(len(list))
				renumbered[old] = index
				list = append(list, stations[old])
			}
			row[end] = index
		}
		kept = append(kept, row)
	}
	return list, kept
}

func (g geometryDraft) removeStationRailReferences(id, key, choices string) {
	plan, ok := member(g.draft, key).([]any)
	if !ok {
		return
	}
	owned := items(cloneEditValue(plan))
	owned = slices.DeleteFunc(owned, func(event any) bool {
		if member(event, "station") == id {
			return true
		}
		if candidates, ok := member(event, choices).([]any); ok {
			remaining := slices.DeleteFunc(candidates, func(choice any) bool { return member(choice, "station") == id })
			object(event)[choices] = remaining
			return len(remaining) == 0
		}
		return false
	})
	g.replaceBranch(key, owned)
}
