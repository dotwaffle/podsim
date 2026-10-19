package editormodel

import (
	"fmt"
	"slices"
)

// depotStationActions are the geometry actions on a station that change its
// geometry or its berths. A depot station refuses them, because its lane
// paths and slots must keep the depot rules of the server.
var depotStationActions = []string{
	"moveStation", "stationBearing", "stationParking", "deleteStation", "addBerth", "removeBerth",
	"stationLayout", "stationBanks", "stationLegacy", "bankLayout", "addBankBerth",
}

// hasDepots reports whether a draft has a depot. Such a draft gets the full
// validation of the server.
func hasDepots(network any) bool {
	return len(items(member(network, "depots"))) > 0
}

// checkDepotEdit refuses a geometry or berth action on a depot station.
func checkDepotEdit(network any, command geometryEdit) error {
	if !slices.Contains(depotStationActions, command.Action) {
		return nil
	}
	for _, depot := range items(member(network, "depots")) {
		if text(member(depot, "station")) == command.ID {
			return fmt.Errorf("station %s is a depot, so its geometry and berths are fixed", command.ID)
		}
	}
	return nil
}
