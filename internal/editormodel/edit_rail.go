package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"github.com/dotwaffle/podsim/internal/project"
)

type railEdit struct {
	Action      string         `json:"action"`
	ID          string         `json:"id,omitempty"`
	Field       string         `json:"field,omitempty"`
	Value       jsontext.Value `json:"value,omitempty"`
	Index       *int           `json:"index,omitempty"`
	ChoiceCount *int           `json:"choiceCount,omitempty"`
}

func editRail(draft any, departure bool, raw jsontext.Value) (projectChange, error) {
	if raw.Kind() != '{' {
		return projectChange{}, errors.New("a rail edit needs a command object")
	}
	var command railEdit
	if err := json.Unmarshal(raw, &command, json.RejectUnknownMembers(true)); err != nil {
		return projectChange{}, fmt.Errorf("decode rail edit: %w", err)
	}
	if err := command.validate(raw); err != nil {
		return projectChange{}, err
	}
	key, choices := "railArrivals", "destinations"
	if departure {
		key, choices = "railDepartures", "origins"
	}
	previous := items(member(draft, key))
	plan := items(cloneEditValue(previous))
	if command.Action == "add" {
		event, err := newRailEvent(draft, departure)
		if err != nil {
			return projectChange{}, err
		}
		plan = append(plan, event)
	} else {
		index := slices.IndexFunc(plan, func(event any) bool { return text(member(event, "id")) == command.ID })
		if index < 0 {
			return projectChange{}, errors.New("the rail event no longer exists")
		}
		if command.Action == "remove" {
			plan = slices.Delete(plan, index, index+1)
		} else if err := changeRailEvent(draft, object(plan[index]), choices, command, departure); err != nil {
			return projectChange{}, err
		}
	}
	change := projectChange{Patch: make(map[string]any)}
	if !reflect.DeepEqual(previous, plan) {
		change.Patch[key] = plan
	}
	return change, nil
}

func (c railEdit) validate(raw jsontext.Value) error {
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("decode rail edit fields: %w", err)
	}
	for key, value := range fields {
		if value.Kind() == 'n' {
			return fmt.Errorf("rail edit field %s must not be null", key)
		}
	}
	if !slices.Contains([]string{"add", "remove", "set", "addChoice", "removeChoice"}, c.Action) {
		return errors.New("unknown rail edit action")
	}
	if (c.Action == "add") != (c.ID == "") || c.Action == "add" && len(fields["id"]) != 0 {
		return errors.New("the rail edit has an invalid event ID")
	}
	if c.Action == "set" {
		if c.Field == "" || len(c.Value) == 0 {
			return errors.New("a rail field edit needs a field and value")
		}
	} else if len(fields["field"]) != 0 || len(c.Value) != 0 {
		return errors.New("the rail action does not accept a field or value")
	}
	choice := c.Action == "removeChoice" || c.Action == "set" && c.Index != nil
	if choice {
		if c.Index == nil || c.ChoiceCount == nil || *c.Index < 0 || *c.ChoiceCount < 1 || *c.ChoiceCount > project.MaxRailDestinations {
			return errors.New("a rail choice edit needs its index and count")
		}
	} else if len(fields["index"]) != 0 || len(fields["choiceCount"]) != 0 {
		return errors.New("the rail action does not accept choice coordinates")
	}
	return nil
}

func passengerStations(draft any) []any {
	return slices.DeleteFunc(slices.Clone(items(member(member(draft, "network"), "Stations"))), func(station any) bool {
		return member(station, "ParkingOnly") == true
	})
}

func newRailEvent(draft any, departure bool) (any, error) {
	stations := passengerStations(draft)
	if len(stations) < 2 {
		return nil, errors.New("rail events need at least two passenger stations")
	}
	arrivals, departures := items(member(draft, "railArrivals")), items(member(draft, "railDepartures"))
	if len(arrivals)+len(departures) >= project.MaxRailArrivals {
		return nil, errors.New("the plan already has 256 rail events")
	}
	plan, prefix, choices, at, walking := arrivals, "train-", "destinations", 0.0, 0.0
	if departure {
		plan, prefix, choices, at, walking = departures, "departure-", "origins", 600, 60
		total := 0.0
		for _, event := range departures {
			total += number(member(event, "passengers"))
		}
		if total+120 > project.MaxRailDeparturePassengers {
			return nil, errors.New("rail departures can offer at most 3000 passengers")
		}
	}
	used := make(map[string]bool, len(plan))
	if len(plan) != 0 {
		latest := 0.0
		for _, event := range plan {
			latest = max(latest, number(member(event, "atSeconds")))
			used[text(member(event, "id"))] = true
		}
		at = min(86400, latest+600)
	}
	id := ""
	for next := 1; id == ""; next++ {
		candidate := prefix + strconv.Itoa(next)
		if !used[candidate] {
			id = candidate
		}
	}
	event := map[string]any{
		"id": id, "station": text(member(stations[0], "ID")), "atSeconds": at,
		"walkingSeconds": walking, "passengers": float64(120),
		choices: []any{map[string]any{"station": text(member(stations[1], "ID")), "weight": float64(1)}},
	}
	if departure {
		event["requestFromSeconds"], event["requestUntilSeconds"] = at-600, at-300
	}
	return event, nil
}

func changeRailEvent(draft any, event map[string]any, choices string, command railEdit, departure bool) error {
	if event == nil {
		return errors.New("the rail event must be an object")
	}
	rows := items(event[choices])
	if command.Index != nil && (*command.ChoiceCount != len(rows) || *command.Index >= len(rows)) {
		return errors.New("the rail choices changed, enter the edit again")
	}
	switch command.Action {
	case "addChoice":
		if len(rows) >= project.MaxRailDestinations {
			return errors.New("a rail event can have at most 16 choices")
		}
		for _, station := range passengerStations(draft) {
			id := text(member(station, "ID"))
			if id != text(event["station"]) && !slices.ContainsFunc(rows, func(row any) bool { return text(member(row, "station")) == id }) {
				event[choices] = append(rows, map[string]any{"station": id, "weight": float64(1)})
				return nil
			}
		}
		return errors.New("all other passenger stations are already choices")
	case "removeChoice":
		if len(rows) <= 1 {
			return errors.New("a rail event needs at least one choice")
		}
		event[choices] = slices.Delete(rows, *command.Index, *command.Index+1)
	case "set":
		return setRailField(event, rows, command, departure)
	}
	return nil
}

func setRailField(event map[string]any, rows []any, command railEdit, departure bool) error {
	allowed := []string{"station", "atSeconds", "walkingSeconds", "passengers"}
	target := event
	if command.Index != nil {
		allowed, target = []string{"station", "weight"}, object(rows[*command.Index])
	} else if departure {
		allowed = append(allowed, "requestFromSeconds", "requestUntilSeconds")
	}
	if target == nil || !slices.Contains(allowed, command.Field) {
		return errors.New("unknown rail field")
	}
	var value any
	if err := json.Unmarshal(command.Value, &value); err != nil {
		return fmt.Errorf("decode rail field value: %w", err)
	}
	if command.Field == "station" {
		if _, ok := value.(string); !ok {
			return errors.New("a rail station must be text")
		}
	} else {
		if input, ok := value.(string); ok && trimEditorSpace(input) == "" {
			return errors.New("a rail number must not be empty")
		}
		x, err := editNumber(value)
		if err != nil {
			return err
		}
		value = x
	}
	target[command.Field] = value
	return nil
}
