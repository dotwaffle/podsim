package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

// plainProofFloat uses a sign and the longest fixed decimal form.
const plainProofFloat = -0.0000010000000000000002

// These fixtures bound plain state, without Express, compact pods, or
// groups, with each incident, fault, and emergency member at its widest.
// Scalar widths and field combinations overestimate valid state. The project
// and topology use their independent byte caps.
func TestPlainStateFileWorstCaseSize(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("plain byte proofs run without -short or -race, in test:embedded")
	}
	file := widestPlainSavedState(t)
	addWidestSavedMembers(t, &file)
	data := marshalWidestSave(t, file)
	// The pod encoder of a save writes the operational tuple. The native pod
	// has no such member, so the proof counts its bytes here.
	operational := len(`,"operational":[1,2,4294967295]`) * project.MaxPods
	t.Logf("saved state: %d encoded bytes, %d operational tuple bytes", len(data), operational)
	assertPlainByteCap(t, "saved state", len(data)+operational, MaxStateBytes)
}

// addWidestSavedMembers gives file each incident, fault, and emergency member
// of a save at its widest. Each pod has both holds and an emergency unload
// with a full interrupt set, each order a leg origin of the last station, and
// each waiting trip an excluded pod. Each record is the widest tuple, in
// serial order, each counter is sim.MaxCounter, and each sampler has a valid
// stream and the widest next tick.
func addWidestSavedMembers(t *testing.T, file *stateFile) {
	t.Helper()
	state := &file.Simulation
	state.Interrupted, state.InterruptedPassengers, state.IncidentSerial = sim.MaxCounter, sim.MaxCounter, math.MaxUint64
	station := file.Project.Network.Stations[project.MaxStations-1].ID
	state.Pods = slices.Clone(state.Pods)
	for index := range state.Pods {
		pod := &state.Pods[index]
		pod.Withdrawn, pod.Purpose, pod.Owner, pod.Interrupt = 3, 1, 2, math.MaxUint32
		pod.Riders = slices.Clone(pod.Riders)
		for rider := range pod.Riders {
			pod.Riders[rider].LegFrom = station
		}
	}
	state.Waiting = slices.Clone(state.Waiting)
	for index := range state.Waiting {
		state.Waiting[index].Request.LegFrom = station
		state.Waiting[index].ExcludedPod = state.Pods[0].ID
	}
	faults := make([]sim.SavedFault, maxFaultRecords)
	for index := range faults {
		faults[index] = sim.SavedFault{Generation: math.MaxUint64, Serial: math.MaxUint64 - uint64(maxFaultRecords-index), Debris: true,
			Start: sim.MaxCounter, End: sim.MaxCounter, Lane: project.MaxLanes - 1, From: plainProofFloat, To: plainProofFloat}
	}
	// The scalar filler cannot build a valid PCG state.
	random, err := rand.NewPCG(math.MaxUint64, math.MaxUint64).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	state.Faults = &sim.SavedFaults{Records: faults, Counters: widestFaultCounters(), Random: random, NextTick: new(int64(sim.MaxCounter))}
	emergencies := make([]sim.SavedEmergency, sim.MaxEmergencies)
	for index := range emergencies {
		emergencies[index] = sim.SavedEmergency{Generation: math.MaxUint64, Serial: math.MaxUint64 - uint64(sim.MaxEmergencies-index),
			Start: sim.MaxCounter, Pod: project.MaxPods - 1, Order: sim.MaxCounter}
	}
	state.Emergencies = &sim.SavedEmergencies{Records: emergencies, Counters: widestEmergencyCounters(), Random: random, NextTick: new(int64(sim.MaxCounter))}
}

// widestFaultCounters has each fault counter at its widest.
func widestFaultCounters() sim.FaultCounters {
	return sim.FaultCounters{Started: sim.MaxCounter, Cleared: sim.MaxCounter, Evacuations: sim.MaxCounter, Reroutes: sim.MaxCounter, FaultWaitTicks: sim.MaxCounter,
		Sampled: sim.MaxCounter, Skipped: sim.MaxCounter, ClearedTicks: sim.MaxCounter, DelayedRiderTicks: sim.MaxCounter}
}

