// Package session owns the shared simulation and serializes commands with clock ticks.
package session

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

// QueueLimit bounds pending work from both manual and generated requests.
const QueueLimit = 200

// State is an authoritative, immutable copy sent to observers.
type State struct {
	Epoch      string       `json:"epoch"`
	Revision   uint64       `json:"revision"`
	Network    sim.Network  `json:"network"`
	Simulation sim.Snapshot `json:"simulation"`
	Speed      int          `json:"speed"`
	Demand     DemandState  `json:"demand"`
}

// Command describes an explicit mutation with a per-client sequence for safe retries.
type Command struct {
	Client      string       `json:"client"`
	Sequence    uint64       `json:"sequence"`
	Epoch       string       `json:"epoch"`
	Action      string       `json:"action"`
	Origin      string       `json:"origin,omitempty"`
	Destination string       `json:"destination,omitempty"`
	Paused      bool         `json:"paused,omitempty"`
	Speed       int          `json:"speed,omitempty"`
	Demand      DemandConfig `json:"demand,omitempty"`
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

// Session contains one fleet and one simulation clock. Use Run once per session.
type Session struct {
	mu         sync.Mutex
	simulation *sim.Simulation
	network    sim.Network
	epoch      string
	revision   uint64
	speed      int
	demand     demandRun
	receipts   map[string]receipt
}

// New creates the supplied fleet with demand disabled.
func New() (*Session, error) {
	network := sim.Example()
	simulation, err := sim.NewFleet(network, []sim.Placement{{ID: "01", StationID: "harbor"}, {ID: "02", StationID: "garden"}})
	if err != nil {
		return nil, fmt.Errorf("create shared fleet: %w", err)
	}
	return &Session{simulation: simulation, network: network, epoch: rand.Text(), speed: 1, demand: newDemand(), receipts: make(map[string]receipt)}, nil
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
		s.simulation.Step()
		s.demand.step(s.simulation)
	}
	s.revision++
}

// State returns a detached snapshot safe for concurrent observers.
func (s *Session) State() State { s.mu.Lock(); defer s.mu.Unlock(); return s.state() }

func (s *Session) state() State {
	network := s.network
	network.Nodes = slices.Clone(network.Nodes)
	network.Lanes = slices.Clone(network.Lanes)
	network.Stations = slices.Clone(network.Stations)
	for i := range network.Stations {
		network.Stations[i].Berths = slices.Clone(network.Stations[i].Berths)
	}
	return State{Epoch: s.epoch, Revision: s.revision, Network: network, Simulation: s.simulation.Snapshot(), Speed: s.speed, Demand: s.demand.state}
}

// Apply serializes commands. The latest sequence can be retried; older sequences never replay.
func (s *Session) Apply(command Command) Reply {
	s.mu.Lock()
	defer s.mu.Unlock()
	reply := Reply{}
	if command.Epoch != s.epoch {
		reply.Error = "The server session changed. Review the current state and try again."
	} else if command.Client == "" || len(command.Client) > 100 || command.Sequence == 0 {
		reply.Error = "Invalid client or command sequence."
	} else {
		previous, exists := s.receipts[command.Client]
		switch {
		case exists && command.Sequence < previous.command.Sequence:
			reply.Error = "This command has expired. Review the current state."
		case exists && command.Sequence == previous.command.Sequence:
			if previous.command != command {
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
			s.receipts[command.Client] = receipt{command: command, orderID: reply.OrderID, errorText: reply.Error}
		}
	}
	reply.State = s.state()
	return reply
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
		s.simulation.Reset()
		s.speed = 1
		s.demand = newDemand()
	case "demo":
		if err := s.simulation.StartDemo(); err != nil {
			return 0, err
		}
		s.speed = 1
		s.demand = newDemand()
	case "demand":
		if s.simulation.Snapshot().Demo {
			return 0, errors.New("wait for the demo to finish before changing demand")
		}
		if err := s.demand.configure(command.Demand); err != nil {
			return 0, err
		}
	default:
		return 0, errors.New("unknown command")
	}
	return 0, nil
}
