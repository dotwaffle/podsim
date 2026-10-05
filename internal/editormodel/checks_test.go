package editormodel

import (
	"bytes"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// checkFixtures captures distinct check reports from the former browser model.
// It covers malformed drafts, links, berth chains, network groups, demand and rail.
//
//go:embed testdata/checks.json
var checkFixtures []byte

func TestDraftChecksBrowserParity(t *testing.T) {
	t.Parallel()
	var fixtures []struct {
		Name    string      `json:"name"`
		Project any         `json:"project"`
		Checks  checkReport `json:"checks"`
	}
	if err := json.Unmarshal(checkFixtures, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			got := draftChecks(fixture.Project)
			// JSON v2 writes nil slices as empty arrays, as the browser expects.
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			var decoded checkReport
			if err = json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, fixture.Checks) {
				want, _ := json.Marshal(fixture.Checks)
				t.Errorf("checks differ\ngot %s\nwant %s", encoded, want)
			}
			model := new(engine)
			rawProject, err := json.Marshal(fixture.Project)
			if err != nil {
				t.Fatal(err)
			}
			keys := make([]string, 0, len(object(fixture.Project)))
			for key := range object(fixture.Project) {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			// Typed decode errors retain the raw draft for checks.
			_, _ = model.sync(request{Keys: keys, Patch: rawProject})
			for range 2 {
				result, err := model.handle(`{"op":"checks"}`)
				if err != nil {
					t.Fatal(err)
				}
				cached, _ := json.Marshal(result.Checks)
				if !bytes.Equal(cached, encoded) {
					t.Errorf("cached checks differ\ngot %s\nwant %s", cached, encoded)
				}
			}
		})
	}
}

func TestCheckCacheTracksChangedAndRemovedBranches(t *testing.T) {
	t.Parallel()
	config := project.Default()
	profile, demand, err := makeParkRide(config, validPlan())
	if err != nil {
		t.Fatal(err)
	}
	config.DemandProfiles, config.Demand = []project.DemandProfile{profile}, demand
	model := new(engine)
	keys := synchronize(t, model, config)
	read := func() checkReport {
		t.Helper()
		result, err := model.handle(`{"op":"checks"}`)
		if err != nil || result.Checks == nil {
			t.Fatal(err)
		}
		return *result.Checks
	}
	contains := func(report checkReport, message string) bool {
		return slices.ContainsFunc(report.Errors, func(item check) bool { return item.Text == message })
	}
	if got := read(); len(got.Errors) != 0 {
		t.Fatal("valid profile has errors", got.Errors)
	}
	prepared := model.checks
	if model.branches["demandProfiles"].value != nil {
		t.Fatal("worker retained generic flow maps")
	}
	if _, err := model.sync(request{Keys: keys, Patch: []byte(`{"name":""}`)}); err != nil {
		t.Fatal(err)
	}
	if !contains(read(), "The scenario needs a name.") || model.checks != prepared {
		t.Fatal("name change reused the old header or discarded prepared topology")
	}
	for index := range config.DemandProfiles[0].Flows {
		for weight := range config.DemandProfiles[0].Flows[index].Weights {
			config.DemandProfiles[0].Flows[index].Weights[weight] = 0
		}
	}
	patch, _ := json.Marshal(map[string]any{"demandProfiles": config.DemandProfiles})
	if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
		t.Fatal(err)
	}
	if !contains(read(), "Demand profile "+profile.ID+" has an empty band.") || model.checks != prepared {
		t.Fatal("profile change retained stale flow checks or discarded topology")
	}
	config.Network.Stations[1].ParkingOnly = true
	patch, _ = json.Marshal(map[string]any{"network": config.Network})
	if _, err := model.sync(request{Keys: keys, Patch: patch}); err != nil {
		t.Fatal(err)
	}
	if !contains(read(), "Demand profile "+profile.ID+" has an invalid flow.") || model.checks == prepared {
		t.Fatal("network change retained the previous passenger profile context")
	}
	keys = slices.DeleteFunc(keys, func(key string) bool { return key == "demandProfiles" })
	if _, err := model.sync(request{Keys: keys, Patch: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if !contains(read(), "The project has no demand profiles. Select another pattern.") {
		t.Fatal("removed profiles remain selectable")
	}
	synchronize(t, model, project.Default())
	if got := read(); len(got.Errors) != 0 {
		t.Fatal("complete replacement retained stale check results", got.Errors)
	}
}

func repeatedBerthDraft() project.Config {
	config := project.Default()
	config.Network = sim.Network{}
	for group := range 2 {
		entry, berth, exit := fmt.Sprintf("entry-%d", group), fmt.Sprintf("berth-%d", group), fmt.Sprintf("exit-%d", group)
		for index, id := range []string{entry, berth, exit} {
			config.Network.Nodes = append(config.Network.Nodes, sim.Node{ID: id, Position: sim.Point{X: float64(index * 40), Y: float64(group * 400)}})
		}
		for index, ends := range [][2]string{{entry, berth}, {berth, exit}, {entry, exit}} {
			config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: fmt.Sprintf("lane-%d-%d", group, index), From: ends[0], To: ends[1], SpeedLimit: 12})
		}
		for index := range project.MaxStations / 2 {
			id := fmt.Sprintf("station-%d-%d", group, index)
			station := sim.Station{ID: id, Name: id, Entry: entry, Exit: exit}
			for row := range project.MaxBerths {
				station.Berths = append(station.Berths, sim.Berth{ID: fmt.Sprintf("%s-%d", id, row), Node: berth})
			}
			config.Network.Stations = append(config.Network.Stations, station)
		}
	}
	first := config.Network.Stations[0]
	config.Fleet = []sim.Placement{{ID: "pod", StationID: first.ID, BerthID: first.Berths[0].ID}}
	return config
}

