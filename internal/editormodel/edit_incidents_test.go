package editormodel

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// editIncident proposes the edit of a field with a text value, as the form
// does.
func editIncident(draft map[string]any, field, value string) (projectChange, error) {
	return editProject(draft, jsontext.Value(`{"field":"`+field+`","value":`+strconv.Quote(value)+`}`))
}

// TestIncidentNumberFields checks that each field writes its member, on a
// draft with no branch, and that clearing it removes the member and then the
// branch.
func TestIncidentNumberFields(t *testing.T) {
	t.Parallel()
	members := map[string][2]string{
		"faultEvacuationSeconds":   {"faults", "evacuationSeconds"},
		"faultPerHour":             {"faults", "perHour"},
		"faultDebrisShare":         {"faults", "debrisShare"},
		"faultDebrisMeters":        {"faults", "debrisMeters"},
		"faultDurationSeconds":     {"faults", "seconds"},
		"faultDurationMinSeconds":  {"faults", "minSeconds"},
		"faultDurationMaxSeconds":  {"faults", "maxSeconds"},
		"faultDurationMeanSeconds": {"faults", "meanSeconds"},
		"emergencyPerHour":         {"emergencies", "perHour"},
	}
	if len(members) != len(incidentFields) {
		t.Fatalf("%d fields, want %d", len(incidentFields), len(members))
	}
	for _, name := range slices.Sorted(maps.Keys(incidentFields)) {
		field := incidentFields[name]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := [2]string{field.branch, field.key}; got != members[name] {
				t.Fatalf("member = %v, want %v", got, members[name])
			}
			draft := editFixture()
			want := map[string]any{field.key: float64(7)}
			if field.duration {
				draft["faults"] = map[string]any{"duration": map[string]any{"kind": "exponential"}}
				want = map[string]any{"duration": map[string]any{"kind": "exponential", field.key: float64(7)}}
			}
			change, err := editIncident(draft, name, "7")
			if err != nil {
				t.Fatal(err)
			}
			if got := change.Patch[field.branch]; !reflect.DeepEqual(got, any(want)) {
				t.Fatalf("patch = %#v, want %#v", got, want)
			}
			if len(change.Patch) != 1 {
				t.Fatalf("patch = %#v, want one branch", change.Patch)
			}
			draft[field.branch] = change.Patch[field.branch]
			cleared, err := editIncident(draft, name, " ")
			if err != nil {
				t.Fatal(err)
			}
			if field.duration {
				want := map[string]any{"duration": map[string]any{"kind": "exponential"}}
				if got := cleared.Patch[field.branch]; !reflect.DeepEqual(got, any(want)) {
					t.Fatalf("cleared patch = %#v, want %#v", got, want)
				}
				return
			}
			got, found := cleared.Patch[field.branch]
			if !found || got != nil {
				t.Fatalf("cleared patch = %#v, %t, want a null branch", got, found)
			}
		})
	}
}

// TestIncidentEditKeepsUntouchedBranches checks that an edit that changes
// nothing proposes nothing: a clear on an absent member keeps an absent
// branch absent and an empty branch empty, and the same value proposes no
// change.
func TestIncidentEditKeepsUntouchedBranches(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		current any
		value   string
	}{
		{name: "clear on no branch", value: ""},
		{name: "clear on an empty branch", current: map[string]any{}, value: ""},
		{name: "clear on an absent member", current: map[string]any{"debrisShare": 0.5}, value: ""},
		{name: "same value", current: map[string]any{"perHour": float64(3)}, value: "3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := editFixture()
			if test.current != nil {
				draft["faults"] = test.current
			}
			change, err := editIncident(draft, "faultPerHour", test.value)
			if err != nil {
				t.Fatal(err)
			}
			if len(change.Patch) != 0 {
				t.Fatalf("patch = %#v, want none", change.Patch)
			}
		})
	}
}

// TestIncidentEditValues checks the zero value, which differs from a cleared
// input, the rounding of a whole number, and the refusal of other values. The
// range belongs to the native validator.
func TestIncidentEditValues(t *testing.T) {
	t.Parallel()
	change, err := editIncident(editFixture(), "faultPerHour", "0")
	if err != nil || !reflect.DeepEqual(change.Patch["faults"], any(map[string]any{"perHour": float64(0)})) {
		t.Errorf("zero: patch = %#v, %v", change.Patch, err)
	}
	change, err = editIncident(editFixture(), "faultDebrisShare", "0.25")
	if err != nil || !reflect.DeepEqual(change.Patch["faults"], any(map[string]any{"debrisShare": 0.25})) {
		t.Errorf("fraction: patch = %#v, %v", change.Patch, err)
	}
	change, err = editIncident(editFixture(), "faultEvacuationSeconds", "1.5")
	if err != nil || !reflect.DeepEqual(change.Patch["faults"], any(map[string]any{"evacuationSeconds": float64(1)})) {
		t.Errorf("whole number: patch = %#v, %v", change.Patch, err)
	}
	change, err = editIncident(editFixture(), "emergencyPerHour", "61")
	if err != nil || !reflect.DeepEqual(change.Patch["emergencies"], any(map[string]any{"perHour": float64(61)})) {
		t.Errorf("out of range: patch = %#v, %v", change.Patch, err)
	}
	if _, err := editIncident(editFixture(), "faultPerHour", "many"); err == nil || err.Error() != "an editor number must be finite" {
		t.Errorf("text: error = %v", err)
	}
	if _, err := editIncident(editFixture(), "faultDurationSeconds", "5"); err == nil || err.Error() != "choose a fault duration kind first" {
		t.Errorf("no kind: error = %v", err)
	}
	draft := editFixture()
	draft["faults"] = "wide"
	if _, err := editIncident(draft, "faultPerHour", "5"); err == nil || err.Error() != "the faults settings must be an object" {
		t.Errorf("malformed branch: error = %v", err)
	}
}

