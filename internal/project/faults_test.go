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

// faultsReserve returns the bytes that the widest faults settings add to a
// project without a faults member.
func faultsReserve(t *testing.T) int {
	t.Helper()
	return len(`"faults":,`) + len(canonicalJSON(t, widestFaults))
}

// faultProject returns the default project with faults.
func faultProject(faults FaultConfig) Config {
	config := Default()
	config.Faults = &faults
	return config
}

// rawProject returns the default project JSON with the members of extra
// added at the root. extra starts with a comma.
func rawProject(t *testing.T, extra string) []byte {
	t.Helper()
	base, err := jsonv2.Marshal(Default(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	return append(bytes.TrimSuffix(base, []byte("}")), []byte(extra+"}")...)
}

// TestFaultsRoundTrip checks that each faults member survives a project
// round trip, that an absent member stays absent, and that a project
// without faults has no fault member.
func TestFaultsRoundTrip(t *testing.T) {
	t.Parallel()
	plain, err := jsonv2.Marshal(Default(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte("fault")) {
		t.Fatal("a project without faults has a fault member")
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
			raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
			if err != nil {
				t.Fatal(err)
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

// TestFaultsDecodeRefusals checks the raw rules of faults. The decoder
// refuses faults that are null or that are not an object, a null member at
// any depth, an unknown member, and a member of the wrong type. A failed
// decode does not change the project. The control members decode.
func TestFaultsDecodeRefusals(t *testing.T) {
	t.Parallel()
	for _, extra := range []string{
		``,
		`,"faults":{}`,
		`,"faults":{"evacuationSeconds":300}`,
		`,"faults":{"evacuationSeconds":0,"perHour":0,"debrisShare":1,"debrisMeters":50,"duration":{"kind":"uniform","minSeconds":1,"maxSeconds":86400}}`,
		`,"faults":{"perHour":60,"duration":{"kind":"fixed","seconds":600}}`,
		`,"faults":{"perHour":-0}`,
	} {
		var got Config
		if err := jsonv2.Unmarshal(rawProject(t, extra), &got, json.DefaultOptionsV1()); err != nil {
			t.Fatalf("control %s: %v", extra, err)
		}
	}
	for _, extra := range []string{
		`,"faults":null`,
		`,"faults":[]`,
		`,"faults":0`,
		`,"faults":{"evacuationSeconds":null}`,
		`,"faults":{"perHour":null}`,
		`,"faults":{"debrisShare":null}`,
		`,"faults":{"debrisMeters":null}`,
		`,"faults":{"duration":null}`,
		`,"faults":{"duration":{"kind":null}}`,
		`,"faults":{"duration":{"kind":"fixed","seconds":null}}`,
		`,"faults":{"duration":{"kind":"fixed","seconds":600,"minSeconds":null}}`,
		`,"faults":{"rate":0}`,
		`,"faults":{"duration":{"kind":"fixed","seconds":600,"extra":1}}`,
		`,"faults":{"EvacuationSeconds":300}`,
		`,"faults":{"evacuationSeconds":1.5}`,
		`,"faults":{"evacuationSeconds":"300"}`,
		`,"faults":{"perHour":1}`,
		`,"faults":{"perHour":61,"duration":{"kind":"fixed","seconds":600}}`,
		`,"faults":{"perHour":-1}`,
		`,"faults":{"duration":{"kind":"fixed","seconds":600,"minSeconds":0}}`,
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

// TestFaultSettingsBounds checks each typed rule of faults at its limit and one step past it.
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
		{"smallest rate and no duration", FaultConfig{PerHour: new(math.SmallestNonzeroFloat64)}, false},
		{"smallest rate", FaultConfig{PerHour: new(math.SmallestNonzeroFloat64), Duration: fixed(1)}, true},
		{"highest rate", FaultConfig{PerHour: new(60.0), Duration: fixed(1)}, true},
		{"rate past the limit", FaultConfig{PerHour: new(math.Nextafter(60, 61)), Duration: fixed(1)}, false},
		{"negative rate", FaultConfig{PerHour: new(-math.SmallestNonzeroFloat64), Duration: fixed(1)}, false},
		{"rate not a number", FaultConfig{PerHour: new(math.NaN()), Duration: fixed(1)}, false},
		{"infinite rate", FaultConfig{PerHour: new(math.Inf(1)), Duration: fixed(1)}, false},
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
	for _, value := range incidentRateVariants() {
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
		if value == nil {
			faults.PerHour = nil
		}
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
	// Decision 15 of the stage 7 contract: the widest rate takes 22 bytes more
	// than the 2 bytes of -0.
	negative := clone()
	negative.PerHour = new(math.Copysign(0, -1))
	if got := len(canonicalJSON(t, negative)); got != widest-22 {
		t.Errorf("a rate of -0 encodes to %d bytes, want %d", got, widest-22)
	}
}

// incidentRateVariants returns accepted rates, with each form that JSON
// writes in another number of bytes, and the rates around the point where
// JSON changes from decimal form to exponent form.
func incidentRateVariants() []*float64 {
	return []*float64{nil, new(0.0), new(math.Copysign(0, -1)), new(1.0), new(60.0), new(59.99999999999999),
		new(math.SmallestNonzeroFloat64), new(1.2345678901234567e-07), new(9.999999999999999e-7),
		new(1.0000000000000002e-06), new(1.0000000000000002e-05), new(0.9999999999999999)}
}

// TestValidateMeasuresWidestFaults checks that Validate measures a project
// with the widest faults settings. Thus a change of the settings to any
// valid value keeps the project in MaxFileBytes.
func TestValidateMeasuresWidestFaults(t *testing.T) {
	t.Parallel()
	reserve := len(canonicalJSON(t, widestFaults)) - len(canonicalJSON(t, FaultConfig{}))
	limit := MaxFileBytes - demandReserve(t) - reserve - emergenciesReserve(t)
	project := func(size int) Config {
		config := weightedProject()
		config.Faults = &FaultConfig{}
		return withEncodedSize(t, config, size)
	}
	if err := Validate(project(limit)); err != nil {
		t.Fatalf("project at the limit: %v", err)
	}
	if err := Validate(project(limit + 1)); !errors.Is(err, errTooLarge) {
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

// TestConfigureFaults checks that the operations argument of
// ConfigureExperiments turns faults on or off, with the evacuation delay of
// the project and the default delay for an absent member. Pod 01 boards a
// rider at Harbor, and then a fault command starts a fault on it or is
// refused. With no delay, the next tick evacuates the pod and interrupts the
// order. With the default delay of 300 seconds, it does not.
func TestConfigureFaults(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		config      Config
		on, evacuee bool
	}{
		"operations off": {faultProject(FaultConfig{EvacuationSeconds: new(0)}), false, false},
		"no faults":      {Default(), true, false},
		"default delay":  {faultProject(FaultConfig{}), true, false},
		"no delay":       {faultProject(FaultConfig{EvacuationSeconds: new(0)}), true, true},
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

// TestFaultSettingsRefusalOrder pins the order of the checks of the faults
// settings with inputs that have more than one error. The new duration check
// runs last, so each other error comes first.
func TestFaultSettingsRefusalOrder(t *testing.T) {
	t.Parallel()
	const needsDuration = "faults perHour above 0 needs a duration"
	badDuration := &FaultDuration{Kind: "none"}
	for _, test := range []struct {
		name   string
		faults FaultConfig
		want   string
	}{
		{"evacuation before the rate", FaultConfig{EvacuationSeconds: new(-1), PerHour: new(61.0)}, "faults evacuationSeconds must be 0 to 3600"},
		{"rate before the debris share", FaultConfig{PerHour: new(61.0), DebrisShare: new(2.0)}, "faults perHour must be 0 to 60"},
		{"rate not a number before the debris share", FaultConfig{PerHour: new(math.NaN()), DebrisShare: new(2.0)}, "faults perHour must be 0 to 60"},
		{"debris share before the debris length", FaultConfig{DebrisShare: new(2.0), DebrisMeters: new(0.0)}, "faults debrisShare must be 0 to 1"},
		{"debris length before the duration", FaultConfig{DebrisMeters: new(0.0), Duration: badDuration}, "faults debrisMeters must be 0.5 to 50"},
		{"debris share before a missing duration", FaultConfig{PerHour: new(1.0), DebrisShare: new(2.0)}, "faults debrisShare must be 0 to 1"},
		{"debris length before a missing duration", FaultConfig{PerHour: new(1.0), DebrisMeters: new(0.0)}, "faults debrisMeters must be 0.5 to 50"},
		{"a bad duration with a rate", FaultConfig{PerHour: new(1.0), Duration: badDuration}, `faults duration kind must be`},
		{"a missing duration", FaultConfig{PerHour: new(1.0)}, needsDuration},
		{"the smallest rate and a missing duration", FaultConfig{PerHour: new(math.SmallestNonzeroFloat64)}, needsDuration},
	} {
		err := Validate(faultProject(test.faults))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: %v, want %q", test.name, err, test.want)
		}
	}
	// A rate of 0 needs no duration, in any of its forms.
	for _, rate := range []*float64{nil, new(0.0), new(math.Copysign(0, -1))} {
		if err := Validate(faultProject(FaultConfig{PerHour: rate})); err != nil {
			t.Errorf("rate %v: %v", rate, err)
		}
	}
}

// TestValidateWidestRateBoundary checks exception 2 of section 5.1.5 of the
// incident stage 7 contract, for a project with rates of 0. Each group has the
// widest rate, which takes 22 bytes more than -0, so the cap moves by 44 bytes.
// The test measures from -0, so the widest settings of the package do not
// decide the limit. A project at the cap with the widest rate in each group
// encodes within the cap.
func TestValidateWidestRateBoundary(t *testing.T) {
	t.Parallel()
	const moved = 44
	withRate := func(rate float64) (FaultConfig, EmergencyConfig) {
		faults := *cloneFaults(&widestFaults)
		faults.PerHour = new(rate)
		return faults, EmergencyConfig{PerHour: new(rate)}
	}
	faults, emergencies := withRate(math.Copysign(0, -1))
	limit := MaxFileBytes - demandReserve(t) - len(`"faults":,`) - len(canonicalJSON(t, faults)) -
		len(`"emergencies":,`) - len(canonicalJSON(t, emergencies)) - moved
	project := func(size int) Config { return withEncodedSize(t, weightedProject(), size) }
	if err := Validate(project(limit)); err != nil {
		t.Fatalf("project at the cap: %v", err)
	}
	for _, size := range []int{limit + 1, limit + moved} {
		if err := Validate(project(size)); !errors.Is(err, errTooLarge) {
			t.Errorf("project %d bytes past the cap: %v, want %v", size-limit, err, errTooLarge)
		}
	}
	rated := project(limit)
	widestRateFaults, widestRateEmergencies := withRate(1.0000000000000002e-06)
	rated.Demand, rated.Faults, rated.Emergencies = widestDemand, &widestRateFaults, &widestRateEmergencies
	if size, err := encodedSize(rated); err != nil || size > MaxFileBytes {
		t.Fatalf("a project at the cap with the widest rates encodes to %d bytes: %v", size, err)
	}
}
