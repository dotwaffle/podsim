package sim

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

func TestSetPositioning(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	advance(s, 100)
	if s.positioning != PositioningOff || s.nextRedistributionTick != 0 {
		t.Fatalf("new simulation has mode %d and next check %d", s.positioning, s.nextRedistributionTick)
	}
	for _, mode := range []Positioning{PositioningOff - 1, PositioningGuarded + 1} {
		if err := s.SetPositioning(mode); err == nil || s.positioning != PositioningOff || s.nextRedistributionTick != 0 {
			t.Fatalf("SetPositioning(%d) = %v, mode %d, next check %d", mode, err, s.positioning, s.nextRedistributionTick)
		}
	}
	if err := s.SetPositioning(PositioningOff); err != nil || s.nextRedistributionTick != 0 {
		t.Fatalf("SetPositioning(off) = %v, next check %d", err, s.nextRedistributionTick)
	}
	if err := s.SetPositioning(PositioningGuarded); err != nil || s.positioning != PositioningGuarded || s.nextRedistributionTick != s.tick {
		t.Fatalf("SetPositioning(guarded) = %v, mode %d, next check %d at tick %d", err, s.positioning, s.nextRedistributionTick, s.tick)
	}
	s.nextRedistributionTick = s.tick + 50
	if err := s.SetPositioning(PositioningGuarded); err != nil || s.nextRedistributionTick != s.tick+50 {
		t.Fatalf("SetPositioning(guarded) moved a later check to %d at tick %d: %v", s.nextRedistributionTick, s.tick, err)
	}
	s.Reset()
	if s.positioning != PositioningOff || s.nextRedistributionTick != 0 {
		t.Fatalf("Reset gives mode %d and next check %d", s.positioning, s.nextRedistributionTick)
	}
}

func TestSetDemandRate(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	if s.DemandRate() != 0 || s.Positioning() != PositioningOff {
		t.Fatalf("new simulation has demand rate %d and mode %d", s.DemandRate(), s.Positioning())
	}
	if err := s.SetDemandRate(5); err != nil || s.DemandRate() != 5 {
		t.Fatalf("SetDemandRate(5) = %v, rate %d", err, s.DemandRate())
	}
	if err := s.SetDemandRate(-1); err == nil || s.DemandRate() != 5 {
		t.Fatalf("SetDemandRate(-1) = %v, rate %d", err, s.DemandRate())
	}
	if clone := s.Clone(); clone.DemandRate() != 5 {
		t.Fatalf("clone has demand rate %d", clone.DemandRate())
	}
	if err := s.SetPositioning(PositioningGuarded); err != nil || s.Positioning() != PositioningGuarded {
		t.Fatalf("SetPositioning(guarded) = %v, mode %d", err, s.Positioning())
	}
	s.Reset()
	if s.DemandRate() != 0 || s.Positioning() != PositioningOff {
		t.Fatalf("Reset gives demand rate %d and mode %d", s.DemandRate(), s.Positioning())
	}
	if err := s.SetDemandRate(0); err != nil || s.DemandRate() != 0 {
		t.Fatalf("SetDemandRate(0) = %v, rate %d", err, s.DemandRate())
	}
}

// lineStation describes a station of lineNetwork.
type lineStation struct {
	id      string
	berths  int
	parking bool
}

// lineNetwork returns a one-way loop of stations in list order. Each
// station is 300 m after the one before it, and a return road of about
// 1,000 m plus the length of the line goes from the last station back to
// the first. A pod goes from a berth to a berth of the next station in
// about 24 s, so a guarded move reaches eight stations ahead, but not a
// station behind. The berths of a station are in pairs on the two sides of
// the through lane, so the two berths of a pair have the same route costs.
func lineNetwork(stations []lineStation) Network {
	var network Network
	node := func(id string, x, y float64) {
		network.Nodes = append(network.Nodes, Node{ID: id, Position: Point{X: x, Y: y}})
	}
	lane := func(id, from, to string) {
		network.Lanes = append(network.Lanes, Lane{ID: id, From: from, To: to, SpeedLimit: 14})
	}
	for index, spec := range stations {
		x := float64(index) * 300
		station := Station{ID: spec.id, Name: spec.id, Entry: spec.id + "-entry", Exit: spec.id + "-exit", ParkingOnly: spec.parking}
		node(station.Entry, x, 0)
		node(station.Exit, x+150, 0)
		lane(spec.id+"-through", station.Entry, station.Exit)
		for berth := range spec.berths {
			id := fmt.Sprintf("%s-%d", spec.id, berth+1)
			y := 60 + 40*float64(berth/2)
			if berth%2 == 1 {
				y = -y
			}
			node(id, x+75, y)
			lane(id+"-in", station.Entry, id)
			lane(id+"-out", id, station.Exit)
			station.Berths = append(station.Berths, Berth{ID: id, Node: id})
		}
		network.Stations = append(network.Stations, station)
		if index > 0 {
			lane(stations[index-1].id+"-link", stations[index-1].id+"-exit", station.Entry)
		}
	}
	end := float64(len(stations)) * 300
	node("return-east", end, 400)
	node("return-west", -150, 400)
	lane("return-down", stations[len(stations)-1].id+"-exit", "return-east")
	lane("return", "return-east", "return-west")
	lane("return-up", "return-west", stations[0].id+"-entry")
	return network
}

// lineStations returns a parking station p with the given berths, when it
// has berths, and then the passenger stations s0, s1, and so on, with the
// given berths.
func lineStations(parking int, berths ...int) []lineStation {
	var stations []lineStation
	if parking > 0 {
		stations = append(stations, lineStation{id: "p", berths: parking, parking: true})
	}
	for index, count := range berths {
		stations = append(stations, lineStation{id: fmt.Sprintf("s%d", index), berths: count})
	}
	return stations
}

// place returns placements with the IDs 01, 02, and so on, at the berths.
func place(berths ...string) []Placement {
	fleet := make([]Placement, len(berths))
	for index, berth := range berths {
		station := berth[:len(berth)-2]
		fleet[index] = Placement{ID: fmt.Sprintf("%02d", index+1), StationID: station, BerthID: berth}
	}
	return fleet
}

// newLineSimulation returns a simulation on lineNetwork in guarded mode.
func newLineSimulation(t *testing.T, stations []lineStation, fleet []Placement) *Simulation {
	t.Helper()
	s, err := NewFleet(lineNetwork(stations), fleet)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPositioning(PositioningGuarded); err != nil {
		t.Fatal(err)
	}
	return s
}

