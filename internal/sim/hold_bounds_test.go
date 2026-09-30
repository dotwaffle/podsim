package sim

import "testing"

func TestFinishingPodTravelBound(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		station     string
		seconds     int
		rule        FinishingPodWait
		rounding    bool
		wantHold    bool
		wantPruning bool
	}{
		{name: "distant finish", station: "garden", seconds: 30, wantPruning: true},
		{name: "near finish", station: "market", seconds: 1, wantHold: true},
		{name: "strict distant finish", station: "garden", seconds: 1, rule: FinishingPodWaitStrict, wantPruning: true},
		{name: "strict cutoff tie", station: "market", seconds: 30, rule: FinishingPodWaitStrict, wantHold: true},
		{name: "rounding at cutoff", station: "market", seconds: 30, rule: FinishingPodWaitStrict, rounding: true, wantHold: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: test.station}})
			if err != nil {
				t.Fatal(err)
			}
			s.finishingPodWait = test.rule
			busy := &s.vehicles[1]
			busy.Pod.Activity, busy.Pod.Occupied = Unloading, true
			busy.phaseTicks = test.seconds * TicksPerSecond
			s.waiting = []waitingTrip{{request: Request{ID: 1, From: "market", To: "harbor"}}}
			fast, full := s.Clone(), s.Clone()
			station, _ := s.station(test.station)
			market, _ := s.station("market")
			if test.rounding {
				// A small reverse-sum error must not exclude a cutoff tie.
				bounds := fast.stationPickupBounds("market")
				bounds[fast.graph.nodes[station.Berths[0].Node]] = 1e-7
			}
			got := fast.waitForFinishingPod(&fast.waiting[0], &fast.vehicles[0], nil)
			want := full.waitForFinishingPodFull(&full.waiting[0], &full.vehicles[0], nil)
			if got != want || got != test.wantHold || !sameHoldState(fast, full) {
				t.Fatalf("hold=%v, full=%v, want=%v, state equality=%v", got, want, test.wantHold, sameHoldState(fast, full))
			}
			key := routeKey{from: station.Berths[0].Node, to: market.Berths[0].Node}
			_, searched := fast.routes[key]
			if searched == test.wantPruning {
				t.Fatalf("empty route searched=%v, want pruning=%v", searched, test.wantPruning)
			}
			if _, searched := full.routes[key]; !searched {
				t.Fatal("original scan did not exercise the empty route")
			}
		})
	}
}
