package scenarios

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestLondonFullPreset(t *testing.T) {
	t.Parallel()
	config := LondonFull()
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if len(config.Network.Stations) != 272 || len(config.Network.Nodes) != 4988 || len(config.Network.Lanes) != 7778 || len(config.Fleet) != 287 {
		t.Fatalf("counts stations=%d nodes=%d lanes=%d fleet=%d", len(config.Network.Stations), len(config.Network.Nodes), len(config.Network.Lanes), len(config.Fleet))
	}
	berths := 0
	for _, station := range config.Network.Stations {
		berths += len(station.Berths)
	}
	if berths != 674 {
		t.Fatalf("berths=%d want 674", berths)
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
	for _, id := range []string{"940GZZLUBNK", "940GZZLUPAC", "940GZZLUHSD", "940GZZLUERC", "940GZZLUERB", "940GZZNEUGST", "940GZZBPSUST"} {
		if !ids[id] {
			t.Fatalf("missing site %s", id)
		}
	}
	for _, id := range []string{"940GZZLUMMT", "940GZZLUPAH", "940GZZLUHSC", "940GZZDLWIQ"} {
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
		{name: "default", change: func(*LondonOptions) {}, fleet: 287},
		{name: "one fewer pod", change: func(o *LondonOptions) { o.Pods = map[string]int{"940GZZLUWLO": 0} }, fleet: 286},
		{name: "unknown site", change: func(o *LondonOptions) { o.Berths["unknown"] = 2 }, wantError: "unknown London station"},
		{name: "too many pods", change: func(o *LondonOptions) { o.StationPods = 2 }, wantError: "fleet has"},
		{name: "too many nodes", change: func(o *LondonOptions) { o.Berths = nil; o.StationBerths = 4 }, wantError: "nodes"},
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
	rebuilt, err := londonFullConfig(source, capacity, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, rebuilt) {
		t.Fatal("uncached full generation differs")
	}
}

func TestLondonCentralMirroredBytesPinned(t *testing.T) {
	t.Parallel()
	config := LondonCentral()
	if config.Name != "LondonCentral" {
		t.Fatal("central display name changed")
	}
	config.Name = "Central London Underground-derived PRT"
	raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	const want = "d9c3685e8472be1b876b84e366bd905d741101f0bb015a0f155a570130eea7db"
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != want {
		t.Fatalf("central project hash=%s want %s", got, want)
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
	if _, err := londonFullConfig(source, capacity, true); err == nil || !strings.Contains(err.Error(), "validate LondonFull scenario") {
		t.Fatalf("invalid project error = %v", err)
	}
}
