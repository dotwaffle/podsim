package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// keptEngine drives one engine through a sequence of changes, and compares
// each reply with the reply of a new engine that holds the same branch
// bytes. The new engine has no kept result, so a difference shows a kept
// result that is stale.
type keptEngine struct {
	t     *testing.T
	model *engine
	keys  []string
	// large skips the new engine when the stateless handler gives the
	// reference, because a new engine decodes the project one more time.
	large bool
}

func newKeptEngine(t *testing.T, config project.Config) *keptEngine {
	t.Helper()
	k := &keptEngine{t: t, model: new(engine)}
	k.keys = synchronize(t, k.model, config)
	return k
}

// ask sends one request to model and returns the encoded reply.
func ask(model *engine, request string) string {
	return model.call(request)
}

// patch synchronizes new values of some branches. The project can then be
// invalid.
func (k *keptEngine) patch(values map[string]any) {
	k.t.Helper()
	raw, err := json.Marshal(values)
	if err != nil {
		k.t.Fatal(err)
	}
	k.patchRaw(raw)
}

func (k *keptEngine) patchRaw(raw jsontext.Value) {
	k.t.Helper()
	if result, err := k.model.sync(request{Keys: k.keys, Patch: raw}); !result.Synced {
		k.t.Fatalf("the patch did not synchronize: %v", err)
	}
}

// fresh returns a new engine with the branch bytes of the engine.
func (k *keptEngine) fresh() *engine {
	k.t.Helper()
	branches := make(map[string]jsontext.Value, len(k.model.branches))
	for key, branch := range k.model.branches {
		branches[key] = branch.raw
	}
	raw, err := json.Marshal(branches)
	if err != nil {
		k.t.Fatal(err)
	}
	model := new(engine)
	if result, err := model.sync(request{Keys: k.keys, Patch: raw}); !result.Synced {
		k.t.Fatalf("the reference engine did not synchronize: %v", err)
	}
	return model
}

// project returns the project of the engine as one JSON object.
func (k *keptEngine) project() string {
	k.t.Helper()
	branches := make(map[string]jsontext.Value, len(k.model.branches))
	for key, branch := range k.model.branches {
		branches[key] = branch.raw
	}
	raw, err := json.Marshal(branches)
	if err != nil {
		k.t.Fatal(err)
	}
	return string(raw)
}

// compare sends each operation to the engine, in order, and fails when a
// reply differs from its references. The first reference is a new engine.
// The second is the stateless handler, which keeps no result at all: for
// the checks always, and for the validation when the project synchronized
// with no error. A large project skips the new engine when it has the
// second reference. It returns the replies.
func (k *keptEngine) compare(step string, operations ...string) []string {
	k.t.Helper()
	replies := make([]string, 0, len(operations))
	for _, operation := range operations {
		request := `{"op":"` + operation + `"}`
		got := ask(k.model, request)
		stateless := operation == "checks" || k.model.err == nil
		if !stateless || !k.large {
			if want := ask(k.fresh(), request); got != want {
				k.t.Fatalf("%s: %s differs from a new engine:\n got %.400s\nwant %.400s", step, operation, got, want)
			}
		}
		if stateless {
			if want := Call(`{"op":"` + operation + `","project":` + k.project() + `}`); got != want {
				k.t.Fatalf("%s: %s differs from the stateless handler:\n got %.400s\nwant %.400s", step, operation, got, want)
			}
		}
		replies = append(replies, got)
	}
	return replies
}

// TestProfileVerdictKey pins the key of the kept profile check: the bytes of
// the demand profiles and the set of the passenger station IDs. A change
// that keeps both keeps the verdict, with and without a profile error. A
// change of one of them runs the check again.
func TestProfileVerdictKey(t *testing.T) {
	t.Parallel()
	config := project.Default()
	profile, demand, err := makeParkRide(config, validPlan())
	if err != nil {
		t.Fatal(err)
	}
	config.DemandProfiles, config.Demand = []project.DemandProfile{profile}, demand
	k := newKeptEngine(t, config)
	network := func() map[string]any { return map[string]any{"network": config.Network} }
	// step applies a change and reports whether the engine kept the verdict.
	step := func(name string, change func()) bool {
		t.Helper()
		before := k.model.profileVerdict
		change()
		k.compare(name, "validate", "checks")
		if k.model.profileVerdict == nil {
			t.Fatalf("%s: the engine has no profile verdict", name)
		}
		return k.model.profileVerdict == before
	}
	step("start", func() {})
	for _, errors := range []bool{false, true} {
		if errors {
			for index := range config.DemandProfiles[0].Flows {
				for weight := range config.DemandProfiles[0].Flows[index].Weights {
					config.DemandProfiles[0].Flows[index].Weights[weight] = 0
				}
			}
			if step("empty band", func() { k.patch(map[string]any{"demandProfiles": config.DemandProfiles}) }) {
				t.Fatal("a change of the profiles kept the verdict")
			}
			if len(k.model.profileVerdict.errors) == 0 {
				t.Fatal("the empty band gives no profile error")
			}
		}
		if !step("move node", func() { config.Network.Nodes[0].Position.X += 0.5; k.patch(network()) }) {
			t.Fatalf("errors=%t: a node move ran the profile check again", errors)
		}
		if !step("rename station", func() { config.Network.Stations[0].Name += " A"; k.patch(network()) }) {
			t.Fatalf("errors=%t: a station rename ran the profile check again", errors)
		}
		if !step("name", func() { k.patch(map[string]any{"name": "Other"}) }) {
			t.Fatalf("errors=%t: a name change ran the profile check again", errors)
		}
		for _, parking := range []bool{true, false} {
			if step("parking flag", func() { config.Network.Stations[1].ParkingOnly = parking; k.patch(network()) }) {
				t.Fatalf("errors=%t: parking=%t kept the verdict of another passenger set", errors, parking)
			}
		}
		stations := config.Network.Stations
		added := stations[0]
		added.ID, added.Berths = "added", nil
		if step("add station", func() { config.Network.Stations = append(slices.Clone(stations), added); k.patch(network()) }) {
			t.Fatalf("errors=%t: a new passenger station kept the verdict", errors)
		}
		if step("delete station", func() { config.Network.Stations = stations; k.patch(network()) }) {
			t.Fatalf("errors=%t: a deleted passenger station kept the verdict", errors)
		}
	}
}
