package session

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonExpressPhysicalCheckpoints saves the London Express preset
// through the encoded session path at the states that the risk-check probe of
// the qualification proposal did not reach: an Express pod at a berth of the
// second bank at Bank, and an Express pod on its way back to Express parking.
// Each save must restore at the physical tier with no change to the pods,
// and the restored copy must pass the safety oracle for 10 s.
//
// A pod chooses the bank with the lower free-flow approach cost, and from
// Waterloo that is always the first bank at Bank. So debris on the Express
// approach to the first bank sends pods to the second bank.
func TestLondonExpressPhysicalCheckpoints(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the London Express checkpoints run without -short or -race")
	}
	const waterloo, bank, parking = "940GZZLUWLO", "940GZZLUBNK", "express-parking"
	config := scenarios.LondonFullExpress()
	config.Demand.Enabled = true
	services := map[string]string{}
	for _, service := range config.ExpressServices {
		services[service.From] = service.ID
	}
	station, ok := config.Network.Station(bank)
	if !ok || len(station.Banks) != 2 {
		t.Fatal("Bank has no second bank")
	}
	bankB := station.Banks[1].BerthIDs
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	client := newTestClient(shared, "london-express")
	trip := func(from, to string, size int) {
		shared.Apply(client.next(Command{Action: "trip", Origin: from, Destination: to, PartySize: size,
			SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: services[from], OrderContract: sim.ExpressOrderContract}))
	}
	steps := realRestoreSteps()
	checkpoint := func(name string) {
		t.Helper()
		file := sessionStateFile(t, shared)
		file.RestoreAttempts = 0
		loaded, err := shared.loadState(loadInput{data: encodeTestState(t, file), project: &config, steps: steps})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r := loaded.result
		if r.Tier != sim.RestorePhysical || len(r.Demoted)+len(r.Requeued)+len(r.Dropped)+len(r.Interrupted)+len(r.LogicalCompleted)+r.DroppedFaults+r.DroppedEmergencies+r.Unaccounted != 0 {
			t.Fatalf("%s: restore %+v", name, r)
		}
		// A restored pod can move to a block end of its rebuilt route, by at
		// most restoreTolerance.
		pods := loaded.simulation.ExportState().Pods
		for i := range min(len(pods), len(file.Simulation.Pods)) {
			if math.Abs(pods[i].Distance-file.Simulation.Pods[i].Distance) <= 1e-6 {
				pods[i].Distance = file.Simulation.Pods[i].Distance
			}
		}
		if !reflect.DeepEqual(pods, file.Simulation.Pods) {
			t.Fatalf("%s: restore changed the pods", name)
		}
		for range 10 * sim.TicksPerSecond {
			loaded.simulation.Step()
			if _, err := loaded.simulation.SafetyObservation().Check(); err != nil {
				t.Fatalf("%s: restored copy: %v", name, err)
			}
		}
	}
	// Hub parties run in both directions until an Express pod uses a berth
	// of the second bank at Bank, for at most 30 minutes. The debris goes on
	// at minute 2 and clears at that berth. Then the orders stop, and the
	// test waits for a pod to go back to Express parking.
	debris := ""
	seen := map[string]bool{}
	for tick := 0; tick < 60*60*sim.TicksPerSecond && len(seen) < 3; tick++ {
		ordering := !seen["bank-b"] && tick < 30*60*sim.TicksPerSecond
		if ordering && tick%(20*sim.TicksPerSecond) == 0 {
			trip(waterloo, bank, 13+tick/(20*sim.TicksPerSecond)%8)
		}
		if ordering && tick%(90*sim.TicksPerSecond) == 45*sim.TicksPerSecond {
			trip(bank, waterloo, 12+tick/(90*sim.TicksPerSecond)%9)
		}
		if tick == 2*60*sim.TicksPerSecond {
			reply := client.mustApply(t, Command{Action: "fault", LaneID: bank + "-entry-xp-stub-2", FromMeters: new(10.0), ToMeters: new(14.0)})
			debris = reply.FaultID
		}
		shared.advance()
		if tick%sim.TicksPerSecond != 0 {
			continue
		}
		state := ""
		for _, v := range shared.simulation.Snapshot().Vehicles {
			switch {
			case v.Pod.Class != sim.ExpressClass:
				continue
			case !seen["bank-b"] && v.Pod.StationID == bank && slices.Contains(bankB, v.Pod.BerthID):
				state = "bank-b"
			case seen["bank-b"] && !seen["return"] && v.Pod.Activity == sim.Traveling && v.RelocatingTo == parking:
				state = "return"
			case seen["return"] && !seen["parking-berth"] && v.Pod.ManeuverStationID == parking:
				state = "parking-berth"
			default:
				continue
			}
			t.Logf("tick %d: %s: %s %s %s", tick, state, v.Pod.ID, v.Pod.Activity, v.Pod.BerthID)
			break
		}
		if state == "" {
			continue
		}
		seen[state] = true
		checkpoint(state)
		if state == "bank-b" && debris != "" {
			client.mustApply(t, Command{Action: "clearFault", FaultID: debris})
		}
	}
	for _, state := range []string{"bank-b", "return", "parking-berth"} {
		if !seen[state] {
			t.Errorf("no Express pod reached state %s", state)
		}
	}
}
