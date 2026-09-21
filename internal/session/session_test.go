package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func newTestSession(t *testing.T) *Session {
	t.Helper()
	s, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func commandFor(s *Session, action string) Command {
	return Command{Client: "test", Sequence: 1, Epoch: s.State().Epoch, Action: action}
}

func TestCommandRetriesAndReset(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	command := commandFor(s, "trip")
	command.Origin, command.Destination = "harbor", "market"
	first := s.Apply(command)
	if first.Error != "" || first.OrderID != 1 {
		t.Fatalf("first: %+v", first)
	}
	for range 3 {
		reply := s.Apply(command)
		if reply.OrderID != 1 || reply.Error != "" || reply.State.Revision != first.State.Revision || reply.State.Simulation.Submitted != 1 {
			t.Fatal("retry changed state")
		}
	}
	changed := command
	changed.Destination = "garden"
	if s.Apply(changed).Error == "" {
		t.Fatal("accepted reused sequence with different command")
	}
	reset := commandFor(s, "reset")
	reset.Sequence = 2
	reply := s.Apply(reset)
	if reply.Error != "" || reply.State.Simulation.Submitted != 0 || reply.State.Epoch != first.State.Epoch || reply.State.Revision <= first.State.Revision {
		t.Fatal("invalid reset")
	}
	if s.Apply(command).Error == "" || s.State().Simulation.Submitted != 0 {
		t.Fatal("old retry replayed after reset")
	}
	for i := range 600 {
		cmd := commandFor(s, "pause")
		cmd.Client = fmt.Sprint(i)
		s.Apply(cmd)
	}
	if s.Apply(command).Error == "" || s.State().Simulation.Submitted != 0 {
		t.Fatal("old retry replayed after many commands")
	}
	other := newTestSession(t)
	if other.Apply(reset).Error == "" {
		t.Fatal("accepted command from old server")
	}
}

func TestDemandDeterminismAndSpeed(t *testing.T) {
	t.Parallel()
	slow, fast := newTestSession(t), newTestSession(t)
	for _, s := range []*Session{slow, fast} {
		cmd := commandFor(s, "demand")
		cmd.Demand = DemandConfig{Enabled: true, PerMinute: 12, Pattern: "balanced", Seed: 42}
		if reply := s.Apply(cmd); reply.Error != "" {
			t.Fatal(reply.Error)
		}
	}
	fast.speed = 8
	for range 3600 {
		slow.advance()
	}
	for range 450 {
		fast.advance()
	}
	a, b := slow.State(), fast.State()
	if !reflect.DeepEqual(a.Simulation, b.Simulation) || a.Demand != b.Demand || a.Demand.Generated != 12 {
		t.Fatalf("speed changes demand: %+v / %+v", a.Demand, b.Demand)
	}
	cmd := commandFor(slow, "pause")
	cmd.Sequence = 2
	cmd.Paused = true
	slow.Apply(cmd)
	before := slow.State()
	for range 100 {
		slow.advance()
	}
	if !reflect.DeepEqual(before, slow.State()) {
		t.Fatal("paused clock advanced")
	}
	cmd = commandFor(slow, "reset")
	cmd.Sequence = 3
	slow.Apply(cmd)
	if slow.State().Demand.Config.Enabled || slow.State().Demand.Generated != 0 {
		t.Fatal("reset retained demand")
	}
}

func TestDemandQueueLimitConsumesArrivals(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	config := DemandConfig{Enabled: true, PerMinute: 12, Pattern: "market", Seed: 3}
	if err := s.demand.configure(config); err != nil {
		t.Fatal(err)
	}
	for len(s.simulation.Snapshot().Pending) < QueueLimit {
		if err := s.simulation.RequestTrip("garden", "market"); err != nil {
			t.Fatal(err)
		}
	}
	// Keep the fleet still while exercising arrivals at a saturated station.
	for range 3600 {
		s.demand.step(s.simulation)
	}
	if s.demand.state.Skipped != 12 || s.demand.state.Generated != 0 {
		t.Fatalf("cap: %+v", s.demand.state)
	}
	manual := commandFor(s, "trip")
	manual.Origin, manual.Destination = "harbor", "garden"
	if s.Apply(manual).Error == "" {
		t.Fatal("manual order bypassed cap")
	}
	s.simulation.Reset()
	s.demand.step(s.simulation)
	if s.demand.state.Generated != 0 {
		t.Fatal("skipped arrivals burst after queue cleared")
	}
	for range 299 {
		s.demand.step(s.simulation)
	}
	if s.demand.state.Generated != 1 {
		t.Fatal("next scheduled arrival missing")
	}
	for _, p := range s.simulation.Snapshot().Pending {
		if p.To != "market" {
			t.Fatal("incorrect demand destination")
		}
	}
	cmd := commandFor(s, "demo")
	cmd.Sequence = 2
	if reply := s.Apply(cmd); reply.Error != "" || reply.State.Demand.Config.Enabled {
		t.Fatal("demo retained demand")
	}
}

func TestHTTPValidationAndSharedObservers(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	handler := s.Handler(t.TempDir())
	command := commandFor(s, "trip")
	command.Origin, command.Destination = "harbor", "market"
	body, _ := json.Marshal(command)
	cases := []struct {
		name, method, body, origin, contentType string
		status                                  int
	}{
		{"valid", "POST", string(body), "http://example.com", "application/json", 200},
		{"retry", "POST", string(body), "", "application/json", 200},
		{"scheme", "POST", string(body), "https://example.com", "application/json", 403},
		{"origin", "POST", string(body), "http://elsewhere", "application/json", 403},
		{"type", "POST", string(body), "", "text/plain", 415},
		{"trailing", "POST", string(body) + " {}", "", "application/json", 400},
		{"unknown", "POST", `{"unknown":1}`, "", "application/json", 400},
		{"oversize", "POST", `{"client":"` + strings.Repeat("a", 9000) + `"}`, "", "application/json", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, "http://example.com/api/command", strings.NewReader(tc.body))
			request.Header.Set("Origin", tc.origin)
			request.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	var observers [2]State
	for i := range observers {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.com/api/state", nil))
		if err := json.NewDecoder(response.Body).Decode(&observers[i]); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(observers[0], observers[1]) || observers[0].Simulation.Submitted != 1 {
		t.Fatal("observers have different orders")
	}
	observers[0].Network.Stations[0].Berths[0].ID = "mutated"
	observers[0].Simulation.Vehicles[0].Pod.ID = "mutated"
	if bytes.Contains(mustJSON(t, s.State()), []byte("mutated")) {
		t.Fatal("snapshot aliases session")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestConcurrentClockAndCommands(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	var group sync.WaitGroup
	group.Go(func() {
		for range 300 {
			s.advance()
		}
	})
	for i := range 8 {
		group.Go(func() {
			cmd := commandFor(s, "pause")
			cmd.Client = fmt.Sprint(i)
			for j := range 20 {
				cmd.Sequence = uint64(j + 1)
				cmd.Paused = j%2 == 0
				if reply := s.Apply(cmd); reply.Error != "" {
					t.Error(reply.Error)
				}
				s.State()
			}
		})
	}
	group.Wait()
}

func TestInvalidDemandDoesNotChangeState(t *testing.T) {
	t.Parallel()
	for _, config := range []DemandConfig{{PerMinute: 0, Pattern: "balanced"}, {PerMinute: 13, Pattern: "market"}, {PerMinute: 2, Pattern: "unknown"}} {
		s := newTestSession(t)
		before := s.State()
		cmd := commandFor(s, "demand")
		cmd.Demand = config
		if s.Apply(cmd).Error == "" || !reflect.DeepEqual(before, s.State()) {
			t.Fatal("invalid demand changed session")
		}
	}
}
