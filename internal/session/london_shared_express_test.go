package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The tests of this file give the evidence of rows SX4, SX5, SX8, SX9, and
// SX13 of the Express shared-track qualification on the preset
// london-full-shared-express. They use the fixture of the London Express
// tests with the preset in place of project D.

// sharedExpressLink holds the lanes of link 100 on the shared route.
var sharedExpressLink = []string{"london-link-100-ab-1", "london-link-100-ab-2", "london-link-100-ba-1", "london-link-100-ba-2"}

// newSharedExpressFixture returns a session of the shared-track preset
// with background demand on, and a client that orders hub parties.
func newSharedExpressFixture(t *testing.T, options ...Option) *londonExpressFixture {
	t.Helper()
	config, err := scenarios.GenerateLondonFullSharedExpress()
	if err != nil {
		t.Fatal(err)
	}
	config.Demand.Enabled = true
	shared, err := NewWithProject(config, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	return sharedExpressFixtureFor(t, config, shared)
}

// sharedExpressFixtureFor returns the fixture of newSharedExpressFixture
// for a session of the preset config.
func sharedExpressFixtureFor(t *testing.T, config project.Config, shared *Session) *londonExpressFixture {
	t.Helper()
	f := &londonExpressFixture{t: t, config: config, shared: shared, client: newTestClient(shared, "london-shared-express"), services: map[string]string{}, steps: realRestoreSteps()}
	for _, service := range config.ExpressServices {
		f.services[service.From] = service.ID
	}
	// Each checkpoint logs its kept speeds, and the restored copy runs the
	// D6 oracle at each tick.
	f.restored = keptSpeeds(t)
	f.tick = func(restored *sim.Simulation) error { return restored.LateralBodyCheckForTest() }
	return f
}

// keptSpeeds returns a restored hook of londonExpressFixture. Each restore
// of a checkpoint must keep the speed of each pod that is not faulted, and
// the hook logs the number of kept speeds.
func keptSpeeds(t *testing.T) func(name string, live, restored *sim.Simulation) {
	t.Helper()
	return func(name string, live, restored *sim.Simulation) {
		t.Helper()
		snapshot := live.Snapshot()
		faulted := make(map[string]bool)
		for _, fault := range snapshot.Faults.Active {
			faulted[fault.PodID] = fault.PodID != ""
		}
		before, after := snapshot.Vehicles, restored.Snapshot().Vehicles
		kept := 0
		for i := range after {
			want := before[i].Pod.Speed
			if faulted[before[i].Pod.ID] {
				want = 0
			}
			if after[i].Pod.Speed != want {
				t.Fatalf("%s: pod %s restored at %g m/s, want %g m/s", name, after[i].Pod.ID, after[i].Pod.Speed, want)
			}
			if want > 0 {
				kept++
			}
		}
		t.Logf("%s: %d kept speeds", name, kept)
	}
}

// plainTrip orders a Compact trip between the hubs, in the direction of
// the tick, so that Compact pods share link 100 with Express pods.
func (f *londonExpressFixture) plainTrip(tick int) {
	from, to := londonWaterloo, londonBank
	if tick%(20*sim.TicksPerSecond) >= 10*sim.TicksPerSecond {
		from, to = to, from
	}
	if refusal := f.shared.Apply(f.client.next(Command{Action: "trip", Origin: from, Destination: to, PartySize: 1, OrderContract: sim.ExpressOrderContract})).Error; refusal != "" {
		f.t.Fatalf("tick %d: trip from %s: %s", tick, from, refusal)
	}
}

// onSharedLink reports whether the session has an Express pod and a
// Compact pod on link 100 at the same time.
func onSharedLink(vehicles []sim.Vehicle) bool {
	express, compact := false, false
	for _, v := range vehicles {
		if slices.Contains(sharedExpressLink, v.Pod.LaneID) {
			express = express || v.Pod.Class == sim.ExpressClass
			compact = compact || v.Pod.Class != sim.ExpressClass
		}
	}
	return express && compact
}

// sharedExpressStates are the states of the script of driveShared, in
// order.
var sharedExpressStates = []string{"shared-link", "fault", "fault-rest", "emergency", "emergency-unload", "bank-berth", "return", "parking-berth"}

// driveShared runs the script of the shared-track checkpoints, and calls
// checkpoint at the first sight of each state. Hub parties run in both
// directions, and Compact trips between the hubs each 10 s, for 15
// minutes. A fault stops an Express pod with riders at speed on link 100.
// When the fault ends, an emergency starts on another Express pod with
// riders on link 100, and runs to its emergency unload. The other states
// are an Express pod at a Bank berth, an Express pod on its way back to
// Express parking from outside the comb, and then that pod in a parking
// maneuver.
func (f *londonExpressFixture) driveShared(checkpoint func(tick int, state string)) {
	t := f.t
	t.Helper()
	const parking = "express-parking"
	shared, client, trip := f.shared, f.client, f.trip
	seen := map[string]bool{}
	faulted, target, returning := "", "", ""
	faultEnd := 0
	for tick := 0; tick < 60*60*sim.TicksPerSecond && len(seen) < len(sharedExpressStates); tick++ {
		if tick < 15*60*sim.TicksPerSecond {
			if tick%(20*sim.TicksPerSecond) == 0 {
				trip(londonWaterloo, londonBank, 13+tick/(20*sim.TicksPerSecond)%8)
			}
			if tick%(90*sim.TicksPerSecond) == 45*sim.TicksPerSecond {
				trip(londonBank, londonWaterloo, 12+tick/(90*sim.TicksPerSecond)%9)
			}
			if tick%(10*sim.TicksPerSecond) == 5*sim.TicksPerSecond {
				f.plainTrip(tick)
			}
		}
		shared.advance()
		if tick%sim.TicksPerSecond != 0 {
			continue
		}
		vehicles := shared.simulation.Snapshot().Vehicles
		if f.second != nil {
			f.second(tick, vehicles)
		}
		state := ""
		for _, v := range vehicles {
			aboard := v.PassengersAboard()
			link := slices.Contains(sharedExpressLink, v.Pod.LaneID)
			switch {
			case v.Pod.Class != sim.ExpressClass:
				continue
			case !seen["shared-link"] && link && aboard > 0 && onSharedLink(vehicles):
				state = "shared-link"
			case seen["shared-link"] && !seen["fault"] && link && aboard > 12 && v.Pod.Speed >= 10:
				client.mustApply(t, Command{Action: "fault", PodID: v.Pod.ID, DurationSeconds: new(int64(60))})
				faulted, faultEnd = v.Pod.ID, tick+60*sim.TicksPerSecond
				state = "fault"
			case seen["fault"] && !seen["fault-rest"] && v.Pod.ID == faulted && v.Pod.Speed == 0:
				state = "fault-rest"
			case seen["fault-rest"] && !seen["emergency"] && tick > faultEnd && v.Pod.ID != faulted && link && aboard > 0 && v.Pod.Speed > 0:
				client.mustApply(t, Command{Action: "emergency", PodID: v.Pod.ID})
				target = v.Pod.ID
				state = "emergency"
			case seen["emergency"] && !seen["emergency-unload"] && v.Pod.ID == target && v.Pod.Activity == sim.Unloading && v.Operational == sim.OperationalEmergencyUnload:
				state = "emergency-unload"
			case !seen["bank-berth"] && v.Pod.ID != target && v.Pod.StationID == londonBank && v.Pod.BerthID != "":
				state = "bank-berth"
			case !seen["return"] && v.Pod.Activity == sim.Traveling && v.RelocatingTo == parking && !strings.HasPrefix(v.Pod.LaneID, "xp-") && !strings.HasPrefix(v.Pod.LaneID, parking):
				// A pod in the comb can move to another berth of the comb.
				state, returning = "return", v.Pod.ID
			case seen["return"] && !seen["parking-berth"] && v.Pod.ID == returning && v.Pod.ManeuverStationID == parking:
				state = "parking-berth"
			default:
				continue
			}
			t.Logf("tick %d: %s: %s %s %s %s, %d aboard", tick, state, v.Pod.ID, v.Pod.Activity, v.Pod.LaneID, v.Pod.BerthID, aboard)
			break
		}
		if state == "" {
			continue
		}
		seen[state] = true
		checkpoint(tick, state)
	}
	for _, state := range sharedExpressStates {
		if !seen[state] {
			t.Errorf("no Express pod reached state %s", state)
		}
	}
}

// TestLondonSharedExpressPhysicalCheckpoints is the checkpoint test of row
// SX4: as TestLondonExpressPhysicalCheckpoints does for D, it saves the
// shared-track preset through the encoded session path at each state of
// driveShared. The states include an Express pod with riders on link 100
// with a Compact pod, a fault on an Express pod at speed on link 100 and
// at rest, an emergency journey and its unload, a Bank berth, a return to
// Express parking, and a parking maneuver. Each save must restore at the
// physical tier with no change to the pods, and the restored copy must pass
// the safety oracle for 10 s. TestSharedExpressEnvelopeRun in package sim
// runs the D6 oracle in restored continuation.
func TestLondonSharedExpressPhysicalCheckpoints(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express checkpoints run without -short or -race")
	}
	f := newSharedExpressFixture(t)
	full, delta := sharedExpressActivities(f)
	f.driveShared(func(_ int, state string) { f.checkpoint(state) })
	var missing []string
	for _, activity := range []string{"Ready", "Departing empty", "Boarding", "Traveling, moving", "Traveling, braking", "Traveling, stopped", "Unloading"} {
		if !full[activity] {
			missing = append(missing, activity+" (full frame)")
		}
		if !delta[activity] {
			missing = append(missing, activity+" (delta)")
		}
	}
	if len(missing) > 0 {
		t.Fatalf("no Express pod restored and streamed at %v", missing)
	}
}

