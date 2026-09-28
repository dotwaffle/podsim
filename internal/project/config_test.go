package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"os"
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
		{"sharing mode", func(config *Config) { config.SharedRideMode = "pickups" }},
		{"sharing stops", func(config *Config) { config.SharedRideMaxStops = sim.MaxSharedRideStops + 1 }},
		{"negative sharing stops", func(config *Config) { config.SharedRideMaxStops = -1 }},
		{"sharing join", func(config *Config) { config.SharedRideJoin = "reassign" }},
		{"platoon limit of one pod", func(config *Config) { config.PlatoonLimit = 1 }},
		{"platoon limit", func(config *Config) { config.PlatoonLimit = sim.MaxPlatoonLimit + 1 }},
		{"negative platoon limit", func(config *Config) { config.PlatoonLimit = -1 }},
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

// TestValidateAcceptsNetworkLimits checks a project with the most nodes and
// lanes that Validate allows. The added nodes are on a circle 40 meters
// apart, away from the stations. With 2 lanes to and 2 lanes from almost
// each added node, the network has the fewest lane pairs at nodes that
// MaxNodes and MaxLanes allow.
func TestValidateAcceptsNetworkLimits(t *testing.T) {
	t.Parallel()
	config := Default()
	count := MaxNodes - len(config.Network.Nodes)
	radius := 40 * float64(count) / (2 * math.Pi)
	for index := range count {
		angle := 2 * math.Pi * float64(index) / float64(count)
		config.Network.Nodes = append(config.Network.Nodes, sim.Node{
			ID: fmt.Sprintf("far-%d", index), Position: sim.Point{X: 40_000 + radius*math.Cos(angle), Y: radius * math.Sin(angle)},
		})
	}
	// Each added lane goes from an added node to one of the next added
	// nodes, so that no node has many lanes and no two lanes are the same.
	far := config.Network.Nodes[len(Default().Network.Nodes):]
	for index := range MaxLanes - len(config.Network.Lanes) {
		from, step := index%len(far), 1+index/len(far)
		config.Network.Lanes = append(config.Network.Lanes, sim.Lane{
			ID: fmt.Sprintf("far-%d", index), From: far[from].ID, To: far[(from+step)%len(far)].ID, SpeedLimit: 12,
		})
	}
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
}

// TestValidateLimitsGeometry checks the coordinate and block limits. The
// errors name the node or lane.
func TestValidateLimitsGeometry(t *testing.T) {
	t.Parallel()
	edge := Default()
	edge.Network.Nodes = append(edge.Network.Nodes,
		sim.Node{ID: "corner-a", Position: sim.Point{X: -MaxCoordinate, Y: MaxCoordinate}},
		sim.Node{ID: "corner-b", Position: sim.Point{X: MaxCoordinate, Y: -MaxCoordinate}})
	edge.Network.Lanes = append(edge.Network.Lanes, sim.Lane{
		ID: "corner", From: "corner-a", To: "corner-b", SpeedLimit: 12, Control: &sim.Point{X: MaxCoordinate, Y: MaxCoordinate},
	})
	if err := Validate(edge); err != nil {
		t.Fatalf("coordinates at the limit: %v", err)
	}
	// long adds count straight lanes of 180 km, each 6,000 blocks.
	long := func(count int) Config {
		config := Default()
		for index := range count {
			from, to := fmt.Sprintf("west-%d", index), fmt.Sprintf("east-%d", index)
			y := 20_000 + 1_000*float64(index)
			config.Network.Nodes = append(config.Network.Nodes,
				sim.Node{ID: from, Position: sim.Point{X: -90_000, Y: y}}, sim.Node{ID: to, Position: sim.Point{X: 90_000, Y: y}})
			config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: fmt.Sprintf("long-%d", index), From: from, To: to, SpeedLimit: 12})
		}
		return config
	}
	if err := Validate(long(MaxNetworkBlocks / 6_000)); err != nil {
		t.Fatalf("lanes below the block limit: %v", err)
	}
	outside := Clone(edge)
	outside.Network.Nodes[len(outside.Network.Nodes)-1].Position.X = math.Nextafter(MaxCoordinate, math.Inf(1))
	notNumber := Clone(edge)
	notNumber.Network.Nodes[len(notNumber.Network.Nodes)-1].Position.Y = math.NaN()
	control := Clone(edge)
	control.Network.Lanes[len(control.Network.Lanes)-1].Control.Y = -MaxCoordinate - 1
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{"node outside", outside, fmt.Sprintf(`node "corner-b" must have coordinates from -%d to %d meters`, MaxCoordinate, MaxCoordinate)},
		{"node not a number", notNumber, `node "corner-b" must have coordinates`},
		{"control point outside", control, fmt.Sprintf(`lane "corner" must have a control point with coordinates from -%d to %d meters`, MaxCoordinate, MaxCoordinate)},
		{"too many blocks", long(MaxNetworkBlocks/6_000 + 1), fmt.Sprintf(`blocks, more than %d, and lane "long-0" has the most, 6000`, MaxNetworkBlocks)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(test.config); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate = %v, want %q", err, test.want)
			}
		})
	}
}

