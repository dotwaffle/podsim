package scenarios

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
)

// largestLondonOptions returns the London options of the largest project in
// the tests: 3 berths at each passenger station, 200 berths at each ordinary
// Parking facility, no Compact stations, wide stations at station scale 1,
// and a 40 meter pitch. It has 3,822
// of the 4,000 nodes that the project limits allow. 4 berths at each
// passenger station would need 4,110 nodes.
func largestLondonOptions() LondonOptions {
	options := ordinaryLondonOptions(DefaultLondonCentralOptions())
	options.StationBerths, options.ParkingBerths, options.BerthPitch = 3, 200, 40
	options.StationScale, options.WideStations = 1, true
	return options
}

func TestLondonWithDefaultOptionsIsLondon(t *testing.T) {
	t.Parallel()
	config, err := LondonCentralWith(DefaultLondonCentralOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, LondonCentral()) {
		t.Fatal("LondonWith with the default options is not London")
	}
	// Overrides with the default values also give the preset.
	options := DefaultLondonCentralOptions()
	options.Berths = map[string]int{"940GZZLUKSX": londonStationBerths, "parking-west": londonDepotSlots}
	options.Pods = map[string]int{"940GZZLUKSX": londonStationPods}
	config, err = LondonCentralWith(options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, LondonCentral()) {
		t.Fatal("LondonWith with default overrides is not London")
	}
}

// TestLondonLateralMergesOption checks that the London presets set
// LateralMerges, and that NoLateralMerges gives the preset network without
// the field and with the name suffix.
func TestLondonLateralMergesOption(t *testing.T) {
	t.Parallel()
	if !LondonCentral().Network.LateralMerges || !LondonFull().Network.LateralMerges {
		t.Fatal("a London preset does not set LateralMerges")
	}
	options := DefaultLondonCentralOptions()
	if options.NoLateralMerges || DefaultLondonFullOptions().NoLateralMerges {
		t.Fatal("the default options turn off the lateral merges")
	}
	options.NoLateralMerges = true
	config, err := LondonCentralWith(options)
	if err != nil {
		t.Fatal(err)
	}
	if config.Network.LateralMerges || config.Name != "LondonCentral"+CustomCapacitySuffix {
		t.Fatalf("name %q, LateralMerges %v, want the suffix and no field", config.Name, config.Network.LateralMerges)
	}
	preset := LondonCentral()
	preset.Network.LateralMerges = false
	if !reflect.DeepEqual(config.Network, preset.Network) || !reflect.DeepEqual(config.Fleet, preset.Fleet) {
		t.Fatal("NoLateralMerges changed more than the network field")
	}
}

// TestLondonWideStationsOption checks that the London presets have tight
// stations, and that WideStations gives the bytes of each preset from before
// tight stations became the default, with the name suffix.
func TestLondonWideStationsOption(t *testing.T) {
	t.Parallel()
	if DefaultLondonCentralOptions().WideStations || DefaultLondonFullOptions().WideStations {
		t.Fatal("the default options give wide stations")
	}
	for _, test := range []struct {
		name    string
		options LondonOptions
		build   func(LondonOptions) (project.Config, error)
		sha256  string
	}{
		{name: "LondonCentral", options: DefaultLondonCentralOptions(), build: LondonCentralWith, sha256: "dd7e853675ceaeb087f746044bce8c11be2fd24ba27722d842c19bea7d06dc0b"},
		{name: "LondonFull", options: DefaultLondonFullOptions(), build: LondonFullWith, sha256: "04ec20877c010b35a7dd4f7e011b849774607e3d153221b6f2d23cddde2bba08"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.options.WideStations = true
			config, err := test.build(test.options)
			if err != nil {
				t.Fatal(err)
			}
			if config.Name != test.name+CustomCapacitySuffix {
				t.Fatalf("name %q, want the suffix", config.Name)
			}
			config.Name = test.name
			data, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != test.sha256 {
				t.Fatalf("wide project SHA-256 = %s, want %s", got, test.sha256)
			}
		})
	}
}

