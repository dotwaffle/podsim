package editormodel

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/session"
)

func packed(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }

func wireRequest(fields map[string]any) map[string]any {
	value := map[string]any{"sharingConsent": "shared", "service": "on-demand", "id": 1.0, "from": "s", "to": "t", "partySize": 1.0,
		"podID": "", "completed": false, "requestedTick": 0.0, "dispatchReason": ""}
	maps.Copy(value, fields)
	for _, key := range []string{"from", "to", "podID", "dispatchReason", "serviceID", "legFrom"} {
		if text, ok := value[key].(string); ok {
			value[key] = packed(text)
		}
	}
	return value
}

func observationNetwork() map[string]any {
	return decodeTestJSON(`{
		"nodes": [{"id": "a", "position": {"x": 0, "y": 0}}, {"id": "b", "position": {"x": 30, "y": 0}},
			{"id": "c", "position": {"x": 30, "y": 40}}, {"id": "d", "position": {"x": 60, "y": 40}}],
		"lanes": [{"id": "straight", "from": "a", "to": "b", "stationID": "s"},
			{"id": "curve", "from": "b", "to": "c", "stationID": "s", "control": {"x": 60, "y": 0}},
			{"id": "road", "from": "a", "to": "d"}],
		"stations": [{"id": "s", "name": "Station S", "berths": [{"id": "b1", "node": "b"}, {"id": "b2", "node": "c"}, {"id": "b3", "node": "d"}]},
			{"id": "t", "name": "Station T", "berths": [{"id": "bt", "node": "a"}]}]}`)
}

func decodeTestJSON(text string) map[string]any {
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		panic(err)
	}
	return value
}

func observationPodValue(id string, fields map[string]any) map[string]any {
	pod := map[string]any{"id": id, "stationID": "s", "berthID": "", "laneID": "", "activity": "Ready", "stationPhase": "At berth", "waitReason": "", "blockedBy": ""}
	maps.Copy(pod, fields)
	return map[string]any{"pod": pod}
}

// observationEnvelope is the HTTP state of the browser tests: station s
// has three berths and three associated pods, and station t has one.
func observationEnvelope() map[string]any {
	identity := func() map[string]any {
		return map[string]any{"epoch": "epoch-1", "serverStart": "server-1", "projectRevision": 7.0}
	}
	topology := identity()
	topology["network"] = observationNetwork()
	state := identity()
	state["revision"], state["generation"] = 120.0, 2.0
	state["simulation"] = map[string]any{"paused": false, "tick": 100.0,
		"berths": []any{decodeTestJSON(`{"id": "b1", "occupant": "p1", "reservedBy": "p1"}`), decodeTestJSON(`{"id": "b2", "occupant": "", "reservedBy": "p2"}`),
			decodeTestJSON(`{"id": "b3", "occupant": "", "reservedBy": ""}`), decodeTestJSON(`{"id": "bt", "occupant": "p4", "reservedBy": ""}`)},
		"vehicles": []any{observationPodValue("p1", map[string]any{"berthID": "b1"}),
			observationPodValue("p2", map[string]any{"stationID": "", "maneuverStationID": "s", "laneID": "straight", "activity": "Boarding", "waitReason": "Junction traffic", "blockedBy": "p3"}),
			observationPodValue("p3", map[string]any{"laneID": "curve", "activity": "Traveling", "stationPhase": "Departing berth", "waitReason": "Pod ahead", "blockedBy": "p1"}),
			observationPodValue("p4", map[string]any{"stationID": "t", "berthID": "bt"})},
		"pending": []any{wireRequest(map[string]any{"id": 1.0, "from": "s", "dispatchReason": "No available pod"}),
			wireRequest(map[string]any{"id": 2.0, "from": "t", "legFrom": "s"}), wireRequest(map[string]any{"id": 3.0, "from": "t", "dispatchReason": "Assigned"})},
	}
	return map[string]any{"topology": topology, "frame": map[string]any{"state": state}}
}

