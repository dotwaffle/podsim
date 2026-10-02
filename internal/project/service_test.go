package project

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestServiceProjectVersionPermitsIndependentBanks(t *testing.T) {
	banked := Default()
	banked.Network = sim.BankExample()
	banked.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
	for _, config := range []Config{Default(), banked} {
		config.Version = ServiceVersion
		if err := Validate(config); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		var got Config
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(config, got) {
			t.Fatal("version 3 changed the bank or project branches")
		}
	}
}

func TestProjectServiceRawShapes(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		present   bool
		valid     bool
	}{
		{"legacy", `{"version":1,"fleet":[{}]}`, false, true},
		{"compact", `{"fleet":[{"Class":"compact"}]}`, true, true},
		{"empty class", `{"fleet":[{"Class":""}]}`, true, false},
		{"null class", `{"fleet":[{"Class":null}]}`, true, false},
		{"unknown class", `{"fleet":[{"Class":"bus"}]}`, true, false},
		{"station classes", `{"network":{"Stations":[{"VehicleClasses":["compact","group"]}]}}`, true, true},
		{"berth classes", `{"network":{"Stations":[{"Berths":[{"VehicleClasses":["legacy"]}]}]}}`, true, true},
		{"lane classes", `{"network":{"Lanes":[{"VehicleClasses":["express"]}]}}`, true, true},
		{"null classes", `{"network":{"Stations":[{"VehicleClasses":null}]}}`, true, false},
		{"empty classes", `{"network":{"Lanes":[{"VehicleClasses":[]}]}}`, true, false},
		{"duplicate classes", `{"network":{"Stations":[{"Berths":[{"VehicleClasses":["compact","compact"]}]}]}}`, true, false},
		{"empty registry", `{"expressServices":[]}`, true, true},
		{"null registry", `{"expressServices":null}`, true, false},
		{"wrong registry type", `{"expressServices":{}}`, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			present, err := scanProjectService([]byte(test.raw))
			if (err == nil) != test.valid || test.valid && present != test.present {
				t.Fatalf("present=%t error=%v", present, err)
			}
		})
	}
}

func TestProjectServiceRawRegistryBound(t *testing.T) {
	for _, count := range []int{MaxExpressServices, MaxExpressServices + 1} {
		raw := `{"expressServices":[` + strings.TrimSuffix(strings.Repeat(`{},`, count), ",") + `]}`
		if _, err := scanProjectService([]byte(raw)); (err == nil) != (count <= MaxExpressServices) {
			t.Fatalf("registry count %d: %v", count, err)
		}
	}
}

func TestOldProjectRejectsServicePresenceUnchanged(t *testing.T) {
	for _, version := range []int{1, BankVersion} {
		for _, member := range []string{
			`"fleet":[{"Class":"compact"}]`, `"fleet":[{"Class":null}]`,
			`"expressServices":[]`, `"expressServices":null`,
			`"network":{"Stations":[{"VehicleClasses":["legacy"]}]}`,
			`"network":{"Lanes":[{"VehicleClasses":null}]}`,
		} {
			got := Default()
			want := Clone(got)
			raw := fmt.Sprintf(`{"version":%d,"name":"Changed",%s}`, version, member)
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatalf("old version accepted %s", raw)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("failed project decode changed previous storage")
			}
		}
	}
}