// openGate gives pod 01 the newest request, which is complete. The request
// has ID 10 and the lowest request tick that makes the gate active. It sets
// the tick 1 s later and makes a guarded check due. Each request has
// boarded, so the budget is open.
func openGate(s *Simulation) {
	const id = 10
	requested := int64(id)*ticksPerMinute*guardedFleetRateShare/int64(len(s.vehicles)) + 1
	v := &s.vehicles[0]
	v.Request = &Request{ID: id, From: "s0", To: "s1", PartySize: 1, PodID: v.Pod.ID, Completed: true, RequestedTick: requested}
	s.requestID, s.boarded = id, id
	s.tick = requested + TicksPerSecond
	s.nextRedistributionTick = s.tick
}

func TestGuardedGate(t *testing.T) {
	t.Parallel()
	// The gate of a fleet of 114 pods, as in London, is active below 5.7
	// requests per minute. The open case has request 100 at 5 per minute.
	const (
		id           = 100
		fivePerMin   = 12 * TicksPerSecond
		requested    = id * fivePerMin
		liveness     = requested + guardedLivenessTicks
		fleetSize    = 114
		workingLimit = fleetSize * guardedWorkingNumerator / guardedWorkingDenominator
	)
	berths := make([]int, 30)
	for index := range berths {
		berths[index] = 4
	}
	stations := lineStations(0, berths...)
	var fleet []Placement
	for _, station := range stations {
		for berth := range station.berths {
			if len(fleet) < fleetSize {
				fleet = append(fleet, Placement{ID: fmt.Sprintf("%03d", len(fleet)), StationID: station.id, BerthID: fmt.Sprintf("%s-%d", station.id, berth+1)})
			}
		}
	}
	newest := func(s *Simulation, tick int64) {
		s.vehicles[0].Request = &Request{ID: id, PartySize: 1, Completed: true, RequestedTick: tick}
	}
	base := func(s *Simulation) {
		newest(s, requested)
		s.tick, s.requestID, s.boarded = requested+TicksPerSecond, id, id
	}
	working := func(s *Simulation, count int) {
		for index := 1; index <= count; index++ {
			s.vehicles[index].Request = &Request{ID: index, PartySize: 1, RequestedTick: 1}
		}
	}
	// lateStart is request 100 at 20 per minute after 2 h with no request.
	// The mean rate is then 0.8 per minute.
	const lateStart = 2*60*ticksPerMinute + id*3*TicksPerSecond
	late := func(s *Simulation) {
		base(s)
		newest(s, lateStart)
		s.tick = lateStart + TicksPerSecond
	}
	tests := []struct {
		name         string
		setup        func(s *Simulation)
		active, open bool
		// requested is the request tick that an active gate reads. 0 selects
		// the tick of the 5 per minute case.
		requested int64
		// rate is the demand rate. A gate with a rate reads the rate and
		// ticksPerMinute.
		rate int
	}{
		{name: "no request", setup: func(*Simulation) {}},
		{name: "demand rate with no request", setup: func(*Simulation) {}, rate: 5},
		{name: "request at tick 0", setup: func(s *Simulation) { base(s); newest(s, 0) }},
		{name: "6 per minute", setup: func(s *Simulation) { base(s); newest(s, id*10*TicksPerSecond) }},
		// These ticks give 5.8 and 5.6 requests per minute. A rate share of
		// 19 makes the first active, and a share of 21 makes the second
		// inert.
		{name: "5.8 per minute", setup: func(s *Simulation) { base(s); newest(s, 62069) }},
		{name: "5.6 per minute", setup: func(s *Simulation) { base(s); newest(s, 64286) }, active: true, open: true, requested: 64286},
		{name: "5 per minute", setup: base, active: true, open: true},
		{name: "newest in a waiting trip", active: true, open: true, setup: func(s *Simulation) {
			base(s)
			s.vehicles[0].Request = nil
			s.waiting = []waitingTrip{{request: Request{ID: id, From: "s1", To: "s2", PartySize: 1, PodID: "005", RequestedTick: requested}}}
		}},
		{name: "liveness edge", setup: func(s *Simulation) { base(s); s.tick = liveness }, active: true, open: true},
		{name: "liveness past", setup: func(s *Simulation) { base(s); s.tick = liveness + 1 }, active: true},
		{name: "budget edge", setup: func(s *Simulation) { base(s); s.rebalanceMoves = id - 1 }, active: true, open: true},
		{name: "budget spent", setup: func(s *Simulation) { base(s); s.rebalanceMoves = id }, active: true},
		{name: "trip with a pod", active: true, open: true, setup: func(s *Simulation) {
			base(s)
			s.waiting = []waitingTrip{{request: Request{ID: id - 1, From: "s1", To: "s2", PartySize: 1, PodID: "005", RequestedTick: 1}}}
		}},
		{name: "trip without a pod", active: true, setup: func(s *Simulation) {
			base(s)
			s.waiting = []waitingTrip{{request: Request{ID: id - 1, From: "s1", To: "s2", PartySize: 1, RequestedTick: 1}}}
		}},
		{name: "load edge", setup: func(s *Simulation) { base(s); working(s, workingLimit) }, active: true, open: true},
		{name: "load over", setup: func(s *Simulation) { base(s); working(s, workingLimit+1) }, active: true},
		{name: "load over with a pickup pod", active: true, setup: func(s *Simulation) {
			base(s)
			working(s, workingLimit)
			s.waiting = []waitingTrip{{request: Request{ID: id - 1, From: "s1", To: "s2", PartySize: 1, PodID: "100", RequestedTick: 1}}}
		}},
		// With a demand rate, the gate reads the rate and not the rate at
		// the newest request. 5*20 is less than 114, and 6*20 is not.
		{name: "demand rate 5 at a mean of 6", rate: 5, active: true, open: true, setup: func(s *Simulation) {
			base(s)
			newest(s, id*10*TicksPerSecond)
			s.tick = id*10*TicksPerSecond + TicksPerSecond
		}},
		{name: "demand rate 6 at a mean of 5", setup: base, rate: 6},
		{name: "demand rate liveness past", setup: func(s *Simulation) { base(s); s.tick = liveness + 1 }, rate: 5, active: true},
		{name: "late start with the mean", setup: late, active: true, open: true, requested: lateStart},
		{name: "late start with a demand rate", setup: late, rate: 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newLineSimulation(t, stations, fleet)
			if err := s.SetDemandRate(tc.rate); err != nil {
				t.Fatal(err)
			}
			tc.setup(s)
			gate := s.guardedGate()
			if gate.active != tc.active || gate.open != tc.open {
				t.Fatalf("gate %+v, want active %v, open %v", gate, tc.active, tc.open)
			}
			count, ticks := id, cmp.Or(tc.requested, requested)
			if tc.rate > 0 {
				count, ticks = tc.rate, ticksPerMinute
			}
			if tc.active && (gate.count != count || gate.ticks != ticks) {
				t.Fatalf("gate %+v, want %d requests in %d ticks", gate, count, ticks)
			}
		})
	}
}

