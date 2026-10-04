package session

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func streamFixture(t *testing.T) (*Session, StreamFrame) {
	t.Helper()
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	return s, f
}
func TestStreamReconstruction(t *testing.T) {
	s, a := streamFixture(t)
	oldJSON := streamJSON(t, a)
	for i := range 50 {
		s.speedReduction = SpeedReduction{Sequence: uint64(i + 1), From: 60, To: 15}
		if i == 0 {
			s.Apply(Command{Client: "test", Sequence: 1, Epoch: a.State.Epoch, Action: "trip", Origin: "harbor", Destination: "market"})
		}
		s.advance()
		b, err := s.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		d, err := makeDelta(a, b)
		if err != nil {
			t.Fatal(err)
		}
		e := StreamEnvelope{Kind: "delta", Stream: "test", Sequence: uint64(i + 2), Base: uint64(i + 1), Source: sourceOf(b), Build: b.State.Build, Delta: &d}
		encoded, err := encodeStream(e)
		if err != nil {
			t.Fatal(err)
		}
		inflated, err := InflateStream(encoded)
		if err != nil {
			t.Fatal(err)
		}
		e, err = DecodeStreamJSON(inflated)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ApplyStream(a, "test", uint64(i+1), e)
		if err != nil {
			t.Fatal(err)
		}
		wantJSON := streamJSON(t, b)
		gotJSON := streamJSON(t, got)
		if !bytes.Equal(gotJSON, wantJSON) {
			t.Fatalf("publication %d mismatch", i)
		}
		before := streamJSON(t, a)
		if !bytes.Equal(before, oldJSON) {
			t.Fatal("delta changed predecessor")
		}
		a = got
		oldJSON = gotJSON
	}
	// Clearing values remain present replacement groups.
	b := a
	b.State.Simulation.Pending = []sim.Request{}
	b.State.Checkpoints = []Checkpoint{}
	b.State.Speed = 0
	b.State.Demand = DemandState{}
	b.State.Restore = RestoreInfo{}
	b.State.Simulation.Paused = false
	d, err := makeDelta(a, b)
	if err != nil {
		t.Fatal(err)
	}
	e := StreamEnvelope{Kind: "delta", Stream: "x", Sequence: 2, Base: 1, Source: sourceOf(b), Delta: &d}
	got, err := ApplyStream(a, "x", 1, e)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Fatal("clearing replacement mismatch")
	}
	b.State.Simulation.MaxDetourRatio = math.NaN()
	if _, err = makeDelta(a, b); err == nil {
		t.Fatal("ignored nonfinite group encoding error")
	}
}
func TestStreamCodecRejectsInvalidEnvelope(t *testing.T) {
	_, f := streamFixture(t)
	e := StreamEnvelope{Kind: "full", Stream: "test", Sequence: 1, Source: sourceOf(f), Full: &f}
	data, err := encodeStream(e)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(slices.Clone(data), data...), append(slices.Clone(data), 0), data[:len(data)-1]} {
		if _, err = InflateStream(bad); err == nil {
			t.Fatal("accepted bad gzip")
		}
	}
	raw := streamJSON(t, e)
	if _, err = DecodeStreamJSON(append(raw, []byte("{}")...)); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	e.Source.Revision++
	if _, err = ApplyStream(StreamFrame{}, "", 0, e); err == nil {
		t.Fatal("accepted inconsistent identity")
	}
}
func TestStreamCreditBoundaries(t *testing.T) {
	now := time.Unix(100, 0)
	c := &streamSubscriber{sent: []streamSent{{"old", 1, 10}, {"new", 1, 20}}, bytes: 30, progress: now}
	p := &statePublisher{}
	if err := p.control(c, "ack", "old", "1", "", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if c.bytes != 20 || len(c.sent) != 1 {
		t.Fatal("stream transition reset credit")
	}
	before := c.progress
	if err := p.control(c, "ack", "old", "1", "", now.Add(10*time.Second)); err != nil || c.progress != before {
		t.Fatal("duplicate ACK advanced progress")
	}
	if err := p.control(c, "ack", "new", "2", "", now); err == nil {
		t.Fatal("accepted unsent ACK")
	}
	if err := p.control(c, "ack", "new", "1", "", now.Add(11*time.Second)); err != nil || c.bytes != 0 {
		t.Fatal("cumulative ACK did not release credit")
	}
	c.heartbeat = 42
	c.heartbeatAt = now
	if err := p.control(c, "heartbeat", "", "", "41", now); err == nil {
		t.Fatal("accepted wrong heartbeat")
	}
	c.sent = []streamSent{{"new", 2, 10}}
	c.bytes = 10
	c.progress = now
	if err := p.control(c, "heartbeat", "", "", "42", now.Add(29*time.Second)); err != nil {
		t.Fatal(err)
	}
	if !streamExpired(c, now.Add(31*time.Second)) {
		t.Fatal("heartbeat ACK extended state progress deadline")
	}
}
func TestStreamOwnership(t *testing.T) {
	p := &statePublisher{}
	b := &streamPayload{refs: 2, data: make([]byte, 12, 16)}
	p.retained = 16
	p.historyBytes = 16
	p.history = []*streamPayload{b}
	p.evict()
	if p.retained != 16 || len(b.data) != 12 {
		t.Fatal("eviction released in-flight buffer")
	}
	p.release(b)
	if p.retained != 0 || b.data != nil {
		t.Fatal("last owner retained buffer")
	}
}
func TestStreamBaselineAndShutdown(t *testing.T) {
	t.Parallel()
	s, _ := streamFixture(t)
	server := httptest.NewServer(s.HandlerFS(fstest.MapFS{}))
	defer server.Close()
	defer s.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(MaxStreamMessage)
	kind, _, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageText {
		t.Fatal("missing hello", err)
	}
	kind, data, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary {
		t.Fatal("missing baseline", err)
	}
	raw, err := InflateStream(data)
	if err != nil {
		t.Fatal(err)
	}
	e, err := DecodeStreamJSON(raw)
	if err != nil || e.Kind != "full" {
		t.Fatal("invalid baseline", err)
	}
	s.Close()
	if err = s.WaitStreams(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err = conn.Read(ctx); err == nil {
		t.Fatal("socket survived shutdown")
	}
	if s.StreamStats().RetainedBytes != 0 {
		t.Fatal("shutdown leaked buffers")
	}
}

func maximumStreamFrame(t *testing.T) StreamFrame {
	t.Helper()
	_, f := streamFixture(t)
	fillStreamScalars(reflect.ValueOf(&f.State).Elem())
	// Keep the maximum fixture in its original stream family.
	f.State.Simulation.CouplingContract = ""
	f.State.Simulation.CouplingEnabled = false
	f.State.Simulation.CouplingGroups = nil
	escaped := strings.Repeat("\x01", 64)
	reason := strings.Repeat("\x01", 1024)
	request := sim.Request{ID: math.MaxInt, From: escaped, To: escaped, PodID: escaped, PartySize: math.MaxInt, LegacyPartySize: true, SharingConsent: sim.LegacyUnknownConsent, Service: sim.OnDemandService, RequestedTick: math.MaxInt64, BoardedTick: math.MaxInt64, DispatchReason: reason}
	// Restored pending requests include QueueLimit plus every pod party.
	f.State.Simulation.Pending = slices.Repeat([]sim.Request{request}, maxSavedTrips)
	f.State.Simulation.Vehicles = make([]VehicleFrame, project.MaxPods)
	f.Routes = make([]sim.RoutePresentation, project.MaxPods)
	for i := range f.Routes {
		p := sim.Pod{Class: sim.LegacyClass, ID: escaped, StationID: escaped, BerthID: escaped, LaneID: escaped, BlockedBy: escaped, ManeuverStationID: escaped, Activity: sim.Activity(escaped), WaitReason: sim.WaitReason(escaped), StationPhase: sim.StationPhase(escaped), Position: sim.Point{X: math.MaxFloat64, Y: -math.MaxFloat64}, LaneDistance: math.MaxFloat64, Speed: math.MaxFloat64}
		f.State.Simulation.Vehicles[i] = VehicleFrame{LegacyCohort: true, Pod: p, Riders: slices.Repeat([]sim.Request{request}, 8), Stops: slices.Repeat([]string{escaped}, 8), RelocatingTo: escaped, Rebalancing: true, PlatoonID: escaped, PlatoonIndex: math.MaxInt}
		f.Routes[i] = sim.RoutePresentation{Identity: math.MaxUint64, Display: slices.Repeat([]int{project.MaxLanes - 1}, project.MaxLanes), Origin: project.MaxNodes - 1, Lanes: slices.Repeat([]int{project.MaxLanes - 1}, sim.MotionRouteLimit), Start: math.MaxUint64, Current: math.MaxUint64, Before: true, After: true}
	}
	// Berths have unique nodes, so MaxNodes also bounds their total count.
	f.State.Simulation.Berths = slices.Repeat([]sim.BerthState{{ID: escaped, Occupant: escaped, ReservedBy: escaped}}, project.MaxNodes)
	f.State.Simulation.DemoError = reason
	f.State.Build = "0123456789abcdef"
	f.State.Checkpoints = make([]Checkpoint, checkpointLimit)
	for i := range f.State.Checkpoints {
		fillStreamScalars(reflect.ValueOf(&f.State.Checkpoints[i]).Elem())
	}
	return f
}

func TestStreamMaximumEncoding(t *testing.T) {
	for _, representation := range []string{"historical", "modern", "mixed"} {
		t.Run(representation, func(t *testing.T) {
			f := maximumStreamFrame(t)
			if representation != "historical" {
				for i := range f.State.Simulation.Vehicles {
					if representation == "mixed" && i%2 == 0 {
						continue
					}
					v := &f.State.Simulation.Vehicles[i]
					v.LegacyCohort = false
					v.Pod.Class = sim.CompactClass
					v.RiddenMeters = 0.0000010000000000000002
					v.Boardings = slices.Repeat([]sim.RiderBoarding{{BerthID: strings.Repeat("\x01", 64), MetersAtBoarding: 0.0000010000000000000002}}, 8)
					for j := range v.Riders {
						v.Riders[j].LegacyPartySize = false
						v.Riders[j].PartySize = sim.MaxNewPartySize
						v.Riders[j].SharingConsent = sim.SharedConsent
					}
				}
			}

			e := StreamEnvelope{Kind: "full", Stream: strings.Repeat("x", 32), Sequence: math.MaxUint64, Source: sourceOf(f), Build: f.State.Build, Full: &f}
			data, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if len(data) > MaxStreamJSON {
				t.Fatalf("full allowance exceeded: %d", len(data))
			}
			t.Logf("conservative full encoder fixture: %d bytes", len(data))
			assertUnpackedStreamMaximum(t, data)
			compressed, err := encodeStream(e)
			if err != nil {
				t.Fatal(err)
			}
			out, err := InflateStream(compressed)
			if err != nil || !bytes.Equal(out, data) {
				t.Fatal("maximum gzip round trip", err)
			}
			// Force all vehicle groups and global groups to change.
			_, empty := streamFixture(t)
			empty.State.Simulation.Vehicles = make([]VehicleFrame, project.MaxPods)
			empty.Routes = make([]sim.RoutePresentation, project.MaxPods)
			empty.State.Simulation.Berths = make([]sim.BerthState, project.MaxNodes)
			d, err := makeDelta(empty, f)
			if err != nil {
				t.Fatal(err)
			}
			e.Kind = "delta"
			e.Full = nil
			e.Delta = &d
			e.Base = math.MaxUint64 - 1
			data, err = json.Marshal(e)
			if err != nil || len(data) > MaxStreamJSON {
				t.Fatal("maximum delta exceeds cap", len(data), err)
			}
			t.Logf("conservative delta encoder fixture: %s bytes", strconv.Itoa(len(data)))
			assertUnpackedStreamMaximum(t, data)
			compressed, err = encodeStream(e)
			if err != nil {
				t.Fatal(err)
			}
			out, err = InflateStream(compressed)
			if err != nil || !bytes.Equal(out, data) {
				t.Fatal("maximum delta gzip round trip", err)
			}
		})
	}
}

func TestStreamLatencyWindow(t *testing.T) {
	t.Parallel()
	for _, rtt := range []time.Duration{300 * time.Millisecond, 600 * time.Millisecond} {
		t.Run(rtt.String(), func(t *testing.T) {
			t.Parallel()
			s, _ := streamFixture(t)
			defer s.Close()
			server := httptest.NewServer(s.HandlerFS(fstest.MapFS{}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			go s.Run(ctx)
			conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.CloseNow() }()
			conn.SetReadLimit(MaxStreamMessage)
			if _, _, err = conn.Read(ctx); err != nil {
				t.Fatal(err)
			}
			type received struct {
				e   StreamEnvelope
				at  time.Time
				err error
			}
			messages := make(chan received, streamCreditCount)
			go func() {
				for {
					kind, data, readErr := conn.Read(ctx)
					if readErr != nil {
						select {
						case messages <- received{err: readErr}:
						case <-ctx.Done():
						}
						return
					}
					if kind == websocket.MessageText {
						continue
					}
					raw, readErr := InflateStream(data)
					if readErr != nil {
						messages <- received{err: readErr}
						return
					}
					e, readErr := DecodeStreamJSON(raw)
					select {
					case messages <- received{e: e, at: time.Now(), err: readErr}:
					case <-ctx.Done():
						return
					}
				}
			}()
			var pending []received
			var full, delta int
			var frame StreamFrame
			var stream string
			var seq uint64
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for full+delta < 30 {
				select {
				case <-ctx.Done():
					t.Fatal("stream stopped under healthy RTT")
				case m := <-messages:
					if m.err != nil {
						t.Fatal(m.err)
					}
					next, applyErr := ApplyStream(frame, stream, seq, m.e)
					if applyErr != nil {
						t.Fatal(applyErr)
					}
					frame, stream, seq = next, m.e.Stream, m.e.Sequence
					if m.e.Kind == "full" {
						full++
					} else {
						delta++
					}
					m.at = m.at.Add(rtt + time.Duration(delta%5)*10*time.Millisecond)
					pending = append(pending, m)
				case now := <-ticker.C:
					last := -1
					for i, m := range pending {
						if now.Before(m.at) {
							break
						}
						last = i
					}
					if last >= 0 {
						e := pending[last].e
						body := streamJSON(t, map[string]string{"kind": "ack", "stream": e.Stream, "sequence": strconv.FormatUint(e.Sequence, 10)})
						if err = conn.Write(ctx, websocket.MessageText, body); err != nil {
							t.Fatal(err)
						}
						pending = pending[last+1:]
					}
				}
			}
			if full != 1 || delta != 29 {
				t.Fatalf("RTT caused resync: %d full, %d delta", full, delta)
			}
			if stats := s.StreamStats(); stats.OutstandingMessages > streamCreditCount || stats.OutstandingBytes > streamCreditBytes {
				t.Fatal("credit overflow", stats)
			}
		})
	}
}
func TestStreamNonReaderIsBounded(t *testing.T) {
	t.Parallel()
	s, _ := streamFixture(t)
	defer s.Close()
	server := httptest.NewServer(s.HandlerFS(fstest.MapFS{}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	go s.Run(ctx)
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	// Do not read hello, state or application heartbeat.
	deadline := time.Now().Add(6 * time.Second)
	for s.StreamStats().OutstandingMessages < streamCreditCount && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	stats := s.StreamStats()
	if stats.OutstandingMessages != streamCreditCount || stats.OutstandingBytes > streamCreditBytes || stats.RetainedBytes > streamRetainedBytes {
		t.Fatal("unbounded or stalled nonreader", stats)
	}
	now := time.Unix(100, 0)
	c := &streamSubscriber{heartbeat: 1, heartbeatAt: now}
	if !streamExpired(c, now.Add(31*time.Second)) {
		t.Fatal("paused nonreader has no progress deadline")
	}
}
func TestStreamSharedFullAndAdmission(t *testing.T) {
	t.Parallel()
	s, f := streamFixture(t)
	s.Apply(Command{Client: "pause", Sequence: 1, Epoch: f.State.Epoch, Action: "pause", Paused: true})
	defer s.Close()
	server := httptest.NewServer(s.HandlerFS(fstest.MapFS{}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var conns []*websocket.Conn
	defer func() {
		for _, c := range conns {
			_ = c.CloseNow()
		}
	}()
	for range 2 {
		c, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
		c.SetReadLimit(MaxStreamMessage)
		if _, _, err = c.Read(ctx); err != nil {
			t.Fatal(err)
		}
		if _, _, err = c.Read(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if stats := s.StreamStats(); stats.Full != 1 || stats.Delta != 0 || stats.Connections != 2 {
		t.Fatal("baseline was encoded per client", stats)
	}
	p := s.publisher()
	p.mu.Lock()
	for len(p.clients) < streamConnections {
		p.clients[&streamSubscriber{}] = true
	}
	p.mu.Unlock()
	c, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/state/stream", nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if c != nil {
		_ = c.CloseNow()
	}
	if err == nil || response.StatusCode != http.StatusServiceUnavailable {
		t.Fatal("missing stream admission bound", err)
	}
	p.mu.Lock()
	for c := range p.clients {
		if c.conn == nil {
			delete(p.clients, c)
		}
	}
	p.mu.Unlock()
}

func TestStreamFieldOwnership(t *testing.T) {
	checks := []struct {
		typ    reflect.Type
		groups map[string][]string
	}{
		{reflect.TypeFor[StateFrame](), map[string][]string{
			"identity": {"Epoch", "Revision", "ProjectRevision", "Generation", "Build", "ServerStart"}, "controls": {"Speed", "Redistribution", "SpeedReduction"}, "demand": {"Demand"}, "restore": {"Restore"}, "checkpoints": {"Checkpoints"}, "simulation": {"Simulation"},
		}},
		{reflect.TypeFor[SimulationFrame](), map[string][]string{
			"contract identity": {"OrderContract", "CouplingContract"}, "coupling": {"CouplingEnabled", "CouplingGroups"}, "global": {"Submitted", "Tick", "Paused", "Completed", "Demo", "DemoError"}, "vehicles": {"Vehicles"}, "berths": {"Berths"}, "pending": {"Pending"},
			"statistics": {"Wait", "Journey", "PassengerDistanceMeters", "RiderDistanceMeters", "DirectDistanceMeters", "MaxDetourRatio", "SharedParties", "SharedRidePartyLimit", "EmptyDistanceMeters", "RebalanceMoves"},
		}},
		{reflect.TypeFor[VehicleFrame](), map[string][]string{"pod": {"Pod"}, "riders": {"Riders"}, "boardings": {"Boardings"}, "stops": {"Stops"}, "presentation replaces route": {"RouteLaneIDs"}, "metadata": {"CouplingID", "RiddenMeters", "LegacyCohort", "RelocatingTo", "Rebalancing", "PlatoonID", "PlatoonIndex"}}},
	}
	for _, check := range checks {
		seen := map[string]string{}
		for group, fields := range check.groups {
			for _, field := range fields {
				if seen[field] != "" {
					t.Errorf("%s.%s belongs to two groups", check.typ, field)
				}
				if _, ok := check.typ.FieldByName(field); !ok {
					t.Errorf("unknown %s.%s", check.typ, field)
				}
				seen[field] = group
			}
		}
		for field := range check.typ.Fields() {
			if seen[field.Name] == "" {
				t.Errorf("%s.%s has no stream owner", check.typ, field.Name)
			}
		}
	}
}
func TestStreamAssemblerCachesRoutes(t *testing.T) {
	s, f := streamFixture(t)
	s.Apply(Command{Client: "test", Sequence: 1, Epoch: f.State.Epoch, Action: "trip", Origin: "harbor", Destination: "market"})
	f, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	assembler, err := NewStreamAssembler(s.Topology())
	if err != nil {
		t.Fatal(err)
	}
	first, err := assembler.State(f)
	if err != nil {
		t.Fatal(err)
	}
	second, err := assembler.State(f)
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range first.Simulation.Vehicles {
		if len(v.Route) > 0 && &v.Route[0] != &second.Simulation.Vehicles[i].Route[0] {
			t.Fatal("unchanged route was expanded again")
		}
		if v.Presentation != second.Simulation.Vehicles[i].Presentation {
			t.Fatal("unchanged route metadata was copied")
		}
	}
	f.Routes = slices.Clone(f.Routes)
	f.Routes[0].Display = []int{-1}
	if _, err = assembler.State(f); err == nil {
		t.Fatal("accepted invalid route index")
	}
}
func TestStreamGzipExpansionBound(t *testing.T) {
	// BestSpeed chooses stored blocks when compressed blocks cost more. Allow
	// 64 KiB above the input for block headers, alignment and the gzip wrapper.
	data := make([]byte, MaxStreamJSON)
	var x uint64 = 1
	for i := range data {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		data[i] = byte(x)
	}
	compressed, err := compressStreamJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) > len(data)+(64<<10) {
		t.Fatalf("gzip expansion exceeded proof allowance: %d", len(compressed))
	}
	t.Logf("64 MiB incompressible gzip fixture: %d bytes, cap %d", len(compressed), MaxStreamMessage)
	output, err := InflateStream(compressed)
	if err != nil || !bytes.Equal(output, data) {
		t.Fatal("gzip boundary round trip", err)
	}
	if _, err = compressStreamJSON(make([]byte, MaxStreamJSON+1)); err == nil {
		t.Fatal("accepted excessive inflate size")
	}
}
func TestStreamBuildBound(t *testing.T) {
	s, _ := streamFixture(t)
	s.build = strings.Repeat("x", 65)
	if _, err := s.presentationFrame(); err == nil {
		t.Fatal("unbounded injected build ID")
	}
}

// fillStreamScalars overestimates global text with the restored-text bound.
func fillStreamScalars(v reflect.Value) {
	if v.Type() == reflect.TypeFor[sim.OrderContract]() {
		return
	}
	switch v.Kind() {
	case reflect.Struct:
		for _, field := range v.Fields() {
			fillStreamScalars(field)
		}
	case reflect.String:
		v.SetString(strings.Repeat("\x01", 1024))
	case reflect.Int, reflect.Int64:
		v.SetInt(math.MinInt64)
	case reflect.Uint64:
		v.SetUint(math.MaxUint64)
	case reflect.Float64:
		v.SetFloat(-math.MaxFloat64)
	case reflect.Bool:
		v.SetBool(true)
	default:
		// Containers have explicit maximum-count fixtures.
	}
}

func TestStreamEvictionRequiresOneBaseline(t *testing.T) {
	s, f := streamFixture(t)
	p := &statePublisher{session: s, clients: map[*streamSubscriber]bool{}, wake: make(chan struct{}, 1)}
	if err := p.publish(t.Context(), true, true); err != nil {
		t.Fatal(err)
	}
	c := &streamSubscriber{stream: p.stream, sequence: p.sequence}
	s.Apply(Command{Client: "test", Sequence: 1, Epoch: f.State.Epoch, Action: "trip", Origin: "harbor", Destination: "market"})
	for range streamHistoryCount + 2 {
		s.advance()
		if err := p.publish(t.Context(), false, true); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.history) != streamHistoryCount {
		t.Fatal("history count not bounded", len(p.history))
	}
	if b := p.nextPayload(c); b != nil || !p.needFull {
		t.Fatal("evicted client did not request shared baseline")
	}
	if err := p.publish(t.Context(), true, false); err != nil {
		t.Fatal(err)
	}
	baseline := p.nextPayload(c)
	if baseline == nil || !baseline.full {
		t.Fatal("missing baseline after eviction")
	}
	c.stream, c.sequence = baseline.stream, baseline.sequence
	s.advance()
	if err := p.publish(t.Context(), false, true); err != nil {
		t.Fatal(err)
	}
	if b := p.nextPayload(c); b == nil || b.full || b.sequence != c.sequence+1 {
		t.Fatal("delta delivery did not resume")
	}
	for len(p.history) > 0 {
		p.evict()
	}
	p.release(p.full)
	if p.retained != 0 {
		t.Fatal("retained history leaked")
	}
}
func TestStreamCachedRouteChecksCurrentPod(t *testing.T) {
	s, f := streamFixture(t)
	s.Apply(Command{Client: "test", Sequence: 1, Epoch: f.State.Epoch, Action: "trip", Origin: "harbor", Destination: "market"})
	f, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewStreamAssembler(s.Topology())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.State(f); err != nil {
		t.Fatal(err)
	}
	f.State.Simulation.Vehicles = slices.Clone(f.State.Simulation.Vehicles)
	f.State.Simulation.Vehicles[0].Pod.LaneID = s.Topology().Network.Lanes[len(s.Topology().Network.Lanes)-1].ID
	if _, err = a.State(f); err == nil {
		t.Fatal("cached route accepted inconsistent pod lane")
	}
}

func TestStreamExactBaseAndEmptyShapes(t *testing.T) {
	_, frame := streamFixture(t)
	delta, err := makeDelta(frame, frame)
	if err != nil {
		t.Fatal(err)
	}
	envelope := StreamEnvelope{Kind: "delta", Stream: "test", Sequence: 2, Base: 9, Source: sourceOf(frame), Build: frame.State.Build, Delta: &delta}
	if _, err := ApplyStream(frame, "test", 1, envelope); err == nil {
		t.Fatal("accepted wrong base with next sequence")
	}
	for _, field := range []string{"routes", "vehicles", "berths"} {
		a, b := StreamFrame{}, StreamFrame{}
		switch field {
		case "routes":
			b.Routes = []sim.RoutePresentation{}
		case "vehicles":
			b.State.Simulation.Vehicles = []VehicleFrame{}
		case "berths":
			b.State.Simulation.Berths = []sim.BerthState{}
		}
		if sameChain(a, b) {
			t.Fatalf("%s null to empty did not require full", field)
		}
	}
}

func streamJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStreamBlockedWriterStops(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	entered := make(chan struct{})
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			finished <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		close(entered)
		finished <- streamWrite(ctx, conn, websocket.MessageBinary, make([]byte, MaxStreamMessage))
	}))
	defer server.Close()
	conn, response, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	<-entered
	// The receiver never reads. A 65 MiB write exceeds the TCP buffer.
	select {
	case err := <-finished:
		t.Fatal("write completed without a reader", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("canceled write succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked writer did not stop on cancellation")
	}
}

func TestStreamReplacementSlicesOwnStorage(t *testing.T) {
	for _, reject := range []bool{false, true} {
		_, previous := streamFixture(t)
		previous.State.Checkpoints = []Checkpoint{{ID: 1, Tick: 2}}
		previous.State.Simulation.Pending = []sim.Request{{ID: 1, From: "harbor", To: "market"}}
		before := streamJSON(t, previous)
		desired := previous
		desired.State.Checkpoints = []Checkpoint{{ID: 2, Tick: 3}}
		desired.State.Simulation.Pending = []sim.Request{{ID: 2, From: "market", To: "harbor"}}
		delta, err := makeDelta(previous, desired)
		if err != nil {
			t.Fatal(err)
		}
		if reject {
			delta.Vehicles = []VehicleDelta{{ID: "absent"}}
		}
		got, err := ApplyStream(previous, "test", 1, StreamEnvelope{Kind: "delta", Stream: "test", Sequence: 2, Base: 1, Source: sourceOf(desired), Build: desired.State.Build, Delta: &delta})
		if reject && err == nil || !reject && err != nil {
			t.Fatal("candidate rejection mismatch", err)
		}
		if !bytes.Equal(before, streamJSON(t, previous)) {
			t.Fatal("replacement changed predecessor storage")
		}
		if !reject && !reflect.DeepEqual(got, desired) {
			t.Fatal("nonempty replacement mismatch")
		}
	}
}
func TestStreamSequenceWrapStartsNewChain(t *testing.T) {
	s, f := streamFixture(t)
	p := &statePublisher{session: s, frame: f, stream: "old", sequence: ^uint64(0), clients: map[*streamSubscriber]bool{}}
	if err := p.publish(t.Context(), false, true); err != nil {
		t.Fatal(err)
	}
	if p.sequence != 1 || p.stream == "old" || p.full == nil {
		t.Fatal("sequence wrapped without new baseline")
	}
}

func TestStreamReplacementClearsOmittedFields(t *testing.T) {
	for _, group := range []string{"demand", "restore"} {
		t.Run(group, func(t *testing.T) {
			_, previous := streamFixture(t)
			switch group {
			case "demand":
				previous.State.Demand.Error = "old demand error"
			case "restore":
				previous.State.Restore = RestoreInfo{Tier: "physical", Reason: "old restore reason", Demoted: 1, Requeued: 2, Dropped: 3, Unaccounted: 4}
			}
			before := streamJSON(t, previous)
			want := previous
			switch group {
			case "demand":
				want.State.Demand = DemandState{}
			case "restore":
				want.State.Restore = RestoreInfo{}
			}
			delta, err := makeDelta(previous, want)
			if err != nil {
				t.Fatal(err)
			}
			envelope := StreamEnvelope{Kind: "delta", Stream: "clear", Sequence: 2, Base: 1, Source: sourceOf(want), Delta: &delta}
			encoded, err := encodeStream(envelope)
			if err != nil {
				t.Fatal(err)
			}
			inflated, err := InflateStream(encoded)
			if err != nil {
				t.Fatal(err)
			}
			envelope, err = DecodeStreamJSON(inflated)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ApplyStream(previous, "clear", 1, envelope)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s replacement retained omitted fields: demand=%+v restore=%+v", group, got.State.Demand, got.State.Restore)
			}
			if !bytes.Equal(before, streamJSON(t, previous)) {
				t.Fatal("replacement changed predecessor")
			}
		})
	}
}
