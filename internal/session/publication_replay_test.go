package session

import (
	"context"
	"log/slog"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// replayCaptureTicks is the tick distance between two captures, 10 captures
// a second at 30 Hz.
const replayCaptureTicks = 3

// replayPayload is the JSON of one envelope that the publisher retained.
type replayPayload struct {
	kind string
	json []byte
}

// replay drives one session and a real publisher with a fixed script. The same
// project, script, and capture ticks give the same envelopes, apart from the
// random stream ID.
type replay struct {
	t       *testing.T
	session *Session
	client  *testClient
	p       *statePublisher
	// checkpoints holds the IDs of the save points of the script.
	checkpoints []uint64
	// errors holds the error text of each command, in order.
	errors []string
}

// replayCommands maps a driven tick to the commands of the script. A driven
// tick counts each advance call, also when the session is paused.
type replayCommands map[int]func(*replay)

func newReplay(t *testing.T, config project.Config) *replay {
	t.Helper()
	s, err := NewWithProject(config, WithLogger(slog.New(slog.DiscardHandler)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	// These are random in a server. Another tree must see the same values.
	s.serverStart, s.epoch = "replay-start", "replay-epoch"
	return &replay{t: t, session: s, client: newTestClient(s, "replay"),
		p: &statePublisher{session: s, clients: map[*streamSubscriber]bool{}, wake: make(chan struct{}, 1)}}
}

// command applies command and keeps its error text. The script continues after
// a refusal, because a refusal depends on the state and it is part of the
// replay.
func (r *replay) command(command Command) Reply {
	reply := r.session.Apply(r.client.next(command))
	r.errors = append(r.errors, reply.Error)
	return reply
}

// movingPod returns the first pod on a lane, or "".
func (r *replay) movingPod() string {
	frame, err := r.session.presentationFrame()
	if err != nil {
		r.t.Fatal(err)
	}
	for _, v := range frame.State.Simulation.Vehicles {
		if v.Pod.LaneID != "" {
			return v.Pod.ID
		}
	}
	return ""
}

func (r *replay) checkpoint() {
	if reply := r.command(Command{Action: "checkpoint"}); reply.Checkpoint != 0 {
		r.checkpoints = append(r.checkpoints, reply.Checkpoint)
	}
}

func (r *replay) fault() {
	r.command(Command{Action: "fault", PodID: r.movingPod(), DurationSeconds: new(int64(20))})
}

func (r *replay) emergency() {
	r.command(Command{Action: "emergency", PodID: r.movingPod()})
}

func (r *replay) rewind(index int) {
	r.command(Command{Action: "rewind", Checkpoint: r.checkpoints[index]})
}

func (r *replay) pause(paused bool) {
	r.command(Command{Action: "pause", Paused: paused})
}

func (r *replay) applyProject(config project.Config) {
	r.command(Command{Action: "project", Project: &config, ProjectRevision: r.session.Frame().ProjectRevision})
}

// run drives ticks ticks. It calls publish at each capture tick, and each
// tenth capture also asks for a full frame, as a client that needs one does.
// visit gets the new payloads of each capture.
func (r *replay) run(ticks int, commands replayCommands, visit func(tick int, payloads []replayPayload)) {
	r.t.Helper()
	ctx := context.Background()
	captures := 0
	for tick := range ticks {
		if command := commands[tick]; command != nil {
			command(r)
		}
		r.session.advance()
		if tick%replayCaptureTicks != 0 {
			continue
		}
		captures++
		before := r.p.metrics
		if err := r.p.publish(ctx, captures%10 == 0, true); err != nil {
			r.t.Fatal(err)
		}
		visit(tick, r.newPayloads(before))
	}
}

func (r *replay) newPayloads(before StreamMetrics) []replayPayload {
	r.t.Helper()
	var payloads []replayPayload
	inflate := func(kind string, b *streamPayload) {
		data, err := InflateStream(b.data)
		if err != nil {
			r.t.Fatal(err)
		}
		payloads = append(payloads, replayPayload{kind: kind, json: data})
	}
	if r.p.metrics.Delta > before.Delta {
		inflate("delta", r.p.history[len(r.p.history)-1])
	}
	if r.p.metrics.Full > before.Full {
		inflate("full", r.p.full)
	}
	return payloads
}

// replayCase is one run of the differential proof.
type replayCase struct {
	name     string
	config   func(*testing.T) project.Config
	ticks    int
	commands func(unit int, other project.Config) replayCommands
	other    func(*testing.T) project.Config
}

// replayCases returns the runs. minutes scales each run. The incident run
// holds the commands that replace or rewind the routes of the fleet.
func replayCases(minutes int) []replayCase {
	ticks := minutes * 60 * sim.TicksPerSecond
	return []replayCase{
		{name: "rail-hub", ticks: ticks, config: func(t *testing.T) project.Config {
			t.Helper()
			return soakPreset(t, "rail-hub")
		}},
		{name: "london-central", ticks: ticks, config: func(t *testing.T) project.Config {
			t.Helper()
			return soakPreset(t, "london-central")
		}},
		{name: "incidents", ticks: 2 * ticks, config: func(t *testing.T) project.Config {
			t.Helper()
			return soakProject(t, "rail-hub", soakArms["incidents"], 1)
		}, other: func(t *testing.T) project.Config {
			t.Helper()
			return soakPreset(t, "london-central")
		}, commands: incidentReplayCommands},
	}
}

// incidentReplayCommands divides the run in 12 units. Rewind and reset use the
// same route versions again with other routes. The project apply replaces the
// network, and the last rewind brings the first network back.
func incidentReplayCommands(unit int, other project.Config) replayCommands {
	return replayCommands{
		unit:         func(r *replay) { r.checkpoint(); r.fault() },
		2 * unit:     (*replay).emergency,
		3 * unit:     func(r *replay) { r.checkpoint(); r.fault() },
		4 * unit:     func(r *replay) { r.rewind(0) },
		4*unit + 30:  func(r *replay) { r.pause(false) },
		5 * unit:     func(r *replay) { r.rewind(1) },
		5*unit + 30:  func(r *replay) { r.pause(false) },
		6 * unit:     func(r *replay) { r.command(Command{Action: "reset"}) },
		7 * unit:     func(r *replay) { r.pause(true) },
		7*unit + 30:  func(r *replay) { r.applyProject(other) },
		7*unit + 60:  func(r *replay) { r.pause(false) },
		9 * unit:     func(r *replay) { r.checkpoint(); r.fault() },
		10 * unit:    func(r *replay) { r.rewind(0) },
		10*unit + 30: func(r *replay) { r.pause(false) },
		11 * unit:    func(r *replay) { r.command(Command{Action: "reset"}) },
	}
}

// start returns the replay of the case and its script.
func (c replayCase) start(t *testing.T) (*replay, replayCommands) {
	t.Helper()
	r := newReplay(t, c.config(t))
	if c.commands == nil {
		return r, nil
	}
	return r, c.commands(c.ticks/12, c.other(t))
}
