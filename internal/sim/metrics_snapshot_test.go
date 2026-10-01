package sim

import (
	"reflect"
	"strconv"
	"testing"
)

func TestMetricsRouteContext(t *testing.T) {
	t.Parallel()
	route := []Lane{
		{ID: "road", From: "a", To: "b", SpeedLimit: 14, Control: &Point{X: 20}},
		{ID: "access", From: "b", To: "c", StationID: "station", StationRole: StationBerthAccessRole},
		{ID: "road", From: "c", To: "berth", SeparationGroup: "grade"},
	}
	full := []Lane{{ID: "road", From: "a", To: "b"}, {ID: "access", From: "b", To: "c"}, {ID: "road", From: "c", To: "berth"}}
	for _, tc := range []struct {
		name  string
		pod   Pod
		empty bool
		want  []Lane
	}{
		{name: "stopped waiting", pod: Pod{LaneID: "access", WaitReason: TrackOccupied}, want: full},
		{name: "below threshold", pod: Pod{LaneID: "access", WaitReason: BerthOccupied, Speed: 0.009999}, want: full},
		{name: "at threshold", pod: Pod{LaneID: "access", WaitReason: TrackOccupied, Speed: 0.01}, want: full[2:]},
		{name: "moving", pod: Pod{LaneID: "access", WaitReason: TrackOccupied, Speed: 14}, want: full[2:]},
		{name: "no wait", pod: Pod{LaneID: "access"}, want: full[2:]},
		{name: "no lane", pod: Pod{WaitReason: TrackOccupied}, want: full[2:]},
		{name: "empty route", empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := route
			if tc.empty {
				input = nil
			}
			if got := metricsRouteContext(tc.pod, input); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("route context = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMetricsSnapshotEqualityAndOwnership(t *testing.T) {
	t.Parallel()
	s, err := New(Example(), "harbor")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		s.Step()
	}
	if err := s.RequestTrip("garden", "market"); err != nil {
		t.Fatal(err)
	}
	want, got := s.Snapshot(), s.MetricsSnapshot()
	context := got.Vehicles[0].Route
	for index := range want.Vehicles {
		want.Vehicles[index].Route = nil
		got.Vehicles[index].Route = nil
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("metrics snapshot changed a non-route field")
	}
	if len(got.Pending) == 0 || len(got.Vehicles[0].Riders) == 0 || len(got.Vehicles[0].Stops) == 0 || len(context) == 0 {
		t.Fatal("fixture needs pending requests, riders, stops, and route context")
	}
	before := s.ExportState()
	got.Pending[0].ID = -1
	got.Vehicles[0].Pod.ID = "changed"
	got.Vehicles[0].Riders[0].ID = -1
	got.Vehicles[0].Stops[0] = "changed"
	context[0].To = "changed"
	got.Berths[0].ID = "changed"
	if !reflect.DeepEqual(before, s.ExportState()) {
		t.Fatal("metrics snapshot exposes mutable storage")
	}
}

func BenchmarkMetricsSnapshot(b *testing.B) {
	route := make([]Lane, 128)
	for index := range route {
		route[index] = Lane{ID: strconv.Itoa(index), From: "a", To: "b", SpeedLimit: 14}
		if index%4 == 0 {
			route[index].Control = &Point{X: 50, Y: 10}
		}
	}
	s := &Simulation{vehicles: make([]vehicle, 287)}
	for index := range s.vehicles {
		s.vehicles[index].Route = route
		s.vehicles[index].Pod = Pod{ID: strconv.Itoa(index), Activity: Traveling, Speed: 14, LaneID: route[0].ID}
	}
	for _, tc := range []struct {
		name string
		take func() Snapshot
	}{
		{"full", s.Snapshot}, {"metrics", s.MetricsSnapshot},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			var state Snapshot
			for b.Loop() {
				state = tc.take()
			}
			if len(state.Vehicles) != 287 {
				b.Fatal("snapshot vehicle count differs")
			}
		})
	}
}
