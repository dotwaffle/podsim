package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func bankEditorFixture(count int) map[string]any {
	network := map[string]any{"Nodes": []any{}, "Lanes": []any{}, "Stations": []any{}}
	station := map[string]any{"ID": "station", "Name": "Station", "Entry": "a-entry", "Exit": "a-exit", "ParkingOnly": false, "Berths": []any{}, "Banks": []any{}}
	node := func(id string, x, y float64) {
		network["Nodes"] = append(items(network["Nodes"]), map[string]any{"ID": id, "Position": map[string]any{"X": x, "Y": y}})
	}
	lane := func(id, from, to, role string) {
		item := map[string]any{"ID": id, "From": from, "To": to, "SpeedLimit": float64(14)}
		if role != "" {
			item["StationID"], item["StationRole"] = "station", role
		}
		network["Lanes"] = append(items(network["Lanes"]), item)
	}
	for index, prefix := range []string{"a", "b"} {
		x := float64(index) * 600
		node(prefix+"-entry", x-100, 120)
		node(prefix+"-exit", x+100, 120)
		node(prefix+"-approach", x-60, 0)
		node(prefix+"-departure", x+60, 0)
		node(prefix+"-road-in", x-60, -100)
		node(prefix+"-road-out", x+60, -100)
		lane(prefix+"-access-in", prefix+"-approach", prefix+"-entry", "entry")
		lane(prefix+"-access-out", prefix+"-exit", prefix+"-departure", "exit")
		lane(prefix+"-road-in", prefix+"-road-in", prefix+"-approach", "")
		lane(prefix+"-road-out", prefix+"-departure", prefix+"-road-out", "")
		lane(prefix+"-through", prefix+"-entry", prefix+"-exit", "through")
		ids := []any{}
		for row := range count {
			suffix := strconv.Itoa(row)
			arrival, berth, departure := prefix+"-arrival-"+suffix, prefix+"-berth-"+suffix, prefix+"-departure-"+suffix
			y := 210 + float64(row)*75
			node(arrival, x-100, y)
			node(berth, x, y)
			node(departure, x+100, y)
			from, to := prefix+"-entry", prefix+"-exit"
			if row != 0 {
				from = prefix + "-arrival-" + strconv.Itoa(row-1)
				to = prefix + "-departure-" + strconv.Itoa(row-1)
			}
			lane(prefix+"-al-"+suffix, from, arrival, "berth-access")
			lane(prefix+"-dl-"+suffix, departure, to, "departure")
			lane(prefix+"-in-"+suffix, arrival, berth, "berth-access")
			lane(prefix+"-out-"+suffix, berth, departure, "departure")
			station["Berths"] = append(items(station["Berths"]), map[string]any{"ID": berth, "Node": berth})
			ids = append(ids, berth)
		}
		station["Banks"] = append(items(station["Banks"]), map[string]any{"ID": prefix, "Entry": prefix + "-entry", "Exit": prefix + "-exit", "BerthIDs": ids})
	}
	network["Stations"] = []any{station}
	return map[string]any{"version": float64(2), "name": "Banks", "network": network, "fleet": []any{map[string]any{"ID": "pod", "BerthID": "a-berth-0"}}}
}

