package session

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"math"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

func onboardConsumerProject() project.Config {
	config := project.Default()
	config.Version, config.OnboardPickups, config.SharedRidePartyLimit = project.ServiceVersion, true, 4
	config.Fleet = []sim.Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
	config.RailDepartures = []project.RailDeparture{{ID: "authored-transfer", Station: "market", AtSeconds: 600,
		Passengers: 1, Origins: []project.RailOrigin{{Station: "garden", Weight: 1}}}}
	return config
}

func newOnboardConsumerRide(t *testing.T) (*Session, StreamFrame) {
	t.Helper()
	s, err := NewWithProject(onboardConsumerProject())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.advance()
	client := newTestClient(s, "manual-shared")
	for _, pair := range [][2]string{{"harbor", "market"}, {"harbor", "garden"}, {"garden", "market"}} {
		client.mustApply(t, Command{Action: "trip", Origin: pair[0], Destination: pair[1], SharingConsent: sim.SharedConsent})
	}
	// Bind the authored transfer to manual request 3. Generated offers keep their consent.
	offers := project.RailServicesSchedule(nil, s.project.RailDepartures, s.project.Demand.Seed)
	if len(offers) != 1 || offers[0].From != "garden" || offers[0].To != "market" || offers[0].Tick != 1 {
		t.Fatalf("authored transfer fixture: %+v", offers)
	}
	if err := s.demand.connections.Add(offers[0], 3, ""); err != nil {
		t.Fatal(err)
	}
	s.demand.state.Connections = s.demand.connections.Counts()
	previous := onboardConsumerFrame(t, s)
	for range 300 * sim.TicksPerSecond {
		s.advance()
		current := onboardConsumerFrame(t, s)
		v := current.State.Simulation.Vehicles[0]
		if v.Pod.Activity == sim.Boarding && v.Pod.Occupied {
			if len(v.Riders) != 3 || len(v.Boardings) != 3 || !v.Riders[1].Completed ||
				v.Riders[2].ID != 3 || v.Riders[2].From != "garden" || len(previous.State.Simulation.Vehicles[0].Boardings) != 0 ||
				v.Boardings[0].BerthID != "harbor-1" || v.Boardings[0].MetersAtBoarding != 0 ||
				v.Boardings[2].BerthID != "garden-1" || v.Boardings[2].MetersAtBoarding != v.RiddenMeters || v.RiddenMeters <= 0 {
				t.Fatalf("occupied pickup fixture: %+v", v)
			}
			for _, rider := range v.Riders {
				if rider.SharingConsent != sim.SharedConsent || rider.Service != sim.OnDemandService || rider.PartySize != 1 {
					t.Fatalf("manual order changed: %+v", rider)
				}
			}
			return s, previous
		}
		previous = current
	}
	t.Fatal("occupied boarding phase did not occur")
	return nil, StreamFrame{}
}