// TestGuardedGateLoadEdge checks the load edge of a fleet of 20 pods. Two
// fifths of this fleet is 8 pods, so the gate stays open when 8 pods work
// and closes when 9 pods work.
func TestGuardedRateLow(t *testing.T) {
	t.Parallel()
	// A fleet of 114 pods has a limit of 5.7 requests per minute. Request 100
	// at tick 62069 is 5.8 per minute, and at tick 64286 it is 5.6 per
	// minute.
	tests := []struct {
		name      string
		id        int
		requested int64
		fleet     int
		want      bool
	}{
		{name: "above the limit", id: 100, requested: 62069, fleet: 114},
		{name: "below the limit", id: 100, requested: 64286, fleet: 114, want: true},
		{name: "tick 0", id: 100, requested: 0, fleet: 114},
		{name: "negative tick", id: 100, requested: -1, fleet: 114},
		{name: "no fleet", id: 100, requested: 64286},
		// 128102389400761*72000 overflows int64 to a negative number, and
		// 114*MaxInt64 does too.
		{name: "large ID at tick 0", id: 128102389400761, requested: 0, fleet: 114},
		{name: "large ID", id: 128102389400761, requested: 1, fleet: 114},
		{name: "large tick", id: 1, requested: math.MaxInt64, fleet: 114, want: true},
		{name: "large ID and tick", id: math.MaxInt, requested: math.MaxInt64, fleet: 114},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := guardedRateLow(tc.id, tc.requested, tc.fleet); got != tc.want {
				t.Fatalf("guardedRateLow(%d, %d, %d) = %t, want %t", tc.id, tc.requested, tc.fleet, got, tc.want)
			}
		})
	}
}

func TestGuardedGateLoadEdge(t *testing.T) {
	t.Parallel()
	berths := make([]string, 0, 20)
	for station := range 10 {
		berths = append(berths, fmt.Sprintf("s%d-1", station), fmt.Sprintf("s%d-2", station))
	}
	for _, tc := range []struct {
		working int
		open    bool
	}{{8, true}, {9, false}} {
		t.Run(fmt.Sprintf("%d working", tc.working), func(t *testing.T) {
			t.Parallel()
			s := newLineSimulation(t, lineStations(0, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2), place(berths...))
			openGate(s)
			for index := 1; index <= tc.working; index++ {
				s.vehicles[index].Request = &Request{ID: index, PartySize: 1, RequestedTick: 1}
			}
			if gate := s.guardedGate(); !gate.active || gate.open != tc.open {
				t.Fatalf("gate %+v, want active and open %v", gate, tc.open)
			}
		})
	}
}

func TestGuardedNewestRequest(t *testing.T) {
	t.Parallel()
	stations := lineStations(0, 2, 2, 2)
	t.Run("highest ID", func(t *testing.T) {
		t.Parallel()
		s := newLineSimulation(t, stations, place("s0-1", "s1-1"))
		s.waiting = []waitingTrip{{request: Request{ID: 7, From: "s1", To: "s2", PartySize: 1, RequestedTick: 700}}}
		s.vehicles[0].Request = &Request{ID: 9, PartySize: 1, Completed: true, RequestedTick: 900}
		s.vehicles[1].Request = &Request{ID: 3, PartySize: 1, RequestedTick: 300}
		for _, want := range []struct {
			id        int
			requested int64
		}{{9, 900}, {7, 700}, {3, 300}, {0, 0}} {
			id, requested, ok := s.newestRequest()
			if id != want.id || requested != want.requested || ok != (want.id > 0) {
				t.Fatalf("newest request %d at %d, %v, want %d at %d", id, requested, ok, want.id, want.requested)
			}
			switch {
			case s.vehicles[0].Request != nil:
				s.vehicles[0].Request = nil
			case len(s.waiting) > 0:
				s.waiting = nil
			default:
				s.vehicles[1].Request = nil
			}
		}
	})
	t.Run("pod boards an older trip", func(t *testing.T) {
		t.Parallel()
		s := newLineSimulation(t, stations, place("s0-1", "s1-1"))
		s.tick, s.requestID = 2000, 2
		s.vehicles[0].Request = &Request{ID: 2, From: "s1", To: "s0", PartySize: 1, PodID: "01", Completed: true, RequestedTick: 1500}
		s.waiting = []waitingTrip{{request: Request{ID: 1, From: "s0", To: "s1", PartySize: 1, RequestedTick: 1000}}}
		s.dispatch()
		if v := s.findVehicle("01"); v.Pod.Activity != Boarding || v.Request.ID != 1 {
			t.Fatalf("pod 01 does not board request 1: %+v", v.Vehicle)
		}
		if id, requested, ok := s.newestRequest(); id != 1 || requested != 1000 || !ok {
			t.Fatalf("newest request %d at %d, %v, want 1 at 1000", id, requested, ok)
		}
	})
	t.Run("shared ride keeps the liveness", func(t *testing.T) {
		t.Parallel()
		s := newLineSimulation(t, stations, place("s0-1", "s1-1", "s2-1"))
		if err := s.SetSharedRidePartyLimit(2); err != nil {
			t.Fatal(err)
		}
		const first = 50000
		s.tick = first
		for range 2 {
			if err := s.RequestTrip("s0", "s1"); err != nil {
				t.Fatal(err)
			}
			s.tick += TicksPerSecond
		}
		if s.requestID != 2 || s.sharedParties != 1 {
			t.Fatalf("request 2 did not join request 1: requests %d, shared %d", s.requestID, s.sharedParties)
		}
		s.tick = first + guardedLivenessTicks
		if gate := s.guardedGate(); !gate.open || gate.count != 1 || gate.ticks != first {
			t.Fatalf("gate at the end of the liveness = %+v, want open at request 1", gate)
		}
		s.tick++
		if gate := s.guardedGate(); gate.open || !gate.active {
			t.Fatalf("gate after the liveness of request 1 = %+v, want active and not open", gate)
		}
	})
}

