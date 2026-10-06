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

// faultProject returns the default project with the incident marker, the
// fault marker, and faults.
func faultProject(faults FaultConfig) Config {
	config := Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.FaultContract = FaultV1Contract
	config.Faults = &faults
	return config
}

// rawProject returns the default project JSON with the members of extra
// added at the root. extra starts with a comma.
func rawProject(t *testing.T, extra string) []byte {
	t.Helper()
	base, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	return append(bytes.TrimSuffix(base, []byte("}")), []byte(extra+"}")...)
}

// TestFaultMarkerRoundTrip checks that the fault marker and each faults
// member survive a project round trip, that an absent member stays
// absent, and that a project without the marker has no fault member.
func TestFaultMarkerRoundTrip(t *testing.T) {
	t.Parallel()
	plain, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte("fault")) {
		t.Fatal("unmarked project has a fault member")
	}
	for name, faults := range map[string]FaultConfig{
		"empty":  {},
		"widest": widestFaults,
		"zero":   {EvacuationSeconds: new(0), PerHour: new(0.0), DebrisShare: new(0.0), DebrisMeters: new(2.0), Duration: &FaultDuration{Kind: "fixed", Seconds: new(600)}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := faultProject(faults)
			if err := Validate(config); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte(`"faultContract":"fault-v1"`)) {
				t.Fatal("marker omitted")
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

// TestFaultMarkerDecodeRefusals checks the raw rules of the fault marker
// and of faults. The decoder refuses a marker other than fault-v1, also
// null and empty text. It refuses faults that are null or that are not an
// object, faults without the marker, also as {}, a null member at any
// depth, an unknown member, and a member of the wrong type. A failed
// decode does not change the project. The control members decode.
func TestFaultMarkerDecodeRefusals(t *testing.T) {
	t.Parallel()
	const marked = `,"incidentContract":"incident-v1","faultContract":"fault-v1"`
	for _, extra := range []string{
		marked + `,"faults":{}`,
		marked + `,"faults":{"evacuationSeconds":0,"perHour":0,"debrisShare":1,"debrisMeters":50,"duration":{"kind":"uniform","minSeconds":1,"maxSeconds":86400}}`,
	} {
		var got Config
		if err := json.Unmarshal(rawProject(t, extra), &got); err != nil {
			t.Fatalf("control %s: %v", extra, err)
		}
	}
	for _, extra := range []string{
		`,"incidentContract":"incident-v1","faultContract":null,"faults":{}`,
		`,"incidentContract":"incident-v1","faultContract":"","faults":{}`,
		`,"incidentContract":"incident-v1","faultContract":"fault-v2","faults":{}`,
		`,"incidentContract":"incident-v1","faultContract":"Fault-v1","faults":{}`,
		`,"incidentContract":"incident-v1","faultContract":1,"faults":{}`,
		`,"faultContract":null`,
		`,"faultContract":""`,
		`,"incidentContract":"incident-v1","faultContract":null`,
		`,"faultContract":"fault-v1","faults":{}`,
		marked,
		marked + `,"faults":null`,
		marked + `,"faults":[]`,
		marked + `,"faults":0`,
		`,"incidentContract":"incident-v1","faults":{}`,
		`,"incidentContract":"incident-v1","faults":null`,
		`,"faults":{"evacuationSeconds":300}`,
		marked + `,"faults":{"evacuationSeconds":null}`,
		marked + `,"faults":{"perHour":null}`,
		marked + `,"faults":{"debrisShare":null}`,
		marked + `,"faults":{"debrisMeters":null}`,
		marked + `,"faults":{"duration":null}`,
		marked + `,"faults":{"duration":{"kind":null}}`,
		marked + `,"faults":{"duration":{"kind":"fixed","seconds":null}}`,
		marked + `,"faults":{"duration":{"kind":"fixed","seconds":600,"minSeconds":null}}`,
		marked + `,"faults":{"rate":0}`,
		marked + `,"faults":{"duration":{"kind":"fixed","seconds":600,"extra":1}}`,
		marked + `,"faults":{"EvacuationSeconds":300}`,
		marked + `,"faults":{"evacuationSeconds":1.5}`,
		marked + `,"faults":{"evacuationSeconds":"300"}`,
		marked + `,"faults":{"perHour":1}`,
		marked + `,"faults":{"duration":{"kind":"fixed","seconds":600,"minSeconds":0}}`,
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

// TestFaultSettingsBounds checks each typed rule of the fault marker and
// of faults at its limit and one step past it.
func TestFaultSettingsBounds(t *testing.T) {
	t.Parallel()
	fixed := func(seconds int) *FaultDuration { return &FaultDuration{Kind: "fixed", Seconds: &seconds} }
	uniform := func(low, high int) *FaultDuration {
		return &FaultDuration{Kind: "uniform", MinSeconds: &low, MaxSeconds: &high}
	}
	exponential := func(low, mean, high int) *FaultDuration {
		return &FaultDuration{Kind: "exponential", MinSeconds: &low, MeanSeconds: &mean, MaxSeconds: &high}
	}
	tests := []struct {
		name   string
		faults FaultConfig
		ok     bool
	}{
		{"no evacuation delay", FaultConfig{EvacuationSeconds: new(0)}, true},
		{"negative evacuation delay", FaultConfig{EvacuationSeconds: new(-1)}, false},
		{"longest evacuation delay", FaultConfig{EvacuationSeconds: new(3600)}, true},
		{"evacuation delay past the limit", FaultConfig{EvacuationSeconds: new(3601)}, false},
		{"no rate", FaultConfig{PerHour: new(0.0)}, true},
		{"negative zero rate", FaultConfig{PerHour: new(math.Copysign(0, -1))}, true},
		{"rate above 0", FaultConfig{PerHour: new(math.SmallestNonzeroFloat64)}, false},
		{"negative rate", FaultConfig{PerHour: new(-1.0)}, false},
		{"rate not a number", FaultConfig{PerHour: new(math.NaN())}, false},
		{"no debris", FaultConfig{DebrisShare: new(0.0)}, true},
		{"negative debris share", FaultConfig{DebrisShare: new(-math.SmallestNonzeroFloat64)}, false},
		{"only debris", FaultConfig{DebrisShare: new(1.0)}, true},
		{"debris share past 1", FaultConfig{DebrisShare: new(math.Nextafter(1, 2))}, false},
		{"debris share not a number", FaultConfig{DebrisShare: new(math.NaN())}, false},
		{"shortest debris", FaultConfig{DebrisMeters: new(0.5)}, true},
		{"debris below the limit", FaultConfig{DebrisMeters: new(math.Nextafter(0.5, 0))}, false},
		{"longest debris", FaultConfig{DebrisMeters: new(50.0)}, true},
		{"debris past the limit", FaultConfig{DebrisMeters: new(math.Nextafter(50, 51))}, false},
		{"zero debris length", FaultConfig{DebrisMeters: new(0.0)}, false},
		{"debris length not a number", FaultConfig{DebrisMeters: new(math.NaN())}, false},
		{"fixed shortest", FaultConfig{Duration: fixed(1)}, true},
		{"fixed zero", FaultConfig{Duration: fixed(0)}, false},
		{"fixed longest", FaultConfig{Duration: fixed(86400)}, true},
		{"fixed past the limit", FaultConfig{Duration: fixed(86401)}, false},
		{"fixed without seconds", FaultConfig{Duration: &FaultDuration{Kind: "fixed"}}, false},
		{"fixed with a minimum", FaultConfig{Duration: &FaultDuration{Kind: "fixed", Seconds: new(1), MinSeconds: new(1)}}, false},
		{"fixed with a maximum", FaultConfig{Duration: &FaultDuration{Kind: "fixed", Seconds: new(1), MaxSeconds: new(1)}}, false},
		{"fixed with a mean", FaultConfig{Duration: &FaultDuration{Kind: "fixed", Seconds: new(1), MeanSeconds: new(1)}}, false},
		{"uniform full range", FaultConfig{Duration: uniform(1, 86400)}, true},
		{"uniform one value", FaultConfig{Duration: uniform(5, 5)}, true},
		{"uniform minimum past the maximum", FaultConfig{Duration: uniform(6, 5)}, false},
		{"uniform zero minimum", FaultConfig{Duration: uniform(0, 5)}, false},
		{"uniform maximum past the limit", FaultConfig{Duration: uniform(1, 86401)}, false},
		{"uniform without a maximum", FaultConfig{Duration: &FaultDuration{Kind: "uniform", MinSeconds: new(1)}}, false},
		{"uniform with seconds", FaultConfig{Duration: &FaultDuration{Kind: "uniform", Seconds: new(1), MinSeconds: new(1), MaxSeconds: new(2)}}, false},
		{"uniform with a mean", FaultConfig{Duration: &FaultDuration{Kind: "uniform", MinSeconds: new(1), MaxSeconds: new(2), MeanSeconds: new(1)}}, false},
		{"exponential mean at the minimum", FaultConfig{Duration: exponential(10, 10, 20)}, true},
		{"exponential mean at the maximum", FaultConfig{Duration: exponential(10, 20, 20)}, true},
		{"exponential mean below the minimum", FaultConfig{Duration: exponential(10, 9, 20)}, false},
		{"exponential mean past the maximum", FaultConfig{Duration: exponential(10, 21, 20)}, false},
		{"exponential minimum past the maximum", FaultConfig{Duration: exponential(21, 21, 20)}, false},
		{"exponential without a mean", FaultConfig{Duration: &FaultDuration{Kind: "exponential", MinSeconds: new(1), MaxSeconds: new(2)}}, false},
		{"exponential with seconds", FaultConfig{Duration: &FaultDuration{Kind: "exponential", Seconds: new(1), MinSeconds: new(1), MaxSeconds: new(1), MeanSeconds: new(1)}}, false},
		{"unknown kind", FaultConfig{Duration: &FaultDuration{Kind: "normal", Seconds: new(1)}}, false},
		{"empty kind", FaultConfig{Duration: &FaultDuration{Seconds: new(1)}}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(faultProject(test.faults)); (err == nil) != test.ok {
				t.Fatalf("Validate error %v, want accepted %t", err, test.ok)
			}
		})
	}
}

// TestFaultMarkerRules checks the rules between the markers and faults:
// the fault marker needs the incident marker and faults, faults need the
// marker, and the marker must be fault-v1.
func TestFaultMarkerRules(t *testing.T) {
	t.Parallel()
	noIncident := faultProject(FaultConfig{})
	noIncident.IncidentContract = ""
	noFaults := faultProject(FaultConfig{})
	noFaults.Faults = nil
	unmarked := Default()
	unmarked.IncidentContract = sim.IncidentV1Contract
	unmarked.Faults = &FaultConfig{}
	unknown := faultProject(FaultConfig{})
	unknown.FaultContract = "fault-v2"
	for name, test := range map[string]struct {
		config Config
		text   string
	}{
		"marker without the incident marker": {noIncident, "requires incidentContract"},
		"marker without faults":              {noFaults, "requires faults"},
		"faults without the marker":          {unmarked, "faults require faultContract"},
		"unknown marker":                     {unknown, "fault contract must be fault-v1"},
	} {
		if err := Validate(test.config); err == nil || !strings.Contains(err.Error(), test.text) {
			t.Errorf("%s: Validate error %v, want %q", name, err, test.text)
		}
	}
}

// TestWidestFaultsBoundsFaults checks that widestFaults is valid, and that
// no accepted faults settings of the examples below have a longer
// encoding. The examples hold the widest number of each range, the other
// duration kinds, and every member.
func TestWidestFaultsBoundsFaults(t *testing.T) {
	t.Parallel()
	if err := Validate(faultProject(widestFaults)); err != nil {
		t.Fatal(err)
	}
	widest := len(canonicalJSON(t, widestFaults))
	for _, faults := range []FaultConfig{
		{EvacuationSeconds: new(3600), PerHour: new(0.0), DebrisShare: new(9.999999999999999e-7), DebrisMeters: new(49.99999999999999),
			Duration: &FaultDuration{Kind: "uniform", MinSeconds: new(86400), MaxSeconds: new(86400)}},
		{EvacuationSeconds: new(3600), PerHour: new(math.Copysign(0, -1)), DebrisShare: new(0.9999999999999999), DebrisMeters: new(0.5000000000000001),
			Duration: &FaultDuration{Kind: "fixed", Seconds: new(86400)}},
		{EvacuationSeconds: new(1000), DebrisShare: new(1.2345678901234567e-06), DebrisMeters: new(12.345678901234567),
			Duration: &FaultDuration{Kind: "exponential", MinSeconds: new(10000), MaxSeconds: new(86400), MeanSeconds: new(12345)}},
	} {
		if err := Validate(faultProject(faults)); err != nil {
			t.Fatal(err)
		}
		if got := len(canonicalJSON(t, faults)); got > widest {
			t.Errorf("faults encode to %d bytes, more than %d: %s", got, widest, canonicalJSON(t, faults))
		}
	}
}

// TestWidestFaultsBoundsEachMember substitutes each accepted extreme or
// long value of one member into widestFaults, and checks that the result
// does not encode to more bytes than widestFaults. The widest settings
// with a rate of negative zero are one case, and a rate of positive zero
// is one byte shorter.
func TestWidestFaultsBoundsEachMember(t *testing.T) {
	t.Parallel()
	widest := len(canonicalJSON(t, widestFaults))
	clone := func() FaultConfig { return *cloneFaults(&widestFaults) }
	var variants []FaultConfig
	for _, value := range []*int{nil, new(0), new(1), new(999), new(maxFaultEvacuationSeconds)} {
		faults := clone()
		faults.EvacuationSeconds = value
		variants = append(variants, faults)
	}
	for _, value := range []*float64{nil, new(0.0), new(math.Copysign(0, -1))} {
		faults := clone()
		faults.PerHour = value
		variants = append(variants, faults)
	}
	for _, value := range []*float64{nil, new(0.0), new(math.Copysign(0, -1)), new(1.0), new(5e-324), new(1.2345678901234567e-07),
		new(9.999999999999999e-7), new(1.0000000000000002e-06), new(0.9999999999999999)} {
		faults := clone()
		faults.DebrisShare = value
		variants = append(variants, faults)
	}
	for _, value := range []*float64{nil, new(minDebrisMeters), new(0.5000000000000001), new(12.345678901234567), new(49.99999999999999), new(float64(maxDebrisMeters))} {
		faults := clone()
		faults.DebrisMeters = value
		variants = append(variants, faults)
	}
	for _, value := range []*FaultDuration{nil, {Kind: "fixed", Seconds: new(maxFaultSeconds)},
		{Kind: "uniform", MinSeconds: new(maxFaultSeconds), MaxSeconds: new(maxFaultSeconds)},
		{Kind: "exponential", MinSeconds: new(1), MaxSeconds: new(maxFaultSeconds), MeanSeconds: new(maxFaultSeconds)}} {
		faults := clone()
		faults.Duration = value
		variants = append(variants, faults)
	}
	for _, faults := range variants {
		if err := Validate(faultProject(faults)); err != nil {
			t.Fatalf("%s: %v", canonicalJSON(t, faults), err)
		}
		if got := len(canonicalJSON(t, faults)); got > widest {
			t.Errorf("faults encode to %d bytes, more than %d: %s", got, widest, canonicalJSON(t, faults))
		}
	}
	positive := clone()
	positive.PerHour = new(0.0)
	if got := len(canonicalJSON(t, positive)); got != widest-1 {
		t.Errorf("a rate of positive zero encodes to %d bytes, want %d", got, widest-1)
	}
}

// TestValidateMeasuresWidestFaults checks that Validate measures a project
// with the fault marker with the widest faults settings. Thus a change of
// the settings to any valid value keeps the project in MaxFileBytes.
func TestValidateMeasuresWidestFaults(t *testing.T) {
	t.Parallel()
	reserve := len(canonicalJSON(t, widestFaults)) - len(canonicalJSON(t, FaultConfig{}))
	limit := MaxFileBytes - demandReserve(t) - reserve
	marked := func(size int) Config {
		config := weightedProject()
		config.IncidentContract, config.FaultContract, config.Faults = sim.IncidentV1Contract, FaultV1Contract, &FaultConfig{}
		return withEncodedSize(t, config, size)
	}
	if err := Validate(marked(limit)); err != nil {
		t.Fatalf("project at the limit: %v", err)
	}
	if err := Validate(marked(limit + 1)); !errors.Is(err, errTooLarge) {
		t.Fatalf("project one byte past the limit: %v, want %v", err, errTooLarge)
	}
}

// TestCloneDetachesFaults checks that a clone shares no faults memory with
// its source: a change of each member of the clone keeps the source.
func TestCloneDetachesFaults(t *testing.T) {
	t.Parallel()
	build := func() Config {
		return faultProject(FaultConfig{EvacuationSeconds: new(10), PerHour: new(0.0), DebrisShare: new(0.5), DebrisMeters: new(2.0),
			Duration: &FaultDuration{Kind: "exponential", Seconds: new(1), MinSeconds: new(10), MaxSeconds: new(30), MeanSeconds: new(20)}})
	}
	source, want := build(), build()
	clone := Clone(source)
	if !reflect.DeepEqual(clone, source) {
		t.Fatal("clone differs from its source")
	}
	faults, duration := clone.Faults, clone.Faults.Duration
	*faults.EvacuationSeconds, *faults.PerHour, *faults.DebrisShare, *faults.DebrisMeters = 1, 1, 1, 1
	duration.Kind = "fixed"
	*duration.Seconds, *duration.MinSeconds, *duration.MaxSeconds, *duration.MeanSeconds = 2, 2, 2, 2
	if !reflect.DeepEqual(source, want) {
		t.Fatal("a change of the clone changed its source")
	}
}

// TestConfigureFaults checks that ConfigureExperiments turns faults on
// with the evacuation delay of a project with the fault marker, with the
// default delay for an absent member, and off for a project without the
// marker. Pod 01 boards a rider at Harbor, and then a fault command starts
// a fault on it or is refused. With no delay, the next tick evacuates the
// pod and interrupts the order. With the default delay of 300 seconds, it
// does not.
func TestConfigureFaults(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		config      Config
		on, evacuee bool
	}{
		"unmarked":      {Default(), false, false},
		"default delay": {faultProject(FaultConfig{}), true, false},
		"no delay":      {faultProject(FaultConfig{EvacuationSeconds: new(0)}), true, true},
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
			_, err = simulation.Fault(sim.FaultRequest{PodID: "01"})
			if (err == nil) != test.on {
				t.Fatalf("fault error %v, want faults on %t", err, test.on)
			}
			simulation.Step()
			if got := simulation.Snapshot().Interrupted; (got > 0) != test.evacuee {
				t.Fatalf("%d orders interrupted, want an evacuation %t", got, test.evacuee)
			}
		})
	}
}
