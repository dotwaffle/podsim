package sim

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
)

func TestStationBufferRecruitmentBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		leader, follower float64
		want             bool
	}{
		{"adjacent interior", 240, 210, true},
		{"beyond one cell", 240, 209.99, false},
		{"entry boundary", 90, 60, true},
		{"before holding region", 75, 45, false},
		{"frontier exclusive cell", 450, 420, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := stationBufferNetwork(Example(), 8)
			for i := range network.Lanes {
				network.Lanes[i].SpeedLimit = 2.5
			}
			s := stagedBufferQueue(t, 2, network, false)
			delete(s.owners, resource{kind: berthResource, id: "market-1"})
			state := s.ExportState()
			for i, position := range []float64{tc.leader, tc.follower} {
				pod := &state.Pods[i]
				pod.Distance += position - pod.LaneDistance
				pod.LaneDistance = position
			}
			r := restoreBufferQueueState(t, s, state)
			r.SetStationBuffers(true)
			if err := r.SetPlatooning(PlatooningVirtual); err != nil {
				t.Fatal(err)
			}
			r.formPlatoons()
			if got := r.CoupledPods() == 2; got != tc.want {
				t.Fatalf("coupled=%t want=%t", got, tc.want)
			}
		})
	}
}

func TestStationBufferRecruitmentLeavesRoadsUnchanged(t *testing.T) {
	t.Parallel()
	network := mergeCorridor(false, 0)
	for i := range network.Lanes {
		network.Lanes[i].SpeedLimit = 2.5
	}
	route := []string{"main", "exit", "approach"}
	s := restoreCorridor(t, network, []corridorPod{{route: route, station: "dest", distance: 1020}, {route: route, station: "dest", distance: 990}})
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	s.formPlatoons()
	if s.CoupledPods() != 0 {
		t.Fatal("one-cell buffer recruitment extended to ordinary roads")
	}
}

func TestStationBufferRecruitmentFractionalPitch(t *testing.T) {
	t.Parallel()
	network := stationBufferNetwork(Example(), 8)
	for i := range network.Lanes {
		network.Lanes[i].SpeedLimit = 2.5
	}
	for i := range network.Nodes {
		if network.Nodes[i].ID == "market-entry" {
			network.Nodes[i].Position.X = 5360 + 271
		}
	}
	s := stagedBufferQueue(t, 2, network, false)
	delete(s.owners, resource{kind: berthResource, id: "market-1"})
	state := s.ExportState()
	for i := range state.Pods {
		pod := &state.Pods[i]
		position := cellOffset(4-i, 10, 271)
		pod.Distance += position - pod.LaneDistance
		pod.LaneDistance = position
	}
	s = restoreBufferQueue(t, restoreBufferQueueState(t, s, state), true)
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	s.formPlatoons()
	if s.CoupledPods() != 2 {
		t.Fatal("fractional cell pitch prevented adjacent recruitment")
	}
	stepBufferQueue(t, s, newPlatoonMonitor(s))
}

func checkBufferCertificate(t *testing.T, v *vehicle) {
	t.Helper()
	link := v.link
	geometry, end := linkEnds(&v.blocks, link)
	if link.lanes != 1 || link.end != end || link.first <= v.blocks.laneFirst(link.lane) ||
		link.terminalCell >= v.blocks.lanes[link.lane].cells.count()-1 {
		t.Fatalf("invalid fixed entry certificate: %+v", link)
	}
	for _, b := range v.blocks.span(link.first, link.end+1) {
		for _, r := range b.resources {
			if r.kind != trackResource || resourceReleaseDistance(b, r)+platoonDrainSlack > geometry {
				t.Fatalf("fixed entry shares an endpoint or conflict: %+v %+v", link, r)
			}
		}
	}
}

