package session

import (
	"crypto/sha256"
	"encoding/json/v2"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// londonQualificationBuild builds the London qualification network. The
// narrower berth pitch is above the generator's 25 m minimum. It fits three
// passenger berths without the hard layout conflicts of a 75 m pitch. Bank
// retains two berths. The three parking sites are depots of 4 lanes of 8
// slots each. With compact, the eight Compact stations of the preset stay;
// without it, the stations are ordinary.
func londonQualificationBuild(compact bool) (project.Config, error) {
	options := scenarios.DefaultLondonFullOptions()
	options.StationPods, options.ParkingPods = 1, 1
	options.ParkingLanes, options.ParkingBerths, options.BerthPitch = 4, 8, 37.5
	if !compact {
		options.CompactStations = nil
	}
	for id, count := range options.Berths {
		if id != "940GZZLUBNK" && count < 3 {
			options.Berths[id] = 3
		}
	}
	return scenarios.LondonFullWith(options)
}

var (
	londonQualificationBase = sync.OnceValues(func() (project.Config, error) { return londonQualificationBuild(true) })
	londonOrdinaryBase      = sync.OnceValues(func() (project.Config, error) { return londonQualificationBuild(false) })
)

// londonQualificationProject returns the London qualification project with
// pods pods, with the Compact stations of the preset and a Compact fleet.
func londonQualificationProject(tb testing.TB, pods int) project.Config {
	tb.Helper()
	return londonFleetProject(tb, londonQualificationBase, pods)
}

// londonOrdinaryProject returns the London qualification project with
// ordinary stations and a fleet of the default class, so that each pod can
// carry sim.MaxSharedRideParties parties.
func londonOrdinaryProject(tb testing.TB, pods int) project.Config {
	tb.Helper()
	return londonFleetProject(tb, londonOrdinaryBase, pods)
}

func londonFleetProject(tb testing.TB, build func() (project.Config, error), pods int) project.Config {
	tb.Helper()
	base, err := build()
	if err != nil {
		tb.Fatal(err)
	}
	config := project.Clone(base)
	// heads holds the slots of each depot heads first, the order of the
	// depot placements of the preset: the first slot of each lane, then
	// the second slot of each lane, and so on.
	heads := make(map[string][]string)
	for _, depot := range config.Network.Depots {
		for slot := range len(depot.Lanes[0].BerthIDs) {
			for _, lane := range depot.Lanes {
				heads[depot.Station] = append(heads[depot.Station], lane.BerthIDs[slot])
			}
		}
	}
	// With Compact stations, each pod is Compact.
	var class sim.VehicleClass
	if len(config.Network.ManeuverGroups) > 0 {
		class = sim.CompactClass
	}
	var full []sim.Placement
	firstParking := true
	for _, station := range config.Network.Stations {
		count := 3
		switch {
		case station.ID == "940GZZLUBNK":
			count = 2
		case station.ParkingOnly:
			count = 24
			if firstParking {
				count, firstParking = 26, false
			}
		}
		for i := range count {
			full = append(full, sim.Placement{
				ID: fmt.Sprintf("qualification-pod-%04d", len(full)+1), StationID: station.ID, BerthID: station.Berths[i].ID, Class: class,
			})
		}
	}
	if len(full) != 1000 || pods < 1 || pods > len(full) {
		tb.Fatalf("qualification fleet %d, requested %d", len(full), pods)
	}
	// A smaller fleet samples the full fleet evenly. The pods of a depot
	// then take its slots heads first, so no lane has a gap ahead of a pod.
	config.Fleet = make([]sim.Placement, pods)
	taken := make(map[string]int)
	for i := range config.Fleet {
		pod := full[i*len(full)/pods]
		if slots, ok := heads[pod.StationID]; ok {
			pod.BerthID = slots[taken[pod.StationID]]
			taken[pod.StationID]++
		}
		config.Fleet[i] = pod
	}
	config.Demand.Enabled = true
	return config
}

func TestLondonFleetQualificationTopology(t *testing.T) {
	t.Parallel()
	var network []byte
	for _, pods := range []int{327, 600, 1000} {
		config := londonQualificationProject(t, pods)
		encoded, err := json.Marshal(config.Network, json.Deterministic(true))
		if err != nil {
			t.Fatal(err)
		}
		if network == nil {
			network = encoded
		}
		if !slices.Equal(encoded, network) || len(config.Fleet) != pods {
			t.Fatalf("%d-pod case changed topology or fleet size", pods)
		}
		if pods <= project.MaxPods {
			if err := project.Validate(config); err != nil {
				t.Fatal(err)
			}
		}
		// No depot lane has an empty slot ahead of a pod.
		taken := make(map[string]bool)
		for _, pod := range config.Fleet {
			taken[pod.BerthID] = true
		}
		for _, depot := range config.Network.Depots {
			for _, lane := range depot.Lanes {
				for slot := 1; slot < len(lane.BerthIDs); slot++ {
					if taken[lane.BerthIDs[slot]] && !taken[lane.BerthIDs[slot-1]] {
						t.Fatalf("%d-pod case: depot slot %s is empty ahead of a pod", pods, lane.BerthIDs[slot-1])
					}
				}
			}
		}
	}
}

type londonQualificationMetrics struct {
	Pods          int     `json:"pods"`
	PerMinute     int     `json:"perMinute"`
	Network       string  `json:"network"`
	MeanMS        float64 `json:"meanMS"`
	P99MS         float64 `json:"p99MS"`
	MaximumMS     float64 `json:"maximumMS"`
	AllocPerTick  uint64  `json:"allocPerTick"`
	PeakHeap      uint64  `json:"peakSampledHeap"`
	SavedGzip     int     `json:"savedGzipBytes"`
	HTTPBytes     int     `json:"httpBytes"`
	PublishedGzip int     `json:"publishedGzipBytes"`
	Publications  int     `json:"publications"`
	Demoted       int     `json:"restoreDemoted"`
	Moved         int     `json:"movedAfterRestore"`
	Completed     int     `json:"completed"`
	Pending       int     `json:"pending"`
}

// TestLondonFleetQualification is opt-in because it runs six long traffic
// cases. Normal tests still check that every case has the same valid layout.
func TestLondonFleetQualification(t *testing.T) {
	if os.Getenv("PODSIM_QUALIFY_LONDON") != "1" {
		t.Skip("run mise run qualify:london for matched fleet qualification")
	}
	if testing.Short() || raceEnabled {
		t.Fatal("London fleet qualification requires full non-race tests")
	}
	for _, pods := range []int{327, 600, 1000} {
		for _, rate := range []int{10, 120} {
			t.Run(fmt.Sprintf("pods-%d-rate-%d", pods, rate), func(t *testing.T) {
				qualifyLondonFleet(t, pods, rate)
			})
		}
	}
}

func qualifyLondonFleet(t *testing.T, pods, rate int) {
	t.Helper()
	config := londonQualificationProject(t, pods)
	config.Demand.PerMinute = rate
	shared, err := NewWithProject(config, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	network, err := json.Marshal(config.Network, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	metrics := londonQualificationMetrics{Pods: pods, PerMinute: rate, Network: fmt.Sprintf("%x", sha256.Sum256(network))}
	check := func(current *Session) {
		if _, checkErr := current.simulation.SafetyObservation().Check(); checkErr != nil {
			t.Fatal(checkErr)
		}
		if checkErr := current.simulation.CheckContract(); checkErr != nil {
			t.Fatal(checkErr)
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		metrics.PeakHeap = max(metrics.PeakHeap, memory.HeapAlloc)
	}
	for tick := range 600 * sim.TicksPerSecond {
		shared.step()
		if tick%sim.TicksPerSecond == 0 {
			check(shared)
		}
	}
	const measuredTicks = 180 * sim.TicksPerSecond
	times := make([]time.Duration, measuredTicks)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var elapsed time.Duration
	var checkAllocated uint64
	for tick := range measuredTicks {
		start := time.Now()
		shared.step()
		duration := time.Since(start)
		times[tick] = duration
		elapsed += duration
		if tick%sim.TicksPerSecond == 0 {
			var checkBefore, checkAfter runtime.MemStats
			runtime.ReadMemStats(&checkBefore)
			check(shared)
			runtime.ReadMemStats(&checkAfter)
			checkAllocated += checkAfter.TotalAlloc - checkBefore.TotalAlloc
		}
	}
	runtime.ReadMemStats(&after)
	slices.Sort(times)
	metrics.MeanMS = float64(elapsed) / float64(measuredTicks) / float64(time.Millisecond)
	metrics.P99MS = float64(times[measuredTicks*99/100]) / float64(time.Millisecond)
	metrics.MaximumMS = float64(times[len(times)-1]) / float64(time.Millisecond)
	metrics.AllocPerTick = (after.TotalAlloc - before.TotalAlloc - checkAllocated) / measuredTicks
	if budget := 1000.0 / sim.TicksPerSecond; metrics.MeanMS >= budget || metrics.P99MS >= budget {
		t.Fatalf("ticks exceed the %.3f ms real-time budget: mean %.3f, p99 %.3f", budget, metrics.MeanMS, metrics.P99MS)
	}
	check(shared)
	state := shared.simulation.Snapshot()
	if state.Completed == 0 || state.Submitted <= state.Completed {
		t.Fatalf("qualification lacks completed and active work: %d/%d", state.Completed, state.Submitted)
	}
	metrics.Completed, metrics.Pending = state.Completed, len(state.Pending)
	topology := shared.Topology()
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	httpState, err := EncodeStateJSON(topology, frame)
	if err != nil {
		t.Fatal(err)
	}
	view, err := DecodeStateJSON(httpState)
	if err != nil || view.Simulation.Completed != state.Completed {
		t.Fatalf("HTTP client round trip: completed %d, error %v", view.Simulation.Completed, err)
	}
	metrics.HTTPBytes = len(httpState)
	assembler, err := NewStreamAssembler(topology)
	if err != nil {
		t.Fatal(err)
	}
	previous, stream, sequence := StreamFrame{}, "qualification", uint64(0)
	for publication := range 101 {
		current, frameErr := shared.presentationFrame()
		if frameErr != nil {
			t.Fatal(frameErr)
		}
		envelope := StreamEnvelope{Kind: "full", Stream: stream, Sequence: uint64(publication + 1), Source: sourceOf(current), Build: current.State.Build, Full: &current}
		if publication > 0 {
			delta, deltaErr := makeDelta(previous, current)
			if deltaErr != nil {
				t.Fatal(deltaErr)
			}
			envelope.Kind, envelope.Base, envelope.Full, envelope.Delta = "delta", sequence, nil, &delta
		}
		compressed, encodeErr := encodeStream(envelope)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		inflated, inflateErr := InflateStream(compressed)
		if inflateErr != nil {
			t.Fatal(inflateErr)
		}
		decoded, decodeErr := DecodeStreamJSON(inflated)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		applied, applyErr := ApplyStream(previous, stream, sequence, decoded)
		if applyErr != nil || !reflect.DeepEqual(applied, current) {
			t.Fatalf("publication %d differs after decode/apply: %v", publication, applyErr)
		}
		if _, stateErr := assembler.State(applied); stateErr != nil {
			t.Fatal(stateErr)
		}
		previous, sequence = applied, envelope.Sequence
		metrics.PublishedGzip += len(compressed)
		metrics.Publications++
		for range sim.TicksPerSecond / 20 {
			shared.step()
		}
	}
	file := sessionStateFile(t, shared)
	// The generic test helper uses one failed-start attempt. A production
	// final save clears it, so this trial must not force logical restore.
	file.RestoreAttempts = 0
	compressed := encodeTestState(t, file)
	metrics.SavedGzip = len(compressed)
	restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: compressed}, Project: &config, Options: []Option{WithLogger(slog.New(slog.DiscardHandler))}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	if restored.restore.Tier != "physical" || restored.restore.Dropped != 0 || restored.restore.Unaccounted != 0 {
		t.Fatalf("physical restore failed: %+v", restored.restore)
	}
	metrics.Demoted = restored.restore.Demoted
	restoreStart := restored.simulation.SafetyObservation()
	for tick := range 60 * sim.TicksPerSecond {
		restored.step()
		if tick%sim.TicksPerSecond == 0 {
			check(restored)
		}
	}
	if got := restored.simulation.Snapshot(); got.Submitted < file.Simulation.RequestID || got.Completed < file.Simulation.Completed {
		t.Fatal("continued operation lost request accounting")
	}
	for i, pod := range restored.simulation.SafetyObservation().Pods {
		if pod.Position != restoreStart.Pods[i].Position {
			metrics.Moved++
		}
	}
	if metrics.Moved == 0 {
		t.Fatal("no pod moved after physical restore")
	}
	raw, err := json.Marshal(metrics, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LONDON_QUALIFICATION %s", raw)
}
