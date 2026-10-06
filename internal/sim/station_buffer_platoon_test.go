package sim

import (
	"maps"
	"reflect"
	"testing"
)

func bufferedPlatoonFixture(t *testing.T, count int) *Simulation {
	t.Helper()

	return stagedBufferQueue(t, count, stationBufferNetwork(Example(), 8), true)
}

func stagedBufferQueue(t *testing.T, count int, network Network, form bool) *Simulation {
	t.Helper()
	placements := []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "03", StationID: "parking", BerthID: "parking-1"}, {ID: "04", StationID: "parking", BerthID: "parking-2"}}
	return stageBufferFleet(t, network, placements[:count], count, form)
}

func stageBufferFleet(t *testing.T, network Network, placements []Placement, count int, form bool) *Simulation {
	t.Helper()

	for i := range network.Stations {
		if network.Stations[i].ID == "parking" {
			network.Stations[i].ParkingOnly = false
		}
	}

	s, err := NewFleet(network, placements)
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	for _, placement := range placements[:count] {
		if requestErr := s.RequestJourney(placement.ID, "market"); requestErr != nil {
			t.Fatal(requestErr)
		}
	}
	for range 60 * TicksPerSecond {
		s.Step()
	}
	saved := s.ExportState()
	for i := range count {
		v := &s.vehicles[i]
		if v.Pod.Activity != Traveling {
			t.Fatal("fixture did not depart")
		}
		start, offset, _ := s.savedStart(v)
		last := len(v.Route) - 1
		position := float64(120 + (count-i)*45)
		pod := &saved.Pods[i]
		pod.RouteIndex = last - start
		pod.LaneID = v.Route[last].ID
		pod.LaneDistance = position
		pod.Distance = v.blocks.lanes[last].start - offset + position
		pod.Waiting = false
		pod.WaitSince = 0
	}
	restored, result, err := RestoreState(RestoreStateInput{Network: network, Fleet: s.initial, State: saved})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("staged queue restore: %+v %v", result, err)
	}
	s = restored
	s.SetStationBuffers(true)
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	s.owners[resource{kind: berthResource, id: "market-1"}] = podResourceOwner("external")
	if form {
		s.formPlatoons()
	}
	if form && s.CoupledPods() != count {
		t.Fatalf("no %d-pod entry platoon: %+v", count, s.Snapshot())
	}
	return s

}

func TestStationBufferPlatoonBlockedDeparture(t *testing.T) {
	t.Parallel()
	fleet := []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}, {ID: "05", StationID: "market"}}
	s := stageBufferFleet(t, stationBufferNetwork(Example(), 8), fleet, 2, true)
	s.owners[resource{kind: berthResource, id: "market-1"}] = podResourceOwner("05")
	barrier := s.laneCells["market-out"].cell(0)[0]
	s.owners[barrier] = podResourceOwner("external")
	sawStopped, restores := false, 0
	for range 300 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		if s.NeedsBufferPlatoonState() {
			_, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState()})
			if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
				t.Fatalf("blocked departure restore: %+v %v", result, err)
			}
			restores++
		}
		head := s.findVehicle("01")
		plan, ok := s.bufferPlan(head)
		sawStopped = sawStopped || ok && head.Pod.Speed == 0 && head.distance == head.blocks.end(plan.frontier)
	}
	if !sawStopped || restores == 0 || s.completed != 0 || s.findVehicle("05").Pod.BerthID != "market-1" {
		t.Fatal("entry platoon did not wait behind the blocked berth departure")
	}
	delete(s.owners, barrier)
	for range 600 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		if s.completed == 2 {
			return
		}
	}
	t.Fatal("entry platoon did not drain after the berth departure cleared")
}

func TestStationBufferPlatoonDifferentBerths(t *testing.T) {
	t.Parallel()
	s := stagedBufferQueue(t, 3, stationBufferNetwork(twoBerthMarket(), 8), true)
	delete(s.owners, resource{kind: berthResource, id: "market-1"})
	seen := make(map[string]bool)
	for range 600 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		for _, v := range s.vehicles {
			if v.destination.ID != "" {
				seen[v.destination.ID] = true
			}
		}
		if s.completed == 3 {
			if !seen["market-1"] || !seen["market-2"] {
				t.Fatal("entry queue did not choose different exclusive berth suffixes")
			}
			return
		}
	}
	t.Fatal("multi-berth entry platoon did not drain")
}

