package editormodel

import (
	"encoding/json/v2"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func measurementDraft() map[string]any { return map[string]any{"network": observationNetwork()} }

func measurementsOf(t *testing.T, draft any, measure string) measurementReport {
	t.Helper()
	var result struct {
		Measurements *measurementReport `json:"measurements"`
		Error        string             `json:"error"`
	}
	encoded := Call(`{"op":"measurements","project":` + string(mustJSON(t, draft)) + `,"measure":` + measure + `}`)
	if err := json.Unmarshal([]byte(encoded), &result); err != nil || result.Measurements == nil {
		t.Fatalf("measurements %s: %s, %v", measure, encoded, err)
	}
	return *result.Measurements
}

func TestMeasurementsReuseLaneLength(t *testing.T) {
	t.Parallel()
	draft := measurementDraft()
	report := measurementsOf(t, draft, `{"selection":{"type":"station","id":"s"},"scale":1}`)
	kinds := []string{}
	for _, item := range report.Items {
		kinds = append(kinds, item.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"lane", "lane", "spacing", "spacing"}) {
		t.Fatalf("kinds %v", kinds)
	}
	curve := envelopeNetworkLane(draft, 1)
	position := func(i int) any { return at(draft, "network", "nodes", i, "position") }
	if report.Items[0].Meters != 30 || report.Items[1].Meters != draftLaneLength(position(1), position(2), curve["control"]) || report.Items[1].Meters <= 40 {
		t.Errorf("lane meters %v %v", report.Items[0].Meters, report.Items[1].Meters)
	}
	if report.Items[1].Method != "curve approximation, 16 segments" || report.Items[2].Method != "straight center chord, authored berth order" ||
		report.Items[2].Meters != 40 || report.Items[3].Meters != 30 || report.Items[2].Name != "Berths b1 / b2" {
		t.Errorf("items %+v", report.Items)
	}
	lane := measurementsOf(t, draft, `{"selection":{"type":"lane","id":"road"},"scale":1}`)
	if len(lane.Items) != 1 || lane.Items[0].Meters != math.Hypot(60, 40) || lane.Items[0].Control != nil {
		t.Errorf("road %+v", lane.Items)
	}
	if !strings.HasPrefix(report.Summary, measurementScope+" Lane straight: 30.00 m (straight centerline)") {
		t.Errorf("summary %q", report.Summary)
	}
}

func envelopeNetworkLane(draft map[string]any, i int) map[string]any {
	return at(draft, "network", "lanes", i)
}

func TestMeasurementsUnavailableGeometry(t *testing.T) {
	t.Parallel()
	lane := `{"selection":{"type":"lane","id":"straight"},"scale":1}`
	station := `{"selection":{"type":"station","id":"s"},"scale":1}`
	for name, value := range map[string]any{"null": nil, "text": "30", "too far": float64(measurementCoordinate + 1), "too far negative": -float64(measurementCoordinate + 1)} {
		draft := measurementDraft()
		at(draft, "network", "nodes", 0, "position")["x"] = value
		report := measurementsOf(t, draft, lane)
		if len(report.Items) != 0 || !strings.Contains(report.Summary, "Lane straight: invalid or missing geometry.") {
			t.Errorf("position %s: %+v", name, report)
		}
	}
	for name, control := range map[string]any{"empty": map[string]any{}, "null y": map[string]any{"x": 0.0, "y": nil}, "too far": map[string]any{"x": 100001.0, "y": 0.0}} {
		draft := measurementDraft()
		envelopeNetworkLane(draft, 1)["control"] = control
		if report := measurementsOf(t, draft, `{"selection":{"type":"lane","id":"curve"},"scale":1}`); len(report.Items) != 0 {
			t.Errorf("control %s: %+v", name, report.Items)
		}
	}
	straight := measurementDraft()
	envelopeNetworkLane(straight, 1)["control"] = nil
	if report := measurementsOf(t, straight, `{"selection":{"type":"lane","id":"curve"},"scale":1}`); len(report.Items) != 1 || report.Items[0].Meters != 40 {
		t.Errorf("null control: %+v", report.Items)
	}
	duplicate := measurementDraft()
	network := at(duplicate, "network")
	network["nodes"] = append(items(network["nodes"]), items(network["nodes"])[0])
	if report := measurementsOf(t, duplicate, station); len(report.Items) != 0 || !strings.HasSuffix(report.Summary, "Draft geometry unavailable.") {
		t.Errorf("repeated node: %+v", report)
	}
	missing := measurementDraft()
	at(missing, "network", "stations", 0, "berths", 1)["node"] = "missing"
	report := measurementsOf(t, missing, station)
	if spacings := strings.Count(report.Summary, "center chord"); spacings != 0 ||
		!strings.HasSuffix(report.Summary, "Berth pair 1/2: center spacing unavailable. Berth pair 2/3: center spacing unavailable.") {
		t.Errorf("missing berth node: %q", report.Summary)
	}
	noBerths := measurementDraft()
	at(noBerths, "network", "stations", 0)["berths"] = nil
	if report := measurementsOf(t, noBerths, station); len(report.Items) != 2 || !strings.HasSuffix(report.Summary, "Berth center spacing unavailable.") {
		t.Errorf("berths null: %+v", report)
	}
	for _, selection := range []string{`{"type":"node","id":"a"}`, `null`, `{"type":"lane","id":"missing"}`, `{"type":"lane"}`} {
		if report := measurementsOf(t, measurementDraft(), `{"selection":`+selection+`,"scale":1}`); len(report.Items) != 0 || !strings.HasSuffix(report.Summary, "Select a station or guideway to measure.") {
			t.Errorf("selection %s: %+v", selection, report)
		}
	}
	if report := measurementsOf(t, map[string]any{}, station); len(report.Items) != 0 || !strings.HasSuffix(report.Summary, "Draft geometry unavailable.") {
		t.Errorf("no network: %+v", report)
	}
	for _, measure := range []string{`[]`, `{"selection":null,"scale":1,"extra":0}`} {
		if encoded := Call(`{"op":"measurements","project":{},"measure":` + measure + `}`); !strings.Contains(encoded, `"error"`) {
			t.Errorf("measure %s: %s", measure, encoded)
		}
	}
}

func TestMeasurementsMapGeometry(t *testing.T) {
	t.Parallel()
	draft := measurementDraft()
	for _, test := range []struct {
		scale     string
		fontSize  float64
		radius    float64
		labelLift float64
	}{
		{"2", 5.5, 1.5, 4},
		{"0.001", 11000, 3000, 8000},
		{"5", 11.0 / 5, 3.0 / 5, 8.0 / 5},
		{"5.01", 11, 3, 8},
		{"null", 11, 3, 8},
		{"-1", 11, 3, 8},
		{"0", 11, 3, 8},
		{"1e-7", 11, 3, 8},
		{`"2"`, 11, 3, 8},
	} {
		report := measurementsOf(t, draft, `{"selection":{"type":"station","id":"s"},"scale":`+test.scale+`}`)
		straight, curve := report.Items[0], report.Items[1]
		if report.FontSize != test.fontSize || report.Radius != test.radius || straight.Label != (measurementPoint{15, 0 - test.labelLift}) ||
			curve.Label != (measurementPoint{(30 + 2*60 + 30) / 4.0, (0+2*0+40)/4.0 - test.labelLift}) {
			t.Errorf("scale %s: font %v radius %v labels %+v %+v", test.scale, report.FontSize, report.Radius, straight.Label, curve.Label)
		}
	}
	many := measurementDraft()
	network := at(many, "network")
	for i := range 210 {
		id := "extra" + strconv.Itoa(i)
		network["lanes"] = append(items(network["lanes"]), map[string]any{"id": id, "from": "a", "to": "b", "stationID": "s"})
	}
	report := measurementsOf(t, many, `{"selection":{"type":"station","id":"s"},"scale":1}`)
	if len(report.Items) != measurementItems || !strings.Contains(report.Summary, "Only the first 200 of 214 measurements are shown.") || strings.Contains(report.Summary, "Lane extra198:") {
		t.Errorf("limit: %d items, summary end %q", len(report.Items), report.Summary[len(report.Summary)-120:])
	}
}

func TestMeasurementMeterText(t *testing.T) {
	t.Parallel()
	// The browser rounds a value halfway between two results up.
	for value, want := range map[float64]string{0.125: "0.13", 0.375: "0.38", 2.625: "2.63", 0.875: "0.88", 1.005: "1.00", 30: "30.00", 0.0049: "0.00", 72.11102550927978: "72.11"} {
		if got := fixed2(value); got != want {
			t.Errorf("fixed2(%v) = %s, want %s", value, got, want)
		}
	}
}