// deficitIDs returns the station IDs of the deficit stations in order.
func deficitIDs(view guardedView) []string {
	ids := make([]string, len(view.deficits))
	for index, deficit := range view.deficits {
		ids[index] = deficit.station.ID
	}
	return ids
}

func TestGuardedDemandSet(t *testing.T) {
	t.Parallel()
	// Station s2 has one berth. Each pod is in parking, so each demand
	// station is a deficit station.
	stations := lineStations(6, 2, 2, 1, 3, 2)
	fleet := place("p-1", "p-2", "p-3", "p-4", "p-5", "p-6")
	busy := guardedGate{count: 100, ticks: 100 * 10 * TicksPerSecond}
	// With these weights, W is 14. Four stations have a weight, so the mean
	// weight is 3.5. Station s0 expects half a request in 15 minutes from
	// request 1 at tick 38571 or before.
	profile := map[string]float64{"s0": 5, "s1": 1, "s2": 5, "s3": 3}
	tests := []struct {
		name    string
		weights map[string]float64
		gate    guardedGate
		want    []string
	}{
		{name: "equal weights", gate: busy, want: []string{"s0", "s1", "s3", "s4"}},
		{name: "profile weights", weights: profile, gate: busy, want: []string{"s0"}},
		// W is 9 and three stations have a weight, so the mean weight is 3.
		// The two stations with a weight of 0 do not lower the mean to 1.8.
		{name: "weight 0 keeps the mean", weights: map[string]float64{"s0": 4, "s1": 2, "s3": 3, "s4": 0}, gate: busy, want: []string{"s0", "s3"}},
		{name: "weight order", weights: map[string]float64{"s0": 2, "s1": 3, "s2": 1, "s3": 3, "s4": 1}, gate: busy, want: []string{"s1", "s3", "s0"}},
		{name: "one weighted station", weights: map[string]float64{"s4": 1}, gate: busy, want: []string{"s4"}},
		{name: "horizon", weights: profile, gate: guardedGate{count: 1, ticks: 30000}, want: []string{"s0"}},
		{name: "horizon edge", weights: profile, gate: guardedGate{count: 1, ticks: 38571}, want: []string{"s0"}},
		{name: "horizon past", weights: profile, gate: guardedGate{count: 1, ticks: 38572}, want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newLineSimulation(t, stations, fleet)
			if err := s.SetDemandWeights(tc.weights); err != nil {
				t.Fatal(err)
			}
			view := s.guardedView(tc.gate)
			if got := deficitIDs(view); !slices.Equal(got, tc.want) {
				t.Fatalf("deficit stations %v, want %v", got, tc.want)
			}
			for _, deficit := range view.deficits {
				if !slices.Equal(deficit.berths, deficit.station.Berths) {
					t.Fatalf("station %s has available berths %v, want %v", deficit.station.ID, deficit.berths, deficit.station.Berths)
				}
			}
		})
	}
}

func TestGuardedSupply(t *testing.T) {
	t.Parallel()
	// Pod 01 unloads at s0. Pod 02 goes from parking to s1. Pod 03 carries
	// a passenger from s2 to s3, the next station. Pod 04 carries a
	// passenger from s4 to s2, which is on the other side of the loop.
	s := newLineSimulation(t, lineStations(4, 3, 3, 3, 3, 3, 3), place("s0-1", "p-1", "s2-1", "s4-1", "p-2", "p-3"))
	for _, trip := range [][2]string{{"s2", "s3"}, {"s4", "s2"}} {
		if err := s.RequestTrip(trip[0], trip[1]); err != nil {
			t.Fatal(err)
		}
	}
	inbound := []*vehicle{s.findVehicle("03"), s.findVehicle("04")}
	stepUntil(t, s, "pods 03 and 04 travel", func() bool {
		return inbound[0].Pod.Activity == Traveling && inbound[1].Pod.Activity == Traveling
	})
	for index, want := range []bool{true, false} {
		if _, seconds, ok := s.finishEstimate(inbound[index]); !ok || (seconds <= guardedReachSeconds) != want {
			t.Fatalf("pod %s becomes available in %.0f s, %v", inbound[index].Pod.ID, seconds, ok)
		}
	}
	unloading := s.findVehicle("01")
	unloading.Pod.Activity, unloading.Pod.Occupied, unloading.phaseTicks = Unloading, true, unloadingTicks
	unloading.Request = &Request{ID: 1, From: "s5", To: "s0", PartySize: 1, PodID: "01", RequestedTick: 1}
	s1, _ := s.station("s1")
	if err := s.startEmptyMove(s.findVehicle("02"), emptyDestination{station: "s1", berth: s1.Berths[0], reserveBerth: true}); err != nil {
		t.Fatal(err)
	}
	view := s.guardedView(guardedGate{count: 100, ticks: 100 * 10 * TicksPerSecond})
	if got, want := deficitIDs(view), []string{"s2", "s4", "s5"}; !slices.Equal(got, want) {
		t.Fatalf("deficit stations %v, want %v", got, want)
	}
}

// moved returns the pods that are not idle, by pod ID.
func moved(s *Simulation) []string {
	var ids []string
	for index := range s.vehicles {
		if s.vehicles[index].Pod.Activity != Idle {
			ids = append(ids, s.vehicles[index].Pod.ID)
		}
	}
	return ids
}

// checkGuardedMove checks that a pod makes a guarded move to a berth and
// holds the berth.
func checkGuardedMove(t *testing.T, s *Simulation, podID, berthID string) {
	t.Helper()
	v := s.findVehicle(podID)
	if !v.Rebalancing || v.released || v.destination.ID != berthID || v.RelocatingTo != v.destinationStation {
		t.Fatalf("pod %s is not on a guarded move to %s: %+v, destination %s", podID, berthID, v.Vehicle, v.destination.ID)
	}
	for _, r := range berthResources(v.destination) {
		if s.owners[r] != podID {
			t.Fatalf("pod %s does not hold %v", podID, r)
		}
	}
}

