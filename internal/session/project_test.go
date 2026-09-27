package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func customProject() project.Config {
	config := project.Default()
	config.Name = "Renamed stations"
	renames := map[string]string{"harbor": "alpha", "garden": "beta", "market": "gamma"}
	for i := range config.Network.Stations {
		if replacement := renames[config.Network.Stations[i].ID]; replacement != "" {
			config.Network.Stations[i].ID = replacement
			config.Network.Stations[i].Name = replacement
		}
	}
	for i := range config.Network.Lanes {
		if replacement := renames[config.Network.Lanes[i].StationID]; replacement != "" {
			config.Network.Lanes[i].StationID = replacement
		}
	}
	for i := range config.Fleet {
		config.Fleet[i].StationID = renames[config.Fleet[i].StationID]
	}
	config.Demand = DemandConfig{Enabled: true, PerMinute: 60, Pattern: "destination", Seed: 7, Destination: "gamma"}
	return config
}

func applyCustomProject(t *testing.T, session *Session, config project.Config) Reply {
	t.Helper()
	pause := commandFor(session, "pause")
	pause.Paused = true
	if reply := session.Apply(pause); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	command := commandFor(session, "project")
	command.Sequence = 2
	command.ProjectRevision = session.State().ProjectRevision
	command.Project = &config
	return session.Apply(command)
}

// TestProjectMutationGuardsPreserveState checks that a rejected project
// command changes nothing. Only a stale project revision to a paused session
// gets StaleProject. An exact retry gets the stored rejection.
func TestProjectMutationGuardsPreserveState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(*Session, *Command)
		want    CommandErrorCode
	}{
		{"running", func(_ *Session, _ *Command) {}, CommandRejected},
		{"stale revision while running", func(_ *Session, command *Command) {
			command.ProjectRevision++
		}, CommandRejected},
		{"stale revision", func(session *Session, command *Command) {
			session.simulation.SetPaused(true)
			command.ProjectRevision++
		}, StaleProject},
		{"missing project", func(session *Session, command *Command) {
			session.simulation.SetPaused(true)
			command.Project = nil
		}, CommandRejected},
		{"invalid project", func(session *Session, command *Command) {
			session.simulation.SetPaused(true)
			command.Project.Version = 2
		}, CommandRejected},
		{"failed project save", func(session *Session, _ *Command) {
			session.simulation.SetPaused(true)
			session.saveProject = func(project.Config) error { return errors.New("disk full") }
		}, CommandRejected},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			session := newTestSession(t)
			config := customProject()
			command := commandFor(session, "project")
			command.ProjectRevision = session.Project().Revision
			command.Project = &config
			test.prepare(session, &command)
			beforeState, beforeProject := session.State(), session.Project()
			reply := session.Apply(command)
			if reply.Error == "" || reply.ErrorCode != test.want {
				t.Fatalf("reply = %+v, want error code %s", reply, test.want)
			}
			if !reflect.DeepEqual(beforeState, session.State()) || !reflect.DeepEqual(beforeProject, session.Project()) {
				t.Fatal("rejected mutation changed session")
			}
			if retry := session.Apply(command); !reflect.DeepEqual(retry, reply) {
				t.Fatalf("retry = %+v, want the stored rejection %+v", retry, reply)
			}
		})
	}
}

// TestStaleProjectOverHTTP checks that a stale project command gets HTTP 409
// and the stale_project error code in the JSON reply.
func TestStaleProjectOverHTTP(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	session.simulation.SetPaused(true)
	config := customProject()
	command := commandFor(session, "project")
	command.ProjectRevision = session.Project().Revision + 1
	command.Project = &config
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/api/command", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	session.Handler(t.TempDir()).ServeHTTP(response, request)
	var reply struct {
		ErrorCode string `json:"errorCode"`
	}
	if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || reply.ErrorCode != "stale_project" {
		t.Fatalf("status %d, error code %q, want %d and stale_project", response.Code, reply.ErrorCode, http.StatusConflict)
	}
}

