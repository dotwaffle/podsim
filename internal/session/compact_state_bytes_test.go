package session

import (
	"bytes"
	"encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"

	"github.com/dotwaffle/podsim/internal/sim"
)

// compactClassWorstCaseFile returns base with maxSavedPods typed pods of
// the compact class, and maxSavedTrips trips. Each pod is a copy of pod,
// with its platoon link, and each trip a copy of trip. Only the first
// maxSavedPods trips keep their route. Independent maxima bound bytes.
// They do not describe reachable placement.
func compactClassWorstCaseFile(base stateFile, pod sim.SavedPod, trip sim.SavedTrip) stateFile {
	const longestNegative = -0.0000010000000000000002
	pod.Class = sim.CompactClass
	pod.RiddenMeters, pod.LaneDistance, pod.Distance = longestNegative, longestNegative, longestNegative
	file := base
	file.Simulation.PassengerDistanceMeters, file.Simulation.EmptyDistanceMeters = longestNegative, longestNegative
	file.Simulation.RiderDistanceMeters, file.Simulation.DirectDistanceMeters, file.Simulation.MaxDetourRatio = longestNegative, longestNegative, longestNegative
	file.Simulation.Pods = make([]sim.SavedPod, maxSavedPods)
	for i := range file.Simulation.Pods {
		file.Simulation.Pods[i] = pod
		file.Simulation.Pods[i].ID = widestID('p', i)
	}
	file.Simulation.Waiting = make([]sim.SavedTrip, maxSavedTrips)
	for i := range file.Simulation.Waiting {
		file.Simulation.Waiting[i] = trip
		if i >= maxSavedPods {
			file.Simulation.Waiting[i].Route = nil
		}
	}
	return file
}

// testCompactClassWorstCaseSize encodes the full typed current-profile
// save shape. Independent maxima bound bytes. They do not describe
// reachable placement.
func testCompactClassWorstCaseSize(t *testing.T, base stateFile, pod sim.SavedPod, trip sim.SavedTrip) {
	t.Helper()
	t.Run("compact-class", func(t *testing.T) {
		file := compactClassWorstCaseFile(base, pod, trip)
		if len(file.Simulation.Waiting) != sim.MaxSavedWaitingTrips {
			t.Fatal("byte fixture differs from the native waiting bound")
		}
		for _, saved := range file.Simulation.Pods {
			if len(saved.Riders) != sim.MaxSharedRideParties || len(saved.Stops) != sim.MaxSharedRideParties {
				t.Fatal("byte fixture differs from the native stored-rider bound")
			}
		}
		raw := marshalSavedJSON(t, file)
		t.Logf("typed compact class: %d JSON bytes, limit %d, headroom %d", len(raw), MaxStateBytes, MaxStateBytes-len(raw))
		if len(raw) > MaxStateBytes {
			t.Fatal("typed compact save exceeds the byte cap")
		}
		// Use the operating array limits, including 2,600 records and eight riders.
		if scanErr := prescanJSON(raw, stateJSONLimits); scanErr != nil {
			t.Fatalf("typed compact operating shape: %v", scanErr)
		}
		assertExplicitArrayBounds(t, "typed compact save maximum", raw, savedLimits(contractMarkers{}))
		data := encodeTestState(t, file)
		if encoded := decompressTestJSON(t, data); !bytes.Equal(encoded, raw) {
			t.Fatal("typed compact fixture differs from the bounded state encoder")
		}
		testBoardingWorstCaseSize(t, file)
		decoded, err := decodeStateFile(data)
		if err != nil || len(decoded.Simulation.Pods) != maxSavedPods || len(decoded.Simulation.Waiting) != sim.MaxSavedWaitingTrips {
			t.Fatalf("typed compact save decode: %v", err)
		}
	})
}

// testBoardingWorstCaseSize sends modern and mixed maxima through the save adapter.
func testBoardingWorstCaseSize(t *testing.T, base stateFile) {
	t.Helper()
	const wide = 0.0000010000000000000002
	for _, mixed := range []bool{false, true} {
		name := "modern"
		if mixed {
			name = "mixed"
		}
		t.Run(name, func(t *testing.T) {
			file := base
			file.Project = project.Clone(base.Project)
			stationID := base.Simulation.Pods[0].Riders[0].From
			berths := make([]sim.Berth, project.MaxBerths)
			for i := range berths {
				berths[i] = sim.Berth{ID: widestID('b', i), Node: file.Project.Network.Nodes[i].ID}
			}
			file.Project.Network.Stations[0].ID = stationID
			file.Project.Network.Stations[0].Berths = berths
			file.Project.Network.Stations[0].Banks = nil
			file.Project.Name = ""
			file.Project.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, file.Project))
			file.Simulation.Pods = slices.Clone(base.Simulation.Pods)
			for i := range file.Simulation.Pods {
				if mixed && i%2 == 0 {
					continue
				}
				pod := &file.Simulation.Pods[i]
				pod.Riders = slices.Clone(pod.Riders)
				pod.RiddenMeters = wide
				for j := range pod.Riders {
					pod.Riders[j].SharingConsent = sim.SharedConsent
					if pod.Riders[j].Completed {
						pod.Riders[j].SharingConsent = sim.PrivateConsent
					}
					pod.Riders[j].PartySize = 4
				}
				pod.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: berths[len(berths)-1].ID, MetersAtBoarding: wide}}, sim.MaxSharedRideParties)
			}
			data := encodeTestState(t, file)
			raw := decompressTestJSON(t, data)
			t.Logf("typed boarding %s compact class: %d JSON bytes, limit %d, headroom %d", name, len(raw), MaxStateBytes, MaxStateBytes-len(raw))
			if len(raw) > MaxStateBytes || len(file.Simulation.Pods) != 300 || len(file.Simulation.Waiting) != 2600 {
				t.Fatal("boarding byte maximum exceeded an unchanged cap")
			}
			if err := prescanJSON(raw, boardingStateLimits(stateJSONLimits)); err != nil {
				t.Fatal("boarding operating shape", err)
			}
			assertExplicitArrayBounds(t, "boarding save maximum", raw, savedLimits(contractMarkers{}))
			decoded, err := decodeStateFile(data)
			if err != nil {
				t.Fatal("boarding maximum decode", err)
			}
			if err := decoded.resolveBoardings(); err != nil {
				t.Fatal("boarding maximum source binding", err)
			}
			for i, pod := range file.Simulation.Pods {
				if len(pod.Riders) != 8 || !slices.Equal(decoded.Simulation.Pods[i].Boardings, pod.Boardings) {
					t.Fatal("boarding maximum lost aligned rider history")
				}
			}
			if !mixed {
				// A direct native-ID encoding writes each boarding record
				// with its berth ID and the order text without packing.
				// When IDs could hold control bytes, it exceeded the save
				// cap. Each ID now has only ID characters, so it fits the
				// cap, and the boarding adapter is headroom.
				bypass, err := json.Marshal(file, json.Deterministic(true))
				if err != nil || len(bypass) > MaxStateBytes {
					t.Fatalf("direct native-ID fixture changed: bytes=%d error=%v", len(bypass), err)
				}
				t.Logf("direct native-ID maximum: %d JSON bytes, headroom %d, adapter %d bytes", len(bypass), MaxStateBytes-len(bypass), len(raw))
			}
		})
	}
}