// TestFaultDurationKindEdit checks that a new kind keeps only its own
// members, that an empty kind removes the duration, and that the edit refuses
// another kind with the text of the validator.
func TestFaultDurationKindEdit(t *testing.T) {
	t.Parallel()
	full := map[string]any{"kind": "exponential", "minSeconds": float64(1), "maxSeconds": float64(9), "meanSeconds": float64(5)}
	for _, test := range []struct {
		name    string
		current map[string]any
		value   string
		want    any
	}{
		{name: "first kind", value: "fixed", want: map[string]any{"duration": map[string]any{"kind": "fixed"}}},
		{name: "exponential to uniform", current: map[string]any{"duration": full}, value: "uniform", want: map[string]any{"duration": map[string]any{"kind": "uniform", "minSeconds": float64(1), "maxSeconds": float64(9)}}},
		{name: "exponential to fixed", current: map[string]any{"duration": full}, value: "fixed", want: map[string]any{"duration": map[string]any{"kind": "fixed"}}},
		{name: "fixed to exponential", current: map[string]any{"duration": map[string]any{"kind": "fixed", "seconds": float64(600)}}, value: "exponential", want: map[string]any{"duration": map[string]any{"kind": "exponential"}}},
		{name: "keeps other members", current: map[string]any{"perHour": float64(2), "duration": full}, value: "", want: map[string]any{"perHour": float64(2)}},
		{name: "last member", current: map[string]any{"duration": full}, value: "", want: nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := editFixture()
			if test.current != nil {
				draft["faults"] = test.current
			}
			change, err := editIncident(draft, "faultDurationKind", test.value)
			if err != nil {
				t.Fatal(err)
			}
			got, found := change.Patch["faults"]
			if !found || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("patch = %#v, %t, want %#v", got, found, test.want)
			}
		})
	}
	if change, err := editIncident(editFixture(), "faultDurationKind", ""); err != nil || len(change.Patch) != 0 {
		t.Errorf("empty kind on no branch: patch = %#v, %v", change.Patch, err)
	}
	for _, value := range []string{"pareto", "Fixed"} {
		_, err := editIncident(editFixture(), "faultDurationKind", value)
		if err == nil || err.Error() != `faults duration kind must be "fixed", "uniform", or "exponential"` {
			t.Errorf("kind %s: error = %v", value, err)
		}
	}
}

// TestIncidentEditDoesNotChangeDraft checks that a proposal owns its branch.
func TestIncidentEditDoesNotChangeDraft(t *testing.T) {
	t.Parallel()
	draft := editFixture()
	duration := map[string]any{"kind": "fixed", "seconds": float64(5)}
	draft["faults"] = map[string]any{"duration": duration}
	if _, err := editIncident(draft, "faultDurationSeconds", "9"); err != nil {
		t.Fatal(err)
	}
	if duration["seconds"] != float64(5) {
		t.Fatalf("the edit changed the draft: %#v", duration)
	}
}

// TestIncidentNormalization checks that normalization adds no settings, and
// keeps the settings of a file.
func TestIncidentNormalization(t *testing.T) {
	t.Parallel()
	for _, current := range []any{nil, map[string]any{}, map[string]any{"perHour": float64(0)}} {
		draft := editFixture()
		if current != nil {
			draft["faults"], draft["emergencies"] = current, current
		}
		change, err := normalizeProject(draft)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"faults", "emergencies"} {
			if got, found := change.Patch[key]; found {
				t.Errorf("current %v: normalization patched %s with %#v", current, key, got)
			}
		}
	}
}

// TestIncidentEditVerdicts runs form edits through the worker. The checks
// refuse a rate above 60 and a rate with no duration with the refusals of the
// server, and a full form passes them.
func TestIncidentEditVerdicts(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(project.Default(), jsonv1.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	model := new(engine)
	if err := syncDraft(model, raw); err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatal(err)
	}
	edit := func(field, value string) {
		t.Helper()
		change, err := editIncident(draft, field, value)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(draft, change.Patch)
		// A project error keeps the draft in the worker, and the checks
		// report it, so the next edit can repair the draft.
		_ = syncDraft(model, encodeDraft(t, draft))
	}
	verdict := func() string {
		t.Helper()
		if _, err := model.handle(`{"op":"validate"}`); err != nil {
			return err.Error()
		}
		return ""
	}
	refused := func(name, want string) {
		t.Helper()
		if got := verdict(); !strings.Contains(got, want) {
			t.Errorf("%s: verdict %q, want %q", name, got, want)
		}
	}
	if got := verdict(); got != "" {
		t.Fatalf("base project: %s", got)
	}
	edit("faultPerHour", "5")
	refused("rate with no duration", "faults perHour above 0 needs a duration")
	edit("faultDurationKind", "fixed")
	refused("kind with no seconds", "faults duration kind fixed needs each of its values from 1 to 86400 seconds")
	edit("faultDurationSeconds", "600")
	if got := verdict(); got != "" {
		t.Errorf("full form: %s", got)
	}
	edit("faultPerHour", "61")
	refused("rate above the limit", "faults perHour must be 0 to 60")
	edit("faultPerHour", "5")
	edit("emergencyPerHour", "61")
	refused("emergency rate above the limit", "emergencies perHour must be 0 to 60")
}
