package project

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// removedMarkers are the names of the markers that the project format no
// longer has.
var removedMarkers = []string{"incidentContract", "faultContract", "emergencyContract"}

// TestRemovedMarkersAreUnknown checks that the strict decode refuses each
// removed marker as an unknown member, whatever its value, in a whole
// project and in a partial update. A failed decode does not change the
// project.
func TestRemovedMarkersAreUnknown(t *testing.T) {
	t.Parallel()
	for _, name := range removedMarkers {
		for _, value := range []string{`null`, `""`, `"incident-v1"`, `"fault-v1"`, `"emergency-v1"`, `0`, `true`, `[]`, `{}`} {
			member := `"` + name + `":` + value
			for form, raw := range map[string][]byte{
				"whole":   rawProject(t, `,`+member),
				"partial": []byte(`{` + member + `}`),
			} {
				got := Default()
				before := Clone(got)
				err := jsonv2.Unmarshal(raw, &got, json.DefaultOptionsV1())
				if err == nil || !strings.Contains(err.Error(), `unknown field "`+name+`"`) {
					t.Errorf("%s, %s: %v", form, member, err)
				}
				if !reflect.DeepEqual(before, got) {
					t.Errorf("%s, %s: a failed decode changed the project", form, member)
				}
			}
		}
	}
}

// TestProjectDecodeRefusalOrder pins the order of the checks of decodeJSON
// with inputs that have more than one error: the null scan of a settings
// member, the strict decode of a removed marker, the version, the faults,
// and the emergencies.
func TestProjectDecodeRefusalOrder(t *testing.T) {
	t.Parallel()
	const unknownFault = `unknown field "faultContract"`
	for _, test := range []struct{ name, raw, want string }{
		{"null faults before an earlier marker", `{"faultContract":"fault-v1","faults":null}`, "faults must be an object"},
		{"null emergencies before an earlier marker", `{"incidentContract":1,"emergencies":null}`, "emergencies must be an object"},
		{"null faults member before a later marker", `{"faults":{"perHour":null},"faultContract":"x"}`, "faults members must not be null"},
		{"marker before the version", `{"version":99,"faultContract":"fault-v1"}`, unknownFault},
		{"marker before the faults range", `{"faults":{"perHour":61},"faultContract":"fault-v1"}`, unknownFault},
		{"version before the faults range", `{"version":99,"faults":{"perHour":61}}`, "project version must be 1"},
		{"faults range before the emergencies range", `{"emergencies":{"perHour":61},"faults":{"perHour":61}}`, "faults perHour must be 0 to 60"},
		{"emergencies range", `{"emergencies":{"perHour":61}}`, "emergencies perHour must be 0 to 60"},
	} {
		got := Default()
		err := jsonv2.Unmarshal([]byte(test.raw), &got, json.DefaultOptionsV1())
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v, want %q", test.name, err, test.want)
		}
	}
}

// TestProjectValidateRefusalOrder pins the order of the checks of Validate
// with inputs that have more than one error.
func TestProjectValidateRefusalOrder(t *testing.T) {
	t.Parallel()
	badFaults := &FaultConfig{PerHour: new(61.0)}
	badEmergencies := &EmergencyConfig{PerHour: new(61.0)}
	for _, test := range []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"version before the faults", func(c *Config) { c.Version, c.Faults = 99, badFaults }, "project version must be 1"},
		{"faults before the emergencies", func(c *Config) { c.Faults, c.Emergencies = badFaults, badEmergencies }, "faults perHour must be 0 to 60"},
		{"emergencies before the name", func(c *Config) { c.Emergencies, c.Name = badEmergencies, "" }, "emergencies perHour must be 0 to 60"},
	} {
		config := Default()
		test.edit(&config)
		if err := Validate(config); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v, want %q", test.name, err, test.want)
		}
	}
	config := Default()
	config.Faults, config.Emergencies = &FaultConfig{}, &EmergencyConfig{}
	if err := Validate(config); errors.Is(err, errTooLarge) || err != nil {
		t.Errorf("control: %v", err)
	}
}

// TestEffectiveFaultSettings checks the evacuation delay of a project: an
// absent faults member and an empty one have the default of 300 seconds,
// and an explicit 0 stays 0, also after a round trip of the project.
func TestEffectiveFaultSettings(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, extra string
		want        int
	}{
		{"absent", ``, 300},
		{"empty", `,"faults":{}`, 300},
		{"explicit 0", `,"faults":{"evacuationSeconds":0}`, 0},
		{"explicit 60", `,"faults":{"evacuationSeconds":60}`, 60},
	} {
		var config Config
		if err := jsonv2.Unmarshal(rawProject(t, test.extra), &config, json.DefaultOptionsV1()); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if got := EffectiveFaultSettings(config).EvacuationSeconds; got != test.want {
			t.Errorf("%s: evacuation %d, want %d", test.name, got, test.want)
		}
		raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		var again Config
		if err := jsonv2.Unmarshal(raw, &again, json.DefaultOptionsV1()); err != nil {
			t.Fatalf("%s: round trip: %v", test.name, err)
		}
		if !reflect.DeepEqual(config, again) {
			t.Errorf("%s: the project changed in a round trip", test.name)
		}
	}
}
