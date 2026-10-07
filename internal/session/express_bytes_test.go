package session

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"math"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

// widestSavedBase combines independent field maxima, not reachable motion.
func widestSavedBase(t *testing.T) stateFile {
	t.Helper()
	const nodes, lanes = project.MaxNodes, project.MaxLanes
	id := func(prefix string, index int) string {
		return prefix + strings.Repeat("0", 64-len(prefix)-len(strconv.Itoa(index))) + strconv.Itoa(index)
	}
	config := project.Default()
	config.Network.Nodes = make([]sim.Node, nodes)
	for index := range config.Network.Nodes {
		config.Network.Nodes[index] = sim.Node{ID: id("n", index)}
	}
	config.Network.Lanes = make([]sim.Lane, lanes)
	for index := range config.Network.Lanes {
		config.Network.Lanes[index] = sim.Lane{ID: id("l", index), From: id("n", index%nodes), To: id("n", (index+1)%nodes)}
	}
	// The project member can hold project.MaxFileBytes, whatever it
	// contains, so a long name fills the rest.
	config.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, config)+len(config.Name))
	if size := jsonSize(t, config); size != project.MaxFileBytes {
		t.Fatalf("project has %d bytes, want %d", size, project.MaxFileBytes)
	}

	// Saved IDs and diagnostic text can contain control bytes. Each byte
	// then occupies six JSON bytes. The project member already fills its
	// independent byte cap, so its node and lane IDs can remain short.
	id = func(_ string, _ int) string { return strings.Repeat("\x01", 64) }
	const widest = -sim.MaxCounter
	text := strings.Repeat("\x01", 1<<10)
	route := func(length int) []int {
		indexes := make([]int, length)
		for index := range indexes {
			indexes[index] = lanes - 1
		}
		return indexes
	}
	request := sim.SavedRequest{
		ID: widest, From: id("f", 0), To: id("t", 0), PartySize: sim.MaxCounter, PodID: id("p", 0),
		Completed: true, RequestedTick: widest, BoardedTick: widest, DispatchReason: text,
		SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService,
	}
	riders, stops := make([]sim.SavedRequest, sim.MaxSharedRideParties), make([]string, sim.MaxSharedRideParties)
	for index := range riders {
		riders[index], stops[index] = request, id("t", index)
	}
	pod := sim.SavedPod{
		ID: id("p", 0), Class: sim.LegacyClass, Activity: "continuing", StationID: id("s", 0), BerthID: id("b", 0),
		Occupied: true, Riders: riders, Stops: stops, RiddenMeters: -math.MaxFloat64, JourneyOrigin: id("j", 0), RelocatingTo: id("r", 0),
		Rebalancing: true, RebalanceAfter: widest, PhaseTicks: widest, Origin: id("o", 0),
		Destination: id("d", 0), DestinationStation: id("e", 0), ClaimsDestination: true, Released: true,
		Route: route(lanes + nodes), RouteIndex: widest, LaneID: id("l", 0),
		LaneDistance: -math.MaxFloat64, Distance: -math.MaxFloat64, Waiting: true, WaitSince: widest,
		Platoon: &sim.SavedPlatoonLink{
			Leader: id("p", 1), Lane: widest, LeaderLane: widest, Lanes: widest, Turn: -math.MaxFloat64, Draining: true,
		},
	}
	trip := sim.SavedTrip{
		Request: request, Route: route(nodes), Boarded: true,
		DeferUntil: widest, DeferCheck: widest, DeferPodID: id("p", 0),
	}
	trip.Request.SharingConsent = sim.PrivateConsent
	// A client ID of control characters has the longest JSON form, 6 bytes
	// for each byte. The last 3 bytes make the IDs increase.
	sequences := make([]savedSequence, clientLimit)
	for index := range sequences {
		suffix := string([]byte{byte(0x10 + index/256), byte(0x10 + index/16%16), byte(0x10 + index%16)})
		sequences[index] = savedSequence{
			Client: strings.Repeat("\x01", maxClientBytes-len(suffix)) + suffix, Sequence: sim.MaxCounter,
		}
	}
	demand := config.Demand
	demand.Enabled, demand.PerMinute, demand.Seed = true, 120, math.MaxUint64
	demand.Pattern = "rail-services"
	// The conservative bound includes the optional daily clock, even
	// though rail-services does not accept that field in a valid project.
	demand.DailyStartMinute = 1439
	demand.Destination, demand.Profile, demand.Band = id("d", 0), id("p", 0), id("b", 0)
	random, err := newDemand(demandInput{config: demand, network: config.Network}).pcg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	connections := make([]rail.Connection, project.MaxRailDeparturePassengers)
	for i := range connections {
		connections[i] = rail.Connection{Event: id("e", i), Passenger: 200, RequestedTick: widest, From: id("f", i), To: id("t", i), RequestID: sim.MaxCounter, AlightedTick: widest, Outcome: "unserved", Reason: "restore-degraded"}
	}

	file := stateFile{
		RailConnections: connections,
		Format:          stateFormat, Version: stateVersion, Final: true,
		SavedAt: time.Date(2026, time.September, 23, 9, 0, 0, 123456789, time.FixedZone("", -12*60*60)),
		Build:   testBuildID, Epoch: strings.Repeat("\x01", maxEpochBytes),
		Revision: sim.MaxCounter - 1, ProjectRevision: sim.MaxCounter - 1, Generation: sim.MaxCounter - 1,
		LastCheckpoint: sim.MaxCounter - 1, Speed: 60, RestoreAttempts: sim.MaxCounter, Sequences: sequences,
		Demand: savedDemand{
			State:  DemandState{Config: demand, Generated: sim.MaxCounter, Skipped: sim.MaxCounter, Error: text, Connections: rail.Counts{Made: 10000, Missed: 10000, Unserved: 10000, Unresolved: 10000}},
			Random: random, Budget: demandBudgetLimit - 1,
		},
		Simulation: sim.SavedState{
			Tick: widest, Paused: true, Completed: widest, RequestID: widest, Boarded: widest,
			TotalWaitTicks: widest, MaxWaitTicks: widest, NextRedistributionTick: widest,
			PassengerDistanceMeters: -math.MaxFloat64, EmptyDistanceMeters: -math.MaxFloat64,
			RebalanceMoves: widest, SharedParties: widest, SharedRidePartyLimit: widest,
			SharedRideMode: sim.SharedRideMode(text), SharedRideMaxStops: widest,
			Journeys: widest, TotalJourneyTicks: widest, MaxJourneyTicks: widest,
			RiderDistanceMeters: -math.MaxFloat64, DirectDistanceMeters: -math.MaxFloat64, MaxDetourRatio: -math.MaxFloat64,
			Demo: &sim.SavedDemo{SecondSent: true, FollowupsSent: true}, DemoError: text,
			Pods: []sim.SavedPod{pod}, Waiting: []sim.SavedTrip{trip},
		},
		Project: config,
	}

	return file
}

