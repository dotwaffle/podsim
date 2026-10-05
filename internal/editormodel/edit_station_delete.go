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

func (g geometryDraft) removeStationFlows(id string) {
	profiles, ok := member(g.draft, "demandProfiles").([]any)
	if !ok {
		return
	}
	owned := items(cloneEditValue(profiles))
	for _, profile := range owned {
		if flows, ok := member(profile, "flows").([]any); ok {
			object(profile)["flows"] = slices.DeleteFunc(flows, func(flow any) bool {
				return member(flow, "from") == id || member(flow, "to") == id
			})
		}
	}
	g.replaceBranch("demandProfiles", owned)
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