// widestEmergencyCounters has each emergency counter at its widest.
func widestEmergencyCounters() sim.EmergencyCounters {
	return sim.EmergencyCounters{Started: sim.MaxCounter, Ended: sim.MaxCounter, EmergencyTicks: sim.MaxCounter,
		Sampled: sim.MaxCounter, Skipped: sim.MaxCounter, EndedTicks: sim.MaxCounter, MaxTicks: sim.MaxCounter}
}

// marshalWidestSave encodes file with the marshalers of the saved orders and
// the saved records.
func marshalWidestSave(t *testing.T, file stateFile) []byte {
	t.Helper()
	indexes := newSavedIndexes(file)
	data, err := jsonv2.Marshal(file, jsonv2.Deterministic(true), jsonv2.WithMarshalers(jsonv2.JoinMarshalers(
		jsonv2.MarshalToFunc(indexes.encodeTrip), jsonv2.MarshalToFunc(indexes.encodeRequest),
		jsonv2.MarshalToFunc(encodeSavedFault), jsonv2.MarshalToFunc(encodeSavedEmergency))))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func widestPlainSavedState(t *testing.T) stateFile {
	t.Helper()
	id := widestID('x', 0)
	const widest = -sim.MaxCounter
	request := sim.SavedRequest{
		ID: widest, From: id, To: id, PodID: id, PartySize: sim.MaxCounter,
		Completed: true, RequestedTick: widest, BoardedTick: widest,
		SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService, DispatchReason: widestReason,
	}
	indexes := slices.Repeat([]int{project.MaxLanes - 1}, sim.MaxSavedRouteLanes)
	pod := sim.SavedPod{
		Class: sim.LegacyClass, ID: id, Activity: "continuing", StationID: id, BerthID: id,
		Occupied: true, RelocatingTo: id, Rebalancing: true, RebalanceAfter: widest,
		PhaseTicks: widest, Origin: id, Destination: id, DestinationStation: id,
		Riders:       slices.Repeat([]sim.SavedRequest{request}, sim.MaxSharedRideParties),
		RiddenMeters: plainProofFloat, ClaimsDestination: true, Released: true, ParkedSince: widest,
		Route: indexes, RouteIndex: widest, LaneID: id, LaneDistance: plainProofFloat,
		Distance: plainProofFloat, Waiting: true, WaitSince: widest,
		Platoon: &sim.SavedPlatoonLink{Leader: id, Lane: widest, LeaderLane: widest, Lanes: widest, Turn: plainProofFloat, Draining: true},
	}
	// Native boarding IDs exceed the saved index tuples in byte width.
	pod.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: id, MetersAtBoarding: plainProofFloat}}, sim.MaxSharedRideParties)
	trip := sim.SavedTrip{Request: request, Route: indexes, Boarded: true, DeferUntil: widest, DeferCheck: widest, DeferPodID: id}
	// Completed history does not consume the outstanding-order budget.
	// Keep every history slot and fill the queue. An active rider replaces
	// one history record and one queued trip, and can add at most one stop.
	// The queued trip costs more than that change, so no stops is the bound.
	trip.Request.Completed = false
	active := request
	active.Completed = false
	assertPlainOrderTradeoff(t, marshalSavedJSON(t, trip), marshalSavedJSON(t, request), marshalSavedJSON(t, active), marshalSavedJSON(t, []string{id}))
	state := sim.SavedState{}
	fillPlainProofScalars(reflect.ValueOf(&state).Elem())
	// addWidestSavedMembers sets the incident members.
	state.Demo = &sim.SavedDemo{SecondSent: true, FollowupsSent: true}
	state.Pods = slices.Repeat([]sim.SavedPod{pod}, project.MaxPods)
	// A restore can keep a cached route on a waiting trip that no pod is
	// bound to, so every waiting trip can have a route.
	state.Waiting = slices.Repeat([]sim.SavedTrip{trip}, maxSavedTrips)
	config := widestProofProject(t, "")
	demand := savedDemand{Budget: demandBudgetLimit - 1}
	fillPlainProofScalars(reflect.ValueOf(&demand.State).Elem())
	demand.State.Config.Seed = math.MaxUint64
	random, err := newDemand(demandInput{config: config.Demand, network: config.Network}).pcg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	demand.Random = random
	file := stateFile{
		Format: stateFormat, Version: stateVersion, Final: true,
		SavedAt: time.Date(2026, time.September, 23, 9, 0, 0, 123456789, time.FixedZone("", -12*60*60)),
		Build:   strings.Repeat("f", buildIDLength), Epoch: strings.Repeat("E", maxEpochBytes),
		Revision: sim.MaxCounter, ProjectRevision: sim.MaxCounter, Generation: sim.MaxCounter,
		LastCheckpoint: sim.MaxCounter, Speed: sim.MaxCounter, RestoreAttempts: sim.MaxCounter,
		Demand: demand, Simulation: state, Project: config,
		Sequences: slices.Repeat([]savedSequence{{Client: strings.Repeat("c", maxClientBytes), Sequence: sim.MaxCounter}}, clientLimit),
		RailConnections: slices.Repeat([]rail.Connection{{Event: id, Passenger: sim.MaxCounter, RequestedTick: widest, From: id, To: id,
			RequestID: sim.MaxCounter, AlightedTick: widest, Outcome: "unserved", Reason: "restore-degraded"}}, project.MaxRailDeparturePassengers),
	}
	return file
}