func exportExpressAsset(t *testing.T, name string, data []byte) {
	t.Helper()
	if dir := os.Getenv("PODSIM_EXPRESS_ASSET_DIR"); dir != "" {
		if err := os.WriteFile(dir+"/"+name, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// widestExpressSave returns the widest Express save: 300 Express pods with
// 20 riders and 20 boarding records each, and sim.MaxExpressWaitingTrips
// trips. It combines independent field maxima, not reachable motion.
func widestExpressSave(t *testing.T) stateFile {
	t.Helper()
	const wide = 0.0000010000000000000002
	base := widestSavedBase(t)
	base.OrderContract = sim.ExpressOrderContract
	base.Project.OrderContract = sim.ExpressOrderContract
	base.Simulation.OrderContract = sim.ExpressOrderContract
	classes, err := sim.NewClassSet("express", "compact", "group")
	if err != nil {
		t.Fatal(err)
	}
	from, to := strings.Repeat("\x01", 64), strings.Repeat("\x02", 64)
	berths := make([]sim.Berth, project.MaxBerths)
	for i := range berths {
		berths[i] = sim.Berth{ID: fmt.Sprintf("%059s%05d", strings.Repeat("\x01", 59), i), Node: base.Project.Network.Nodes[i].ID, VehicleClasses: classes}
	}
	base.Project.Network.Stations = []sim.Station{{ID: from, Name: "source", Entry: base.Project.Network.Nodes[0].ID, Exit: base.Project.Network.Nodes[1].ID, Berths: berths, VehicleClasses: classes}, {ID: to, Name: "target", Entry: base.Project.Network.Nodes[2].ID, Exit: base.Project.Network.Nodes[3].ID, Berths: []sim.Berth{{ID: "target", Node: base.Project.Network.Nodes[2].ID, VehicleClasses: classes}}, VehicleClasses: classes}}
	for i := range base.Project.Network.Lanes {
		base.Project.Network.Lanes[i].VehicleClasses = classes
	}
	base.Project.Fleet = make([]sim.Placement, 300)
	pod := base.Simulation.Pods[0]
	pod.Class = sim.ExpressClass
	pod.Platoon = nil
	pod.RiddenMeters, pod.Distance, pod.LaneDistance = wide, wide, -wide
	pod.Riders = slices.Repeat(pod.Riders[:1], 20)
	pod.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: berths[199].ID, MetersAtBoarding: wide}}, 20)
	for i := range pod.Riders {
		r := &pod.Riders[i]
		r.From, r.To = from, to
		r.PartySize = 20
		r.SharingConsent, r.Service = sim.SharedConsent, sim.ExpressServiceChoice
		r.ServiceID = strings.Repeat("\x03", 64)
	}
	pod.StationID, pod.BerthID = from, berths[199].ID
	base.Simulation.Pods = make([]sim.SavedPod, 300)
	for i := range base.Simulation.Pods {
		p := pod
		p.ID = fmt.Sprintf("%059s%05d", strings.Repeat("\x01", 59), i)
		base.Simulation.Pods[i] = p
		base.Project.Fleet[i] = sim.Placement{ID: p.ID, Class: p.Class, StationID: from, BerthID: berths[i%200].ID}
	}
	base.Project.ExpressServices = make([]sim.ExpressService, 300)
	for i := range base.Project.ExpressServices {
		base.Project.ExpressServices[i] = sim.ExpressService{ID: fmt.Sprintf("%059s%05d", strings.Repeat("\x03", 59), i), From: from, To: to, Class: sim.ExpressClass, PartyLimit: 20}
	}
	trip := base.Simulation.Waiting[0]
	trip.Request = pod.Riders[0]
	trip.Request.Completed = false
	base.Simulation.Waiting = make([]sim.SavedTrip, sim.MaxExpressWaitingTrips)
	for i := range base.Simulation.Waiting {
		trip.Request.ID = i + 1
		trip.Request.PodID = base.Simulation.Pods[i%300].ID
		base.Simulation.Waiting[i] = trip
		if i >= 300 {
			base.Simulation.Waiting[i].Route = nil
		}
	}
	base.Project.Name = ""
	base.Project.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, base.Project))
	base.Simulation.PassengerDistanceMeters, base.Simulation.EmptyDistanceMeters = -wide, -wide
	base.Simulation.RiderDistanceMeters, base.Simulation.DirectDistanceMeters, base.Simulation.MaxDetourRatio = -wide, -wide, -wide
	return base
}

