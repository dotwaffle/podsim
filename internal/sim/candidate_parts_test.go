package sim

import (
	"reflect"
	"slices"
	"testing"
)

// candidateRouteOriginal preserves the route assembly before the parts split.
func (s *Simulation) candidateRouteOriginal(v *vehicle, stationID string, load func(Berth) int) ([]Lane, Berth, bool) {
	if v.Pod.Activity == Idle {
		from, _ := s.station(v.Pod.StationID)
		berth, _ := from.berth(v.Pod.BerthID)
		if v.Pod.StationID == stationID {
			// The pod can board where it is. Its own berth has a load of at
			// least one because the pod holds it, so stationRouteByLoad
			// would choose a free berth and a loop around the network.
			return nil, berth, true
		}
		route, destination, err := s.stationRouteByLoad(stationRouteInput{from: berth.Node, station: stationID, load: load})
		return route, destination, err == nil
	}
	prefix, from, ok := s.divertStart(v)
	if !ok {
		return nil, Berth{}, false
	}
	suffix, berth, err := s.stationRouteByLoad(stationRouteInput{from: from, station: stationID, load: load})
	if err != nil {
		return nil, Berth{}, false
	}
	return append(slices.Clone(v.Route[:prefix]), suffix...), berth, true
}

func checkCandidateRouteOriginal(t *testing.T, s *Simulation, podID, station string, load func(Berth) int) {
	t.Helper()
	fast, original := s.Clone(), s.Clone()
	gotRoute, gotBerth, gotOK := fast.candidateRoute(fast.findVehicle(podID), station, load)
	wantRoute, wantBerth, wantOK := original.candidateRouteOriginal(original.findVehicle(podID), station, load)
	if gotOK != wantOK || gotBerth != wantBerth || !slices.Equal(gotRoute, wantRoute) {
		t.Fatalf("tick %d pod %s station %s: route/berth/ok differ", s.tick, podID, station)
	}
	if !reflect.DeepEqual(fast, original) { //nolint:govet // deepequalerrors: compare route errors by value.
		t.Fatalf("tick %d pod %s station %s: simulation or route cache differs", s.tick, podID, station)
	}
}

func TestCandidateRoutePartsMatchOriginal(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, congestion := range []bool{false, true} {
		t.Run(map[bool]string{false: "free-flow", true: "congestion"}[congestion], func(t *testing.T) {
			t.Parallel()
			idle, moving, rejected := 0, 0, 0
			holdScenario{rule: FinishingPodWaitCurrent, congestion: congestion}.run(t, func(s *Simulation) {
				if s.tick%(5*TicksPerSecond) != 0 {
					return
				}
				for i := range s.vehicles {
					v := &s.vehicles[i]
					switch v.Pod.Activity {
					case Idle:
						idle++
					default:
						if _, _, ok := s.divertStart(v); ok {
							moving++
						} else {
							rejected++
						}
					}
					for _, station := range []string{"harbor", "garden", "market", "parking", "unknown"} {
						for _, load := range []func(Berth) int{nil, noBerthLoad, func(b Berth) int {
							if b.ID == "market-1" {
								return 10
							}
							return 0
						}} {
							checkCandidateRouteOriginal(t, s, v.Pod.ID, station, load)
						}
					}
				}
			})
			if idle == 0 || moving == 0 || rejected == 0 {
				t.Fatalf("coverage idle%d moving%d rejected%d", idle, moving, rejected)
			}
		})
	}
}

func TestCandidateRoutePartsCommitmentAndOwnership(t *testing.T) {
	t.Parallel()
	for _, noReservations := range []bool{false, true} {
		t.Run(map[bool]string{false: "reserved-prefix", true: "zero-prefix"}[noReservations], func(t *testing.T) {
			t.Parallel()
			s, err := New(twoBerthMarket(), "harbor")
			if err != nil {
				t.Fatal(err)
			}
			v := &s.vehicles[0]
			destination, _ := s.station("garden")
			if err := s.startEmptyMove(v, emptyDestination{station: "garden", berth: destination.Berths[0]}); err != nil {
				t.Fatal(err)
			}
			if noReservations {
				v.reservedThrough = -1
			} else {
				for range 300 * TicksPerSecond {
					if prefix, _, ok := s.divertStart(v); ok && prefix > 0 {
						break
					}
					s.Step()
				}
			}
			prefix, _, ok := s.divertStart(v)
			if !ok || (prefix == 0) != noReservations {
				t.Fatalf("prefix %d, divert %v", prefix, ok)
			}
			checkCandidateRouteOriginal(t, s, v.Pod.ID, "market", nil)
			route, _, ok := s.candidateRoute(v, "market", nil)
			if !ok || len(route) == 0 {
				t.Fatal("no successful moving route")
			}
			beforeRoute := slices.Clone(v.Route)
			beforeCache := make(map[routeKey][]Lane, len(s.routes))
			for key, cached := range s.routes {
				beforeCache[key] = slices.Clone(cached.lanes)
			}
			route[0].ID = "modified-result"
			if !slices.Equal(v.Route, beforeRoute) {
				t.Fatal("moving result aliases vehicle route")
			}
			for key, cached := range s.routes {
				if !slices.Equal(cached.lanes, beforeCache[key]) {
					t.Fatal("moving result aliases route cache")
				}
			}
			v.follower = 1
			checkCandidateRouteOriginal(t, s, v.Pod.ID, "market", nil)
			if _, _, _, ok := s.candidateRouteParts(v, "market", nil); ok {
				t.Fatal("linked pod can divert")
			}
		})
	}
	t.Run("parking-inlet", func(t *testing.T) {
		t.Parallel()
		s, err := New(twoBerthMarket(), "harbor")
		if err != nil {
			t.Fatal(err)
		}
		v := &s.vehicles[0]
		parking, _ := s.station("parking")
		if err := s.startEmptyMove(v, emptyDestination{station: "parking", berth: parking.Berths[0]}); err != nil {
			t.Fatal(err)
		}
		for range 300 * TicksPerSecond {
			if _, _, ok := s.divertStart(v); !ok && v.Pod.Activity != Idle {
				checkCandidateRouteOriginal(t, s, v.Pod.ID, "market", nil)
				if _, _, _, ok := s.candidateRouteParts(v, "market", nil); ok {
					t.Fatal("committed parking arrival can divert")
				}
				return
			}
			s.Step()
		}
		t.Fatal("parking inlet commitment not exercised")
	})
}

func TestCandidateRoutePartsUnreachable(t *testing.T) {
	t.Parallel()
	network := Example()
	network.Lanes = slices.DeleteFunc(network.Lanes, func(lane Lane) bool {
		return lane.ID == "approach-branch"
	})
	s, err := New(network, "harbor")
	if err != nil {
		t.Fatal(err)
	}
	v := &s.vehicles[0]
	for _, activity := range []Activity{Idle, Traveling} {
		v.Pod.Activity = activity
		checkCandidateRouteOriginal(t, s, v.Pod.ID, "market", nil)
		if _, _, _, ok := s.candidateRouteParts(v, "market", nil); ok {
			t.Fatalf("activity %s accepted an unreachable station", activity)
		}
	}
}
