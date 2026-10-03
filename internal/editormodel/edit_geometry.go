package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strconv"

	"github.com/dotwaffle/podsim/internal/project"
)

type geometryPoint struct{ X, Y float64 }

type geometryEdit struct {
	Action string         `json:"action"`
	ID     string         `json:"id,omitempty"`
	To     string         `json:"to,omitempty"`
	Paired bool           `json:"paired,omitempty"`
	Point  jsontext.Value `json:"point,omitempty"`
	Delta  jsontext.Value `json:"delta,omitempty"`
	Value  jsontext.Value `json:"value,omitempty"`
}

var geometryFields = map[string][]string{
	"addNode": {"point"}, "addStation": {"point", "value"},
	"addLane":  {"id", "to", "paired", "point"},
	"moveNode": {"id", "point"}, "moveControl": {"id", "point"},
	"moveStation":    {"id", "delta"},
	"stationBearing": {"id", "value"}, "stationName": {"id", "value"}, "stationParking": {"id", "value"},
	"laneSpeed": {"id", "value"}, "toggleCurve": {"id"},
	"deleteNode": {"id"}, "deleteLane": {"id"},
	"deleteStation": {"id"},
	"addBerth":      {"id"}, "removeBerth": {"id", "value"},
	"stationLayout": {"id", "value"},
	"stationBanks":  {"id", "value"}, "bankLayout": {"id", "value"},
	"addBankBerth": {"id", "value"}, "stationLegacy": {"id"},
}

type geometryDraft struct {
	network map[string]any
	draft   any
	patch   map[string]any
}

func editGeometry(draft any, raw jsontext.Value) (projectChange, error) {
	command, err := decodeGeometryEdit(raw)
	if err != nil {
		return projectChange{}, err
	}
	original := object(member(draft, "network"))
	if original == nil {
		return projectChange{}, errors.New("the draft needs a network object")
	}
	change := projectChange{Patch: make(map[string]any)}
	geometry := geometryDraft{network: object(cloneEditValue(original)), draft: draft, patch: change.Patch}
	if err := geometry.change(command); err != nil {
		return projectChange{}, err
	}
	for key, limit := range map[string]int{"Nodes": project.MaxNodes, "Lanes": project.MaxLanes, "Stations": project.MaxStations} {
		if len(items(geometry.network[key])) > limit {
			return projectChange{}, fmt.Errorf("the edit exceeds the %s count limit", key)
		}
	}
	if hasBanks(original) || hasBanks(geometry.network) || hasLargeGeometry(original) || hasLargeGeometry(geometry.network) {
		if err := geometry.validateBankChanges(original); err != nil {
			return projectChange{}, err
		}
	}
	if hasBanks(geometry.network) || hasLargeGeometry(geometry.network) {
		if err := validateBankDraft(geometry.network); err != nil {
			return projectChange{}, err
		}
	}
	if number(member(draft, "version")) != 3 && (hasBanks(original) || command.Action == "stationBanks" || command.Action == "stationLegacy") {
		version := float64(1)
		if hasBanks(geometry.network) {
			version = 2
		}
		geometry.replaceBranch("version", version)
	}
	if !reflect.DeepEqual(original, geometry.network) {
		change.Patch["network"] = geometry.network
	}
	return change, nil
}