// TestProjectServerStart checks the serverStart member of a project command
// over HTTP. A command with the server start ID of the session or with no
// ID applies. A command with the ID of an earlier server process gets HTTP
// 409 and session_changed, and the session keeps its state and project.
// Other actions ignore the member.
func TestProjectServerStart(t *testing.T) {
	t.Parallel()
	const start = "0123456789abcdef"
	const earlier = "fedcba9876543210"
	tests := []struct {
		name        string
		action      string
		serverStart string
		wantStatus  int
		wantCode    CommandErrorCode
	}{
		{"same server start", "project", start, http.StatusOK, ""},
		{"no server start", "project", "", http.StatusOK, ""},
		{"earlier server start", "project", earlier, http.StatusConflict, SessionChanged},
		{"pause with an earlier server start", "pause", earlier, http.StatusOK, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			session := newTestSession(t)
			session.serverStart = start
			session.simulation.SetPaused(true)
			config := customProject()
			command := commandFor(session, test.action)
			if test.action == "project" {
				command.ProjectRevision = session.Project().Revision
				command.Project = &config
			} else {
				command.Paused = true
			}
			command.ServerStart = test.serverStart
			body, err := json.Marshal(command)
			if err != nil {
				t.Fatal(err)
			}
			beforeState, beforeProject := session.State(), session.Project()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://example.com/api/command", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			session.Handler(t.TempDir()).ServeHTTP(response, request)
			var reply Reply
			if err := json.NewDecoder(response.Body).Decode(&reply); err != nil {
				t.Fatal(err)
			}
			if response.Code != test.wantStatus || reply.ErrorCode != test.wantCode {
				t.Fatalf("status %d, reply %+v, want status %d and error code %q", response.Code, reply, test.wantStatus, test.wantCode)
			}
			afterState, afterProject := session.State(), session.Project()
			switch {
			case test.wantCode != "":
				if !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(beforeProject, afterProject) {
					t.Fatal("rejected project command changed the session")
				}
			case test.action != "project":
				if !reflect.DeepEqual(beforeProject, afterProject) {
					t.Fatalf("%s command changed the project", test.action)
				}
			case afterProject.Revision != beforeProject.Revision+1 || afterProject.Project.Name != config.Name:
				t.Fatalf("project revision %d, name %q, want revision %d and name %q",
					afterProject.Revision, afterProject.Project.Name, beforeProject.Revision+1, config.Name)
			}
		})
	}
}

func TestProjectApplyIsAtomicDetachedAndIdempotent(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	config := customProject()
	config.SharedRidePartyLimit = 3
	original := project.Clone(config)
	reply := applyCustomProject(t, session, config)
	if reply.Error != "" {
		t.Fatal(reply.Error)
	}
	state := session.State()
	if !state.Simulation.Paused || reply.ProjectRevision != 2 || reply.Generation != 2 || state.Simulation.Submitted != 0 || reply.Revision != state.Revision {
		t.Fatalf("invalid applied state: reply=%+v state=%+v", reply, state)
	}
	if state.Simulation.SharedRidePartyLimit != 3 {
		t.Fatalf("shared ride party limit = %d", state.Simulation.SharedRidePartyLimit)
	}
	config.Name = "caller mutation"
	config.Network.Nodes[0].ID = "caller mutation"
	if got := session.Project().Project; !reflect.DeepEqual(got, original) {
		t.Fatal("session aliases project command")
	}
	retry := commandFor(session, "project")
	retry.Sequence = 2
	retry.ProjectRevision = 1
	retry.Project = &original
	if got := session.Apply(retry); got.Error != "" || got.Revision != reply.Revision || got.ProjectRevision != 2 {
		t.Fatalf("retry changed project: %+v", got)
	}
	detached := session.Project()
	detached.Project.Network.Stations[0].ID = "observer mutation"
	if session.Project().Project.Network.Stations[0].ID == "observer mutation" {
		t.Fatal("project response aliases session")
	}
}

func TestCustomDemandAndResetUseActiveProject(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	config := customProject()
	if reply := applyCustomProject(t, session, config); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	for range 60 {
		session.demand.step(session.simulation)
	}
	snapshot := session.simulation.Snapshot()
	if session.demand.state.Generated != 1 || snapshot.Submitted != 1 {
		t.Fatalf("demand did not use custom stations: %+v", session.demand.state)
	}
	for _, request := range snapshot.Pending {
		if request.To != "gamma" {
			t.Fatalf("demand target = %q", request.To)
		}
	}
	for _, vehicle := range snapshot.Vehicles {
		for _, rider := range vehicle.Riders {
			if rider.To != "gamma" {
				t.Fatalf("active demand target = %q", rider.To)
			}
		}
	}
	reset := commandFor(session, "reset")
	reset.Sequence = 3
	reply := session.Apply(reset)
	state := session.State()
	if reply.Error != "" || reply.Generation != 3 || !state.Simulation.Paused {
		t.Fatalf("reset failed: %+v", reply)
	}
	if !reflect.DeepEqual(state.Demand.Config, config.Demand) || state.Demand.Generated != 0 {
		t.Fatal("reset did not restore configured demand")
	}
	if len(state.Simulation.Vehicles) != len(config.Fleet) {
		t.Fatal("reset did not restore configured fleet")
	}
}

