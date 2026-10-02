package sim

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"testing"
)

func stationBufferNetwork(network Network, scale float64) Network {
	for index := range network.Nodes {
		network.Nodes[index].Position.X *= scale
		network.Nodes[index].Position.Y *= scale
	}
	for index := range network.Lanes {
		if network.Lanes[index].ID == "market-approach" {
			network.Lanes[index].StationRole = StationEntryRole
		}
	}
	return network
}

func stationBufferFixture(t *testing.T) *Simulation {
	t.Helper()
	network := stationBufferNetwork(Example(), 4)
	s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	if requestErr := s.RequestJourney("01", "market"); requestErr != nil {
		t.Fatal(requestErr)
	}
	if _, ok := s.bufferPlan(&s.vehicles[0]); !ok {
		t.Fatal("fixture has no eligible holding region")
	}
	return s
}

func stoppedBufferFixture(t *testing.T) *Simulation {
	t.Helper()
	s := stationBufferFixture(t)
	// An external berth reservation holds the head while it reaches its
	// stopping frontier. It has no pod that the clearing controller can move.
	s.owners[resource{kind: berthResource, id: "market-1"}] = "external"
	for range 600 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		v := &s.vehicles[0]
		if plan, ok := s.bufferPlan(v); ok && v.Pod.Activity == Traveling && v.Pod.Speed == 0 && v.distance == v.blocks.end(plan.frontier) {
			return s
		}
	}
	t.Fatal("pod never stopped at the buffer frontier")
	return nil
}

func TestStationBufferHoldingAndFailedCommit(t *testing.T) {
	t.Parallel()
	s := stoppedBufferFixture(t)
	v := &s.vehicles[0]
	distance := v.distance
	owners := maps.Clone(s.owners)
	route := v.Route
	for range 2 * TicksPerSecond {
		s.Step()
	}
	if v.destination.ID != "" || v.Pod.Activity != Traveling || s.completed != 0 || s.requestID != 1 || v.distance != distance {
		t.Fatalf("holding changed arrival/accounting: %+v", s.Snapshot())
	}
	if !reflect.DeepEqual(route, v.Route) || !maps.Equal(owners, s.owners) {
		t.Fatal("failed berth commit changed route or ownership")
	}
	if v.Pod.WaitReason != BerthOccupied || v.Pod.BlockedBy != "external" {
		t.Fatalf("missing berth blockage: %+v", v.Pod)
	}
}

func TestStationBufferRestoreContractAndDisabledDrain(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "v2", true: "v3"}[enabled], func(t *testing.T) {
			t.Parallel()
			s := stoppedBufferFixture(t)
			saved := s.ExportState()
			if !enabled {
				saved.Pods[0].StationBuffered = false
			}
			input := RestoreStateInput{Network: s.network, Fleet: s.initial, State: saved, StationBuffers: enabled}
			restored, result, err := RestoreState(input)
			if err != nil {
				t.Fatal(err)
			}
			if !enabled {
				if len(result.Demoted) != 1 {
					t.Fatalf("v2 accepted a final-lane berthless pod: %+v", result)
				}
				return
			}
			if result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
				t.Fatalf("v3 failed faithful restore: %+v", result)
			}
			v := &restored.vehicles[0]
			if restored.stationBuffers || !v.buffered || v.Pod.Position != s.vehicles[0].Pod.Position || v.waitSince != s.vehicles[0].waitSince {
				t.Fatal("restore lost position, wait age, or disabled drain state")
			}
			for range 300 * TicksPerSecond {
				restored.Step()
				checkTraffic(t, restored.Snapshot())
				if restored.completed == 1 {
					break
				}
			}
			if restored.completed != 1 || restored.NeedsBufferState() {
				t.Fatalf("disabled buffer failed to drain: %+v", restored.Snapshot())
			}
		})
	}
}

