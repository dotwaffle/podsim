package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func couplingProject(t *testing.T, express bool) Config {
	t.Helper()
	config := Default()
	if express {
		config = expressProject(t)
	}
	config.Version = CouplingVersion
	config.CouplingContract = sim.CompactPairV1CouplingContract
	compact, err := sim.NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	// These authored records use the native geometry fixture's dimensions.
	// They declare geometry, without creating a runtime train or a service claim.
	config.Network.Nodes = append(config.Network.Nodes,
		sim.Node{ID: "coupling-a", Position: sim.Point{Y: 1000}},
		sim.Node{ID: "coupling-b", Position: sim.Point{X: 200, Y: 1000}},
		sim.Node{ID: "coupling-c", Position: sim.Point{X: 400, Y: 1000}},
	)
	config.Network.Lanes = append(config.Network.Lanes,
		sim.Lane{ID: "coupling-ab", From: "coupling-a", To: "coupling-b", SpeedLimit: 14, VehicleClasses: compact},
		sim.Lane{ID: "coupling-bc", From: "coupling-b", To: "coupling-c", SpeedLimit: 7, VehicleClasses: compact},
	)
	config.CouplingSites = []sim.CouplingSite{
		{ID: "assembly", LaneID: "coupling-ab", StartMeters: 20, EndMeters: 100, RearStagingMeters: 40, FrontStagingMeters: 52},
		{ID: "split", LaneID: "coupling-bc", StartMeters: 50, EndMeters: 140, RearStagingMeters: 70, FrontStagingMeters: 82},
	}
	config.CouplingCorridors = []sim.CouplingCorridor{{ID: "corridor", AssemblySiteID: "assembly", SplitSiteID: "split", LaneIDs: []string{"coupling-ab", "coupling-bc"}}}
	return config
}

func TestCouplingProjectRoundTrip(t *testing.T) {
	for _, express := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			config := couplingProject(t, express)
			config.CouplingEnabled = enabled
			if err := Validate(config); err != nil {
				t.Fatal(err)
			}
			raw, err := jsonv2.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			for _, decode := range []func([]byte, any) error{json.Unmarshal, func(data []byte, value any) error { return jsonv2.Unmarshal(data, value) }} {
				var got Config
				if err := decode(raw, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(config, got) {
					t.Fatal("coupling project changed authored records")
				}
				if err := Validate(got); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Contains(raw, []byte(`"laneId":"coupling-ab"`)) || bytes.Contains(raw, []byte(`"LaneID"`)) {
				t.Fatal("descriptor did not use canonical member names")
			}
			if bytes.Contains(raw, []byte(`"couplingEnabled":true`)) != enabled {
				t.Fatal("enable flag changed")
			}
		}
	}
	for _, empty := range []bool{false, true} {
		config := Default()
		config.Version = CouplingVersion
		config.CouplingContract = sim.CompactPairV1CouplingContract
		config.CouplingEnabled = true
		if empty {
			config.CouplingSites = []sim.CouplingSite{}
			config.CouplingCorridors = []sim.CouplingCorridor{}
		}
		if err := Validate(config); err != nil {
			t.Fatal("known marker without formation paths", err)
		}
		raw, err := jsonv2.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		var got Config
		if err := jsonv2.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, config) {
			t.Fatalf("empty registry round trip: %v", err)
		}
	}
}

func couplingHistoricalBankProject() Config {
	config := Default()
	config.Version = BankVersion
	config.Network = sim.BankExample()
	config.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
	return config
}

