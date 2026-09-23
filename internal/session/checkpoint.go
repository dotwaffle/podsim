package session

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// checkpointLimit is the number of save points that a session keeps. A new
// save point removes the oldest one at the limit.
const checkpointLimit = 8

// Checkpoint describes one retained save point. Rewind to it by ID.
// RestoresProject is true when the save point holds a project or demand
// configuration that is different from the current one. A rewind to it
// restores and saves that project.
type Checkpoint struct {
	ID              uint64 `json:"id"`
	Tick            int64  `json:"tick"`
	RestoresProject bool   `json:"restoresProject,omitzero"`
}

// checkpoint holds the state that a rewind restores. Its simulation is a
// private clone. The session never installs or steps it. projectOrigin is
// the project revision that installed project.
//
// project shares its slices and maps with the session project and with
// other save points. The session replaces its project whole and never
// writes to it in place, so the shared value cannot change.
type checkpoint struct {
	id            uint64
	tick          int64
	projectOrigin uint64
	project       project.Config
	simulation    *sim.Simulation
	demand        demandRun
}

// captureCheckpoint saves the simulation, the demand stream, and the
// project. It returns the new ID and the event to log.
func (s *Session) captureCheckpoint() outcome {
	s.lastCheckpoint++
	var evicted uint64
	if len(s.checkpoints) >= checkpointLimit {
		evicted = s.checkpoints[0].id
		// Delete clears the freed slot, so the garbage collector can free the
		// removed simulation.
		s.checkpoints = slices.Delete(s.checkpoints, 0, 1)
	}
	// Copy the demand stream. Do not make a new one from the project. After a
	// demo or a disable, the stream settings can differ from the project.
	entry := checkpoint{
		id:            s.lastCheckpoint,
		tick:          s.simulation.Snapshot().Tick,
		projectOrigin: s.projectOrigin,
		project:       s.project,
		simulation:    s.simulation.Clone(),
		demand:        s.demand.clone(),
	}
	s.checkpoints = append(s.checkpoints, entry)
	return outcome{checkpoint: entry.id, event: &sessionEvent{message: "Saved checkpoint", details: []any{
		slog.Uint64("checkpoint", entry.id),
		slog.Int64("tick", entry.tick),
		slog.Uint64("projectRevision", s.projectRevision),
		slog.Int("retained", len(s.checkpoints)),
		slog.Uint64("evicted", evicted),
	}}}
}

// rewind restores the simulation and the demand stream of a save point and
// pauses the session. When the save point holds a different project, rewind
// saves that project, restores it, and increases the project revision. It
// keeps the epoch, the receipts, the speed, and the save points. It returns
// the event to log.
func (s *Session) rewind(id uint64) (outcome, error) {
	if id == 0 {
		return outcome{}, errors.New("rewind requires a save point")
	}
	index := slices.IndexFunc(s.checkpoints, func(entry checkpoint) bool { return entry.id == id })
	if index < 0 {
		return outcome{}, fmt.Errorf("save point #%d is no longer available", id)
	}
	entry := s.checkpoints[index]
	// Compare the origins, not the revisions. A restore increases the
	// revision, so a second rewind to the same save point does not save again.
	restore := entry.projectOrigin != s.projectOrigin
	// Save before the first change. If the save fails, the session and the
	// project file do not change. Nothing after the save can fail.
	if restore {
		if err := s.save(entry.project); err != nil {
			return outcome{}, err
		}
	}
	fromTick := s.simulation.Snapshot().Tick
	// Install a new clone. Step, Reset, RequestTrip, and StartDemo change the
	// simulation in place, and the save point must stay the same for the next
	// rewind. The clone already has the redistribution settings of the save
	// point and its project, so do not configure them again.
	simulation := entry.simulation.Clone()
	simulation.SetPaused(true)
	s.simulation = simulation
	s.demand = entry.demand.clone()
	if restore {
		// The project revision only increases. Topology caches and editor
		// drafts then see a new revision and never an earlier one again.
		s.project = entry.project
		s.projectOrigin = entry.projectOrigin
		s.projectRevision++
	}
	s.generation++
	return outcome{event: &sessionEvent{message: "Rewound session", details: []any{
		slog.Uint64("checkpoint", id),
		slog.Int64("fromTick", fromTick),
		slog.Int64("toTick", entry.tick),
		slog.Uint64("generation", s.generation),
		slog.Uint64("projectRevision", s.projectRevision),
		slog.Bool("projectRestored", restore),
	}}}, nil
}

// checkpointList returns a new list of the retained save points, oldest
// first. It returns nil when there are none.
func (s *Session) checkpointList() []Checkpoint {
	if len(s.checkpoints) == 0 {
		return nil
	}
	list := make([]Checkpoint, len(s.checkpoints))
	for i, entry := range s.checkpoints {
		list[i] = Checkpoint{ID: entry.id, Tick: entry.tick, RestoresProject: entry.projectOrigin != s.projectOrigin}
	}
	return list
}