// sharedExpressActivities sets the second hook of f. At the first sight of
// each activity of an Express pod, the hook saves and restores the session
// (see checkpoint), and it encodes and decodes the full frame, and the
// delta from the frame of the last activity, or from the frame of the
// session before its first tick for the first activity. It requires the
// pod with its activity in the frame that each of the two gives. A
// traveling pod is moving, braking (slower than one second before), or
// stopped. The activities with a full frame check and with a delta check
// are in the two returned maps. An Express pod has no Continuing
// activity on the preset, because only Waterloo and Bank are passenger
// stations with Express berths.
func sharedExpressActivities(f *londonExpressFixture) (full, delta map[string]bool) {
	t := f.t
	seen := map[string]bool{}
	full, delta = map[string]bool{}, map[string]bool{}
	speeds := map[string]float64{}
	assembler, err := NewStreamAssembler(f.shared.Topology())
	if err != nil {
		t.Fatal(err)
	}
	// previous is the frame of the last activity, as the stream gives it.
	// The first is the frame of the session before its first tick.
	var previous *StreamFrame
	// stream encodes e, decodes it, and applies it to base, the frame of
	// sequence 1 of the stream.
	stream := func(base StreamFrame, e StreamEnvelope) StreamFrame {
		t.Helper()
		data, encodeErr := EncodeStreamJSON(e)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		decoded, decodeErr := DecodeStreamJSON(data)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		frame, applyErr := ApplyStream(base, e.Stream, 1, decoded)
		if applyErr != nil {
			t.Fatal(applyErr)
		}
		return frame
	}
	baseline, err := f.shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	first := stream(StreamFrame{}, StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "activities", Sequence: 1, Source: sourceOf(baseline), Build: baseline.State.Build, Full: &baseline})
	previous = &first
	f.second = func(tick int, vehicles []sim.Vehicle) {
		for _, v := range vehicles {
			if v.Pod.Class != sim.ExpressClass {
				continue
			}
			key := string(v.Pod.Activity)
			if v.Pod.Activity == sim.Traveling {
				switch speed, ok := speeds[v.Pod.ID]; {
				case v.Pod.Speed == 0:
					key += ", stopped"
				case ok && v.Pod.Speed < speed:
					key += ", braking"
				default:
					key += ", moving"
				}
			}
			speeds[v.Pod.ID] = v.Pod.Speed
			if seen[key] {
				continue
			}
			seen[key] = true
			t.Logf("tick %d: activity %s: %s on %s%s", tick, key, v.Pod.ID, v.Pod.LaneID, v.Pod.BerthID)
			f.checkpoint("activity " + key)
			frame, err := f.shared.presentationFrame()
			if err != nil {
				t.Fatal(err)
			}
			fullFrame := stream(StreamFrame{}, StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "activities", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame})
			d, err := makeDelta(*previous, frame)
			if err != nil {
				t.Fatal(err)
			}
			deltaFrame := stream(*previous, StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: "activities", Sequence: 2, Base: 1, Source: sourceOf(frame), Build: frame.State.Build, Delta: &d})
			for _, c := range []struct {
				kind    string
				frame   StreamFrame
				covered map[string]bool
			}{{"full frame", fullFrame, full}, {"delta", deltaFrame, delta}} {
				state, err := assembler.State(c.frame)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.ContainsFunc(state.Simulation.Vehicles, func(w sim.Vehicle) bool { return w.Pod.ID == v.Pod.ID && w.Pod.Activity == v.Pod.Activity }) {
					t.Fatalf("activity %s: the %s does not show pod %s %s", key, c.kind, v.Pod.ID, v.Pod.Activity)
				}
				c.covered[key] = true
			}
			previous = &fullFrame
		}
	}
	return full, delta
}

