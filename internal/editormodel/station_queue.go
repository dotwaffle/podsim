package editormodel

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func checkStationQueueSetting(value any, errors *checkList) {
	if !has(value, "stationQueueSpacing") {
		return
	}
	setting, textValue := member(value, "stationQueueSpacing").(string)
	mode := sim.StationQueueSpacing(setting)
	if !textValue || !project.ValidStationQueueSpacing(mode) {
		errors.add("Station queue spacing must be ordinary or compact-v1.", nil)
		return
	}
	limit, whole := draftInt(member(value, "platoonLimit"))
	if mode == sim.StationQueueCompactV1 && (!whole || !project.CompactStationQueuesAllowed(member(value, "stationBuffers") == true, limit)) {
		errors.add(fmt.Sprintf("Compact station queues require station buffers and a platoon limit from %d to %d.", sim.MinPlatoonLimit, sim.MaxPlatoonLimit), nil)
	}
}