func TestStationBufferRejectsUnsafeRestore(t *testing.T) {
	t.Parallel()
	s := stoppedBufferFixture(t)
	for _, beyond := range []bool{false, true} {
		state := s.ExportState()
		if beyond {
			state.Pods[0].LaneDistance = s.laneLength(s.vehicles[0].Route[len(s.vehicles[0].Route)-1])
			state.Pods[0].Distance += state.Pods[0].LaneDistance - s.vehicles[0].Pod.LaneDistance
		} else {
			state.Pods[0].DestinationStation = "garden"
		}
		_, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, StationBuffers: true})
		if err == nil && len(result.Demoted) == 0 && result.Tier == RestorePhysical {
			t.Fatalf("unsafe buffer restored physically: beyond=%t result=%+v", beyond, result)
		}
	}
}

func TestStationBufferPreparedRestore(t *testing.T) {
	t.Parallel()
	s := stoppedBufferFixture(t)
	prepared, err := PrepareNetwork(s.network)
	if err != nil {
		t.Fatal(err)
	}
	for _, version3 := range []bool{false, true} {
		state := s.ExportState()
		if !version3 {
			state.Pods[0].StationBuffered = false
		}
		ordinary, want, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, StationBuffers: version3})
		if err != nil {
			t.Fatal(err)
		}
		cached, got, err := prepared.RestoreState(PreparedRestoreInput{Fleet: s.initial, State: state, StationBuffers: version3})
		if err != nil {
			t.Fatal(err)
		}
		if got.PhysicalError != nil || want.PhysicalError != nil {
			t.Fatalf("unexpected physical error: got=%v want=%v", got.PhysicalError, want.PhysicalError)
		}
		if !sameRestoreResult(got, want) || !reflect.DeepEqual(cached.ExportState(), ordinary.ExportState()) {
			t.Fatalf("prepared contract differs: version3=%t got=%+v want=%+v err=%v", version3, got, want, err)
		}
	}
}

func TestStationBufferRestoreDuringEntry(t *testing.T) {
	t.Parallel()
	s := stationBufferFixture(t)
	for range 600 * TicksPerSecond {
		s.Step()
		v := &s.vehicles[0]
		plan, ok := s.bufferPlan(v)
		if !ok || v.Pod.LaneID != plan.lane.ID || v.Pod.LaneDistance >= Clearance {
			continue
		}
		restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), StationBuffers: true})
		if err != nil || len(result.Demoted) != 0 {
			t.Fatalf("entry restore failed: %+v %v", result, err)
		}
		head := &restored.vehicles[0]
		want, ok := restored.bufferPlan(head)
		if !ok || head.reservedThrough < want.entryStop {
			t.Fatal("entry restore lost the tail-clear stopping reservation")
		}
		return
	}
	t.Fatal("did not capture an in-flight entry")
}

func TestStationBufferDiversionClearsMembership(t *testing.T) {
	t.Parallel()
	s := stationBufferFixture(t)
	v := &s.vehicles[0]
	if !s.bufferApproach(v) || !s.NeedsBufferState() {
		t.Fatal("pending admission did not require v3")
	}
	s.SetStationBuffers(false)
	station, _ := s.station("garden")
	route, berth, err := s.stationRoute(v.origin.Node, station.ID)
	if err != nil {
		t.Fatal(err)
	}
	// This checks route-membership cleanup independently of the diversion
	// eligibility guards that forbid diverting passenger-bearing pods.
	s.redirect(v, redirection{route: route, berth: berth, station: station.ID})
	if v.buffered || v.bufferBerth != "" || s.NeedsBufferState() {
		t.Fatal("diversion retained a buffer membership")
	}
}

