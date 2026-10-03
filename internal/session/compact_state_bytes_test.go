package session

import (
	"bytes"
	"encoding/json/v2"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// testCompactWorstCaseSize encodes the full typed current-profile save shape.
// Independent maxima bound bytes. They do not describe reachable placement.
func testCompactWorstCaseSize(t *testing.T, base stateFile, pod sim.SavedPod, trip sim.SavedTrip) {
	t.Helper()
	const longestNegative = -0.0000010000000000000002
	const longestNonnegative = 0.0000010000000000000002
	pod.Class = sim.CompactClass
	pod.RiddenMeters, pod.LaneDistance, pod.Distance = longestNegative, longestNegative, longestNegative
	base.Simulation.PassengerDistanceMeters, base.Simulation.EmptyDistanceMeters = longestNegative, longestNegative
	base.Simulation.RiderDistanceMeters, base.Simulation.DirectDistanceMeters, base.Simulation.MaxDetourRatio = longestNegative, longestNegative, longestNegative
	// All 26 symbols have six-byte escapes. Two symbols distinguish 300 IDs.
	alphabet := []byte{1, 2, 3, 4, 5, 6, 7, 11, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31}
	for count := 1; count <= 4; count++ {
		t.Run("compact-members-"+strconv.Itoa(count), func(t *testing.T) {
			file := base
			file.Simulation.Pods = make([]sim.SavedPod, maxSavedPods)
			for i := range file.Simulation.Pods {
				file.Simulation.Pods[i] = pod
				file.Simulation.Pods[i].ID = strings.Repeat("\x01", 62) + string([]byte{alphabet[i/len(alphabet)], alphabet[i%len(alphabet)]})
			}
			for first := 0; first < maxSavedPods; first += count {
				n := min(count, maxSavedPods-first)
				queue := &sim.SavedCompactQueue{
					Kind: "compact-buffer-v1", Phase: "recovering", Lane: strings.Repeat("\x01", 64),
					Start: longestNegative, Frontier: longestNegative,
					Members: make([]string, n), StopCells: make([]int, n), Speeds: make([]float64, n),
					Targets: make([]float64, n), LandingSpeeds: make([]float64, n),
				}
				for offset := range n {
					member := &file.Simulation.Pods[first+offset]
					queue.Members[offset], queue.StopCells[offset] = member.ID, math.MaxInt
					queue.Speeds[offset] = longestNonnegative
					queue.Targets[offset], queue.LandingSpeeds[offset] = longestNegative, longestNegative
					if offset == 0 {
						member.Platoon, member.CompactQueue = nil, queue
					} else {
						member.Platoon = &sim.SavedPlatoonLink{Kind: "compact-buffer-v1", Leader: file.Simulation.Pods[first+offset-1].ID,
							Lane: math.MaxInt, LeaderLane: math.MaxInt, Lanes: 1, TerminalCell: new(math.MaxInt)}
					}
				}
			}
			file.Simulation.Waiting = make([]sim.SavedTrip, maxSavedTrips)
			for i := range file.Simulation.Waiting {
				file.Simulation.Waiting[i] = trip
				if i >= maxSavedPods {
					file.Simulation.Waiting[i].Route = nil
				}
			}
			if len(file.Simulation.Waiting) != sim.MaxSavedWaitingTrips {
				t.Fatal("byte fixture differs from the native waiting bound")
			}
			for _, saved := range file.Simulation.Pods {
				if len(saved.Riders) != sim.MaxSharedRideParties || len(saved.Stops) != sim.MaxSharedRideParties {
					t.Fatal("byte fixture differs from the native stored-rider bound")
				}
			}
			raw, err := json.Marshal(file, json.Deterministic(true))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("typed compact members=%d: %d JSON bytes, limit %d, headroom %d", count, len(raw), MaxStateBytes, MaxStateBytes-len(raw))
			if len(raw) > MaxStateBytes {
				t.Fatal("typed compact save exceeds the byte cap")
			}
			// Use the operating array limits, including 2,600 records and eight riders.
			if scanErr := prescanJSON(raw, compactStateLimits(stateJSONLimits)); scanErr != nil {
				t.Fatalf("typed compact operating shape: %v", scanErr)
			}
			data := encodeTestState(t, file)
			if encoded := decompressTestJSON(t, data); !bytes.Equal(encoded, raw) {
				t.Fatal("typed compact fixture differs from the bounded state encoder")
			}
			decoded, err := decodeStateFile(data)
			if err != nil || len(decoded.Simulation.Pods) != maxSavedPods || len(decoded.Simulation.Waiting) != sim.MaxSavedWaitingTrips {
				t.Fatalf("typed compact save decode: %v", err)
			}
		})
	}
}