func decodeGeometryEdit(raw jsontext.Value) (geometryEdit, error) {
	if raw.Kind() != '{' {
		return geometryEdit{}, errors.New("a geometry edit needs a command object")
	}
	var command geometryEdit
	if err := json.Unmarshal(raw, &command, json.RejectUnknownMembers(true)); err != nil {
		return command, fmt.Errorf("decode geometry edit: %w", err)
	}
	allowed, found := geometryFields[command.Action]
	if !found {
		return command, errors.New("unknown geometry action")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields); err != nil {
		return command, fmt.Errorf("decode geometry fields: %w", err)
	}
	for key, value := range fields {
		if key != "action" && !slices.Contains(allowed, key) || value.Kind() == 'n' {
			return command, fmt.Errorf("the geometry action does not accept %s", key)
		}
	}
	if slices.Contains(allowed, "id") && command.ID == "" {
		return command, errors.New("the geometry edit needs an item ID")
	}
	if command.Action == "addLane" && (command.To == "" || command.ID == command.To) {
		return command, errors.New("a lane needs two different end nodes")
	}
	if slices.Contains(allowed, "point") && command.Action != "addLane" {
		if _, err := decodeGeometryPoint(command.Point); err != nil {
			return command, err
		}
	}
	if command.Action == "moveStation" {
		if _, err := decodeGeometryPoint(command.Delta); err != nil {
			return command, err
		}
	}
	if slices.Contains(allowed, "value") && command.Action != "addStation" && len(command.Value) == 0 {
		return command, errors.New("the geometry edit needs a value")
	}
	return command, nil
}

func decodeGeometryPoint(raw jsontext.Value) (geometryPoint, error) {
	var point map[string]any
	if raw.Kind() != '{' {
		return geometryPoint{}, errors.New("a geometry point must be an object")
	}
	if err := json.Unmarshal(raw, &point); err != nil {
		return geometryPoint{}, fmt.Errorf("decode geometry point: %w", err)
	}
	if len(point) != 2 || !finite(point["X"]) || !finite(point["Y"]) {
		return geometryPoint{}, errors.New("a geometry point needs finite X and Y coordinates")
	}
	return geometryPoint{X: number(point["X"]), Y: number(point["Y"])}, nil
}

func (g geometryDraft) find(key, id string) (map[string]any, error) {
	if id == "" {
		return nil, errors.New("a geometry reference needs a nonempty ID")
	}
	for _, item := range items(g.network[key]) {
		if text(member(item, "ID")) == id && object(item) != nil {
			return object(item), nil
		}
	}
	return nil, fmt.Errorf("the %s item %s no longer exists", key, id)
}

func (g geometryDraft) point(id string) (geometryPoint, error) {
	node, err := g.find("Nodes", id)
	if err != nil {
		return geometryPoint{}, err
	}
	position := object(node["Position"])
	if !finite(position["X"]) || !finite(position["Y"]) {
		return geometryPoint{}, errors.New("the node needs finite coordinates")
	}
	return geometryPoint{number(position["X"]), number(position["Y"])}, nil
}

func (g geometryDraft) allIDs() map[string]bool {
	ids := make(map[string]bool)
	for _, key := range []string{"Nodes", "Lanes", "Stations"} {
		for _, item := range items(g.network[key]) {
			ids[text(member(item, "ID"))] = true
			if key == "Stations" {
				for _, berth := range items(member(item, "Berths")) {
					ids[text(member(berth, "ID"))] = true
				}
			}
		}
	}
	for _, pod := range items(member(g.draft, "fleet")) {
		ids[text(member(pod, "ID"))] = true
	}
	return ids
}

func (g geometryDraft) nextID(prefix string) string {
	ids := g.allIDs()
	for next := 1; ; next++ {
		id := prefix + "-" + strconv.Itoa(next)
		if !ids[id] {
			return id
		}
	}
}

func geometryPosition(point geometryPoint) (map[string]any, error) {
	if !finiteRange(point.X, -project.MaxCoordinate, project.MaxCoordinate) || !finiteRange(point.Y, -project.MaxCoordinate, project.MaxCoordinate) {
		return nil, errors.New("the geometry edit exceeds the coordinate limit")
	}
	return map[string]any{"X": point.X, "Y": point.Y}, nil
}

func (g geometryDraft) addNode(prefix string, point geometryPoint) (string, error) {
	position, err := geometryPosition(point)
	if err != nil {
		return "", err
	}
	id := g.nextID(prefix)
	g.network["Nodes"] = append(items(g.network["Nodes"]), map[string]any{"ID": id, "Position": position})
	return id, nil
}