func TestStationBufferQueueRestoreAndHeadOrder(t *testing.T) {
	t.Parallel()
	for _, scale := range []float64{2, 4} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(stationBufferNetwork(Example(), scale), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			s.SetStationBuffers(true)
			for _, id := range []string{"01", "02"} {
				if requestErr := s.RequestJourney(id, "market"); requestErr != nil {
					t.Fatal(requestErr)
				}
			}
			s.owners[resource{kind: berthResource, id: "market-1"}] = "external"
			stepUntil(t, s, "two-pod stopped queue", func() bool {
				return slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.Pod.LaneID == "market-approach" && v.Pod.Speed == 0 }) &&
					s.vehicles[0].Pod.Activity == Traveling && s.vehicles[1].Pod.Activity == Traveling && s.vehicles[0].Pod.Speed == 0 && s.vehicles[1].Pod.Speed == 0
			})
			head := ""
			for index := range s.vehicles {
				v := &s.vehicles[index]
				if v.Pod.LaneID == "market-approach" {
					plan, ok := s.bufferPlan(v)
					if ok && s.bufferHead(v, plan) {
						head = v.Pod.ID
					}
				}
			}
			if head == "" || s.completed != 0 {
				t.Fatal("queue lacks an uncompleted physical head")
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), StationBuffers: true})
			if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
				t.Fatalf("queue restore: %+v %v", result, err)
			}
			first := ""
			stepUntil(t, restored, "queue drain", func() bool {
				for _, v := range restored.vehicles {
					if v.Pod.StationID == "market" && v.Pod.BerthID != "" && first == "" {
						first = v.Pod.ID
					}
				}
				return restored.completed == 2
			})
			if first != head || restored.NeedsBufferState() {
				t.Fatalf("head=%s first=%s buffer=%t", head, first, restored.NeedsBufferState())
			}
		})
	}
}

func TestStationBufferClearsRealBerth(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	if requestErr := s.RequestJourney("01", "market"); requestErr != nil {
		t.Fatal(requestErr)
	}
	sawBlocked := false
	stepUntil(t, s, "passenger completion after berth clearing", func() bool {
		arrival := s.findVehicle("01")
		sawBlocked = sawBlocked || arrival.buffered && arrival.bufferBerth == "market-1" && arrival.Pod.BlockedBy == "02"
		return s.completed == 1
	})
	if !sawBlocked || s.findVehicle("02").Pod.BerthID == "market-1" {
		t.Fatal("real berth blocker was not observed and cleared")
	}
}

func TestStationBufferWaitsForBlockedDeparture(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	if requestErr := s.RequestJourney("01", "market"); requestErr != nil {
		t.Fatal(requestErr)
	}
	// Hold the departure path, so the real idle berth owner cannot clear.
	barrier := s.laneCells["market-out"].cell(0)[0]
	s.owners[barrier] = "external"
	stepUntil(t, s, "buffer head behind blocked departure", func() bool {
		arrival := s.findVehicle("01")
		plan, ok := s.bufferPlan(arrival)
		return ok && arrival.Pod.Speed == 0 && arrival.distance == arrival.blocks.end(plan.frontier)
	})
	for range 2 * TicksPerSecond {
		s.Step()
		checkTraffic(t, s.Snapshot())
		if _, safetyErr := s.SafetyObservation().Check(); safetyErr != nil {
			t.Fatal(safetyErr)
		}
	}
	if s.completed != 0 || s.findVehicle("02").Pod.BerthID != "market-1" || s.findVehicle("01").destination.ID != "" {
		t.Fatal("arrival crossed the buffer while the berth departure was blocked")
	}
	delete(s.owners, barrier)
	stepUntil(t, s, "buffer drain after departure clears", func() bool { return s.completed == 1 })
}

func TestStationBufferCompetingArrivalAge(t *testing.T) {
	t.Parallel()
	for _, aged := range []bool{false, true} {
		t.Run(map[bool]string{false: "passenger-priority", true: "aged-pickup"}[aged], func(t *testing.T) {
			t.Parallel()
			s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			s.SetStationBuffers(true)
			if requestErr := s.RequestJourney("01", "market"); requestErr != nil {
				t.Fatal(requestErr)
			}
			// Keep a pickup assigned before buffers were enabled.
			s.SetStationBuffers(false)
			if requestErr := s.RequestTrip("market", "harbor"); requestErr != nil {
				t.Fatal(requestErr)
			}
			s.SetStationBuffers(true)
			barrier := resource{kind: nodeResource, id: "merge"}
			s.owners[barrier] = "external"
			stepUntil(t, s, "competing buffer arrivals", func() bool {
				return slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.Pod.ID == "01" && v.Pod.Speed == 0 && v.Pod.BlockedBy == "external" }) &&
					slices.ContainsFunc(s.vehicles, func(v vehicle) bool { return v.Pod.ID == "02" && v.Pod.Speed == 0 && v.Pod.BlockedBy == "external" })
			})
			passenger, pickup := s.findVehicle("01"), s.findVehicle("02")
			if !passenger.buffered || !s.assigned(pickup.Pod.ID) || !passenger.carriesPassengers() || pickup.destination.ID != "market-1" {
				t.Fatal("fixture lacks a buffered passenger and a pickup with a retained berth assignment")
			}
			passenger.waitSince, pickup.waitSince = s.tick, s.tick
			if aged {
				pickup.waitSince -= admissionAgeTicks
			}
			delete(s.owners, barrier)
			s.admit()
			want := "01"
			if aged {
				want = "02"
			}
			if got := s.owners[barrier]; got != want {
				t.Fatalf("merge owner = %q, want %q", got, want)
			}
			// Existing ownership must survive a later priority change.
			passenger.waitSince, pickup.waitSince = 0, 0
			s.admit()
			if got := s.owners[barrier]; got != want {
				t.Fatalf("buffer admission revoked the existing merge owner: %q", got)
			}
		})
	}
}

