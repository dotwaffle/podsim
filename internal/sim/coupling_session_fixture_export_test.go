package sim

import (
	"bytes"
	"crypto/sha256"
	legacyJSON "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// couplingFixtureEnv names the output file of TestCouplingSessionFixtureExport.
const couplingFixtureEnv = "PODSIM_COUPLING_FIXTURE_OUT"

// TestCouplingSessionFixtureExport writes the private certificate states
// that internal/session/testdata/coupling_native_phases.json holds. It runs
// only when PODSIM_COUPLING_FIXTURE_OUT names the output file. Each frame
// hash is the SHA-256 of the encoding/json form of the complete restore
// input, which the session test recomputes.
func TestCouplingSessionFixtureExport(t *testing.T) {
	path := os.Getenv(couplingFixtureEnv)
	if path == "" {
		t.Skip(couplingFixtureEnv + " is not set")
	}
	type frame struct {
		Name   string         `json:"name"`
		SHA256 string         `json:"sha256"`
		Cohort string         `json:"cohort"`
		State  jsontext.Value `json:"state"`
	}
	type fixture struct {
		Provenance           string                    `json:"provenance"`
		SourceManifestSHA256 string                    `json:"sourceManifestSha256"`
		Cohorts              map[string]jsontext.Value `json:"cohorts"`
		Frames               []frame                   `json:"frames"`
	}
	out := fixture{
		Provenance:           "Private native certificate-derived saved phases. Not live recruitment. Exact ROOT fixture State records and common cohort inputs.",
		SourceManifestSHA256: os.Getenv("PODSIM_COUPLING_FIXTURE_MANIFEST_SHA256"),
		Cohorts:              map[string]jsontext.Value{},
	}
	cohorts := map[string]RestoreStateInput{}
	for _, occupied := range []bool{false, true} {
		cohort := "empty"
		if occupied {
			cohort = "occupied"
		}
		for _, phase := range []struct {
			phase couplingReservationPhase
			leg   int
		}{
			{couplingClosing, 0}, {couplingLatching, -1}, {couplingConnected, 1},
			{couplingUnlatching, -1}, {couplingOpening, 2}, {couplingDraining, 3}, {couplingDraining, 4},
		} {
			input := nativeCouplingSavedFixture(t, occupied, phase.phase, phase.leg)
			data, err := legacyJSON.Marshal(input) //nolint:musttag // The fixture keeps the Go names of the native API.
			if err != nil {
				t.Fatal(err)
			}
			state, err := legacyJSON.Marshal(input.State)
			if err != nil {
				t.Fatal(err)
			}
			common := input
			common.State = SavedState{}
			if previous, found := cohorts[cohort]; found && !reflect.DeepEqual(previous, common) {
				t.Fatal("cohort inputs differ between phases", cohort)
			}
			cohorts[cohort] = common
			name := fmt.Sprintf("occupied-%t-phase-%d-leg-%d.json", occupied, phase.phase, phase.leg)
			out.Frames = append(out.Frames, frame{Name: name, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Cohort: cohort, State: state})
		}
	}
	for name, common := range cohorts {
		data, err := legacyJSON.Marshal(common) //nolint:musttag // The fixture keeps the Go names of the native API.
		if err != nil {
			t.Fatal(err)
		}
		out.Cohorts[name] = withoutStateMember(t, data)
	}
	data, err := json.Marshal(out, json.Deterministic(true), jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// withoutStateMember removes the top-level State member of a restore input.
func withoutStateMember(t *testing.T, data []byte) jsontext.Value {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	var buffer bytes.Buffer
	encoder := jsontext.NewEncoder(&buffer)
	for {
		token, err := decoder.ReadToken()
		if err != nil {
			break
		}
		if decoder.StackDepth() == 1 && token.Kind() == '"' && token.String() == "State" {
			if _, err := decoder.ReadValue(); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := encoder.WriteToken(token); err != nil {
			t.Fatal(err)
		}
	}
	return jsontext.Value(buffer.Bytes())
}
