package session

import (
	"bytes"
	"encoding/binary"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// poolingProject has one pod at harbor, with the party limit in drop-offs
// mode. Its demand pattern is pattern, and each pattern offers exactly two
// orders from harbor to market at one tick.
func poolingProject(pattern string, limit int, consent sim.SharingConsent) project.Config {
	config := project.Default()
	config.Fleet = []sim.Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
	config.SharedRidePartyLimit, config.SharedRideMode = limit, sim.SharedRideDropOffs
	config.GeneratedOrderConsent = consent
	config.Demand = DemandConfig{Enabled: true, PerMinute: 12, Pattern: pattern, Seed: 7}
	if pattern == "rail-services" {
		config.RailDepartures = []project.RailDeparture{{ID: "evening", Station: "market", AtSeconds: 900, RequestFromSeconds: 1, RequestUntilSeconds: 1,
			Passengers: 2, Origins: []project.RailOrigin{{Station: "harbor", Weight: 1}}}}
		return config
	}
	config.RailArrivals = []project.RailArrival{{ID: "train", Station: "harbor", AtSeconds: 1,
		Passengers: 2, Destinations: []project.RailDestination{{Station: "market", Weight: 1}}}}
	return config
}

// runPooling runs config until both orders complete. It gives the largest
// number of parties that the pod carried at one time, and the consent of
// each rider that it saw.
func runPooling(t *testing.T, config project.Config) (int, map[int]sim.SharingConsent) {
	t.Helper()
	s := newRailSession(t, config)
	carried, consents := 0, make(map[int]sim.SharingConsent)
	for range 900 * sim.TicksPerSecond {
		s.advance()
		state := s.simulation.Snapshot()
		for _, vehicle := range state.Vehicles {
			aboard := 0
			for _, rider := range vehicle.Riders {
				consents[rider.ID] = rider.SharingConsent
				if !rider.Completed {
					aboard++
				}
			}
			carried = max(carried, aboard)
		}
		if state.Completed == 2 {
			if s.demand.state.Generated != 2 || s.demand.state.Skipped != 0 || s.demand.state.Error != "" {
				t.Fatalf("demand state: %+v", s.demand.state)
			}
			return carried, consents
		}
	}
	t.Fatalf("the two orders did not complete: %+v", s.simulation.Snapshot())
	return 0, nil
}

// TestGeneratedConsentPools runs one fixture for each path that offers a
// generated order. With shared consent the pod carries both parties. A
// private run with the same seed carries them one at a time.
func TestGeneratedConsentPools(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"rail-arrivals", "rail-services"} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()
			for _, test := range []struct {
				consent sim.SharingConsent
				carried int
				want    sim.SharingConsent
			}{
				{sim.SharedConsent, 2, sim.SharedConsent},
				{sim.PrivateConsent, 1, sim.PrivateConsent},
				{"", 1, sim.PrivateConsent},
			} {
				carried, consents := runPooling(t, poolingProject(pattern, 8, test.consent))
				if carried != test.carried {
					t.Errorf("consent %q: the pod carried %d parties, want %d", test.consent, carried, test.carried)
				}
				if len(consents) != 2 {
					t.Errorf("consent %q: riders %v, want two", test.consent, consents)
				}
				for id, consent := range consents {
					if consent != test.want {
						t.Errorf("consent %q: order %d has consent %q, want %q", test.consent, id, consent, test.want)
					}
				}
			}
		})
	}
}

// consentCounts is the demand state and the ledger of one offer test.
type consentCounts struct {
	generated, skipped int
	errorText          string
	reasons            []string
}

func readConsentCounts(s *Session) consentCounts {
	counts := consentCounts{generated: s.demand.state.Generated, skipped: s.demand.state.Skipped, errorText: s.demand.state.Error}
	for _, record := range s.demand.connectionRecords() {
		counts.reasons = append(counts.reasons, record.Reason)
	}
	return counts
}

