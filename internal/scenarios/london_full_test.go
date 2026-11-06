package scenarios

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestLondonFullPreset(t *testing.T) {
	t.Parallel()
	config := LondonFull()
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if len(config.Network.Stations) != 312 || len(config.Network.Nodes) != 5740 || len(config.Network.Lanes) != 9024 || len(config.Fleet) != 327 {
		t.Fatalf("counts stations=%d nodes=%d lanes=%d fleet=%d", len(config.Network.Stations), len(config.Network.Nodes), len(config.Network.Lanes), len(config.Fleet))
	}
	berths := 0
	for _, station := range config.Network.Stations {
		berths += len(station.Berths)
	}
	if berths != 770 {
		t.Fatalf("berths=%d want 770", berths)
	}
	if config.Demand.PerMinute != 10 || config.Demand.Band != "am-peak" || config.Demand.Profile != londonFullDemandProfileID {
		t.Fatalf("demand=%+v", config.Demand)
	}
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	if err = layoutError(config.Network, auditLondonLayout(config.Network, newLondonAuditInput(source))); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, station := range config.Network.Stations {
		ids[station.ID] = true
	}
	for _, id := range []string{"940GZZLUBNK", "940GZZLUPAC", "940GZZLUHSD", "940GZZLUERC", "940GZZLUERB", "940GZZNEUGST", "940GZZBPSUST", "940GZZDLCAN", "940GZZLUCYF", "940GZZDLPOP", "940GZZDLWLA", "940GZZDLLEW", "940GZZDLBEC", "940GZZDLSIT"} {
		if !ids[id] {
			t.Fatalf("missing site %s", id)
		}
	}
	for _, id := range []string{"940GZZLUMMT", "940GZZLUPAH", "940GZZLUHSC", "940GZZDLWIQ", "940GZZDLBNK", "940GZZDLCGT", "940GZZDLSTD", "940GZZDLWHM"} {
		if ids[id] {
			t.Fatalf("unexpected site %s", id)
		}
	}
	raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LondonFull project bytes=%d", len(raw))
}

func TestLondonFullCapacityMatchesAuditedAllocation(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/london_full_capacity.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]int
	if err := jsonv2.Unmarshal(data, &want, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	options := DefaultLondonFullOptions()
	if !reflect.DeepEqual(options.Berths, want) {
		t.Fatal("full capacity differs from audited largest-remainder allocation")
	}
	options.Berths["940GZZLUBNK"] = 99
	if DefaultLondonFullOptions().Berths["940GZZLUBNK"] != 2 {
		t.Fatal("default capacity map is shared")
	}
}

func TestLondonFullWith(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		change    func(*LondonOptions)
		wantError string
		fleet     int
	}{
		{name: "default", change: func(*LondonOptions) {}, fleet: 327},
		{name: "one fewer pod", change: func(o *LondonOptions) { o.Pods = map[string]int{"940GZZLUWLO": 0} }, fleet: 326},
		{name: "unknown site", change: func(o *LondonOptions) { o.Berths["unknown"] = 2 }, wantError: "unknown London station"},
		{name: "too many pods", change: func(o *LondonOptions) { o.Berths = nil; o.StationBerths, o.StationPods = 4, 4 }, wantError: "fleet has"},
		{name: "too many nodes", change: func(o *LondonOptions) { o.Berths = nil; o.StationBerths = 12 }, wantError: "nodes"},
		{name: "Bank geometry", change: func(o *LondonOptions) { o.Berths["940GZZLUBNK"] = 4 }, wantError: "layout conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := DefaultLondonFullOptions()
			test.change(&options)
			config, err := LondonFullWith(options)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error=%v want %s", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(config.Fleet) != test.fleet {
				t.Fatalf("fleet=%d", len(config.Fleet))
			}
			if test.name == "default" && !reflect.DeepEqual(config, LondonFull()) {
				t.Fatal("default full options differ from preset")
			}
			if test.name != "default" && !strings.HasSuffix(config.Name, customCapacitySuffix) {
				t.Fatal("custom capacity name missing")
			}
		})
	}
}