// TestLondonSharedExpressIncidentFixtures is the evidence of rows SX8 and
// SX9: the directed fixtures of TestLondonExpressIncidentFixtures on the
// shared-track preset, with background demand and Compact trips between
// the hubs each 10 s. 20 pooled singletons share one Express pod, a fault
// stops that pod at speed on link 100 with its standees, and an emergency
// starts on a pod with standees at speed on link 100. It saves and restores
// at each state, and checks the safety oracle at each tick of each
// incident.
func TestLondonSharedExpressIncidentFixtures(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express fixtures run without -short or -race")
	}
	f := newSharedExpressFixture(t)
	shared := f.shared
	dt := 1.0 / sim.TicksPerSecond
	aboard := func(v sim.Vehicle) (parties, passengers int) {
		for _, r := range v.Riders {
			if !r.Completed && r.BoardedTick > 0 {
				parties, passengers = parties+1, passengers+r.PartySize
			}
		}
		return parties, passengers
	}
	find := func(id string) sim.Vehicle {
		for _, v := range shared.simulation.Snapshot().Vehicles {
			if v.Pod.ID == id {
				return v
			}
		}
		t.Fatalf("no pod %s", id)
		return sim.Vehicle{}
	}
	oracle := func(name string) {
		if _, err := shared.simulation.SafetyObservation().Check(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// until advances the session one second at a time, for at most limit
	// seconds, until done reports true. each runs at every tick. A Compact
	// trip between the hubs starts each 10 s.
	tick := 0
	until := func(name string, limit int, each func(), done func() bool) {
		t.Helper()
		for range limit {
			for range sim.TicksPerSecond {
				if tick%(10*sim.TicksPerSecond) == 5*sim.TicksPerSecond {
					f.plainTrip(tick)
				}
				tick++
				shared.advance()
				if each != nil {
					each()
				}
			}
			if done() {
				return
			}
		}
		t.Fatalf("%s: not reached in %d s", name, limit)
	}
	// mixed reports whether a Compact pod is on link 100 with the Express
	// pod id.
	mixed := func(id string) bool {
		vehicles := shared.simulation.Snapshot().Vehicles
		return onSharedLink(vehicles) && slices.ContainsFunc(vehicles, func(v sim.Vehicle) bool {
			return v.Pod.ID == id && slices.Contains(sharedExpressLink, v.Pod.LaneID)
		})
	}

	// Pooled singletons: 20 parties of one share one Express pod. Faults
	// hold all Express pods but one in parking until the pod departs. The
	// riders past the 12 seats stand.
	var held []string
	for _, v := range shared.simulation.Snapshot().Vehicles {
		if v.Pod.Class == sim.ExpressClass && v.Pod.ID != "express-01" {
			held = append(held, f.client.mustApply(t, Command{Action: "fault", PodID: v.Pod.ID}).FaultID)
		}
	}
	for range 20 {
		if refusal := f.trip(londonWaterloo, londonBank, 1); refusal != "" {
			t.Fatal(refusal)
		}
	}
	// Whole-party admission at the capacity boundary: a party of 21 is
	// refused, and a 21st singleton does not join the pod with 20
	// passengers. It waits for a second Express pod.
	if refusal := f.trip(londonWaterloo, londonBank, 21); refusal != "party size must be 1 to 20: invalid trip options" {
		t.Fatalf("a party of 21: refusal %q", refusal)
	}
	if refusal := f.trip(londonWaterloo, londonBank, 1); refusal != "" {
		t.Fatal(refusal)
	}
	pooled := ""
	standing := false
	until("pooled singletons", 600, nil, func() bool {
		for _, v := range shared.simulation.Snapshot().Vehicles {
			parties, passengers := aboard(v)
			if v.Pod.Class != sim.ExpressClass || parties == 0 {
				continue
			}
			if !standing && passengers > 12 && v.Pod.Activity == sim.Boarding {
				standing = true
				f.checkpoint("standees boarding")
			}
			if parties > 20 || passengers > 20 {
				t.Fatalf("pod %s carries %d parties and %d passengers", v.Pod.ID, parties, passengers)
			}
			if parties == 20 && v.Pod.Activity == sim.Traveling && v.Pod.Speed >= 10 && slices.Contains(sharedExpressLink, v.Pod.LaneID) {
				if !slices.ContainsFunc(shared.simulation.Snapshot().Pending, func(r sim.Request) bool {
					return r.Service == sim.ExpressServiceChoice && r.From == londonWaterloo && r.PartySize == 1
				}) {
					t.Fatal("the 21st singleton is not pending when the pod with 20 passengers departs")
				}
				pooled = v.Pod.ID
				return true
			}
		}
		return false
	})
	if !standing {
		t.Fatal("no Express pod boarded with standees")
	}
	f.checkpoint("pooled at speed on link 100")
	for _, id := range held {
		f.client.mustApply(t, Command{Action: "clearFault", FaultID: id})
	}

	// A fault at speed on the pooled pod on link 100. It brakes at the
	// maximum rate and stops within its braking distance.
	v := find(pooled)
	start, lane := v.Pod.Speed, v.Pod.LaneID
	f.client.mustApply(t, Command{Action: "fault", PodID: pooled, DurationSeconds: new(int64(60))})
	speed, largest, travel := start, 0.0, 0.0
	compact := false
	for range 30 * sim.TicksPerSecond {
		tick++
		shared.advance()
		oracle("fault")
		compact = compact || mixed(pooled)
		v = find(pooled)
		largest, travel, speed = max(largest, speed-v.Pod.Speed), travel+v.Pod.Speed*dt, v.Pod.Speed
		if speed == 0 {
			break
		}
	}
	if speed != 0 || v.Withdrawn&1 == 0 {
		t.Fatalf("the faulted pod did not stop: %+v withdrawn %d", v.Pod, v.Withdrawn)
	}
	if largest > expressMaximumBraking*dt+1e-9 || largest < 0.95*expressMaximumBraking*dt {
		t.Fatalf("largest drop %g, want about %g", largest, expressMaximumBraking*dt)
	}
	if limit := start * start / (2 * expressMaximumBraking); travel > limit+start*dt {
		t.Fatalf("the pod moved %g m after the fault, want at most %g", travel, limit)
	}
	if _, passengers := aboard(v); passengers != 20 {
		t.Fatalf("the faulted pod holds %d passengers", passengers)
	}
	t.Logf("fault on %s on %s at %.2f m/s: largest drop %.5f m/s per tick, %.2f m to rest on %s, a Compact pod on link 100 with it %v", pooled, lane, start, largest, travel, v.Pod.LaneID, compact)
	f.checkpoint("fault at rest with standees")
	until("fault end", 120, func() {
		oracle("fault hold")
		compact = compact || mixed(pooled)
	}, func() bool { return find(pooled).Withdrawn&1 == 0 })
	if !compact {
		t.Fatal("no Compact pod was on link 100 with the faulted pod")
	}

	// An emergency on a pod with standees at speed on link 100. It unloads
	// at a station and leaves service until the emergency ends.
	if refusal := f.trip(londonWaterloo, londonBank, 18); refusal != "" {
		t.Fatal(refusal)
	}
	target := ""
	until("standees at speed", 600, nil, func() bool {
		for _, v := range shared.simulation.Snapshot().Vehicles {
			if _, passengers := aboard(v); passengers > 12 && v.Pod.Activity == sim.Traveling && v.Pod.Speed >= 10 && slices.Contains(sharedExpressLink, v.Pod.LaneID) {
				target = v.Pod.ID
				return true
			}
		}
		return false
	})
	riders := map[int]bool{}
	for _, r := range find(target).Riders {
		if !r.Completed && r.BoardedTick > 0 {
			riders[r.ID] = true
		}
	}
	f.client.mustApply(t, Command{Action: "emergency", PodID: target})
	t.Logf("emergency on %s on %s at tick %d", target, find(target).Pod.LaneID, shared.simulation.Snapshot().Tick)
	f.checkpoint("emergency journey")
	unloaded, compact := false, false
	until("emergency unload", 600, func() {
		oracle("emergency")
		compact = compact || mixed(target)
	}, func() bool {
		v := find(target)
		if !unloaded && v.Pod.Activity == sim.Unloading && v.Operational == sim.OperationalEmergencyUnload {
			unloaded = true
			t.Logf("emergency unload of %s at %s", target, v.Pod.StationID)
			f.checkpoint("emergency unload")
		}
		// The pod can board a new Compact trip right after the unload, so
		// the test waits for the riders of the emergency journey.
		return unloaded && !slices.ContainsFunc(v.Riders, func(r sim.Request) bool { return riders[r.ID] && !r.Completed && r.BoardedTick > 0 })
	})
	if !compact {
		t.Fatal("no Compact pod was on link 100 with the emergency pod")
	}
}

// TestLondonSharedExpressByteBounds is the evidence of rows SX5 and SX13.
// TestExpressStateFileWorstCaseSize and TestExpressStreamMaximumEncoding
// pad a worst-case project to its caps (widestProofProject), so they do not
// take the geometry of the preset. This test shows that the worst-case
// project bounds the preset: the preset is a valid Express project, and each
// project input of the proofs is within the value that the proofs use. It
// then measures the saved state, the full stream, a replacement delta, the
// HTTP state, and the topology of the preset at each state of driveShared,
// with mixed-fleet, fault, and emergency members, against their caps.
func TestLondonSharedExpressByteBounds(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express byte bounds run without -short or -race")
	}
	f := newSharedExpressFixture(t, WithBuildID(testBuildID))
	config, n := f.config, f.config.Network
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	berths, express := 0, 0
	ids := []string{config.Name}
	for _, station := range n.Stations {
		berths += len(station.Berths)
		ids = append(ids, station.ID)
		for _, berth := range station.Berths {
			ids = append(ids, berth.ID)
		}
	}
	for _, node := range n.Nodes {
		ids = append(ids, node.ID)
	}
	for _, lane := range n.Lanes {
		ids = append(ids, lane.ID)
	}
	for _, placement := range config.Fleet {
		ids = append(ids, placement.ID)
		if placement.Class == sim.ExpressClass {
			express++
		}
	}
	for _, service := range config.ExpressServices {
		ids = append(ids, service.ID)
	}
	longest := 0
	for _, id := range ids[1:] {
		longest = max(longest, len(id))
	}
	projectBytes := len(marshalSavedJSON(t, config))
	for _, c := range []struct {
		name         string
		value, limit int
	}{
		{"nodes", len(n.Nodes), project.MaxNodes},
		{"lanes", len(n.Lanes), project.MaxLanes},
		{"stations", len(n.Stations), project.MaxStations},
		// The worst-case frame has a berth state for each of
		// project.MaxNodes nodes.
		{"berths", berths, project.MaxNodes},
		{"pods", len(config.Fleet), sim.MaxExpressPods},
		{"longest ID", longest, project.MaxIDLength},
		{"project JSON bytes", projectBytes, project.MaxFileBytes},
	} {
		t.Logf("%s: %d, the proofs use %d, margin %d", c.name, c.value, c.limit, c.limit-c.value)
		if c.value > c.limit {
			t.Errorf("the preset has %d %s, over the %d of the worst-case project", c.value, c.name, c.limit)
		}
	}
	if config.OrderContract != sim.ExpressOrderContract || express != 16 {
		t.Fatalf("the preset is not the Express project of the proofs: %q, %d Express pods", config.OrderContract, express)
	}
	topology, err := jsonv2.Marshal(f.shared.Topology(), json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	largest := map[string]int{"topology": len(topology)}
	encode := func(e StreamEnvelope) int {
		t.Helper()
		data, encodeErr := EncodeStreamJSON(e)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		return len(data)
	}
	var previous *StreamFrame
	ratio := 0.0
	f.driveShared(func(tick int, state string) {
		compressed := encodeTestState(t, sessionStateFile(t, f.shared))
		saved := decompressTestJSON(t, compressed)
		frame, frameErr := f.shared.presentationFrame()
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		full := encode(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "bounds", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame})
		delta := 0
		if previous != nil {
			d, deltaErr := makeDelta(*previous, frame)
			if deltaErr != nil {
				t.Fatal(deltaErr)
			}
			delta = encode(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: "bounds", Sequence: 2, Base: 1, Source: sourceOf(frame), Build: frame.State.Build, Delta: &d})
		}
		previous = &frame
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody)
		request.Header.Set("Accept", StateMediaType)
		response := httptest.NewRecorder()
		f.shared.HandlerFS(nil).ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Content-Encoding") != "" {
			t.Fatalf("%s: HTTP state %d, encoding %q", state, response.Code, response.Header().Get("Content-Encoding"))
		}
		sizes := map[string]int{"saved state": len(saved), "saved state gzip": len(compressed), "full stream": full, "delta": delta, "HTTP state": response.Body.Len()}
		t.Logf("%s at tick %d: %v", state, tick, sizes)
		for key, value := range sizes {
			largest[key] = max(largest[key], value)
		}
		ratio = max(ratio, float64(len(saved))/float64(len(compressed)))
	})
	for _, c := range []struct {
		name  string
		limit int
	}{
		{"saved state", MaxStateBytes}, {"full stream", MaxStreamJSON}, {"delta", MaxStreamJSON},
		{"HTTP state", MaxHTTPStateJSON}, {"topology", MaxTopologyJSON},
	} {
		t.Logf("largest %s: %d bytes, limit %d, margin %d", c.name, largest[c.name], c.limit, c.limit-largest[c.name])
		if largest[c.name] > c.limit {
			t.Errorf("%s of %d bytes exceeds its cap of %d", c.name, largest[c.name], c.limit)
		}
	}
	// The ratio is a compression ratio of one saved state. The bound of the
	// gzip expansion of a stream message is TestStreamGzipExpansionBound,
	// which does not depend on the geometry.
	t.Logf("largest saved state gzip: %d bytes, largest compression ratio of one saved state %.1f", largest["saved state gzip"], ratio)
}