// TestGeneratedConsentRefusals pins the counts of a full queue and of a
// refused request on both offer paths, with each consent.
func TestGeneratedConsentRefusals(t *testing.T) {
	t.Parallel()
	const refused = "choose a passenger pickup station"
	for _, test := range []struct {
		name, pattern string
		refuse        bool
		want          consentCounts
	}{
		{"offer full queue", "rail-arrivals", false, consentCounts{skipped: 2}},
		{"offer refused", "rail-arrivals", true, consentCounts{skipped: 2, errorText: refused}},
		{"connection full queue", "rail-services", false, consentCounts{skipped: 2, reasons: []string{"queue-limit", "queue-limit"}}},
		{"connection refused", "rail-services", true, consentCounts{skipped: 2, errorText: refused, reasons: []string{"request-error", "request-error"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, consent := range []sim.SharingConsent{"", sim.SharedConsent} {
				s := newRailSession(t, poolingProject(test.pattern, 8, consent))
				for s.simulation.Tick() < sim.TicksPerSecond-1 {
					s.advance()
				}
				if test.refuse {
					// With harbor closed to passengers, the simulation refuses each
					// request from it. Project validation does not admit a demand
					// plan from such a station, so the test steps the run by itself.
					network := project.Default().Network
					for index := range network.Stations {
						if network.Stations[index].ID == "harbor" {
							network.Stations[index].ParkingOnly = true
						}
					}
					closed, err := sim.NewFleet(network, []sim.Placement{{ID: "01", StationID: "garden", BerthID: "garden-1"}})
					if err != nil {
						t.Fatal(err)
					}
					for closed.Tick() < sim.TicksPerSecond {
						closed.Step()
					}
					s.demand.step(closed)
				} else {
					// The margin keeps the queue full through the physics of the
					// offer tick.
					for range QueueLimit + 10 {
						if err := s.simulation.RequestTrip("garden", "market"); err != nil {
							t.Fatal(err)
						}
					}
					s.advance()
				}
				if got := readConsentCounts(s); !reflect.DeepEqual(got, test.want) {
					t.Errorf("consent %q: counts %+v, want %+v", consent, got, test.want)
				}
			}
		})
	}
}

// consentTrace is the state of one tick that the limit-one rule keeps.
type consentTrace struct {
	tick                       int64
	submitted, completed, wait int
	pods                       []sim.Pod
}

// runLimitOne gives the trace of a run of config, and the consents of the
// orders of the run.
func runLimitOne(t *testing.T, config project.Config) ([]consentTrace, map[sim.SharingConsent]int) {
	t.Helper()
	s := newRailSession(t, config)
	trace := make([]consentTrace, 0, 120*sim.TicksPerSecond)
	orders := make(map[int]sim.SharingConsent)
	for range 120 * sim.TicksPerSecond {
		s.advance()
		state := s.simulation.Snapshot()
		pods := make([]sim.Pod, len(state.Vehicles))
		for index, vehicle := range state.Vehicles {
			pods[index] = vehicle.Pod
			aboard := 0
			for _, rider := range vehicle.Riders {
				orders[rider.ID] = rider.SharingConsent
				if !rider.Completed {
					aboard++
				}
			}
			if aboard > 1 {
				t.Fatalf("tick %d: pod %s carried %d parties with a limit of one", state.Tick, vehicle.Pod.ID, aboard)
			}
		}
		for _, request := range state.Pending {
			orders[request.ID] = request.SharingConsent
		}
		trace = append(trace, consentTrace{tick: state.Tick, submitted: state.Submitted, completed: state.Completed, wait: len(state.Pending), pods: pods})
	}
	count := make(map[sim.SharingConsent]int)
	for _, consent := range orders {
		count[consent]++
	}
	return trace, count
}

// TestGeneratedConsentLimitOne states the limit-one rule. With a party limit
// of one, shared and private orders give the same pod positions and counts.
func TestGeneratedConsentLimitOne(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"balanced", "rail-services"} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()
			run := func(consent sim.SharingConsent) ([]consentTrace, map[sim.SharingConsent]int) {
				config := project.Default()
				config.SharedRidePartyLimit, config.GeneratedOrderConsent = 1, consent
				config.Demand = DemandConfig{Enabled: true, PerMinute: 60, Pattern: pattern, Seed: 11}
				if pattern == "rail-services" {
					config.RailDepartures = poolingProject(pattern, 1, consent).RailDepartures
				}
				return runLimitOne(t, config)
			}
			shared, sharedOrders := run(sim.SharedConsent)
			private, privateOrders := run(sim.PrivateConsent)
			if sharedOrders[sim.PrivateConsent] != 0 || privateOrders[sim.SharedConsent] != 0 || sharedOrders[sim.SharedConsent] != privateOrders[sim.PrivateConsent] || sharedOrders[sim.SharedConsent] == 0 {
				t.Fatalf("orders: shared run %v, private run %v", sharedOrders, privateOrders)
			}
			if !reflect.DeepEqual(shared, private) {
				t.Fatal("a limit of one gave different positions or counts")
			}
		})
	}
}

// addOrders records the consent of each order of requests.
func addOrders(orders map[int]sim.SharingConsent, requests []sim.Request) {
	for _, request := range requests {
		orders[request.ID] = request.SharingConsent
	}
}

