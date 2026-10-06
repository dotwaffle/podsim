package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// emergencyProject returns the default project with the incident marker,
// the emergency marker, and emergencies.
func emergencyProject(emergencies EmergencyConfig) Config {
	config := Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.EmergencyContract = EmergencyV1Contract
	config.Emergencies = &emergencies
	return config
}

// TestEmergencyMarkerRoundTrip checks that the emergency marker and the
// emergencies settings survive a project round trip, that an absent rate
// stays absent, and that a project without the marker has no emergency
// member.
func TestEmergencyMarkerRoundTrip(t *testing.T) {
	t.Parallel()
	plain, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte("emergenc")) {
		t.Fatal("unmarked project has an emergency member")
	}
	for name, test := range map[string]struct {
		emergencies EmergencyConfig
		encoded     string
	}{
		"empty":  {EmergencyConfig{}, `"emergencies":{}`},
		"zero":   {EmergencyConfig{PerHour: new(0.0)}, `"emergencies":{"perHour":0}`},
		"widest": {widestEmergencies, `"emergencies":{"perHour":-0}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := emergencyProject(test.emergencies)
			if err := Validate(config); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte(`"emergencyContract":"emergency-v1"`)) || !bytes.Contains(raw, []byte(test.encoded)) {
				t.Fatalf("members missing: %s", raw)
			}
			var got Config
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, config) {
				t.Fatalf("project changed in a round trip: %s", raw)
			}
		})
	}
}

// TestEmergencyMarkerDecodeRefusals checks the raw rules of the emergency
// marker and of emergencies. The decoder refuses a marker other than
// emergency-v1, also null and empty text. It refuses emergencies that are
// null or that are not an object, emergencies without the marker, also as
// {}, a null member, an unknown member, a member of the wrong type, and a
// rate above 0. A failed decode does not change the project. The control
// members decode, also with the fault marker and without it.
func TestEmergencyMarkerDecodeRefusals(t *testing.T) {
	t.Parallel()
	const marked = `,"incidentContract":"incident-v1","emergencyContract":"emergency-v1"`
	for _, extra := range []string{
		marked + `,"emergencies":{}`,
		marked + `,"emergencies":{"perHour":0}`,
		marked + `,"emergencies":{"perHour":-0}`,
		marked + `,"faultContract":"fault-v1","faults":{},"emergencies":{}`,
	} {
		var got Config
		if err := json.Unmarshal(rawProject(t, extra), &got); err != nil {
			t.Fatalf("control %s: %v", extra, err)
		}
	}
	for _, extra := range []string{
		`,"incidentContract":"incident-v1","emergencyContract":null,"emergencies":{}`,
		`,"incidentContract":"incident-v1","emergencyContract":"","emergencies":{}`,
		`,"incidentContract":"incident-v1","emergencyContract":"emergency-v2","emergencies":{}`,
		`,"incidentContract":"incident-v1","emergencyContract":"Emergency-v1","emergencies":{}`,
		`,"incidentContract":"incident-v1","emergencyContract":1,"emergencies":{}`,
		`,"emergencyContract":null`,
		`,"emergencyContract":""`,
		`,"incidentContract":"incident-v1","emergencyContract":null`,
		`,"emergencyContract":"emergency-v1","emergencies":{}`,
		marked,
		marked + `,"emergencies":null`,
		marked + `,"emergencies":[]`,
		marked + `,"emergencies":0`,
		`,"incidentContract":"incident-v1","emergencies":{}`,
		`,"incidentContract":"incident-v1","emergencies":null`,
		`,"emergencies":{"perHour":0}`,
		marked + `,"emergencies":{"perHour":null}`,
		marked + `,"emergencies":{"rate":0}`,
		marked + `,"emergencies":{"PerHour":0}`,
		marked + `,"emergencies":{"perHour":"0"}`,
		marked + `,"emergencies":{"perHour":1}`,
		marked + `,"emergencies":{"perHour":-1}`,
	} {
		got := Default()
		before := Clone(got)
		if err := json.Unmarshal(rawProject(t, extra), &got); err == nil {
			t.Errorf("decode accepted %s", extra)
		}
		if !reflect.DeepEqual(before, got) {
			t.Errorf("failed decode of %s changed the project", extra)
		}
	}
}

// TestEmergencyMarkerRules checks the typed rules of the emergency marker
// and of emergencies: the marker needs the incident marker and
// emergencies, emergencies need the marker, the marker must be
// emergency-v1, and the rate must be 0. The marker does not need the fault
// marker.
func TestEmergencyMarkerRules(t *testing.T) {
	t.Parallel()
	noIncident := emergencyProject(EmergencyConfig{})
	noIncident.IncidentContract = ""
	noEmergencies := emergencyProject(EmergencyConfig{})
	noEmergencies.Emergencies = nil
	unmarked := Default()
	unmarked.IncidentContract = sim.IncidentV1Contract
	unmarked.Emergencies = &EmergencyConfig{}
	unknown := emergencyProject(EmergencyConfig{})
	unknown.EmergencyContract = "emergency-v2"
	for name, test := range map[string]struct {
		config Config
		text   string
	}{
		"marker without the incident marker": {noIncident, "requires incidentContract"},
		"marker without emergencies":         {noEmergencies, "requires emergencies"},
		"emergencies without the marker":     {unmarked, "emergencies require emergencyContract"},
		"unknown marker":                     {unknown, "emergency contract must be emergency-v1"},
		"rate above 0":                       {emergencyProject(EmergencyConfig{PerHour: new(math.SmallestNonzeroFloat64)}), "perHour must be 0"},
		"negative rate":                      {emergencyProject(EmergencyConfig{PerHour: new(-1.0)}), "perHour must be 0"},
		"rate not a number":                  {emergencyProject(EmergencyConfig{PerHour: new(math.NaN())}), "perHour must be 0"},
	} {
		if err := Validate(test.config); err == nil || !strings.Contains(err.Error(), test.text) {
			t.Errorf("%s: Validate error %v, want %q", name, err, test.text)
		}
	}
	both := emergencyProject(EmergencyConfig{PerHour: new(math.Copysign(0, -1))})
	both.FaultContract, both.Faults = FaultV1Contract, &FaultConfig{}
	for _, config := range []Config{emergencyProject(EmergencyConfig{PerHour: new(0.0)}), both} {
		if err := Validate(config); err != nil {
			t.Errorf("Validate refused a valid marked project: %v", err)
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
	for _, value := range []*float64{nil, new(0.0), new(math.Copysign(0, -1))} {
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
// project with the emergency marker with the widest emergencies settings.
// Thus a change of the settings to any valid value keeps the project in
// MaxFileBytes.
func TestValidateMeasuresWidestEmergencies(t *testing.T) {
	t.Parallel()
	reserve := len(canonicalJSON(t, widestEmergencies)) - len(canonicalJSON(t, EmergencyConfig{}))
	if reserve <= 0 {
		t.Fatalf("the widest emergencies reserve %d bytes", reserve)
	}
	limit := MaxFileBytes - demandReserve(t) - reserve
	marked := func(size int) Config {
		config := weightedProject()
		config.IncidentContract, config.EmergencyContract, config.Emergencies = sim.IncidentV1Contract, EmergencyV1Contract, &EmergencyConfig{}
		return withEncodedSize(t, config, size)
	}
	if err := Validate(marked(limit)); err != nil {
		t.Fatalf("project at the limit: %v", err)
	}
	if err := Validate(marked(limit + 1)); !errors.Is(err, errTooLarge) {
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

// TestConfigureEmergencies checks that ConfigureExperiments turns
// emergencies on for a project with the emergency marker, also with the
// fault marker, and off for a project without it. Pod 01 boards a rider
// at Harbor, and then an emergency command starts or is refused. While a
// record is active, ConfigureExperiments refuses a project without the
// marker.
func TestConfigureEmergencies(t *testing.T) {
	t.Parallel()
	incident := Default()
	incident.IncidentContract = sim.IncidentV1Contract
	both := emergencyProject(EmergencyConfig{})
	both.FaultContract, both.Faults = FaultV1Contract, &FaultConfig{}
	for name, test := range map[string]struct {
		config Config
		on     bool
	}{
		"unmarked":        {Default(), false},
		"incident marker": {incident, false},
		"marked":          {emergencyProject(EmergencyConfig{}), true},
		"with faults":     {both, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			simulation, err := sim.NewFleetWithContracts(test.config.Network, test.config.Fleet, sim.FleetContracts{IncidentContract: test.config.IncidentContract})
			if err != nil {
				t.Fatal(err)
			}
			if err = ConfigureExperiments(simulation, test.config); err != nil {
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
			if err = ConfigureExperiments(simulation, incident); err == nil {
				t.Fatal("ConfigureExperiments turned emergencies off with an active record")
			}
		})
	}
}