func TestStationBufferMixedBerthBlockers(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(stationBufferNetwork(twoBerthMarket(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "market", BerthID: "market-1"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	if requestErr := s.RequestJourney("01", "market"); requestErr != nil {
		t.Fatal(requestErr)
	}
	// The free second berth has a blocked inlet. The occupied first berth
	// must still identify its idle blocker for the clearing controller.
	cells := s.laneCells["market-in-2"]
	blocker := cells.cell(cells.count() - 1)[0]
	s.owners[blocker] = "external"
	sawClearing := false
	stepUntil(t, s, "mixed-blocker completion", func() bool {
		sawClearing = sawClearing || s.findVehicle("02").Pod.Activity != Idle
		return s.completed == 1
	})
	if !sawClearing || s.findVehicle("01").Pod.BerthID != "market-1" {
		t.Fatal("failed free-inlet trial hid the idle berth blocker")
	}
}

func TestStationBufferRestoresPendingAdmission(t *testing.T) {
	t.Parallel()
	s := stationBufferFixture(t)
	stepUntil(t, s, "upstream pending membership", func() bool {
		return s.vehicles[0].buffered && s.vehicles[0].Pod.Activity == Traveling && s.vehicles[0].Pod.LaneID != "market-approach"
	})
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), StationBuffers: true})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("pending restore: %+v %v", result, err)
	}
	if !restored.vehicles[0].buffered || restored.stationBuffers || !restored.NeedsBufferState() {
		t.Fatal("lost pending membership with admissions disabled")
	}
	stepUntil(t, restored, "pending admission drain", func() bool { return restored.completed == 1 })
}

func TestStationBufferConflictingRestore(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	for _, id := range []string{"01", "02"} {
		if requestErr := s.RequestJourney(id, "market"); requestErr != nil {
			t.Fatal(requestErr)
		}
	}
	s.owners[resource{kind: berthResource, id: "market-1"}] = "external"
	stepUntil(t, s, "stopped queue", func() bool {
		return s.vehicles[0].Pod.LaneID == "market-approach" && s.vehicles[1].Pod.LaneID == "market-approach" && s.vehicles[0].Pod.Speed == 0 && s.vehicles[1].Pod.Speed == 0
	})
	state := s.ExportState()
	state.Pods[1].LaneDistance = state.Pods[0].LaneDistance
	state.Pods[1].Distance = state.Pods[0].Distance
	_, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, StationBuffers: true})
	if err == nil && result.Tier == RestorePhysical && len(result.Demoted) == 0 {
		t.Fatal("overlapping queue restored without loss report")
	}
}