func (g geometryDraft) addLane(from, to, station, role string, control *geometryPoint) error {
	if _, err := g.point(from); err != nil {
		return err
	}
	if _, err := g.point(to); err != nil {
		return err
	}
	for _, item := range items(g.network["Lanes"]) {
		if member(item, "From") == from && member(item, "To") == to {
			return nil
		}
	}
	lane := map[string]any{"ID": g.nextID("lane"), "From": from, "To": to, "SpeedLimit": float64(12)}
	if station != "" {
		lane["StationID"], lane["StationRole"] = station, role
	}
	if control != nil {
		position, err := geometryPosition(*control)
		if err != nil {
			return err
		}
		lane["Control"] = position
	}
	g.network["Lanes"] = append(items(g.network["Lanes"]), lane)
	return nil
}

func (g geometryDraft) change(command geometryEdit) error {
	switch command.Action {
	case "addNode":
		point, _ := decodeGeometryPoint(command.Point)
		_, err := g.addNode("node", point)
		return err
	case "addStation":
		return g.addStation(command)
	case "addLane":
		var control *geometryPoint
		if len(command.Point) != 0 {
			point, err := decodeGeometryPoint(command.Point)
			if err != nil {
				return err
			}
			control = &point
		}
		if err := g.addLane(command.ID, command.To, "", "", control); err != nil {
			return err
		}
		if command.Paired {
			return g.addLane(command.To, command.ID, "", "", control)
		}
		return nil
	case "moveNode", "moveControl":
		return g.movePoint(command)
	case "moveStation", "stationBearing":
		return g.moveStation(command)
	case "stationName", "stationParking", "laneSpeed":
		return g.setField(command)
	case "toggleCurve":
		return g.toggleCurve(command.ID)
	case "deleteLane":
		if _, err := g.find("Lanes", command.ID); err != nil {
			return err
		}
		g.network["Lanes"] = slices.DeleteFunc(items(g.network["Lanes"]), func(lane any) bool { return member(lane, "ID") == command.ID })
		return nil
	case "deleteNode":
		return g.deleteNode(command.ID)
	case "deleteStation":
		return g.deleteStation(command.ID)
	case "addBerth":
		return g.addBerth(command.ID)
	case "removeBerth":
		var berthID string
		if err := json.Unmarshal(command.Value, &berthID); err != nil {
			return fmt.Errorf("decode berth ID: %w", err)
		}
		return g.removeBerth(command.ID, berthID)
	case "stationLayout":
		return g.setStationLayout(command.ID, command.Value)
	case "stationBanks":
		return g.setStationBanks(command.ID, command.Value)
	case "stationLegacy":
		return g.setStationLegacy(command.ID)
	case "bankLayout":
		return g.setBankLayout(command.ID, command.Value)
	case "addBankBerth":
		var bankID string
		if err := json.Unmarshal(command.Value, &bankID); err != nil {
			return fmt.Errorf("decode bank ID: %w", err)
		}
		return g.addBankBerth(command.ID, bankID)
	}
	return errors.New("unknown geometry action")
}

func (g geometryDraft) addStation(command geometryEdit) error {
	point, _ := decodeGeometryPoint(command.Point)
	var options struct {
		Name        string `json:"name"`
		ParkingOnly bool   `json:"parkingOnly"`
	}
	if len(command.Value) != 0 {
		if command.Value.Kind() != '{' {
			return errors.New("station options must be an object")
		}
		if err := json.Unmarshal(command.Value, &options, json.RejectUnknownMembers(true)); err != nil {
			return fmt.Errorf("decode station options: %w", err)
		}
	}
	id := g.nextID("station")
	entry, err := g.addNode(id+"-entry", geometryPoint{point.X - 36, point.Y})
	if err != nil {
		return err
	}
	exit, err := g.addNode(id+"-exit", geometryPoint{point.X + 36, point.Y})
	if err != nil {
		return err
	}
	berthID := g.nextID(id + "-berth")
	berthNode, err := g.addNode(id+"-berth-node", geometryPoint{point.X, point.Y + 30})
	if err != nil {
		return err
	}
	name := options.Name
	if name == "" {
		name = "Station " + strconv.Itoa(len(items(g.network["Stations"]))+1)
	}
	g.network["Stations"] = append(items(g.network["Stations"]), map[string]any{"ID": id, "Name": name, "Entry": entry, "Exit": exit, "ParkingOnly": options.ParkingOnly, "Berths": []any{map[string]any{"ID": berthID, "Node": berthNode}}})
	for _, lane := range [][3]string{{entry, berthNode, "berth-access"}, {berthNode, exit, "departure"}, {entry, exit, "through"}} {
		if err := g.addLane(lane[0], lane[1], id, lane[2], nil); err != nil {
			return err
		}
	}
	return nil
}

