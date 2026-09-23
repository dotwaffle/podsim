package session

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// checkpointLimit is the number of save points that a session keeps. A new
// save point removes the oldest one at the limit.
const checkpointLimit = 8

// Checkpoint describes one retained save point. Rewind to it by ID.
type Checkpoint struct {
	ID   uint64 `json:"id"`
	Tick int64  `json:"tick"`
}

// checkpoint holds the state that a rewind restores. Its simulation is a
// private clone. The session never installs or steps it.
type checkpoint struct {
	id              uint64
	tick            int64
	projectRevision uint64
	simulation      *sim.Simulation
	demand          demandRun
}

// captureCheckpoint saves the simulation and the demand stream, and returns
// the new ID.
func (s *Session) captureCheckpoint() uint64 {
	s.lastCheckpoint++
	if len(s.checkpoints) >= checkpointLimit {
		// Delete clears the freed slot, so the garbage collector can free the
		// removed simulation.
		s.checkpoints = slices.Delete(s.checkpoints, 0, 1)
	}
	// Copy the demand stream. Do not make a new one from the project. After a
	// demo or a disable, the stream settings can differ from the project.
	s.checkpoints = append(s.checkpoints, checkpoint{
		id:              s.lastCheckpoint,
		tick:            s.simulation.Snapshot().Tick,
		projectRevision: s.projectRevision,
		simulation:      s.simulation.Clone(),
		demand:          s.demand.clone(),
	})
	return s.lastCheckpoint
}

// rewind restores the simulation and the demand stream of a save point and
// pauses the session. It keeps the epoch, the receipts, the speed, and the
// save points.
func (s *Session) rewind(id uint64) error {
	if id == 0 {
		return errors.New("rewind requires a save point")
	}
	index := slices.IndexFunc(s.checkpoints, func(entry checkpoint) bool { return entry.id == id })
	if index < 0 {
		return fmt.Errorf("save point #%d is no longer available", id)
	}
	entry := s.checkpoints[index]
	if entry.projectRevision != s.projectRevision {
		return fmt.Errorf("the project or demand settings changed after save point #%d, so a rewind to it is not available", id)
	}
	// Install a new clone. Step, Reset, RequestTrip, and StartDemo change the
	// simulation in place, and the save point must stay the same for the next
	// rewind. The clone already has the redistribution settings of the save
	// point, so do not configure them again.
	simulation := entry.simulation.Clone()
	simulation.SetPaused(true)
	s.simulation = simulation
	s.demand = entry.demand.clone()
	s.generation++
	return nil
}

// checkpointList returns a new list of the retained save points, oldest
// first. It returns nil when there are none.
func (s *Session) checkpointList() []Checkpoint {
	if len(s.checkpoints) == 0 {
		return nil
	}
	list := make([]Checkpoint, len(s.checkpoints))
	for i, entry := range s.checkpoints {
		list[i] = Checkpoint{ID: entry.id, Tick: entry.tick}
	}
	return list
}
