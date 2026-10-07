package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

// arrayPaths returns the path of each array of data, in the form of the
// limit tables.
func arrayPaths(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	paths := map[string]bool{}
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return paths
		}
		if err != nil {
			t.Fatal(err)
		}
		if token.Kind() == jsontext.KindBeginArray {
			paths[arrayPath(decoder)] = true
		}
	}
}

// boundsProject gives config one element in each project array that the
// example projects leave empty. The values need not form a valid project,
// because the save decoder does not validate the project.
func boundsProject(t *testing.T, config project.Config) project.Config {
	t.Helper()
	// withBankMetadata returns a detached clone with station banks.
	config = withBankMetadata(config)
	network := &config.Network
	station := network.Stations[0]
	classes, err := sim.NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	if network.Lanes[0].VehicleClasses == 0 {
		network.Lanes[0].VehicleClasses = classes
	}
	if network.Stations[0].VehicleClasses == 0 {
		network.Stations[0].VehicleClasses = classes
	}
	if network.Stations[0].Berths[0].VehicleClasses == 0 {
		network.Stations[0].Berths[0].VehicleClasses = classes
	}
	config.DemandProfiles = []project.DemandProfile{{
		ID: "profile", Name: "Profile", Bands: []project.DemandBand{{ID: "band", Name: "Band", DurationMinutes: 60}},
		Flows: []project.DemandFlow{{From: station.ID, To: network.Stations[1].ID, Weights: []float64{1}}},
	}}
	config.RailArrivals = []project.RailArrival{{ID: "arrival", Station: station.ID, Passengers: 1, Destinations: []project.RailDestination{{Station: network.Stations[1].ID, Weight: 1}}}}
	config.RailDepartures = []project.RailDeparture{{ID: "departure", Station: station.ID, Passengers: 1, Origins: []project.RailOrigin{{Station: network.Stations[1].ID, Weight: 1}}}}
	return config
}

// boundsSaveJSON returns the JSON form of a save with markers and at least
// one element in each array of the save format. It has the incident,
// fault and emergency markers.
func boundsSaveJSON(t *testing.T, markers contractMarkers) []byte {
	t.Helper()
	var file stateFile
	if markers.order == sim.ExpressOrderContract {
		file = sessionStateFile(t, expressSession(t))
		file.OrderContract = markers.order
	} else {
		file = newTestStateFile(t)
	}
	config := boundsProject(t, file.Project)
	config.IncidentContract = sim.IncidentV1Contract
	config.FaultContract, config.Faults = sim.FaultV1Contract, &project.FaultConfig{}
	config.EmergencyContract, config.Emergencies = sim.EmergencyV1Contract, &project.EmergencyConfig{}
	file.Project = config
	file.RailConnections = []rail.Connection{{Event: "arrival", Passenger: 1, From: config.Network.Stations[0].ID, To: config.Network.Stations[1].ID, Outcome: "unserved", Reason: "restore-degraded"}}
	file.Sequences = append(slices.Clone(file.Sequences), savedSequence{Client: "bounds", Sequence: 1})

	state := &file.Simulation
	station := config.Network.Stations[0]
	request := sim.SavedRequest{
		ID: state.RequestID + 1, From: station.ID, To: config.Network.Stations[1].ID, PartySize: 1, LegFrom: station.ID,
		SharingConsent: sim.SharedConsent, Service: sim.OnDemandService,
	}
	if markers.order == sim.ExpressOrderContract {
		request.Service, request.ServiceID = sim.ExpressServiceChoice, config.ExpressServices[0].ID
	}
	state.RequestID++
	state.Pods = slices.Clone(state.Pods)
	pod := &state.Pods[0]
	if pod.Class == sim.LegacyClass || pod.Class == "" {
		pod.Class = sim.CompactClass
	}
	rider := request
	rider.Completed, rider.PodID = true, pod.ID
	pod.Riders, pod.Stops = []sim.SavedRequest{rider}, []string{request.To}
	pod.Boardings = []sim.RiderBoarding{{BerthID: station.Berths[0].ID}}
	pod.Route = []int{0}
	pod.Withdrawn, pod.Purpose, pod.Owner = 3, 1, 2
	pod.Platoon = &sim.SavedPlatoonLink{Leader: state.Pods[len(state.Pods)-1].ID}
	state.Waiting = append(slices.Clone(state.Waiting), sim.SavedTrip{Request: request, Route: []int{0}, ExcludedPod: pod.ID})
	state.Faults = &sim.SavedFaults{Records: []sim.SavedFault{
		{Generation: 1, Serial: 1, Start: 1, End: 2, Pod: 0},
		{Generation: 1, Serial: 2, Start: 1, End: 2, Debris: true, Lane: 0, From: 0, To: 1},
	}}
	state.Emergencies = &sim.SavedEmergencies{Records: []sim.SavedEmergency{{Generation: 1, Serial: 3, Start: 1, Pod: 0, Order: 1}}}

	data := encodeTestState(t, file)
	if _, err := decodeStateFile(data); err != nil {
		t.Fatalf("the save with markers %+v: %v", markers, err)
	}
	return decompressTestJSON(t, data)
}

// changedDelta returns a delta that changes every vehicle, every berth and
// every group of frame. Its base has the markers of frame and empty
// vehicles, routes and berths.
func changedDelta(t *testing.T, frame StreamFrame) StreamDelta {
	t.Helper()
	_, empty := streamFixture(t)
	simulation := frame.State.Simulation
	empty.State.Simulation.OrderContract = simulation.OrderContract
	empty.State.Simulation.IncidentContract = simulation.IncidentContract
	empty.State.Simulation.FaultContract = simulation.FaultContract
	empty.State.Simulation.EmergencyContract = simulation.EmergencyContract
	empty.State.Simulation.Vehicles = make([]VehicleFrame, len(simulation.Vehicles))
	empty.State.Simulation.Berths = make([]sim.BerthState, len(simulation.Berths))
	empty.Routes = make([]sim.RoutePresentation, len(frame.Routes))
	delta, err := makeDelta(empty, frame)
	if err != nil {
		t.Fatal(err)
	}
	return delta
}