// widestProofProject returns a project of exactly project.MaxFileBytes
// bytes with the order contract.
func widestProofProject(t *testing.T, contract sim.OrderContract) project.Config {
	t.Helper()
	c := project.Default()
	c.OrderContract = contract
	c.Network.Nodes = make([]sim.Node, project.MaxNodes)
	for i := range c.Network.Nodes {
		c.Network.Nodes[i] = sim.Node{ID: widestID('n', i)}
	}
	c.Network.Lanes = make([]sim.Lane, project.MaxLanes)
	for i := range c.Network.Lanes {
		c.Network.Lanes[i] = sim.Lane{ID: widestID('l', i), From: c.Network.Nodes[i%project.MaxNodes].ID, To: c.Network.Nodes[(i+1)%project.MaxNodes].ID}
	}
	c.Network.Stations = make([]sim.Station, project.MaxStations)
	for i := range c.Network.Stations {
		c.Network.Stations[i] = sim.Station{ID: widestID('s', i)}
	}
	c.Fleet = slices.Repeat([]sim.Placement{{ID: widestID('p', 0), StationID: widestID('s', 0), BerthID: widestID('b', 0)}}, project.MaxPods)
	// Padding fills the independent project cap without relying on geometry.
	// The long name is a size bound, not a project validation fixture.
	c.Name = ""
	c.Name = strings.Repeat("n", project.MaxFileBytes-len(marshalSavedJSON(t, c)))
	if size := len(marshalSavedJSON(t, c)); size != project.MaxFileBytes {
		t.Fatalf("project has %d bytes, want %d", size, project.MaxFileBytes)
	}
	return c
}

func TestPlainStreamMaximumEncoding(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("plain byte proofs run without -short or -race, in test:embedded")
	}
	f := widestPlainStreamFrame(t)
	e := StreamEnvelope{Kind: "full", Stream: strings.Repeat("x", maxEpochBytes), Sequence: sim.MaxCounter, Source: sourceOf(f), Build: f.State.Build, Full: &f}
	data, err := EncodeStreamJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	apart := widestPlainStreamRecordBytes(t)
	t.Logf("full stream: %d encoded bytes, %d bytes of records measured apart", len(data), apart)
	assertPlainByteCap(t, "full stream", len(data)+apart, MaxStreamJSON)
	assertHTTPByteCap(t, f, apart, "")
}

