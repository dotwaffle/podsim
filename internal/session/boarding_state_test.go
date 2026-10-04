package session

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func boardingTestFile(t *testing.T) stateFile {
	t.Helper()
	shared, err := NewWithProject(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	file := sessionStateFile(t, shared)
	station := &file.Project.Network.Stations[0]
	station.Berths = append(slices.Clone(station.Berths), sim.Berth{ID: "second-source-berth", Node: station.Berths[0].Node})
	other := file.Project.Network.Stations[1]
	file.Simulation.Pods = []sim.SavedPod{{
		ID: "recorded", Activity: "boarding", RiddenMeters: 100, JourneyOrigin: station.Berths[0].ID,
		Riders: []sim.SavedRequest{
			{ID: 1, From: station.ID, To: other.ID, PartySize: 2, SharingConsent: sim.SharedConsent, Completed: true},
			{ID: 2, From: other.ID, To: station.ID, PartySize: 3, SharingConsent: sim.SharedConsent},
		},
		Boardings: []sim.RiderBoarding{{BerthID: station.Berths[len(station.Berths)-1].ID, MetersAtBoarding: 0}, {BerthID: other.Berths[0].ID, MetersAtBoarding: 60}},
	}}
	return file
}

func TestBoardingStateRoundTrip(t *testing.T) {
	t.Parallel()
	file := boardingTestFile(t)
	data := encodeTestState(t, file)
	raw := decompressTestJSON(t, data)
	if bytes.Contains(raw, []byte(`"journeyOrigin"`)) || !bytes.Contains(raw, []byte(`"boardings":[[1,0],[0,60]]`)) || bytes.Contains(raw, []byte(`"MetersAtBoarding"`)) {
		t.Fatalf("source-bound tuple encoding missing: %s", raw)
	}
	decoded, err := decodeStateFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Simulation.Pods[0].Boardings) != 0 || len(decoded.boardingTuples) != 1 {
		t.Fatal("decode published unresolved native records")
	}
	if err := decoded.resolveBoardings(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(decoded.Simulation.Pods[0].Boardings, file.Simulation.Pods[0].Boardings) {
		t.Fatal("saved source records changed")
	}
	if decoded.boardingTuples != nil {
		t.Fatal("resolved tuples were retained")
	}
	if !bytes.Equal(raw, decompressTestJSON(t, encodeTestState(t, decoded))) {
		t.Fatal("source-bound save changed on reencode")
	}
}

func TestBoardingStateOrdinaryBytes(t *testing.T) {
	t.Parallel()
	file := newTestStateFile(t)
	want, err := json.Marshal(file, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if got := decompressTestJSON(t, encodeTestState(t, file)); !bytes.Equal(got, want) {
		t.Fatal("ordinary save encoding changed")
	}
}

func TestBoardingStateMalformed(t *testing.T) {
	t.Parallel()
	file := boardingTestFile(t)
	raw := decompressTestJSON(t, encodeTestState(t, file))
	tests := []struct {
		name, replacement string
	}{
		{"null", `null`}, {"empty", `[]`}, {"short", `[[0,0]]`},
		{"long", `[[0,0],[0,0],[0,0]]`}, {"null tuple", `[null,[0,60]]`},
		{"short tuple", `[[0],[0,60]]`}, {"long tuple", `[[0,0,0],[0,60]]`},
		{"null index", `[[null,0],[0,60]]`}, {"null baseline", `[[0,null],[0,60]]`},
		{"fractional index", `[[0.5,0],[0,60]]`}, {"negative index", `[[-1,0],[0,60]]`},
		{"index overflow", `[[999999999999999999999,0],[0,60]]`},
		{"negative baseline", `[[0,-1],[0,60]]`}, {"nonfinite baseline", `[[0,1e999],[0,60]]`},
		{"string baseline", `[[0,"0"],[0,60]]`}, {"object tuple", `[{"index":0,"meters":0},[0,60]]`},
		{"unknown member", `[[0,0],[0,60]],"unknown":0`},
		{"duplicate member", `[[0,0],[0,60]],"boardings":[[0,0],[0,60]]`},
		{"origin", `[[0,0],[0,60]],"journeyOrigin":""`},
		{"null origin", `[[0,0],[0,60]],"journeyOrigin":null`},
		{"closed cohort", `[[0,0],[0,60]],"legacyCohort":true`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			changed := bytes.Replace(raw, []byte(`[[1,0],[0,60]]`), []byte(tc.replacement), 1)
			if _, err := decodeStateFile(compressTestJSON(t, changed)); err == nil {
				t.Fatal("invalid boarding shape accepted")
			}
		})
	}
}

func TestBoardingStateLimits(t *testing.T) {
	t.Parallel()
	limits := boardingStateLimits(stateJSONLimits)
	for _, raw := range []string{
		`{"simulation":{"pods":[{"boardings":[[0,0,0]]}]}}`,
		`{"simulation":{"pods":[{"boardings":[[0,0],[0,0],[0,0],[0,0],[0,0],[0,0],[0,0],[0,0],[0,0]]}]}}`,
	} {
		if err := prescanJSON([]byte(raw), limits); !errors.Is(err, errJSONArrayTooLong) {
			t.Fatalf("boarding prescan bound missing: %v", err)
		}
	}
	if err := prescanJSON([]byte(`{"other":[[0,0,0]]}`), limits); err != nil {
		t.Fatal("tuple limit affected another path", err)
	}
}

func TestBoardingStateNativeGuards(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*sim.SavedPod)
	}{
		{"alignment", func(p *sim.SavedPod) { p.Riders = p.Riders[:1] }},
		{"closed cohort", func(p *sim.SavedPod) { p.LegacyCohort = true }},
		{"source ownership", func(p *sim.SavedPod) { p.Boardings[0].BerthID = p.Boardings[1].BerthID }},
		{"unknown source", func(p *sim.SavedPod) { p.Riders[0].From = "unknown" }},
		{"baseline above C", func(p *sim.SavedPod) { p.Boardings[1].MetersAtBoarding = 101 }},
		{"negative baseline", func(p *sim.SavedPod) { p.Boardings[1].MetersAtBoarding = -1 }},
		{"nonfinite baseline", func(p *sim.SavedPod) { p.Boardings[1].MetersAtBoarding = math.Inf(1) }},
		{"private", func(p *sim.SavedPod) { p.Riders[1].SharingConsent = sim.PrivateConsent }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := boardingTestFile(t)
			tc.change(&file.Simulation.Pods[0])
			if _, err := new(stateEncoder).encode(file); err == nil {
				t.Fatal("invalid native records reached save publication")
			}
		})
	}
}

