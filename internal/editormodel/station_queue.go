package editormodel

import "slices"

func checkStationQueueSetting(value any, errors *checkList) {
	if !has(value, "stationQueueSpacing") {
		return
	}
	mode, textValue := member(value, "stationQueueSpacing").(string)
	if !textValue || mode != "ordinary" && mode != "compact-v1" {
		errors.add("Station queue spacing must be ordinary or compact-v1.", nil)
		return
	}
	if mode == "compact-v1" && (member(value, "stationBuffers") != true || !slices.Contains([]float64{2, 3, 4}, number(member(value, "platoonLimit")))) {
		errors.add("Compact station queues require station buffers and a platoon limit from 2 to 4.", nil)
	}
}