// widestPlainStreamRecordBytes measures the widest faults and emergencies
// members of a frame. All vehicles of the widest frame have one ID, and the
// frame check allows one record for each pod and 64 debris records, so these
// records cannot pass the encoder. The proof adds their bytes to the total.
// Each record has the members of both kinds, and the widest ID and ticks.
func widestPlainStreamRecordBytes(t *testing.T) int {
	t.Helper()
	id := widestID('p', 0)
	from, to, tick := plainProofFloat, plainProofFloat, int64(-sim.MaxCounter)
	fault := sim.FaultView{ID: "i18446744073709551615.18446744073709551615", Kind: sim.FaultKindDebris, PodID: id, LaneID: id, FromMeters: &from, ToMeters: &to,
		Phase: sim.FaultPhaseEvacuated, StartTick: tick, EndTick: tick, EvacuateTick: &tick}
	faults := sim.FaultsView{Active: slices.Repeat([]sim.FaultView{fault}, maxFaultRecords), Counters: widestFaultCounters()}
	emergency := sim.EmergencyView{ID: fault.ID, PodID: id, OrderID: sim.MaxCounter, Phase: sim.EmergencyPhaseUnloading, StartTick: tick}
	emergencies := sim.EmergenciesView{Active: slices.Repeat([]sim.EmergencyView{emergency}, sim.MaxEmergencies), Counters: widestEmergencyCounters()}
	faultBytes, err := jsonv2.Marshal(faults)
	if err != nil {
		t.Fatal(err)
	}
	emergencyBytes, err := jsonv2.Marshal(emergencies)
	if err != nil {
		t.Fatal(err)
	}
	return len(`"faults":,`) + len(faultBytes) + len(`"emergencies":,`) + len(emergencyBytes)
}

func widestPlainStreamFrame(t *testing.T) StreamFrame {
	t.Helper()
	f := StreamFrame{}
	fillPlainProofScalars(reflect.ValueOf(&f.State).Elem())
	// The widest counters pass the encoder, and the records are measured
	// apart. A frame with operationsOff has none of these members.
	f.State.Simulation.OperationsOff = false
	f.State.Simulation.Interrupted, f.State.Simulation.InterruptedPassengers = sim.MaxCounter, sim.MaxCounter
	f.State.Demand.Config.Seed = math.MaxUint64
	f.State.ServerStart, f.State.Epoch = strings.Repeat("x", maxDemandErrorBytes), strings.Repeat("x", maxDemandErrorBytes)
	id := widestID('p', 0)
	c := &f.State.Demand.Config
	c.Destination, c.Profile, c.Band = id, id, id
	vehicle := widestVehicle()
	vehicle.Pod.Position = sim.Point{X: plainProofFloat, Y: plainProofFloat}
	vehicle.Pod.LaneDistance, vehicle.Pod.Speed = plainProofFloat, plainProofFloat
	vehicle.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: id, MetersAtBoarding: plainProofFloat}}, sim.MaxSharedRideParties)
	pending := vehicle.Riders[0]
	for i := range vehicle.Riders {
		vehicle.Riders[i].Completed = true
	}
	vehicle.Stops = nil
	vehicle.Withdrawn, vehicle.Operational = 3, sim.OperationalEmergencyUnload
	escaped := strings.Repeat("\x01", project.MaxIDLength)
	pending.LegFrom = escaped
	for i := range vehicle.Riders {
		vehicle.Riders[i].LegFrom = escaped
	}
	// As in the saved-state proof, keep history outside the outstanding
	// budget. Replacing it with an active rider and a stop costs less than
	// the pending order that must leave the full queue.
	encode := func(value any) []byte {
		data, err := jsonv2.Marshal(value, json.DefaultOptionsV1(), packedRequestOptions())
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	assertPlainOrderTradeoff(t, encode(pending), encode(vehicle.Riders[0]), encode(pending), encode([]string{id}))
	f.State.Simulation.Pending = slices.Repeat([]sim.Request{pending}, maxSavedTrips)
	f.State.Simulation.Vehicles = slices.Repeat([]VehicleFrame{vehicle}, project.MaxPods)
	// The largest distinct indexes also have the widest decimal form.
	indexes := make([]int, sim.MotionRouteLimit)
	for i := range indexes {
		indexes[i] = project.MaxLanes - len(indexes) + i
	}
	f.Routes = slices.Repeat([]sim.RoutePresentation{{Identity: sim.MaxCounter, Display: indexes, Lanes: indexes,
		Origin: project.MaxNodes - 1, Start: sim.MaxCounter, Current: sim.MaxCounter, Before: true, After: true}}, project.MaxPods)
	// Each berth has a unique node, so MaxNodes bounds the total berth count.
	f.State.Simulation.Berths = slices.Repeat([]sim.BerthState{{ID: id, Occupant: id, ReservedBy: id}}, project.MaxNodes)
	f.State.Build = strings.Repeat("f", buildIDLength)
	f.State.Checkpoints = slices.Repeat([]Checkpoint{{ID: sim.MaxCounter, Tick: -sim.MaxCounter, RestoresProject: true}}, checkpointLimit)
	return f
}

