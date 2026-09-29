package session

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStreamLondonWire(t *testing.T) {
	config := scenarios.LondonCentral()
	config.Demand.Enabled = true
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	// Warm the same simulation without transport or clock-status snapshots.
	step := func() { shared.simulation.Step(); shared.demand.step(shared.simulation); shared.revision++ }
	for range 20 * 60 * sim.TicksPerSecond {
		step()
	}
	previous, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := NewStreamAssembler(shared.Topology())
	if err != nil {
		t.Fatal(err)
	}
	var httpBytes, deltaBytes int
	var httpTime, deltaTime time.Duration
	const publications = 100
	for i := range publications {
		for range sim.TicksPerSecond / 20 {
			step()
		}
		started := time.Now()
		frame := shared.Frame()
		raw, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		baseline, err := compressStreamJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		httpTime += time.Since(started)
		httpBytes += len(baseline)
		started = time.Now()
		current, err := shared.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		d, err := makeDelta(previous, current)
		if err != nil {
			t.Fatal(err)
		}
		e := StreamEnvelope{Kind: "delta", Stream: "measurement", Sequence: uint64(i + 2), Base: uint64(i + 1), Source: sourceOf(current), Build: current.State.Build, Delta: &d}
		compressed, err := encodeStream(e)
		if err != nil {
			t.Fatal(err)
		}
		deltaTime += time.Since(started)
		deltaBytes += len(compressed)
		if _, err = assembler.State(current); err != nil {
			t.Fatalf("London publication %d: %v", i, err)
		}
		previous = current
	}
	if deltaBytes > httpBytes {
		t.Fatalf("London bandwidth regression: delta %d > HTTP %d", deltaBytes, httpBytes)
	}
	t.Logf("London 20 min warmup, %d publications: HTTP gzip %d B/s, delta gzip %d B/s, HTTP capture+encode %s/publication, delta capture+diff+encode %s/publication", publications, httpBytes*20/publications, deltaBytes*20/publications, httpTime/publications, deltaTime/publications)
}

// TestStreamLargeRouteWire measures codec work with 200 pods and 8,000-lane
// routes. It excludes simulation stepping and topology transfer.
func TestStreamLargeRouteWire(t *testing.T) {
	_, previous := streamFixture(t)
	previous.State.Simulation.Vehicles = make([]VehicleFrame, 200)
	previous.Routes = make([]sim.RoutePresentation, 200)
	indexes := make([]int, 8000)
	laneIDs := make([]string, 8000)
	for i := range indexes {
		indexes[i] = i
		laneIDs[i] = fmt.Sprintf("synthetic-lane-%04d", i)
	}
	for i := range previous.Routes {
		previous.State.Simulation.Vehicles[i].Pod.ID = fmt.Sprintf("pod-%03d", i)
		previous.Routes[i] = sim.RoutePresentation{Identity: 1, Display: indexes, Lanes: indexes[:2048], Current: 1024, After: true}
	}
	var fullBytes, deltaBytes int
	var fullTime, deltaTime time.Duration
	const publications = 20
	for n := range publications {
		current := previous
		current.State.Revision++
		current.State.Simulation.Vehicles = slices.Clone(previous.State.Simulation.Vehicles)
		for i := range current.State.Simulation.Vehicles {
			current.State.Simulation.Vehicles[i].Pod.Position.X += 0.125
		}
		started := time.Now()
		legacy := current.State
		legacy.Simulation.Vehicles = slices.Clone(current.State.Simulation.Vehicles)
		for i := range legacy.Simulation.Vehicles {
			legacy.Simulation.Vehicles[i].RouteLaneIDs = laneIDs
		}
		raw, err := json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		full, err := compressStreamJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		fullTime += time.Since(started)
		fullBytes += len(full)
		started = time.Now()
		delta, err := makeDelta(previous, current)
		if err != nil {
			t.Fatal(err)
		}
		e := StreamEnvelope{Kind: "delta", Stream: "synthetic", Sequence: uint64(n + 2), Base: uint64(n + 1), Source: sourceOf(current), Build: current.State.Build, Delta: &delta}
		data, err := encodeStream(e)
		if err != nil {
			t.Fatal(err)
		}
		deltaTime += time.Since(started)
		deltaBytes += len(data)
		previous = current
	}
	if deltaBytes >= fullBytes {
		t.Fatalf("delta %d >= full %d", deltaBytes, fullBytes)
	}
	t.Logf("Synthetic 200 pods, 8000-lane routes: HTTP gzip %d B/s, delta gzip %d B/s, HTTP encode %s/publication, delta diff+encode %s/publication", fullBytes*20/publications, deltaBytes*20/publications, fullTime/publications, deltaTime/publications)
}
