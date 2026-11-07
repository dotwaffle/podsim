package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// These fixtures bound Express state under the express-v1 order contract.
// They start from the plain fixtures, so each member that Express does not
// change keeps its widest plain value. Express changes four bounds: at most
// sim.MaxExpressPods pods, sim.MaxExpressWaitingTrips pending orders, and
// sim.MaxExpressParties riders and boardings for each pod. A waiting trip
// keeps a route only when it names a pod, and no two such trips name one
// pod, so at most sim.MaxExpressPods waiting trips have a route. Each order
// chooses an Express service with the widest service ID.

// widestExpressRequest returns request with an Express service, the widest
// service ID, and the wider of the two consents in the encoding of encode.
func widestExpressRequest[T any](request T, set func(*T, sim.SharingConsent), encode func(T) []byte) T {
	best, width := request, -1
	for _, consent := range []sim.SharingConsent{sim.PrivateConsent, sim.SharedConsent} {
		candidate := request
		set(&candidate, consent)
		if size := len(encode(candidate)); size > width {
			best, width = candidate, size
		}
	}
	return best
}

func setSavedExpressRequest(request *sim.SavedRequest, consent sim.SharingConsent) {
	request.SharingConsent, request.Service, request.ServiceID = consent, sim.ExpressServiceChoice, widestID('v', 0)
}

func TestExpressStateFileWorstCaseSize(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("Express byte proofs run without -short or -race, in test:embedded")
	}
	file := widestExpressSavedState(t)
	addWidestSavedMembers(t, &file)
	data := marshalWidestSave(t, file)
	operational := len(`,"operational":[1,2,4294967295]`) * sim.MaxExpressPods
	t.Logf("Express saved state: %d encoded bytes, %d operational tuple bytes", len(data), operational)
	assertPlainByteCap(t, "Express saved state", len(data)+operational, MaxStateBytes)
}

func widestExpressSavedState(t *testing.T) stateFile {
	t.Helper()
	file := widestPlainSavedState(t)
	state := &file.Simulation
	encode := func(request sim.SavedRequest) []byte { return marshalSavedJSON(t, request) }
	pod := state.Pods[0]
	pod.Class = sim.ExpressClass
	request := widestExpressRequest(pod.Riders[0], setSavedExpressRequest, encode)
	pod.Riders = slices.Repeat([]sim.SavedRequest{request}, sim.MaxExpressParties)
	pod.Boardings = slices.Repeat(pod.Boardings[:1], sim.MaxExpressParties)
	state.Pods = slices.Repeat([]sim.SavedPod{pod}, sim.MaxExpressPods)
	routed := state.Waiting[0]
	routed.Request = widestExpressRequest(routed.Request, setSavedExpressRequest, encode)
	queued := routed
	queued.Route = nil
	// As in the plain proof, completed history fills every rider slot and
	// the queue is full. An active rider replaces one history record and
	// one queued trip. A trip without a route is the smallest trip that
	// can leave the queue.
	active := request
	active.Completed = false
	assertPlainOrderTradeoff(t, marshalSavedJSON(t, queued), marshalSavedJSON(t, request), marshalSavedJSON(t, active), marshalSavedJSON(t, []string{pod.ID}))
	state.Waiting = slices.Concat(slices.Repeat([]sim.SavedTrip{routed}, sim.MaxExpressPods),
		slices.Repeat([]sim.SavedTrip{queued}, sim.MaxExpressWaitingTrips-sim.MaxExpressPods))
	file.Project = widestProofProject(t, sim.ExpressOrderContract)
	file.OrderContract, state.OrderContract = sim.ExpressOrderContract, sim.ExpressOrderContract
	return file
}

func setExpressRequest(request *sim.Request, consent sim.SharingConsent) {
	request.SharingConsent, request.Service, request.ServiceID = consent, sim.ExpressServiceChoice, widestID('v', 0)
}

