package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func synchronize(t *testing.T, model *engine, config project.Config) []string {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if _, err := model.sync(request{Keys: keys, Patch: data}); err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestEngineOwnsCompleteProjectAndReplacesBranches(t *testing.T) {
	t.Parallel()
	config := project.Default()
	additional := `{"geo":{"latitude":51.5,"longitude":0,"projection":"equirectangular","radius":6371000},"map":{"provider":"osm","opacity":0.5},"sharedRidePartyLimit":2,"sharedRideMode":"destination","sharedRideMaxStops":4,"sharedRideJoin":"reassign-existing","pickupReassignment":true,"platoonLimit":3,"redistribution":true}`
	if err := json.Unmarshal([]byte(additional), &config); err != nil {
		t.Fatal(err)
	}
	profile, demand, err := makeParkRide(config, validPlan())
	if err != nil {
		t.Fatal(err)
	}
	config.DemandProfiles, config.Demand = []project.DemandProfile{profile}, demand
	model := new(engine)
	keys := synchronize(t, model, config)
	got, err := json.Marshal(model.config)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(config)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("project state differs: %v", err)
	}
	config.Network.Nodes[0].Position.X = 900
	*config.DemandProfiles[0].Bands[0].PerMinute = 99
	if model.config.Network.Nodes[0].Position.X == 900 || *model.config.DemandProfiles[0].Bands[0].PerMinute == 99 {
		t.Fatal("worker state shares caller data")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"demand":{"enabled":true}}`)}); err != nil {
		t.Fatal(err)
	}
	if model.config.Demand != (project.DemandConfig{Enabled: true}) {
		t.Fatal("replacement retained omitted fields of the old demand")
	}
	keys = slices.DeleteFunc(keys, func(key string) bool { return key == "geo" || key == "map" })
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{}`)}); err != nil || model.config.Geo != nil || model.config.Map != nil {
		t.Fatal("removed project fields remain in worker state")
	}
	for field := range reflect.TypeFor[project.Config]().Fields() {
		key, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		var dst project.Config
		if !copyBranch(&dst, key, project.Config{}) {
			t.Errorf("project field %s has no editor transfer rule", key)
		}
	}
}

func TestEngineRejectsInvalidStateAndRecovers(t *testing.T) {
	t.Parallel()
	model := new(engine)
	keys := synchronize(t, model, project.Default())
	for _, patch := range []string{`{"demand":{"seed":-1}}`, `{"name":false}`} {
		if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(patch)}); err == nil {
			t.Fatal("invalid branch was accepted")
		}
		if _, err := model.handle(`{"op":"validate"}`); err == nil {
			t.Fatal("operation used the previous valid state after an invalid update")
		}
		if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{"redistribution":true}`)}); err == nil {
			t.Fatal("an unrelated update cleared the invalid branch")
		}
		keys = synchronize(t, model, project.Default())
		if result, err := model.handle(`{"op":"validate"}`); err != nil || !result.Valid {
			t.Fatal("corrected project did not recover")
		}
	}
	unknown := append(slices.Clone(keys), "unknown")
	if _, err := model.sync(request{Keys: unknown, Patch: jsontext.Value(`{"unknown":true}`)}); err == nil {
		t.Fatal("unknown project member was accepted")
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{}`)}); err != nil {
		t.Fatal("removing an unknown member did not recover")
	}
}

func TestStatefulCallsPreserveExplicitProjectIsolation(t *testing.T) {
	t.Parallel()
	model := new(engine)
	keys := synchronize(t, model, project.Default())
	input := `{"op":"place-view","project":{"geo":{"latitude":0,"longitude":0,"projection":"equirectangular","radius":6371000}},"view":{"latitude":0,"longitude":0,"width":900,"height":800}}`
	if result, err := model.handle(input); err != nil || result.View == nil {
		t.Fatal("explicit project operation failed")
	}
	if result, err := model.handle(`{"op":"validate"}`); err != nil || !result.Valid || model.config.Geo != nil {
		t.Fatal("place navigation replaced the synchronized project")
	}
	for _, input := range []string{
		`{"op":"sync","keys":[],"patch":{},"view":{}}`,
		`{"op":"sync","patch":{}}`,
		`{"op":"validate","keys":[],"patch":{}}`,
		`{"op":"sync","keys":["name","name"],"patch":{"name":"Test"}}`,
		`{"op":"sync","keys":[],"patch":{"name":"Undeclared"}}`,
		`{"op":"sync","keys":["missing"],"patch":{}}`,
		`{"op":"sync","keys":["network"],"patch":{"network":{"nodes":[` + strings.Repeat(`{},`, project.MaxNodes) + `{}]}}}`,
	} {
		if _, err := model.handle(input); err == nil {
			t.Fatalf("invalid request accepted: %.100s", input)
		}
	}
	if _, err := model.sync(request{Keys: keys, Patch: jsontext.Value(`{}`)}); err != nil {
		t.Fatal("structurally rejected requests damaged the previous state")
	}
	call := NewCall()
	if !strings.Contains(call(`{"op":"validate"}`), "not synchronized") {
		t.Fatal("new handler inherited another worker's state")
	}
	if !strings.Contains(call(`{"op":"sync","keys":["name"],"patch":{"name":"Draft"}}`), `"valid":true`) || !strings.Contains(call(`{"op":"validate"}`), `"error"`) {
		t.Fatal("stateful handler did not distinguish decoded draft from valid project")
	}
}