// occupiedBufferQueue has four arrivals and real pods in every reachable berth.
// A one-way return prevents clearing into the free origin berths.
func occupiedBufferQueue(t *testing.T, mode Platooning) *Simulation {
	t.Helper()
	network := stationBufferNetwork(Example(), 8)
	for _, id := range []string{"harbor", "garden"} {
		station := &network.Stations[slices.IndexFunc(network.Stations, func(s Station) bool { return s.ID == id })]
		first, _ := network.Node(station.Berths[0].Node)
		first.ID = id + "-berth-2"
		first.Position.Y -= 400
		network.Nodes = append(network.Nodes, first)
		station.Berths = append(station.Berths, Berth{ID: id + "-2", Node: first.ID})
		network.Lanes = append(network.Lanes,
			Lane{ID: id + "-in-2", From: station.Entry, To: first.ID, SpeedLimit: 2.5, StationID: id, StationRole: StationBerthAccessRole},
			Lane{ID: id + "-out-2", From: first.ID, To: station.Exit, SpeedLimit: 2.5, StationID: id, StationRole: StationDepartureRole})
	}
	network.Lanes = slices.DeleteFunc(network.Lanes, func(l Lane) bool { return l.ID == "return" })
	network.Lanes = append(network.Lanes, Lane{ID: "parking-loop", From: "parking-exit", To: "merge", SpeedLimit: 2.5})
	for i := range network.Lanes {
		network.Lanes[i].SpeedLimit = 2.5
	}
	fleet := []Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
		{ID: "03", StationID: "harbor", BerthID: "harbor-2"},
		{ID: "04", StationID: "garden", BerthID: "garden-2"},
		{ID: "05", StationID: "market"},
		{ID: "06", StationID: "parking", BerthID: "parking-1"},
		{ID: "07", StationID: "parking", BerthID: "parking-2"},
	}
	s := stageBufferFleet(t, network, fleet, 4, false)
	s.owners[resource{kind: berthResource, id: "market-1"}] = "05"
	if err := s.SetPlatooning(mode); err != nil {
		t.Fatal(err)
	}
	checkIncrementalOwners(t, s)
	return s
}

func restoreBufferQueue(t *testing.T, s *Simulation, enabled bool) *Simulation {
	t.Helper()
	r := restoreBufferQueueState(t, s, s.ExportState())
	if err := r.SetReservationLookahead(s.reservationLookaheadSeconds); err != nil {
		t.Fatal(err)
	}
	r.SetStationBuffers(enabled)
	mode := PlatooningOff
	if enabled {
		mode = s.platooning
	}
	if err := r.SetPlatooning(mode); err != nil {
		t.Fatal(err)
	}
	return r
}

func restoreBufferQueueState(t *testing.T, s *Simulation, state SavedState) *Simulation {
	t.Helper()
	r, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, StationBuffers: true, BufferPlatoons: true})
	if err != nil || result.Tier != RestorePhysical || result.PhysicalError != nil || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
		t.Fatalf("physical restore: %+v %v", result, err)
	}
	if !reflect.DeepEqual(r.ExportState(), state) {
		t.Fatal("physical restore changed the queue or its real blocker")
	}
	return r
}

func stepBufferQueue(t *testing.T, s *Simulation, m *platoonMonitor) {
	t.Helper()
	s.Step()
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	m.check(t)
	for _, v := range s.vehicles {
		if v.Pod.Activity == Traveling && v.Pod.Speed > v.Route[v.blocks.routeLane(v.blockIndex)].SpeedLimit+1e-9 {
			t.Fatal("pod exceeded its lane speed")
		}
	}
}

func TestStationBufferRecruitmentOccupiedQueue(t *testing.T) {
	t.Parallel()
	for _, mode := range []Platooning{PlatooningOff, PlatooningVirtual} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			t.Parallel()
			s := occupiedBufferQueue(t, mode)
			m := newPlatoonMonitor(s)
			stable := 0
			for range 300 * TicksPerSecond {
				stepBufferQueue(t, s, m)
				stopped := true
				for _, v := range s.vehicles[:4] {
					stopped = stopped && v.Pod.Activity == Traveling && v.Pod.Speed == 0
				}
				if stopped {
					stable++
				} else {
					stable = 0
				}
				if stable == 5*TicksPerSecond {
					break
				}
			}
			wantSpan, wantCoupled := 90.0, 0
			if mode == PlatooningVirtual {
				wantSpan, wantCoupled = 54.02, 3
			}
			span := s.vehicles[0].Pod.LaneDistance - s.vehicles[3].Pod.LaneDistance
			if stable < 5*TicksPerSecond || math.Abs(span-wantSpan) > 1e-6 || s.CoupledPods() != wantCoupled || s.findVehicle("05").Pod.Activity != Idle {
				t.Fatalf("queue did not settle behind real berth pod: span %.6f coupled %d state %+v", span, s.CoupledPods(), s.Snapshot())
			}
			t.Logf("mode=%v settled_seconds=%.6f span_m=%.6f coupled=%d", mode, float64(s.tick-60*TicksPerSecond)/TicksPerSecond, span, s.CoupledPods())
			for _, enabled := range []bool{true, false} {
				t.Run(fmt.Sprintf("restore_enabled_%t", enabled), func(t *testing.T) {
					r := restoreBufferQueue(t, s, enabled)
					monitor := newPlatoonMonitor(r)
					for range 5 * TicksPerSecond {
						stepBufferQueue(t, r, monitor)
					}
					if r.completed != 0 || r.findVehicle("05").Pod.BerthID != "market-1" {
						t.Fatal("restore lost the occupied berth")
					}

				})
			}
		})
	}
}

