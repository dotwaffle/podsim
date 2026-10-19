package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/project"
)

// helperFields gives the request members of each helper operation other
// than op. A helper operation does not use the synchronized project.
var helperFields = map[string][]string{
	"backgroundMetadata":  {"metadata"},
	"stationLayout":       {"project", "layout"},
	"measurements":        {"project", "measure"},
	"stationObservations": {"state"},
	"observationScope":    {"scope"},
}

func helperOperation(op string) bool {
	_, ok := helperFields[op]
	return ok
}

func executeHelper(command request, input string) (response, error) {
	allowed := map[string]bool{"op": true}
	for _, field := range helperFields[command.Op] {
		allowed[field] = true
	}
	if command.Op == "stationObservations" {
		// The live state is large, so check the decoded members and do not
		// decode the request again.
		if len(command.Project) != 0 || len(command.ParkRide) != 0 || len(command.View) != 0 || command.Keys != nil || len(command.Patch) != 0 ||
			len(command.Edit) != 0 || len(command.History) != 0 || len(command.Metadata) != 0 || len(command.Layout) != 0 || len(command.Measure) != 0 || len(command.Scope) != 0 {
			return response{}, errors.New("station observations accept only a live state")
		}
		if command.State.Kind() != '{' {
			return response{}, errors.New("station observations need a live state object")
		}
		report, err := decodeStationObservations(command.State)
		return response{Observations: &report}, err
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
	switch command.Op {
	case "backgroundMetadata":
		metadata, err := backgroundMetadata(command.Metadata)
		return response{Valid: err == nil, Metadata: metadata}, err
	case "observationScope":
		scope, err := observationScopeText(command.Scope)
		return response{ScopeText: scope}, err
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
	if command.Op == "measurements" {
		report, err := inspectMeasurements(draft, command.Measure)
		return response{Measurements: &report}, err
	}
	summary, err := inspectStationLayout(draft, command.Layout)
	return response{Layout: &summary, helper: true}, err
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
	station, err := g.find("stations", selection.StationID)
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
			original, _ := g.find("stations", selection.StationID)
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