func TestLondonFullOwnedAndDeterministic(t *testing.T) {
	t.Parallel()
	config := LondonFull()
	original := LondonFull()
	before, err := jsonv2.Marshal(original, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	config.Network.Nodes[0].Position.X++
	config.Network.Stations[0].Berths[0].Node = "changed"
	config.Fleet[0].StationID = "changed"
	config.Geo.Latitude++
	config.DemandProfiles[0].Bands[0].Name = "changed"
	config.DemandProfiles[0].Flows[0].Weights[0]++
	config.DemandProfiles[0].Flows[0].From = "changed"
	after, err := jsonv2.Marshal(LondonFull(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("preset exposes cached slices or pointers")
	}
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := DefaultLondonFullOptions().resolve(source)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := londonFullConfig(source, capacity, true, londonFullDemandProfile())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, rebuilt) {
		t.Fatal("uncached full generation differs")
	}
}

// TestLondonCentralMirroredBytesPinned pins LondonCentral, and the
// opt-out layout at the hash of the preset before depots and Compact
// stations became the default.
func TestLondonCentralMirroredBytesPinned(t *testing.T) {
	t.Parallel()
	config := LondonCentral()
	if config.Name != "LondonCentral" {
		t.Fatal("central display name changed")
	}
	for _, test := range []struct {
		name   string
		config project.Config
		want   string
	}{
		{"default", config, "8019b7a6e988b3e31d31b0c0bc18a6798ae4d7969564fc5a8bd39f95e9c93516"},
		{"opt-out", londonVariantPresets[0].ordinary(t), "b3b8f3a1695804fcd398d71bedc4f4718e84e8b9448ce422109edfb8d7f12807"},
	} {
		test.config.Name = "Central London Underground-derived PRT"
		raw, err := jsonv2.Marshal(test.config, json.DefaultOptionsV1())
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != test.want {
			t.Fatalf("%s central project hash=%s want %s", test.name, got, test.want)
		}
	}
}

func TestLondonFullRejectsInvalidProject(t *testing.T) {
	t.Parallel()
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := DefaultLondonFullOptions().resolve(source)
	if err != nil {
		t.Fatal(err)
	}
	source.Stations[0].Name = ""
	if _, err := londonFullConfig(source, capacity, true, londonFullDemandProfile()); err == nil || !strings.Contains(err.Error(), "validate LondonFull scenario") {
		t.Fatalf("invalid project error = %v", err)
	}
}

func TestLondonBankMovementPathShape(t *testing.T) {
	t.Parallel()
	network := LondonFull().Network
	for _, lane := range network.Lanes {
		if lane.ID != "940GZZLUBNK-move-08-02" {
			continue
		}
		from, _ := network.Node(lane.From)
		to, _ := network.Node(lane.To)
		points := network.Polyline(lane)
		dx, dy := to.Position.X-from.Position.X, to.Position.Y-from.Position.Y
		for i := 1; i < len(points); i++ {
			if (points[i].X-points[i-1].X)*dx+(points[i].Y-points[i-1].Y)*dy <= 0 {
				t.Fatal("Bank movement folds")
			}
		}
		classes, err := sim.NewClassSet(string(sim.GroupClass))
		if err != nil {
			t.Fatal(err)
		}
		// A large incident lane makes preparation check the small movement too.
		incident := sim.Node{ID: "incident", Position: sim.Point{X: from.Position.X - 100, Y: from.Position.Y}}
		isolated := sim.Network{Nodes: []sim.Node{from, to, incident}, Lanes: []sim.Lane{lane, {ID: "large", From: incident.ID, To: from.ID, SpeedLimit: speedLimit, VehicleClasses: classes}}}
		if _, err := sim.PrepareNetwork(isolated); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("Bank movement missing")
}

func TestLondonBankMovementConnectivity(t *testing.T) {
	t.Parallel()
	network := LondonFull().Network
	arrivals, departures := map[string]bool{}, map[string]bool{}
	movements := map[string]sim.Lane{}
	for _, lane := range network.Lanes {
		if strings.HasPrefix(lane.ID, "940GZZLUBNK-move-") {
			arrivals[lane.From], departures[lane.To] = true, true
			movements[lane.ID] = lane
		}
	}
	if len(movements) != 64 || len(arrivals) != 8 || len(departures) != 8 {
		t.Fatalf("Bank connectivity: %d movements, %d arrivals, %d departures", len(movements), len(arrivals), len(departures))
	}
	lane := movements["940GZZLUBNK-move-08-02"]
	if lane.From != "london-link-100-a-arrival" || lane.To != "london-link-094-a-departure" {
		t.Fatalf("Bank movement endpoints: %+v", lane)
	}
	for a := range 8 {
		for d := range 8 {
			id := fmt.Sprintf("940GZZLUBNK-move-%02d-%02d", a+1, d+1)
			if _, ok := movements[id]; !ok {
				t.Fatalf("missing %s", id)
			}
		}
	}
	bank, _ := network.Station("940GZZLUBNK")
	for _, station := range network.Stations {
		if station.ID == "940GZZLUBNK" {
			continue
		}
		for _, pair := range [][2]string{{station.Exit, bank.Entry}, {bank.Exit, station.Entry}} {
			if _, err := network.Route(pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestLondonMovementProjection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		center sim.Point
		want   sim.Point
	}{
		{"positive", sim.Point{X: 3, Y: 30}, sim.Point{X: 3, Y: 30}},
		{"before arrival", sim.Point{X: -5, Y: 30}, sim.Point{X: 5, Y: 30}},
		{"past departure", sim.Point{X: 15, Y: 30}, sim.Point{X: 5, Y: 30}},
		{"zero start projection", sim.Point{Y: 30}, sim.Point{X: 5, Y: 30}},
		{"zero end projection", sim.Point{X: 10, Y: 30}, sim.Point{X: 5, Y: 30}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			portals := londonStationPortals{arrivals: []londonPortal{{node: "a"}}, departures: []londonPortal{{node: "b", position: sim.Point{X: 10}}}}
			var network sim.Network
			addLondonMovements(&network, "test", test.center, portals)
			lane := network.Lanes[0]
			want := lane
			want.Control = &test.want
			gotBytes, err := jsonv2.Marshal(lane)
			if err != nil {
				t.Fatal(err)
			}
			wantBytes, err := jsonv2.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotBytes, wantBytes) {
				t.Fatalf("generated lane=%s want %s", gotBytes, wantBytes)
			}
			segments := londonMovementSegments(test.center, portals)
			expected := []londonSegment{{from: sim.Point{}, to: test.want}, {from: test.want, to: sim.Point{X: 10}}}
			if !reflect.DeepEqual(segments, expected) {
				t.Fatalf("search segments=%+v want %+v", segments, expected)
			}
		})
	}
}

func TestLondonBankLayoutRefusal(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ berths, conflicts int }{{3, 14}, {4, 38}} {
		t.Run(strconv.Itoa(test.berths), func(t *testing.T) {
			t.Parallel()
			options := DefaultLondonFullOptions()
			options.Berths["940GZZLUBNK"] = test.berths
			_, err := LondonFullWith(options)
			want := fmt.Sprintf("layout conflict at station Bank and Monument (940GZZLUBNK): lane %q is nearer than 36 meters to St. Paul's lane %q (%d hard conflicts)", "940GZZLUBNK-03-arrival-link", "940GZZLUSPU-access-in", test.conflicts)
			if err == nil || err.Error() != want {
				t.Fatalf("layout refusal=%v want %s", err, want)
			}
		})
	}
}