func TestEngineBoundsAccumulatedProject(t *testing.T) {
	t.Parallel()
	model := new(engine)
	node := `{"id":"` + strings.Repeat("a", 1000) + `","position":{"x":0,"y":0}}`
	network := `{"nodes":[` + strings.Repeat(node+`,`, 999) + node + `],"lanes":[],"stations":[]}`
	if _, err := model.handle(`{"op":"sync","keys":["network"],"patch":{"network":` + network + `}}`); err != nil {
		t.Fatal(err)
	}
	flow := `{"from":"` + strings.Repeat("a", 36) + `","to":"` + strings.Repeat("b", 36) + `","weights":[` + strings.Repeat(`0,`, 23) + `0]}`
	// The flows fill the file limit, less half of the network.
	flows := (project.MaxFileBytes - len(network)/2) / (len(flow) + 1)
	profiles := `[{"id":"p","name":"Profile","bands":[],"flows":[` + strings.Repeat(flow+`,`, flows-1) + flow + `]}]`
	if len(profiles) >= project.MaxFileBytes || len(profiles)+len(network) <= project.MaxFileBytes {
		t.Fatal("fixture does not test accumulation across individually bounded updates")
	}
	if _, err := model.handle(`{"op":"sync","keys":["network","demandProfiles"],"patch":{"demandProfiles":` + profiles + `}}`); err == nil {
		t.Fatal("combined retained project exceeded its size limit")
	}
	if len(model.config.Network.Nodes) != 1000 || len(model.config.DemandProfiles) != 0 {
		t.Fatal("oversized update replaced the previous project")
	}
}

// TestSyncRefusalOrder pins the error that a synchronization with two
// faults gets. sync checks the request shape, the patch, each key in order,
// the undeclared patch keys, each branch in key order, and then the
// metadata branches together. Each case breaks two checks, and the earlier
// check gives the refusal. A refusal before the branch errors leaves the
// engine unsynchronized.
func TestSyncRefusalOrder(t *testing.T) {
	t.Parallel()
	// encoding/json/v2 picks the wording of its errors once in each
	// process, so the test gets the decoder text in the same process. The
	// local type has the name of the type in decodeBranch.
	type branchConfig project.Config
	decodeText := func(data string, value any, options ...json.Options) string {
		t.Helper()
		err := json.Unmarshal([]byte(data), value, options...)
		if err == nil {
			t.Fatalf("decode %s: no error", data)
		}
		return err.Error()
	}
	var (
		draft  = "decode editor draft branch: " + decodeText("1e400", new(any))
		bogus  = "decode project branch bogus: " + decodeText(`{"bogus":1}`, new(branchConfig), json.RejectUnknownMembers(true))
		extra  = "decode project branch extra: " + decodeText(`{"extra":1}`, new(branchConfig), json.RejectUnknownMembers(true))
		meta   = decodeText(`{"version":4}`, new(project.Config), json.RejectUnknownMembers(true))
		patch  = "decode project patch: " + decodeText(`{"name":"a","name":"b"}`, new(map[string]jsontext.Value))
		large  = "editor project is too large"
		needs  = "project synchronization needs missing"
		repeat = "project synchronization repeats a key"
	)
	duplicate := `{"name":"a","name":"b"}`
	network := `"network":"` + strings.Repeat("x", project.MaxFileBytes) + `"`
	for _, test := range []struct {
		name   string
		keys   []string
		patch  string
		want   string
		synced bool
	}{
		{"keys_before_patch", nil, duplicate, "synchronization needs project keys and a patch object", false},
		{"patch_before_keys", []string{"name", "name"}, duplicate, patch, false},
		{"repeat_before_later_key", []string{"name", "name", "missing"}, `{"name":"a"}`, repeat, false},
		{"missing_before_repeat", []string{"missing", "name", "name"}, `{"name":"a"}`, needs, false},
		{"missing_before_size", []string{"missing", "network"}, "{" + network + "}", needs, false},
		{"size_before_missing", []string{"network", "missing"}, "{" + network + "}", large, false},
		{"size_before_undeclared", []string{"network"}, "{" + network + `,"undeclared":1}`, large, false},
		{"undeclared_before_branches", []string{"name"}, `{"name":1e400,"undeclared":1}`, "project patch contains an undeclared key", false},
		{"draft_after_branch_error", []string{"bogus", "name"}, `{"bogus":1,"name":1e400}`, draft, false},
		{"branch_errors_in_key_order", []string{"bogus", "extra"}, `{"bogus":1,"extra":1}`, bogus, true},
		{"key_order_not_patch_order", []string{"extra", "bogus"}, `{"bogus":1,"extra":1}`, extra, true},
		{"branch_error_before_metadata", []string{"bogus", "version"}, `{"bogus":1,"version":4}`, bogus, true},
		{"metadata", []string{"version"}, `{"version":4}`, meta, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			model := new(engine)
			_, err := model.sync(request{Keys: test.keys, Patch: jsontext.Value(test.patch)})
			if err == nil || err.Error() != test.want {
				t.Fatalf("got %v, want %q", err, test.want)
			}
			if model.ready != test.synced {
				t.Fatalf("synchronized is %v, want %v", model.ready, test.synced)
			}
		})
	}
}
