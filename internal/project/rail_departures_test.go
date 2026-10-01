package project

import (
	"encoding/json/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func testRailDeparture() RailDeparture {
	return RailDeparture{ID: "train", Station: "harbor", AtSeconds: 600, WalkingSeconds: 30,
		RequestFromSeconds: 10, RequestUntilSeconds: 100, Passengers: 120,
		Origins: []RailOrigin{{Station: "market", Weight: 3}, {Station: "garden", Weight: 1}}}
}

func TestRailDepartureValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func(*RailDeparture)
		want string
	}{
		{"empty ID", func(d *RailDeparture) { d.ID = "" }, "ID"},
		{"long ID", func(d *RailDeparture) { d.ID = strings.Repeat("x", 65) }, "ID"},
		{"missing hub", func(d *RailDeparture) { d.Station = "missing" }, "hub"},
		{"parking hub", func(d *RailDeparture) { d.Station = "parking" }, "hub"},
		{"zero departure", func(d *RailDeparture) { d.AtSeconds = 0 }, "time"},
		{"late departure", func(d *RailDeparture) { d.AtSeconds = 86401 }, "time"},
		{"negative walk", func(d *RailDeparture) { d.WalkingSeconds = -1 }, "walking"},
		{"long walk", func(d *RailDeparture) { d.WalkingSeconds = 3601 }, "walking"},
		{"negative start", func(d *RailDeparture) { d.RequestFromSeconds = -1 }, "window"},
		{"reversed window", func(d *RailDeparture) { d.RequestFromSeconds = 101 }, "window"},
		{"no transfer margin", func(d *RailDeparture) { d.RequestUntilSeconds = 570 }, "window"},
		{"no passengers", func(d *RailDeparture) { d.Passengers = 0 }, "passengers"},
		{"large burst", func(d *RailDeparture) { d.Passengers = 201 }, "passengers"},
		{"no origins", func(d *RailDeparture) { d.Origins = nil }, "origins"},
		{"missing origin", func(d *RailDeparture) { d.Origins[0].Station = "missing" }, "origin"},
		{"parking origin", func(d *RailDeparture) { d.Origins[0].Station = "parking" }, "origin"},
		{"hub origin", func(d *RailDeparture) { d.Origins[0].Station = d.Station }, "origin"},
		{"duplicate origin", func(d *RailDeparture) { d.Origins[1] = d.Origins[0] }, "origin"},
		{"zero weight", func(d *RailDeparture) { d.Origins[0].Weight = 0 }, "weight"},
		{"large weight", func(d *RailDeparture) { d.Origins[0].Weight = 1_000_001 }, "weight"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			d := testRailDeparture()
			tc.edit(&d)
			config.RailDepartures = []RailDeparture{d}
			if err := Validate(config); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %s", err, tc.want)
			}
		})
	}
	for _, seconds := range []int{1, 86400} {
		d := testRailDeparture()
		d.AtSeconds, d.WalkingSeconds, d.RequestFromSeconds, d.RequestUntilSeconds = seconds, 0, 0, 0
		if err := validateRailServices(nil, []RailDeparture{d}, Default().Network); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDepartureOfferTickInterpolation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name               string
		from, until, count int
		want               []int64
	}{
		{"floor", 0, 1, 8, []int64{1, 8, 17, 25, 34, 42, 51, 60}},
		{"single", 3, 9, 1, []int64{180}},
		{"burst", 3, 3, 3, []int64{180, 180, 180}},
		{"zero burst", 0, 0, 3, []int64{1, 1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := testRailDeparture()
			d.RequestFromSeconds, d.RequestUntilSeconds, d.Passengers = tc.from, tc.until, tc.count
			offers := RailServicesSchedule(nil, []RailDeparture{d}, 7)
			got := make([]int64, len(offers))
			for i, offer := range offers {
				got[i] = offer.Tick
				if offer.Kind != "departure" || offer.Passenger != i+1 || offer.To != d.Station ||
					offer.DepartureTick != 36000 || offer.WalkingTicks != 1800 {
					t.Fatalf("bad outbound offer: %+v", offer)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ticks %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRailServicesCombinedCaps(t *testing.T) {
	t.Parallel()
	network := Default().Network
	a, d := testRailArrival(), testRailDeparture()
	a.AtSeconds, a.WalkingSeconds, a.Passengers = 0, 0, 199
	d.RequestFromSeconds, d.RequestUntilSeconds, d.Passengers = 0, 0, 1
	if err := validateRailServices([]RailArrival{a}, []RailDeparture{d}, network); err != nil {
		t.Fatal(err)
	}
	d.Passengers = 2
	if err := validateRailServices([]RailArrival{a}, []RailDeparture{d}, network); err == nil {
		t.Fatal("accepted rounded and normalized combined release above cap")
	}
	d.RequestFromSeconds, d.RequestUntilSeconds = 1, 1
	if err := validateRailServices([]RailArrival{a}, []RailDeparture{d}, network); err != nil {
		t.Fatal(err)
	}
	if err := validateRailServices(nil, []RailDeparture{d, d}, network); err == nil {
		t.Fatal("accepted duplicate departure ID")
	}
	plan := make([]RailDeparture, MaxRailArrivals)
	for i := range plan {
		plan[i] = testRailDeparture()
		plan[i].ID = strconv.Itoa(i)
		plan[i].Passengers = 1
		plan[i].RequestFromSeconds, plan[i].RequestUntilSeconds = i, i
	}
	if err := validateRailServices(nil, plan, network); err != nil {
		t.Fatal(err)
	}
	if err := validateRailServices([]RailArrival{a}, plan, network); err == nil {
		t.Fatal("accepted too many combined events")
	}
	plan = plan[:15]
	for i := range plan {
		plan[i].Passengers = 200
	}
	if err := validateRailServices(nil, plan, network); err != nil {
		t.Fatal(err)
	}
	extra := testRailDeparture()
	extra.ID = "extra"
	extra.Passengers = 1
	extra.RequestFromSeconds, extra.RequestUntilSeconds = 500, 500
	if err := validateRailServices(nil, append(slices.Clone(plan), extra), network); err == nil {
		t.Fatal("accepted outbound passenger cap overflow")
	}
	arrivals := make([]RailArrival, 35)
	for i := range arrivals {
		arrivals[i] = a
		arrivals[i].ID = strconv.Itoa(i)
		arrivals[i].Passengers = 200
		arrivals[i].AtSeconds = 1000 + i
		arrivals[i].WalkingSeconds = 0
	}
	if err := validateRailServices(arrivals, plan, network); err != nil {
		t.Fatal(err)
	}
	a.Passengers, a.AtSeconds = 1, 50000
	if err := validateRailServices(slices.Concat(arrivals, []RailArrival{a}), plan, network); err == nil {
		t.Fatal("accepted too many combined passengers")
	}

}

func TestRailServicesStreamsAndOwnership(t *testing.T) {
	t.Parallel()
	a, d := testRailArrival(), testRailDeparture()
	a.AtSeconds, a.WalkingSeconds, a.Passengers = 10, 0, 3
	d.Passengers = 20
	arrivals, departures := []RailArrival{a}, []RailDeparture{d}
	offers := RailServicesSchedule(arrivals, departures, 7)
	if !slices.Equal(offers, RailServicesSchedule(arrivals, departures, 7)) || slices.Equal(offers, RailServicesSchedule(arrivals, departures, 8)) {
		t.Fatal("service streams are not deterministic and seed-dependent")
	}
	var inbound []RailOffer
	var outbound []RailServiceOffer
	for _, offer := range offers {
		if offer.Kind == "arrival" {
			inbound = append(inbound, offer.RailOffer)
		} else {
			outbound = append(outbound, offer)
		}
	}
	if !slices.Equal(inbound, RailSchedule(arrivals, 7)) || !slices.Equal(outbound, RailServicesSchedule(nil, departures, 7)) {
		t.Fatal("adding another kind changed event choices")
	}
	if offers[0].Kind != "arrival" || offers[3].Kind != "departure" || offers[3].Tick != offers[0].Tick {
		t.Fatal("arrival/departure tie order changed")
	}
	unrelated := d
	unrelated.ID = "unrelated"
	withUnrelated := RailServicesSchedule(nil, append([]RailDeparture{unrelated}, departures...), 7)
	var original []RailServiceOffer
	for _, offer := range withUnrelated {
		if offer.Event == d.ID {
			original = append(original, offer)
		}
	}
	if !slices.Equal(original, outbound) {
		t.Fatal("unrelated departure changed choices")
	}
	config := Default()
	config.RailDepartures = departures
	clone := Clone(config)
	clone.RailDepartures[0].Station = "changed"
	clone.RailDepartures[0].Origins[0].Weight = 100
	if !reflect.DeepEqual(config.RailDepartures[0], d) {
		t.Fatal("project clone aliases departure storage")
	}
	raw, err := json.Marshal(config)
	var restored Config
	if err != nil {
		t.Fatal(err)
	}
	if decodeErr := json.Unmarshal(raw, &restored, json.RejectUnknownMembers(true)); decodeErr != nil || !reflect.DeepEqual(restored, config) {
		t.Fatalf("departure project round trip: %v", decodeErr)
	}
	raw, err = json.Marshal(Default())
	if err != nil || strings.Contains(string(raw), "railDepartures") {
		t.Fatalf("legacy encoding changed: %v", err)
	}
}

func TestDepartureStreamCannotMatchArrivalDomain(t *testing.T) {
	t.Parallel()
	a, d := testRailArrival(), testRailDeparture()
	a.ID = "rail-departure\x00" + d.ID
	a.AtSeconds, a.WalkingSeconds = 0, 0
	a.Destinations = []RailDestination{{Station: "market", Weight: 3}, {Station: "garden", Weight: 1}}
	if err := validateRailServices([]RailArrival{a}, []RailDeparture{d}, Default().Network); err != nil {
		t.Fatal(err)
	}
	for _, arrivalID := range []string{a.ID, d.ID} {
		a.ID = arrivalID
		inbound := RailSchedule([]RailArrival{a}, 7)
		outbound := RailServicesSchedule(nil, []RailDeparture{d}, 7)
		same := true
		for i := range inbound {
			same = same && inbound[i].To == outbound[i].From
		}
		if same {
			t.Fatalf("arrival ID %q shares the departure random stream", arrivalID)
		}
	}
}
