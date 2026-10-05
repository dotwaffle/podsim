package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
)

type stationDimensions struct {
	station               map[string]any
	rows                  []berthRow
	origin, along, across geometryPoint
	side, spacing         float64
	body                  map[string]bool
	pitch, setback        *float64
}

func (g geometryDraft) stationLayout(id string) (stationDimensions, error) {
	station, err := g.find("stations", id)
	if err != nil {
		return stationDimensions{}, err
	}
	if has(station, "banks") {
		return stationDimensions{}, errors.New("select a bank for layout changes")
	}
	return g.stationLayoutFor(station)
}

func (g geometryDraft) stationLayoutFor(station map[string]any) (stationDimensions, error) {
	id := text(station["id"])
	rows := g.berthChain(station)
	if len(rows) == 0 {
		return stationDimensions{}, errors.New("layout controls require a straight berth chain")
	}
	entry, err := g.point(text(station["entry"]))
	if err != nil {
		return stationDimensions{}, err
	}
	exit, err := g.point(text(station["exit"]))
	if err != nil {
		return stationDimensions{}, err
	}
	origin, along, across := geometryAxes(entry, exit)
	layout := stationDimensions{station: station, rows: rows, origin: origin, along: along, across: across, spacing: math.Hypot(exit.X-entry.X, exit.Y-entry.Y), body: map[string]bool{text(station["entry"]): true, text(station["exit"]): true}}
	if layout.spacing < 48 {
		return stationDimensions{}, errors.New("entry/exit spacing must be at least 48 meters")
	}
	rowNodes, rowLanes := map[string]bool{}, map[string]bool{}
	for _, row := range rows {
		for _, node := range []string{row.arrival, text(row.berth["node"]), row.departure} {
			if layout.body[node] {
				return stationDimensions{}, errors.New("berth rows must use distinct nodes")
			}
			layout.body[node], rowNodes[node] = true, true
		}
		for _, lane := range []map[string]any{row.arrivalLink, row.departureLink, row.inlet, row.outlet} {
			rowLanes[text(lane["id"])] = true
		}
	}
	for _, other := range items(g.network["stations"]) {
		if member(other, "id") == id {
			continue
		}
		for node := range stationCoreNodes(other) {
			if layout.body[node] {
				return stationDimensions{}, errors.New("another station shares these nodes")
			}
		}
	}
	for _, lane := range items(g.network["lanes"]) {
		from, to := text(member(lane, "from")), text(member(lane, "to"))
		if !layout.body[from] && !layout.body[to] {
			continue
		}
		if member(lane, "control") != nil {
			return stationDimensions{}, fmt.Errorf("curved lane %s requires manual node edits", text(member(lane, "id")))
		}
		if (rowNodes[from] || rowNodes[to]) && (!rowLanes[text(member(lane, "id"))] || member(lane, "stationID") != id) {
			return stationDimensions{}, fmt.Errorf("lane %s shares a berth row node", text(member(lane, "id")))
		}
	}
	if err := g.alignedRows(&layout); err != nil {
		return stationDimensions{}, err
	}
	g.layoutSetback(&layout)
	return layout, nil
}

func (g geometryDraft) layoutOffset(layout stationDimensions, id string, along bool) float64 {
	point, err := g.point(id)
	if err != nil {
		return math.NaN()
	}
	axis := layout.across
	if along {
		axis = layout.along
	}
	return (point.X-layout.origin.X)*axis.X + (point.Y-layout.origin.Y)*axis.Y
}

func geometryNear(a, b float64) bool {
	return !math.IsNaN(a) && !math.IsNaN(b) && !math.IsInf(a, 0) && !math.IsInf(b, 0) && math.Abs(a-b) < 1e-6
}

func (g geometryDraft) alignedRows(layout *stationDimensions) error {
	depth := g.layoutOffset(*layout, text(layout.rows[0].berth["node"]), false)
	if depth == 0 || math.IsNaN(depth) {
		return errors.New("berth rows must lie on one side of the station mouth")
	}
	layout.side = 1
	if depth < 0 {
		layout.side = -1
	}
	first := layout.side * depth
	if len(layout.rows) > 1 {
		pitch := layout.side*g.layoutOffset(*layout, text(layout.rows[1].berth["node"]), false) - first
		layout.pitch = &pitch
	}
	for index, row := range layout.rows {
		depth := layout.side * g.layoutOffset(*layout, text(row.berth["node"]), false)
		if !geometryNear(g.layoutOffset(*layout, row.arrival, true), -layout.spacing/2) || !geometryNear(g.layoutOffset(*layout, row.departure, true), layout.spacing/2) || !geometryNear(g.layoutOffset(*layout, text(row.berth["node"]), true), 0) ||
			!geometryNear(g.layoutOffset(*layout, row.arrival, false), layout.side*depth) || !geometryNear(g.layoutOffset(*layout, row.departure, false), layout.side*depth) || depth <= 0 ||
			layout.pitch != nil && (*layout.pitch < 25-1e-6 || !geometryNear(depth, first+float64(index)**layout.pitch)) {
			return errors.New("berth rows need an aligned rectangular chain with uniform pitch of at least 25 meters")
		}
	}
	return nil
}