func applyBankEdit(t *testing.T, draft map[string]any, raw string) map[string]any {
	t.Helper()
	before := cloneEditValue(draft)
	change, err := editGeometry(draft, jsontext.Value(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(draft, before) {
		t.Fatal("the proposal changed its source")
	}
	out := object(cloneEditValue(draft))
	maps.Copy(out, change.Patch)
	return out
}

func TestBankLayoutScopeAndOrder(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(3)
	if err := validateBankDraft(draft["network"]); err != nil {
		t.Fatal(err)
	}
	raw := `{"action":"bankLayout","id":"station","value":{"bank":"a","departureLength":150,"setback":140,"approachLength":160,"spacing":160,"pitch":50}}`
	changed := applyBankEdit(t, draft, raw)
	g := geometryDraft{network: object(changed["network"])}
	station, bank, err := g.findBank("station", "a")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := g.stationLayoutFor(bankStation(station, bank))
	if err != nil || !geometryNear(*layout.pitch, 50) || !geometryNear(layout.spacing, 160) {
		t.Fatal("bank dimensions differ", layout, err)
	}
	for _, id := range []string{"a-access-in", "a-access-out"} {
		lane, err := g.find("Lanes", id)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := g.point(text(lane["From"]))
		b, _ := g.point(text(lane["To"]))
		expected := 160.0
		if id == "a-access-out" {
			expected = 150
		}
		if !geometryNear(draftLaneLength(map[string]any{"X": a.X, "Y": a.Y}, map[string]any{"X": b.X, "Y": b.Y}, nil), expected) {
			t.Fatal("length control did not use the final bank gate")
		}
	}
	original := geometryDraft{network: object(draft["network"])}
	for _, node := range items(g.network["Nodes"]) {
		if strings.HasPrefix(text(member(node, "ID")), "b-") {
			before, _ := original.find("Nodes", text(member(node, "ID")))
			if !reflect.DeepEqual(before, node) {
				t.Fatal("the edit changed the other bank")
			}
		}
	}
	if !reflect.DeepEqual(member(draft, "fleet"), member(changed, "fleet")) || !reflect.DeepEqual(member(draft["network"], "Lanes"), member(changed["network"], "Lanes")) {
		t.Fatal("layout changed IDs or fleet")
	}
}

func TestBankCommandsRejectAtomically(t *testing.T) {
	t.Parallel()
	commands := []string{
		`{"action":"addBerth","id":"station"}`,
		`{"action":"stationLayout","id":"station","value":{"pitch":50}}`,
		`{"action":"addBankBerth","id":"station","value":"missing"}`,
		`{"action":"stationBanks","id":"station","value":null}`,
		`{"action":"stationBanks","id":"station","value":[]}`,
		`{"action":"stationBanks","id":"station","value":[{"ID":"a","Entry":"a-entry","Exit":"a-exit","BerthIDs":["a-berth-0"]}]}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a"}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","pitch":null}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","pitch":24}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","spacing":47}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","setback":0}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","approachLength":23}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","pitch":"50"}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","unknown":50}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","pitch":50},"paired":true}`,
		`{"action":"stationLegacy","id":"station","value":true}`,
		`{"action":"stationLegacy","id":"station"}`,
		`{"action":"deleteLane","id":"a-through"}`,
		`{"action":"deleteNode","id":"b-entry"}`,
		`{"action":"moveNode","id":"b-berth-0","point":{"X":0,"Y":211}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","setback":100001}}`,
	}
	for _, raw := range commands {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			draft := bankEditorFixture(3)
			before := cloneEditValue(draft)
			if _, err := editGeometry(draft, jsontext.Value(raw)); err == nil {
				t.Fatal("invalid bank edit accepted")
			}
			if !reflect.DeepEqual(draft, before) {
				t.Fatal("rejected edit changed its source")
			}
		})
	}
}

func TestBankBerthMembershipAndTransforms(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(1)
	grown := applyBankEdit(t, draft, `{"action":"addBankBerth","id":"station","value":"b"}`)
	station := items(member(grown["network"], "Stations"))[0]
	last := member(items(member(station, "Berths"))[2], "ID")
	if !slices.Contains(items(member(items(member(station, "Banks"))[1], "BerthIDs")), last) {
		t.Fatal("new berth has no bank membership")
	}
	removed := applyBankEdit(t, grown, `{"action":"removeBerth","id":"station","value":"a-berth-0"}`)
	station = items(member(removed["network"], "Stations"))[0]
	if len(items(member(station, "Banks"))) != 1 || member(station, "Entry") != "b-entry" || len(items(member(removed["network"], "Nodes"))) != 16 {
		t.Fatal("empty bank topology or aliases remain", station)
	}
	moved := applyBankEdit(t, removed, `{"action":"moveStation","id":"station","delta":{"X":10,"Y":15}}`)
	before := geometryDraft{network: object(removed["network"])}
	after := geometryDraft{network: object(moved["network"])}
	for id := range before.stationNodes(station) {
		a, _ := before.point(id)
		b, _ := after.point(id)
		if b.X != a.X+10 || b.Y != a.Y+15 {
			t.Fatal("station move omitted a bank node", id)
		}
	}
	deleted := applyBankEdit(t, grown, `{"action":"deleteStation","id":"station"}`)
	if len(items(member(deleted["network"], "Stations"))) != 0 || deleted["version"] != float64(1) || len(items(deleted["fleet"])) != 0 {
		t.Fatal("bank delete did not restore legacy project version")
	}
}