func TestGuardedRefill(t *testing.T) {
	t.Parallel()
	// Eight passenger stations with two berths each. From s6, a guarded
	// move reaches s7, but not s0 to s5.
	stations := lineStations(6, 2, 2, 2, 2, 2, 2, 2, 2)
	lone := []string{"s1-1", "s2-1", "s3-1", "s4-1", "s5-1"}
	tests := []struct {
		name    string
		fleet   []Placement
		weights map[string]float64
		setup   func(s *Simulation)
		// pod and berth give the move. An empty pod expects no move.
		pod, berth string
		// closed is true when setup closes the gate, but the gate stays
		// active.
		closed bool
	}{
		// Pods 01 and 02 have the same route cost, and 01 is first in the
		// fleet.
		{name: "fleet order", fleet: place("p-2", "p-1", "p-3", "p-4", "p-5", "p-6"), pod: "01", berth: "s0-1"},
		{
			name: "liveness past", fleet: place("p-2", "p-1", "p-3", "p-4", "p-5", "p-6"), closed: true,
			setup: func(s *Simulation) { s.tick += guardedLivenessTicks; s.nextRedistributionTick = s.tick },
		},
		{
			name: "budget spent", fleet: place("p-2", "p-1", "p-3", "p-4", "p-5", "p-6"), closed: true,
			setup: func(s *Simulation) { s.rebalanceMoves = s.boarded },
		},
		{name: "lone pods stay", fleet: place(append(lone, "s6-1")...)},
		{name: "second idle pod", fleet: place(append(lone, "s6-2", "s6-1")...), pod: "06", berth: "s7-1"},
		{
			name: "weight 0 station", fleet: place(append(lone, "s6-1")...), pod: "03", berth: "s7-1",
			weights: map[string]float64{"s0": 1, "s1": 1, "s2": 1, "s4": 1, "s5": 1, "s6": 1, "s7": 1},
		},
		{
			name: "cooldown", fleet: place(append(lone, "s6-2", "s6-1")...),
			setup: func(s *Simulation) {
				s.findVehicle("06").rebalanceAfter = s.tick + 1
				s.findVehicle("07").rebalanceAfter = s.tick + 1
			},
		},
		{
			name: "cooldown ends", fleet: place(append(lone, "s6-2", "s6-1")...), pod: "07", berth: "s7-1",
			setup: func(s *Simulation) {
				s.findVehicle("06").rebalanceAfter = s.tick + 1
				s.findVehicle("07").rebalanceAfter = s.tick
			},
		},
		{
			name: "assigned pod", fleet: place(append(lone, "s6-2", "s6-1")...),
			setup: func(s *Simulation) {
				s.waiting = []waitingTrip{{request: Request{ID: 5, From: "s6", To: "s7", PartySize: 1, PodID: "06", RequestedTick: 1}}}
			},
		},
		{
			name: "assigned pod in parking", fleet: place(append(lone, "s6-1", "p-1")...),
			setup: func(s *Simulation) {
				s.waiting = []waitingTrip{{request: Request{ID: 5, From: "s1", To: "s2", PartySize: 1, PodID: "07", RequestedTick: 1}}}
			},
		},
		{name: "pod in parking", fleet: place(append(lone, "s6-1", "p-1")...), pod: "07", berth: "s0-1"},
		{
			name: "waiting trip at the deficit", fleet: place(append(lone, "s6-2", "s6-1")...),
			setup: func(s *Simulation) {
				s.waiting = []waitingTrip{{request: Request{ID: 5, From: "s7", To: "s1", PartySize: 1, PodID: "01", RequestedTick: 1}}}
			},
		},
		{
			name: "keeps a free berth", fleet: place(append(lone, "s6-2", "s6-1")...),
			setup: func(s *Simulation) {
				// Pod 01 boards at s1 for s7 and has chosen berth s7-1.
				v := s.findVehicle("01")
				v.Pod.Activity, v.destination, v.destinationStation = Boarding, s.network.Stations[8].Berths[0], "s7"
			},
		},
		{
			// Pod 01 travels, and its route crosses berth s7-1 after its
			// claims, as after a diversion inside the berth access of s7.
			// A move to s7-1 would claim the berth ahead of pod 01.
			name: "berth on a route", fleet: place(append(lone, "s6-2", "s6-1")...),
			setup: func(s *Simulation) {
				v := s.findVehicle("01")
				v.Pod.Activity = Traveling
				v.blocks = append(v.blocks[:v.reservedThrough+1], block{resources: []resource{{kind: berthResource, id: "s7-1"}}})
			},
		},
		{
			// The first three deficit stations are behind s6, so the check
			// does not try s7.
			name: "three tries", fleet: place("s3-1", "s4-1", "s5-1", "s6-2", "s6-1"),
			weights: map[string]float64{"s0": 3, "s1": 3, "s2": 3, "s3": 0.5, "s4": 0.5, "s5": 0.5, "s6": 0.5, "s7": 2},
		},
		{
			name: "fourth station first", fleet: place("s3-1", "s4-1", "s5-1", "s6-2", "s6-1"), pod: "04", berth: "s7-1",
			weights: map[string]float64{"s0": 3, "s1": 3, "s2": 3, "s3": 0.5, "s4": 0.5, "s5": 0.5, "s6": 0.5, "s7": 3.5},
		},
		{name: "no candidate in reach", fleet: place("s1-1", "s2-1", "s3-1", "s7-1", "s6-2", "s6-1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newLineSimulation(t, stations, tc.fleet)
			if err := s.SetDemandWeights(tc.weights); err != nil {
				t.Fatal(err)
			}
			openGate(s)
			if tc.setup != nil {
				tc.setup(s)
			}
			if gate := s.guardedGate(); !gate.active || gate.open == tc.closed {
				t.Fatalf("gate %+v, want open %v", gate, !tc.closed)
			}
			before := slices.Clone(moved(s))
			moves := s.rebalanceMoves
			s.positionGuarded()
			if s.nextRedistributionTick != s.tick+guardedCheckTicks {
				t.Fatalf("next check %d at tick %d", s.nextRedistributionTick, s.tick)
			}
			got := slices.DeleteFunc(moved(s), func(id string) bool { return slices.Contains(before, id) })
			if tc.pod == "" {
				if len(got) != 0 || s.rebalanceMoves != moves {
					t.Fatalf("pods %v moved, %d moves, want none", got, s.rebalanceMoves-moves)
				}
				return
			}
			if !slices.Equal(got, []string{tc.pod}) || s.rebalanceMoves != moves+1 {
				t.Fatalf("pods %v moved, %d moves, want pod %s", got, s.rebalanceMoves-moves, tc.pod)
			}
			checkGuardedMove(t, s, tc.pod, tc.berth)
			// The next check comes after guardedCheckTicks.
			s.positionGuarded()
			if s.rebalanceMoves != moves+1 {
				t.Fatal("a second move came before the next check")
			}
		})
	}
}