// sharedExpressLedger holds the orders of a session: the counters, the
// passengers aboard, and each pending order and each rider by request ID.
type sharedExpressLedger struct {
	submitted, completed, interrupted, active, aboard int
	requests                                          map[int]sim.Request
}

// newSharedExpressLedger returns the ledger of a snapshot. It requires
// conservation: each accepted order is complete, interrupted, pending, or
// an active rider of one pod.
func newSharedExpressLedger(t *testing.T, name string, snapshot sim.Snapshot) sharedExpressLedger {
	t.Helper()
	l := sharedExpressLedger{submitted: snapshot.Submitted, completed: snapshot.Completed, interrupted: snapshot.Interrupted, requests: map[int]sim.Request{}}
	add := func(r sim.Request) {
		if _, ok := l.requests[r.ID]; ok {
			t.Fatalf("%s: request %d is in two places", name, r.ID)
		}
		l.requests[r.ID] = r
	}
	for _, r := range snapshot.Pending {
		add(r)
	}
	for _, v := range snapshot.Vehicles {
		for _, r := range v.Riders {
			if !r.Completed {
				add(r)
				l.active++
				if r.BoardedTick > 0 {
					l.aboard += r.PartySize
				}
			}
		}
	}
	if l.submitted != l.completed+l.interrupted+len(snapshot.Pending)+l.active {
		t.Fatalf("%s: %d orders, %d complete, %d interrupted, %d pending, %d active", name, l.submitted, l.completed, l.interrupted, len(snapshot.Pending), l.active)
	}
	return l
}

