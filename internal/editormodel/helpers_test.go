package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"strings"
	"testing"
)

func helperResult(t *testing.T, input string) response {
	t.Helper()
	var result response
	if err := json.Unmarshal([]byte(Call(input)), &result, jsontext.AllowInvalidUTF8(true)); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBackgroundMetadataGuards(t *testing.T) {
	t.Parallel()
	license := `{"source":"","attribution":"","license":"","licenseURL":"https://example.com","copyrightURL":"","retrieved":"2026-99-99T99:99Z","method":"","notice":""}`
	valid := `{"asset":{"frameState":"none","license":` + license + `},"urlFacts":{"licenseURL":{"text":"https://example.com","https":true}}}`
	cases := []struct {
		name, metadata string
		valid          bool
	}{
		{"defaults", `{}`, true},
		{"placement", `{"placement":{"x":-1,"y":2,"width":3,"height":4,"opacity":0}}`, true},
		{"license", valid, true},

		{"attached defaults", `{"asset":{"frameState":"attached","frame":{"source":"equirectangular","south":51,"north":52,"west":-1,"east":0}}}`, true},
		{"detached frame", `{"asset":{"frameState":"detached","frame":{"source":"web-mercator","south":51,"north":52,"west":-1,"east":0}}}`, true},
		{"frame unknown", `{"asset":{"frameState":"attached","frame":{"source":"equirectangular","south":51,"north":52,"west":-1,"east":0,"extra":0}}}`, false},
		{"frame bounds", `{"asset":{"frameState":"attached","frame":{"source":"equirectangular","south":52,"north":51,"west":-1,"east":0}}}`, false},
		{"empty URL fact", strings.ReplaceAll(valid, "https://example.com", ""), true},
		{"asset null", `{"asset":null}`, false},
		{"metadata null", `null`, false},
		{"missing state", `{"asset":{}}`, false},
		{"state null", `{"asset":{"frameState":null}}`, false},
		{"attached missing frame", `{"asset":{"frameState":"attached"}}`, false},
		{"none with frame", `{"asset":{"frameState":"none","frame":{}}}`, false},
		{"unknown", `{"unknown":1}`, false},
		{"placement unknown", `{"placement":{"x":0,"y":0,"width":1,"height":1,"opacity":1,"extra":0}}`, false},
		{"placement missing", `{"placement":{"x":0,"y":0,"width":1,"height":1}}`, false},
		{"placement zero", `{"placement":{"x":0,"y":0,"width":0,"height":1,"opacity":1}}`, false},
		{"placement opacity", `{"placement":{"x":0,"y":0,"width":1,"height":1,"opacity":2}}`, false},
		{"license missing", `{"asset":{"frameState":"none","license":{}}}`, false},
		{"URL missing fact", `{"asset":{"frameState":"none","license":` + license + `}}`, false},
		{"URL text mismatch", strings.Replace(valid, `"text":"https://example.com"`, `"text":"https://other.com"`, 1), false},
		{"URL protocol", strings.Replace(valid, `"https":true`, `"https":false`, 1), false},
		{"URL bool", strings.Replace(valid, `"https":true`, `"https":1`, 1), false},
		{"URL extra", strings.Replace(valid, `"https":true`, `"https":true,"extra":0`, 1), false},
		{"null license facts", `{"urlFacts":{"licenseURL":{"text":"","https":false}}}`, false},
		{"URL unknown", `{"urlFacts":{"other":{"text":"","https":true}}}`, false},
		{"method boundary", strings.Replace(valid, `"method":""`, `"method":"`+strings.Repeat("😀", 10000)+`"`, 1), true},
		{"method too long", strings.Replace(valid, `"method":""`, `"method":"`+strings.Repeat("😀", 10001)+`"`, 1), false},
		{"retrieved format", strings.Replace(valid, `2026-99-99T99:99Z`, `yesterday`, 1), false},
	}
	for _, run := range cases {
		t.Run(run.name, func(t *testing.T) {
			t.Parallel()
			result := helperResult(t, `{"op":"backgroundMetadata","metadata":`+run.metadata+`}`)
			if (result.Error == "") != run.valid {
				t.Fatalf("result: %+v", result)
			}
		})
	}
	output := Call(`{"op":"backgroundMetadata","metadata":` + strings.Replace(valid, `"attribution":""`, `"attribution":"\ud800"`, 1) + `}`)
	if !strings.Contains(output, `"attribution":"\ud800"`) || strings.Contains(output, `urlFacts`) {
		t.Fatalf("metadata lost original code units or facts leaked: %s", output)
	}
	// HTTPS is a browser fact, not a second URL parser.
	permissive := strings.ReplaceAll(valid, "https://example.com", " browser-accepted text ")
	if result := helperResult(t, `{"op":"backgroundMetadata","metadata":`+permissive+`}`); result.Error != "" {
		t.Fatal(result.Error)
	}
}

func TestImportCompatibilityPreservesRawDraft(t *testing.T) {
	t.Parallel()
	input := `{"network":{"Stations":[{"ID":"origin","Berths":[{"ID":"berth"}]},{"ID":"last"},{"ID":"market","ParkingOnly":true}]},"fleet":[{"StationID":"origin","BerthID":false},{"StationID":"unknown","BerthID":0},{"StationID":"origin","BerthID":"kept"},null],"demand":{"pattern":"market","destination":null,"rate":"invalid"},"version":99,"unknown":true}`
	result := helperResult(t, `{"op":"importCompatibility","project":`+input+`}`)
	if result.Error != "" {
		t.Fatal(result.Error)
	}
	patch := result.Change.Patch
	if len(patch) != 2 || member(patch["demand"], "destination") != "last" || member(patch["demand"], "rate") != "invalid" {
		t.Fatalf("patch: %#v", patch)
	}
	pods := items(patch["fleet"])
	if member(pods[0], "BerthID") != "berth" || member(pods[1], "BerthID") != "" || member(pods[2], "BerthID") != "kept" || pods[3] != nil {
		t.Fatalf("fleet: %#v", pods)
	}
	for _, draft := range []string{`{}`, `{"fleet":null}`, `{"fleet":[[]]}`, `{"fleet":[{"BerthID":true}],"demand":{"pattern":"destination"}}`} {
		if r := helperResult(t, `{"op":"importCompatibility","project":`+draft+`}`); r.Error != "" || len(r.Change.Patch) != 0 {
			t.Fatalf("unexpected defaults: %+v", r)
		}
	}
	noStations := helperResult(t, `{"op":"importCompatibility","project":{"demand":{"pattern":"market","destination":false}}}`)
	if member(noStations.Change.Patch["demand"], "destination") != false {
		t.Fatal("repair changed destination without station array")
	}
	var draft any
	if err := json.Unmarshal([]byte(input), &draft); err != nil {
		t.Fatal(err)
	}
	before := cloneEditValue(draft)
	change, err := importCompatibility(draft)
	if err != nil {
		t.Fatal(err)
	}
	object(items(change.Patch["fleet"])[0])["StationID"] = "changed"
	if !reflect.DeepEqual(draft, before) {
		t.Fatal("returned compatibility branches alias draft")
	}
}

func TestStationLayoutToleratesIncompleteDraft(t *testing.T) {
	t.Parallel()
	draft := bankEditorFixture(2)
	encoded, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	input := `{"op":"stationLayout","project":` + string(encoded) + `,"layout":{"stationID":"station","bankID":"a"}}`
	r := helperResult(t, input)
	if r.Error != "" || r.Layout.Pitch.Value == nil || *r.Layout.Pitch.Value != 75 || r.Layout.ApproachLength.Value == nil || r.Layout.DepartureLength.Value == nil {
		t.Fatalf("bank layout: %+v", r)
	}
	whole := helperResult(t, strings.Replace(input, `,"bankID":"a"`, "", 1))
	if whole.Error != "" || whole.Layout.ApproachLength.Value != nil || whole.Layout.ApproachLength.Reason == "" {
		t.Fatalf("whole layout: %+v", whole)
	}
	for _, layout := range []string{`{"stationID":"missing"}`, `{"stationID":"station","bankID":"missing"}`, `{"stationID":"station","bankID":null}`, `{"stationID":"station","extra":1}`} {
		if r := helperResult(t, `{"op":"stationLayout","project":`+string(encoded)+`,"layout":`+layout+`}`); r.Error == "" {
			t.Fatalf("accepted %s", layout)
		}
	}
	incomplete := helperResult(t, `{"op":"stationLayout","project":{"network":{"Stations":[{"ID":"station"}]}},"layout":{"stationID":"station"}}`)
	if incomplete.Error != "" || incomplete.Layout.Spacing.Value != nil || incomplete.Layout.Spacing.Reason == "" {
		t.Fatalf("incomplete: %+v", incomplete)
	}
}

func TestHelpersDoNotMutateEngine(t *testing.T) {
	t.Parallel()
	inputs := []string{
		`{"op":"backgroundMetadata","metadata":{}}`,
		`{"op":"backgroundMetadata","metadata":{"asset":null}}`,
		`{"op":"importCompatibility","project":{"version":99,"fleet":[{}]}}`,
		`{"op":"stationLayout","project":{"network":{"Stations":[{"ID":"station"}]}},"layout":{"stationID":"station"}}`,
		`{"op":"stationLayout","project":{},"layout":{"stationID":"missing"}}`,
	}
	e := new(engine)
	for index, input := range inputs {
		if _, err := e.handle(input); (err != nil) != (index == 1 || index == 4) {
			t.Fatalf("helper verdict with synchronized=%v: %v", e.ready, err)
		}
		if e.ready || e.branches != nil || e.timeline != nil || e.err != nil || e.checks != nil || e.profiles != nil || e.size != 0 {
			t.Fatal("helper initialized engine")
		}
	}
	if _, err := e.handle(`{"op":"sync","keys":["version","fleet"],"patch":{"version":99,"fleet":"invalid"}}`); err == nil {
		t.Fatal("malformed sync accepted")
	}
	before := *e
	beforeError := reflect.ValueOf(e.err).Pointer()
	beforeBranches := reflect.ValueOf(e.branches).Pointer()
	for index, input := range inputs {
		if _, err := e.handle(input); (err != nil) != (index == 1 || index == 4) {
			t.Fatalf("helper verdict with synchronized=%v: %v", e.ready, err)
		}
		if !reflect.DeepEqual(e.config, before.config) || !reflect.DeepEqual(e.profiles, before.profiles) || e.ready != before.ready || e.size != before.size || e.checks != before.checks || e.timeline != before.timeline || reflect.ValueOf(e.err).Pointer() != beforeError || reflect.ValueOf(e.branches).Pointer() != beforeBranches {
			t.Fatal("helper mutated synchronized engine")
		}
	}
	for _, input := range []string{
		`{"op":"backgroundMetadata","metadata":{},"project":null}`,
		`{"op":"backgroundMetadata","metadata":{},"keys":null}`,
		`{"op":"importCompatibility","project":{},"layout":null}`,
		`{"op":"stationLayout","project":{},"layout":{},"patch":null}`,
		`{"op":"validate","project":{},"metadata":{}}`,
	} {
		if r := helperResult(t, input); r.Error == "" {
			t.Fatalf("unrelated parameters accepted: %s", input)
		}
	}
}

func TestHelpersPreservePendingHistory(t *testing.T) {
	t.Parallel()
	e := new(engine)
	background := historyTarget(t, e, map[string]any{"scenario": map[string]any{"version": float64(1), "name": "draft"}, "background": nil})
	acceptedHistory(t, e, historyCommand{Kind: "reset", Background: background})
	prepared := historyRequest(t, e, historyCommand{Action: "prepare", Kind: "replace", Revision: "1", Background: background})
	timeline := e.timeline
	before, err := json.Marshal(historyMetadata(e.timeline.state, e.timeline.revision, false))
	if err != nil {
		t.Fatal(err)
	}
	revision, pending := timeline.revision, timeline.pending
	for _, input := range []string{`{"op":"backgroundMetadata","metadata":{}}`, `{"op":"backgroundMetadata","metadata":{"asset":null}}`, `{"op":"importCompatibility","project":{}}`, `{"op":"stationLayout","project":{},"layout":{"stationID":"missing"}}`} {
		_, _ = e.handle(input)
	}
	after, err := json.Marshal(historyMetadata(e.timeline.state, e.timeline.revision, false))
	if err != nil {
		t.Fatal(err)
	}
	if e.timeline != timeline || timeline.revision != revision || timeline.pending != pending || !bytes.Equal(before, after) {
		t.Fatal("helper changed pending history")
	}
	if historyRequest(t, e, historyCommand{Action: "accept", Token: prepared.Proposal}).Revision != "2" {
		t.Fatal("helper invalidated pending acceptance")
	}
}

func TestMetadataBoundsAndSurrogateURLFacts(t *testing.T) {
	t.Parallel()
	raw := `{"source":"","attribution":"","license":"","licenseURL":"https://example.com/\ud800","copyrightURL":"","retrieved":"","method":"","notice":""}`
	input := `{"op":"backgroundMetadata","metadata":{"asset":{"frameState":"none","license":` + raw + `},"urlFacts":{"licenseURL":{"text":"https://example.com/\ud800","https":true}}}}`
	if r := helperResult(t, input); r.Error != "" {
		t.Fatal(r.Error)
	}
	if r := helperResult(t, strings.Replace(input, `"text":"https://example.com/\ud800"`, `"text":"https://example.com/\ud801"`, 1)); r.Error == "" {
		t.Fatal("different lone surrogate URL fact accepted")
	}
	if r := helperResult(t, `{"op":"backgroundMetadata","metadata":{`+strings.Repeat(" ", metadataBytes)+`}}`); r.Error == "" {
		t.Fatal("oversized metadata accepted")
	}
	if r := helperResult(t, `{"op":"importCompatibility","project":{`+strings.Repeat(" ", MaxRequestBytes-metadataBytes)+`}}`); r.Error == "" {
		t.Fatal("oversized raw helper project accepted")
	}
	if r := helperResult(t, `{"op":"backgroundMetadata","metadata":{},"extra":"`+strings.Repeat("x", MaxRequestBytes)+`"}`); r.Error == "" {
		t.Fatal("oversized request accepted")
	}
}

func TestHelperWireShapes(t *testing.T) {
	t.Parallel()
	for _, run := range []struct {
		input string
		keys  []string
	}{
		{`{"op":"backgroundMetadata","metadata":{}}`, []string{"valid", "metadata"}},
		{`{"op":"importCompatibility","project":{}}`, []string{"change"}},
		{`{"op":"stationLayout","project":{"network":{"Stations":[{"ID":"station"}]}},"layout":{"stationID":"station"}}`, []string{"layout"}},
		{`{"op":"backgroundMetadata","metadata":null}`, []string{"error"}},
		{`{"op":"importCompatibility","project":null}`, []string{"error"}},
		{`{"op":"stationLayout","project":{},"layout":null}`, []string{"error"}},
		{`{"op":"backgroundMetadata","metadata":{},"keys":null}`, []string{"error"}},
		{`{"op":"backgroundMetadata","metadata":{},"extra":null}`, []string{"error"}},
	} {
		var result map[string]any
		if err := json.Unmarshal([]byte(Call(run.input)), &result); err != nil {
			t.Fatal(err)
		}
		if len(result) != len(run.keys) {
			t.Fatalf("unexpected wire fields: %#v", result)
		}
		for _, key := range run.keys {
			if _, exists := result[key]; !exists {
				t.Fatalf("missing %s: %#v", key, result)
			}
		}
	}
}
