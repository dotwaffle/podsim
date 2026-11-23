package session

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

const londonWaterloo, londonBank = "940GZZLUWLO", "940GZZLUBNK"

// londonExpressFixture is a session of the London Express preset with
// background demand on, and a client that orders hub parties.
type londonExpressFixture struct {
	t        *testing.T
	config   project.Config
	shared   *Session
	client   *testClient
	services map[string]string
	steps    restoreSteps
	// restored, when it is set, runs on each restore of checkpoint, with
	// the name of the checkpoint, the live simulation, and the restored
	// simulation. tick, when it is set, runs at each tick of the restored
	// simulation after the safety oracle.
	restored func(name string, live, restored *sim.Simulation)
	tick     func(restored *sim.Simulation) error
	// second, when it is set, runs at each second of driveShared with the
	// vehicles of the session.
	second func(tick int, vehicles []sim.Vehicle)
}

func newLondonExpressFixture(t *testing.T, options ...Option) *londonExpressFixture {
	t.Helper()
	config := scenarios.LondonFullExpress()
	config.Demand.Enabled = true
	shared, err := NewWithProject(config, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	f := &londonExpressFixture{t: t, config: config, shared: shared, client: newTestClient(shared, "london-express"), services: map[string]string{}, steps: realRestoreSteps()}
	for _, service := range config.ExpressServices {
		f.services[service.From] = service.ID
	}
	// The restored copy of each checkpoint runs the D6 oracle at each tick.
	f.tick = func(restored *sim.Simulation) error { return restored.LateralBodyCheckForTest() }
	return f
}

// trip orders a shared hub party of size from the hub from to the hub to.
// It returns the refusal text, or "".
func (f *londonExpressFixture) trip(from, to string, size int) string {
	return f.shared.Apply(f.client.next(Command{Action: "trip", Origin: from, Destination: to, PartySize: size,
		SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: f.services[from], OrderContract: sim.ExpressOrderContract})).Error
}

// checkpoint saves the session through the encoded path and restores the
// save. The restore must be physical with no change to the pods, except a
// distance within restoreTolerance, and with no fallback to the restore
// before the saved motion. The restored copy must pass the safety oracle
// at each tick for 10 s.
func (f *londonExpressFixture) checkpoint(name string) {
	t := f.t
	t.Helper()
	file := sessionStateFile(t, f.shared)
	file.RestoreAttempts = 0
	loaded, err := f.shared.loadState(loadInput{data: encodeTestState(t, file), project: &f.config, steps: f.steps})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	r := loaded.result
	if r.Tier != sim.RestorePhysical || len(r.Demoted)+len(r.Requeued)+len(r.Dropped)+len(r.Interrupted)+len(r.LogicalCompleted)+r.DroppedFaults+r.DroppedEmergencies+r.Unaccounted+len(r.AtRest) != 0 ||
		r.EarlierRestore {
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
	if f.restored != nil {
		f.restored(name, f.shared.simulation, loaded.simulation)
	}
	for range 10 * sim.TicksPerSecond {
		loaded.simulation.Step()
		if _, err := loaded.simulation.SafetyObservation().Check(); err != nil {
			t.Fatalf("%s: restored copy: %v", name, err)
		}
		if f.tick != nil {
			if err := f.tick(loaded.simulation); err != nil {
				t.Fatalf("%s: restored copy: %v", name, err)
			}
		}
	}
}

// TestLondonExpressPhysicalCheckpoints saves the London Express preset
// through the encoded session path at the states that the risk-check probe of
// the qualification proposal did not reach: an Express pod at a berth of the
// second bank at Bank, and an Express pod on its way back to Express parking.
// It also saves at the first second with a traveling pod on a join turn at
// a lane end (see JoinTurnPodsForTest). Each save must restore at the
// physical tier with no change to the pods, and the restored copy must pass
// the safety oracle and the D6 oracle for 10 s. The test logs the kept
// speeds of each checkpoint.
func TestLondonExpressPhysicalCheckpoints(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the London Express checkpoints run without -short or -race")
	}
	f := newLondonExpressFixture(t)
	f.restored = keptSpeeds(t)
	joined := false
	f.drive(func(_ int, state string) { f.checkpoint(state) }, func(tick int) bool {
		if !joined && (tick+1)%sim.TicksPerSecond == 0 {
			if pods := f.shared.simulation.JoinTurnPodsForTest(); len(pods) > 0 {
				joined = true
				t.Logf("tick %d: join turn: %v", tick, pods)
				f.checkpoint("join turn")
			}
		}
		return !joined
	})
	if !joined {
		t.Error("no pod reached a join turn at a lane end")
	}
}

// londonExpressStates are the states of the script of drive, in order.
var londonExpressStates = []string{"bank-b", "return", "parking-berth"}

// drive runs the script of the London Express checkpoints, and calls
// checkpoint at each state. advanced, if not nil, runs after each tick.
// While it returns true, the script continues after the last state, and it
// runs at least once after each checkpoint.
//
// A pod chooses the bank with the lower free-flow approach cost, and from
// Waterloo that is always the first bank at Bank. So debris on the Express
// approach to the first bank sends pods to the second bank.
func (f *londonExpressFixture) drive(checkpoint func(tick int, state string), advanced func(tick int) bool) {
	t := f.t
	t.Helper()
	const parking = "express-parking"
	station, ok := f.config.Network.Station(londonBank)
	if !ok || len(station.Banks) != 2 {
		t.Fatal("Bank has no second bank")
	}
	bankB := station.Banks[1].BerthIDs
	shared, client, trip := f.shared, f.client, f.trip
	waterloo, bank := londonWaterloo, londonBank
	// Hub parties run in both directions until an Express pod uses a berth
	// of the second bank at Bank, for at most 30 minutes. The debris goes on
	// at minute 2 and clears at that berth. Then the orders stop, and the
	// test waits for a pod to go back to Express parking.
	debris := ""
	seen := map[string]bool{}
	busy := false
	for tick := 0; tick < 60*60*sim.TicksPerSecond && (len(seen) < len(londonExpressStates) || busy); tick++ {
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
		if advanced != nil {
			busy = advanced(tick)
		}
		if tick%sim.TicksPerSecond != 0 || len(seen) == len(londonExpressStates) {
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
		checkpoint(tick, state)
		// advanced decides on the next tick whether the script continues.
		busy = advanced != nil
		if state == "bank-b" && debris != "" {
			client.mustApply(t, Command{Action: "clearFault", FaultID: debris})
		}
	}
	for _, state := range londonExpressStates {
		if !seen[state] {
			t.Errorf("no Express pod reached state %s", state)
		}
	}
}