func TestExpressWidestSaveAdapters(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("maximum codec proof runs in the required test:embedded task")
	}
	base := widestExpressSave(t)
	for _, mixed := range []bool{false, true} {
		file := base
		file.Simulation.Pods = slices.Clone(base.Simulation.Pods)
		name := "modern"
		if mixed {
			name = "mixed"
			p := &file.Simulation.Pods[0]
			p.Class = sim.CompactClass
			p.Riders = p.Riders[:8]
			p.Boardings = p.Boardings[:8]
			file.Project.Fleet = slices.Clone(base.Project.Fleet)
			file.Project.Fleet[0].Class = sim.CompactClass
		}
		started := time.Now()
		data := encodeTestState(t, file)
		raw := decompressTestJSON(t, data)
		if len(raw) > MaxStateBytes || len(data) > MaxStateBytes {
			t.Fatal("save cap exceeded", len(raw), len(data))
		}
		assertExplicitArrayBounds(t, "Express save "+name, raw, savedLimits(contractMarkers{order: sim.ExpressOrderContract}))
		decoded, err := decodeStateFile(data)
		if err != nil {
			t.Fatal(err)
		}
		if err = decoded.resolveBoardings(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(raw, decompressTestJSON(t, encodeTestState(t, decoded))) {
			t.Fatal("maximum save reencode changed")
		}
		t.Logf("asset save-%s raw=%d gzip=%d elapsed=%s", name, len(raw), len(data), time.Since(started))
		exportExpressAsset(t, "save-"+name+".json", raw)
		exportExpressAsset(t, "save-"+name+".json.gz", data)
		runtime.GC()
	}
}