func TestBankMembershipAndLegacyVersion(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(1)
	network := object(draft["network"])
	station := object(items(network["Stations"])[0])
	// One bank can use the existing legacy entry and exit.
	network["Stations"] = []any{station}
	station["Berths"] = items(station["Berths"])[:1]
	station["Banks"] = items(station["Banks"])[:1]
	network["Nodes"] = slices.DeleteFunc(items(network["Nodes"]), func(node any) bool { return strings.HasPrefix(text(member(node, "ID")), "b-") })
	network["Lanes"] = slices.DeleteFunc(items(network["Lanes"]), func(lane any) bool { return strings.HasPrefix(text(member(lane, "ID")), "b-") })
	legacy := applyBankEdit(t, draft, `{"action":"stationLegacy","id":"station"}`)
	if legacy["version"] != float64(1) || has(items(member(legacy["network"], "Stations"))[0], "Banks") {
		t.Fatal("legacy metadata remains")
	}
	banks, err := json.Marshal(station["Banks"])
	if err != nil {
		t.Fatal(err)
	}
	banked := applyBankEdit(t, legacy, `{"action":"stationBanks","id":"station","value":`+string(banks)+`}`)
	if banked["version"] != float64(2) {
		t.Fatal("bank membership did not upgrade the project")
	}
	normalized, err := normalizeProject(banked)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := normalized.Patch["version"]; exists {
		t.Fatal("normalization downgraded the project")
	}
}

func TestEditorBankArrayLimits(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"project", "patch", "edit"} {
		for _, kind := range []string{"banks", "berths"} {
			t.Run(path+kind, func(t *testing.T) {
				t.Parallel()
				count := 9
				if kind == "berths" {
					count = project.MaxBerths + 1
				}
				entries := make([]any, count)
				for i := range entries {
					entries[i] = "berth"
				}
				banks := any(entries)
				if kind == "berths" {
					banks = []any{map[string]any{"BerthIDs": entries}}
				}
				value := map[string]any{path: map[string]any{"network": map[string]any{"Stations": []any{map[string]any{"Banks": banks}}}}}
				if path == "edit" {
					value = map[string]any{"edit": map[string]any{"field": "geometry", "value": map[string]any{"action": "stationBanks", "id": "station", "value": banks}}}
				}
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := scanRequest(raw); err == nil {
					t.Fatal("bank request array cap accepted excess elements")
				}
			})
		}
	}
}

func TestBankHistoryRestoresVersionAndTopology(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(2)
	model := new(engine)
	background := historyTarget(t, model, map[string]any{"scenario": draft, "background": nil})
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: background})
	initial := historyValue(t, model, background)
	changed := applyBankEdit(t, draft, `{"action":"deleteStation","id":"station"}`)
	background = historyTarget(t, model, map[string]any{"scenario": changed, "background": nil})
	acceptedHistory(t, model, historyCommand{Kind: "replace", Background: background})
	next := historyValue(t, model, background)
	before := cloneEditValue(next)
	pending := model.timeline.pending
	if _, err := model.edit(jsontext.Value(`{"field":"geometry","value":{"action":"bankLayout","id":"station","value":{"bank":"a","pitch":50}}}`)); err == nil {
		t.Fatal("stale bank action accepted")
	}
	if !reflect.DeepEqual(before, historyValue(t, model, background)) || model.timeline.pending != pending {
		t.Fatal("rejection changed project or history")
	}
	undo := acceptedHistory(t, model, historyCommand{Kind: "undo"})
	if !reflect.DeepEqual(historyValue(t, model, undo.Background), initial) {
		t.Fatal("undo did not restore bank topology and version")
	}
	redo := acceptedHistory(t, model, historyCommand{Kind: "redo"})
	if !reflect.DeepEqual(historyValue(t, model, redo.Background), next) {
		t.Fatal("redo did not restore the legacy version")
	}
}

