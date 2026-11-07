package session

import (
	"slices"
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