// bumpStations has parking pa before s0, and parking pb after s1. Station
// s1 has one berth. Parking pb is nearer to s1 than pa, but pa is first in
// network order.
func bumpStations() []lineStation {
	return []lineStation{
		{id: "pa", berths: 2, parking: true}, {id: "s0", berths: 2}, {id: "s1", berths: 1},
		{id: "pb", berths: 2, parking: true}, {id: "s2", berths: 2}, {id: "s3", berths: 2}, {id: "s4", berths: 2}, {id: "s5", berths: 2},
	}
}

func TestGuardedClear(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		fleet []Placement
		// gate sets the gate. It is nil for a closed gate.
		gate func(s *Simulation)
		// berth is the new destination of pod 01. It is empty when
		// guardedClear must fail.
		berth     string
		rebalance bool
	}{
		{
			name: "open gate goes to a deficit", fleet: place("s1-1", "s0-1", "pa-1", "pa-2", "pb-1", "pb-2"),
			gate: func(s *Simulation) { openGate(s) }, berth: "s2-1", rebalance: true,
		},
		{
			name: "active gate goes to the nearest parking", fleet: place("s1-1", "s0-1", "s2-1", "s3-1", "s4-1", "s5-1"),
			gate: func(s *Simulation) { openGate(s); s.tick += guardedLivenessTicks }, berth: "pb-1",
		},
		{
			name: "open gate with no deficit goes to parking", fleet: place("s1-1", "s0-1", "s2-1", "s3-1", "s4-1", "s5-1"),
			gate: func(s *Simulation) { openGate(s) }, berth: "pb-1",
		},
		{name: "closed gate", fleet: place("s1-1", "s0-1", "s2-1", "s3-1", "s4-1", "s5-1")},
		{
			name: "inert gate", fleet: place("s1-1", "s0-1", "s2-1", "s3-1", "s4-1", "s5-1"),
			gate: func(s *Simulation) { openGate(s); s.vehicles[0].Request.RequestedTick-- },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newLineSimulation(t, bumpStations(), tc.fleet)
			if tc.gate != nil {
				tc.gate(s)
			}
			blocker := s.findVehicle("01")
			owners, moves := maps.Clone(s.owners), s.rebalanceMoves
			ok := s.guardedClear(blocker)
			if s.rebalanceMoves != moves {
				t.Fatal("a bump counted as a positioning move")
			}
			if tc.berth == "" {
				if ok || blocker.Pod.Activity != Idle || !maps.Equal(owners, s.owners) {
					t.Fatalf("guardedClear = %v and changed the simulation: %+v", ok, blocker.Vehicle)
				}
				return
			}
			if !ok || blocker.destination.ID != tc.berth || blocker.Rebalancing != tc.rebalance || blocker.Pod.Activity != DepartingEmpty {
				t.Fatalf("guardedClear = %v, pod 01 %+v goes to %s, want %s", ok, blocker.Vehicle, blocker.destination.ID, tc.berth)
			}
			for _, r := range berthResources(blocker.destination) {
				if s.owners[r] != "01" {
					t.Fatalf("pod 01 does not hold %v", r)
				}
			}
		})
	}
}

// TestGuardedBumpInTraffic checks the bump rules in a run. Pod 02 carries a
// passenger from s0 to s1, where the idle pod 01 holds the one berth.
func TestGuardedBumpInTraffic(t *testing.T) {
	t.Parallel()
	// The request at 250 s is active for a fleet of 6. At tick 0 it gives
	// no rate.
	tests := []struct {
		name    string
		mode    Positioning
		request int64
		want    string
	}{
		{name: "off", mode: PositioningOff, request: 250 * TicksPerSecond, want: "pa-1"},
		{name: "guarded", mode: PositioningGuarded, request: 250 * TicksPerSecond, want: "pb-1"},
		{name: "guarded at an inert rate", mode: PositioningGuarded, want: "pa-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newLineSimulation(t, bumpStations(), place("s1-1", "s0-1", "s2-1", "s3-1", "s4-1", "s5-1"))
			if err := s.SetPositioning(tc.mode); err != nil {
				t.Fatal(err)
			}
			advance(s, int(tc.request))
			if err := s.RequestTrip("s0", "s1"); err != nil {
				t.Fatal(err)
			}
			blocker := s.findVehicle("01")
			stepUntil(t, s, "pod 01 leaves s1", func() bool { return blocker.Pod.Activity != Idle })
			if blocker.destination.ID != tc.want || blocker.Rebalancing || s.rebalanceMoves != 0 {
				t.Fatalf("pod 01 goes to %s, want %s: %+v", blocker.destination.ID, tc.want, blocker.Vehicle)
			}
			stepUntil(t, s, "the passenger arrives", func() bool { return s.completed == 1 })
		})
	}
}

// newYieldSimulation returns the example network in guarded mode. Pod 01
// makes a rebalancing move from parking to Market, as a guarded refill
// does, and pod 02 is idle at Garden.
func newYieldSimulation(t *testing.T) (*Simulation, *vehicle) {
	t.Helper()
	s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "parking", BerthID: "parking-1"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPositioning(PositioningGuarded); err != nil {
		t.Fatal(err)
	}
	v := s.findVehicle("01")
	market, _ := s.station("market")
	if err := s.startEmptyMove(v, emptyDestination{station: "market", berth: market.Berths[0], reserveBerth: true, rebalance: true}); err != nil {
		t.Fatal(err)
	}
	return s, v
}

// idleSet returns the pods that are idle and that no waiting trip names.
func idleSet(s *Simulation) map[string]bool {
	idle := make(map[string]bool)
	for index := range s.vehicles {
		if v := &s.vehicles[index]; v.Pod.Activity == Idle && !s.assigned(v.Pod.ID) {
			idle[v.Pod.ID] = true
		}
	}
	return idle
}

// stepWithoutBumps steps s until done reports true. It fails when an idle
// pod that no waiting trip names starts to move.
func stepWithoutBumps(t *testing.T, s *Simulation, done func() bool) {
	t.Helper()
	for range 300 * TicksPerSecond {
		if done() {
			return
		}
		idle := idleSet(s)
		s.Step()
		checkTraffic(t, s.Snapshot())
		for id := range idle {
			if v := s.findVehicle(id); v.Pod.Activity != Idle && !s.assigned(id) {
				t.Fatalf("tick %d: idle pod %s was bumped: %+v", s.tick, id, v.Vehicle)
			}
		}
	}
	t.Fatalf("the run did not finish: %+v", s.Snapshot())
}

