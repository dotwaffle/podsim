package editormodel

// repairDemandDestination moves the former render-time default into Go edits.
func repairDemandDestination(draft any, change *projectChange) {
	network := member(draft, "network")
	if replacement, exists := change.Patch["network"]; exists {
		network = replacement
	}
	demand := object(member(draft, "demand"))
	if replacement, exists := change.Patch["demand"]; exists {
		demand = object(replacement)
	}
	if demand == nil {
		return
	}
	first := ""
	for _, station := range items(member(network, "stations")) {
		id := text(member(station, "id"))
		if id == "" || editorTruthy(member(station, "parkingOnly")) {
			continue
		}
		if member(demand, "destination") == id {
			return
		}
		if first == "" {
			first = id
		}
	}
	if first == "" {
		return
	}
	owned := object(cloneEditValue(demand))
	owned["destination"] = first
	change.Patch["demand"], change.Flag = owned, ""
}