// widestExpressStreamFrame returns maximumStreamFrame with the Express
// marker, sim.MaxExpressWaitingTrips pending orders, and 300 Express pods
// with 20 riders and 20 boarding records each.
func widestExpressStreamFrame(t *testing.T) StreamFrame {
	t.Helper()
	frame := maximumStreamFrame(t)
	frame.State.Simulation.OrderContract = sim.ExpressOrderContract
	request := frame.State.Simulation.Pending[0]
	request.From, request.To = strings.Repeat("\x01", 64), strings.Repeat("\x02", 64)
	request.PartySize = 20
	request.SharingConsent, request.Service = sim.SharedConsent, sim.ExpressServiceChoice
	request.ServiceID = strings.Repeat("\x03", 64)
	frame.State.Simulation.Pending = slices.Repeat([]sim.Request{request}, 8600)
	for i := range frame.State.Simulation.Vehicles {
		v := &frame.State.Simulation.Vehicles[i]
		v.Pod.Class = sim.ExpressClass
		v.PlatoonID, v.PlatoonIndex = "", 0
		v.RiddenMeters = 0.0000010000000000000002
		r := request
		r.Completed = true
		v.Riders = slices.Repeat([]sim.Request{r}, 20)
		v.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: strings.Repeat("\x01", 64), MetersAtBoarding: v.RiddenMeters}}, 20)
	}
	return frame
}

// maximumStreamDelta returns a delta that changes every vehicle, every
// berth and every group of frame. Its base has the markers of frame and
// empty vehicles, routes and berths.
func maximumStreamDelta(t *testing.T, frame StreamFrame) StreamDelta {
	t.Helper()
	_, empty := streamFixture(t)
	empty.State.Simulation.OrderContract = frame.State.Simulation.OrderContract
	empty.State.Simulation.IncidentContract = frame.State.Simulation.IncidentContract
	empty.State.Simulation.FaultContract = frame.State.Simulation.FaultContract
	empty.State.Simulation.Vehicles = make([]VehicleFrame, len(frame.State.Simulation.Vehicles))
	empty.State.Simulation.Berths = make([]sim.BerthState, len(frame.State.Simulation.Berths))
	empty.Routes = make([]sim.RoutePresentation, len(frame.Routes))
	delta, err := makeDelta(empty, frame)
	if err != nil {
		t.Fatal(err)
	}
	return delta
}

func TestExpressWidestStreamAdapters(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("maximum codec proof runs in the required test:embedded task")
	}
	frame := widestExpressStreamFrame(t)
	full := StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: strings.Repeat("x", 32), Sequence: sim.MaxCounter, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	delta := maximumStreamDelta(t, frame)
	changed := full
	changed.Kind, changed.Full, changed.Delta, changed.Base = "delta", nil, &delta, sim.MaxCounter-1
	for _, envelope := range []StreamEnvelope{full, changed} {
		started := time.Now()
		raw, err := EncodeStreamJSON(envelope)
		if err != nil {
			t.Fatal(err)
		}
		compressed, err := encodeStream(envelope)
		if err != nil {
			t.Fatal(err)
		}
		inflated, err := InflateStream(compressed)
		if err != nil || !bytes.Equal(raw, inflated) {
			t.Fatal("stream gzip changed", err)
		}
		if scanErr := prescanJSON(inflated, streamLimits(contractMarkers{order: sim.ExpressOrderContract})); scanErr != nil {
			t.Fatal("maximum stream failed the bounded scan", scanErr)
		}
		assertExplicitArrayBounds(t, "Express "+envelope.Kind, inflated, streamLimits(contractMarkers{order: sim.ExpressOrderContract}))
		decoded, err := DecodeStreamJSON(inflated)
		if err != nil {
			t.Fatal(err)
		}
		reencoded, err := EncodeStreamJSON(decoded)
		if err != nil || !bytes.Equal(raw, reencoded) {
			t.Fatal("maximum stream semantic reencode changed", err)
		}
		t.Logf("asset %s raw=%d gzip=%d elapsed=%s", envelope.Kind, len(raw), len(compressed), time.Since(started))
		exportExpressAsset(t, envelope.Kind+".json", raw)
		exportExpressAsset(t, envelope.Kind+".json.gz", compressed)
		runtime.GC()
	}
}