func TestBankAnchorAndGeneratedLimits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, command string
		change        func(map[string]any)
	}{
		{"shared anchor", `{"action":"bankLayout","id":"station","value":{"bank":"a","approachLength":150}}`, func(draft map[string]any) {
			network := object(draft["network"])
			network["Nodes"] = append(items(network["Nodes"]), map[string]any{"ID": "extra-node", "Position": map[string]any{"X": float64(-300), "Y": float64(-100)}})
			network["Lanes"] = append(items(network["Lanes"]), map[string]any{"ID": "extra", "From": "a-approach", "To": "extra-node", "SpeedLimit": float64(14)})
		}},
		{"curved access", `{"action":"bankLayout","id":"station","value":{"bank":"a","approachLength":150}}`, func(draft map[string]any) {
			g := geometryDraft{network: object(draft["network"])}
			lane, _ := g.find("Lanes", "a-access-in")
			lane["Control"] = map[string]any{"X": float64(-110), "Y": float64(60)}
		}},
		{"curved anchor road", `{"action":"bankLayout","id":"station","value":{"bank":"a","departureLength":150}}`, func(draft map[string]any) {
			g := geometryDraft{network: object(draft["network"])}
			lane, _ := g.find("Lanes", "a-road-out")
			lane["Control"] = map[string]any{"X": float64(70), "Y": float64(-60)}
		}},
		{"station anchor", `{"action":"bankLayout","id":"station","value":{"bank":"a","approachLength":150}}`, func(draft map[string]any) {
			g := geometryDraft{network: object(draft["network"])}
			lane, _ := g.find("Lanes", "a-road-in")
			lane["StationID"], lane["StationRole"] = "other-station", "approach"
		}},
		{"zero ray", `{"action":"bankLayout","id":"station","value":{"bank":"a","approachLength":150}}`, func(draft map[string]any) {
			g := geometryDraft{network: object(draft["network"])}
			node, _ := g.find("Nodes", "a-approach")
			gate, _ := g.find("Nodes", "a-entry")
			node["Position"] = cloneEditValue(gate["Position"])
		}},
		{"short changed lane", `{"action":"moveNode","id":"a-arrival-0","point":{"X":-100,"Y":130}}`, func(map[string]any) {}},
		{"node growth cap", `{"action":"addBankBerth","id":"station","value":"a"}`, func(draft map[string]any) {
			network := object(draft["network"])
			for len(items(network["Nodes"])) < project.MaxNodes-2 {
				network["Nodes"] = append(items(network["Nodes"]), map[string]any{"ID": "filler-" + strconv.Itoa(len(items(network["Nodes"]))), "Position": map[string]any{"X": float64(9000), "Y": float64(9000)}})
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := bankEditorFixture(2)
			test.change(draft)
			before := cloneEditValue(draft)
			if _, err := editGeometry(draft, jsontext.Value(test.command)); err == nil {
				t.Fatal("invalid anchor or generated geometry accepted")
			}
			if !reflect.DeepEqual(draft, before) {
				t.Fatal("rejected bank edit changed the draft")
			}
		})
	}
	for _, raw := range []string{
		`{"action":"bankLayout","id":"station","value":{"bank":"a","pitch":25}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","spacing":48}}`,
		`{"action":"bankLayout","id":"station","value":{"bank":"a","approachLength":24,"departureLength":24}}`,
	} {
		applyBankEdit(t, bankEditorFixture(3), raw)
	}
}

func TestBankExampleEditorControls(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(sim.BankExample())
	if err != nil {
		t.Fatal(err)
	}
	var network map[string]any
	if decodeErr := json.Unmarshal(raw, &network); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	draft := map[string]any{"version": float64(2), "network": network, "fleet": []any{}}
	grown := applyBankEdit(t, draft, `{"action":"addBankBerth","id":"hub","value":"b"}`)
	changed := applyBankEdit(t, grown, `{"action":"bankLayout","id":"hub","value":{"bank":"b","pitch":80,"spacing":240,"approachLength":100,"departureLength":100}}`)
	g := geometryDraft{network: object(changed["network"])}
	station, bank, err := g.findBank("hub", "b")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := g.stationLayoutFor(bankStation(station, bank))
	if err != nil || !geometryNear(*layout.pitch, 80) || !geometryNear(layout.spacing, 240) {
		t.Fatal("example bank controls differ", layout, err)
	}
}

