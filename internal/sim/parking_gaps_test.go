package sim

import (
	"reflect"
	"testing"
)

// TestParkingGaps limits one lane of BankExample to Compact and checks the
// gaps for Legacy pods. The lane into parking takes each route to parking,
// and the lane out of it takes each route from parking.
func TestParkingGaps(t *testing.T) {
	t.Parallel()
	compact, err := NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	gaps := func(to, from bool) []ParkingGap {
		var want []ParkingGap
		for _, berth := range []struct{ station, berth string }{{"origin", "origin-1"}, {"hub", "bank-a-1"}, {"hub", "bank-b-1"}} {
			want = append(want, ParkingGap{StationID: berth.station, BerthID: berth.berth, Class: LegacyClass, ToParking: to, FromParking: from})
		}
		return want
	}
	tests := []struct {
		name    string
		lane    string
		berth   string
		classes []VehicleClass
		want    []ParkingGap
	}{
		{name: "no limit", classes: []VehicleClass{LegacyClass, CompactClass}},
		{name: "into parking", lane: "parking-entry", classes: []VehicleClass{LegacyClass, CompactClass}, want: gaps(false, true)},
		{name: "out of parking", lane: "return-west", classes: []VehicleClass{LegacyClass}, want: gaps(true, false)},
		// The parking station accepts no Express pod, so Express has no
		// parking goal and no gap.
		{name: "no parking goal", lane: "parking-entry", classes: []VehicleClass{ExpressClass}},
		// A parking berth that refuses Legacy is no goal, so Legacy has no
		// goal and no gap.
		{name: "no parking berth", berth: "parking-1", classes: []VehicleClass{LegacyClass}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			network := BankExample()
			for index := range network.Lanes {
				if network.Lanes[index].ID == test.lane {
					network.Lanes[index].VehicleClasses = compact
				}
			}
			for index := range network.Stations {
				for berth := range network.Stations[index].Berths {
					if network.Stations[index].Berths[berth].ID == test.berth {
						network.Stations[index].Berths[berth].VehicleClasses = compact
					}
				}
			}
			if got := network.ParkingGaps(test.classes); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestParkingGapsDepot checks that the entry of a depot station is the
// parking goal of the class that the depot accepts. With the lanes into
// the entry limited to Compact, each passenger berth has a Legacy gap.
func TestParkingGapsDepot(t *testing.T) {
	t.Parallel()
	network := mixedDepotNetwork()
	if len(network.Depots) == 0 {
		t.Fatal("the fixture has no depot")
	}
	classes := []VehicleClass{LegacyClass, ExpressClass}
	if got := network.ParkingGaps(classes); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
	depot, _ := network.Station(network.Depots[0].Station)
	compact, err := NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	limited := 0
	for index := range network.Lanes {
		if network.Lanes[index].To == depot.Entry {
			network.Lanes[index].VehicleClasses = compact
			limited++
		}
	}
	passenger := 0
	for _, station := range network.Stations {
		if !station.ParkingOnly {
			passenger += len(station.Berths)
		}
	}
	got := network.ParkingGaps(classes)
	if limited == 0 || len(got) != passenger {
		t.Fatalf("%d lanes limited, got %d gaps for %d passenger berths: %+v", limited, len(got), passenger, got)
	}
	for _, gap := range got {
		if gap.Class != LegacyClass || gap.ToParking || !gap.FromParking {
			t.Fatalf("gap %+v", gap)
		}
	}
}
