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
	"runtime"
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
	data, err := jsonv2.Marshal(want, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err = jsonv2.Unmarshal(data, &got, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip changed config\n got: %#v\nwant: %#v", got, want)
	}
	want.Geo = &Geo{Latitude: 51.5, Longitude: -0.1, Projection: GeoProjection, Radius: GeoRadius}
	want.Map = &MapBackground{Provider: "osm", Opacity: .45}
	data, err = jsonv2.Marshal(want, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	var withGeo Config
	if err = jsonv2.Unmarshal(data, &withGeo, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(withGeo, want) {
		t.Fatalf("round trip changed the geo reference\n got: %#v\nwant: %#v", withGeo.Geo, want.Geo)
	}
	clone := Clone(want)
	clone.Geo.Latitude = 1
	clone.Map.Opacity = .9
	if want.Map.Opacity != .45 {
		t.Fatal("clone aliases map settings")
	}
	if want.Geo.Latitude != 51.5 {
		t.Fatal("clone aliases the geo reference")
	}
	clone.Network.Nodes[0].ID = "changed"
	clone.Network.Stations[0].Berths[0].ID = "changed"
	clone.Fleet[0].ID = "changed"
	clone.DemandProfiles[0].Flows[0].Weights[0] = 99
	if strings.Contains(string(mustJSON(t, want)), "changed") {
		t.Fatal("clone aliases config storage")
	}
}

// testGeo gives a valid geo reference at the latitude and the longitude.
func testGeo(latitude, longitude float64) *Geo {
	return &Geo{Latitude: latitude, Longitude: longitude, Projection: GeoProjection, Radius: GeoRadius}
}

// TestValidateAcceptsGeo checks the geo references at the limits, and a
// reference at latitude 0 and longitude 0.
func TestValidateAcceptsGeo(t *testing.T) {
	t.Parallel()
	for _, geo := range []*Geo{nil, testGeo(0, 0), testGeo(MaxGeoLatitude, 180), testGeo(-MaxGeoLatitude, -180), testGeo(51.5074, -0.1278)} {
		config := Default()
		config.Geo = geo
		if err := Validate(config); err != nil {
			t.Fatalf("Validate with geo %+v: %v", geo, err)
		}
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
		{"long name", func(config *Config) { config.Name = strings.Repeat("x", MaxNameLength+1) }},
		{"invalid UTF-8 name", func(config *Config) { config.Name = "Podsim \xff" }},
		{"long node ID", func(config *Config) { config.Network.Nodes[0].ID = strings.Repeat("x", MaxIDLength+1) }},
		{"long lane separation group", func(config *Config) { config.Network.Lanes[0].SeparationGroup = strings.Repeat("x", MaxIDLength+1) }},
		{"unknown lane station", func(config *Config) {
			config.Network.Lanes[0].StationID, config.Network.Lanes[0].StationRole = "missing", sim.StationThroughRole
		}},
		{"missing lane station role", func(config *Config) { config.Network.Lanes[0].StationRole = "" }},
		{"invalid lane station role", func(config *Config) { config.Network.Lanes[0].StationRole = "invalid" }},
		{"long berth separation group", func(config *Config) {
			config.Network.Stations[0].Berths[0].SeparationGroup = strings.Repeat("x", MaxIDLength+1)
		}},
		{"long station name", func(config *Config) { config.Network.Stations[0].Name = strings.Repeat("x", MaxNameLength+1) }},
		{"berth bound", func(config *Config) { config.Network.Stations[3].Berths = make([]sim.Berth, MaxBerths+1) }},
		{"nan", func(config *Config) { config.Network.Nodes[0].Position.X = math.NaN() }},
		{"duplicate node", func(config *Config) { config.Network.Nodes[1].ID = config.Network.Nodes[0].ID }},
		{"duplicate lane", func(config *Config) { config.Network.Lanes[1].ID = config.Network.Lanes[0].ID }},
		{"duplicate station", func(config *Config) { config.Network.Stations[1].ID = config.Network.Stations[0].ID }},
		{"duplicate pod", func(config *Config) { config.Fleet[1].ID = config.Fleet[0].ID }},
		{"occupied berth", func(config *Config) { config.Fleet[1].StationID = config.Fleet[0].StationID }},
		{"pod with no berth ID", func(config *Config) { config.Fleet[0].BerthID = "" }},
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
		// The decoders refuse an integral number above sim.MaxCounter.
		{"profile weight above the largest counter", func(config *Config) {
			profile := testDemandProfile()
			profile.Flows[0].Weights[0] = 1e20
			config.DemandProfiles = []DemandProfile{profile}
		}},
		{"lane speed limit above the largest counter", func(config *Config) {
			config.Network = CloneNetwork(config.Network)
			config.Network.Lanes[0].SpeedLimit = 1e20
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
		{"geo latitude", func(config *Config) { config.Geo = testGeo(MaxGeoLatitude+0.001, 0) }},
		{"geo negative latitude", func(config *Config) { config.Geo = testGeo(-MaxGeoLatitude-0.001, 0) }},
		{"geo latitude not a number", func(config *Config) { config.Geo = testGeo(math.NaN(), 0) }},
		{"geo longitude", func(config *Config) { config.Geo = testGeo(0, 180.001) }},
		{"geo longitude not a number", func(config *Config) { config.Geo = testGeo(0, math.NaN()) }},
		{"geo projection", func(config *Config) { config.Geo = testGeo(0, 0); config.Geo.Projection = "web-mercator" }},
		{"geo radius", func(config *Config) { config.Geo = testGeo(0, 0); config.Geo.Radius = 6_378_137 }},
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

// TestEditorMirrorsLimits checks that the editor uses the lane limit of
// Validate.
func TestEditorMirrorsLimits(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../../web/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("const MAX_LANES = %d;", MaxLanes); !strings.Contains(string(source), want) {
		t.Errorf("web/editor.js does not have %s", want)
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
	reference := strings.Repeat("z", MaxIDLength)
	demand := DemandConfig{PerMinute: 120, Pattern: "balanced", Seed: math.MaxUint64, Destination: reference, Profile: reference, Band: reference}
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
	reference := strings.Repeat("z", MaxIDLength)
	network := CloneNetwork(Default().Network)
	network.Stations[0].ID = reference
	context := DemandContext{Network: network, Profiles: []DemandProfile{{ID: reference, Bands: []DemandBand{{ID: reference}}}}, RailArrivals: []RailArrival{{ID: "train", Station: reference, Passengers: 1, Destinations: []RailDestination{{Station: "market", Weight: 1}}}}}
	tooFast := DemandConfig{PerMinute: widestDemand.PerMinute + 1, Pattern: "balanced"}
	if err := ValidateDemand(tooFast, context); err == nil {
		t.Errorf("ValidateDemand accepted %d orders per minute", tooFast.PerMinute)
	}
	tooLong := DemandConfig{PerMinute: 1, Pattern: "balanced", Band: reference + "x"}
	if err := ValidateDemand(tooLong, context); err == nil {
		t.Errorf("ValidateDemand accepted a reference of %d bytes", len(tooLong.Band))
	}
	control := DemandConfig{PerMinute: 1, Pattern: "balanced", Band: "\x01"}
	if err := ValidateDemand(control, context); err == nil || err.Error() != `ID "\x01" has a character other than A-Z, a-z, 0-9, '.', '+' or '-'` {
		t.Errorf("ValidateDemand accepted a control character in a reference: %v", err)
	}
	widest := len(canonicalJSON(t, widestDemand))
	for _, enabled := range []bool{false, true} {
		for _, pattern := range []string{"balanced", "market", "destination", "profile", "rail-arrivals"} {
			demand := DemandConfig{
				Enabled: enabled, PerMinute: widestDemand.PerMinute, Pattern: pattern, Seed: math.MaxUint64,
				Destination: reference, Profile: reference, Band: reference,
			}
			if err := ValidateDemand(demand, context); err != nil {
				t.Fatalf("pattern %s: %v", pattern, err)
			}
			if got := len(canonicalJSON(t, demand)); got > widest {
				t.Errorf("enabled %t, pattern %s: encoding has %d bytes, more than %d", enabled, pattern, got, widest)
			}
		}
	}
	// No ID character has an escape.
	for _, c := range []byte(sim.IDCharacters) {
		if got, want := len(canonicalJSON(t, strings.Repeat(string(c), MaxIDLength))), len(canonicalJSON(t, reference)); got != want {
			t.Errorf("character %q: reference encodes to %d bytes, not %d", c, got, want)
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

// weightedProject returns a valid project with 50 passenger stations and the
// largest number of demand profiles and bands. Each profile has a flow for
// each ordered pair of stations, so the project has 470,400 weights. Each
// weight is 1.
func weightedProject() Config {
	const stations = 50
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

// wideWeight has the longest canonical form of a weight, 24 bytes. A
// weight is at most sim.MaxCounter, so an integral weight has at most 16
// digits.
const wideWeight = 0.0000010000000000000002

// withEncodedSize raises the weights of config until its canonical encoding
// has size bytes. Each weight must be 1 at the start. wideWeight adds 23
// bytes to a weight, and the canonical form of 10^k has k+1 digits, so
// 10^k adds k bytes for k from 0 to 15.
func withEncodedSize(t *testing.T, config Config, size int) Config {
	t.Helper()
	extra := size - len(canonicalJSON(t, config))
	for _, profile := range config.DemandProfiles {
		for _, flow := range profile.Flows {
			for index := range flow.Weights {
				if extra >= 23 {
					flow.Weights[index] = wideWeight
					extra -= 23
					continue
				}
				digits := min(extra, 15)
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
// MaxFileBytes. Half of the weights are 1e15, and the others wideWeight.
// The file writes them as 1e15 and 1.0000000000000002e-6, and the
// canonical encoding writes them with 16 and 24 bytes.
func shortExponentProject(t *testing.T) Config {
	t.Helper()
	config := weightedProject()
	for _, profile := range config.DemandProfiles {
		for _, flow := range profile.Flows {
			for index := range flow.Weights {
				flow.Weights[index] = 1e15
				if index%2 == 0 {
					flow.Weights[index] = wideWeight
				}
			}
		}
	}
	canonical := canonicalJSON(t, config)
	file := bytes.ReplaceAll(canonical, []byte("0.0000010000000000000002"), []byte("1.0000000000000002e-6"))
	file = bytes.ReplaceAll(file, []byte("1000000000000000"), []byte("1e15"))
	if len(file) > MaxFileBytes || len(canonical) <= MaxFileBytes {
		t.Fatalf("file has %d bytes and canonical encoding has %d bytes, limit %d", len(file), len(canonical), MaxFileBytes)
	}
	var decoded Config
	if err := jsonv2.Unmarshal(file, &decoded, json.DefaultOptionsV1()); err != nil {
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
	data, err := jsonv2.Marshal(value, json.DefaultOptionsV1())
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
	id := strings.Repeat("x", MaxIDLength)
	if got, want := quoteID(id), `"`+id+`"`; got != want {
		t.Fatalf("quoteID(%d bytes) = %s, want %s", len(id), got, want)
	}
	if got, want := quoteID(id+"y"), `"`+id+`"...`; got != want {
		t.Fatalf("quoteID(%d bytes) = %s, want %s", len(id)+1, got, want)
	}
	// A rune that crosses the limit is removed whole.
	cut := strings.Repeat("x", MaxIDLength-1) + "é"
	if got, want := quoteID(cut), `"`+strings.Repeat("x", MaxIDLength-1)+`"...`; got != want {
		t.Fatalf("quoteID(%q) = %s, want %s", cut, got, want)
	}
	// Invalid UTF-8 does not remove more than one rune of bytes.
	invalid := strings.Repeat("\x80", 2*MaxIDLength)
	if got := quoteID(invalid); len(got) < 4*(MaxIDLength-utf8.UTFMax) {
		t.Fatalf("quoteID(invalid) = %s, too short", got)
	}
}

func TestValidateMapBackground(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		background     *MapBackground
		noGeo, wantErr bool
	}{
		{name: "absent", noGeo: true},
		{name: "opaque", background: &MapBackground{Provider: "osm", Opacity: 1}},
		{name: "transparent", background: &MapBackground{Provider: "osm", Opacity: 0}},
		{name: "no geo", background: &MapBackground{Provider: "osm", Opacity: .45}, noGeo: true, wantErr: true},
		{name: "unknown provider", background: &MapBackground{Provider: "custom", Opacity: .45}, wantErr: true},
		{name: "negative opacity", background: &MapBackground{Provider: "osm", Opacity: -.01}, wantErr: true},
		{name: "large opacity", background: &MapBackground{Provider: "osm", Opacity: 1.01}, wantErr: true},
		{name: "nan opacity", background: &MapBackground{Provider: "osm", Opacity: math.NaN()}, wantErr: true},
		{name: "infinite opacity", background: &MapBackground{Provider: "osm", Opacity: math.Inf(1)}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			config.Map = test.background
			if !test.noGeo {
				config.Geo = testGeo(0, 0)
			}
			if err := validateMap(config.Map, config.Geo); (err != nil) != test.wantErr {
				t.Fatalf("map metadata: %v, want error %t", err, test.wantErr)
			}
			if err := Validate(config); (err != nil) != test.wantErr {
				t.Fatalf("Validate map: %v, want error %t", err, test.wantErr)
			}
		})
	}
	data := mustJSON(t, Default())
	if bytes.Contains(data, []byte(`"map"`)) {
		t.Fatal("absent map was encoded")
	}
}

func TestProjectRefusesEarlierVersions(t *testing.T) {
	t.Parallel()
	for _, version := range []int{2, 3, 4, 5} {
		config := Default()
		config.Version = version
		err := Validate(config)
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("project version %d is not supported", version)) {
			t.Fatalf("version %d: %v", version, err)
		}
		raw := fmt.Appendf(nil, `{"version":%d}`, version)
		got := Default()
		if err := jsonv2.Unmarshal(raw, &got, json.DefaultOptionsV1()); err == nil || !strings.Contains(err.Error(), "use version 1") {
			t.Fatalf("decode version %d: %v", version, err)
		}
	}
	for _, version := range []int{0, 6} {
		config := Default()
		config.Version = version
		if err := Validate(config); err == nil || !strings.Contains(err.Error(), "project version must be 1") {
			t.Fatalf("version %d: %v", version, err)
		}
	}
}

// TestValidateCheckOrder pins the order of the checks of Validate. The
// first check that fails gives the refusal.
func TestValidateCheckOrder(t *testing.T) {
	t.Parallel()
	want := []string{
		"validateVersion",
		"validateIncidentContract",
		"validateFaultContract",
		"validateEmergencyContract",
		"validateFormatBounds",
		"validateSharedRideSettings",
		"validateOnboardPickups",
		"validatePlatoonLimit",
		"validateGeoAndMap",
		"validateNames",
		"validateCharacters",
		"validateNetworkShape",
		"validateProjectDemand",
		"validateScenario",
		"validatePassengerStations",
	}
	var got []string
	for _, check := range projectChecks {
		name := runtime.FuncForPC(reflect.ValueOf(check).Pointer()).Name()
		got = append(got, name[strings.LastIndex(name, ".")+1:])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("project checks are %v, want %v", got, want)
	}
}

// TestValidateRefusalOrder pins the error that a project with two faults
// gets. Each case breaks two adjacent checks, in one check function or at
// the edge of two, and the earlier check gives the refusal.
func TestValidateRefusalOrder(t *testing.T) {
	t.Parallel()
	duplicatePath := func(config *Config) {
		lane := config.Network.Lanes[0]
		lane.ID = "copy"
		config.Network.Lanes = append(config.Network.Lanes, lane)
	}
	for _, test := range []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"name_before_nodes", func(config *Config) { config.Name, config.Network.Nodes = "", nil },
			fmt.Sprintf("project name must contain 1 to %d characters", MaxNameLength)},
		{"nodes_before_lanes", func(config *Config) { config.Network.Nodes, config.Network.Lanes = nil, nil },
			fmt.Sprintf("network must contain 1 to %d nodes", MaxNodes)},
		{"lanes_before_stations", func(config *Config) {
			config.Network.Lanes, config.Network.Stations = nil, config.Network.Stations[:1]
		}, fmt.Sprintf("network must contain 1 to %d lanes", MaxLanes)},
		{"stations_before_fleet", func(config *Config) { config.Network.Stations, config.Fleet = config.Network.Stations[:1], nil },
			fmt.Sprintf("network must contain 2 to %d stations", MaxStations)},
		{"fleet_before_party_limit", func(config *Config) { config.Fleet, config.SharedRidePartyLimit = nil, -1 },
			fmt.Sprintf("fleet must contain 1 to %d pods", MaxPods)},
		{"party_limit_before_mode", func(config *Config) { config.SharedRidePartyLimit, config.SharedRideMode = -1, "pickups" },
			fmt.Sprintf("shared ride party limit must be 1 to %d", sim.MaxSharedRideParties)},
		{"mode_before_stops", func(config *Config) { config.SharedRideMode, config.SharedRideMaxStops = "pickups", -1 },
			fmt.Sprintf("shared ride mode must be %q or %q", sim.SharedRideDestination, sim.SharedRideDropOffs)},
		{"stops_before_join", func(config *Config) { config.SharedRideMaxStops, config.SharedRideJoin = -1, "reassign" },
			fmt.Sprintf("shared ride stop limit must be 1 to %d", sim.MaxSharedRideStops)},
		{"join_before_onboard", func(config *Config) { config.SharedRideJoin, config.OnboardPickups = "reassign", true },
			fmt.Sprintf("shared ride join policy must be %q or %q", sim.SharedRideJoinUnassigned, sim.SharedRideJoinReassignExisting)},
		{"geo_before_map", func(config *Config) {
			config.Geo, config.Map = testGeo(0, 0), &MapBackground{Provider: "tiles"}
			config.Geo.Radius = 1
		}, fmt.Sprintf("geo radius must be %d meters", GeoRadius)},
		{"map_before_names", func(config *Config) {
			config.Map, config.Network.Stations[0].Name = &MapBackground{Provider: "osm"}, strings.Repeat("x", MaxNameLength+1)
		}, "a map background needs a geographic reference"},
		{"names_before_characters", func(config *Config) {
			config.Network.Lanes[0].ID, config.Name = strings.Repeat("_", MaxIDLength+1), "Pod\x01"
		}, fmt.Sprintf("lane IDs must contain 1 to %d characters", MaxIDLength)},
		{"project_name_before_ids", func(config *Config) { config.Name, config.Network.Nodes[0].ID = "Pod\x01", "a_b" },
			`name "Pod\x01" has a control character or is not UTF-8`},
		{"nodes_before_station_names", func(config *Config) {
			config.Network.Nodes[0].ID, config.Network.Stations[0].Name = "a_b", "Harbor\u0085"
		}, `ID "a_b" has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`},
		{"station_names_before_fleet", func(config *Config) {
			config.Network.Stations[0].Name, config.Fleet[0].ID = "Harbor\x7f", "0 1"
		}, `name "Harbor\x7f" has a control character or is not UTF-8`},
		{"fleet_before_profiles", func(config *Config) {
			profile := testDemandProfile()
			profile.Bands[0].Name = "AM\tpeak"
			config.Fleet[0].ID, config.DemandProfiles = "0 1", []DemandProfile{profile}
		}, `ID "0 1" has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`},
		{"profiles_before_rail_text", func(config *Config) {
			profile := testDemandProfile()
			profile.Bands[0].Name = "AM\tpeak"
			arrival := testRailArrival()
			arrival.ID = "train/1"
			config.DemandProfiles, config.RailArrivals = []DemandProfile{profile}, []RailArrival{arrival}
		}, `name "AM\tpeak" has a control character or is not UTF-8`},
		{"rail_text_before_demand_text", func(config *Config) {
			arrival := testRailArrival()
			arrival.ID = "train/1"
			config.RailArrivals, config.Demand.Destination = []RailArrival{arrival}, "harbor&"
		}, `ID "train/1" has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`},
		{"express_text_before_demand_text", func(config *Config) {
			config.ExpressServices, config.Demand.Destination = []sim.ExpressService{{ID: "a<b"}}, "harbor&"
		}, `ID "a<b" has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`},
		{"characters_before_lanes", func(config *Config) {
			duplicatePath(config)
			config.Demand.Destination = "harbor&"
		}, `ID "harbor&" has a character other than A-Z, a-z, 0-9, '.', '+' or '-'`},
		{"lanes_before_geometry", func(config *Config) {
			duplicatePath(config)
			config.Network.Nodes[0].Position.X = MaxCoordinate + 1
		}, fmt.Sprintf("lanes %q and %q have the same nodes and path", Default().Network.Lanes[0].ID, "copy")},
		{"profiles_before_rail", func(config *Config) {
			config.DemandProfiles = []DemandProfile{testDemandProfile(), testDemandProfile()}
			arrival := testRailArrival()
			arrival.Passengers = -1
			config.RailArrivals = []RailArrival{arrival}
		}, `invalid or duplicate demand profile "weekday"`},
		{"rail_before_demand", func(config *Config) {
			arrival := testRailArrival()
			arrival.Passengers = -1
			config.RailArrivals, config.Demand.PerMinute = []RailArrival{arrival}, 0
		}, `rail arrival "train" must offer 1 to 200 passengers`},
		{"demand_before_fleet", func(config *Config) { config.Demand.PerMinute, config.Fleet[1].ID = 0, "01" },
			"demand rate must be 1 to 120 orders per simulated minute"},
		{"fleet_before_express", func(config *Config) {
			config.Fleet[1].ID = config.Fleet[0].ID
			config.ExpressServices = []sim.ExpressService{{}}
		}, `invalid project scenario: invalid or duplicate pod "01"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			test.edit(&config)
			if err := Validate(config); err == nil || err.Error() != test.want {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}