// TestEditorMirrorsLimits checks that the editor uses the limits of
// Validate.
func TestEditorMirrorsLimits(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../../web/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"MAX_STATIONS": MaxStations, "MAX_BERTHS": MaxBerths, "MAX_NODES": MaxNodes, "MAX_LANES": MaxLanes, "MAX_NODE_LANES": MaxNodeLanes, "MAX_FLOWS": MaxFlows} {
		if !strings.Contains(string(source), fmt.Sprintf("const %s = %d;", name, want)) {
			t.Errorf("web/editor.js does not set %s to %d", name, want)
		}
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

func TestSharedRideModeDefaults(t *testing.T) {
	t.Parallel()
	config := Default()
	if mode, stops := EffectiveSharedRideMode(config), EffectiveSharedRideMaxStops(config); mode != sim.SharedRideDropOffs || stops != sim.DefaultSharedRideMaxStops {
		t.Fatalf("effective mode %q with %d stops", mode, stops)
	}
	config.SharedRideMode, config.SharedRideMaxStops = sim.SharedRideDestination, sim.MaxSharedRideStops
	if err := Validate(config); err != nil {
		t.Fatal(err)
	}
	if mode, stops := EffectiveSharedRideMode(config), EffectiveSharedRideMaxStops(config); mode != sim.SharedRideDestination || stops != sim.MaxSharedRideStops {
		t.Fatalf("effective mode %q with %d stops", mode, stops)
	}
}

// TestSharedRideJoin checks that an empty join policy loads as the
// default, and that ConfigureSharedRides applies each policy.
func TestSharedRideJoin(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		join, want sim.SharedRideJoin
	}{
		{"", sim.SharedRideJoinUnassigned},
		{sim.SharedRideJoinUnassigned, sim.SharedRideJoinUnassigned},
		{sim.SharedRideJoinReassignExisting, sim.SharedRideJoinReassignExisting},
	} {
		config := Default()
		config.SharedRideJoin = test.join
		if err := Validate(config); err != nil {
			t.Fatalf("policy %q: %v", test.join, err)
		}
		if got := EffectiveSharedRideJoin(config); got != test.want {
			t.Fatalf("policy %q loads as %q, want %q", test.join, got, test.want)
		}
		simulation, err := sim.NewFleet(config.Network, config.Fleet)
		if err != nil {
			t.Fatal(err)
		}
		if err := ConfigureSharedRides(simulation, config); err != nil {
			t.Fatal(err)
		}
		if got := simulation.ExportState().SharedRideJoin; got != test.want {
			t.Fatalf("policy %q applies as %q, want %q", test.join, got, test.want)
		}
	}
}