// wireOrders gives the orders of the current state of s, as the HTTP state
// and the stream decode them.
func wireOrders(t *testing.T, s *Session) (fromHTTP, fromStream map[int]sim.SharingConsent) {
	t.Helper()
	frame, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := EncodeStateJSON(s.Topology(), frame)
	if err != nil {
		t.Fatal(err)
	}
	state, err := DecodeStateJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	streamRaw, err := EncodeStreamJSON(fullStreamEnvelope(frame))
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := DecodeStreamJSON(streamRaw)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := ApplyStream(StreamFrame{}, "", 0, envelope)
	if err != nil {
		t.Fatal(err)
	}
	fromHTTP, fromStream = make(map[int]sim.SharingConsent), make(map[int]sim.SharingConsent)
	addOrders(fromHTTP, state.Simulation.Pending)
	for _, vehicle := range state.Simulation.Vehicles {
		addOrders(fromHTTP, vehicle.Riders)
	}
	addOrders(fromStream, accepted.State.Simulation.Pending)
	for _, vehicle := range accepted.State.Simulation.Vehicles {
		addOrders(fromStream, vehicle.Riders)
	}
	return fromHTTP, fromStream
}

// expectGeneratedConsent runs s until the demand run offers a new order, and
// checks the consent of that order in the decoded HTTP and stream states.
func expectGeneratedConsent(t *testing.T, s *Session, want sim.SharingConsent) {
	t.Helper()
	fromHTTP, _ := wireOrders(t, s)
	newest := 0
	for id := range maps.Keys(fromHTTP) {
		newest = max(newest, id)
	}
	generated := s.demand.state.Generated
	for range 60 * sim.TicksPerSecond {
		s.advance()
		if s.demand.state.Generated != generated {
			break
		}
	}
	if s.demand.state.Generated == generated {
		t.Fatalf("the demand run offered no order: %+v", s.demand.state)
	}
	fromHTTP, fromStream := wireOrders(t, s)
	ids := slices.Sorted(maps.Keys(fromHTTP))
	if len(ids) == 0 || ids[len(ids)-1] <= newest {
		t.Fatalf("no new order after order %d: %v", newest, ids)
	}
	id := ids[len(ids)-1]
	if fromHTTP[id] != want || fromStream[id] != want {
		t.Fatalf("order %d: HTTP consent %q, stream consent %q, want %q", id, fromHTTP[id], fromStream[id], want)
	}
}

// lifecycleProject offers one order each second on the example network.
func lifecycleProject(consent sim.SharingConsent) project.Config {
	config := project.Default()
	config.SharedRidePartyLimit, config.GeneratedOrderConsent = 8, consent
	config.Demand = DemandConfig{Enabled: true, PerMinute: 60, Pattern: "market", Seed: 3}
	return config
}

// lifecycleRun is one session of the lifecycle table. A restart step
// replaces the session and its client.
type lifecycleRun struct {
	session *Session
	client  *testClient
}

// restart saves the session and restores the file in a new session.
func (run *lifecycleRun) restart(t *testing.T) {
	t.Helper()
	file := sessionStateFile(t, run.session)
	file.RestoreAttempts = 0
	data := encodeTestState(t, file)
	raw := decompressTestJSON(t, data)
	if want := run.session.project.GeneratedOrderConsent != ""; bytes.Contains(raw, []byte(`"generatedOrderConsent"`)) != want {
		t.Fatalf("saved member present = %t, want %t", !want, want)
	}
	loaded, err := run.session.loadState(loadInput{data: data, steps: realRestoreSteps()})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.config.GeneratedOrderConsent != run.session.project.GeneratedOrderConsent {
		t.Fatalf("restored project consent %q", loaded.config.GeneratedOrderConsent)
	}
	restarted := newSession(nil, nil)
	restarted.installRestored(loaded)
	t.Cleanup(restarted.Close)
	run.session, run.client = restarted, newTestClient(restarted, "restarted")
}

// applyConsent applies the project of the session with another consent.
func (run *lifecycleRun) applyConsent(t *testing.T, consent sim.SharingConsent) {
	t.Helper()
	config := project.Clone(run.session.project)
	config.GeneratedOrderConsent = consent
	run.client.mustApply(t, Command{Action: "pause", Paused: true})
	run.client.mustApply(t, Command{Action: "project", ProjectRevision: run.session.Project().Revision, Project: &config})
	run.client.mustApply(t, Command{Action: "pause", Paused: false})
}