func TestCouplingHistoricalBytesAndInverseEdits(t *testing.T) {
	service := Default()
	service.Version = ServiceVersion
	for _, config := range []Config{Default(), couplingHistoricalBankProject(), service, expressProject(t)} {
		before, err := jsonv2.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(before, []byte("coupling")) {
			t.Fatal("historical writer added coupling members")
		}
		changed := Clone(config)
		changed.Version = CouplingVersion
		changed.CouplingContract = sim.CompactPairV1CouplingContract
		changed.CouplingEnabled = true
		if validationErr := Validate(changed); validationErr != nil {
			t.Fatal(validationErr)
		}
		changed.Version = config.Version
		changed.CouplingContract = ""
		changed.CouplingEnabled = false
		after, err := jsonv2.Marshal(changed)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("inverse format edits changed historical bytes", err)
		}
		var decoded Config
		if err := jsonv2.Unmarshal(before, &decoded); err != nil {
			t.Fatal(err)
		}
		if err := Validate(decoded); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCouplingProjectNativeGeometryGuards(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"missing marker", func(c *Config) { c.CouplingContract = "" }},
		{"unknown marker", func(c *Config) { c.CouplingContract = "unknown" }},
		{"missing sites", func(c *Config) { c.CouplingSites = nil }},
		{"missing corridors", func(c *Config) { c.CouplingCorridors = nil }},
		{"orphan", func(c *Config) { c.CouplingCorridors[0].SplitSiteID = "missing" }},
		{"wrong order family", func(c *Config) { c.OrderContract = "unknown" }},
		{"mixed corridor class", func(c *Config) {
			classes, _ := sim.NewClassSet("compact", "group")
			c.Network.Lanes[len(c.Network.Lanes)-1].VehicleClasses = classes
		}},
		{"unknown path", func(c *Config) { c.CouplingCorridors[0].LaneIDs[0] = "missing" }},
		{"invalid number", func(c *Config) { c.CouplingSites[0].StartMeters = math.Inf(1) }},
		{"invalid UTF8", func(c *Config) { c.CouplingSites[0].ID = string([]byte{255}) }},
		{"long ID", func(c *Config) { c.CouplingSites[0].ID = strings.Repeat("x", maxIDLength+1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := couplingProject(t, false)
			test.change(&config)
			if err := Validate(config); err == nil {
				t.Fatal("invalid coupling project passed native validation")
			}
			if test.name == "invalid number" || test.name == "invalid UTF8" {
				return
			}
			raw, err := jsonv2.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			got := Default()
			before := Clone(got)
			if err := jsonv2.Unmarshal(raw, &got); err == nil {
				t.Fatal("public decoder accepted invalid coupling geometry")
			}
			if !reflect.DeepEqual(got, before) {
				t.Fatal("invalid geometry changed prior project")
			}
		})
	}
}

func TestCouplingCloneOwnsRecords(t *testing.T) {
	config := couplingProject(t, false)
	before := couplingProject(t, false)
	clone := Clone(config)
	clone.CouplingSites[0].ID = "changed"
	clone.CouplingCorridors[0].ID = "changed"
	clone.CouplingCorridors[0].LaneIDs[0] = "changed"
	if !reflect.DeepEqual(config, before) {
		t.Fatal("clone aliases coupling records")
	}
	config.CouplingSites = []sim.CouplingSite{}
	config.CouplingCorridors = []sim.CouplingCorridor{}
	clone = Clone(config)
	if clone.CouplingSites == nil || clone.CouplingCorridors == nil {
		t.Fatal("clone discarded explicit empty arrays")
	}
}

func TestCouplingProjectIndependentPolicies(t *testing.T) {
	for _, express := range []bool{false, true} {
		config := couplingProject(t, express)
		config.StationQueueSpacing = sim.StationQueueCompactV1
		config.StationBuffers = true
		config.PlatoonLimit = 2
		config.OnboardPickups = true
		config.SharedRidePartyLimit = 2
		config.SharedRideMode = sim.SharedRideDropOffs
		if err := Validate(config); err != nil {
			t.Fatal("independent order and policy settings", err)
		}
		for _, change := range []func(*Config){func(c *Config) { c.StationBuffers = false }, func(c *Config) { c.PlatoonLimit = 0 }, func(c *Config) { c.SharedRidePartyLimit = 1 }, func(c *Config) { c.SharedRideMode = sim.SharedRideDestination }} {
			invalid := Clone(config)
			change(&invalid)
			if err := Validate(invalid); err == nil {
				t.Fatal("version 5 bypassed existing queue or shared-ride prerequisites")
			}
		}
	}
	invalid := couplingProject(t, true)
	invalid.OrderContract = ""
	if err := Validate(invalid); err == nil {
		t.Fatal("Express vehicle admitted without independent order marker")
	}
	old := expressProject(t)
	old.StationQueueSpacing = sim.StationQueueCompactV1
	old.StationBuffers = true
	old.PlatoonLimit = 2
	if err := Validate(old); err == nil {
		t.Fatal("project 4 queue capability changed")
	}
}