// at gives the object at a path of member names and list indexes in a
// decoded JSON value, or nil.
func at(value any, path ...any) map[string]any {
	for _, step := range path {
		if index, ok := step.(int); ok {
			value = items(value)[index]
		} else {
			key, _ := step.(string)
			value = member(value, key)
		}
	}
	return object(value)
}

func envelopeSimulation(reply map[string]any) map[string]any {
	return at(reply, "frame", "state", "simulation")
}

func envelopeNetwork(reply map[string]any) map[string]any {
	return at(reply, "topology", "network")
}

func observe(t *testing.T, reply any) observationReport {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	report, err := decodeStationObservations(raw)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func intValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func TestUnpackOrderText(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		packed   any
		freeText bool
		want     string
	}{
		{packed("station-1.+"), false, "station-1.+"},
		{"", false, ""},
		{packed(strings.Repeat("a", 64)), false, strings.Repeat("a", 64)},
		{packed(strings.Repeat("é", 512)), true, strings.Repeat("é", 512)},
		// A leading byte order mark stays text.
		{packed("\ufeffÀ quai 東京  "), true, "\ufeffÀ quai 東京  "},
	} {
		got, err := unpackOrderText(test.packed, test.freeText)
		if err != nil || got != test.want {
			t.Errorf("unpackOrderText(%q) = %q, %v; want %q", test.packed, got, err, test.want)
		}
	}
	for _, test := range []struct {
		name     string
		packed   any
		freeText bool
		want     string
	}{
		{"null", nil, false, "canonical"},
		{"number", 0.0, false, "canonical"},
		{"no padding", "cw", false, "canonical"},
		{"short padding", "cw=", false, "canonical"},
		{"space", "c w==", false, "canonical"},
		{"line break", "cw==\n", false, "canonical"},
		{"line break inside", "c\nw=", false, "canonical"},
		// The base64 decoder skips line breaks, so the byte check refuses them.
		{"line breaks", "YW\nJj\r\n\n", false, "canonical"},
		{"padding bits two", "cx==", false, "canonical"},
		{"padding bits one", "c3N=", false, "canonical"},
		{"URL alphabet", "____", false, "canonical"},
		{"only padding", "====", false, "canonical"},
		{"three padding", "c===", false, "canonical"},
		{"long padding", "cw===", false, "canonical"},
		{"long ID", packed(strings.Repeat("a", 65)), false, "canonical"},
		{"long text", packed(strings.Repeat("é", 513)), true, "canonical"},
		{"byte FF", base64.StdEncoding.EncodeToString([]byte{255}), true, "UTF-8"},
		{"overlong", base64.StdEncoding.EncodeToString([]byte{0xc0, 0xaf}), true, "UTF-8"},
		{"surrogate", base64.StdEncoding.EncodeToString([]byte{0xed, 0xa0, 0x80}), true, "UTF-8"},
		{"truncated", base64.StdEncoding.EncodeToString([]byte{0xe2, 0x82}), true, "UTF-8"},
		{"slash", packed("a/b"), false, "ID characters"},
		{"ID space", packed("a b"), false, "ID characters"},
		{"ID letter", packed("À"), false, "ID characters"},
		{"ID mark", packed("\ufeffstation"), false, "ID characters"},
		{"ID NUL", packed("a\u0000"), false, "ID characters"},
		{"ID C1", packed("a\u0080"), false, "ID characters"},
		{"NUL", packed("Reason\u0000"), true, "control character"},
		{"newline", packed("Reason\n"), true, "control character"},
		{"unit separator", packed("Reason\u001f"), true, "control character"},
		{"delete", packed("Reason\u007f"), true, "control character"},
		{"C1 first", packed("Reason\u0080"), true, "control character"},
		{"C1 last", packed("Reason\u009f"), true, "control character"},
	} {
		if _, err := unpackOrderText(test.packed, test.freeText); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %v, want %q", test.name, err, test.want)
		}
	}
}

