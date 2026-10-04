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

func TestCouplingRawPresenceRequiresMarker(t *testing.T) {
	for _, family := range []struct {
		version int
		express bool
	}{{CurrentVersion, false}, {CurrentVersion, true}, {2, false}, {3, false}, {4, true}} {
		for _, member := range []string{
			`"couplingContract":"compact-pair-v1"`, `"couplingContract":null`, `"couplingContract":""`,
			`"couplingEnabled":true`, `"couplingEnabled":false`, `"couplingEnabled":null`,
			`"couplingSites":[]`, `"couplingSites":null`, `"couplingCorridors":[]`, `"couplingCorridors":null`,
		} {
			for _, rawMember := range []string{member, strings.ToUpper(strings.SplitN(member, ":", 2)[0]) + ":" + strings.SplitN(member, ":", 2)[1]} {
				if family.version == CurrentVersion && member == `"couplingContract":"compact-pair-v1"` {
					// The marker is the only member that a project can add alone.
					continue
				}
				for _, decode := range []func([]byte, any) error{json.Unmarshal, func(data []byte, value any) error { return jsonv2.Unmarshal(data, value) }} {
					got := Default()
					before := Clone(got)
					raw := fmt.Sprintf(`{"version":%d,"name":"Changed",%s}`, family.version, rawMember)
					if family.express {
						raw = strings.Replace(raw, `"name":`, `"orderContract":"express-v1","name":`, 1)
					}
					if err := decode([]byte(raw), &got); err == nil {
						t.Fatal("project accepted an unmarked or refused coupling member", raw)
					}
					if !reflect.DeepEqual(got, before) {
						t.Fatal("failed parse changed prior project", raw)
					}
				}
			}
		}
	}
}

