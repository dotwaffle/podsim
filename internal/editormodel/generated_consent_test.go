package editormodel

import (
	"encoding/json/jsontext"
	"slices"
	"testing"
)

const consentKey = "generatedOrderConsent"

// TestGeneratedConsentEdit checks that Shared writes the member, that Private
// asks the browser to delete it, and that an edit refuses another value.
func TestGeneratedConsentEdit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, value string
		current     any
		wantPatch   bool
		want        any
	}{
		{"shared on absent", `"shared"`, nil, true, "shared"},
		{"shared on shared", `"shared"`, "shared", false, nil},
		{"shared on private", `"shared"`, "private", true, "shared"},
		{"private on absent", `"private"`, nil, false, nil},
		{"private on shared", `"private"`, "shared", true, nil},
		{"private on private", `"private"`, "private", true, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			draft := editFixture()
			if test.current != nil {
				draft[consentKey] = test.current
			}
			change, err := editProject(draft, jsontext.Value(`{"field":"`+consentKey+`","value":`+test.value+`}`))
			if err != nil {
				t.Fatal(err)
			}
			got, found := change.Patch[consentKey]
			if found != test.wantPatch || got != test.want {
				t.Fatalf("patch = %#v, %t, want %#v, %t", got, found, test.want, test.wantPatch)
			}
		})
	}
	for _, value := range []string{`"pooled"`, `""`, `"Shared"`, `1`, `true`} {
		if _, err := editProject(editFixture(), jsontext.Value(`{"field":"`+consentKey+`","value":`+value+`}`)); err == nil || err.Error() != "the generated order consent must be private or shared" {
			t.Errorf("value %s: error = %v", value, err)
		}
	}
}

// TestGeneratedConsentChecks checks the accepted values and the refusal of
// the draft checks.
func TestGeneratedConsentChecks(t *testing.T) {
	t.Parallel()
	const refused = "The generated order consent must be private or shared."
	for _, test := range []struct {
		name    string
		value   any
		refused bool
	}{
		{"private", "private", false},
		{"shared", "shared", false},
		{"empty", "", false},
		{"another value", "pooled", true},
		{"another type", float64(1), true},
		{"null", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var list checkList
			checkSettings(map[string]any{consentKey: test.value}, &list)
			found := slices.ContainsFunc(list.items, func(item check) bool { return item.Text == refused })
			if found != test.refused {
				t.Fatalf("checks = %+v, want refusal %t", list.items, test.refused)
			}
		})
	}
	var list checkList
	checkSettings(map[string]any{}, &list)
	if slices.ContainsFunc(list.items, func(item check) bool { return item.Text == refused }) {
		t.Fatal("the checks refused an absent member")
	}
}

// TestGeneratedConsentNormalization checks that normalization keeps an
// absent member absent, unlike the other shared ride settings, and that it
// does not change a member that the checks must see.
func TestGeneratedConsentNormalization(t *testing.T) {
	t.Parallel()
	for _, current := range []any{nil, "private", "shared", "pooled"} {
		draft := editFixture()
		if current != nil {
			draft[consentKey] = current
		}
		change, err := normalizeProject(draft)
		if err != nil {
			t.Fatal(err)
		}
		if got, found := change.Patch[consentKey]; found {
			t.Errorf("current %v: normalization patched the member with %#v", current, got)
		}
		if _, found := change.Patch["sharedRideMode"]; !found {
			t.Error("the fixture does not show that normalization adds the other settings")
		}
	}
}
