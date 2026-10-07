package project

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"strings"
	"testing"
)

// TestDemandProfileJSONShape pins the JSON form of a demand profile. The
// station list has the order of first use, and each flow is an array of
// two station indexes and the weights.
func TestDemandProfileJSONShape(t *testing.T) {
	t.Parallel()
	profile := testDemandProfile()
	profile.Flows = append(profile.Flows, DemandFlow{From: "market", To: "harbor", Weights: []float64{0.25}})
	const want = `{"id":"weekday","name":"Weekday","bands":[{"id":"am","name":"AM peak","startMinute":420,"durationMinutes":180}],` +
		`"stations":["harbor","garden","market"],"flows":[[0,1,3],[1,2,1],[2,0,0.25]]}`
	for name, encode := range map[string]func(any) ([]byte, error){
		"canonical": func(value any) ([]byte, error) { return jsonv2.Marshal(value, jsonv2.Deterministic(true)) },
		"version 1": func(value any) ([]byte, error) { return jsonv2.Marshal(value, json.DefaultOptionsV1()) },
		"package 1": json.Marshal,
	} {
		got, err := encode(profile)
		if err != nil || string(got) != want {
			t.Fatalf("%s encoding = %s, %v; want %s", name, got, err, want)
		}
	}
	var decoded DemandProfile
	if err := json.Unmarshal([]byte(want), &decoded); err != nil || !reflect.DeepEqual(decoded, profile) {
		t.Fatalf("decoded %+v, %v; want %+v", decoded, err, profile)
	}
}

// TestProjectDecodeRefusesInvalidFlowJSON pins the order and the text of
// the decode refusals of the compact flows: a repeated station, then each
// flow in order, then a station that no flow names.
func TestProjectDecodeRefusesInvalidFlowJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, stations, flows, want string
	}{
		{"repeated station", `["harbor","garden","harbor"]`, `[[0,9,1]]`, `demand profile "weekday" lists station "harbor" more than once`},
		{"flows not an array", `["harbor","garden"]`, `{}`, `demand profile "weekday" flows must be an array`},
		{"flow object", `["harbor","garden"]`, `[{"from":"harbor","to":"garden","weights":[1]}]`, `demand profile "weekday" flow 0 must be an array that starts with two station indexes`},
		{"flow with one index", `["harbor","garden"]`, `[[0,1,1],[0]]`, `demand profile "weekday" flow 1 must be an array that starts with two station indexes`},
		{"empty flow", `["harbor","garden"]`, `[[]]`, `demand profile "weekday" flow 0 must be an array that starts with two station indexes`},
		{"index out of range", `["harbor","garden"]`, `[[0,2,1]]`, `demand profile "weekday" flow 0 has an invalid station index`},
		{"negative index", `["harbor","garden"]`, `[[-1,1,1]]`, `demand profile "weekday" flow 0 has an invalid station index`},
		{"index with a fraction", `["harbor","garden"]`, `[[0,1.0,1]]`, `demand profile "weekday" flow 0 has an invalid station index`},
		{"index with an exponent", `["harbor","garden"]`, `[[0,1e0,1]]`, `demand profile "weekday" flow 0 has an invalid station index`},
		{"index as text", `["harbor","garden"]`, `[["0",1,1]]`, `demand profile "weekday" flow 0 has an invalid station index`},
		{"index past a flow error", `["harbor","garden","market"]`, `[[0,5,1],[0,1,"x"]]`, `demand profile "weekday" flow 0 has an invalid station index`},
		{"weight as text", `["harbor","garden"]`, `[[0,1,"1"]]`, `demand profile "weekday" flow 0 has a weight that is not a number`},
		{"null weight", `["harbor","garden"]`, `[[0,1,null]]`, `demand profile "weekday" flow 0 has a weight that is not a number`},
		{"weight out of range", `["harbor","garden"]`, `[[0,1,1e999]]`, `demand profile "weekday" flow 0 has a weight that is not a number`},
		{"weight before an unused station", `["harbor","garden","market"]`, `[[0,1,[1]]]`, `demand profile "weekday" flow 0 has a weight that is not a number`},
		{"unused station", `["harbor","garden","market"]`, `[[0,1,1]]`, `demand profile "weekday" lists station "market" that no flow names`},
		{"stations missing", ``, `[[0,1,1]]`, `demand profile "weekday" flow 0 has an invalid station index`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw := profileProjectJSON(t, test.stations, test.flows)
			var config Config
			err := json.Unmarshal(raw, &config)
			if err == nil || err.Error() != test.want {
				t.Fatalf("decode error %v, want %s", err, test.want)
			}
			if _, err := DecodeCanonicalJSON(raw); err == nil || err.Error() != test.want {
				t.Fatalf("canonical decode error %v, want %s", err, test.want)
			}
		})
	}
}

// TestProjectValidateChecksDecodedFlows checks that the flow rules that
// Validate applies to the in-memory flows keep their refusals for a decoded
// project.
func TestProjectValidateChecksDecodedFlows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, stations, flows, want string
	}{
		{"valid", `["harbor","garden"]`, `[[0,1,1]]`, ""},
		{"no flows", `[]`, `[]`, `demand profile "weekday" must contain 1 to 400000 flows`},
		{"null flows", `[]`, `null`, `demand profile "weekday" must contain 1 to 400000 flows`},
		{"no weights", `["harbor","garden"]`, `[[0,1]]`, `demand profile "weekday" has an invalid flow from "harbor" to "garden"`},
		{"extra weight", `["harbor","garden"]`, `[[0,1,1,1]]`, `demand profile "weekday" has an invalid flow from "harbor" to "garden"`},
		{"same station", `["harbor"]`, `[[0,0,1]]`, `demand profile "weekday" has an invalid flow from "harbor" to "harbor"`},
		{"repeated pair", `["harbor","garden"]`, `[[0,1,1],[0,1,2]]`, `demand profile "weekday" has an invalid flow from "harbor" to "garden"`},
		{"unknown station", `["harbor","nowhere"]`, `[[0,1,1]]`, `demand profile "weekday" has an invalid flow from "harbor" to "nowhere"`},
		{"negative weight", `["harbor","garden"]`, `[[0,1,-1]]`, `demand profile "weekday" has an invalid weight`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeCanonicalJSON(profileProjectJSON(t, test.stations, test.flows))
			if test.want == "" && err != nil || test.want != "" && (err == nil || err.Error() != test.want) {
				t.Fatalf("error %v, want %q", err, test.want)
			}
		})
	}
}

// profileProjectJSON returns the default project with one demand profile
// "weekday" of one band. The profile has the stations and flows members
// that the caller gives. An empty stations text omits the member.
func profileProjectJSON(t *testing.T, stations, flows string) []byte {
	t.Helper()
	config := Default()
	config.DemandProfiles = []DemandProfile{{ID: "weekday", Name: "Weekday", Bands: testDemandProfile().Bands}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	members := `"flows":` + flows
	if stations != "" {
		members = `"stations":` + stations + `,` + members
	}
	encoded := `"stations":[],"flows":[]`
	if !strings.Contains(string(raw), encoded) {
		t.Fatalf("project has no empty flows: %s", raw)
	}
	return []byte(strings.Replace(string(raw), encoded, members, 1))
}