// TestGeneratedConsentLifecycle checks that a new generated order has the
// consent of the project after each step that builds or replaces the demand
// run.
func TestGeneratedConsentLifecycle(t *testing.T) {
	t.Parallel()
	shared, private := sim.SharedConsent, sim.PrivateConsent
	for _, test := range []struct {
		name  string
		start sim.SharingConsent
		steps func(*testing.T, *lifecycleRun)
	}{
		{"start", shared, func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, shared)
		}},
		{"start without the member", "", func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, private)
		}},
		{"consent-only apply", "", func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, private)
			run.applyConsent(t, shared)
			expectGeneratedConsent(t, run.session, shared)
			run.applyConsent(t, "")
			expectGeneratedConsent(t, run.session, private)
		}},
		{"save and restore", shared, func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, shared)
			run.restart(t)
			expectGeneratedConsent(t, run.session, shared)
		}},
		{"restore without the member", "", func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, private)
			run.restart(t)
			expectGeneratedConsent(t, run.session, private)
		}},
		{"restore then a live demand change", shared, func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, shared)
			run.restart(t)
			run.client.mustApply(t, Command{Action: "demand", Demand: DemandConfig{Enabled: true, PerMinute: 120, Pattern: "market", Seed: 9}})
			expectGeneratedConsent(t, run.session, shared)
		}},
		{"reset", shared, func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, shared)
			run.client.mustApply(t, Command{Action: "reset"})
			expectGeneratedConsent(t, run.session, shared)
		}},
		{"demo exit then enable", shared, func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			run.client.mustApply(t, Command{Action: "demo"})
			// The run of a demo is disabled, and the demand command replaces it
			// before it offers an order, so no order can show its consent.
			if got := run.session.demand.consent; got != shared {
				t.Fatalf("the demo run has consent %q", got)
			}
			for range 600 * sim.TicksPerSecond {
				run.session.advance()
				if !run.session.simulation.Snapshot().Demo {
					break
				}
			}
			if run.session.simulation.Snapshot().Demo {
				t.Fatal("the demo did not end")
			}
			run.client.mustApply(t, Command{Action: "demand", Demand: DemandConfig{Enabled: true, PerMinute: 60, Pattern: "market", Seed: 3}})
			expectGeneratedConsent(t, run.session, shared)
		}},
		{"rewind", shared, func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			expectGeneratedConsent(t, run.session, shared)
			checkpoint := run.client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
			advanceTicks(run.session, 2*sim.TicksPerSecond)
			run.client.mustApply(t, Command{Action: "pause", Paused: true})
			run.client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
			run.client.mustApply(t, Command{Action: "pause", Paused: false})
			expectGeneratedConsent(t, run.session, shared)
		}},
		{"rewind across an apply", shared, func(t *testing.T, run *lifecycleRun) {
			t.Helper()
			checkpoint := run.client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
			run.applyConsent(t, private)
			expectGeneratedConsent(t, run.session, private)
			run.client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
			run.client.mustApply(t, Command{Action: "pause", Paused: false})
			expectGeneratedConsent(t, run.session, shared)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := NewWithProject(lifecycleProject(test.start))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			run := &lifecycleRun{session: s, client: newTestClient(s, "lifecycle")}
			test.steps(t, run)
		})
	}
}

// TestGeneratedConsentDigest checks digest extension 15. A command with the
// member hashes the input of the command without the member, and then a
// trailer with the N and the value of the member.
func TestGeneratedConsentDigest(t *testing.T) {
	t.Parallel()
	const field = "Project.GeneratedOrderConsent"
	if !slices.Contains(digestExtensions, digestExtension{n: 15, path: field}) {
		t.Fatalf("no registry entry 15 for %s", field)
	}
	plain := project.Default()
	base := Command{Action: "project", Client: "digest", Sequence: 1, Epoch: testStateEpoch, ProjectRevision: 1, Project: &plain}
	for _, consent := range []sim.SharingConsent{sim.PrivateConsent, sim.SharedConsent} {
		marked := base
		markedProject := project.Clone(plain)
		markedProject.GeneratedOrderConsent = consent
		marked.Project = &markedProject
		var encoded recordingHash
		writer := digestWriter{hash: &encoded}
		writer.value(reflect.ValueOf(consent))
		trailer := slices.Concat(binary.AppendUvarint(nil, 1), binary.AppendUvarint(nil, 15), encoded.Bytes())
		if got, want := digestInput(marked), slices.Concat(digestInput(base), trailer); !bytes.Equal(got, want) {
			t.Fatalf("consent %q: the input is not the input without the member and the trailer", consent)
		}
		if digestCommand(base).matches(digestCommand(marked)) {
			t.Fatalf("consent %q: the member did not change the digest", consent)
		}
	}
}
