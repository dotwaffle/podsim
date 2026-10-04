package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"maps"
	"math"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

func scanStateCompactFields(data []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		path := strings.Split(string(decoder.StackPointer()), "/")
		if len(path) != 5 || path[1] != "simulation" || path[2] != "pods" || path[4] != "compactQueue" {
			continue
		}
		value, err := decoder.ReadValue()
		if err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errors.New("saved compact queue is null")
		}
	}
}

func compactStateLimits(limits jsonLimits) jsonLimits {
	limits.arrays = maps.Clone(limits.arrays)
	for _, name := range []string{"members", "stopCells", "speeds", "targets", "landingSpeeds"} {
		limits.arrays["/simulation/pods/*/compactQueue/"+name] = 4
	}
	return limits
}

func decodeCompactQueue(decoder *jsontext.Decoder, queue *sim.SavedCompactQueue) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(value, &fields); err != nil {
		return err
	}
	for _, name := range []string{"kind", "phase", "lane", "members", "start", "frontier", "stopCells", "speeds", "targets", "landingSpeeds"} {
		if _, present := fields[name]; !present {
			return errors.New("saved compact queue has a missing member")
		}
	}
	// Null numbers and array elements otherwise decode as zero values.
	tokens := jsontext.NewDecoder(bytes.NewReader(value))
	for {
		token, err := tokens.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if token.Kind() == jsontext.KindNull {
			return errors.New("saved compact queue contains null")
		}
	}
	type fieldsOnly sim.SavedCompactQueue
	var saved fieldsOnly
	if err := json.Unmarshal(value, &saved, json.RejectUnknownMembers(true)); err != nil {
		return err
	}
	n := len(saved.Members)
	if saved.Kind != "compact-buffer-v1" || saved.Phase != "compact" && saved.Phase != "recovering" ||
		len(saved.Lane) < 1 || len(saved.Lane) > 64 || n < 1 || n > 4 ||
		len(saved.StopCells) != n || len(saved.Speeds) != n || len(saved.Targets) != n || len(saved.LandingSpeeds) != n ||
		!finiteCompactNumber(saved.Start) || !finiteCompactNumber(saved.Frontier) {
		return errors.New("invalid saved compact queue shape")
	}
	for i, member := range saved.Members {
		if len(member) < 1 || len(member) > 64 || saved.StopCells[i] < 0 ||
			!finiteCompactNumber(saved.Speeds[i]) || saved.Speeds[i] < 0 || saved.Speeds[i] > 2.5 ||
			!finiteCompactNumber(saved.Targets[i]) || !finiteCompactNumber(saved.LandingSpeeds[i]) {
			return errors.New("invalid saved compact member shape")
		}
	}
	*queue = sim.SavedCompactQueue(saved)
	return nil
}

func finiteCompactNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validateSavedCompactMembers(state sim.SavedState) error {
	claimed := make(map[string]bool)
	for _, pod := range state.Pods {
		if pod.CompactQueue == nil {
			continue
		}
		for _, member := range pod.CompactQueue.Members {
			if claimed[member] || len(claimed) >= maxSavedPods {
				return errors.New("saved compact members repeat or exceed the fleet limit")
			}
			claimed[member] = true
		}
	}
	return nil
}
