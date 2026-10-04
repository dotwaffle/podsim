package session

import (
	"encoding/json"
	"fmt"
	"math"
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

// TestStreamLargeRouteWire checks that one delta publication is smaller
// than the legacy HTTP state for 200 pods with 8,000-lane routes. The
// sequences are the first publication, the changes from 1 to 2, 9 to 10,
// and 19 to 20 decimal digits, and the largest sequence. The benchmark of
// the same name repeats the publication to measure codec work.
func TestStreamLargeRouteWire(t *testing.T) {
	previous, laneIDs := largeRouteFixture(t)
	for _, sequence := range []uint64{2, 10, 1e9, 1e19, math.MaxUint64} {
		current, sizes := largeRoutePublication(t, previous, laneIDs, sequence)
		if sizes.delta >= sizes.full {
			t.Fatalf("sequence %d: delta %d >= full %d", sequence, sizes.delta, sizes.full)
		}
		t.Logf("sequence %d: HTTP gzip %d B, delta gzip %d B", sequence, sizes.full, sizes.delta)
		previous = current
	}
}

// BenchmarkStreamLargeRouteWire measures codec work for one publication
// with 200 pods and 8,000-lane routes. It excludes simulation stepping
// and topology transfer. With -benchtime=20x it does the same work as the
// earlier 20-publication test.
func BenchmarkStreamLargeRouteWire(b *testing.B) {
	previous, laneIDs := largeRouteFixture(b)
	var total largeRouteSizes
	publications := 0
	for b.Loop() {
		current, sizes := largeRoutePublication(b, previous, laneIDs, uint64(publications+2))
		total.full += sizes.full
		total.delta += sizes.delta
		total.fullTime += sizes.fullTime
		total.deltaTime += sizes.deltaTime
		previous = current
		publications++
	}
	if total.delta >= total.full {
		b.Fatalf("delta %d >= full %d", total.delta, total.full)
	}
	n := float64(publications)
	b.ReportMetric(float64(total.full)/n, "full-B/pub")
	b.ReportMetric(float64(total.delta)/n, "delta-B/pub")
	b.ReportMetric(float64(total.fullTime.Nanoseconds())/n, "full-ns/pub")
	b.ReportMetric(float64(total.deltaTime.Nanoseconds())/n, "delta-ns/pub")
}

// largeRouteFixture returns a frame with 200 pods. Each pod has an
// 8,000-lane display route and a 2,048-lane motion route. It also returns
// the 8,000 lane IDs that the legacy HTTP state sends for each pod.
func largeRouteFixture(tb testing.TB) (StreamFrame, []string) {
	tb.Helper()
	s, err := New()
	if err != nil {
		tb.Fatal(err)
	}
	frame, err := s.presentationFrame()
	if err != nil {
		tb.Fatal(err)
	}
	frame.State.Simulation.Vehicles = make([]VehicleFrame, 200)
	frame.Routes = make([]sim.RoutePresentation, 200)
	indexes := make([]int, 8000)
	laneIDs := make([]string, 8000)
	for i := range indexes {
		indexes[i] = i
		laneIDs[i] = fmt.Sprintf("synthetic-lane-%04d", i)
	}
	for i := range frame.Routes {
		frame.State.Simulation.Vehicles[i].Pod.ID = fmt.Sprintf("pod-%03d", i)
		frame.Routes[i] = sim.RoutePresentation{Identity: 1, Display: indexes, Lanes: indexes[:2048], Current: 1024, After: true}
	}
	return frame, laneIDs
}

// largeRouteSizes holds the gzip sizes and encode times of the two
// encodings of one or more publications.
type largeRouteSizes struct {
	full, delta         int
	fullTime, deltaTime time.Duration
}

// largeRoutePublication moves each pod 0.125 m and encodes the next frame
// twice: as the legacy gzip HTTP state with lane IDs, and as a gzip delta
// envelope from previous with the given sequence.
func largeRoutePublication(tb testing.TB, previous StreamFrame, laneIDs []string, sequence uint64) (StreamFrame, largeRouteSizes) {
	tb.Helper()
	current := previous
	current.State.Revision++
	current.State.Simulation.Vehicles = slices.Clone(previous.State.Simulation.Vehicles)
	for i := range current.State.Simulation.Vehicles {
		current.State.Simulation.Vehicles[i].Pod.Position.X += 0.125
	}
	var sizes largeRouteSizes
	started := time.Now()
	legacy := current.State
	legacy.Simulation.Vehicles = slices.Clone(current.State.Simulation.Vehicles)
	for i := range legacy.Simulation.Vehicles {
		legacy.Simulation.Vehicles[i].RouteLaneIDs = laneIDs
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		tb.Fatal(err)
	}
	full, err := compressStreamJSON(raw)
	if err != nil {
		tb.Fatal(err)
	}
	sizes.fullTime = time.Since(started)
	sizes.full = len(full)
	started = time.Now()
	delta, err := makeDelta(previous, current)
	if err != nil {
		tb.Fatal(err)
	}
	e := StreamEnvelope{Kind: "delta", Stream: "synthetic", Sequence: sequence, Base: sequence - 1, Source: sourceOf(current), Build: current.State.Build, Delta: &delta}
	data, err := encodeStream(e)
	if err != nil {
		tb.Fatal(err)
	}
	sizes.deltaTime = time.Since(started)
	sizes.delta = len(data)
	return current, sizes
}
