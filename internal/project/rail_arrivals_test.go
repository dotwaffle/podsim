package project

import (
	"encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func testRailArrival() RailArrival {
	return RailArrival{ID: "train", Station: "harbor", AtSeconds: 60, WalkingSeconds: 15, Passengers: 120,
		Destinations: []RailDestination{{Station: "market", Weight: 3}, {Station: "garden", Weight: 1}}}
}

func TestRailArrivalValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		edit func(*RailArrival)
		want string
	}{
		{"empty ID", func(a *RailArrival) { a.ID = "" }, "ID"},
		{"long ID", func(a *RailArrival) { a.ID = strings.Repeat("a", 65) }, "ID"},
		{"missing hub", func(a *RailArrival) { a.Station = "missing" }, "hub"},
		{"negative time", func(a *RailArrival) { a.AtSeconds = -1 }, "time"},
		{"late time", func(a *RailArrival) { a.AtSeconds = 86401 }, "time"},
		{"negative walk", func(a *RailArrival) { a.WalkingSeconds = -1 }, "walking"},
		{"long walk", func(a *RailArrival) { a.WalkingSeconds = 3601 }, "walking"},
		{"late release", func(a *RailArrival) { a.AtSeconds = 86400 }, "walking"},
		{"no passengers", func(a *RailArrival) { a.Passengers = 0 }, "passengers"},
		{"large burst", func(a *RailArrival) { a.Passengers = 201 }, "passengers"},
		{"no destinations", func(a *RailArrival) { a.Destinations = nil }, "destinations"},
		{"missing destination", func(a *RailArrival) { a.Destinations[0].Station = "missing" }, "destination"},
		{"hub destination", func(a *RailArrival) { a.Destinations[0].Station = a.Station }, "destination"},
		{"duplicate destination", func(a *RailArrival) { a.Destinations[1] = a.Destinations[0] }, "duplicate"},
		{"zero weight", func(a *RailArrival) { a.Destinations[0].Weight = 0 }, "weight"},
		{"large weight", func(a *RailArrival) { a.Destinations[0].Weight = 1_000_001 }, "weight"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			arrival := testRailArrival()
			tc.edit(&arrival)
			config.RailArrivals = []RailArrival{arrival}
			if err := Validate(config); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	for _, seconds := range []int{0, 86400} {
		arrival := testRailArrival()
		arrival.AtSeconds, arrival.WalkingSeconds, arrival.Passengers = seconds, 0, 200
		if err := validateRailArrivals([]RailArrival{arrival}, Default().Network); err != nil {
			t.Fatal(err)
		}
	}
	network := Default().Network
	network.Stations[0].ParkingOnly = true
	if err := validateRailArrivals([]RailArrival{testRailArrival()}, network); err == nil {
		t.Fatal("accepted parking hub")
	}
	network = Default().Network
	network.Stations[2].ParkingOnly = true
	if err := validateRailArrivals([]RailArrival{testRailArrival()}, network); err == nil {
		t.Fatal("accepted parking destination")
	}
}

func TestRailArrivalPlanLimits(t *testing.T) {
	t.Parallel()
	plan := make([]RailArrival, MaxRailArrivals)
	for index := range plan {
		plan[index] = testRailArrival()
		plan[index].ID, plan[index].AtSeconds = strconv.Itoa(index), index
		plan[index].WalkingSeconds, plan[index].Passengers = 0, 1
	}
	network := Default().Network
	if err := validateRailArrivals(plan, network); err != nil {
		t.Fatal(err)
	}
	if err := validateRailArrivals(append(slices.Clone(plan), testRailArrival()), network); err == nil {
		t.Fatal("accepted too many events")
	}
	plan[1].ID = plan[0].ID
	if err := validateRailArrivals(plan, network); err == nil {
		t.Fatal("accepted duplicate event ID")
	}
	plan = []RailArrival{testRailArrival(), testRailArrival()}
	plan[1].ID = "second"
	plan[0].Passengers, plan[1].Passengers = 100, 100
	if err := validateRailArrivals(plan, network); err != nil {
		t.Fatal(err)
	}
	plan[1].Passengers++
	if err := validateRailArrivals(plan, network); err == nil {
		t.Fatal("accepted coincident releases above cap")
	}
	plan[0].AtSeconds, plan[0].WalkingSeconds = 0, 0
	plan[1].AtSeconds, plan[1].WalkingSeconds = 0, 0
	if err := validateRailArrivals(plan, network); err == nil {
		t.Fatal("accepted zero-time coincident releases above cap")
	}
	plan = make([]RailArrival, 50)
	for index := range plan {
		plan[index] = testRailArrival()
		plan[index].ID, plan[index].AtSeconds, plan[index].Passengers = strconv.Itoa(index), index*600, 200
	}
	if err := validateRailArrivals(plan, network); err != nil {
		t.Fatal(err)
	}
	extra := testRailArrival()
	extra.ID, extra.AtSeconds, extra.Passengers = "extra", 40000, 1
	if err := validateRailArrivals(append(slices.Clone(plan), extra), network); err == nil {
		t.Fatal("accepted plan above passenger cap")
	}
	arrival := testRailArrival()
	arrival.Destinations = nil
	for index := range MaxRailDestinations {
		id := fmt.Sprintf("destination-%d", index)
		network.Stations = append(network.Stations, sim.Station{ID: id})
		arrival.Destinations = append(arrival.Destinations, RailDestination{Station: id, Weight: 1_000_000})
	}
	if err := validateRailArrivals([]RailArrival{arrival}, network); err != nil {
		t.Fatal(err)
	}
	arrival.Destinations = append(arrival.Destinations, RailDestination{Station: "market", Weight: 1})
	if err := validateRailArrivals([]RailArrival{arrival}, network); err == nil {
		t.Fatal("accepted too many destinations")
	}
}

func TestRailScheduleDeterminismAndOwnership(t *testing.T) {
	t.Parallel()
	plan := []RailArrival{testRailArrival(), testRailArrival(), testRailArrival()}
	plan[0].ID, plan[1].ID, plan[2].ID = "later", "first", "second"
	plan[0].AtSeconds = 100
	plan[1].AtSeconds, plan[1].WalkingSeconds = 0, 0
	plan[2].AtSeconds, plan[2].WalkingSeconds = 0, 0
	plan[1].Passengers, plan[2].Passengers = 100, 100
	if err := validateRailArrivals(plan, Default().Network); err != nil {
		t.Fatal(err)
	}
	offers := RailSchedule(plan, 17)
	if !reflect.DeepEqual(offers, RailSchedule(plan, 17)) || reflect.DeepEqual(offers, RailSchedule(plan, 18)) {
		t.Fatal("seeded schedule is not deterministic or seed independent")
	}
	for index, offer := range offers {
		event, tick, passenger := "first", int64(1), index+1
		if index >= 100 {
			event, passenger = "second", index-100+1
		}
		if index >= 200 {
			event, tick, passenger = "later", 115*sim.TicksPerSecond, index-200+1
		}
		if offer.Event != event || offer.Tick != tick || offer.Passenger != passenger || offer.From != "harbor" || offer.To != "market" && offer.To != "garden" {
			t.Fatalf("offer %d: %+v", index, offer)
		}
	}
	if want := RailSchedule(plan[2:], 17); !slices.Equal(offers[100:200], want) {
		t.Fatal("another event changed the event random stream")
	}
	config := Default()
	config.RailArrivals = []RailArrival{testRailArrival()}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var restored Config
	if err = json.Unmarshal(raw, &restored, json.RejectUnknownMembers(true)); err != nil || !reflect.DeepEqual(config, restored) {
		t.Fatalf("project round trip: %v", err)
	}
	cloned := Clone(config)
	cloned.RailArrivals[0].Station = "changed"
	cloned.RailArrivals[0].Destinations[0].Weight = 100
	if !reflect.DeepEqual(config.RailArrivals[0], testRailArrival()) {
		t.Fatal("project clone aliases rail plan")
	}
	offers[0].To = "changed"
	if RailSchedule(plan, 17)[0].To == "changed" {
		t.Fatal("offered schedule aliases caller storage")
	}
}

func TestRailPlanLargestEncoding(t *testing.T) {
	t.Parallel()
	hub := strings.Repeat("\x01", 64)
	network := sim.Network{Stations: []sim.Station{{ID: hub}}}
	var destinations []RailDestination
	for index := range MaxRailDestinations {
		id := strings.Repeat("\x01", 62) + fmt.Sprintf("%02d", index)
		network.Stations = append(network.Stations, sim.Station{ID: id})
		destinations = append(destinations, RailDestination{Station: id, Weight: 1_000_000})
	}
	plan := make([]RailArrival, MaxRailArrivals)
	for index := range plan {
		plan[index] = RailArrival{ID: strings.Repeat("\x01", 61) + fmt.Sprintf("%03d", index), Station: hub,
			AtSeconds: 80000 + index, WalkingSeconds: 3600, Passengers: 39, Destinations: destinations}
	}
	plan[len(plan)-1].Passengers += 16
	if err := validateRailArrivals(plan, network); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= MaxFileBytes {
		t.Fatalf("largest bounded plan has %d bytes, project cap %d", len(raw), MaxFileBytes)
	}
	if offers := RailSchedule(plan, 1); len(offers) != MaxRailPassengers {
		t.Fatalf("largest plan offers %d passengers", len(offers))
	}
	t.Logf("largest bounded plan: %d bytes, project cap %d", len(raw), MaxFileBytes)
}