func (g geometryDraft) movePoint(command geometryEdit) error {
	point, _ := decodeGeometryPoint(command.Point)
	position, err := geometryPosition(point)
	if err != nil {
		return err
	}
	key, field := "Nodes", "Position"
	if command.Action == "moveControl" {
		key, field = "Lanes", "Control"
	}
	item, err := g.find(key, command.ID)
	if err != nil {
		return err
	}
	item[field] = position
	return nil
}

func (g geometryDraft) setField(command geometryEdit) error {
	key := "Stations"
	if command.Action == "laneSpeed" {
		key = "Lanes"
	}
	item, err := g.find(key, command.ID)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(command.Value, &value); err != nil {
		return fmt.Errorf("decode geometry field: %w", err)
	}
	switch command.Action {
	case "stationName":
		name, ok := value.(string)
		if !ok {
			return errors.New("a station name must be text")
		}
		item["Name"] = trimEditorSpace(name)
	case "stationParking":
		flag, ok := value.(bool)
		if !ok {
			return errors.New("the parking option must be true or false")
		}
		item["ParkingOnly"] = flag
	case "laneSpeed":
		speed, err := editNumber(value)
		if err != nil {
			return err
		}
		item["SpeedLimit"] = speed / 3.6
	}
	return nil
}

func (g geometryDraft) toggleCurve(id string) error {
	lane, err := g.find("Lanes", id)
	if err != nil {
		return err
	}
	if lane["Control"] != nil {
		delete(lane, "Control")
		return nil
	}
	from, err := g.point(text(lane["From"]))
	if err != nil {
		return err
	}
	to, err := g.point(text(lane["To"]))
	if err != nil {
		return err
	}
	position, err := geometryPosition(geometryPoint{(from.X+to.X)/2 - (to.Y-from.Y)*.25, (from.Y+to.Y)/2 + (to.X-from.X)*.25})
	if err == nil {
		lane["Control"] = position
	}
	return err
}

func stationCoreNodes(station any) map[string]bool {
	ids := make(map[string]bool)
	for _, key := range []string{"Entry", "Exit"} {
		if id := text(member(station, key)); id != "" {
			ids[id] = true
		}
	}
	for _, bank := range items(member(station, "Banks")) {
		for _, key := range []string{"Entry", "Exit"} {
			if id := text(member(bank, key)); id != "" {
				ids[id] = true
			}
		}
	}
	for _, berth := range items(member(station, "Berths")) {
		if id := text(member(berth, "Node")); id != "" {
			ids[id] = true
		}
	}
	return ids
}

func (g geometryDraft) nodeOwners() map[string]string {
	stationIDs := make(map[string]bool)
	for _, station := range items(g.network["Stations"]) {
		stationIDs[text(member(station, "ID"))] = true
	}
	owners := make(map[string]string)
	for _, lane := range items(g.network["Lanes"]) {
		station := text(member(lane, "StationID"))
		if !stationIDs[station] {
			station = ""
		}
		for _, id := range []string{text(member(lane, "From")), text(member(lane, "To"))} {
			previous, found := owners[id]
			if found && previous != station {
				owners[id] = ""
			} else {
				owners[id] = station
			}
		}
	}
	for id, station := range owners {
		if station == "" {
			delete(owners, id)
		}
	}
	for _, station := range items(g.network["Stations"]) {
		for id := range stationCoreNodes(station) {
			owners[id] = text(member(station, "ID"))
		}
	}
	return owners
}