func TestChecksBoundRepeatedBerthSearches(t *testing.T) {
	t.Parallel()
	model := new(engine)
	synchronize(t, model, repeatedBerthDraft())
	result, err := model.handle(`{"op":"checks"}`)
	if err != nil || result.Checks == nil {
		t.Fatal(err)
	}
	// The largest per-station counts give 60,000 repeated berth rows.
	// Two duplicate-node errors and 150 cut-off stations cover both groups.
	if len(result.Checks.Errors) != 2+project.MaxStations/2 {
		t.Fatalf("unexpected repeated-berth report: %d errors", len(result.Checks.Errors))
	}
	if _, err := model.handle(`{"op":"validate"}`); err == nil {
		t.Fatal("malformed repeated berth draft became a valid project")
	}
}

func BenchmarkChecksRepeatedBerths(b *testing.B) {
	config := repeatedBerthDraft()
	raw, err := json.Marshal(config)
	if err != nil {
		b.Fatal(err)
	}
	var draft any
	if err = json.Unmarshal(raw, &draft); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		draftChecks(draft)
	}
}

func TestChecksPreserveInvalidDraftTargetsAndOperationBarrier(t *testing.T) {
	t.Parallel()
	model := new(engine)
	input := `{"op":"sync","keys":["network"],"patch":{"network":{"nodes":[{"id":"a","position":{"x":0,"y":0}},{"id":"b","position":{"x":40,"y":0}}],"lanes":[{"id":"road","from":"a","to":"b","speedLimit":0,"control":{"x":"bad","y":0}}],"stations":[]}}}`
	if _, err := model.handle(input); err == nil {
		t.Fatal("incorrectly typed draft was accepted")
	}
	result, err := model.handle(`{"op":"checks"}`)
	if err != nil || result.Checks == nil {
		t.Fatal("invalid synchronized draft lost its check results")
	}
	for _, message := range []string{"Lane road needs a positive speed limit.", "Lane road has an invalid control point."} {
		found := false
		for _, item := range result.Checks.Errors {
			if item.Text == message {
				found = item.Target != nil && *item.Target == (checkTarget{Type: "lane", ID: "road"})
			}
		}
		if !found {
			t.Errorf("missing lane target for %s", message)
		}
	}
	if _, err := model.handle(`{"op":"validate"}`); err == nil {
		t.Fatal("check inspection permitted invalid project operations")
	}
	for _, command := range []string{`{"op":"checks","view":{}}`, `{"op":"checks","parkRide":{}}`} {
		if _, err := model.handle(command); err == nil {
			t.Errorf("unrelated check parameters accepted: %s", command)
		}
	}
	var stateless response
	if err := json.Unmarshal([]byte(Call(`{"op":"checks","project":{"version":1,"network":{}}}`)), &stateless); err != nil || stateless.Checks == nil || len(stateless.Checks.Errors) != 2 {
		t.Fatal("stateless malformed draft lost its check results")
	}
}