func departingBufferQueue(t *testing.T) *Simulation {
	t.Helper()
	network := stationBufferNetwork(Example(), 8)
	for i := range network.Nodes {
		switch network.Nodes[i].ID {
		case "market-berth":
			network.Nodes[i].Position = Point{X: 5865, Y: 2080}
		case "market-exit":
			network.Nodes[i].Position = Point{X: 5890, Y: 2105}
		}
	}
	for i := range network.Lanes {
		if network.Lanes[i].StationID == "market" {
			network.Lanes[i].SpeedLimit = 2.5
		}
	}
	fleet := []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"},
		{ID: "03", StationID: "parking", BerthID: "parking-1"}, {ID: "04", StationID: "parking", BerthID: "parking-2"},
		{ID: "05", StationID: "market"}}
	s := stageBufferFleet(t, network, fleet, 4, false)
	s.owners[resource{kind: berthResource, id: "market-1"}] = "05"
	state := s.ExportState()
	for i := range 4 {
		pod := &state.Pods[i]
		position := float64(300 - i*30)
		pod.Distance += position - pod.LaneDistance
		pod.LaneDistance = position
	}
	s, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: fleet, State: state, StationBuffers: true})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("adjacent queue restore: %+v %v", result, err)
	}
	s.SetStationBuffers(true)
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestJourney("05", "harbor"); err != nil {
		t.Fatal(err)
	}
	return s
}

func drainBufferQueue(t *testing.T, s *Simulation) {
	t.Helper()
	m := newPlatoonMonitor(s)
	for range 3600 * TicksPerSecond {
		stepBufferQueue(t, s, m)
		if s.completed == 5 {
			if s.NeedsBufferPlatoonState() {
				t.Fatal("discharged queue retained a buffer certificate")
			}
			return
		}
	}
	t.Fatalf("real queue did not discharge: %+v", s.Snapshot())
}

func TestStationBufferRecruitmentRealDeparture(t *testing.T) {
	t.Parallel()
	s := departingBufferQueue(t)
	// The existing long lookahead exercises suffix commitment with retained cells.
	if err := s.SetReservationLookahead(maxReservationLookaheadSeconds); err != nil {
		t.Fatal(err)
	}
	m := newPlatoonMonitor(s)
	phases := make(map[string]*Simulation)
	for range 3600 * TicksPerSecond {
		stepBufferQueue(t, s, m)
		for i := range s.vehicles {
			v := &s.vehicles[i]
			if v.destination.ID != "" && v.follower != 0 && s.vehicles[v.follower-1].link.buffer &&
				s.vehicles[v.follower-1].link.draining && phases["suffix"] == nil {
				phases["suffix"] = s.Clone()
			}
			if !v.link.buffer {
				continue
			}
			leader := &s.vehicles[v.link.leader-1]
			gap := leaderPosition(v, leader, v.link) - v.distance
			if gap > v.link.clearance+stoppingDistance(v.Route[v.link.lane].SpeedLimit) && phases["wide"] == nil {
				phases["wide"] = s.Clone()
			}
			phase := ""
			if s.holdsPending(v) {
				phase = "shared"
			}
			if v.link.draining {
				phase = "draining"
			}
			if phase != "" && phases[phase] == nil {
				phases[phase] = s.Clone()
			}
			if gap < v.link.clearance+0.01 && phases["compacted"] == nil {
				phases["compacted"] = s.Clone()
			}
		}
		if s.completed == 5 {
			t.Logf("completed=%d final_tick=%d coupled_distinct=%d", s.completed, s.tick, len(m.coupled))
			break
		}
	}
	if s.completed != 5 || s.NeedsBufferPlatoonState() {
		t.Fatalf("queue did not discharge after the real berth departure: %+v", s.Snapshot())
	}
	for _, phase := range []string{"wide", "shared", "compacted", "draining", "suffix"} {
		checkpoint := phases[phase]
		if checkpoint == nil {
			t.Fatalf("missing %s restore phase", phase)
		}
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s_enabled_%t", phase, enabled), func(t *testing.T) {
				restored := restoreBufferQueue(t, checkpoint, enabled)
				t.Parallel()
				drainBufferQueue(t, restored)
			})
		}
	}
}

func TestStationBufferRecruitmentDefaultDeparture(t *testing.T) {
	t.Parallel()
	drainBufferQueue(t, departingBufferQueue(t))
}