// TestLondonSharedExpressRecoveryChain is the repeated recovery of row
// SX8 on the preset. The session saves and restores four times, and each
// restored session is the source of the next save. The saves are with
// Express and Compact pods on link 100, with a fault on an Express pod with
// riders, with an emergency on an Express pod with riders, and after the
// emergency unload. Each restore must be physical with no demotion,
// requeue, drop, or unaccounted order. The counters, the passengers
// aboard, and each pending order and active rider, with its service, its
// party, and its stations, must not change, and the orders must be
// conserved.
func TestLondonSharedExpressRecoveryChain(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express recovery chain runs without -short or -race")
	}
	config, err := scenarios.GenerateLondonFullSharedExpress()
	if err != nil {
		t.Fatal(err)
	}
	config.Demand.Enabled = true
	store := &fakeStore{}
	first := startFromStore(t, StoreInput{Store: store, Project: &config})
	t.Cleanup(first.Close)
	f := sharedExpressFixtureFor(t, config, first)
	tick := 0
	// run advances the session for at most seconds, with hub parties and
	// Compact trips between the hubs, until done reports true.
	run := func(name string, seconds int, done func([]sim.Vehicle) bool) {
		t.Helper()
		for range seconds * sim.TicksPerSecond {
			if tick%(30*sim.TicksPerSecond) == 0 {
				if refusal := f.trip(londonWaterloo, londonBank, 13+tick/(30*sim.TicksPerSecond)%8); refusal != "" {
					t.Fatal(refusal)
				}
			}
			if tick%(60*sim.TicksPerSecond) == 15*sim.TicksPerSecond {
				if refusal := f.trip(londonBank, londonWaterloo, 12); refusal != "" {
					t.Fatal(refusal)
				}
			}
			if tick%(10*sim.TicksPerSecond) == 5*sim.TicksPerSecond {
				f.plainTrip(tick)
			}
			tick++
			f.shared.advance()
			if tick%sim.TicksPerSecond == 0 && done != nil && done(f.shared.simulation.Snapshot().Vehicles) {
				return
			}
		}
		if done != nil {
			t.Fatalf("%s: not reached in %d s", name, seconds)
		}
	}
	hops := 0
	hop := func(name string) {
		t.Helper()
		before := newSharedExpressLedger(t, name+", saved", f.shared.simulation.Snapshot())
		f.shared.Close()
		if err := f.shared.SaveState(t.Context(), SaveFinal); err != nil {
			t.Fatal(err)
		}
		writes := store.writeList()
		store = &fakeStore{data: writes[len(writes)-1]}
		next := startFromStore(t, StoreInput{Store: store, Project: &config})
		t.Cleanup(next.Close)
		if info := next.State().Restore; info.Tier != "physical" || info.Demoted+info.Requeued+info.Dropped+info.Unaccounted != 0 {
			t.Fatalf("%s: restore %+v", name, info)
		}
		after := newSharedExpressLedger(t, name+", restored", next.simulation.Snapshot())
		if after.submitted != before.submitted || after.completed != before.completed || after.interrupted != before.interrupted || after.active != before.active || after.aboard != before.aboard {
			t.Fatalf("%s: the restore changed the orders: %+v, want %+v", name, after, before)
		}
		for id, want := range before.requests {
			if got := after.requests[id]; !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: request %d is %+v, want %+v", name, id, got, want)
			}
		}
		hops++
		t.Logf("%s at tick %d: %d orders, %d complete, %d interrupted, %d pending or active, %d passengers aboard", name, tick, after.submitted, after.completed, after.interrupted, len(after.requests), after.aboard)
		f = sharedExpressFixtureFor(t, config, next)
		// The restored session keeps the sequence of each client, so each
		// session has a new client.
		f.client = newTestClient(next, fmt.Sprintf("london-shared-express-%d", hops))
	}
	riders := func(v sim.Vehicle) bool {
		return v.Pod.Class == sim.ExpressClass && v.PassengersAboard() > 0 && v.Pod.Speed > 0 && slices.Contains(sharedExpressLink, v.Pod.LaneID)
	}
	target := ""
	run("mixed traffic", 600, func(vehicles []sim.Vehicle) bool {
		return onSharedLink(vehicles) && slices.ContainsFunc(vehicles, riders)
	})
	hop("mixed traffic on link 100")
	run("Express riders for the fault", 600, func(vehicles []sim.Vehicle) bool {
		index := slices.IndexFunc(vehicles, riders)
		if index >= 0 {
			target = vehicles[index].Pod.ID
		}
		return index >= 0
	})
	f.client.mustApply(t, Command{Action: "fault", PodID: target, DurationSeconds: new(int64(60))})
	run("fault", 2, nil)
	hop("fault")
	faulted := target
	run("Express riders for the emergency", 600, func(vehicles []sim.Vehicle) bool {
		index := slices.IndexFunc(vehicles, func(v sim.Vehicle) bool { return riders(v) && v.Pod.ID != faulted })
		if index >= 0 {
			target = vehicles[index].Pod.ID
		}
		return index >= 0
	})
	f.client.mustApply(t, Command{Action: "emergency", PodID: target})
	run("emergency", 2, nil)
	hop("emergency")
	run("emergency unload", 600, func(vehicles []sim.Vehicle) bool {
		return slices.ContainsFunc(vehicles, func(v sim.Vehicle) bool {
			return v.Pod.ID == target && v.Pod.Activity == sim.Unloading && v.Operational == sim.OperationalEmergencyUnload
		})
	})
	hop("emergency unload")
	run("after the chain", 60, nil)
	if _, err := f.shared.simulation.SafetyObservation().Check(); err != nil {
		t.Fatal(err)
	}
	newSharedExpressLedger(t, "after the chain", f.shared.simulation.Snapshot())
	if hops != 4 {
		t.Fatalf("%d hops", hops)
	}
}