// boundsStreamJSON returns a full frame, a delta and an HTTP state with
// markers and at least one element in each array of the stream format.
// The plain documents have the incident, fault and emergency markers.
func boundsStreamJSON(t *testing.T, markers contractMarkers) map[string][]byte {
	t.Helper()
	var topology TopologySnapshot
	var frame StreamFrame
	if markers.order == sim.ExpressOrderContract {
		topology, frame = expressGuardFrame(t)
	} else {
		topology, _, frame = emergencyStreamFrames(t, true)
		vehicle := &frame.State.Simulation.Vehicles[0]
		vehicle.Pod.Class = sim.CompactClass
		vehicle.Boardings = make([]sim.RiderBoarding, len(vehicle.Riders))
		for i := range vehicle.Boardings {
			vehicle.Boardings[i] = sim.RiderBoarding{BerthID: vehicle.Pod.BerthID}
		}
	}
	simulation := &frame.State.Simulation
	vehicle := &simulation.Vehicles[0]
	request := vehicle.Riders[0]
	request.ID, request.Completed, request.PodID = sim.MaxCounter, false, ""
	simulation.Pending = append(slices.Clone(simulation.Pending), request)
	if len(vehicle.Stops) == 0 {
		vehicle.Stops = []string{request.To}
	}
	frame.State.Checkpoints = append(slices.Clone(frame.State.Checkpoints), Checkpoint{ID: 1, Tick: 1})
	frame.Routes = slices.Clone(frame.Routes)
	frame.Routes[0].Display, frame.Routes[0].Lanes = []int{0}, []int{0}

	full := StreamEnvelope{OrderContract: markers.order, Kind: "full", Stream: "bounds", Sequence: 2, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	delta := changedDelta(t, frame)
	changed := full
	changed.Kind, changed.Full, changed.Delta, changed.Base = "delta", nil, &delta, 1
	documents := map[string][]byte{}
	for _, envelope := range []StreamEnvelope{full, changed} {
		raw, err := EncodeStreamJSON(envelope)
		if err != nil {
			t.Fatalf("%s with markers %+v: %v", envelope.Kind, markers, err)
		}
		if _, err := DecodeStreamJSON(raw); err != nil {
			t.Fatalf("%s with markers %+v: %v", envelope.Kind, markers, err)
		}
		documents[envelope.Kind] = raw
	}
	if markers.order == "" {
		// The Express topology has the Express services and class sets.
		topology = boundsTopology(t, topology)
	}
	raw, err := jsonv2.Marshal(StateEnvelope{OrderContract: markers.order, Topology: topology, Frame: frame}, json.DefaultOptionsV1(), packedRequestOptions())
	if err != nil {
		t.Fatal(err)
	}
	var decoded StateEnvelope
	if _, err := decodeMarkedJSON(raw, &decoded); err != nil {
		t.Fatalf("HTTP state with markers %+v: %v", markers, err)
	}
	documents["HTTP"] = raw
	return documents
}

// boundsTopology gives topology the network of sim.BankExample, which has
// station banks, and a class set on a lane, a station and a berth.
func boundsTopology(t *testing.T, topology TopologySnapshot) TopologySnapshot {
	t.Helper()
	classes, err := sim.NewClassSet("compact")
	if err != nil {
		t.Fatal(err)
	}
	network := sim.BankExample()
	network.Lanes[0].VehicleClasses = classes
	for i := range network.Stations {
		if len(network.Stations[i].Banks) != 0 {
			network.Stations[i].VehicleClasses = classes
			network.Stations[i].Berths[0].VehicleClasses = classes
		}
	}
	topology.Network = network
	return topology
}

// TestFormatArraysHaveLimits checks that each array of a save, a full
// frame, a delta and an HTTP state has an explicit limit in the table of
// its decoder, so that the general element limit does not bound it. The
// fixtures are small. Each array has at least one element, and the
// fixtures together have each array of the tables.
func TestFormatArraysHaveLimits(t *testing.T) {
	t.Parallel()
	saved, stream := map[string]bool{}, map[string]bool{}
	for _, markers := range []contractMarkers{{}, {order: sim.ExpressOrderContract}} {
		name := "plain"
		if markers.order != "" {
			name = string(markers.order)
		}
		raw := boundsSaveJSON(t, markers)
		assertExplicitArrayBounds(t, name+" save", raw, savedLimits(markers))
		maps.Copy(saved, arrayPaths(t, raw))
		for kind, raw := range boundsStreamJSON(t, markers) {
			assertExplicitArrayBounds(t, name+" "+kind, raw, streamLimits(markers))
			maps.Copy(stream, arrayPaths(t, raw))
		}
	}
	for path := range savedLimits(contractMarkers{}).arrays {
		if !saved[path] {
			t.Errorf("no save fixture has the array %s", path)
		}
	}
	for path := range streamLimits(contractMarkers{}).arrays {
		// A vehicle has no complete route, and "/active" is the faults
		// group alone, without its envelope.
		if !stream[path] && !strings.HasSuffix(path, "/routeLaneIDs") && path != "/active" {
			t.Errorf("no stream fixture has the array %s", path)
		}
	}
}
