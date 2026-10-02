package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"

	"github.com/dotwaffle/podsim/internal/project"
)

func helperOperation(op string) bool {
	return op == "backgroundMetadata" || op == "importCompatibility" || op == "stationLayout"
}

func executeHelper(command request, input string) (response, error) {
	allowed := map[string]bool{"op": true}
	switch command.Op {
	case "backgroundMetadata":
		allowed["metadata"] = true
	case "importCompatibility":
		allowed["project"] = true
	case "stationLayout":
		allowed["project"], allowed["layout"] = true, true
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal([]byte(input), &fields, jsontext.AllowInvalidUTF8(command.Op == "backgroundMetadata")); err != nil {
		return response{}, fmt.Errorf("decode helper request: %w", err)
	}
	for key := range fields {
		if !allowed[key] {
			return response{}, fmt.Errorf("helper operation does not accept %s", key)
		}
	}
	if command.Op == "backgroundMetadata" {
		metadata, err := backgroundMetadata(command.Metadata)
		return response{Valid: err == nil, Metadata: metadata}, err
	}
	if command.Project.Kind() != '{' {
		return response{}, errors.New("helper request needs a project object")
	}
	if len(command.Project) > project.MaxFileBytes {
		return response{}, errors.New("editor project is too large")
	}
	var draft any
	if err := json.Unmarshal(command.Project, &draft); err != nil {
		return response{}, fmt.Errorf("decode helper draft: %w", err)
	}
	if command.Op == "importCompatibility" {
		change, err := importCompatibility(draft)
		return response{Change: &change, helper: true}, err
	}
	summary, err := inspectStationLayout(draft, command.Layout)
	return response{Layout: &summary, helper: true}, err
}

func importCompatibility(draft any) (projectChange, error) {
	change := projectChange{Patch: map[string]any{}}
	fleet := cloneEditValue(member(draft, "fleet"))
	for _, pod := range items(fleet) {
		if !editorTruthy(pod) || editorTruthy(member(pod, "BerthID")) {
			continue
		}
		if _, array := pod.([]any); array {
			continue
		}
		if object(pod) == nil {
			return projectChange{}, errors.New("a pod must be an object before import repair")
		}
		berthID := any("")
		for _, station := range items(member(member(draft, "network"), "Stations")) {
			if editorTruthy(station) && sameOptionalMember(station, "ID", pod, "StationID") {
				if berths := items(member(station, "Berths")); len(berths) != 0 && editorTruthy(member(berths[0], "ID")) {
					berthID = member(berths[0], "ID")
				}
				break
			}
		}
		object(pod)["BerthID"] = berthID
	}
	if !reflect.DeepEqual(fleet, member(draft, "fleet")) {
		change.Patch["fleet"] = fleet
	}
	demand := object(cloneEditValue(member(draft, "demand")))
	if member(demand, "pattern") == "market" {
		demand["pattern"] = "destination"
		if stations, ok := member(member(draft, "network"), "Stations").([]any); !editorTruthy(demand["destination"]) && ok {
			demand["destination"] = ""
			for _, station := range stations {
				if !editorTruthy(station) || editorTruthy(member(station, "ParkingOnly")) {
					continue
				}
				demand["destination"] = member(station, "ID")
				if !editorTruthy(demand["destination"]) {
					demand["destination"] = ""
				}
				if member(station, "ID") == "market" {
					break
				}
			}
		}
		change.Patch["demand"] = demand
	}
	return change, nil
}

type layoutField struct {
	Value  *float64 `json:"value"`
	Reason string   `json:"reason"`
}

type layoutSummary struct {
	Pitch           layoutField `json:"pitch"`
	Spacing         layoutField `json:"spacing"`
	Setback         layoutField `json:"setback"`
	ApproachLength  layoutField `json:"approachLength"`
	DepartureLength layoutField `json:"departureLength"`
}

func unavailableLayout(reason string) layoutSummary {
	field := layoutField{Reason: reason}
	return layoutSummary{field, field, field, field, field}
}

func inspectStationLayout(draft any, raw jsontext.Value) (layoutSummary, error) {
	var selection struct {
		StationID string `json:"stationID"`
		BankID    string `json:"bankID,omitempty"`
	}
	if raw.Kind() != '{' {
		return layoutSummary{}, errors.New("layout inspection needs a selection object")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return layoutSummary{}, fmt.Errorf("decode layout fields: %w", err)
	}
	if err := json.Unmarshal(raw, &selection, json.RejectUnknownMembers(true)); err != nil {
		return layoutSummary{}, fmt.Errorf("decode layout selection: %w", err)
	}
	_, bankSelected := fields["bankID"]
	if !validID(selection.StationID) || bankSelected && (!validID(selection.BankID) || fields["bankID"].Kind() != '"') {
		return layoutSummary{}, errors.New("layout inspection needs valid station and bank IDs")
	}
	g := geometryDraft{network: object(member(draft, "network"))}
	station, err := g.find("Stations", selection.StationID)
	if err != nil {
		return layoutSummary{}, err
	}
	var bank map[string]any
	if selection.BankID != "" {
		station, bank, err = g.findBank(selection.StationID, selection.BankID)
		if err != nil {
			return layoutSummary{}, err
		}
		station = bankStation(station, bank)
	}
	layout, err := g.stationLayoutFor(station)
	summary := unavailableLayout("Select a station bank for access lengths.")
	if err != nil {
		summary.Pitch, summary.Spacing, summary.Setback = layoutField{Reason: err.Error()}, layoutField{Reason: err.Error()}, layoutField{Reason: err.Error()}
	} else {
		summary.Spacing = layoutField{Value: &layout.spacing}
		summary.Pitch = layoutField{Value: layout.pitch}
		if layout.pitch == nil {
			summary.Pitch.Reason = "Pitch requires two berth rows."
		}
		summary.Setback = layoutField{Value: layout.setback}
		if layout.setback == nil {
			summary.Setback.Reason = "Setback requires an aligned entry/exit throat."
		}
	}
	if bank != nil {
		for _, target := range []struct {
			key   string
			field *layoutField
		}{{"approachLength", &summary.ApproachLength}, {"departureLength", &summary.DepartureLength}} {
			original, _ := g.find("Stations", selection.StationID)
			_, _, _, length, err := g.bankLengthAnchor(original, bank, target.key)
			if err != nil {
				*target.field = layoutField{Reason: err.Error()}
			} else {
				*target.field = layoutField{Value: &length}
			}
		}
	}
	return summary, nil
}