func TestCouplingProjectRawShapesAtomic(t *testing.T) {
	valid := couplingRaw(t)
	for _, test := range []struct{ name, from, to string }{
		{"marker omitted", `"couplingContract":"compact-pair-v1",`, ""},
		{"marker null", `"couplingContract":"compact-pair-v1"`, `"couplingContract":null`},
		{"marker unknown", `"couplingContract":"compact-pair-v1"`, `"couplingContract":"other"`},
		{"marker duplicate", `"couplingContract":"compact-pair-v1"`, `"couplingContract":"compact-pair-v1","couplingContract":"compact-pair-v1"`},
		{"marker case duplicate", `"couplingContract":"compact-pair-v1"`, `"couplingContract":"compact-pair-v1","CouplingContract":"compact-pair-v1"`},
		{"enabled null", `"version":1`, `"version":1,"couplingEnabled":null`},
		{"enabled number", `"version":1`, `"version":1,"couplingEnabled":0`},
		{"enabled text", `"version":1`, `"version":1,"couplingEnabled":"false"`},
		{"enabled case duplicate", `"version":1`, `"version":1,"couplingEnabled":false,"CouplingEnabled":false`},
		{"sites null", `"couplingSites":[`, `"couplingSites":null,"ignored":[`},
		{"sites shape", `"couplingSites":[`, `"couplingSites":{},"ignored":[`},
		{"corridors null", `"couplingCorridors":[`, `"couplingCorridors":null,"ignored":[`},
		{"corridors shape", `"couplingCorridors":[`, `"couplingCorridors":{},"ignored":[`},
		{"site null", `"couplingSites":[`, `"couplingSites":[null,`},
		{"corridor null", `"couplingCorridors":[`, `"couplingCorridors":[null,`},
		{"missing ID", `"id":"assembly",`, ""},
		{"missing lane ID", `"laneId":"coupling-ab",`, ""},
		{"empty ID", `"id":"assembly"`, `"id":""`},
		{"long ID", `"id":"assembly"`, `"id":"` + strings.Repeat("x", maxIDLength+1) + `"`},
		{"unknown site member", `"id":"assembly"`, `"id":"assembly","unused":true`},
		{"case duplicate ID", `"id":"assembly"`, `"id":"assembly","ID":"assembly"`},
		{"missing number", `"startMeters":20,`, ""},
		{"missing end", `"endMeters":100,`, ""},
		{"missing front staging", `"frontStagingMeters":52,`, ""},
		{"missing rear staging", `,"rearStagingMeters":40`, ""},
		{"null number", `"startMeters":20`, `"startMeters":null`},
		{"text number", `"startMeters":20`, `"startMeters":"20"`},
		{"overflow number", `"startMeters":20`, `"startMeters":1e1000`},
		{"case duplicate number", `"startMeters":20`, `"startMeters":20,"StartMeters":20`},
		{"missing corridor ID", `"id":"corridor",`, ""},
		{"missing corridor site", `"assemblySiteId":"assembly",`, ""},
		{"missing split site", `"splitSiteId":"split",`, ""},
		{"missing path", `,"laneIds":["coupling-ab","coupling-bc"]`, ""},
		{"unknown corridor member", `"assemblySiteId":"assembly"`, `"assemblySiteId":"assembly","unused":true`},
		{"case duplicate corridor site", `"assemblySiteId":"assembly"`, `"assemblySiteId":"assembly","AssemblySiteID":"assembly"`},
		{"null path", `"laneIds":["coupling-ab","coupling-bc"]`, `"laneIds":null`},
		{"empty path", `"laneIds":["coupling-ab","coupling-bc"]`, `"laneIds":[]`},
		{"null lane ID", `"laneIds":["coupling-ab","coupling-bc"]`, `"laneIds":[null,"coupling-bc"]`},
		{"invalid UTF8", `"id":"assembly"`, "\"id\":\"\xff\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !strings.Contains(valid, test.from) {
				t.Fatal("fixture replacement did not match")
			}
			raw := strings.Replace(valid, test.from, test.to, 1)
			for _, decode := range []func([]byte, any) error{json.Unmarshal, func(data []byte, value any) error { return jsonv2.Unmarshal(data, value) }} {
				got := couplingProject(t, false)
				before := Clone(got)
				// A missing marker must not inherit one from a prior coupling project.
				if test.name == "marker omitted" {
					got = Default()
					before = Clone(got)
				}
				if err := decode([]byte(raw), &got); err == nil {
					t.Fatal("invalid coupling shape accepted")
				}
				if !reflect.DeepEqual(got, before) {
					t.Fatal("failed shape parse changed prior project")
				}
			}
		})
	}
}

func TestCouplingRegistryPrescanBounds(t *testing.T) {
	site := `{"id":"s","laneId":"lane","startMeters":0,"endMeters":100,"frontStagingMeters":52,"rearStagingMeters":40}`
	corridor := `{"id":"c","assemblySiteId":"a","splitSiteId":"b","laneIds":["lane"]}`
	for _, test := range []struct {
		field, value string
		limit        int
	}{
		{"couplingSites", site, sim.MaxCouplingSites}, {"couplingCorridors", corridor, sim.MaxCouplingCorridors},
	} {
		for _, count := range []int{test.limit, test.limit + 1} {
			raw := fmt.Sprintf(`{%q:[%s]}`, test.field, strings.TrimSuffix(strings.Repeat(test.value+",", count), ","))
			if _, err := scanProjectFields([]byte(raw)); (err == nil) != (count <= test.limit) {
				t.Fatalf("%s count %d: %v", test.field, count, err)
			}
		}
	}
	for _, count := range []int{MaxLanes, MaxLanes + 1} {
		raw := `{"couplingCorridors":[{"id":"c","assemblySiteId":"a","splitSiteId":"b","laneIds":[` + strings.TrimSuffix(strings.Repeat(`"lane",`, count), ",") + `]}]}`
		if _, err := scanProjectFields([]byte(raw)); (err == nil) != (count <= MaxLanes) {
			t.Fatalf("path count %d: %v", count, err)
		}
	}
}