func TestCouplingOldVersionTypedPresence(t *testing.T) {
	for _, version := range []int{1, 2, 3, 4} {
		for _, change := range []func(*Config){func(c *Config) { c.CouplingContract = sim.CompactPairV1CouplingContract }, func(c *Config) { c.CouplingEnabled = true }, func(c *Config) { c.CouplingSites = []sim.CouplingSite{} }, func(c *Config) { c.CouplingCorridors = []sim.CouplingCorridor{} }} {
			config := Default()
			if version == 2 {
				config = couplingHistoricalBankProject()
			}
			if version == 4 {
				config = expressProject(t)
			}
			config.Version = version
			change(&config)
			if err := Validate(config); err == nil {
				t.Fatal("historical project accepted coupling presence", version)
			}
		}
	}
}

func couplingRaw(t *testing.T) string {
	t.Helper()
	raw, err := jsonv2.Marshal(couplingProject(t, false))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCouplingExplicitZeroAndPartialUpdate(t *testing.T) {
	got := Default()
	if err := json.Unmarshal([]byte(`{"version":5,"couplingContract":"compact-pair-v1","couplingEnabled":false}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != CouplingVersion || got.CouplingEnabled || got.CouplingContract != sim.CompactPairV1CouplingContract {
		t.Fatal("explicit false changed")
	}
	if err := json.Unmarshal([]byte(`{"couplingEnabled":true}`), &got); err != nil || !got.CouplingEnabled {
		t.Fatal("version 5 partial update failed", err)
	}
	before := Clone(got)
	if err := json.Unmarshal([]byte(`{"version":3}`), &got); err == nil || !reflect.DeepEqual(before, got) {
		t.Fatal("implicit historical downgrade changed project", err)
	}
	// All positions are required. An explicit zero is a number, not omission.
	raw := strings.Replace(couplingRaw(t), `"startMeters":20`, `"startMeters":0`, 1)
	var zero Config
	if err := jsonv2.Unmarshal([]byte(raw), &zero); err != nil || zero.CouplingSites[0].StartMeters != 0 {
		t.Fatal("valid zero position changed", err)
	}
	if err := Validate(zero); err != nil {
		t.Fatal(err)
	}
}

func TestCouplingFileBound(t *testing.T) {
	got := Default()
	before := Clone(got)
	raw := fmt.Sprintf(`{"version":5,"couplingContract":"compact-pair-v1","name":%q}`, strings.Repeat("x", MaxFileBytes))
	if err := json.Unmarshal([]byte(raw), &got); err == nil || !reflect.DeepEqual(got, before) {
		t.Fatal("oversize parse changed project")
	}
}

func TestCouplingKnownEmptyRequiresMarker(t *testing.T) {
	config := Default()
	config.Version = CouplingVersion
	if err := Validate(config); err == nil {
		t.Fatal("complete project 5 accepted an absent marker with empty registries")
	}
	for _, decode := range []func([]byte, any) error{json.Unmarshal, func(data []byte, value any) error { return jsonv2.Unmarshal(data, value) }} {
		got := Default()
		before := Clone(got)
		if err := decode([]byte(`{"version":5}`), &got); err == nil {
			t.Fatal("public decoder accepted an absent marker with empty registries")
		}
		if !reflect.DeepEqual(got, before) {
			t.Fatal("missing marker changed prior project")
		}
	}
}