func TestBoardingStatePhases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, activity       string
		completed            bool
		base, distance, want float64
		invalid              bool
	}{
		{"active travel", "traveling", false, 100, 25, 125, false},
		{"boarding", "boarding", false, 100, 0, 100, false},
		{"unloading", "unloading", false, 100, 0, 100, false},
		{"continuing", "continuing", false, 100, 0, 100, false},
		{"idle history", "idle", true, 100, 0, 100, false},
		{"empty departure", "departing", true, 100, 0, 100, false},
		{"empty travel", "traveling", true, 100, 999, 100, false},
		{"overflow", "traveling", false, math.MaxFloat64, math.MaxFloat64, 0, true},
		{"negative base", "idle", true, -1, 0, 0, true},
		{"nonfinite base", "idle", true, math.Inf(1), 0, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pod := sim.SavedPod{Activity: tc.activity, RiddenMeters: tc.base, Distance: tc.distance, Riders: []sim.SavedRequest{{Completed: tc.completed}}}
			got, err := savedPassengerMeters(pod)
			if (err != nil) != tc.invalid || err == nil && got != tc.want {
				t.Fatalf("meters %g, error %v", got, err)
			}
		})
	}
}

func TestBoardingStateLoadSourceAndFallback(t *testing.T) {
	t.Parallel()
	file := boardingTestFile(t)
	file.RestoreAttempts = 0
	sentinel := errors.New("native restore spy")
	shared, err := NewWithProject(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, logical := range []bool{false, true} {
		t.Run(map[bool]string{false: "physical", true: "logical"}[logical], func(t *testing.T) {
			t.Parallel()
			file := file
			if logical {
				file.RestoreAttempts = restoreLoopAttempts - 1
			}
			data := encodeTestState(t, file)
			calls := 0
			steps := restoreSteps{validateProject: func(project.Config) error { return nil }, restoreSimulation: func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
				calls++
				if !input.BoardingRecords || input.LogicalOnly != logical || !reflect.DeepEqual(input.State.Pods[0].Boardings, file.Simulation.Pods[0].Boardings) {
					t.Error("source-bound native restore input changed")
				}
				return nil, sim.RestoreResult{}, sentinel
			}}
			if _, err := shared.loadState(loadInput{data: data, steps: steps}); !errors.Is(err, sentinel) || calls != 1 {
				t.Fatalf("native input spy: calls=%d error=%v", calls, err)
			}
			selected := project.Clone(file.Project)
			selected.Network.Stations[0].Berths[0], selected.Network.Stations[0].Berths[1] = selected.Network.Stations[0].Berths[1], selected.Network.Stations[0].Berths[0]
			calls = 0
			if _, err := shared.loadState(loadInput{data: data, project: &selected, steps: steps}); err == nil || calls != 0 {
				t.Fatal("reordered caller project reached native restore")
			}
			for _, replacement := range []string{`[[99,0],[0,60]]`, `[[1,0],[0,101]]`} {
				raw := bytes.Replace(decompressTestJSON(t, data), []byte(`[[1,0],[0,60]]`), []byte(replacement), 1)
				calls = 0
				if _, err := shared.loadState(loadInput{data: compressTestJSON(t, raw), steps: steps}); err == nil || calls != 0 {
					t.Fatal("invalid reference reached native fallback")
				}
			}
		})
	}
}