func TestStationBufferFullQueueAndUpstreamPlatoon(t *testing.T) {
	t.Parallel()
	network := geometryNetwork(Point{}, []geometryLane{{to: Point{X: 3000}}, {to: Point{X: 3120}}}, 8)
	for index := range network.Lanes {
		if network.Lanes[index].ID == "l1" {
			network.Lanes[index].StationID = "dest"
			network.Lanes[index].StationRole = StationEntryRole
		}
	}
	// A return path lets idle destination pods clear the single berth.
	network.Lanes = append(network.Lanes, Lane{ID: "return", From: "dest-exit", To: "origin-entry", SpeedLimit: 14})
	s := restoreGeometry(t, network, 2, 8, 2900, 60)
	s.SetStationBuffers(true)
	if err := s.SetPlatooning(PlatooningVirtual); err != nil {
		t.Fatal(err)
	}
	s.owners[resource{kind: berthResource, id: "dest-1"}] = "external"
	monitor := newPlatoonMonitor(s)
	for range 60 * TicksPerSecond {
		s.Step()
		// The synthetic berth barrier is outside the modeled fleet.
		delete(s.owners, resource{kind: berthResource, id: "dest-1"})
		monitor.check(t)
		s.owners[resource{kind: berthResource, id: "dest-1"}] = "external"
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
	}
	geometry := s.StationBufferGeometry()
	inside, upstream := 0, 0
	for index := range s.vehicles {
		v := &s.vehicles[index]
		if v.destination.ID != "" {
			t.Fatal("full queue chose a berth before head admission")
		}
		if v.Pod.LaneID == "l1" {
			inside++
			if v.coupled() {
				t.Fatal("entry pod remained coupled")
			}
			plan, ok := s.bufferPlan(v)
			if !ok || v.reservedThrough > plan.frontier || v.distance > v.blocks.end(plan.frontier) {
				t.Fatal("pod crossed holding frontier")
			}
		} else {
			upstream++
		}
	}
	if len(geometry) != 1 || inside != geometry[0].StoppingCells || upstream == 0 || len(monitor.coupled) == 0 || s.completed != 0 {
		t.Fatalf("full queue not exercised: inside%d upstream%d coupled%d geometry%+v", inside, upstream, len(monitor.coupled), geometry)
	}
	delete(s.owners, resource{kind: berthResource, id: "dest-1"})
	s.SetStationBuffers(false)
	for range 900 * TicksPerSecond {
		s.Step()
		monitor.check(t)
		if _, err := s.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
		if s.completed == 8 {
			break
		}
	}
	if s.completed != 8 || s.NeedsBufferState() {
		t.Fatalf("full queue failed to drain: completed%d", s.completed)
	}
}

func TestStationBufferDisabledRestoreKeepsOrdinaryTrip(t *testing.T) {
	t.Parallel()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	if requestErr := s.RequestJourney("01", "market"); requestErr != nil {
		t.Fatal(requestErr)
	}
	stepUntil(t, s, "pending membership", func() bool { return s.findVehicle("01").buffered })
	s.SetStationBuffers(false)
	if requestErr := s.RequestJourney("02", "market"); requestErr != nil {
		t.Fatal(requestErr)
	}
	if s.findVehicle("02").buffered {
		t.Fatal("ordinary trip entered buffer before save")
	}
	restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), StationBuffers: true})
	if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
		t.Fatalf("mixed restore: %+v %v", result, err)
	}
	if !restored.findVehicle("01").buffered || restored.findVehicle("02").buffered {
		t.Fatal("restore changed existing and ordinary membership")
	}
}

func TestStationBufferReleasedPickupDrain(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"released", "orphan", "overlong"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			s := stoppedBufferFixture(t)
			v := &s.vehicles[0]
			v.Pod.Occupied = false
			v.Riders, v.Stops = nil, nil
			v.RelocatingTo = "market"
			s.boarded, s.requestID = 0, 1
			trip := waitingTrip{request: Request{SharingConsent: SharedConsent, Service: OnDemandService, ID: 1, From: "market", To: "garden", PodID: "01", PartySize: 1}}
			s.waiting = []waitingTrip{trip}
			switch mode {
			case "released":
				s.waiting[0].request.PodID = ""
				if !s.releasePickup(v) {
					t.Fatal("pickup not released")
				}
			case "orphan":
				s.waiting[0].request.To = "unknown"
			case "overlong":
				s.waiting[0].route = make([]Lane, newRouteLimits(s.network).trip+1)
				for index := range s.waiting[0].route {
					s.waiting[0].route[index] = s.network.Lanes[0]
				}
			}
			state := s.ExportState()
			if _, err := state.checkContract(); err != nil {
				t.Fatal(err)
			}
			restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: state, StationBuffers: true})
			if err != nil || result.Tier != RestorePhysical || len(result.Demoted) != 0 {
				t.Fatalf("released restore: %+v %v", result, err)
			}
			head := restored.findVehicle("01")
			if !head.buffered || !head.released {
				t.Fatal("released member lost drain state")
			}
			// Stop new work so this case checks discharge of the released member.
			restored.waiting = nil
			stepUntil(t, restored, "released buffer drain", func() bool { return !restored.NeedsBufferState() })
		})
	}
}