func TestUnpackPendingRequest(t *testing.T) {
	t.Parallel()
	if decoded, err := unpackPendingRequest(wireRequest(nil)); err != nil || decoded != (pendingRequest{id: 1, from: "s", to: "t"}) {
		t.Fatalf("defaults = %+v, %v", decoded, err)
	}
	for _, key := range requiredOrderFields {
		request := wireRequest(nil)
		delete(request, key)
		if _, err := unpackPendingRequest(request); err == nil {
			t.Errorf("missing %s accepted", key)
		}
	}
	for _, key := range []string{"serviceID", "legFrom", "boardedTick"} {
		for _, value := range []any{nil, map[string]any{}, -1.0} {
			if _, err := unpackPendingRequest(wireRequest(map[string]any{key: value})); err == nil {
				t.Errorf("%s %v accepted", key, value)
			}
		}
	}
	for _, key := range []string{"From", "dispatchreason", "unexpected"} {
		request := wireRequest(nil)
		request[key] = ""
		if _, err := unpackPendingRequest(request); err == nil || !strings.Contains(err.Error(), "unknown") {
			t.Errorf("member %s: error %v", key, err)
		}
	}
	for _, test := range []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"empty origin", map[string]any{"from": ""}, "origin"},
		{"completed", map[string]any{"completed": true}, "invalid pending order data"},
		{"zero ID", map[string]any{"id": 0.0}, "invalid pending order data"},
		{"fractional ID", map[string]any{"id": 1.5}, "invalid pending order data"},
		{"unsafe ID", map[string]any{"id": math.Pow(2, 53)}, "invalid pending order data"},
		{"zero party", map[string]any{"partySize": 0.0}, "invalid pending order data"},
		{"consent", map[string]any{"sharingConsent": "any"}, "invalid pending order data"},
		{"service", map[string]any{"service": "taxi"}, "invalid pending order data"},
		{"tick", map[string]any{"requestedTick": -1.0}, "invalid pending order data"},
	} {
		if _, err := unpackPendingRequest(wireRequest(test.fields)); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %v, want %q", test.name, err, test.want)
		}
	}
	all, err := unpackPendingRequest(wireRequest(map[string]any{"podID": "p1", "service": "express", "serviceID": "service-1", "legFrom": "s", "dispatchReason": "\ufeffÀ quai 東京", "boardedTick": 4.0}))
	if err != nil || all != (pendingRequest{id: 1, from: "s", to: "t", podID: "p1", serviceID: "service-1", legFrom: "s", dispatchReason: "\ufeffÀ quai 東京"}) {
		t.Fatalf("all text = %+v, %v", all, err)
	}
}

