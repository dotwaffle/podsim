package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func hasBanks(network any) bool {
	for _, station := range items(member(network, "stations")) {
		if has(station, "banks") {
			return true
		}
	}
	return false
}

func validateBankDraft(network any) error {
	raw, err := json.Marshal(network)
	if err != nil {
		return fmt.Errorf("encode bank geometry: %w", err)
	}
	var typed sim.Network
	if err := json.Unmarshal(raw, &typed, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("decode bank geometry: %w", err)
	}
	for _, station := range items(member(network, "stations")) {
		if has(station, "banks") {
			if err := validateBankMembers(station); err != nil {
				return err
			}
		}
	}
	if err := typed.ValidateStationBanks(); err != nil {
		return fmt.Errorf("validate station banks: %w", err)
	}
	if hasLargeGeometry(network) {
		if _, err := sim.PrepareNetwork(typed); err != nil {
			return fmt.Errorf("validate large geometry: %w", err)
		}
		return nil
	}
	if err := typed.ValidateBankGeometry(); err != nil {
		return fmt.Errorf("validate bank geometry: %w", err)
	}
	return nil
}

func validateBankMembers(station any) error {
	banks := items(member(station, "banks"))
	if len(banks) < 1 || len(banks) > sim.MaxStationBanks {
		return errors.New("a station needs 1 to 8 banks")
	}
	seen, assigned, berthIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, berth := range items(member(station, "berths")) {
		berthIDs[text(member(berth, "id"))] = true
	}
	for _, bank := range banks {
		if object(bank) == nil || len(object(bank)) != 4 || !validID(member(bank, "id")) || !validID(member(bank, "entry")) || !validID(member(bank, "exit")) {
			return errors.New("a bank needs ID, Entry, Exit, and BerthIDs")
		}
		id := text(member(bank, "id"))
		if seen[id] {
			return errors.New("bank IDs must be unique within the station")
		}
		seen[id] = true
		members := items(member(bank, "berthIDs"))
		if len(members) == 0 || len(members) > project.MaxBerths {
			return errors.New("a bank needs 1 to 200 berth IDs")
		}
		for _, value := range members {
			id := text(value)
			if !validID(value) || !berthIDs[id] || assigned[id] {
				return errors.New("each station berth must belong to exactly one bank")
			}
			assigned[id] = true
		}
	}
	if len(assigned) != len(berthIDs) {
		return errors.New("each station berth must belong to exactly one bank")
	}
	return nil
}

func refreshBankAliases(station map[string]any) {
	if banks := items(station["banks"]); len(banks) != 0 {
		station["entry"], station["exit"] = member(banks[0], "entry"), member(banks[0], "exit")
	}
}

func (g geometryDraft) setStationBanks(id string, raw jsontext.Value) error {
	station, err := g.find("stations", id)
	if err != nil {
		return err
	}
	if raw.Kind() != '[' {
		return errors.New("station banks must be an array")
	}
	var banks []any
	if err := json.Unmarshal(raw, &banks); err != nil {
		return fmt.Errorf("decode station banks: %w", err)
	}
	station["banks"] = banks
	if err := validateBankMembers(station); err != nil {
		return err
	}
	refreshBankAliases(station)
	return nil
}

func (g geometryDraft) setStationLegacy(id string) error {
	station, err := g.find("stations", id)
	if err != nil {
		return err
	}
	delete(station, "banks")
	var checks checkList
	checkNetwork(g.network, &checks)
	if len(checks.items) != 0 {
		return fmt.Errorf("legacy station topology is invalid: %s", checks.items[0].Text)
	}
	return nil
}

func bankStation(station, bank map[string]any) map[string]any {
	selected := map[string]bool{}
	for _, id := range items(bank["berthIDs"]) {
		selected[text(id)] = true
	}
	berths := []any{}
	for _, berth := range items(station["berths"]) {
		if selected[text(member(berth, "id"))] {
			berths = append(berths, berth)
		}
	}
	return map[string]any{"id": station["id"], "entry": bank["entry"], "exit": bank["exit"], "berths": berths}
}

func (g geometryDraft) findBank(stationID, bankID string) (map[string]any, map[string]any, error) {
	station, err := g.find("stations", stationID)
	if err != nil {
		return nil, nil, err
	}
	for _, bank := range items(station["banks"]) {
		if member(bank, "id") == bankID && bankID != "" {
			return station, object(bank), nil
		}
	}
	return nil, nil, errors.New("select an existing station bank")
}