// TestGuardedYieldParksAtOnce checks that a rebalancing pod that yields its
// claim becomes a released pod and goes to the nearest free berth. It does
// not go on to its old berth, so it bumps no pod there.
func TestGuardedYieldParksAtOnce(t *testing.T) {
	t.Parallel()
	s, v := newYieldSimulation(t)
	stepUntil(t, s, "pod 01 on the return lane", func() bool { return v.Pod.LaneID == "return" && v.Pod.LaneDistance > 20 })
	// Pod 02 goes from Garden to the pickup at Market-1, the berth that
	// pod 01 holds.
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if pickup := s.findVehicle("02"); pickup.destination.ID != "market-1" || !s.assigned("02") {
		t.Fatalf("pod 02 does not go to the pickup at Market-1: %+v", pickup.Vehicle)
	}
	s.Step()
	checkReleasedTo(t, s, v, "harbor-1")
	if v.Rebalancing {
		t.Fatal("the released pod is still rebalancing")
	}
	stepWithoutBumps(t, s, func() bool { return s.completed == 1 && v.Pod.Activity == Idle })
	if v.Pod.BerthID != "harbor-1" || v.released || v.rebalanceAfter != 0 {
		t.Fatalf("pod 01 stopped at %s, released %v, cooldown until %d", v.Pod.BerthID, v.released, v.rebalanceAfter)
	}
}

// TestGuardedYieldInCommittedInlet checks a rebalancing pod that yields its
// claim after it enters the inlet of its berth. The pod becomes a released
// pod, but it cannot divert, so it keeps its route with no claim. It then
// goes on to the berth, as in the other modes.
func TestGuardedYieldInCommittedInlet(t *testing.T) {
	t.Parallel()
	s, v := newYieldSimulation(t)
	// Without this rule, dispatch holds the pickup for pod 01.
	if err := s.SetFinishingPodWait(FinishingPodWaitNone); err != nil {
		t.Fatal(err)
	}
	stepUntil(t, s, "pod 01 commits to the Market inlet", func() bool {
		_, _, divert := s.divertStart(v)
		return v.Pod.Activity == Traveling && !divert
	})
	if s.relocationDestinationAdmitted(v) {
		t.Fatal("pod 01 already holds the track to its berth")
	}
	route := slices.Clone(v.Route)
	if err := s.RequestTrip("market", "garden"); err != nil {
		t.Fatal(err)
	}
	if pickup := s.findVehicle("02"); pickup.destination.ID != "market-1" || !s.assigned("02") {
		t.Fatalf("pod 02 does not go to the pickup at Market-1: %+v", pickup.Vehicle)
	}
	s.Step()
	if !v.released || v.Rebalancing || v.destination.ID != "market-1" || !slices.EqualFunc(route, v.Route, func(a, b Lane) bool { return a.ID == b.ID }) {
		t.Fatalf("pod 01 is not a released pod on its route to Market-1: %+v, released %v", v.Vehicle, v.released)
	}
	for _, r := range berthResources(v.destination) {
		if s.owners[r] == "01" {
			t.Fatalf("pod 01 still holds %v", r)
		}
	}
	stepUntil(t, s, "the passenger arrives at Garden", func() bool { return s.completed == 1 })
	if !maps.Equal(s.owners, s.retainedOwners()) {
		t.Fatal("the owners differ from the retention rules")
	}
}

// guardedClaims returns the pods that hold a guarded claim, with the
// destination berth of each.
func guardedClaims(s *Simulation) map[string]string {
	claims := make(map[string]string)
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.Rebalancing && s.owners[resource{kind: berthResource, id: v.destination.ID}] == v.Pod.ID {
			claims[v.Pod.ID] = v.destination.ID
		}
	}
	return claims
}

// claimState is the part of a simulation before a step that
// checkNewClaimsLeaveABerth reads.
type claimState struct {
	claims       map[string]string
	destinations map[string]string
	moves        int
}

func newClaimState(s *Simulation) claimState {
	state := claimState{claims: guardedClaims(s), destinations: make(map[string]string, len(s.vehicles)), moves: s.rebalanceMoves}
	for index := range s.vehicles {
		state.destinations[s.vehicles[index].Pod.ID] = s.vehicles[index].destination.ID
	}
	return state
}

// checkNewClaimsLeaveABerth checks that each guarded claim that started in
// the last step left another available berth at its station. It returns
// the number of new claims. A berth was available at the claim when it is
// available now, or when only pods that chose it in the step hold it or go
// to it. After the guarded check, only a pod that chooses a berth can take
// one.
func checkNewClaimsLeaveABerth(t *testing.T, s *Simulation, before claimState) int {
	t.Helper()
	chose := func(v *vehicle, berthID string) bool {
		return before.destinations[v.Pod.ID] != berthID && v.destination.ID == berthID
	}
	claims := 0
	for podID, berthID := range guardedClaims(s) {
		if before.claims[podID] == berthID {
			continue
		}
		claims++
		v := s.findVehicle(podID)
		station, _ := s.station(v.destinationStation)
		free := 0
		for _, berth := range station.Berths {
			if berth.ID == berthID || slices.ContainsFunc(s.waiting, func(trip waitingTrip) bool { return trip.destination.ID == berth.ID }) {
				continue
			}
			taken := false
			for _, r := range berthResources(berth) {
				if owner := s.findVehicle(s.owners[r]); owner != nil && !chose(owner, berth.ID) {
					taken = true
				}
			}
			for index := range s.vehicles {
				if other := &s.vehicles[index]; other.Pod.Activity != Idle && other.destination.ID == berth.ID && !chose(other, berth.ID) {
					taken = true
				}
			}
			if !taken {
				free++
			}
		}
		if free == 0 {
			t.Fatalf("tick %d: pod %s took the last available berth of %s", s.tick, podID, station.ID)
		}
	}
	return claims
}