// TestStationObservationsHTTPFixtureParity reads the HTTP state that
// TestStationObservationsHTTPFixture of the session package writes with
// EncodeStateJSON.
func TestStationObservationsHTTPFixtureParity(t *testing.T) {
	t.Parallel()
	raw, readErr := os.ReadFile(filepath.Join("..", "..", "web", "testdata", "station-observations.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	var fixture struct {
		Status string `json:"status"`
		Cases  []struct {
			Name     string         `json:"name"`
			Envelope jsontext.Value `json:"envelope"`
			Expected []struct {
				ID                    string              `json:"id"`
				PendingOriginRequests int                 `json:"pendingOriginRequests"`
				DispatchReasons       []observationReason `json:"dispatchReasons"`
			} `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Status != "GENERATED BY EncodeStateJSON" || len(fixture.Cases) != 2 || fixture.Cases[0].Name != "queued" || fixture.Cases[1].Name != "empty-queue" {
		t.Fatalf("fixture status %q, %d cases", fixture.Status, len(fixture.Cases))
	}
	for _, test := range fixture.Cases {
		report, err := decodeStationObservations(test.Envelope)
		if err != nil || report.Status != "snapshot" {
			t.Fatalf("%s: %+v, %v", test.Name, report, err)
		}
		for _, want := range test.Expected {
			index := slicesIndex(report.Stations, want.ID)
			if index < 0 {
				t.Fatalf("%s: station %s missing", test.Name, want.ID)
			}
			station := report.Stations[index]
			if intValue(station.PendingOriginRequests) != want.PendingOriginRequests || !reflect.DeepEqual(station.DispatchReasons, want.DispatchReasons) {
				t.Errorf("%s %s: pending %v %v, want %d %v", test.Name, want.ID, intValue(station.PendingOriginRequests), station.DispatchReasons, want.PendingOriginRequests, want.DispatchReasons)
			}
		}
	}
	var queued struct {
		Frame struct {
			State struct {
				Simulation struct {
					Pending []map[string]any `json:"pending"`
				} `json:"simulation"`
			} `json:"state"`
		} `json:"frame"`
	}
	if err := json.Unmarshal(fixture.Cases[0].Envelope, &queued); err != nil {
		t.Fatal(err)
	}
	pending := queued.Frame.State.Simulation.Pending
	for _, key := range []string{"serviceID", "legFrom", "boardedTick"} {
		if _, present := pending[0][key]; present {
			t.Errorf("queued request has omitted member %s", key)
		}
	}
	for _, request := range pending {
		for _, key := range []string{"from", "to", "podID", "dispatchReason", "serviceID", "legFrom"} {
			if value, present := request[key]; present {
				text, err := unpackOrderText(value, key == "dispatchReason")
				if err != nil || packed(text) != value {
					t.Errorf("%s %v: unpacked %q, %v", key, value, text, err)
				}
			}
		}
	}
	if pending[0]["dispatchReason"] != "" {
		t.Errorf("queued dispatch reason %v", pending[0]["dispatchReason"])
	}
	transfer, err := unpackPendingRequest(pending[1])
	if err != nil || transfer.legFrom != "garden" || transfer.dispatchReason != "\ufeffÀ quai 東京" {
		t.Fatalf("transfer = %+v, %v", transfer, err)
	}
}

func slicesIndex(stations []observationStation, id string) int {
	for i, station := range stations {
		if station.ID == id {
			return i
		}
	}
	return -1
}

func TestStationObservationsMalformedPendingUnavailable(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(map[string]any){
		"plain origin":      func(r map[string]any) { r["from"] = "s" },
		"padding bits":      func(r map[string]any) { r["to"] = "cx==" },
		"pod ID characters": func(r map[string]any) { r["podID"] = packed("invalid/pod") },
		"reason control":    func(r map[string]any) { r["dispatchReason"] = packed("reason\n") },
		"service ID":        func(r map[string]any) { r["serviceID"] = packed("invalid service") },
		"leg origin":        func(r map[string]any) { r["legFrom"] = "cx==" },
		"extra member":      func(r map[string]any) { r["extra"] = "" },
		"missing reason":    func(r map[string]any) { delete(r, "dispatchReason") },
		"unknown pod":       func(r map[string]any) { r["podID"] = packed("missing") },
		"unknown leg":       func(r map[string]any) { r["legFrom"] = packed("missing") },
	} {
		reply := observationEnvelope()
		mutate(at(envelopeSimulation(reply), "pending", 0))
		station := observe(t, reply).Stations[0]
		if station.PendingOriginRequests != nil || station.DispatchReasons != nil {
			t.Errorf("%s: pending %v %v", name, intValue(station.PendingOriginRequests), station.DispatchReasons)
		}
	}
}

func TestStationObservationsCounts(t *testing.T) {
	t.Parallel()
	report := observe(t, observationEnvelope())
	if report.Status != "snapshot" || *report.Revision != 120 || *report.Tick != 100 || *report.Identity != (observationIdentity{"epoch-1", "server-1", 7}) {
		t.Fatalf("report = %+v", report)
	}
	station := report.Stations[0]
	if *station.Occupancy != (observationOccupancy{Occupied: 1, ReservedEmpty: 1, Free: 1, Capacity: 3}) {
		t.Errorf("occupancy %+v", *station.Occupancy)
	}
	counts := []any{intValue(station.AssociatedPods), intValue(station.IdlePods), intValue(station.BoardingPods), intValue(station.DepartingPods), intValue(station.BlockedPods), intValue(station.PendingOriginRequests)}
	if !reflect.DeepEqual(counts, []any{3, 1, 1, 1, 2, 2}) {
		t.Errorf("counts %v", counts)
	}
	if want := []observationReason{{"Junction traffic", 1}, {"Pod ahead", 1}}; !reflect.DeepEqual(station.WaitReasons, want) {
		t.Errorf("wait reasons %v", station.WaitReasons)
	}
	if want := []observationReason{{"", 1}, {"No available pod", 1}}; !reflect.DeepEqual(station.DispatchReasons, want) {
		t.Errorf("dispatch reasons %v", station.DispatchReasons)
	}
	if *station.Pods[1].BlockedBy != "p3" || !strings.Contains(report.Scope, "May now be stale") || !strings.Contains(report.Scope, "NOT MEASURED") ||
		!strings.Contains(report.Association, "does not prove physical presence") {
		t.Errorf("pods %+v, scope %q, association %q", station.Pods, report.Scope, report.Association)
	}
	// A pod belongs to its maneuver station, else the station of its berth,
	// else the station of its lane, else its reported station.
	moved := observationEnvelope()
	at(envelopeSimulation(moved), "vehicles", 0, "pod")["maneuverStationID"] = "t"
	pod := at(envelopeSimulation(moved), "vehicles", 3, "pod")
	pod["berthID"], pod["laneID"] = "", "straight"
	if stations := observe(t, moved).Stations; *stations[0].AssociatedPods != 3 || *stations[1].AssociatedPods != 1 || stations[1].Pods[0].ID != "p1" {
		t.Errorf("association order: %v %v", intValue(stations[0].AssociatedPods), intValue(stations[1].AssociatedPods))
	}
	encoded := Call(`{"op":"stationObservations","state":` + string(mustJSON(t, observationEnvelope())) + `}`)
	if strings.Contains(encoded, `"position"`) || strings.Contains(encoded, `"network"`) || !strings.Contains(encoded, `"observations":{"status":"snapshot"`) {
		t.Errorf("encoded report %s", encoded)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStationObservationsIdentityGates(t *testing.T) {
	t.Parallel()
	topology := func(r map[string]any) map[string]any { return at(r, "topology") }
	state := func(r map[string]any) map[string]any { return at(r, "frame", "state") }
	for name, test := range map[string]struct {
		mutate func(map[string]any)
		reason string
	}{
		"epoch":                {func(r map[string]any) { topology(r)["epoch"] = "other" }, "identity"},
		"server start":         {func(r map[string]any) { topology(r)["serverStart"] = "other" }, "identity"},
		"project revision":     {func(r map[string]any) { topology(r)["projectRevision"] = 8.0 }, "identity"},
		"missing server start": {func(r map[string]any) { delete(topology(r), "serverStart") }, "identity"},
		"null revision":        {func(r map[string]any) { state(r)["projectRevision"] = nil }, "identity"},
		"identity characters": {func(r map[string]any) {
			topology(r)["epoch"], state(r)["epoch"] = "invalid identity", "invalid identity"
		}, "identity"},
		"topology array":       {func(r map[string]any) { r["topology"] = []any{} }, "identity"},
		"negative revision":    {func(r map[string]any) { state(r)["revision"] = -1.0 }, "tick"},
		"unsafe tick":          {func(r map[string]any) { envelopeSimulation(r)["tick"] = math.Pow(2, 53) }, "tick"},
		"repeated berth":       {func(r map[string]any) { appendBerth(envelopeNetwork(r), 1, "b1") }, "duplicate berth"},
		"invalid berth":        {func(r map[string]any) { appendBerth(envelopeNetwork(r), 1, "") }, "duplicate berth"},
		"berths missing":       {func(r map[string]any) { delete(at(envelopeNetwork(r), "stations", 0), "berths") }, "berth definitions"},
		"unknown lane station": {func(r map[string]any) { at(envelopeNetwork(r), "lanes", 0)["stationID"] = "missing" }, "station or lane"},
		"null lane station":    {func(r map[string]any) { at(envelopeNetwork(r), "lanes", 0)["stationID"] = nil }, "station or lane"},
		"repeated station": {func(r map[string]any) {
			n := envelopeNetwork(r)
			n["stations"] = append(items(n["stations"]), items(n["stations"])[0])
		}, "station or lane"},
		"network not an object": {func(r map[string]any) { topology(r)["network"] = "network" }, "station or lane"},
	} {
		reply := observationEnvelope()
		test.mutate(reply)
		report := observe(t, reply)
		if report.Status != "unavailable" || len(report.Stations) != 0 || !strings.Contains(report.Reason, test.reason) {
			t.Errorf("%s: %+v", name, report)
		}
	}
	reply := observationEnvelope()
	envelopeSimulation(reply)["tick"] = math.Pow(2, 53) - 1
	if report := observe(t, reply); report.Status != "snapshot" {
		t.Errorf("largest safe tick refused: %+v", report)
	}
}

func appendBerth(network map[string]any, station int, id string) {
	entry := at(network, "stations", station)
	entry["berths"] = append(items(entry["berths"]), map[string]any{"id": id})
}

func TestStationObservationsUnavailableSources(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"berths", "vehicles", "pending"} {
		for name, value := range map[string]any{"absent": "absent", "null": nil, "object": map[string]any{}, "hole": []any{nil}, "too long": make([]any, observationMaxItems+1)} {
			if source == "pending" && name == "null" {
				continue
			}
			reply := observationEnvelope()
			if value == "absent" {
				delete(envelopeSimulation(reply), source)
			} else {
				envelopeSimulation(reply)[source] = value
			}
			station := observe(t, reply).Stations[0]
			unavailable := map[string]bool{
				"berths":   station.Occupancy == nil,
				"vehicles": station.AssociatedPods == nil && station.IdlePods == nil && station.WaitReasons == nil && station.Pods == nil,
				"pending":  station.PendingOriginRequests == nil,
			}[source]
			if !unavailable {
				t.Errorf("%s %s: %+v", source, name, station)
			}
		}
	}
	reply := observationEnvelope()
	sim := envelopeSimulation(reply)
	delete(at(sim, "berths", 0), "occupant")
	delete(at(sim, "vehicles", 0, "pod"), "activity")
	delete(at(sim, "vehicles", 1, "pod"), "waitReason")
	delete(at(sim, "vehicles", 2, "pod"), "stationPhase")
	delete(at(sim, "pending", 0), "dispatchReason")
	station := observe(t, reply).Stations[0]
	if station.Occupancy != nil || station.IdlePods != nil || station.BoardingPods != nil || station.BlockedPods != nil || station.DepartingPods != nil ||
		station.WaitReasons != nil || station.PendingOriginRequests != nil || station.DispatchReasons != nil || !strings.Contains(strings.Join(station.Lines, " "), "Unavailable") {
		t.Errorf("partial sources: %+v", station)
	}
	if phase := station.Pods[2].Phase; phase == nil || *phase != "Not reported" {
		t.Errorf("absent phase %v", phase)
	}
	for name, mutate := range map[string]func(map[string]any){
		"repeated pod":      func(sim map[string]any) { sim["vehicles"] = append(items(sim["vehicles"]), items(sim["vehicles"])[0]) },
		"unknown berth":     func(sim map[string]any) { at(sim, "vehicles", 0, "pod")["berthID"] = "unknown" },
		"unknown lane":      func(sim map[string]any) { at(sim, "vehicles", 0, "pod")["laneID"] = "unknown" },
		"unknown station":   func(sim map[string]any) { at(sim, "vehicles", 0, "pod")["stationID"] = "unknown" },
		"unknown maneuver":  func(sim map[string]any) { at(sim, "vehicles", 0, "pod")["maneuverStationID"] = "unknown" },
		"null maneuver":     func(sim map[string]any) { at(sim, "vehicles", 0, "pod")["maneuverStationID"] = nil },
		"pod not an object": func(sim map[string]any) { at(sim, "vehicles", 0)["pod"] = []any{} },
	} {
		reply := observationEnvelope()
		mutate(envelopeSimulation(reply))
		if station := observe(t, reply).Stations[0]; station.AssociatedPods != nil {
			t.Errorf("%s: associated %d", name, *station.AssociatedPods)
		}
	}
	repeated := observationEnvelope()
	sim = envelopeSimulation(repeated)
	sim["pending"] = append(items(sim["pending"]), items(sim["pending"])[0])
	if station := observe(t, repeated).Stations[0]; station.PendingOriginRequests != nil {
		t.Errorf("repeated request: pending %d", *station.PendingOriginRequests)
	}
}

func TestStationObservationsEmptySources(t *testing.T) {
	t.Parallel()
	reply := observationEnvelope()
	sim := envelopeSimulation(reply)
	sim["vehicles"], sim["pending"] = []any{}, []any{}
	for _, berth := range items(sim["berths"]) {
		object(berth)["occupant"], object(berth)["reservedBy"] = "", ""
	}
	station := observe(t, reply).Stations[0]
	if *station.Occupancy != (observationOccupancy{Free: 3, Capacity: 3}) || *station.BlockedPods != 0 || *station.PendingOriginRequests != 0 ||
		station.WaitReasons == nil || len(station.WaitReasons) != 0 || !strings.Contains(strings.Join(station.Lines, " "), "none reported") {
		t.Errorf("empty sources: %+v", station)
	}
	for name, pending := range map[string]any{"null": nil, "empty": []any{}} {
		reply := observationEnvelope()
		envelopeSimulation(reply)["pending"] = pending
		report := observe(t, reply)
		for _, station := range report.Stations {
			if station.PendingOriginRequests == nil || *station.PendingOriginRequests != 0 || station.DispatchReasons == nil || len(station.DispatchReasons) != 0 {
				t.Errorf("%s pending: %+v", name, station)
			}
		}
	}
	missing := observationEnvelope()
	delete(envelopeSimulation(missing), "pending")
	if station := observe(t, missing).Stations[0]; station.PendingOriginRequests != nil {
		t.Errorf("missing pending counted %d", *station.PendingOriginRequests)
	}
}

func TestStationObservationsBrowserText(t *testing.T) {
	t.Parallel()
	reply := observationEnvelope()
	pod := func(i int) map[string]any { return at(envelopeSimulation(reply), "vehicles", i, "pod") }
	// UTF-16 order puts U+10000 before U+FFFF. UTF-8 order does not.
	pod(1)["waitReason"], pod(2)["waitReason"] = "￿", "\U00010000"
	station := observe(t, reply).Stations[0]
	if want := []observationReason{{"\U00010000", 1}, {"￿", 1}}; !reflect.DeepEqual(station.WaitReasons, want) {
		t.Errorf("wait reason order %+v", station.WaitReasons)
	}
	// The text limit counts UTF-16 code units.
	for text, known := range map[string]bool{strings.Repeat("é", 4096): true, strings.Repeat("😀", 2048): true, strings.Repeat("😀", 2049): false} {
		reply := observationEnvelope()
		at(envelopeSimulation(reply), "vehicles", 1, "pod")["activity"] = text
		if station := observe(t, reply).Stations[0]; (station.IdlePods != nil) != known {
			t.Errorf("activity of %d bytes: known %v", len(text), station.IdlePods != nil)
		}
	}
	many := observationEnvelope()
	vehicles := items(envelopeSimulation(many)["vehicles"])
	for i := range 120 {
		vehicles = append(vehicles, observationPodValue("x"+strconv.Itoa(i), map[string]any{"waitReason": "W" + strconv.Itoa(i)}))
	}
	envelopeSimulation(many)["vehicles"] = vehicles
	lines := strings.Join(observe(t, many).Stations[0].Lines, "\n")
	for _, want := range []string{"Only the first 100 reason groups are shown.", "Only the first 100 of 123 associated pods are listed", "Pod p2: activity Boarding; phase At berth; wait Junction traffic; blockedBy p3."} {
		if !strings.Contains(lines, want) {
			t.Errorf("lines miss %q", want)
		}
	}
	if strings.Contains(lines, "Pod x97:") || !strings.Contains(lines, "Pod x96:") {
		t.Error("pod lines are not limited to 100")
	}
}

func TestObservationScope(t *testing.T) {
	t.Parallel()
	identity := `{"epoch":"epoch-1","serverStart":"server-1","projectRevision":7}`
	for _, test := range []struct {
		scope, want string
	}{
		{`{"identity":` + identity + `,"base":` + identity + `,"unchanged":true}`, "matches the draft base"},
		{`{"identity":` + identity + `,"base":` + identity + `,"unchanged":false}`, "Unsaved draft differs"},
		{`{"identity":` + identity + `,"base":{"epoch":"epoch-1","serverStart":"server-1","projectRevision":8},"unchanged":true}`, "identity differs"},
		{`{"identity":` + identity + `,"base":{"epoch":"epoch-1","serverStart":"server-2","projectRevision":7},"unchanged":true}`, "identity differs"},
		{`{"identity":` + identity + `,"base":{"epoch":"epoch-1","serverStart":"server-1","projectRevision":"7"},"unchanged":true}`, "identity differs"},
		{`{"identity":null,"base":` + identity + `,"unchanged":true}`, "identity differs"},
	} {
		var result struct {
			Scope string `json:"scope"`
		}
		encoded := Call(`{"op":"observationScope","scope":` + test.scope + `}`)
		if err := json.Unmarshal([]byte(encoded), &result); err != nil || !strings.Contains(result.Scope, test.want) {
			t.Errorf("%s: %s", test.scope, encoded)
		}
	}
}

func TestStationObservationRequests(t *testing.T) {
	t.Parallel()
	if MaxObservationRequestBytes < session.MaxHTTPStateJSON+1024 {
		t.Fatalf("observation request limit %d is below the HTTP state limit %d", MaxObservationRequestBytes, session.MaxHTTPStateJSON)
	}
	state := string(mustJSON(t, observationEnvelope()))
	// A reply larger than a project request still reads.
	padded := `{"op":"stationObservations","state":` + state[:len(state)-1] + `,"padding":"` + strings.Repeat("x", MaxRequestBytes) + `"}}`
	if encoded := Call(padded); !strings.Contains(encoded, `"status":"snapshot"`) {
		t.Errorf("large reply: %.200s", encoded)
	}
	if encoded := Call(`{"op":"validate","project":{"name":"` + strings.Repeat("x", MaxRequestBytes) + `"}}`); !strings.Contains(encoded, "too large") {
		t.Errorf("large project request: %.200s", encoded)
	}
	for name, input := range map[string]string{
		"project member": `{"op":"stationObservations","state":` + state + `,"project":{}}`,
		"scope member":   `{"op":"stationObservations","state":` + state + `,"scope":{}}`,
		"state array":    `{"op":"stationObservations","state":[]}`,
		"no state":       `{"op":"stationObservations"}`,
		"state on edit":  `{"op":"edit","state":{}}`,
	} {
		if encoded := Call(input); !strings.Contains(encoded, `"error"`) {
			t.Errorf("%s: %s", name, encoded)
		}
	}
	// A missing list is null, as the browser shows it as unavailable.
	reply := observationEnvelope()
	delete(envelopeSimulation(reply), "vehicles")
	encoded := Call(`{"op":"stationObservations","state":` + string(mustJSON(t, reply)) + `}`)
	if !strings.Contains(encoded, `"waitReasons":null`) || !strings.Contains(encoded, `"pods":null`) || !strings.Contains(encoded, `"associatedPods":null`) {
		t.Errorf("missing vehicles: %s", encoded)
	}
}
