package session

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/rail"
	"github.com/dotwaffle/podsim/internal/sim"
)

// interruptionProject returns the example project with the incident
// marker, one pod, shared rides of two parties, and one rail departure at
// harbor at atSeconds, for one passenger from market. Demand is off, so the
// test submits the orders itself.
func interruptionProject(atSeconds int) project.Config {
	config := project.Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.Fleet = config.Fleet[:1]
	config.SharedRidePartyLimit = 2
	config.Demand = DemandConfig{Pattern: "rail-services", PerMinute: 12, Seed: 7}
	config.RailDepartures = []project.RailDeparture{{ID: "train", Station: "harbor", AtSeconds: atSeconds, Passengers: 1,
		Origins: []project.RailOrigin{{Station: "market", Weight: 1}}}}
	return config
}

// railPair is a rail-bound order and a companion that ride one pod.
type railPair struct {
	pod   string
	bound int
	// boarded is the first tick with both orders aboard, and alighted is
	// the first later tick with the bound order not aboard.
	boarded, alighted int64
}

// boardRailPair submits, at the offer tick, the rail-bound order and a
// companion from market to harbor that shares its pod. It steps s until both
// orders ride one pod, and returns them. The caller holds s.mu.
func boardRailPair(t *testing.T, s *Session) railPair {
	t.Helper()
	offer := project.RailServicesSchedule(nil, s.project.RailDepartures, s.project.Demand.Seed)[0]
	for s.simulation.Tick() < offer.Tick {
		if err := s.step(); err != nil {
			t.Fatal(err)
		}
	}
	options := sim.TripOptions{From: offer.From, To: offer.To, SharingConsent: sim.SharedConsent}
	bound, err := s.simulation.SubmitTripOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.demand.connections.Add(offer, bound, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.simulation.SubmitTripOptions(options); err != nil {
		t.Fatal(err)
	}
	for s.simulation.Tick() < 300*sim.TicksPerSecond {
		if err := s.step(); err != nil {
			t.Fatal(err)
		}
		for _, vehicle := range s.simulation.Snapshot().Vehicles {
			if vehicle.RidersAboard() == 2 {
				return railPair{pod: vehicle.Pod.ID, bound: bound, boarded: s.simulation.Tick()}
			}
		}
	}
	t.Fatal("the orders do not ride one pod")
	return railPair{}
}

// aboard reports whether order rides a pod of s.
func aboard(s *Session, order int) bool {
	for _, vehicle := range s.simulation.Snapshot().Vehicles {
		for _, rider := range vehicle.Riders {
			if rider.ID == order && !rider.Completed {
				return true
			}
		}
	}
	return false
}

// probeRailPair returns the pair of a run of interruptionProject with no
// interruption, with the tick at which the bound order alights.
func probeRailPair(t *testing.T) railPair {
	t.Helper()
	s := newRailSession(t, interruptionProject(3600))
	s.mu.Lock()
	defer s.mu.Unlock()
	pair := boardRailPair(t, s)
	for aboard(s, pair.bound) {
		if err := s.step(); err != nil {
			t.Fatal(err)
		}
	}
	pair.alighted = s.simulation.Tick()
	return pair
}

// checkDelivered checks that s has no undelivered interruption, that the
// rail record of order is unserved with reason interrupted, and that the
// demand state has the counts of the ledger.
func checkDelivered(t *testing.T, s *Session, order int, want rail.Counts) {
	t.Helper()
	if undelivered := s.simulation.DrainInterruptions(); undelivered != nil {
		t.Fatalf("undelivered interruptions %v", undelivered)
	}
	for _, record := range s.demand.connections.Records() {
		if record.RequestID == order && (record.Outcome != "unserved" || record.Reason != "interrupted" || record.AlightedTick != -1) {
			t.Fatalf("rail record %+v, want unserved with reason interrupted", record)
		}
	}
	if counts := s.demand.connections.Counts(); counts != want || s.demand.state.Connections != counts {
		t.Fatalf("ledger counts %+v and demand counts %+v, want %+v", counts, s.demand.state.Connections, want)
	}
}

// interrupt interrupts the bound order of pair. The caller holds s.mu.
func interrupt(t *testing.T, s *Session, pair railPair) {
	t.Helper()
	if err := s.simulation.InterruptRider(pair.pod, pair.bound); err != nil {
		t.Fatal(err)
	}
}

// TestDepartureTickDelivery checks an interruption on the departure tick
// of its rail record (incident contract, section 8.5). The session delivers
// it before Advance scores the departure, so the record is unserved with
// reason interrupted, not missed.
func TestDepartureTickDelivery(t *testing.T) {
	t.Parallel()
	probe := probeRailPair(t)
	departure := probe.boarded/sim.TicksPerSecond + 2
	if departure*sim.TicksPerSecond > probe.alighted {
		t.Fatalf("the pair rides from tick %d to %d, so the departure at %d s is too late", probe.boarded, probe.alighted, departure)
	}
	s := newRailSession(t, interruptionProject(int(departure)))
	s.mu.Lock()
	defer s.mu.Unlock()
	pair := boardRailPair(t, s)
	if pair.pod != probe.pod || pair.bound != probe.bound || pair.boarded != probe.boarded {
		t.Fatalf("pair %+v, probe %+v", pair, probe)
	}
	for s.simulation.Tick() < departure*sim.TicksPerSecond-1 {
		if err := s.step(); err != nil {
			t.Fatal(err)
		}
	}
	// Step interrupts the order at the departure tick in a later stage.
	// Here the interruption waits undelivered until the step of that tick.
	interrupt(t, s, pair)
	if err := s.step(); err != nil {
		t.Fatal(err)
	}
	if s.simulation.Tick() != departure*sim.TicksPerSecond {
		t.Fatalf("tick %d, want the departure tick", s.simulation.Tick())
	}
	checkDelivered(t, s, pair.bound, rail.Counts{Unserved: 1})
}

// TestCommandDelivery checks that a command delivers the interruptions
// that it makes before the session releases its lock.
func TestCommandDelivery(t *testing.T) {
	t.Parallel()
	s := newRailSession(t, interruptionProject(3600))
	s.mu.Lock()
	defer s.mu.Unlock()
	pair := boardRailPair(t, s)
	interrupt(t, s, pair)
	if _, err := s.apply(Command{Action: "pause", Paused: true}); err != nil {
		t.Fatal(err)
	}
	checkDelivered(t, s, pair.bound, rail.Counts{Unserved: 1})
}

// TestSaveAndPublicationDelivery checks the second delivery guard of a save
// and of a state read, and a full save of a delivered interruption: the
// save restores, its rail record is unserved with reason interrupted, and
// a publication shows the rail counts of the save.
func TestSaveAndPublicationDelivery(t *testing.T) {
	t.Parallel()
	start := func(t *testing.T) (*Session, *fakeStore, railPair) {
		t.Helper()
		config := interruptionProject(3600)
		store := &fakeStore{}
		s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		s.mu.Lock()
		pair := boardRailPair(t, s)
		interrupt(t, s, pair)
		s.mu.Unlock()
		return s, store, pair
	}
	want := rail.Counts{Unserved: 1}
	t.Run("publication", func(t *testing.T) {
		t.Parallel()
		s, _, pair := start(t)
		if got := s.State().Demand.Connections; got != want {
			t.Fatalf("published counts %+v, want %+v", got, want)
		}
		if metrics := s.Metrics(); metrics.Interrupted != 1 {
			t.Fatalf("metrics count %d interrupted orders, want 1", metrics.Interrupted)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		checkDelivered(t, s, pair.bound, want)
	})
	t.Run("save", func(t *testing.T) {
		t.Parallel()
		s, store, pair := start(t)
		if err := s.SaveState(t.Context(), SavePeriodic); err != nil {
			t.Fatal(err)
		}
		file := store.lastWrite(t)
		if file.Demand.State.Connections != want || len(file.RailConnections) != 1 || file.RailConnections[0].Reason != "interrupted" {
			t.Fatalf("saved counts %+v and records %+v", file.Demand.State.Connections, file.RailConnections)
		}
		s.mu.Lock()
		checkDelivered(t, s, pair.bound, want)
		s.mu.Unlock()
	})
	t.Run("full save", func(t *testing.T) {
		t.Parallel()
		s, store, pair := start(t)
		s.mu.Lock()
		if _, err := s.apply(Command{Action: "pause", Paused: true}); err != nil {
			t.Fatal(err)
		}
		s.mu.Unlock()
		if err := s.SaveState(t.Context(), SavePeriodic); err != nil {
			t.Fatal(err)
		}
		file := store.lastWrite(t)
		if published := s.State().Demand.Connections; published != want || file.Demand.State.Connections != published {
			t.Fatalf("published counts %+v, saved counts %+v", published, file.Demand.State.Connections)
		}
		writes := store.writeList()
		restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: writes[len(writes)-1]}})
		if err != nil || restored.restore.Tier != "physical" {
			t.Fatalf("restore: %v, %+v", err, restored.restore)
		}
		t.Cleanup(restored.Close)
		restored.mu.Lock()
		defer restored.mu.Unlock()
		checkDelivered(t, restored, pair.bound, want)
		if aboard(restored, pair.bound) {
			t.Fatal("the restored session has the interrupted order aboard")
		}
	})
}
