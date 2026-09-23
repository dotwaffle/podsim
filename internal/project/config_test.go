package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestConfigJSONRoundTripAndClone(t *testing.T) {
	t.Parallel()
	want := Default()
	want.Redistribution = true
	want.Demand = DemandConfig{Enabled: true, PerMinute: 30, Pattern: "destination", Seed: 9, Destination: "garden"}
	want.Network.Lanes[0].SeparationGroup = "surface"
	want.Network.Stations[0].Berths[0].SeparationGroup = "surface"
	want.DemandProfiles = []DemandProfile{testDemandProfile()}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed config\n got: %#v\nwant: %#v", got, want)
	}
	clone := Clone(want)
	clone.Network.Nodes[0].ID = "changed"
	clone.Network.Stations[0].Berths[0].ID = "changed"
	clone.Fleet[0].ID = "changed"
	clone.DemandProfiles[0].Flows[0].Weights[0] = 99
	if strings.Contains(string(mustJSON(t, want)), "changed") {
		t.Fatal("clone aliases config storage")
	}
}

func testDemandProfile() DemandProfile {
	return DemandProfile{
		ID: "weekday", Name: "Weekday",
		Bands: []DemandBand{{ID: "am", Name: "AM peak", StartMinute: 420, DurationMinutes: 180}},
		Flows: []DemandFlow{
			{From: "harbor", To: "garden", Weights: []float64{3}},
			{From: "garden", To: "market", Weights: []float64{1}},
		},
	}
}

func TestValidateRejectsMalformedProjects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"version", func(config *Config) { config.Version = 2 }},
		{"name", func(config *Config) { config.Name = "" }},
		{"long name", func(config *Config) { config.Name = strings.Repeat("x", maxNameLength+1) }},
		{"invalid UTF-8 name", func(config *Config) { config.Name = "Podsim \xff" }},
		{"long node ID", func(config *Config) { config.Network.Nodes[0].ID = strings.Repeat("x", maxIDLength+1) }},
		{"long lane separation group", func(config *Config) { config.Network.Lanes[0].SeparationGroup = strings.Repeat("x", maxIDLength+1) }},
		{"unknown lane station", func(config *Config) {
			config.Network.Lanes[0].StationID, config.Network.Lanes[0].StationRole = "missing", sim.StationThroughRole
		}},
		{"missing lane station role", func(config *Config) { config.Network.Lanes[0].StationRole = "" }},
		{"invalid lane station role", func(config *Config) { config.Network.Lanes[0].StationRole = "invalid" }},
		{"long berth separation group", func(config *Config) {
			config.Network.Stations[0].Berths[0].SeparationGroup = strings.Repeat("x", maxIDLength+1)
		}},
		{"long station name", func(config *Config) { config.Network.Stations[0].Name = strings.Repeat("x", maxNameLength+1) }},
		{"berth bound", func(config *Config) { config.Network.Stations[3].Berths = make([]sim.Berth, MaxBerths+1) }},
		{"nan", func(config *Config) { config.Network.Nodes[0].Position.X = math.NaN() }},
		{"duplicate node", func(config *Config) { config.Network.Nodes[1].ID = config.Network.Nodes[0].ID }},
		{"duplicate lane", func(config *Config) { config.Network.Lanes[1].ID = config.Network.Lanes[0].ID }},
		{"duplicate station", func(config *Config) { config.Network.Stations[1].ID = config.Network.Stations[0].ID }},
		{"duplicate pod", func(config *Config) { config.Fleet[1].ID = config.Fleet[0].ID }},
		{"occupied berth", func(config *Config) { config.Fleet[1].StationID = config.Fleet[0].StationID }},
		{"rate low", func(config *Config) { config.Demand.PerMinute = 0 }},
		{"rate high", func(config *Config) { config.Demand.PerMinute = 121 }},
		{"sharing limit", func(config *Config) { config.SharedRidePartyLimit = sim.MaxSharedRideParties + 1 }},
		{"pattern", func(config *Config) { config.Demand.Pattern = "rush" }},
		{"missing profile", func(config *Config) {
			config.Demand.Pattern, config.Demand.Profile, config.Demand.Band = "profile", "missing", "am"
		}},
		{"unknown band", func(config *Config) {
			config.DemandProfiles = []DemandProfile{testDemandProfile()}
			config.Demand.Pattern, config.Demand.Profile, config.Demand.Band = "profile", "weekday", "missing"
		}},
		{"profile station", func(config *Config) {
			profile := testDemandProfile()
			profile.Flows[0].From = "missing"
			config.DemandProfiles = []DemandProfile{profile}
		}},
		{"profile weights", func(config *Config) {
			profile := testDemandProfile()
			profile.Flows[0].Weights = nil
			config.DemandProfiles = []DemandProfile{profile}
		}},
		{"profile negative weight", func(config *Config) {
			profile := testDemandProfile()
			profile.Flows[0].Weights[0] = -1
			config.DemandProfiles = []DemandProfile{profile}
		}},
		{"duplicate profile", func(config *Config) {
			profile := testDemandProfile()
			config.DemandProfiles = []DemandProfile{profile, profile}
		}},
		{"parking destination", func(config *Config) {
			config.Demand.Pattern, config.Demand.Destination = "destination", "parking"
		}},
		{"unreachable", func(config *Config) { config.Network.Lanes = config.Network.Lanes[:3] }},
		{"pod bound", func(config *Config) {
			config.Fleet = make([]sim.Placement, MaxPods+1)
		}},
		{"station bound", func(config *Config) {
			config.Network.Stations = make([]sim.Station, MaxStations+1)
		}},
		{"node bound", func(config *Config) { config.Network.Nodes = make([]sim.Node, MaxNodes+1) }},
		{"lane bound", func(config *Config) { config.Network.Lanes = make([]sim.Lane, MaxLanes+1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			test.change(&config)
			if err := Validate(config); err == nil {
				t.Fatal("accepted malformed project")
			}
		})
	}
}