func (g geometryDraft) addBankBerth(stationID, bankID string) error {
	station, bank, err := g.findBank(stationID, bankID)
	if err != nil {
		return err
	}
	if len(items(station["berths"])) >= project.MaxBerths {
		return errors.New("the station already has 200 berths")
	}
	selected := bankStation(station, bank)
	rows := g.berthChain(selected)
	if len(rows) == 0 {
		return errors.New("bank berth growth requires a berth chain")
	}
	if _, err := g.addChainBerth(selected, rows); err != nil {
		return err
	}
	berths := items(selected["berths"])
	berth := berths[len(berths)-1]
	station["berths"] = append(items(station["berths"]), berth)
	bank["berthIDs"] = append(items(bank["berthIDs"]), member(berth, "id"))
	return nil
}

func (g geometryDraft) setBankLayout(id string, raw jsontext.Value) error {
	if raw.Kind() != '{' {
		return errors.New("bank dimensions must be an object")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("decode bank dimensions: %w", err)
	}
	var bankID string
	if fields["bank"].Kind() != '"' || json.Unmarshal(fields["bank"], &bankID) != nil {
		return errors.New("bank dimensions need a bank ID")
	}
	station, bank, err := g.findBank(id, bankID)
	if err != nil {
		return err
	}
	if len(fields) < 2 {
		return errors.New("bank dimensions need at least one dimension")
	}
	values := make(map[string]float64)
	for key, raw := range fields {
		if key == "bank" {
			continue
		}
		minimum := math.SmallestNonzeroFloat64
		switch key {
		case "pitch":
			minimum = 25
		case "spacing":
			minimum = 48
		case "setback":
		case "approachLength", "departureLength":
			minimum = 24
		default:
			return fmt.Errorf("unknown bank dimension %s", key)
		}
		var value float64
		if raw.Kind() != '0' || json.Unmarshal(raw, &value) != nil || !finiteRange(value, minimum, math.MaxFloat64) {
			return fmt.Errorf("bank dimension %s must be finite and at least %g meters", key, minimum)
		}
		if key == "spacing" {
			value = max(value, 48+1e-9)
		}
		values[key] = value
	}
	for _, key := range []string{"pitch", "spacing", "setback", "approachLength", "departureLength"} {
		value, found := values[key]
		if !found {
			continue
		}
		if key == "approachLength" || key == "departureLength" {
			if err := g.setBankLaneLength(station, bank, key, value); err != nil {
				return err
			}
			continue
		}
		layout, err := g.stationLayoutFor(bankStation(station, bank))
		if err != nil {
			return err
		}
		dimensions, err := json.Marshal(map[string]float64{key: value})
		if err != nil {
			return fmt.Errorf("encode bank dimension: %w", err)
		}
		if err := g.setLayoutDimensions(layout, dimensions, false); err != nil {
			return err
		}
	}
	return nil
}

func (g geometryDraft) bankLengthAnchor(station, bank map[string]any, key string) (string, geometryPoint, geometryPoint, float64, error) {
	gate, role := text(bank["entry"]), "entry"
	if key == "departureLength" {
		gate, role = text(bank["exit"]), "exit"
	}
	var lanes []any
	for _, lane := range items(g.network["lanes"]) {
		if member(lane, "stationID") == station["id"] && member(lane, "stationRole") == role && (role == "entry" && member(lane, "to") == gate || role == "exit" && member(lane, "from") == gate) {
			lanes = append(lanes, lane)
		}
	}
	if len(lanes) != 1 || member(lanes[0], "control") != nil {
		return "", geometryPoint{}, geometryPoint{}, 0, errors.New("bank length requires one straight entry or exit lane")
	}
	anchor := text(member(lanes[0], "from"))
	if role == "exit" {
		anchor = text(member(lanes[0], "to"))
	}
	incident := []any{}
	for _, lane := range items(g.network["lanes"]) {
		if member(lane, "from") == anchor || member(lane, "to") == anchor {
			incident = append(incident, lane)
		}
	}
	if len(incident) != 2 {
		return "", geometryPoint{}, geometryPoint{}, 0, errors.New("bank length requires a dedicated anchor with two incident lanes")
	}
	for _, other := range items(g.network["stations"]) {
		if stationCoreNodes(other)[anchor] {
			return "", geometryPoint{}, geometryPoint{}, 0, errors.New("a bank anchor cannot be a station node")
		}
	}
	for _, lane := range incident {
		if member(lane, "control") != nil {
			return "", geometryPoint{}, geometryPoint{}, 0, errors.New("bank length requires straight anchor lanes")
		}
		if member(lane, "id") == member(lanes[0], "id") {
			continue
		}
		otherStation := text(member(lane, "stationID"))
		otherRole := "approach"
		if role == "exit" {
			otherRole = "exit"
		}
		if otherStation != "" && (otherStation != station["id"] || member(lane, "stationRole") != otherRole) || member(lane, "from") == gate || member(lane, "to") == gate {
			return "", geometryPoint{}, geometryPoint{}, 0, errors.New("a bank anchor cannot serve another station lane")
		}
	}
	at, err := g.point(anchor)
	if err != nil {
		return "", geometryPoint{}, geometryPoint{}, 0, err
	}
	fixed, err := g.point(gate)
	if err != nil {
		return "", geometryPoint{}, geometryPoint{}, 0, err
	}
	ray := geometryPoint{at.X - fixed.X, at.Y - fixed.Y}
	oldLength := math.Hypot(ray.X, ray.Y)
	if oldLength == 0 {
		return "", geometryPoint{}, geometryPoint{}, 0, errors.New("a bank anchor needs a nonzero lane ray")
	}
	return anchor, fixed, ray, oldLength, nil
}