func TestStationBufferPlatoonFixedEndpoint(t *testing.T) {
	t.Parallel()
	for _, count := range []int{2, 3, 4} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			t.Parallel()
			s := bufferedPlatoonFixture(t, count)
			for i := range s.vehicles {
				v := &s.vehicles[i]
				if !v.link.buffer {
					continue
				}
				link := v.link
				plan, ok := s.bufferPlan(v)
				geometry, end := linkEnds(&v.blocks, link)
				if !ok || link.lanes != 1 || link.terminalCell != plan.frontier-plan.first || geometry != v.blocks.end(plan.frontier) || end >= plan.frontier {
					t.Fatalf("invalid fixed endpoint: %+v %+v", link, plan)
				}
				if link.first != plan.entryStop+1 || s.coupledSpan(v, plan.entryStop, link.end) || s.coupledSpan(v, link.first, plan.frontier) {
					t.Fatal("shared span crossed a boundary group")
				}
				for _, b := range v.blocks.span(link.first, link.end+1) {
					for _, claimed := range b.resources {
						if claimed.kind != trackResource || resourceReleaseDistance(b, claimed)+platoonDrainSlack > geometry {
							t.Fatal("shared cell contains a conflict or cannot drain before the endpoint")
						}
					}
				}
				s.extendLink(v, &s.vehicles[v.link.leader-1])
				if v.link != link {
					t.Fatal("entry certificate grew")
				}
			}
		})
	}
}

func TestStationBufferPlatoonHeadDischarge(t *testing.T) {
	t.Parallel()
	s := bufferedPlatoonFixture(t, 3)
	delete(s.owners, resource{kind: berthResource, id: "market-1"})
	extendedWithFollower := false
	for range 600 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		for _, v := range s.vehicles {
			if v.destination.ID != "" && v.follower != 0 && s.vehicles[v.follower-1].link.buffer && s.vehicles[v.follower-1].link.draining {
				extendedWithFollower = true
			}
		}
		if s.completed == 3 {
			if !extendedWithFollower {
				t.Fatal("head never extended while follower ownership drained")
			}
			return
		}
	}
	t.Fatalf("entry platoon did not discharge: %+v", s.Snapshot())
}

func TestStationBufferPlatoonRestoreTransitions(t *testing.T) {
	t.Parallel()
	skipLong(t)
	for _, count := range []int{2, 3, 4} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			t.Parallel()
			checkBufferPlatoonRestoreTransitions(t, count)
		})
	}
}

func checkBufferPlatoonRestoreTransitions(t *testing.T, count int) {
	t.Helper()
	s := bufferedPlatoonFixture(t, count)
	delete(s.owners, resource{kind: berthResource, id: "market-1"})
	checks, sharedChecks, transfers := 0, 0, 0
	for range 600 * TicksPerSecond {
		if s.NeedsBufferPlatoonState() {
			state := s.ExportState()
			restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state})
			if err != nil || result.Tier != RestorePhysical || result.PhysicalError != nil || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
				t.Fatalf("tick %d restore: %+v %v", s.tick, result, err)
			}
			if !restored.NeedsBufferPlatoonState() || restored.stationBuffers {
				t.Fatal("restore lost the certificate or enabled new admissions")
			}
			if got := restored.ExportState(); !reflect.DeepEqual(got, state) {
				t.Fatalf("tick %d changed saved state on physical restore\ngot: %+v\nwant: %+v", s.tick, got, state)
			}
			for i := range restored.vehicles {
				v := &restored.vehicles[i]
				if !v.link.buffer {
					continue
				}
				for _, claimed := range v.footprint(v.reservedThrough, v.distance) {
					owner := s.owners[claimed]
					if !owner.isZero() && owner != podResourceOwner(v.Pod.ID) && s.ownerAheadInPlatoon(&s.vehicles[i], owner) {
						if restored.owners[claimed] != owner {
							t.Fatal("restore changed a required ancestor holding")
						}
						sharedChecks++
					}
				}
			}
			checks++
		}
		owners := maps.Clone(s.owners)
		s.Step()
		for claimed, previous := range owners {
			if owner := s.owners[claimed]; claimed.kind == trackResource && claimed.id == "market-approach" && !owner.isZero() && owner != previous {
				transfers++
			}
		}
		if s.completed == count {
			if checks == 0 || count > 2 && sharedChecks == 0 || transfers == 0 {
				t.Fatalf("missing transition evidence: restores=%d shared=%d transfers=%d", checks, sharedChecks, transfers)
			}
			return
		}
	}
	t.Fatal("transition fixture did not finish")
}