// TestGuardedInvariants runs guarded mode on a line with one-berth and
// two-berth stations. Each tick, it checks that the moves do not pass the
// boardings, that a guarded claim leaves an available berth, the track and
// berth safety, and the owners.
func TestGuardedInvariants(t *testing.T) {
	t.Parallel()
	stations := lineStations(6, 1, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1, 2)
	fleet := place("p-1", "p-2", "p-3", "p-4", "p-5", "p-6", "s0-1", "s1-1", "s2-1", "s3-1", "s4-1", "s5-1", "s6-1", "s7-1", "s8-1", "s9-1")
	s := newLineSimulation(t, stations, fleet)
	// Requests every 80 s are active for a fleet of 16.
	rng := rand.New(rand.NewPCG(1, 2))
	refills, bumps := 0, 0
	for tick := range 1500 * TicksPerSecond {
		if tick > 0 && tick%(80*TicksPerSecond) == 0 {
			from := rng.IntN(12)
			to := (from + 1 + rng.IntN(11)) % 12
			if err := s.RequestTrip(fmt.Sprintf("s%d", from), fmt.Sprintf("s%d", to)); err != nil {
				t.Fatal(err)
			}
		}
		before := newClaimState(s)
		s.Step()
		if s.rebalanceMoves > s.boarded {
			t.Fatalf("tick %d: %d moves for %d boardings", s.tick, s.rebalanceMoves, s.boarded)
		}
		claims := checkNewClaimsLeaveABerth(t, s, before)
		checkTraffic(t, s.Snapshot())
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatalf("tick %d: %v", s.tick, err)
		}
		checkIncrementalOwners(t, s)
		refills += s.rebalanceMoves - before.moves
		bumps += claims - (s.rebalanceMoves - before.moves)
	}
	if refills == 0 || bumps == 0 || s.completed == 0 {
		t.Fatalf("the run made %d refills and %d guarded bumps and completed %d trips", refills, bumps, s.completed)
	}
}

// guardedInputs is the input of each guarded decision.
type guardedInputs struct {
	gate       guardedGate
	view       guardedView
	candidates []int
	found      bool
}

func newGuardedInputs(s *Simulation) guardedInputs {
	inputs := guardedInputs{gate: s.guardedGate()}
	inputs.view = s.guardedView(inputs.gate)
	inputs.candidates, inputs.found = s.guardedCandidates(inputs.view)
	return inputs
}

// TestGuardedRestoreKeepsDecisions saves a guarded run while the gate is
// open and a guarded move and a passenger are under way. The gate reads
// only saved state and the demand rate. The view uses the estimate of
// availableAfter, which depends on pod speed as dispatch does. In this run,
// each restore gives the inputs of the saved run when the demand rate is
// set again. Two restores of the save then run the same.
func TestGuardedRestoreKeepsDecisions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// parking is the number of parking berths, each with a pod.
		parking int
		// rate is the demand rate. The requests come each 80 s, which is 0.75
		// per minute.
		rate int
	}{
		// The rate of 0.75 per minute is active for a fleet of 16.
		{name: "mean rate", parking: 6},
		// A demand rate of 1 per minute is active for a fleet of 22.
		{name: "demand rate", parking: 12, rate: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkGuardedRestore(t, tc.parking, tc.rate)
		})
	}
}

// checkGuardedRestore runs TestGuardedRestoreKeepsDecisions with the given
// parking berths and demand rate.
func checkGuardedRestore(t *testing.T, parking, rate int) {
	t.Helper()
	stations := lineStations(parking, 1, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1, 2)
	var fleet []Placement
	for index := range parking {
		fleet = append(fleet, Placement{StationID: "p", BerthID: fmt.Sprintf("p-%d", index+1)})
	}
	for index := range 10 {
		fleet = append(fleet, Placement{StationID: fmt.Sprintf("s%d", index), BerthID: fmt.Sprintf("s%d-1", index)})
	}
	for index := range fleet {
		fleet[index].ID = fmt.Sprintf("%02d", index+1)
	}
	// Without a demand rate, the gate reads 3 requests in 4 minutes.
	count, ticks := 3, int64(4*ticksPerMinute)
	if rate > 0 {
		count, ticks = rate, ticksPerMinute
	}
	if !guardedRateLow(count, ticks, len(fleet)) {
		t.Fatalf("the gate of a fleet of %d pods is not active", len(fleet))
	}
	rng := rand.New(rand.NewPCG(3, 4))
	var trips []timedTrip
	for second := 80; second < 1200; second += 80 {
		from := rng.IntN(12)
		trips = append(trips, timedTrip{second: second, from: fmt.Sprintf("s%d", from), to: fmt.Sprintf("s%d", (from+1+rng.IntN(11))%12)})
	}
	step := func(s *Simulation) {
		t.Helper()
		for _, trip := range trips {
			if int64(trip.second*TicksPerSecond) == s.tick {
				if err := s.RequestTrip(trip.from, trip.to); err != nil {
					t.Fatal(err)
				}
			}
		}
		s.Step()
	}
	live := newLineSimulation(t, stations, fleet)
	if err := live.SetDemandRate(rate); err != nil {
		t.Fatal(err)
	}
	underWay := func() bool {
		rebalancing, carrying := false, false
		for index := range live.vehicles {
			v := &live.vehicles[index]
			rebalancing = rebalancing || v.Rebalancing
			carrying = carrying || v.Pod.Occupied && v.Pod.Activity == Traveling
		}
		return rebalancing && carrying && live.guardedGate().open
	}
	for live.tick < 400*TicksPerSecond || !underWay() {
		if live.tick >= 1000*TicksPerSecond {
			t.Fatal("no guarded move and passenger were under way with an open gate")
		}
		step(live)
	}
	state := live.ExportState()
	want := newGuardedInputs(live)
	var restored []*Simulation
	for range 2 {
		s, result, err := RestoreState(RestoreStateInput{Network: lineNetwork(stations), Fleet: fleet, State: state})
		if err != nil || !cleanRestore(result) {
			t.Fatalf("restore: %v, %+v", err, result)
		}
		if err := s.SetPositioning(PositioningGuarded); err != nil {
			t.Fatal(err)
		}
		// The saved state does not keep the demand rate.
		if got := newGuardedInputs(s); rate > 0 && reflect.DeepEqual(got, want) {
			t.Fatal("the restore gives the inputs of the saved run with no demand rate")
		}
		if err := s.SetDemandRate(rate); err != nil {
			t.Fatal(err)
		}
		if got := newGuardedInputs(s); !reflect.DeepEqual(got, want) {
			t.Fatalf("restored guarded inputs %+v, want %+v", got, want)
		}
		restored = append(restored, s)
	}
	start := restored[0].rebalanceMoves
	for range 200 * TicksPerSecond {
		for _, s := range restored {
			step(s)
		}
		if !reflect.DeepEqual(restored[0].Snapshot(), restored[1].Snapshot()) {
			t.Fatalf("the restores differ at tick %d", restored[0].tick)
		}
	}
	if restored[0].rebalanceMoves == start {
		t.Fatal("the restores made no guarded move")
	}
}