func (g geometryDraft) layoutSetback(layout *stationDimensions) {
	incoming, outgoing := []any{}, []any{}
	id := text(layout.station["id"])
	for _, lane := range items(g.network["lanes"]) {
		if member(lane, "stationID") != id {
			continue
		}
		if member(lane, "to") == layout.station["entry"] && member(lane, "stationRole") == "entry" {
			incoming = append(incoming, lane)
		}
		if member(lane, "from") == layout.station["exit"] && member(lane, "stationRole") == "exit" {
			outgoing = append(outgoing, lane)
		}
	}
	if len(incoming) != 1 || len(outgoing) != 1 {
		return
	}
	from, to := text(member(incoming[0], "from")), text(member(outgoing[0], "to"))
	if layout.body[from] || layout.body[to] || !geometryNear(g.layoutOffset(*layout, from, false), g.layoutOffset(*layout, to, false)) || !geometryNear(g.layoutOffset(*layout, from, true)+g.layoutOffset(*layout, to, true), 0) {
		return
	}
	if depth := -layout.side * g.layoutOffset(*layout, from, false); depth > 0 {
		layout.setback = &depth
	}
}

func (g geometryDraft) setStationLayout(id string, raw jsontext.Value) error {
	layout, err := g.stationLayout(id)
	if err != nil {
		return err
	}
	return g.setLayoutDimensions(layout, raw, true)
}

func (g geometryDraft) setLayoutDimensions(layout stationDimensions, raw jsontext.Value, check bool) error {
	if raw.Kind() != '{' {
		return errors.New("station dimensions must be an object")
	}
	var dimensions map[string]float64
	if err := json.Unmarshal(raw, &dimensions); err != nil {
		return fmt.Errorf("decode station dimensions: %w", err)
	}
	previous := map[string]*float64{"pitch": layout.pitch, "spacing": &layout.spacing, "setback": layout.setback}
	changed := false
	for key, value := range dimensions {
		old, found := previous[key]
		if !found {
			return fmt.Errorf("unknown station dimension %s", key)
		}
		if old == nil {
			return fmt.Errorf("the station does not support %s changes", key)
		}
		minimum := math.SmallestNonzeroFloat64
		switch key {
		case "pitch":
			minimum = 25
		case "spacing":
			minimum = 48
		}
		if !finiteRange(value, minimum, math.MaxFloat64) {
			return fmt.Errorf("station dimension %s must be finite and at least %g meters", key, minimum)
		}
		changed = changed || math.Abs(value-*old) > 1e-6
	}
	if !changed {
		return nil
	}
	moves := make(map[string]geometryPoint)
	add := func(id string, along, across float64) {
		old := moves[id]
		moves[id] = geometryPoint{old.X + along*layout.along.X + across*layout.across.X, old.Y + along*layout.along.Y + across*layout.across.Y}
	}
	delta := func(key string) float64 {
		if value, found := dimensions[key]; found {
			return value - *previous[key]
		}
		return 0
	}
	if spacing := delta("spacing"); spacing != 0 {
		add(text(layout.station["entry"]), -spacing/2, 0)
		add(text(layout.station["exit"]), spacing/2, 0)
		for _, row := range layout.rows {
			add(row.arrival, -spacing/2, 0)
			add(row.departure, spacing/2, 0)
		}
	}
	if pitch := delta("pitch"); pitch != 0 {
		for index, row := range layout.rows {
			for _, id := range []string{row.arrival, text(row.berth["node"]), row.departure} {
				add(id, 0, layout.side*float64(index)*pitch)
			}
		}
	}
	if setback := delta("setback"); setback != 0 {
		for id := range layout.body {
			add(id, 0, layout.side*setback)
		}
	}
	moved := make(map[string]bool)
	for _, node := range items(g.network["nodes"]) {
		if object(node) == nil {
			continue
		}
		id := text(member(node, "id"))
		offset, found := moves[id]
		if !found || offset.X == 0 && offset.Y == 0 {
			continue
		}
		point, err := g.point(id)
		if err != nil {
			return err
		}
		position, err := geometryPosition(geometryPoint{point.X + offset.X, point.Y + offset.Y})
		if err != nil {
			return err
		}
		object(node)["position"] = position
		moved[id] = true
	}
	lanes := []string{}
	for _, lane := range items(g.network["lanes"]) {
		from, to := text(member(lane, "from")), text(member(lane, "to"))
		if !moved[from] && !moved[to] {
			continue
		}
		a, err := g.point(from)
		if err != nil {
			return err
		}
		b, err := g.point(to)
		if err != nil {
			return err
		}
		minimum := draftLaneMinimum(lane)
		if length := draftLaneLength(map[string]any{"x": a.X, "y": a.Y}, map[string]any{"x": b.X, "y": b.Y}, member(lane, "control")); check && length < minimum {
			return fmt.Errorf("lane %s would be shorter than %g meters", text(member(lane, "id")), minimum)
		}
		lanes = append(lanes, text(member(lane, "id")))
	}
	if !check {
		return nil
	}
	if conflict := g.laneConflict(lanes, true); conflict != nil {
		return fmt.Errorf("lane %s would come within %g meters of lane %s", conflict.lane, conflict.minimum, conflict.other)
	}
	return nil
}