func TestStationBufferPlatoonRestoredDrain(t *testing.T) {
	t.Parallel()
	s := bufferedPlatoonFixture(t, 4)
	found := false
	for range 300 * TicksPerSecond {
		s.Step()
		for _, v := range s.vehicles {
			if v.link.buffer && s.holdsPending(&v) {
				found = true
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no shared holding to restore")
	}
	state := s.ExportState()
	prepared, err := PrepareNetwork(s.network)
	if err != nil {
		t.Fatal(err)
	}
	for _, logical := range []bool{false, true} {
		restored, result, err := prepared.RestoreState(PreparedRestoreInput{Fleet: s.initial, State: state, LogicalOnly: logical})
		want := RestorePhysical
		if logical {
			want = RestoreLogical
		}
		if err != nil || result.Tier != want || result.PhysicalError != nil || len(result.Demoted) != 0 {
			t.Fatalf("prepared logical=%t restore: %+v %v", logical, result, err)
		}
		if logical {
			if restored.NeedsBufferPlatoonState() || len(result.Requeued) != 4 {
				t.Fatal("explicit logical recovery lost orders or retained physical links")
			}
			continue
		}
		for range 600 * TicksPerSecond {
			restored.Step()
			if _, err := restored.SafetyObservation().Check(); err != nil {
				t.Fatal(err)
			}
			if restored.completed == 4 {
				break
			}
		}
		if restored.completed != 4 || restored.NeedsBufferState() || restored.NeedsBufferPlatoonState() {
			t.Fatalf("restored members did not drain with both policies off: %+v", restored.Snapshot())
		}
	}
}

func TestStationBufferPlatoonCurvedEntry(t *testing.T) {
	t.Parallel()
	network := stationBufferNetwork(Example(), 8)
	for i := range network.Lanes {
		lane := &network.Lanes[i]
		if lane.ID != "market-approach" {
			continue
		}
		from, _ := network.Node(lane.From)
		to, _ := network.Node(lane.To)
		lane.Control = &Point{X: (from.Position.X + to.Position.X) / 2, Y: (from.Position.Y+to.Position.Y)/2 + 60}
	}
	s := stagedBufferQueue(t, 3, network, true)
	for _, v := range s.vehicles {
		if v.link.buffer && v.link.turn <= 0 {
			t.Fatal("curved certificate lost conservative turn")
		}
	}
	for range 300 * TicksPerSecond {
		s.Step()
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
	}
}
func TestStationBufferPlatoonRejectsUnequalSpeeds(t *testing.T) {
	t.Parallel()
	network := stationBufferNetwork(Example(), 8)
	for i := range network.Lanes {
		if network.Lanes[i].ID == "market-in" {
			network.Lanes[i].SpeedLimit = 7
		}
	}
	s := stagedBufferQueue(t, 2, network, false)
	s.formPlatoons()
	if s.CoupledPods() != 0 {
		t.Fatal("unequal-speed entry formed a link")
	}
}
func TestStationBufferPlatoonFailedSuffixKeepsPrefix(t *testing.T) {
	t.Parallel()
	s := bufferedPlatoonFixture(t, 3)
	headIndex := -1
	for range 300 * TicksPerSecond {
		s.Step()
		for i := range s.vehicles {
			v := &s.vehicles[i]
			plan, ok := s.bufferPlan(v)
			if ok && v.link.leader == 0 && v.follower != 0 && v.reservedThrough >= plan.frontier && s.holdsPending(&s.vehicles[v.follower-1]) {
				headIndex = i
				break
			}
		}
		if headIndex >= 0 {
			break
		}
	}
	if headIndex < 0 {
		t.Fatal("no head with retained shared holdings")
	}
	delete(s.owners, resource{kind: berthResource, id: "market-1"})
	s.owners[resource{kind: trackResource, id: "market-in", cell: 0}] = podResourceOwner("external")
	v := &s.vehicles[headIndex]
	plan, _ := s.bufferPlan(v)
	before := *v
	owners := maps.Clone(s.owners)
	link := s.vehicles[v.follower-1].link
	s.grantBufferedHead(intent{index: headIndex, block: v.reservedThrough + 1}, plan)
	if !reflect.DeepEqual(v.Route, before.Route) || v.routeVersion != before.routeVersion || v.destination != before.destination || !maps.Equal(owners, s.owners) || s.vehicles[v.follower-1].link != link {
		t.Fatal("denied suffix changed prefix, ownership or draining state")
	}
}
func TestStationBufferPlatoonDisabledDrain(t *testing.T) {
	t.Parallel()
	for _, modeOff := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffers", true: "platoons"}[modeOff], func(t *testing.T) {
			t.Parallel()
			s := bufferedPlatoonFixture(t, 3)
			if modeOff {
				if err := s.SetPlatooning(PlatooningOff); err != nil {
					t.Fatal(err)
				}
			} else {
				s.SetStationBuffers(false)
			}
			delete(s.owners, resource{kind: berthResource, id: "market-1"})
			for range 600 * TicksPerSecond {
				s.Step()
				if _, err := s.SafetyObservation().Check(); err != nil {
					t.Fatal(err)
				}
				if s.completed == 3 {
					if s.NeedsBufferPlatoonState() {
						t.Fatal("drained certificates retained version4")
					}
					return
				}
			}
			t.Fatal("disabled entry platoon did not drain")
		})
	}
}