// widestTopology returns a topology of the largest network, with IDs that
// contain escapes control bytes. markers select the Express services. The
// HTTP topology cap bounds the member whatever it contains.
func widestTopology(t *testing.T, escapes int, markers contractMarkers) TopologySnapshot {
	t.Helper()
	id := func(prefix string, i int) string {
		width := escapes
		if prefix == "s" || prefix == "b" || prefix == "e" || prefix == "l" && i == 0 {
			width = 58
		}
		return prefix + strings.Repeat("\x01", width) + strings.Repeat("x", 58-width) + fmt.Sprintf("%05d", i)
	}
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	topology := TopologySnapshot{ProjectVersion: project.CurrentVersion, OrderContract: markers.order, ServerStart: "server", Epoch: "epoch", ProjectRevision: sim.MaxCounter}
	topology.Network.Nodes = make([]sim.Node, 5000)
	for i := range topology.Network.Nodes {
		topology.Network.Nodes[i] = sim.Node{ID: id("n", i), Position: sim.Point{X: float64(i) * 40, Y: 0.0000010000000000000002}}
	}
	topology.Network.Stations = make([]sim.Station, 300)
	next := 0
	for i := range topology.Network.Stations {
		count := 16
		if i < 200 {
			count++
		}
		station := &topology.Network.Stations[i]
		*station = sim.Station{ID: id("s", i), Name: strings.Repeat("\x01", 80), VehicleClasses: classes, Berths: make([]sim.Berth, count)}
		for j := range station.Berths {
			station.Berths[j] = sim.Berth{ID: id("b", next), Node: id("n", next), SeparationGroup: id("r", next), VehicleClasses: classes}
			next++
		}
		station.Entry, station.Exit = station.Berths[0].Node, station.Berths[len(station.Berths)-1].Node
	}
	topology.Network.Lanes = make([]sim.Lane, 8000)
	for i := range topology.Network.Lanes {
		topology.Network.Lanes[i] = sim.Lane{ID: id("l", i), From: id("n", i%5000), To: id("n", (i+1)%5000), SpeedLimit: 2.5, SeparationGroup: id("r", i), StationID: topology.Network.Stations[0].ID, StationRole: sim.StationBerthAccessRole, VehicleClasses: classes}
	}
	if markers.order == sim.ExpressOrderContract {
		topology.ExpressServices = make([]sim.ExpressService, 300)
		for i := range topology.ExpressServices {
			topology.ExpressServices[i] = sim.ExpressService{ID: id("e", i), From: topology.Network.Stations[0].ID, To: topology.Network.Stations[1].ID, Class: sim.ExpressClass, PartyLimit: 20}
		}
	}
	return topology
}

