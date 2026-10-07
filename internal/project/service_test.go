package project

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestServiceProjectPermitsIndependentBanks(t *testing.T) {
	banked := Default()
	banked.Network = sim.BankExample()
	banked.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
	for _, config := range []Config{Default(), banked} {
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
		if !reflect.DeepEqual(config, got) {
			t.Fatal("round trip changed the bank or project branches")
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
		{"compact", `{"fleet":[{"class":"compact"}]}`, true, true},
		{"empty class", `{"fleet":[{"class":""}]}`, true, false},
		{"null class", `{"fleet":[{"class":null}]}`, true, false},
		{"unknown class", `{"fleet":[{"class":"bus"}]}`, true, false},
		{"station classes", `{"network":{"stations":[{"vehicleClasses":["compact","group"]}]}}`, true, true},
		{"berth classes", `{"network":{"stations":[{"berths":[{"vehicleClasses":["legacy"]}]}]}}`, true, true},
		{"lane classes", `{"network":{"lanes":[{"vehicleClasses":["express"]}]}}`, true, true},
		{"null classes", `{"network":{"stations":[{"vehicleClasses":null}]}}`, true, false},
		{"empty classes", `{"network":{"lanes":[{"vehicleClasses":[]}]}}`, true, false},
		{"duplicate classes", `{"network":{"stations":[{"berths":[{"vehicleClasses":["compact","compact"]}]}]}}`, true, false},
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

func TestProjectServicePresenceFailsAtomically(t *testing.T) {
	for _, version := range []int{CurrentVersion, 2, 3} {
		for _, member := range []string{
			`"fleet":[{"class":"compact"}]`, `"fleet":[{"class":null}]`,
			`"expressServices":[]`, `"expressServices":null`,
			`"network":{"stations":[{"vehicleClasses":["legacy"]}]}`,
			`"network":{"lanes":[{"vehicleClasses":null}]}`,
		} {
			got := Default()
			want := Clone(got)
			raw := fmt.Sprintf(`{"version":%d,"name":"Changed",%s}`, version, member)
			err := jsonv2.Unmarshal([]byte(raw), &got, json.DefaultOptionsV1())
			refused := version != CurrentVersion || strings.Contains(member, "null")
			if (err != nil) != refused {
				t.Fatalf("decode %s: %v", raw, err)
			}
			if refused && !reflect.DeepEqual(got, want) {
				t.Fatal("failed project decode changed previous storage")
			}
		}
	}
}