func (g geometryDraft) stationNodes(station any) map[string]bool {
	ids := stationCoreNodes(station)
	for id, owner := range g.nodeOwners() {
		if owner == member(station, "ID") {
			ids[id] = true
		}
	}
	return ids
}

func (g geometryDraft) moveStation(command geometryEdit) error {
	station, err := g.find("Stations", command.ID)
	if err != nil {
		return err
	}
	for _, key := range []string{"Entry", "Exit"} {
		if _, referenceErr := g.point(text(station[key])); referenceErr != nil {
			return referenceErr
		}
	}
	delta, center, cos, sin := geometryPoint{}, geometryPoint{}, 1.0, 0.0
	if command.Action == "moveStation" {
		delta, _ = decodeGeometryPoint(command.Delta)
	} else {
		entry, err := g.point(text(station["Entry"]))
		if err != nil {
			return err
		}
		exit, err := g.point(text(station["Exit"]))
		if err != nil {
			return err
		}
		var value any
		if decodeErr := json.Unmarshal(command.Value, &value); decodeErr != nil {
			return fmt.Errorf("decode bearing: %w", decodeErr)
		}
		if value == "" {
			return errors.New("a station bearing must not be empty")
		}
		bearing, err := editNumber(value)
		if err != nil {
			return err
		}
		center = geometryPoint{(entry.X + exit.X) / 2, (entry.Y + exit.Y) / 2}
		x, y := exit.X-entry.X, exit.Y-entry.Y
		if x == 0 && y == 0 {
			x = 1
		}
		current := math.Mod(math.Atan2(x, -y)*180/math.Pi+360, 360)
		degrees := math.Mod(math.Mod(bearing-current, 360)+540, 360) - 180
		cos, sin = math.Cos(degrees*math.Pi/180), math.Sin(degrees*math.Pi/180)
	}
	turn := func(point geometryPoint) (map[string]any, error) {
		x, y := point.X-center.X, point.Y-center.Y
		return geometryPosition(geometryPoint{center.X + x*cos - y*sin + delta.X, center.Y + x*sin + y*cos + delta.Y})
	}
	ids := g.stationNodes(station)
	for _, node := range items(g.network["Nodes"]) {
		if object(node) == nil {
			continue
		}
		if !ids[text(member(node, "ID"))] {
			continue
		}
		point, err := g.point(text(member(node, "ID")))
		if err != nil {
			return err
		}
		updated, err := turn(point)
		if err != nil {
			return err
		}
		object(node)["Position"] = updated
	}
	for _, lane := range items(g.network["Lanes"]) {
		if member(lane, "Control") == nil || !ids[text(member(lane, "From"))] || !ids[text(member(lane, "To"))] {
			continue
		}
		control := object(member(lane, "Control"))
		if !finite(control["X"]) || !finite(control["Y"]) {
			return errors.New("the lane needs a finite control point")
		}
		updated, err := turn(geometryPoint{number(control["X"]), number(control["Y"])})
		if err != nil {
			return err
		}
		object(lane)["Control"] = updated
	}
	return nil
}

func (g geometryDraft) deleteNode(id string) error {
	if _, owned := g.nodeOwners()[id]; owned {
		return errors.New("delete the station or berth from its controls")
	}
	if _, err := g.find("Nodes", id); err != nil {
		return err
	}
	g.network["Nodes"] = slices.DeleteFunc(items(g.network["Nodes"]), func(node any) bool { return member(node, "ID") == id })
	g.network["Lanes"] = slices.DeleteFunc(items(g.network["Lanes"]), func(lane any) bool { return member(lane, "From") == id || member(lane, "To") == id })
	return nil
}