// assertPlainOrderTradeoff pins the byte inequality used by both fixtures.
// Stops name the distinct destinations of active riders. Charging each stop
// its own array and member header overestimates a shared stops array.
func assertPlainOrderTradeoff(t *testing.T, queued, history, active, stop []byte) {
	t.Helper()
	removed := len(queued) + 1 // Include the queue element's comma.
	added := max(0, len(active)-len(history)) + len(`,"stops":`) + len(stop)
	if removed < added {
		t.Fatalf("order-budget proof needs %d removed bytes to cover %d added bytes", removed, added)
	}
}

// assertHTTPByteCap bounds the HTTP state of frame under the order
// contract. apart is the number of bytes of the records that frame does not
// hold.
func assertHTTPByteCap(t *testing.T, frame StreamFrame, apart int, contract sim.OrderContract) {
	t.Helper()
	topology := TopologySnapshot{ProjectVersion: project.CurrentVersion, OrderContract: contract, ServerStart: frame.State.ServerStart, Epoch: frame.State.Epoch}
	topologyBytes, err := jsonv2.Marshal(topology, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	data, err := jsonv2.Marshal(StateEnvelope{OrderContract: contract, Topology: topology, Frame: frame}, json.DefaultOptionsV1(), packedRequestOptions())
	if err != nil {
		t.Fatal(err)
	}
	// Replace the small topology with its independent admission cap.
	// This bounds every accepted topology, including HTML name escapes.
	assertPlainByteCap(t, "HTTP state", len(data)-len(topologyBytes)+MaxTopologyJSON+apart, MaxHTTPStateJSON)
}

func assertPlainByteCap(t *testing.T, label string, size, limit int) {
	t.Helper()
	t.Logf("%s: %d JSON bytes, limit %d, headroom %d", label, size, limit, limit-size)
	if size > limit {
		t.Fatalf("%s exceeds its byte cap by %d bytes", label, size-limit)
	}
}

// fillPlainProofScalars leaves the order contract, the fault members, and the
// emergency members absent. The builders set the members. Containers
// get their explicit count bounds in the builders. Control bytes bound text
// escapes, and negative numbers include the widest sign and numeric value.
func fillPlainProofScalars(v reflect.Value) {
	switch v.Type() {
	case reflect.TypeFor[sim.OrderContract](), reflect.TypeFor[sim.FaultsView](), reflect.TypeFor[sim.EmergenciesView]():
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for _, field := range v.Fields() {
			fillPlainProofScalars(field)
		}
	case reflect.String:
		v.SetString(strings.Repeat("\x01", maxDemandErrorBytes))
	case reflect.Int, reflect.Int64:
		v.SetInt(-sim.MaxCounter)
	case reflect.Uint64:
		v.SetUint(sim.MaxCounter)
	case reflect.Float64:
		v.SetFloat(plainProofFloat)
	case reflect.Bool:
		v.SetBool(true)
	default:
		// Containers and optional pointers have explicit fixtures.
	}
}
