package session

import (
	"runtime"
	"testing"
)

// BenchmarkCouplingFormats measures the cost of each coupled format on the
// phase fixture with one committed group: one session tick, the encode and
// the client decode of a full and of a one-tick delta publication, the
// HTTP state, and the save. Each wire benchmark reports its size. The
// retained benchmarks report the live heap that each decoded value keeps
// after a garbage collection.
func BenchmarkCouplingFormats(b *testing.B) {
	data := couplingPhaseFixtures(b)
	s, topology, first := couplingStreamFixture(b, data.Frames[0], "")
	file := couplingPhaseFile(b, couplingPhaseInput(b, data, data.Frames[0]))
	client := newTestClient(s, "cost")
	if reply := s.Apply(client.next(Command{Action: "pause", Paused: false})); reply.Error != "" {
		b.Fatal(reply.Error)
	}
	s.advance()
	second, err := s.presentationFrame()
	if err != nil {
		b.Fatal(err)
	}
	if len(second.State.Simulation.CouplingGroups) != 1 {
		b.Fatal("the group ended after one tick")
	}
	full := fullStreamEnvelope(first)
	deltaEnvelope := func() StreamEnvelope {
		d, deltaErr := makeDelta(first, second)
		if deltaErr != nil {
			b.Fatal(deltaErr)
		}
		e := full
		e.Kind, e.Full, e.Delta, e.Base, e.Sequence, e.Source, e.Build = "delta", nil, &d, 1, 2, sourceOf(second), second.State.Build
		return e
	}
	fullWire, err := encodeStream(full)
	if err != nil {
		b.Fatal(err)
	}
	deltaWire, err := encodeStream(deltaEnvelope())
	if err != nil {
		b.Fatal(err)
	}
	httpState, err := EncodeStateJSON(topology, first)
	if err != nil {
		b.Fatal(err)
	}
	var saver stateEncoder
	saved, err := saver.encode(file)
	if err != nil {
		b.Fatal(err)
	}
	assembler, err := NewStreamAssembler(topology)
	if err != nil {
		b.Fatal(err)
	}
	decode := func(wire []byte, previous StreamFrame) (StreamFrame, State) {
		inflated, err := InflateStream(wire)
		if err != nil {
			b.Fatal(err)
		}
		e, err := DecodeStreamJSON(inflated)
		if err != nil {
			b.Fatal(err)
		}
		frame, err := ApplyStream(previous, full.Stream, 1, e)
		if err != nil {
			b.Fatal(err)
		}
		state, err := assembler.State(frame)
		if err != nil {
			b.Fatal(err)
		}
		return frame, state
	}

	b.Run("tick", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.advance()
		}
		b.ReportMetric(float64(len(s.State().Simulation.CouplingGroups)), "groups")
	})
	b.Run("encode/full", func(b *testing.B) {
		b.ReportAllocs()
		var encoder streamEncoder
		for b.Loop() {
			if _, err := encoder.encode(full); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(fullWire)), "wire-B")
	})
	b.Run("encode/delta", func(b *testing.B) {
		b.ReportAllocs()
		var encoder streamEncoder
		for b.Loop() {
			if _, err := encoder.encode(deltaEnvelope()); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(deltaWire)), "wire-B")
	})
	b.Run("decode/full", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			decode(fullWire, StreamFrame{})
		}
	})
	b.Run("decode/delta", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			decode(deltaWire, first)
		}
	})
	b.Run("http/encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := EncodeStateJSON(topology, first); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(httpState)), "wire-B")
	})
	b.Run("http/decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := DecodeStateJSON(httpState); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("save/encode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := saver.encode(file); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(saved)), "wire-B")
	})
	b.Run("save/decode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := decodeStateFile(saved); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("retained/stream", func(b *testing.B) {
		frames, states := make([]StreamFrame, 0, b.N), make([]State, 0, b.N)
		before := liveHeap()
		for b.Loop() {
			frame, state := decode(fullWire, StreamFrame{})
			frames, states = append(frames, frame), append(states, state)
		}
		b.ReportMetric(float64(liveHeap()-before)/float64(len(frames)), "retained-B/op")
		runtime.KeepAlive(frames)
		runtime.KeepAlive(states)
	})
	b.Run("retained/http", func(b *testing.B) {
		states := make([]State, 0, b.N)
		before := liveHeap()
		for b.Loop() {
			state, err := DecodeStateJSON(httpState)
			if err != nil {
				b.Fatal(err)
			}
			states = append(states, state)
		}
		b.ReportMetric(float64(liveHeap()-before)/float64(len(states)), "retained-B/op")
		runtime.KeepAlive(states)
	})
	b.Run("retained/http-at-cap", func(b *testing.B) {
		// Each run decodes its own copy of the HTTP state, padded to
		// MaxStreamJSON. The copy is made after the baseline, and only the
		// decoded states stay live. So a decoded state that kept its
		// input would add 65 MiB to the result.
		states := make([]State, 0, b.N)
		before := liveHeap()
		for b.Loop() {
			b.StopTimer()
			padded := make([]byte, MaxStreamJSON)
			copy(padded, httpState)
			for i := len(httpState); i < len(padded); i++ {
				padded[i] = ' '
			}
			b.StartTimer()
			state, err := DecodeStateJSON(padded)
			if err != nil {
				b.Fatal(err)
			}
			states = append(states, state)
		}
		b.ReportMetric(float64(liveHeap()-before)/float64(len(states)), "retained-B/op")
		runtime.KeepAlive(states)
	})
}

// liveHeap returns the bytes of live heap objects after a full collection.
func liveHeap() int64 {
	runtime.GC()
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return int64(stats.HeapAlloc)
}