func TestLondonWithCapacity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*LondonOptions)
		// nodes is the node count, or zero for an error.
		nodes int
		// check checks the project.
		check func(*testing.T, project.Config)
		// err is a part of the error text.
		err string
	}{
		{
			name:   "third berth at King's Cross",
			change: func(options *LondonOptions) { options.Berths = map[string]int{"940GZZLUKSX": 3} },
			nodes:  1910,
			check: func(t *testing.T, config project.Config) {
				t.Helper()
				station, _ := config.Network.Station("940GZZLUKSX")
				if len(station.Berths) != 3 {
					t.Fatalf("King's Cross has %d berths, want 3", len(station.Berths))
				}
			},
		},
		{
			name: "third berth at Embankment with wide stations at station scale 1",
			change: func(options *LondonOptions) {
				options.Berths, options.StationScale, options.WideStations = map[string]int{"940GZZLUEMB": 3}, 1, true
			},
			err: "layout conflict at station Embankment (940GZZLUEMB)",
		},
		{
			name: "three berths at each station with wide stations at station scale 1",
			change: func(options *LondonOptions) {
				options.StationBerths, options.StationScale, options.WideStations = 3, 1, true
			},
			err: "layout conflict at station Embankment (940GZZLUEMB)",
		},
		{
			name:   "largest",
			change: func(options *LondonOptions) { *options = largestLondonOptions() },
			nodes:  3822,
		},
		{
			name: "24 Parking berths",
			change: func(options *LondonOptions) {
				*options = ordinaryLondonOptions(*options)
				options.ParkingBerths = 24
			},
			nodes: 1950,
			check: func(t *testing.T, config project.Config) {
				t.Helper()
				station, _ := config.Network.Station("parking-north")
				if len(station.Berths) != 24 {
					t.Fatalf("North London Parking has %d berths, want 24", len(station.Berths))
				}
			},
		},
		{
			name: "past the node limit",
			change: func(options *LondonOptions) {
				*options = ordinaryLondonOptions(*options)
				options.StationBerths, options.ParkingBerths = 40, 200
			},
			err: "London network needs 14478 nodes, more than the limit of 12000",
		},
		{
			name: "pods per station",
			change: func(options *LondonOptions) {
				options.StationPods, options.ParkingPods = 2, 0
				options.Pods = map[string]int{"parking-east": 10, "940GZZLUKSX": 0}
			},
			nodes: 1907,
			check: func(t *testing.T, config project.Config) {
				t.Helper()
				count := make(map[string]int)
				for _, placement := range config.Fleet {
					count[placement.StationID]++
				}
				if len(config.Fleet) != 95*2+10 || count["940GZZLUBST"] != 2 || count["940GZZLUKSX"] != 0 || count["parking-east"] != 10 || count["parking-west"] != 0 {
					t.Fatalf("got %d pods: %v", len(config.Fleet), count)
				}
			},
		},
		{
			name: "shorter pitch of wide stations",
			change: func(options *LondonOptions) {
				options.StationBerths, options.BerthPitch, options.WideStations = 3, 50, true
			},
			nodes: 2174,
		},
		{
			name: "Parking pods past the berths",
			change: func(options *LondonOptions) {
				options.Berths = map[string]int{"parking-east": 2}
			},
			err: "East London Parking (parking-east) has 6 pods, want 0 to its 4 berths",
		},
		{
			name:   "unknown station",
			change: func(options *LondonOptions) { options.Pods = map[string]int{"parking-south": 1} },
			err:    `unknown London station ID "parking-south"`,
		},
		{
			name: "no berth",
			change: func(options *LondonOptions) {
				options.Berths = map[string]int{"940GZZLUKSX": 0}
				options.Pods = map[string]int{"940GZZLUKSX": 0}
			},
			err: "King's Cross St. Pancras (940GZZLUKSX) has 0 berths, want 1 to 200",
		},
		{
			name:   "too many berths",
			change: func(options *LondonOptions) { options.Berths = map[string]int{"940GZZLUKSX": 201} },
			err:    "has 201 berths, want 1 to 200",
		},
		{
			name:   "negative pods",
			change: func(options *LondonOptions) { options.StationPods = -1 },
			err:    "has -1 pods",
		},
		{
			name:   "no pod",
			change: func(options *LondonOptions) { options.StationPods, options.ParkingPods = 0, 0 },
			err:    "fleet has 0 pods, want 1 to 1000",
		},
		{
			name: "too many pods",
			change: func(options *LondonOptions) {
				options.StationBerths, options.StationPods, options.ParkingPods = 11, 11, 12
				options.CompactStations = nil
			},
			err: "fleet has 1092 pods, want 1 to 1000",
		},
		{
			name:   "pitch below the floor",
			change: func(options *LondonOptions) { options.BerthPitch = 24 },
			err:    "berth pitch must be a finite distance of at least 25 meters",
		},
		{
			name:   "pitch not a number",
			change: func(options *LondonOptions) { options.BerthPitch = math.NaN() },
			err:    "berth pitch",
		},
		{
			name:   "infinite pitch",
			change: func(options *LondonOptions) { options.BerthPitch = math.Inf(1) },
			err:    "berth pitch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := DefaultLondonCentralOptions()
			test.change(&options)
			config, err := LondonCentralWith(options)
			if test.err != "" {
				if err == nil || !strings.Contains(err.Error(), test.err) {
					t.Fatalf("LondonWith error = %v, want an error with %q", err, test.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := project.Validate(config); err != nil {
				t.Fatal(err)
			}
			if len(config.Network.Nodes) != test.nodes {
				t.Fatalf("got %d nodes, want %d", len(config.Network.Nodes), test.nodes)
			}
			if !strings.HasSuffix(config.Name, " (custom capacity)") {
				t.Fatalf("project name %q has no custom capacity suffix", config.Name)
			}
			if test.check != nil {
				test.check(t, config)
			}
		})
	}
}

// TestLargestLondonFitsTheFileLimits checks the largest London project of
// the tests against the file limits. cmd/scenario writes indented JSON, and
// the serve and compare commands read a -project file of at most
// project.MaxFileBytes. The editor sends the compact form in one gzip
// command, and the server accepts at most session.MaxInflatedCommandBytes
// of command JSON after decompression.
func TestLargestLondonFitsTheFileLimits(t *testing.T) {
	t.Parallel()
	config, err := LondonCentralWith(largestLondonOptions())
	if err != nil {
		t.Fatal(err)
	}
	indented, err := jsonv2.Marshal(config, json.DefaultOptionsV1(), jsontext.WithIndent("  "))
	if err != nil {
		t.Fatal(err)
	}
	compact, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("largest London: %d nodes, %d lanes, %d indented bytes, %d compact bytes", len(config.Network.Nodes), len(config.Network.Lanes), len(indented), len(compact))
	if len(indented) >= project.MaxFileBytes || len(compact) >= session.MaxInflatedCommandBytes {
		t.Fatalf("largest London has %d indented and %d compact bytes, want less than %d and %d", len(indented), len(compact), project.MaxFileBytes, session.MaxInflatedCommandBytes)
	}
}