func TestLegacySharedRideLimitDefaultsToOne(t *testing.T) {
	t.Parallel()
	config := Default()
	config.SharedRidePartyLimit = 0
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	if got := EffectiveSharedRidePartyLimit(config); got != 1 {
		t.Fatalf("effective shared ride party limit = %d", got)
	}
}

func TestValidateAcceptsProfileDemand(t *testing.T) {
	t.Parallel()
	config := Default()
	config.DemandProfiles = []DemandProfile{testDemandProfile()}
	config.Demand = DemandConfig{Enabled: true, PerMinute: 12, Pattern: "profile", Seed: 7, Profile: "weekday", Band: "am"}
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRequiresTwoReachablePassengerStations(t *testing.T) {
	t.Parallel()
	config := Default()
	for i := range config.Network.Stations[:3] {
		config.Network.Stations[i].ParkingOnly = true
	}
	if err := Validate(config); err == nil {
		t.Fatal("accepted one passenger station")
	}
}

func TestValidateLimitsEncodedSize(t *testing.T) {
	t.Parallel()
	// Validate measures the project with the widest demand settings. The
	// weighted project has the default demand settings.
	limit := MaxFileBytes - demandReserve(t)
	tests := []struct {
		name   string
		config Config
		err    error
	}{
		{"default project", Default(), nil},
		{"encoding at the limit", withEncodedSize(t, weightedProject(), limit), nil},
		{"encoding one byte over the limit", withEncodedSize(t, weightedProject(), limit+1), errTooLarge},
		{"no room for demand settings", withEncodedSize(t, weightedProject(), MaxFileBytes), errTooLarge},
		{"file in short exponent form", shortExponentProject(t), errTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(test.config); !errors.Is(err, test.err) {
				t.Fatalf("Validate error %v, want %v", err, test.err)
			}
		})
	}
}

// TestDemandChangeKeepsProjectInLimit checks that a change to the demand
// settings of a valid project cannot take its encoding over MaxFileBytes.
// The demand command checks the new settings with ValidateDemand only.
func TestDemandChangeKeepsProjectInLimit(t *testing.T) {
	t.Parallel()
	config := withEncodedSize(t, weightedProject(), MaxFileBytes-demandReserve(t))
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	control := strings.Repeat("\x01", maxIDLength)
	demand := DemandConfig{PerMinute: 120, Pattern: "balanced", Seed: math.MaxUint64, Destination: control, Profile: control, Band: control}
	if err := ValidateDemand(demand, DemandContext{Network: config.Network, Profiles: config.DemandProfiles}); err != nil {
		t.Fatal(err)
	}
	config.Demand = demand
	if size := len(canonicalJSON(t, config)); size > MaxFileBytes {
		t.Fatalf("canonical encoding has %d bytes after the demand change, limit %d", size, MaxFileBytes)
	}
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
}