func TestCouplingProjectRawBounds(t *testing.T) {
	for _, test := range []struct {
		name, format, element string
		limit                 int
		public                bool
	}{
		{"nodes", `"network":{"Nodes":[%s]}`, `{}`, MaxNodes, true},
		{"lanes", `"network":{"Lanes":[%s]}`, `{}`, MaxLanes, true},
		{"stations", `"network":{"Stations":[%s]}`, `{}`, MaxStations, true},
		{"fleet", `"fleet":[%s]`, `{}`, MaxPods, true},
		{"berths", `"network":{"Stations":[{"Berths":[%s]}]}`, `{}`, MaxBerths, true},
		{"banks", `"network":{"Stations":[{"Banks":[%s]}]}`, `{}`, sim.MaxStationBanks, false},
		{"bank berth IDs", `"network":{"Stations":[{"Banks":[{"BerthIDs":[%s]}]}]}`, `"b"`, MaxBerths, false},
		{"classes", `"network":{"Lanes":[{"VehicleClasses":[%s]}]}`, `"compact"`, 4, false},
		{"services", `"expressServices":[%s]`, `{}`, MaxExpressServices, true},
		{"profiles", `"demandProfiles":[%s]`, `{}`, MaxProfiles, true},
		{"bands", `"demandProfiles":[{"bands":[%s]}]`, `{}`, MaxBands, true},
		{"flows", `"demandProfiles":[{"flows":[%s]}]`, `{}`, MaxFlows, true},
		{"weights", `"demandProfiles":[{"flows":[{"weights":[%s]}]}]`, `0`, MaxBands, true},
		{"arrivals", `"railArrivals":[%s]`, `{}`, MaxRailArrivals, true},
		{"departures", `"railDepartures":[%s]`, `{}`, MaxRailArrivals, true},
		{"destinations", `"railArrivals":[{"destinations":[%s]}]`, `{}`, MaxRailDestinations, true},
		{"origins", `"railDepartures":[{"origins":[%s]}]`, `{}`, MaxRailDestinations, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, count := range []int{test.limit, test.limit + 1} {
				elements := strings.TrimSuffix(strings.Repeat(test.element+",", count), ",")
				raw := `{"version":1,"couplingContract":"compact-pair-v1",` + fmt.Sprintf(test.format, elements) + `}`
				if err := scanCouplingProjectBounds([]byte(raw)); (err == nil) != (count <= test.limit) {
					t.Fatalf("count %d: %v", count, err)
				}
				if !test.public {
					continue
				}
				got := Default()
				before := Clone(got)
				err := json.Unmarshal([]byte(raw), &got)
				// These rows test allocation bounds, not complete scenario validity.
				if (err == nil) != (count <= test.limit) {
					t.Fatalf("public count %d: %v", count, err)
				}
				if err != nil && !reflect.DeepEqual(got, before) {
					t.Fatal("oversize array changed prior project")
				}
			}
		})
	}
	for _, total := range []int{MaxRailArrivals, MaxRailArrivals + 1} {
		arrivals := strings.TrimSuffix(strings.Repeat(`{},`, 128), ",")
		departures := strings.TrimSuffix(strings.Repeat(`{},`, total-128), ",")
		raw := fmt.Sprintf(`{"version":1,"couplingContract":"compact-pair-v1","railArrivals":[%s],"railDepartures":[%s]}`, arrivals, departures)
		got := Default()
		before := Clone(got)
		err := json.Unmarshal([]byte(raw), &got)
		if (err == nil) != (total <= MaxRailArrivals) {
			t.Fatalf("combined rail count %d: %v", total, err)
		}
		if err != nil && !reflect.DeepEqual(got, before) {
			t.Fatal("oversize combined rail plan changed prior project")
		}
		// A partial update of a marked project has no coupling member.
		// The marker of the destination selects the bound scan.
		partial := fmt.Sprintf(`{"railArrivals":[%s],"railDepartures":[%s]}`, arrivals, departures)
		marked := couplingProject(t, false)
		if err := json.Unmarshal([]byte(partial), &marked); (err == nil) != (total <= MaxRailArrivals) {
			t.Fatalf("partial combined rail count %d: %v", total, err)
		}
	}
}