// TestConfigurePlatoons checks each valid platoon limit. A limit of 0
// turns platoons off and keeps the limit of the simulation.
func TestConfigurePlatoons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		limit, wantLimit int
		want             sim.Platooning
	}{
		{0, sim.MaxPlatoonLimit, sim.PlatooningOff},
		{2, 2, sim.PlatooningVirtual},
		{3, 3, sim.PlatooningVirtual},
		{4, 4, sim.PlatooningVirtual},
	}
	for _, test := range tests {
		config := Default()
		config.PlatoonLimit = test.limit
		if err := Validate(config); err != nil {
			t.Fatalf("limit %d: %v", test.limit, err)
		}
		simulation, err := sim.NewFleet(config.Network, config.Fleet)
		if err != nil {
			t.Fatal(err)
		}
		if err := simulation.SetPlatooning(sim.PlatooningVirtual); err != nil {
			t.Fatal(err)
		}
		if err := ConfigurePlatoons(simulation, config); err != nil {
			t.Fatalf("limit %d: %v", test.limit, err)
		}
		if simulation.Platooning() != test.want || simulation.PlatoonLimit() != test.wantLimit {
			t.Fatalf("limit %d: mode %d with limit %d, want mode %d with limit %d",
				test.limit, simulation.Platooning(), simulation.PlatoonLimit(), test.want, test.wantLimit)
		}
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

// weightedProject returns a valid project with 48 passenger stations and the
// largest number of demand profiles and bands. Each profile has a flow for
// each ordered pair of stations, so the project has 215,424 weights. Each
// weight is 1.
func weightedProject() Config {
	const stations = 48
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

// TestValidateErrorsStayShort sets each string of a valid project to a
// very long value in turn. The session keeps the error of a rejected
// command, so each error must stay short.
func TestValidateErrorsStayShort(t *testing.T) {
	t.Parallel()
	const maxErrorBytes = 1024
	long := strings.Repeat("x", 1<<20)
	base := Default()
	base.DemandProfiles = []DemandProfile{testDemandProfile()}
	base.Demand = DemandConfig{Enabled: true, PerMinute: 12, Pattern: "profile", Seed: 7, Profile: "weekday", Band: "am"}
	if err := Validate(base); err != nil {
		t.Fatal(err)
	}
	var paths []string
	var walk func(value reflect.Value, path string)
	walk = func(value reflect.Value, path string) {
		switch value.Kind() {
		case reflect.String:
			paths = append(paths, path)
			config := Clone(base)
			field := reflect.ValueOf(&config).Elem()
			for name := range strings.SplitSeq(path[1:], ".") {
				if field.Kind() == reflect.Slice {
					field = field.Index(0)
				}
				field = field.FieldByName(name)
			}
			field.SetString(long)
			err := Validate(config)
			if err == nil {
				t.Errorf("%s: a value of %d bytes is valid", path, len(long))
			} else if len(err.Error()) > maxErrorBytes {
				t.Errorf("%s: the error has %d bytes, more than %d", path, len(err.Error()), maxErrorBytes)
			}
		case reflect.Struct:
			for field, fieldValue := range value.Fields() {
				walk(fieldValue, path+"."+field.Name)
			}
		case reflect.Slice:
			if value.Len() > 0 {
				walk(value.Index(0), path)
			}
		default:
			// A project has no strings in maps or behind pointers.
		}
	}
	walk(reflect.ValueOf(base), "")
	t.Logf("checked %d strings", len(paths))
	if len(paths) < 20 {
		t.Fatalf("checked only %d strings: %v", len(paths), paths)
	}
}

func TestQuoteID(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("x", maxIDLength)
	if got, want := quoteID(id), `"`+id+`"`; got != want {
		t.Fatalf("quoteID(%d bytes) = %s, want %s", len(id), got, want)
	}
	if got, want := quoteID(id+"y"), `"`+id+`"...`; got != want {
		t.Fatalf("quoteID(%d bytes) = %s, want %s", len(id)+1, got, want)
	}
	// A rune that crosses the limit is removed whole.
	cut := strings.Repeat("x", maxIDLength-1) + "é"
	if got, want := quoteID(cut), `"`+strings.Repeat("x", maxIDLength-1)+`"...`; got != want {
		t.Fatalf("quoteID(%q) = %s, want %s", cut, got, want)
	}
	// Invalid UTF-8 does not remove more than one rune of bytes.
	invalid := strings.Repeat("\x80", 2*maxIDLength)
	if got := quoteID(invalid); len(got) < 4*(maxIDLength-utf8.UTFMax) {
		t.Fatalf("quoteID(invalid) = %s, too short", got)
	}
}