// TestWidestDemandBoundsDemandSettings checks that ValidateDemand refuses
// values past those of widestDemand, and that no demand settings that it
// accepts have a longer encoding.
func TestWidestDemandBoundsDemandSettings(t *testing.T) {
	t.Parallel()
	control := strings.Repeat("\x01", maxIDLength)
	network := CloneNetwork(Default().Network)
	network.Stations[0].ID = control
	context := DemandContext{Network: network, Profiles: []DemandProfile{{ID: control, Bands: []DemandBand{{ID: control}}}}}
	tooFast := DemandConfig{PerMinute: widestDemand.PerMinute + 1, Pattern: "balanced"}
	if err := ValidateDemand(tooFast, context); err == nil {
		t.Errorf("ValidateDemand accepted %d orders per minute", tooFast.PerMinute)
	}
	tooLong := DemandConfig{PerMinute: 1, Pattern: "balanced", Band: control + "x"}
	if err := ValidateDemand(tooLong, context); err == nil {
		t.Errorf("ValidateDemand accepted a reference of %d bytes", len(tooLong.Band))
	}
	widest := len(canonicalJSON(t, widestDemand))
	for _, enabled := range []bool{false, true} {
		for _, pattern := range []string{"balanced", "market", "destination", "profile"} {
			demand := DemandConfig{
				Enabled: enabled, PerMinute: widestDemand.PerMinute, Pattern: pattern, Seed: math.MaxUint64,
				Destination: control, Profile: control, Band: control,
			}
			if err := ValidateDemand(demand, context); err != nil {
				t.Fatalf("pattern %s: %v", pattern, err)
			}
			if got := len(canonicalJSON(t, demand)); got > widest {
				t.Errorf("enabled %t, pattern %s: encoding has %d bytes, more than %d", enabled, pattern, got, widest)
			}
		}
	}
	// A control character has the longest escape. Compare it with each
	// ASCII character and with some characters of 2 to 4 bytes.
	runes := []rune{'\u00e9', '\u2028', '\ufffd', '\U0001f600'}
	for r := range rune(utf8.RuneSelf) {
		runes = append(runes, r)
	}
	for _, r := range runes {
		reference := strings.Repeat(string(r), maxIDLength/utf8.RuneLen(r))
		if got, want := len(canonicalJSON(t, reference)), len(canonicalJSON(t, control)); got > want {
			t.Errorf("rune %U: reference encodes to %d bytes, more than %d", r, got, want)
		}
	}
}

// TestEncodedSizeCountsCanonicalEncoding checks that encodedSize counts the
// bytes of the encoding that the session state file uses for its project
// member.
func TestEncodedSizeCountsCanonicalEncoding(t *testing.T) {
	t.Parallel()
	profiles := Default()
	profiles.DemandProfiles = []DemandProfile{testDemandProfile()}
	// Other JSON options change the length of HTML characters, empty slices
	// and control characters.
	escaped := Default()
	escaped.Name = "Podsim <A & B>"
	escaped.DemandProfiles = []DemandProfile{{ID: "empty", Name: "Empty"}}
	escaped.Demand = widestDemand
	tests := []struct {
		name   string
		config Config
	}{
		{"default project", Default()},
		{"profile demand", profiles},
		{"escaped characters and empty slices", escaped},
		{"many weights", weightedProject()},
		{"encoding at the limit", withEncodedSize(t, weightedProject(), MaxFileBytes)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := encodedSize(test.config)
			if err != nil {
				t.Fatal(err)
			}
			if want := len(canonicalJSON(t, test.config)); got != want {
				t.Fatalf("encodedSize = %d, want %d", got, want)
			}
		})
	}
}

// demandReserve returns the bytes that Validate keeps free for a change
// from the default demand settings to the widest demand settings.
func demandReserve(t *testing.T) int {
	t.Helper()
	return len(canonicalJSON(t, widestDemand)) - len(canonicalJSON(t, Default().Demand))
}