func TestStationBufferPlatoonRejectsMalformedState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func(*SavedState)
	}{
		{"missing predecessor", func(s *SavedState) { s.Pods[1].Platoon.Leader = "missing" }},
		{"self predecessor", func(s *SavedState) { s.Pods[1].Platoon.Leader = s.Pods[1].ID }},
		{"duplicate followers", func(s *SavedState) { s.Pods[2].Platoon.Leader = s.Pods[0].ID }},
		{"cycle", func(s *SavedState) { s.Pods[1].Platoon.Leader = s.Pods[2].ID }},
		{"mixed turns", func(s *SavedState) { s.Pods[2].Platoon.Turn += 0.01 }},
		{"mixed kinds", func(s *SavedState) { s.Pods[1].Platoon.Kind, s.Pods[1].Platoon.TerminalCell = "", nil }},
		{"missing endpoint", func(s *SavedState) { s.Pods[1].Platoon.TerminalCell = nil }},
		{"different endpoint", func(s *SavedState) { (*s.Pods[1].Platoon.TerminalCell)-- }},
		{"out of range endpoint", func(s *SavedState) { *s.Pods[1].Platoon.TerminalCell = 1_000_000 }},
		{"negative endpoint", func(s *SavedState) { *s.Pods[1].Platoon.TerminalCell = -1 }},
		{"unknown kind", func(s *SavedState) { s.Pods[1].Platoon.Kind = "other" }},
		{"several lanes", func(s *SavedState) { s.Pods[1].Platoon.Lanes = 2 }},
		{"no membership", func(s *SavedState) { s.Pods[1].StationBuffered = false }},
		{"wrong lane identity", func(s *SavedState) { s.Pods[1].LaneID = "wrong" }},
		{"bad route index", func(s *SavedState) { s.Pods[1].RouteIndex = len(s.Pods[1].Route) }},
		{"inconsistent route distance", func(s *SavedState) { s.Pods[1].Distance = 1e9 }},
		{"negative lane distance", func(s *SavedState) { s.Pods[1].LaneDistance = -1 }},
		{"outside lane distance", func(s *SavedState) { s.Pods[1].LaneDistance = 1e9 }},
		{"predecessor route distance", func(s *SavedState) { s.Pods[0].Distance = 1e9 }},
		{"too close", func(s *SavedState) {
			s.Pods[1].LaneDistance, s.Pods[1].Distance = s.Pods[0].LaneDistance, s.Pods[0].Distance
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := bufferedPlatoonFixture(t, 3)
			for _, logical := range []bool{false, true} {
				state := s.ExportState()
				tc.edit(&state)
				_, _, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, LogicalOnly: logical})
				if err == nil {
					t.Fatalf("logical=%t accepted invalid buffer certificate", logical)
				}
			}
		})
	}
}
