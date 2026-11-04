package view

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestButtonsFollowOperations takes the simulation of each session change
// into a game, and checks the Fault and Emergency buttons. Both show only
// when the frame has the operations on. They do not read the demo flag: the
// parked demo pods stay after the demo ends, until a reset.
func TestButtonsFollowOperations(t *testing.T) {
	t.Parallel()
	s, err := session.NewWithProject(project.Default())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	sequence := uint64(0)
	apply := func(command session.Command) {
		t.Helper()
		sequence++
		command.Client, command.Sequence, command.Epoch = "operations", sequence, s.State().Epoch
		if reply := s.Apply(command); reply.Error != "" {
			t.Fatalf("%s: %s", command.Action, reply.Error)
		}
	}
	check := func(stage string, simulation sim.Snapshot, want bool) {
		t.Helper()
		game := faultTestGame(t)
		game.state.Simulation = simulation
		game.selected = 0
		boardParty(&game.state.Simulation.Vehicles[0])
		if _, shown := game.faultButton(game.state.Simulation); shown != want {
			t.Errorf("%s: fault button shown %t, want %t", stage, shown, want)
		}
		if _, shown := game.emergencyButton(game.state.Simulation); shown != want {
			t.Errorf("%s: emergency button shown %t, want %t", stage, shown, want)
		}
	}
	check("start", s.State().Simulation, true)
	sequence++
	reply := s.Apply(session.Command{Client: "operations", Sequence: sequence, Epoch: s.State().Epoch, Action: "checkpoint"})
	if reply.Error != "" {
		t.Fatalf("checkpoint: %s", reply.Error)
	}
	checkpoint := reply.Checkpoint
	apply(session.Command{Action: "demo"})
	demo := s.State().Simulation
	check("demo", demo, false)
	ended := demo
	ended.Demo = false
	check("demo end", ended, false)
	apply(session.Command{Action: "reset"})
	check("reset", s.State().Simulation, true)
	apply(session.Command{Action: "demo"})
	check("second demo", s.State().Simulation, false)
	apply(session.Command{Action: "rewind", Checkpoint: checkpoint})
	check("rewind", s.State().Simulation, true)
	apply(session.Command{Action: "demo"})
	apply(session.Command{Action: "pause", Paused: true})
	next := project.Default()
	next.Name = "Operations project"
	apply(session.Command{Action: "project", Project: &next, ProjectRevision: s.Project().Revision})
	check("project apply", s.State().Simulation, true)
}