func TestBankStationRotationAndLastBerth(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(2)
	before := geometryDraft{network: object(draft["network"])}
	for _, prefix := range []string{"a", "b"} {
		for _, suffix := range []string{"in", "out"} {
			lane, err := before.find("Lanes", prefix+"-road-"+suffix)
			if err != nil {
				t.Fatal(err)
			}
			role := "approach"
			if suffix == "out" {
				role = "exit"
			}
			lane["StationID"], lane["StationRole"] = "station", role
		}
	}
	rotated := applyBankEdit(t, draft, `{"action":"stationBearing","id":"station","value":180}`)
	after := geometryDraft{network: object(rotated["network"])}
	for _, node := range items(before.network["Nodes"]) {
		id := text(member(node, "ID"))
		a, _ := before.point(id)
		b, _ := after.point(id)
		if !geometryNear(b.X, 120-a.Y) || !geometryNear(b.Y, 120+a.X) {
			t.Fatal("rotation omitted or repeated a bank node", id)
		}
	}
	one := applyBankEdit(t, bankEditorFixture(1), `{"action":"removeBerth","id":"station","value":"a-berth-0"}`)
	if _, err := editGeometry(one, jsontext.Value(`{"action":"removeBerth","id":"station","value":"b-berth-0"}`)); err == nil {
		t.Fatal("the station's last bank berth was removed")
	}
}

func TestBankEditorSynchronizationChecksCombinedVersion(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Version = 2
	config.Network = sim.BankExample()
	config.Fleet = []sim.Placement{{ID: "pod", StationID: "origin", BerthID: "origin-1"}}
	config.Demand.Enabled = false
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if decodeErr := json.Unmarshal(raw, &draft); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	model := new(engine)
	background := historyTarget(t, model, map[string]any{"scenario": draft, "background": nil})
	if model.err != nil {
		t.Fatal("bank branches did not synchronize together", model.err)
	}
	if _, validateErr := model.handle(`{"op":"validate"}`); validateErr != nil {
		t.Fatal("banked editor config failed validation", validateErr)
	}
	acceptedHistory(t, model, historyCommand{Kind: "reset", Background: background})
	keys := slices.Sorted(maps.Keys(draft))
	sync, err := json.Marshal(map[string]any{"op": "sync", "keys": keys, "patch": map[string]any{"version": float64(1)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, syncErr := model.handle(string(sync)); syncErr == nil {
		t.Fatal("version-only downgrade retained Banks")
	}
	if _, validateErr := model.handle(`{"op":"validate"}`); validateErr == nil {
		t.Fatal("typed operation ignored invalid bank version")
	}
	sync, err = json.Marshal(map[string]any{"op": "sync", "keys": keys, "patch": map[string]any{"version": float64(2)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := model.handle(string(sync)); err != nil {
		t.Fatal("version-only repair did not use cached network", err)
	}
	if _, err := model.handle(`{"op":"validate"}`); err != nil {
		t.Fatal("version repair failed validation", err)
	}
}

func TestBankMinimumSpacingInRotatedFrames(t *testing.T) {
	t.Parallel()
	for _, degrees := range []float64{0, 37, 90, 271} {
		t.Run(strconv.FormatFloat(degrees, 'f', -1, 64), func(t *testing.T) {
			t.Parallel()
			draft := bankEditorFixture(3)
			angle := degrees * math.Pi / 180
			for _, node := range items(member(draft["network"], "Nodes")) {
				position := object(member(node, "Position"))
				x, y := number(position["X"]), number(position["Y"])
				position["X"], position["Y"] = x*math.Cos(angle)-y*math.Sin(angle), x*math.Sin(angle)+y*math.Cos(angle)
			}
			applyBankEdit(t, draft, `{"action":"bankLayout","id":"station","value":{"bank":"a","spacing":48}}`)
		})
	}
}