func TestSaveFailureDoesNotChangeProjectOrDemand(t *testing.T) {
	t.Parallel()
	session, err := NewWithProject(project.Default(), WithProjectSaver(func(project.Config) error {
		return errors.New("disk full")
	}))
	if err != nil {
		t.Fatal(err)
	}
	beforeState, beforeProject := session.State(), session.Project()
	demand := commandFor(session, "demand")
	demand.Demand = DemandConfig{Enabled: true, PerMinute: 10, Pattern: "balanced", Seed: 4}
	if reply := session.Apply(demand); reply.Error == "" {
		t.Fatal("reported successful demand save")
	}
	if !reflect.DeepEqual(beforeState, session.State()) || !reflect.DeepEqual(beforeProject, session.Project()) {
		t.Fatal("failed demand save changed session")
	}
	session.simulation.SetPaused(true)
	beforeState, beforeProject = session.State(), session.Project()
	config := customProject()
	command := commandFor(session, "project")
	command.Client = "project"
	command.ProjectRevision = beforeProject.Revision
	command.Project = &config
	if reply := session.Apply(command); reply.Error == "" {
		t.Fatal("reported successful project save")
	}
	if !reflect.DeepEqual(beforeState, session.State()) || !reflect.DeepEqual(beforeProject, session.Project()) {
		t.Fatal("failed project save changed session")
	}
}

func TestProjectEndpointSupportsConcurrentDetachedObservers(t *testing.T) {
	t.Parallel()
	session := newTestSession(t)
	handler := session.Handler(t.TempDir())
	const observers = 12
	results := make([]ProjectState, observers)
	var group sync.WaitGroup
	for i := range results {
		group.Go(func() {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/project", http.NoBody))
			if response.Code != http.StatusOK {
				t.Errorf("status = %d", response.Code)
				return
			}
			if err := json.NewDecoder(response.Body).Decode(&results[i]); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	for i := 1; i < len(results); i++ {
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatal("project observers received different values")
		}
	}
	results[0].Project.Network.Nodes[0].ID = "observer mutation"
	if session.Project().Project.Network.Nodes[0].ID == "observer mutation" {
		t.Fatal("HTTP project response aliases session")
	}
}

// TestRedistributionRestoredAfterDemoAndReset checks the positioning
// settings of the simulation through the demo and a reset. The project
// keeps demand on, but the stream is off after the demo, so the gate then
// reads the mean rate. A reset starts the stream again at its rate. A
// demand command sets the rate, and a project command sets the mode.
func TestRedistributionRestoredAfterDemoAndReset(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Redistribution = true
	config.Demand.Enabled = true
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	checkPositioning := func(name string, mode sim.Positioning, rate int) {
		t.Helper()
		if got, gotRate := shared.simulation.Positioning(), shared.simulation.DemandRate(); got != mode || gotRate != rate {
			t.Fatalf("%s: mode %d and demand rate %d, want %d and %d", name, got, gotRate, mode, rate)
		}
	}
	checkPositioning("start", sim.PositioningGuarded, config.Demand.PerMinute)
	if reply := shared.Apply(commandFor(shared, "demo")); reply.Error != "" || shared.State().Redistribution {
		t.Fatalf("demo policy: %+v", reply)
	}
	checkPositioning("demo", sim.PositioningOff, 0)
	for range 600 * sim.TicksPerSecond {
		shared.advance()
		if state := shared.State(); !state.Simulation.Demo {
			break
		}
	}
	if state := shared.State(); state.Simulation.Demo || !state.Redistribution {
		t.Fatalf("policy not restored: %+v", state.Simulation)
	}
	checkPositioning("demo end", sim.PositioningGuarded, 0)
	reset := commandFor(shared, "reset")
	reset.Sequence = 2
	reply := shared.Apply(reset)
	state := shared.State()
	if reply.Error != "" || !state.Redistribution || state.Simulation.RebalanceMoves != 0 {
		t.Fatalf("reset policy: %+v", reply)
	}
	checkPositioning("reset", sim.PositioningGuarded, config.Demand.PerMinute)
	send := func(command Command) {
		t.Helper()
		if reply := shared.Apply(command); reply.Error != "" {
			t.Fatalf("%s: %s", command.Action, reply.Error)
		}
	}
	demand := commandFor(shared, "demand")
	demand.Sequence = 3
	demand.Demand = config.Demand
	demand.Demand.PerMinute = config.Demand.PerMinute + 2
	send(demand)
	checkPositioning("demand rate", sim.PositioningGuarded, demand.Demand.PerMinute)
	demand.Sequence = 4
	demand.Demand.Enabled = false
	send(demand)
	checkPositioning("demand off", sim.PositioningGuarded, 0)
	shared.simulation.SetPaused(true)
	changed := shared.Project().Project
	changed.Redistribution = false
	changed.Demand.Enabled = true
	apply := commandFor(shared, "project")
	apply.Sequence = 5
	apply.ProjectRevision = shared.Project().Revision
	apply.Project = &changed
	send(apply)
	checkPositioning("project", sim.PositioningOff, changed.Demand.PerMinute)
	reset = commandFor(shared, "reset")
	reset.Sequence = 6
	send(reset)
	checkPositioning("reset without redistribution", sim.PositioningOff, changed.Demand.PerMinute)
}