// bufferedHeadWithPickup has a passenger at the holding frontier and an
// assigned pickup behind it. The pickup retains the only berth destination.
func bufferedHeadWithPickup(t *testing.T) *Simulation {
	t.Helper()
	s, err := NewFleet(stationBufferNetwork(Example(), 4), []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	if err := s.RequestJourney("01", "market"); err != nil {
		t.Fatal(err)
	}
	barrier := resource{kind: berthResource, id: "market-1"}
	s.owners[barrier] = "external"
	stepUntil(t, s, "head at holding frontier", func() bool {
		v := s.findVehicle("01")
		plan, ok := s.bufferPlan(v)
		return ok && v.Pod.Speed == 0 && v.distance == v.blocks.end(plan.frontier)
	})
	// Preserve a pickup with an existing berth assignment behind the head.
	s.SetStationBuffers(false)
	if err := s.RequestTrip("market", "harbor"); err != nil {
		t.Fatal(err)
	}
	s.SetStationBuffers(true)
	stepUntil(t, s, "assigned pickup behind buffer head", func() bool {
		v := s.findVehicle("02")
		return v.Pod.Speed == 0 && v.Pod.BlockedBy == "01"
	})
	delete(s.owners, barrier)
	if s.findVehicle("02").destination.ID != "market-1" || s.findVehicle("01").destination.ID != "" {
		t.Fatal("fixture lost the head or following berth assignment")
	}
	return s
}

func checkBufferOrders(t *testing.T, s *Simulation) {
	t.Helper()
	state := s.Snapshot()
	checkTraffic(t, state)
	if _, err := s.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	ids := make(map[int]bool)
	for _, request := range state.Pending {
		if ids[request.ID] {
			t.Fatal("duplicate pending request")
		}
		ids[request.ID] = true
	}
	for _, v := range state.Vehicles {
		for _, rider := range v.Riders {
			if rider.Completed {
				continue
			}
			if ids[rider.ID] {
				t.Fatal("duplicate active request")
			}
			ids[rider.ID] = true
		}
	}
	if len(ids)+state.Completed != state.Submitted || state.Submitted != 2 {
		t.Fatalf("buffer lost order accounting: %+v", state)
	}
}

func TestStationBufferHeadBeforeAssignedPickup(t *testing.T) {
	t.Parallel()
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "uninterrupted", true: "restored"}[restore], func(t *testing.T) {
			t.Parallel()
			s := bufferedHeadWithPickup(t)
			committed := false
			for range 600 * TicksPerSecond {
				s.Step()
				checkBufferOrders(t, s)
				if !committed && s.findVehicle("01").destination.ID == "market-1" {
					committed = true
					if s.findVehicle("02").destination.ID != "market-1" || !s.assigned("02") {
						t.Fatal("head admission revoked the following pickup assignment")
					}
					if s.owners[resource{kind: berthResource, id: "market-1"}] != "01" {
						t.Fatal("head destination committed without berth ownership")
					}
					if restore {
						restored, result, err := RestoreState(RestoreStateInput{Network: s.network, Fleet: s.initial, State: s.ExportState(), StationBuffers: true})
						if err != nil || result.Tier != RestorePhysical || len(result.Demoted)+len(result.Requeued)+len(result.Dropped) != 0 {
							t.Fatalf("head/pickup restore: %+v %v", result, err)
						}
						if restored.findVehicle("02").destination.ID != "market-1" || !restored.assigned("02") {
							t.Fatal("restore lost the following pickup destination or order")
						}
						s = restored
						checkBufferOrders(t, s)
					}
				}
				if s.completed == 2 {
					break
				}
			}
			if !committed || s.completed != 2 || s.PendingCount() != 0 {
				t.Fatalf("head and assigned pickup did not complete: committed=%t state=%+v", committed, s.Snapshot())
			}
		})
	}
}