func TestBoardingStateDuplicatePodPositions(t *testing.T) {
	t.Parallel()
	file := boardingTestFile(t)
	second := file.Simulation.Pods[0]
	second.Boardings = slices.Clone(second.Boardings)
	second.Boardings[0].MetersAtBoarding = 30
	file.Simulation.Pods = append(file.Simulation.Pods, second)
	decoded, err := decodeStateFile(encodeTestState(t, file))
	if err != nil {
		t.Fatal(err)
	}
	if err := decoded.resolveBoardings(); err != nil {
		t.Fatal(err)
	}
	if decoded.Simulation.Pods[0].Boardings[0].MetersAtBoarding != 0 || decoded.Simulation.Pods[1].Boardings[0].MetersAtBoarding != 30 {
		t.Fatal("duplicate pod IDs overwrote position-aligned tuples")
	}
}

func TestBoardingStateConsent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		completed bool
		consent   sim.SharingConsent
		valid     bool
	}{
		{"active private", false, sim.PrivateConsent, false},
		{"active shared", false, sim.SharedConsent, true},
		{"completed private", true, sim.PrivateConsent, true},
		{"completed shared", true, sim.SharedConsent, true},
		{"completed unknown", true, sim.LegacyUnknownConsent, false},
		{"active unknown", false, sim.LegacyUnknownConsent, false},
		{"completed missing", true, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := boardingTestFile(t)
			rider := &file.Simulation.Pods[0].Riders[0]
			rider.Completed, rider.SharingConsent = tc.completed, tc.consent
			data, encodeErr := new(stateEncoder).encode(file)
			if (encodeErr == nil) != tc.valid {
				t.Fatalf("native save consent: valid=%t error=%v", tc.valid, encodeErr)
			}
			if tc.valid {
				decoded, decodeErr := decodeStateFile(data)
				if decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if err := decoded.resolveBoardings(); err != nil {
					t.Fatal("source-bound consent restore", err)
				}
				if decoded.Simulation.Pods[0].Riders[0] != *rider || !slices.Equal(decoded.Simulation.Pods[0].Boardings, file.Simulation.Pods[0].Boardings) {
					t.Fatal("source-bound round trip changed consent, completion, or records")
				}
			}
			// Construct invalid input without the native encoder's consent check.
			pristine := boardingTestFile(t)
			pristine.Simulation.Pods[0].Riders[0].Completed = tc.completed
			raw := decompressTestJSON(t, encodeTestState(t, pristine))
			consent, err := json.Marshal(tc.consent)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte(`"sharingConsent":"shared"`), append([]byte(`"sharingConsent":`), consent...), 1)
			decoded, err := decodeStateFile(compressTestJSON(t, raw))
			if err != nil {
				if tc.valid {
					t.Fatal(err)
				}
				return
			}
			if err := decoded.resolveBoardings(); (err == nil) != tc.valid {
				t.Fatalf("saved consent: valid=%t error=%v", tc.valid, err)
			}
			if tc.valid && (decoded.Simulation.Pods[0].Riders[0] != *rider || !slices.Equal(decoded.Simulation.Pods[0].Boardings, file.Simulation.Pods[0].Boardings)) {
				t.Fatal("decoded consent or saved source records changed")
			}
		})
	}
}