func onboardConsumerFrame(t *testing.T, s *Session) StreamFrame {
	t.Helper()
	frame, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func onboardConsumerFinish(t *testing.T, s *Session) sim.Snapshot {
	t.Helper()
	for range 600 * sim.TicksPerSecond {
		s.advance()
		state := s.simulation.Snapshot()
		onboardConsumerConservation(t, state)
		if state.Completed == 3 && s.demand.state.Connections.Made == 1 {
			if s.simulation.ExportState().Journeys != 3 || state.MaxDetourRatio > 1.5+1e-9 || state.RiderDistanceMeters <= 0 {
				t.Fatalf("completion metrics: %+v", state)
			}
			return state
		}
	}
	t.Fatalf("journeys or authored transfer did not complete: %+v, %+v", s.simulation.Snapshot(), s.demand.state.Connections)
	return sim.Snapshot{}
}

func onboardConsumerConservation(t *testing.T, state sim.Snapshot) {
	t.Helper()
	seen := make(map[int]bool)
	active := len(state.Pending)
	for _, request := range state.Pending {
		if seen[request.ID] || request.Completed || request.SharingConsent != sim.SharedConsent {
			t.Fatalf("invalid pending identity: %+v", request)
		}
		seen[request.ID] = true
	}
	for _, v := range state.Vehicles {
		for _, rider := range v.Riders {
			if rider.Completed {
				continue
			}
			if seen[rider.ID] || rider.SharingConsent != sim.SharedConsent {
				t.Fatalf("duplicate or changed active identity: %+v", rider)
			}
			seen[rider.ID] = true
			active++
		}
	}
	if state.Submitted != 3 || state.Submitted != state.Completed+active {
		t.Fatalf("request conservation: submitted %d, completed %d, active %d", state.Submitted, state.Completed, active)
	}
}

func TestOnboardConsumerSaveRestore(t *testing.T) {
	t.Parallel()
	s, _ := newOnboardConsumerRide(t)
	advanceTicks(s, 37)
	file := sessionStateFile(t, s)
	file.RestoreAttempts = 0
	pod := file.Simulation.Pods[0]
	if pod.PhaseTicks <= 0 || pod.PhaseTicks >= 3*sim.TicksPerSecond || len(pod.Boardings) != 3 {
		t.Fatalf("save did not capture partial occupied dwell: %+v", pod)
	}
	data := encodeTestState(t, file)
	raw := decompressTestJSON(t, data)
	if bytes.Contains(raw, []byte(`"journeyOrigin"`)) || bytes.Contains(raw, []byte(`"MetersAtBoarding"`)) ||
		!bytes.Contains(raw, []byte(`"boardings":[[0,0],[0,0],[0,`)) {
		t.Fatal("actual exporter did not use saved-source tuples")
	}
	decoded, err := decodeCheckedState(data)
	if err != nil || len(decoded.Simulation.Pods[0].Boardings) != 0 || len(decoded.boardingTuples) != 1 {
		t.Fatalf("decoder published unresolved records: %v", err)
	}
	if resolveErr := decoded.resolveBoardings(); resolveErr != nil || !slices.Equal(decoded.Simulation.Pods[0].Boardings, pod.Boardings) {
		t.Fatalf("saved source resolution: %v", resolveErr)
	}
	selected := project.Clone(s.project)
	selected.OnboardPickups = false
	loaded, err := s.loadState(loadInput{data: data, project: &selected, steps: realRestoreSteps()})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.result.Tier != sim.RestorePhysical || loaded.config.OnboardPickups {
		t.Fatalf("selected policy-off physical restore: %+v", loaded.result)
	}
	restoredPod := loaded.simulation.ExportState().Pods[0]
	if restoredPod.PhaseTicks != pod.PhaseTicks || restoredPod.RiddenMeters != pod.RiddenMeters ||
		!reflect.DeepEqual(restoredPod.Riders, pod.Riders) || !slices.Equal(restoredPod.Boardings, pod.Boardings) {
		t.Fatalf("physical restore changed dwell, riders, or origins: %+v", restoredPod)
	}
	for _, enabled := range []bool{false, true} {
		candidate := loaded.simulation.Clone()
		if enabled {
			if policyErr := candidate.SetOnboardPickups(true); policyErr != nil {
				t.Fatal(policyErr)
			}
		}
		id, offerErr := candidate.SubmitTripOptions(sim.TripOptions{From: "garden", To: "market", SharingConsent: sim.SharedConsent})
		if offerErr != nil || id != 4 {
			t.Fatalf("restored admission offer: %v, ID %d", offerErr, id)
		}
		got := candidate.ExportState()
		wantRecords, wantWaiting := 3, 1
		if enabled {
			wantRecords, wantWaiting = 4, 0
		}
		if len(got.Pods[0].Boardings) != wantRecords || len(got.Waiting) != wantWaiting || got.Pods[0].PhaseTicks != pod.PhaseTicks {
			t.Fatalf("restored policy %t changed admission or dwell: %+v", enabled, got)
		}
	}
	restarted := newSession(nil, nil)
	restarted.installRestored(loaded)
	t.Cleanup(restarted.Close)
	before := loaded.simulation.Snapshot().Vehicles[0]
	before.Boardings[2].BerthID = "observer-change"
	if loaded.simulation.Snapshot().Vehicles[0].Boardings[2].BerthID != "garden-1" {
		t.Fatal("native snapshot borrowed authoritative records")
	}
	want, got := onboardConsumerFinish(t, s), onboardConsumerFinish(t, restarted)
	// Rider 2 leaves at the pickup baseline. Rider 3 rides only the remaining distance.
	for _, state := range []sim.Snapshot{want, got} {
		if math.Abs(state.RiderDistanceMeters-2*state.Vehicles[0].RiddenMeters) > 1e-8 {
			t.Fatal("rider distance totals included travel before the occupied pickup")
		}
	}
	for _, metric := range []struct {
		name      string
		want, got float64
	}{
		{"passenger", want.PassengerDistanceMeters, got.PassengerDistanceMeters},
		{"rider", want.RiderDistanceMeters, got.RiderDistanceMeters},
		{"direct", want.DirectDistanceMeters, got.DirectDistanceMeters},
		{"detour", want.MaxDetourRatio, got.MaxDetourRatio},
	} {
		if math.Abs(metric.want-metric.got) > 1e-8 {
			t.Errorf("%s metric changed: %.12g -> %.12g", metric.name, metric.want, metric.got)
		}
	}
	if !slices.Equal(s.demand.connectionRecords(), restarted.demand.connectionRecords()) {
		t.Fatal("physical restore changed the authored transfer outcome")
	}
	frozen := got.Vehicles[0]
	if len(frozen.Boardings) != 3 || frozen.RiddenMeters <= pod.Boardings[2].MetersAtBoarding ||
		!slices.ContainsFunc(frozen.Riders, func(rider sim.Request) bool { return rider.ID == 3 && rider.Completed }) {
		t.Fatalf("completed history was lost: %+v", frozen)
	}
	resaved := sessionStateFile(t, restarted)
	resaved.RestoreAttempts = 0
	again, err := restarted.loadState(loadInput{data: encodeTestState(t, resaved), project: &selected, steps: realRestoreSteps()})
	if err != nil || again.result.Tier != sim.RestorePhysical || !slices.Equal(again.simulation.Snapshot().Vehicles[0].Boardings, frozen.Boardings) {
		t.Fatalf("completed history resave: %v", err)
	}
}

func TestOnboardConsumerLogicalRetry(t *testing.T) {
	t.Parallel()
	s, _ := newOnboardConsumerRide(t)
	file := sessionStateFile(t, s)
	file.RestoreAttempts = restoreLoopAttempts - 1
	selected := project.Clone(s.project)
	selected.OnboardPickups = false
	loaded, err := s.loadState(loadInput{data: encodeTestState(t, file), project: &selected, steps: realRestoreSteps()})
	if err != nil || loaded.result.Tier != sim.RestoreLogical || !slices.Equal(loaded.result.Requeued, []int{1, 3}) ||
		len(loaded.result.Dropped) != 0 || loaded.result.Unaccounted != 0 {
		t.Fatalf("logical retry receipt: %v, %+v", err, loaded.result)
	}
	state := loaded.simulation.Snapshot()
	onboardConsumerConservation(t, state)
	if state.Completed != 1 || len(state.Pending) != 2 || len(state.Vehicles[0].Boardings) != 0 ||
		loaded.demand.state.Connections != (rail.Counts{Unresolved: 1}) || loaded.demand.connectionRecords()[0].RequestID != 3 {
		t.Fatalf("logical retry lost identity or ledger binding: %+v", state)
	}
	restarted := newSession(nil, nil)
	restarted.installRestored(loaded)
	t.Cleanup(restarted.Close)
	if restarted.restore.Reason != reasonRestoreLoop {
		t.Fatal("logical retry did not retain its reason")
	}
	resaved := sessionStateFile(t, restarted)
	resaved.RestoreAttempts = 0
	again, err := restarted.loadState(loadInput{data: encodeTestState(t, resaved), project: &selected, steps: realRestoreSteps()})
	if err != nil || again.result.Tier != sim.RestorePhysical {
		t.Fatalf("save after logical retry: %v", err)
	}
	restarted.installRestored(again)
	onboardConsumerFinish(t, restarted)
	if records := restarted.demand.connectionRecords(); len(records) != 1 || records[0].RequestID != 3 || records[0].Outcome != "made" || records[0].AlightedTick < 0 {
		t.Fatalf("retry changed authored transfer identity: %+v", records)
	}
}

func TestOnboardConsumerSourceGuards(t *testing.T) {
	t.Parallel()
	s, _ := newOnboardConsumerRide(t)
	file := sessionStateFile(t, s)
	file.RestoreAttempts = 0
	data := encodeTestState(t, file)
	for _, tc := range []struct {
		name string
		edit func(*project.Config)
	}{
		{"reordered caller", func(config *project.Config) { slices.Reverse(config.Network.Stations) }},
		{"changed source berth", func(config *project.Config) { config.Network.Stations[0].Berths[0].ID = "other-source" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selected := project.Clone(file.Project)
			tc.edit(&selected)
			steps := realRestoreSteps()
			calls := 0
			steps.restoreSimulation = func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
				calls++
				return sim.RestoreState(input)
			}
			loaded, err := s.loadState(loadInput{data: data, project: &selected, steps: steps})
			var stateErr *stateError
			if !errors.As(err, &stateErr) || stateErr.reason != reasonProjectChanged || calls != 0 ||
				len(loaded.file.Simulation.Pods[0].Boardings) != 0 {
				t.Fatalf("caller mismatch reached record publication or native restore: %v, calls %d", err, calls)
			}
		})
	}
	raw := decompressTestJSON(t, data)
	for _, tc := range []struct{ name, old, replacement string }{
		{"unknown source", `"from":"harbor"`, `"from":"missing"`},
		{"tuple shape", `"boardings":[[0,0]`, `"boardings":[[0,0,0]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			changed := bytes.Replace(raw, []byte(tc.old), []byte(tc.replacement), 1)
			if bytes.Equal(changed, raw) {
				t.Fatal("malformed fixture did not change bytes")
			}
			steps := realRestoreSteps()
			calls := 0
			steps.restoreSimulation = func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
				calls++
				return sim.RestoreState(input)
			}
			if _, err := s.loadState(loadInput{data: compressTestJSON(t, changed), steps: steps}); err == nil || calls != 0 {
				t.Fatalf("malformed actual state reached native restore: %v, calls %d", err, calls)
			}
		})
	}
}

func onboardConsumerEnvelope(t *testing.T, envelope StreamEnvelope) StreamEnvelope {
	t.Helper()
	encoded, err := encodeStream(envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := InflateStream(encoded)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeStreamJSONVersion(raw, FoundationStreamVersion)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestOnboardConsumerStreamLifecycle(t *testing.T) {
	t.Parallel()
	s, prior := newOnboardConsumerRide(t)
	assembler, err := NewStreamAssemblerVersion(s.Topology(), FoundationStreamVersion)
	if err != nil || FoundationStreamVersion != 3 {
		t.Fatalf("hello3 topology: %v", err)
	}
	envelope := onboardConsumerEnvelope(t, StreamEnvelope{Kind: "full", Stream: "occupied", Sequence: 1, Source: sourceOf(prior), Build: prior.State.Build, Full: &prior})
	current, err := ApplyStream(StreamFrame{}, "", 0, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, assembleErr := assembler.State(current); assembleErr != nil {
		t.Fatal(assembleErr)
	}
	sequence := uint64(1)
	publish := func(next StreamFrame) StreamDelta {
		t.Helper()
		before, codecErr := json.Marshal(current)
		if codecErr != nil {
			t.Fatal(codecErr)
		}
		delta, codecErr := makeDelta(current, next)
		if codecErr != nil {
			t.Fatal(codecErr)
		}
		envelope := onboardConsumerEnvelope(t, StreamEnvelope{Kind: "delta", Stream: "occupied", Sequence: sequence + 1, Base: sequence,
			Source: sourceOf(next), Build: next.State.Build, Delta: &delta})
		candidate, codecErr := ApplyStream(current, "occupied", sequence, envelope)
		if codecErr != nil {
			t.Fatal(codecErr)
		}
		state, codecErr := assembler.State(candidate)
		if codecErr != nil {
			t.Fatal(codecErr)
		}
		if !reflect.DeepEqual(state.Simulation.Vehicles[0].Riders, s.simulation.Snapshot().Vehicles[0].Riders) ||
			!slices.Equal(state.Simulation.Vehicles[0].Boardings, next.State.Simulation.Vehicles[0].Boardings) ||
			state.Simulation.Vehicles[0].RiddenMeters != next.State.Simulation.Vehicles[0].RiddenMeters {
			t.Fatal("stream reconstruction changed authoritative passenger state")
		}
		after, codecErr := json.Marshal(current)
		if codecErr != nil || !bytes.Equal(before, after) {
			t.Fatal("delta changed predecessor")
		}
		if len(state.Simulation.Vehicles[0].Boardings) > 0 {
			state.Simulation.Vehicles[0].Boardings[0].BerthID = "observer-change"
			owned, codecErr := assembler.State(candidate)
			if codecErr != nil || owned.Simulation.Vehicles[0].Boardings[0].BerthID != "harbor-1" {
				t.Fatal("assembler output borrowed records")
			}
		}
		current, sequence = candidate, sequence+1
		return delta
	}
	entry := publish(onboardConsumerFrame(t, s))
	if len(entry.Vehicles) != 1 || entry.Vehicles[0].Boardings == nil || entry.Vehicles[0].Riders == nil {
		t.Fatal("occupied entry did not replace both arrays")
	}
	bad := entry
	bad.Vehicles = slices.Clone(entry.Vehicles)
	bad.Vehicles[0].Riders = nil
	if _, applyErr := ApplyStream(prior, "occupied", 1, StreamEnvelope{Kind: "delta", Stream: "occupied", Sequence: 2, Base: 1,
		Source: sourceOf(current), Build: current.State.Build, Delta: &bad}); applyErr == nil {
		t.Fatal("consumer accepted unpaired record entry")
	}
	unpaired := StreamDelta{Vehicles: []VehicleDelta{{ID: "01", Boardings: &Replacement[[]sim.RiderBoarding]{Value: slices.Clone(current.State.Simulation.Vehicles[0].Boardings)}}}}
	if _, pairErr := ApplyStream(current, "occupied", sequence, StreamEnvelope{Kind: "delta", Stream: "occupied", Sequence: sequence + 1, Base: sequence,
		Source: sourceOf(current), Build: current.State.Build, Delta: &unpaired}); pairErr == nil {
		t.Fatal("consumer accepted unpaired same-length record replacement")
	}
	onboardConsumerStreamGuards(t, assembler, current)
	// The occupied full frame decodes under hello 3.
	onboardConsumerEnvelope(t, StreamEnvelope{Kind: "full", Stream: "occupied", Sequence: 1, Source: sourceOf(current), Build: current.State.Build, Full: &current})
	for range 300 * sim.TicksPerSecond {
		s.advance()
		v := s.simulation.Snapshot().Vehicles[0]
		if v.Pod.Activity == sim.Traveling && v.RiddenMeters > current.State.Simulation.Vehicles[0].RiddenMeters {
			break
		}
	}
	travel := onboardConsumerFrame(t, s)
	if travel.State.Simulation.Vehicles[0].Pod.Activity != sim.Traveling || travel.State.Simulation.Vehicles[0].RiddenMeters <= current.State.Simulation.Vehicles[0].RiddenMeters {
		t.Fatal("record-bearing passenger travel did not occur")
	}
	publish(travel)
	advanceTicks(s, 1)
	update := publish(onboardConsumerFrame(t, s))
	if len(update.Vehicles) != 1 || update.Vehicles[0].Metadata == nil || update.Vehicles[0].Riders != nil || update.Vehicles[0].Boardings != nil ||
		update.Vehicles[0].Metadata.Value.RiddenMeters <= travel.State.Simulation.Vehicles[0].RiddenMeters {
		t.Fatal("passenger travel did not emit a metadata-only C update")
	}
	for range 300 * sim.TicksPerSecond {
		s.advance()
		if s.simulation.Snapshot().Completed == 3 {
			break
		}
	}
	completed := onboardConsumerFrame(t, s)
	if completed.State.Simulation.Completed != 3 || len(completed.State.Simulation.Vehicles[0].Boardings) != 3 {
		t.Fatal("completed record history did not occur")
	}
	publish(completed)
	client := newTestClient(s, "ordinary-after-history")
	client.mustApply(t, Command{Action: "trip", Origin: "market", Destination: "harbor"})
	for range 300 * sim.TicksPerSecond {
		if s.simulation.Snapshot().Vehicles[0].Pod.Activity == sim.Boarding {
			break
		}
		s.advance()
	}
	ordinary := onboardConsumerFrame(t, s)
	v := ordinary.State.Simulation.Vehicles[0]
	if v.Pod.Activity != sim.Boarding || len(v.Riders) != 1 || v.Riders[0].ID != 4 || v.Riders[0].SharingConsent != sim.PrivateConsent || len(v.Boardings) != 0 || v.RiddenMeters != 0 {
		t.Fatalf("ordinary new boarding did not clear history: %+v", v)
	}
	cleared := publish(ordinary)
	if len(cleared.Vehicles) != 1 || cleared.Vehicles[0].Riders == nil || cleared.Vehicles[0].Boardings == nil ||
		cleared.Vehicles[0].Boardings.Value == nil || len(cleared.Vehicles[0].Boardings.Value) != 0 || cleared.Vehicles[0].Metadata == nil || cleared.Vehicles[0].Metadata.Value.RiddenMeters != 0 {
		t.Fatal("ordinary boarding did not emit paired arrays and C clearing")
	}
}

func onboardConsumerStreamGuards(t *testing.T, assembler *StreamAssembler, frame StreamFrame) {
	t.Helper()
	for _, tc := range []struct{ name, berth string }{{"unknown berth", "missing"}, {"wrong source", "market-1"}, {"long ID", strings.Repeat("x", 65)}} {
		bad := ownStreamBoardings(frame)
		bad.State.Simulation.Vehicles[0].Boardings[0].BerthID = tc.berth
		if _, err := assembler.State(bad); err == nil {
			t.Fatalf("assembler accepted %s", tc.name)
		}
		state, err := assembler.State(frame)
		if err != nil || state.Simulation.Vehicles[0].Boardings[0].BerthID != "harbor-1" {
			t.Fatalf("%s rejection changed published state", tc.name)
		}
	}
}

func TestOnboardConsumerHello(t *testing.T) {
	t.Parallel()
	s, _ := newOnboardConsumerRide(t)
	server := httptest.NewServer(s.HandlerFS(fstest.MapFS{}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(MaxStreamMessage)
	kind, raw, err := conn.Read(ctx)
	var hello struct {
		Kind        string `json:"kind"`
		Version     int    `json:"version"`
		Build       string `json:"build"`
		ServerStart string `json:"serverStart"`
	}
	if err != nil || kind != websocket.MessageText {
		t.Fatalf("hello: %v", err)
	}
	if helloErr := json.Unmarshal(raw, &hello); helloErr != nil || hello.Kind != "hello" || hello.Version != 3 || hello.ServerStart != s.serverStart {
		t.Fatalf("hello3 contract: %s, %v", raw, helloErr)
	}
	kind, data, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary {
		t.Fatalf("full message: %v", err)
	}
	raw, err = InflateStream(data)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := DecodeStreamJSONVersion(raw, hello.Version)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := ApplyStream(StreamFrame{}, "", 0, envelope)
	if err != nil || envelope.Kind != "full" {
		t.Fatalf("full: %v", err)
	}
	assembler, err := NewStreamAssemblerVersion(s.Topology(), hello.Version)
	if err != nil {
		t.Fatal(err)
	}
	state, err := assembler.State(frame)
	if err != nil || len(state.Simulation.Vehicles[0].Boardings) != 3 || state.Simulation.Vehicles[0].RiddenMeters <= 0 {
		t.Fatalf("hello3 occupied reconstruction: %v", err)
	}
}

func TestOnboardConsumerGeneratedRailConsent(t *testing.T) {
	t.Parallel()
	config := onboardConsumerProject()
	config.Demand.Enabled, config.Demand.Pattern = true, "rail-services"
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	client := newTestClient(s, "manual-host")
	for _, destination := range []string{"market", "garden"} {
		client.mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: destination, SharingConsent: sim.SharedConsent})
	}
	s.advance()
	state := s.simulation.Snapshot()
	if s.demand.state.Generated != 1 || len(state.Pending) != 1 || state.Pending[0].ID != 3 || state.Pending[0].SharingConsent != sim.PrivateConsent {
		t.Fatalf("generated rail order changed consent: %+v", state.Pending)
	}
	encountered := false
	for range 300 * sim.TicksPerSecond {
		s.advance()
		state = s.simulation.Snapshot()
		v := state.Vehicles[0]
		if v.Pod.Activity == sim.Boarding && v.Pod.Occupied || len(v.Boardings) != 0 {
			t.Fatal("generated private request joined an occupied host")
		}
		if v.Pod.StationID == "garden" && v.Pod.Occupied {
			encountered = true
			if len(state.Pending) != 1 || state.Pending[0].ID != 3 || state.Pending[0].SharingConsent != sim.PrivateConsent || len(v.Riders) != 2 {
				t.Fatalf("garden encounter changed generated private order: %+v", state)
			}
		}
		if state.Completed == 2 {
			break
		}
	}
	if !encountered || state.Completed != 2 || len(state.Pending) != 1 || state.Pending[0].SharingConsent != sim.PrivateConsent {
		t.Fatal("private offer did not remain queued through the occupied garden encounter")
	}
	for range 300 * sim.TicksPerSecond {
		s.advance()
		state = s.simulation.Snapshot()
		if state.Completed == 3 {
			break
		}
	}
	if state.Completed != 3 || len(state.Pending) != 0 || state.Vehicles[0].Riders[0].ID != 3 || state.Vehicles[0].Riders[0].SharingConsent != sim.PrivateConsent ||
		len(s.demand.connectionRecords()) != 1 || s.demand.connectionRecords()[0].RequestID != 3 || s.demand.connectionRecords()[0].AlightedTick < 0 {
		t.Fatal("generated private order did not complete as an ordinary journey")
	}
}