// fitWidestTopology returns the widest topology of markers whose JSON form
// fits the HTTP topology cap, and the first escape count that does not fit.
func fitWidestTopology(t *testing.T, markers contractMarkers) (TopologySnapshot, int) {
	t.Helper()
	var topology TopologySnapshot
	lo, hi := 0, 58
	for lo <= hi {
		mid := (lo + hi) / 2
		candidate := widestTopology(t, mid, markers)
		raw, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) <= project.MaxFileBytes+4096 {
			topology = candidate
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return topology, lo
}

// These topology and HTTP assets combine independent bounded fields.
// Their parser acceptance does not qualify physical placement or motion.
func TestExpressWidestTopologyHTTPAdapters(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("maximum codec proof runs in the required test:embedded task")
	}
	express := contractMarkers{order: sim.ExpressOrderContract}
	topology, lo := fitWidestTopology(t, express)
	raw, err := json.Marshal(topology)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TopologySnapshot
	if err = json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	oversized := widestTopology(t, lo, express)
	large, err := json.Marshal(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if len(large) <= project.MaxFileBytes+4096 || json.Unmarshal(large, &decoded) == nil {
		t.Fatal("topology cap failed")
	}
	if err = preflightTopology(project.Config{Version: project.CurrentVersion, OrderContract: sim.ExpressOrderContract, Network: oversized.Network, ExpressServices: oversized.ExpressServices}, "server", "epoch"); err == nil || err.Error() != "topology exceeds supported limit" {
		t.Fatal("producer topology preflight accepted overflow", err)
	}
	t.Logf("asset topology raw=%d cap=%d next-step-overflow=%d", len(raw), project.MaxFileBytes+4096, len(large))
	exportExpressAsset(t, "topology.json", raw)
	frame := maximumStreamFrame(t)
	frame.State.ServerStart, frame.State.Epoch, frame.State.ProjectRevision = topology.ServerStart, topology.Epoch, topology.ProjectRevision
	frame.State.Simulation.OrderContract = sim.ExpressOrderContract
	request := sim.Request{ID: sim.MaxCounter, From: topology.ExpressServices[0].From, To: topology.ExpressServices[0].To, PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: topology.ExpressServices[0].ID, RequestedTick: sim.MaxCounter, BoardedTick: sim.MaxCounter, DispatchReason: strings.Repeat("\x01", 1024), PodID: fmt.Sprintf("%059s%05d", strings.Repeat("\x01", 59), 0)}
	frame.State.Simulation.Pending = slices.Repeat([]sim.Request{request}, 8600)
	frame.State.Simulation.Berths = make([]sim.BerthState, 0, 5000)
	for _, station := range topology.Network.Stations {
		for _, berth := range station.Berths {
			frame.State.Simulation.Berths = append(frame.State.Simulation.Berths, sim.BerthState{ID: berth.ID, Occupant: request.PodID, ReservedBy: request.PodID})
		}
	}
	display := make([]int, 8000)
	for i := range display {
		display[i] = i
	}
	motion := make([]int, 2048)
	for i := range motion {
		motion[i] = i
	}
	for i := range frame.State.Simulation.Vehicles {
		rider := request
		rider.Completed = true
		podID := fmt.Sprintf("%059s%05d", strings.Repeat("\x01", 59), i)
		rider.PodID = podID
		frame.State.Simulation.Vehicles[i] = VehicleFrame{Pod: sim.Pod{ID: podID, Class: sim.ExpressClass, Activity: sim.Traveling, LaneID: topology.Network.Lanes[0].ID, Position: sim.Point{X: -0.0000010000000000000002, Y: 0.0000010000000000000002}, StationID: topology.Network.Stations[0].ID, BerthID: topology.Network.Stations[0].Berths[0].ID, BlockedBy: request.PodID, ManeuverStationID: topology.Network.Stations[0].ID, Speed: math.MaxFloat64, LaneDistance: 0.0000010000000000000002}, Riders: slices.Repeat([]sim.Request{rider}, 20), Boardings: slices.Repeat([]sim.RiderBoarding{{BerthID: topology.Network.Stations[0].Berths[0].ID, MetersAtBoarding: 0.0000010000000000000002}}, 20), RiddenMeters: 0.0000010000000000000002, Stops: slices.Repeat([]string{request.To}, 8), RelocatingTo: request.To, Rebalancing: true}
		frame.Routes[i] = sim.RoutePresentation{Identity: sim.MaxCounter, Display: display, Origin: 0, Lanes: motion, Start: 0, Current: 0, After: true}
	}
	frame.State.Revision = 1
	frame.State.Simulation.Tick = sim.MaxCounter
	full := StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "widest-reference-shape", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	fullRaw, err := EncodeStreamJSON(full)
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeErr := DecodeStreamJSON(fullRaw); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	assertExplicitArrayBounds(t, "Express reference full", fullRaw, streamLimits(contractMarkers{order: sim.ExpressOrderContract}))
	exportExpressAsset(t, "reference-full.json", fullRaw)
	t.Logf("asset reference-full raw=%d", len(fullRaw))
	// HTTP state conversion rejects a speed that is not a playback choice.
	frame.State.Speed = 60
	started := time.Now()
	httpRaw, err := EncodeStateJSON(topology, frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(httpRaw) > MaxStreamJSON {
		t.Fatal("HTTP cap exceeded")
	}
	assertExplicitArrayBounds(t, "Express HTTP state", httpRaw, streamLimits(contractMarkers{order: sim.ExpressOrderContract}))
	state, err := DecodeStateJSON(httpRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Simulation.Pending) != 8600 || len(state.Simulation.Vehicles) != 300 || state.Simulation.Pending[0].RequestedTick != sim.MaxCounter {
		t.Fatal("HTTP maximum lost native fields")
	}
	gzipData, err := compressStreamJSON(httpRaw)
	if err != nil {
		t.Fatal(err)
	}
	inflated, err := InflateStream(gzipData)
	if err != nil || !bytes.Equal(inflated, httpRaw) {
		t.Fatal("HTTP gzip changed", err)
	}
	t.Logf("asset http raw=%d gzip=%d elapsed=%s", len(httpRaw), len(gzipData), time.Since(started))
	exportExpressAsset(t, "http.json", httpRaw)
	exportExpressAsset(t, "http.json.gz", gzipData)
	var packed StateEnvelope
	if err = jsonv2.Unmarshal(httpRaw, &packed, json.DefaultOptionsV1(), packedDecodeOptions()); err != nil {
		t.Fatal(err)
	}
	if packed.Frame.State.Simulation.Pending[0].From != request.From {
		t.Fatal("native HTTP text changed")
	}
}
