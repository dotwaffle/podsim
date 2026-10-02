package editormodel

import (
	"errors"
	"math"
	"reflect"
	"strconv"

	"github.com/dotwaffle/podsim/internal/project"
)

func (c *projectChange) fleetCount(draft any, stationID string, requested any) error {
	if stationID == "" {
		return errors.New("a fleet edit needs a station ID")
	}
	var station any
	for _, candidate := range items(member(member(draft, "network"), "Stations")) {
		if text(member(candidate, "ID")) == stationID {
			station = candidate
			break
		}
	}
	if station == nil {
		return errors.New("the fleet station no longer exists")
	}
	berths := items(member(station, "Berths"))
	requestedCount, err := editNumber(requested)
	if err != nil {
		requestedCount = 0
	}
	count := int(max(0, min(float64(len(berths)), math.Floor(requestedCount))))
	available := make(map[string]bool, len(berths))
	for _, berth := range berths {
		available[text(member(berth, "ID"))] = true
	}
	other, current := []any{}, []any{}
	for _, pod := range items(member(draft, "fleet")) {
		if text(member(pod, "StationID")) != stationID {
			other = append(other, pod)
		} else if available[text(member(pod, "BerthID"))] {
			current = append(current, pod)
		}
	}
	kept := current[:min(count, len(current))]
	occupied, ids := map[string]bool{}, map[string]bool{}
	for _, group := range [][]any{other, kept} {
		for _, pod := range group {
			occupied[text(member(pod, "BerthID"))] = true
			ids[text(member(pod, "ID"))] = true
		}
	}
	for _, berth := range berths {
		if len(kept) >= count {
			break
		}
		berthID := text(member(berth, "ID"))
		if occupied[berthID] {
			continue
		}
		id := freePodID(ids)
		kept = append(kept, map[string]any{"ID": id, "StationID": stationID, "BerthID": berthID})
		occupied[berthID], ids[id] = true, true
	}
	if len(other)+len(kept) > project.MaxPods {
		return errors.New("the fleet edit exceeds the pod limit")
	}
	updated := other
	updated = append(updated, kept...)
	if !reflect.DeepEqual(items(member(draft, "fleet")), updated) {
		c.Patch["fleet"] = cloneEditValue(updated)
	}
	return nil
}

func freePodID(used map[string]bool) string {
	for number := 1; ; number++ {
		id := strconv.Itoa(number)
		if number < 10 {
			id = "0" + id
		}
		if !used[id] {
			return id
		}
	}
}
