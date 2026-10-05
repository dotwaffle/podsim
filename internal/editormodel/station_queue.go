package editormodel

import (
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// stationQueueContractError mirrors the native refusal of queue spacing
// with Express but without the coupling marker.
func stationQueueContractError(draft any) string {
	if draftOrderContract(draft) == sim.ExpressOrderContract && !couplingMarked(draft) {
		return "Station queue spacing with express-v1 requires couplingContract compact-pair-v1."
	}
	return ""
}

func checkStationQueueSetting(value any, errors *checkList) {
	if !has(value, "stationQueueSpacing") {
		return
	}
	mode, textValue := member(value, "stationQueueSpacing").(string)
	if !textValue || mode != "ordinary" && mode != "compact-v1" {
		errors.add("Station queue spacing must be ordinary or compact-v1.", nil)
		return
	}
	if problem := stationQueueContractError(value); problem != "" {
		errors.add(problem, nil)
	}
	if mode == "compact-v1" && (member(value, "stationBuffers") != true || !slices.Contains([]float64{2, 3, 4}, number(member(value, "platoonLimit")))) {
		errors.add("Compact station queues require station buffers and a platoon limit from 2 to 4.", nil)
	}
}