func TestStationBufferHeadHonorsOwnedPath(t *testing.T) {
	t.Parallel()
	for _, blocked := range []resource{
		{kind: berthResource, id: "market-1"},
		{kind: nodeResource, id: "market-berth"},
		{kind: trackResource, id: "market-in", cell: 0},
	} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			t.Parallel()
			s := bufferedHeadWithPickup(t)
			s.owners[blocked] = "02"
			head := s.findVehicle("01")
			before := s.ExportState().Pods[0]
			distance := head.distance
			owners := maps.Clone(s.owners)
			for range 2 {
				s.grant(intent{index: 0, block: head.pending, since: head.waitSince})
			}
			after := s.ExportState().Pods[0]
			if head.destination.ID != "" || head.distance != distance || !reflect.DeepEqual(before.Route, after.Route) || !maps.Equal(owners, s.owners) {
				t.Fatal("blocked berth-path trial changed the head route or existing ownership")
			}
			if s.findVehicle("02").destination.ID != "market-1" {
				t.Fatal("blocked trial revoked the pickup assignment")
			}
			delete(s.owners, blocked)
			stepUntil(t, s, "head progress after resource release", func() bool { return s.findVehicle("01").destination.ID == "market-1" })
		})
	}
}

func TestStationBufferCompetingBerthAdmission(t *testing.T) {
	t.Parallel()
	for _, aged := range []bool{false, true} {
		t.Run(map[bool]string{false: "passenger-priority", true: "aged-pickup"}[aged], func(t *testing.T) {
			t.Parallel()
			network := stationBufferNetwork(Example(), 4)
			for index := range network.Lanes {
				if network.Lanes[index].ID == "garden-merge" {
					network.Lanes[index].To = "market-entry"
					network.Lanes[index].StationID = "market"
					network.Lanes[index].StationRole = StationEntryRole
				}
			}
			s, err := NewFleet(network, []Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
			if err != nil {
				t.Fatal(err)
			}
			s.SetStationBuffers(true)
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			// This case retains a pickup's pre-buffer berth assignment.
			s.SetStationBuffers(false)
			if err := s.RequestTrip("market", "harbor"); err != nil {
				t.Fatal(err)
			}
			s.SetStationBuffers(true)
			barrier := resource{kind: nodeResource, id: "market-entry"}
			berth := resource{kind: berthResource, id: "market-1"}
			s.owners[barrier] = "external"
			stepUntil(t, s, "two independent station approaches", func() bool {
				head, pickup := s.findVehicle("01"), s.findVehicle("02")
				return head.Pod.Speed == 0 && pickup.Pod.Speed == 0 && head.Pod.BlockedBy == "external" && pickup.Pod.BlockedBy == "external"
			})
			head, pickup := s.findVehicle("01"), s.findVehicle("02")
			if head.Pod.LaneID == pickup.Pod.LaneID || !head.buffered || pickup.destination.ID != berth.id {
				t.Fatal("fixture lacks independent approaches and retained pickup destination")
			}
			head.waitSince, pickup.waitSince = s.tick, s.tick
			if aged {
				pickup.waitSince -= admissionAgeTicks
			}
			delete(s.owners, barrier)
			s.admit()
			want := "01"
			if aged {
				want = "02"
			}
			if s.owners[barrier] != want {
				t.Fatalf("entry owner=%q, want %q", s.owners[barrier], want)
			}
			firstBerthOwner := s.owners[berth]
			if pickup.destination.ID != berth.id {
				t.Fatal("competition revoked pickup destination")
			}
			head.waitSince, pickup.waitSince = 0, 0
			s.admit()
			if s.owners[barrier] != want {
				t.Fatal("later priority change revoked committed entry")
			}
			if firstBerthOwner != "" && s.owners[berth] != firstBerthOwner {
				t.Fatal("later priority change revoked committed berth")
			}
			for range 600 * TicksPerSecond {
				s.Step()
				checkBufferOrders(t, s)
				if firstBerthOwner == "" {
					firstBerthOwner = s.owners[berth]
				}
				if s.completed == 2 {
					break
				}
			}
			if s.completed != 2 || firstBerthOwner != want {
				t.Fatalf("competing arrivals: completed=%d first berth owner=%q, want %q", s.completed, firstBerthOwner, want)
			}
		})
	}
}
