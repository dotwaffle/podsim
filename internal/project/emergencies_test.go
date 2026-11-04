package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// emergenciesReserve returns the bytes that the widest emergencies settings
// add to a project without an emergencies member.
func emergenciesReserve(t *testing.T) int {
	t.Helper()
	return len(`"emergencies":,`) + len(canonicalJSON(t, widestEmergencies))
}

// emergencyProject returns the default project with emergencies.
func emergencyProject(emergencies EmergencyConfig) Config {
	config := Default()
	config.Emergencies = &emergencies
	return config
}

// TestEmergenciesRoundTrip checks that the emergencies settings survive a
// project round trip, that an absent rate stays absent, and that a project
// without emergencies has no emergency member.
func TestEmergenciesRoundTrip(t *testing.T) {
	t.Parallel()
	plain, err := jsonv2.Marshal(Default(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte("emergenc")) {
		t.Fatal("a project without emergencies has an emergency member")
	}
	for name, test := range map[string]struct {
		emergencies EmergencyConfig
		encoded     string
	}{
		"empty":         {EmergencyConfig{}, `"emergencies":{}`},
		"zero":          {EmergencyConfig{PerHour: new(0.0)}, `"emergencies":{"perHour":0}`},
		"negative zero": {EmergencyConfig{PerHour: new(math.Copysign(0, -1))}, `"emergencies":{"perHour":-0}`},
		"highest":       {EmergencyConfig{PerHour: new(60.0)}, `"emergencies":{"perHour":60}`},
		"widest":        {widestEmergencies, `"emergencies":{"perHour":0.0000010000000000000002}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := emergencyProject(test.emergencies)
			if err := Validate(config); err != nil {
				t.Fatal(err)
			}
			raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte(test.encoded)) {
				t.Fatalf("members missing: %s", raw)
			}
			var got Config
			if err := jsonv2.Unmarshal(raw, &got, json.DefaultOptionsV1()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, config) {
				t.Fatalf("project changed in a round trip: %s", raw)
			}
		})
	}
}

// TestEmergenciesDecodeRefusals checks the raw rules of emergencies. The
// decoder refuses emergencies that are null or that are not an object, a
// null member, an unknown member, a member of the wrong type, and a rate
// above 0. A failed decode does not change the project. The control members
// decode, also with faults and without them.
func TestEmergenciesDecodeRefusals(t *testing.T) {
	t.Parallel()
	for _, extra := range []string{
		``,
		`,"emergencies":{}`,
		`,"emergencies":{"perHour":0}`,
		`,"emergencies":{"perHour":-0}`,
		`,"emergencies":{"perHour":1}`,
		`,"emergencies":{"perHour":60}`,
		`,"faults":{},"emergencies":{}`,
	} {
		var got Config
		if err := jsonv2.Unmarshal(rawProject(t, extra), &got, json.DefaultOptionsV1()); err != nil {
			t.Fatalf("control %s: %v", extra, err)
		}
	}
	for _, extra := range []string{
		`,"emergencies":null`,
		`,"emergencies":[]`,
		`,"emergencies":0`,
		`,"emergencies":{"perHour":null}`,
		`,"emergencies":{"rate":0}`,
		`,"emergencies":{"PerHour":0}`,
		`,"emergencies":{"perHour":"0"}`,
		`,"emergencies":{"perHour":61}`,
		`,"emergencies":{"perHour":-1}`,
	} {
		got := Default()
		before := Clone(got)
		if err := jsonv2.Unmarshal(rawProject(t, extra), &got, json.DefaultOptionsV1()); err == nil {
			t.Errorf("decode accepted %s", extra)
		}
		if !reflect.DeepEqual(before, got) {
			t.Errorf("failed decode of %s changed the project", extra)
		}
	}
}

// TestEmergencyRateRules checks the typed rule of emergencies: the rate is
// from 0 to 60. A rate above 0 needs nothing else.
func TestEmergencyRateRules(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		config Config
		text   string
	}{
		"rate past the limit": {emergencyProject(EmergencyConfig{PerHour: new(math.Nextafter(60, 61))}), "emergencies perHour must be 0 to 60"},
		"infinite rate":       {emergencyProject(EmergencyConfig{PerHour: new(math.Inf(1))}), "emergencies perHour must be 0 to 60"},
		"negative rate":       {emergencyProject(EmergencyConfig{PerHour: new(-math.SmallestNonzeroFloat64)}), "emergencies perHour must be 0 to 60"},
		"rate not a number":   {emergencyProject(EmergencyConfig{PerHour: new(math.NaN())}), "emergencies perHour must be 0 to 60"},
	} {
		if err := Validate(test.config); err == nil || !strings.Contains(err.Error(), test.text) {
			t.Errorf("%s: Validate error %v, want %q", name, err, test.text)
		}
	}
	both := emergencyProject(EmergencyConfig{PerHour: new(math.Copysign(0, -1))})
	both.Faults = &FaultConfig{}
	for _, config := range []Config{emergencyProject(EmergencyConfig{}), emergencyProject(EmergencyConfig{PerHour: new(0.0)}), both,
		emergencyProject(EmergencyConfig{PerHour: new(math.SmallestNonzeroFloat64)}), emergencyProject(EmergencyConfig{PerHour: new(60.0)})} {
		if err := Validate(config); err != nil {
			t.Errorf("Validate refused a valid project: %v", err)
		}
	}
}

// TestWidestEmergenciesBoundsEachValue checks that widestEmergencies is
// valid, and that no accepted rate encodes to more bytes.
func TestWidestEmergenciesBoundsEachValue(t *testing.T) {
	t.Parallel()
	if err := Validate(emergencyProject(widestEmergencies)); err != nil {
		t.Fatal(err)
	}
	widest := len(canonicalJSON(t, widestEmergencies))
	for _, value := range incidentRateVariants() {
		emergencies := EmergencyConfig{PerHour: value}
		if err := Validate(emergencyProject(emergencies)); err != nil {
			t.Fatal(err)
		}
		if got := len(canonicalJSON(t, emergencies)); got > widest {
			t.Errorf("emergencies encode to %d bytes, more than %d: %s", got, widest, canonicalJSON(t, emergencies))
		}
	}
}

// TestValidateMeasuresWidestEmergencies checks that Validate measures a
// project with the widest emergencies settings. Thus a change of the settings to any valid value keeps the project in
// MaxFileBytes.
func TestValidateMeasuresWidestEmergencies(t *testing.T) {
	t.Parallel()
	reserve := len(canonicalJSON(t, widestEmergencies)) - len(canonicalJSON(t, EmergencyConfig{}))
	if reserve <= 0 {
		t.Fatalf("the widest emergencies reserve %d bytes", reserve)
	}
	limit := MaxFileBytes - demandReserve(t) - reserve - faultsReserve(t)
	project := func(size int) Config {
		config := weightedProject()
		config.Emergencies = &EmergencyConfig{}
		return withEncodedSize(t, config, size)
	}
	if err := Validate(project(limit)); err != nil {
		t.Fatalf("project at the limit: %v", err)
	}
	if err := Validate(project(limit + 1)); !errors.Is(err, errTooLarge) {
		t.Fatalf("project one byte past the limit: %v, want %v", err, errTooLarge)
	}
}

// TestCloneDetachesEmergencies checks that a clone shares no emergencies
// memory with its source.
func TestCloneDetachesEmergencies(t *testing.T) {
	t.Parallel()
	source, want := emergencyProject(EmergencyConfig{PerHour: new(0.0)}), emergencyProject(EmergencyConfig{PerHour: new(0.0)})
	clone := Clone(source)
	if !reflect.DeepEqual(clone, source) {
		t.Fatal("clone differs from its source")
	}
	*clone.Emergencies.PerHour = 1
	clone.Emergencies.PerHour = nil
	if !reflect.DeepEqual(source, want) {
		t.Fatal("a change of the clone changed its source")
	}
}

// TestConfigureEmergencies checks that the operations argument of
// ConfigureExperiments turns emergencies on or off, whatever the settings of
// the project. Pod 01 boards a rider at Harbor, and then an emergency
// command starts or is refused. While a record is active,
// ConfigureExperiments refuses to turn the operations off.
func TestConfigureEmergencies(t *testing.T) {
	t.Parallel()
	both := emergencyProject(EmergencyConfig{})
	both.Faults = &FaultConfig{}
	for name, test := range map[string]struct {
		config Config
		on     bool
	}{
		"operations off":   {both, false},
		"no settings":      {Default(), true},
		"with emergencies": {emergencyProject(EmergencyConfig{}), true},
		"with faults":      {both, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			simulation, err := sim.NewFleet(test.config.Network, test.config.Fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err = ConfigureExperiments(simulation, test.config, test.on); err != nil {
				t.Fatal(err)
			}
			if err = simulation.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			for simulation.Snapshot().Vehicles[0].Pod.Activity != sim.Boarding {
				if simulation.Tick() > 600 {
					t.Fatal("pod 01 does not board")
				}
				simulation.Step()
			}
			_, err = simulation.Emergency("01", 0)
			if (err == nil) != test.on {
				t.Fatalf("emergency error %v, want emergencies on %t", err, test.on)
			}
			if !test.on {
				return
			}
			if err = ConfigureExperiments(simulation, test.config, false); err == nil {
				t.Fatal("ConfigureExperiments turned emergencies off with an active record")
			}
		})
	}
}
