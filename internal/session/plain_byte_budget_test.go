package session

import "testing"

// TestPlainProofHistoryBudget pins the distinction the byte proof needs:
// completed history can coexist with a full queue, but active riders cannot.
func TestPlainProofHistoryBudget(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("restore budget proof runs without -short, in test:race:session")
	}
	config, data := maximalRequeueState(t, 0)
	file := decodeTestState(t, data)
	history := 0
	for i := range file.Simulation.Pods {
		pod := &file.Simulation.Pods[i]
		pod.Activity, pod.Stops = "idle", nil
		for j := range pod.Riders {
			pod.Riders[j].Completed = true
			history++
		}
	}
	file.Simulation.Completed = history
	trip := file.Simulation.Waiting[0]
	for len(file.Simulation.Waiting) < maxSavedTrips {
		file.Simulation.RequestID++
		next := trip
		next.Request.ID = file.Simulation.RequestID
		file.Simulation.Waiting = append(file.Simulation.Waiting, next)
	}
	seed := encodeTestState(t, file)
	for _, test := range []struct {
		name   string
		active bool
		extra  bool
		stop   bool
	}{
		{name: "completed history with full queue"},
		{name: "active rider at budget", active: true},
		{name: "active rider above budget", active: true, extra: true},
		{name: "history without active rider cannot have stops", stop: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate := decodeTestState(t, seed)
			if test.active {
				pod := &candidate.Simulation.Pods[0]
				pod.Activity, pod.Occupied = "traveling", true
				pod.Origin, pod.StationID, pod.BerthID = pod.BerthID, "", ""
				pod.Riders[0].Completed = false
				pod.Riders[0].To = trip.Request.To
				pod.DestinationStation = pod.Riders[0].To
				pod.Stops = []string{pod.DestinationStation}
				candidate.Simulation.Completed--
				if !test.extra {
					candidate.Simulation.Waiting = candidate.Simulation.Waiting[:maxSavedTrips-1]
				}
			}
			if test.stop {
				pod := &candidate.Simulation.Pods[0]
				pod.Activity = "traveling"
				pod.Origin, pod.StationID, pod.BerthID = pod.BerthID, "", ""
				pod.DestinationStation, pod.RelocatingTo = trip.Request.To, trip.Request.To
				pod.Stops = []string{trip.Request.To}
			}
			store := &fakeStore{data: encodeTestState(t, candidate)}
			shared := startFromStore(t, StoreInput{Store: store, Project: &config})
			state := shared.State()
			if test.extra || test.stop {
				if state.Restore.Tier != "empty" || state.Restore.Reason != reasonInvalidState {
					t.Fatalf("invalid order/stop restore = %+v, want invalid_state", state.Restore)
				}
				return
			}
			if state.Restore.Tier != "logical" || state.Restore.Dropped != 0 || len(state.Simulation.Pending) != maxSavedTrips {
				t.Fatalf("restore = %+v, pending %d, want logical restore with %d pending orders", state.Restore, len(state.Simulation.Pending), maxSavedTrips)
			}
		})
	}
}
