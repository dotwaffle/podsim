// Package session owns the shared simulation and serializes commands with clock ticks.
package session

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// QueueLimit bounds pending work from both manual and generated requests.
const QueueLimit = 200

// State is an authoritative, immutable copy sent to observers.
type State struct {
	Epoch           string       `json:"epoch"`
	Revision        uint64       `json:"revision"`
	ProjectRevision uint64       `json:"projectRevision"`
	Generation      uint64       `json:"generation"`
	Redistribution  bool         `json:"redistribution"`
	Network         sim.Network  `json:"network"`
	Simulation      sim.Snapshot `json:"simulation"`
	Speed           int          `json:"speed"`
	Demand          DemandState  `json:"demand"`
}

// ProjectState contains a copied project and its edit revision.
type ProjectState struct {
	Revision uint64         `json:"revision"`
	Project  project.Config `json:"project"`
}

// Command describes an explicit mutation with a per-client sequence for safe retries.
type Command struct {
	Client          string          `json:"client"`
	Sequence        uint64          `json:"sequence"`
	Epoch           string          `json:"epoch"`
	Action          string          `json:"action"`
	Origin          string          `json:"origin,omitempty"`
	Destination     string          `json:"destination,omitempty"`
	Paused          bool            `json:"paused,omitempty"`
	Speed           int             `json:"speed,omitempty"`
	Demand          DemandConfig    `json:"demand,omitzero"`
	Project         *project.Config `json:"project,omitempty"`
	ProjectRevision uint64          `json:"projectRevision,omitempty"`
}

// Reply acknowledges one command and includes current authoritative state.
type Reply struct {
	State   State  `json:"state"`
	OrderID int    `json:"orderID,omitempty"`
	Error   string `json:"error,omitempty"`
}

type receipt struct {
	command   Command
	orderID   int
	errorText string
}

// Option configures a session constructor.
type Option func(*Session)

// WithProjectSaver saves project changes before the session applies them.
func WithProjectSaver(save func(project.Config) error) Option {
	return func(session *Session) { session.saveProject = save }
}

// Session contains one fleet and one simulation clock. Use Run once per session.
type Session struct {
	mu              sync.Mutex
	simulation      *sim.Simulation
	project         project.Config
	epoch           string
	revision        uint64
	projectRevision uint64
	generation      uint64
	speed           int
	demand          demandRun
	receipts        map[string]receipt
	saveProject     func(project.Config) error
}

// New creates the supplied example project with demand disabled.
func New() (*Session, error) { return NewWithProject(project.Default()) }

// NewWithProject validates and copies a project into a new session.
func NewWithProject(config project.Config, options ...Option) (*Session, error) {
	if err := project.Validate(config); err != nil {
		return nil, err
	}
	owned := project.Clone(config)
	simulation, err := sim.NewFleet(owned.Network, owned.Fleet)
	if err != nil {
		return nil, fmt.Errorf("create shared fleet: %w", err)
	}
	session := &Session{
		simulation:      simulation,
		project:         owned,
		epoch:           rand.Text(),
		projectRevision: 1,
		generation:      1,
		speed:           1,
		demand:          newDemand(demandInput{config: owned.Demand, network: owned.Network, profiles: owned.DemandProfiles}),
		receipts:        make(map[string]receipt),
	}
	for _, option := range options {
		option(session)
	}
	session.configureRedistribution()
	return session, nil
}

// Run advances the shared clock until cancellation. Browsers never advance it.
func (s *Session) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Second / sim.TicksPerSecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.advance()
		}
	}
}

func (s *Session) advance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.simulation.Snapshot().Paused {
		return
	}
	for range s.speed {
		wasDemo := s.simulation.Snapshot().Demo
		s.simulation.Step()
		if wasDemo && !s.simulation.Snapshot().Demo {
			s.configureRedistribution()
		}
		s.demand.step(s.simulation)
	}
	s.revision++
}

// State returns a detached snapshot safe for concurrent observers.
func (s *Session) State() State { s.mu.Lock(); defer s.mu.Unlock(); return s.state() }

func (s *Session) state() State {
	network := project.CloneNetwork(s.project.Network)
	return State{
		Epoch:           s.epoch,
		Revision:        s.revision,
		ProjectRevision: s.projectRevision,
		Generation:      s.generation,
		Redistribution:  s.project.Redistribution && !s.simulation.Snapshot().Demo,
		Network:         network,
		Simulation:      s.simulation.Snapshot(),
		Speed:           s.speed,
		Demand:          s.demand.state,
	}
}

// Project returns a detached project safe for concurrent observers.
func (s *Session) Project() ProjectState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ProjectState{Revision: s.projectRevision, Project: project.Clone(s.project)}
}