// weightedProject returns a valid project with 34 passenger stations and the
// largest number of demand profiles and bands. Each profile has a flow for
// each ordered pair of stations, so the project has 215,424 weights. Each
// weight is 1.
func weightedProject() Config {
	const stations = 34
	config := Default()
	config.Network = loopNetwork(stations)
	config.Fleet = []sim.Placement{{ID: "01", StationID: "s00", BerthID: "s00-1"}}
	bands := make([]DemandBand, MaxBands)
	for index := range bands {
		bands[index] = DemandBand{ID: fmt.Sprintf("b%02d", index), Name: "Band", DurationMinutes: 60}
	}
	for index := range MaxProfiles {
		profile := DemandProfile{ID: fmt.Sprintf("p%d", index), Name: "Profile", Bands: bands}
		for from := range stations {
			for to := range stations {
				if from != to {
					profile.Flows = append(profile.Flows, DemandFlow{
						From: fmt.Sprintf("s%02d", from), To: fmt.Sprintf("s%02d", to),
						Weights: slices.Repeat([]float64{1}, MaxBands),
					})
				}
			}
		}
		config.DemandProfiles = append(config.DemandProfiles, profile)
	}
	return config
}

// loopNetwork returns a one-way loop of passenger stations. Each station has
// one berth.
func loopNetwork(stations int) sim.Network {
	var network sim.Network
	for index := range stations {
		id := fmt.Sprintf("s%02d", index)
		entry, exit, berth := id+"-entry", id+"-exit", id+"-berth"
		x := 200 * float64(index)
		network.Nodes = append(network.Nodes,
			sim.Node{ID: entry, Position: sim.Point{X: x}},
			sim.Node{ID: exit, Position: sim.Point{X: x + 100}},
			sim.Node{ID: berth, Position: sim.Point{X: x + 50, Y: 60}},
		)
		network.Lanes = append(network.Lanes,
			sim.Lane{ID: id + "-through", From: entry, To: exit, SpeedLimit: 14, StationID: id, StationRole: sim.StationThroughRole},
			sim.Lane{ID: id + "-in", From: entry, To: berth, SpeedLimit: 14, StationID: id, StationRole: sim.StationBerthAccessRole},
			sim.Lane{ID: id + "-out", From: berth, To: exit, SpeedLimit: 14, StationID: id, StationRole: sim.StationDepartureRole},
			sim.Lane{ID: id + "-next", From: exit, To: fmt.Sprintf("s%02d-entry", (index+1)%stations), SpeedLimit: 14},
		)
		network.Stations = append(network.Stations, sim.Station{
			ID: id, Name: "Station " + id, Entry: entry, Exit: exit, Berths: []sim.Berth{{ID: id + "-1", Node: berth}},
		})
	}
	return network
}

// withEncodedSize raises the weights of config until its canonical encoding
// has size bytes. Each weight must be 1 at the start. The canonical form of
// 10^k has k+1 digits for k from 0 to 20, so each weight can add 0 to 20
// bytes.
func withEncodedSize(t *testing.T, config Config, size int) Config {
	t.Helper()
	extra := size - len(canonicalJSON(t, config))
	for _, profile := range config.DemandProfiles {
		for _, flow := range profile.Flows {
			for index := range flow.Weights {
				digits := min(extra, 20)
				flow.Weights[index] = math.Pow10(digits)
				extra -= digits
			}
		}
	}
	if got := len(canonicalJSON(t, config)); got != size {
		t.Fatalf("canonical encoding has %d bytes, want %d", got, size)
	}
	return config
}

// shortExponentProject returns a project decoded from a file of at most
// MaxFileBytes. The canonical encoding of the project has more than
// MaxFileBytes. The file writes each weight as 1e20, and the canonical
// encoding writes it with 21 digits.
func shortExponentProject(t *testing.T) Config {
	t.Helper()
	config := weightedProject()
	for _, profile := range config.DemandProfiles {
		for _, flow := range profile.Flows {
			for index := range flow.Weights {
				flow.Weights[index] = 1e20
			}
		}
	}
	canonical := canonicalJSON(t, config)
	file := bytes.ReplaceAll(canonical, []byte("100000000000000000000"), []byte("1e20"))
	if len(file) > MaxFileBytes || len(canonical) <= MaxFileBytes {
		t.Fatalf("file has %d bytes and canonical encoding has %d bytes, limit %d", len(file), len(canonical), MaxFileBytes)
	}
	var decoded Config
	if err := json.Unmarshal(file, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

// canonicalJSON returns the encoding that the session state file uses for
// its project member.
func canonicalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := jsonv2.Marshal(value, jsonv2.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
