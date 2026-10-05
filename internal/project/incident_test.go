package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestIncidentMarkerRoundTrip checks that the incident marker survives a
// project round trip, and that a project without it has no member.
func TestIncidentMarkerRoundTrip(t *testing.T) {
	t.Parallel()
	plain, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte("incidentContract")) {
		t.Fatal("unmarked project has an incident member")
	}
	config := Default()
	config.IncidentContract = sim.IncidentV1Contract
	if validateErr := Validate(config); validateErr != nil {
		t.Fatal(validateErr)
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"incidentContract":"incident-v1"`)) {
		t.Fatal("marker omitted")
	}
	var got Config
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, config) {
		t.Fatal("marked project changed in a round trip")
	}
}

// TestIncidentMarkerRejectsOtherValues checks that the decoder refuses an
// incident marker that is not incident-v1, also an explicit null or an
// empty text, which the typed decode cannot tell from no marker. A failed
// decode does not change the project.
func TestIncidentMarkerRejectsOtherValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{`null`, `""`, `"incident-v2"`, `"Incident-v1"`, `0`, `true`, `[]`, `{}`} {
		got := Default()
		before := Clone(got)
		raw := []byte(`{"version":1,"incidentContract":` + value + `}`)
		if err := json.Unmarshal(raw, &got); err == nil {
			t.Errorf("incident marker %s accepted", value)
		}
		if !reflect.DeepEqual(before, got) {
			t.Errorf("failed decode of marker %s changed the project", value)
		}
	}
	config := Default()
	config.IncidentContract = "incident-v2"
	if err := Validate(config); !errors.Is(err, sim.ErrUnknownIncidentContract) {
		t.Fatalf("Validate error %v, want an unknown incident contract", err)
	}
}