// Apply serializes commands. The latest sequence can be retried; older sequences never replay.
func (s *Session) Apply(command Command) Reply {
	command = cloneCommand(command)
	s.mu.Lock()
	defer s.mu.Unlock()
	reply := Reply{}
	switch {
	case command.Epoch != s.epoch:
		reply.Error = "The server session changed. Review the current state and try again."
	case command.Client == "" || len(command.Client) > 100 || command.Sequence == 0:
		reply.Error = "Invalid client or command sequence."
	default:
		previous, exists := s.receipts[command.Client]
		switch {
		case exists && command.Sequence < previous.command.Sequence:
			reply.Error = "This command has expired. Review the current state."
		case exists && command.Sequence == previous.command.Sequence:
			if !reflect.DeepEqual(previous.command, command) {
				reply.Error = "This sequence was already used for another command."
			} else {
				reply.OrderID, reply.Error = previous.orderID, previous.errorText
			}
		case !exists && len(s.receipts) >= 1024:
			reply.Error = "The session client limit was reached. Restart the server."
		default:
			orderID, err := s.apply(command)
			reply.OrderID = orderID
			if err != nil {
				reply.Error = err.Error()
			} else {
				s.revision++
			}
			s.receipts[command.Client] = receipt{command: cloneCommand(command), orderID: reply.OrderID, errorText: reply.Error}
		}
	}
	reply.State = s.state()
	return reply
}

func cloneCommand(command Command) Command {
	if command.Project != nil {
		command.Project = new(project.Clone(*command.Project))
	}
	return command
}

func (s *Session) apply(command Command) (int, error) {
	switch command.Action {
	case "trip":
		state := s.simulation.Snapshot()
		if state.Demo {
			return 0, errors.New("wait for the demo to finish before requesting a journey")
		}
		if len(state.Pending) >= QueueLimit {
			return 0, errors.New("the order queue is full; try again after a pickup")
		}
		if err := s.simulation.RequestTrip(command.Origin, command.Destination); err != nil {
			return 0, err
		}
		return s.simulation.Snapshot().Submitted, nil
	case "pause":
		s.simulation.SetPaused(command.Paused)
	case "speed":
		if command.Speed != 1 && command.Speed != 2 && command.Speed != 4 && command.Speed != 8 {
			return 0, errors.New("speed must be 1, 2, 4, or 8")
		}
		s.speed = command.Speed
	case "reset":
		paused := s.simulation.Snapshot().Paused
		s.simulation.Reset()
		s.simulation.SetPaused(paused)
		s.speed = 1
		s.demand = newDemand(demandInput{config: s.project.Demand, network: s.project.Network, profiles: s.project.DemandProfiles})
		s.configureRedistribution()
		s.generation++
	case "demo":
		defaults := project.Default()
		if !reflect.DeepEqual(s.project.Network, defaults.Network) || !reflect.DeepEqual(s.project.Fleet, defaults.Fleet) {
			return 0, errors.New("the supplied demo is available only for the example project")
		}
		if err := s.simulation.StartDemo(); err != nil {
			return 0, err
		}
		s.speed = 1
		disabled := s.project.Demand
		disabled.Enabled = false
		s.demand = newDemand(demandInput{config: disabled, network: s.project.Network, profiles: s.project.DemandProfiles})
		s.generation++
	case "demand":
		if s.simulation.Snapshot().Demo {
			return 0, errors.New("wait for the demo to finish before changing demand")
		}
		demandContext := project.DemandContext{Network: s.project.Network, Profiles: s.project.DemandProfiles}
		if err := project.ValidateDemand(command.Demand, demandContext); err != nil {
			return 0, err
		}
		updated := project.Clone(s.project)
		updated.Demand = command.Demand
		if err := s.save(updated); err != nil {
			return 0, err
		}
		if err := s.demand.configure(demandInput{config: command.Demand, network: s.project.Network, profiles: s.project.DemandProfiles}); err != nil {
			return 0, err
		}
		s.project = updated
		s.configureRedistribution()
		s.projectRevision++
	case "project":
		return 0, s.applyProject(command)
	default:
		return 0, errors.New("unknown command")
	}
	return 0, nil
}

func (s *Session) applyProject(command Command) error {
	if !s.simulation.Snapshot().Paused {
		return errors.New("pause the simulation before applying a project")
	}
	if command.ProjectRevision != s.projectRevision {
		return errors.New("the project changed; reload it before applying edits")
	}
	if command.Project == nil {
		return errors.New("project command requires a project")
	}
	config := project.Clone(*command.Project)
	if err := project.Validate(config); err != nil {
		return err
	}
	candidate, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		return fmt.Errorf("create project fleet: %w", err)
	}
	candidate.SetPaused(true)
	if err := s.save(config); err != nil {
		return err
	}
	s.project = config
	s.simulation = candidate
	s.speed = 1
	s.demand = newDemand(demandInput{config: config.Demand, network: config.Network, profiles: config.DemandProfiles})
	s.configureRedistribution()
	s.projectRevision++
	s.generation++
	return nil
}

func (s *Session) save(config project.Config) error {
	if s.saveProject == nil {
		return nil
	}
	if err := s.saveProject(project.Clone(config)); err != nil {
		return fmt.Errorf("save project: %w", err)
	}
	return nil
}

// Expected pickup weights follow the configured arrival pattern.
func (s *Session) configureRedistribution() {
	demand := newDemand(demandInput{config: s.project.Demand, network: s.project.Network, profiles: s.project.DemandProfiles})
	// Project validation guarantees at least two passenger stations and valid settings.
	if err := s.simulation.SetDemandWeights(demand.pickupWeights); err != nil {
		panic(err)
	}
	s.simulation.SetRedistribution(s.project.Redistribution)
}