func (g geometryDraft) setBankLaneLength(station, bank map[string]any, key string, length float64) error {
	anchor, fixed, ray, oldLength, err := g.bankLengthAnchor(station, bank, key)
	if err != nil {
		return err
	}
	target := geometryPoint{fixed.X + ray.X*length/oldLength, fixed.Y + ray.Y*length/oldLength}
	// Keep a requested minimum length above 24 after coordinate rounding.
	if math.Hypot(target.X-fixed.X, target.Y-fixed.Y) < length {
		target = geometryPoint{fixed.X + ray.X*(length+1e-9)/oldLength, fixed.Y + ray.Y*(length+1e-9)/oldLength}
	}
	position, err := geometryPosition(target)
	if err != nil {
		return err
	}
	node, err := g.find("nodes", anchor)
	if err != nil {
		return err
	}
	node["position"] = position
	return nil
}

func bankForBerth(station any, berthID string) any {
	for _, bank := range items(member(station, "banks")) {
		if slices.ContainsFunc(items(member(bank, "berthIDs")), func(id any) bool { return id == berthID }) {
			return bank
		}
	}
	return nil
}

func (g geometryDraft) validateBankChanges(original map[string]any) error {
	old := geometryDraft{network: original}
	oldCounts, _ := old.nodeLaneCounts()
	counts, order := g.nodeLaneCounts()
	for _, id := range order {
		if counts[id] > project.MaxNodeLanes && counts[id] > oldCounts[id] {
			return fmt.Errorf("node %s would exceed the 64-lane limit", id)
		}
	}
	for _, key := range []string{"nodes", "lanes", "stations"} {
		for _, item := range items(g.network[key]) {
			if !validID(member(item, "id")) {
				return errors.New("a generated item ID must contain 1 to 64 bytes")
			}
			if key == "nodes" {
				position := member(item, "position")
				if !finiteRange(number(member(position, "x")), -project.MaxCoordinate, project.MaxCoordinate) || !finiteRange(number(member(position, "y")), -project.MaxCoordinate, project.MaxCoordinate) {
					return errors.New("the geometry edit exceeds the coordinate limit")
				}
			}
		}
	}
	for _, station := range items(g.network["stations"]) {
		for _, berth := range items(member(station, "berths")) {
			if !validID(member(berth, "id")) {
				return errors.New("a berth ID must contain 1 to 64 bytes")
			}
		}
	}
	for _, lane := range items(g.network["lanes"]) {
		before, _ := old.find("lanes", text(member(lane, "id")))
		from, to := text(member(lane, "from")), text(member(lane, "to"))
		a, err := g.point(from)
		if err != nil {
			return err
		}
		b, err := g.point(to)
		if err != nil {
			return err
		}
		oldA, _ := old.point(from)
		oldB, _ := old.point(to)
		if before != nil && reflect.DeepEqual(before, lane) && a == oldA && b == oldB {
			continue
		}
		var control *geometryPoint
		if value := member(lane, "control"); value != nil {
			if !finite(member(value, "x")) || !finite(member(value, "y")) {
				return errors.New("the lane needs a finite control point")
			}
			control = &geometryPoint{number(member(value, "x")), number(member(value, "y"))}
		}
		points := geometryPolyline(a, b, control)
		length := 0.0
		for index := 1; index < len(points); index++ {
			length += math.Hypot(points[index].X-points[index-1].X, points[index].Y-points[index-1].Y)
		}
		minimum := draftLaneMinimum(lane)
		if length < minimum {
			return fmt.Errorf("lane %s would be shorter than %g meters", text(member(lane, "id")), minimum)
		}
	}
	return nil
}
