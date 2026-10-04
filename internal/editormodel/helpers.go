package editormodel

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/project"
)

func helperOperation(op string) bool {
	return op == "backgroundMetadata" || op == "canonicalImport" || op == "stationLayout"
}

func executeHelper(command request, input string) (response, error) {
	allowed := map[string]bool{"op": true}
	switch command.Op {
	case "backgroundMetadata":
		allowed["metadata"] = true
	case "canonicalImport":
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
	if command.Op == "canonicalImport" {
		return response{Replace: canonicalProject(command.Project), helper: true}, nil
	}
	summary, err := inspectStationLayout(draft, command.Layout)
	return response{Layout: &summary, helper: true}, err
}

// canonicalProject gives the project with canonical member names when the
// server decoder accepts member names that differ only in case. The server
// decodes with encoding/json, which matches names without case and keeps the
// last of two such names. The canonical encoding keeps the member order of
// project.Config. Without such names, it gives nil and the imported draft
// stays unchanged.
func canonicalProject(raw jsontext.Value) jsontext.Value {
	type plainConfig project.Config
	var strict plainConfig
	if !errors.Is(json.Unmarshal(raw, &strict, json.RejectUnknownMembers(true)), json.ErrUnknownName) {
		return nil
	}
	var config project.Config
	if jsonv1.Unmarshal(raw, &config) != nil {
		return nil
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil
	}
	return encoded
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