// TestExpressStreamMaximumEncoding bounds the full stream, the HTTP state,
// and a replacement delta of the widest Express frame. Each request has a
// dispatch reason of sim.MaxExpressDispatchReasonBytes, the longest that an
// Express restore accepts and the simulation writes.
func TestExpressStreamMaximumEncoding(t *testing.T) {
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("Express byte proofs run without -short or -race, in test:embedded")
	}
	f := widestExpressStreamFrame(t)
	e := StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: strings.Repeat("x", maxEpochBytes), Sequence: sim.MaxCounter, Source: sourceOf(f), Build: f.State.Build, Full: &f}
	data, err := EncodeStreamJSON(e)
	if err != nil {
		t.Fatalf("%v: %d bytes", err, len(data))
	}
	apart := widestPlainStreamRecordBytes(t)
	t.Logf("Express full stream: %d encoded bytes, %d bytes of records measured apart", len(data), apart)
	assertPlainByteCap(t, "Express full stream", len(data)+apart, MaxStreamJSON)
	assertHTTPByteCap(t, f, apart, sim.ExpressOrderContract)
	// The widest delta replaces each vehicle member, each berth, and each
	// group, from a frame of the same chain in which all of them differ.
	before := f
	before.Routes = make([]sim.RoutePresentation, len(f.Routes))
	before.State.Checkpoints, before.State.Simulation.Pending = nil, nil
	before.State.Simulation.Vehicles = make([]VehicleFrame, len(f.State.Simulation.Vehicles))
	for i, vehicle := range f.State.Simulation.Vehicles {
		before.State.Simulation.Vehicles[i].Pod.ID = vehicle.Pod.ID
	}
	before.State.Simulation.Berths = make([]sim.BerthState, len(f.State.Simulation.Berths))
	for i, berth := range f.State.Simulation.Berths {
		before.State.Simulation.Berths[i].ID = berth.ID
	}
	before.State.Simulation.Tick++
	delta, err := makeDelta(before, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Vehicles) != len(f.State.Simulation.Vehicles) || len(delta.Berths) != len(f.State.Simulation.Berths) {
		t.Fatalf("delta replaces %d vehicles and %d berths", len(delta.Vehicles), len(delta.Berths))
	}
	e = StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: e.Stream, Sequence: sim.MaxCounter, Base: sim.MaxCounter, Source: sourceOf(f), Build: f.State.Build, Delta: &delta}
	data, err = EncodeStreamJSON(e)
	if err != nil {
		t.Fatalf("%v: %d bytes", err, len(data))
	}
	assertPlainByteCap(t, "Express delta", len(data)+apart, MaxStreamJSON)
}

func widestExpressStreamFrame(t *testing.T) StreamFrame {
	t.Helper()
	f := widestPlainStreamFrame(t)
	f.State.Simulation.OrderContract = sim.ExpressOrderContract
	encode := func(request sim.Request) []byte {
		data, err := jsonv2.Marshal(request, json.DefaultOptionsV1(), packedRequestOptions())
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	reason := strings.Repeat("r", sim.MaxExpressDispatchReasonBytes)
	vehicle := f.State.Simulation.Vehicles[0]
	vehicle.Pod.Class = sim.ExpressClass
	history := widestExpressRequest(vehicle.Riders[0], setExpressRequest, encode)
	history.DispatchReason = reason
	vehicle.Riders = slices.Repeat([]sim.Request{history}, sim.MaxExpressParties)
	vehicle.Boardings = slices.Repeat(vehicle.Boardings[:1], sim.MaxExpressParties)
	pending := widestExpressRequest(f.State.Simulation.Pending[0], setExpressRequest, encode)
	pending.DispatchReason = reason
	active := history
	active.Completed = false
	stop, err := jsonv2.Marshal([]string{widestID('p', 0)}, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	assertPlainOrderTradeoff(t, encode(pending), encode(history), encode(active), stop)
	f.State.Simulation.Pending = slices.Repeat([]sim.Request{pending}, sim.MaxExpressWaitingTrips)
	f.State.Simulation.Vehicles = slices.Repeat([]VehicleFrame{vehicle}, sim.MaxExpressPods)
	f.Routes = f.Routes[:sim.MaxExpressPods]
	return f
}
