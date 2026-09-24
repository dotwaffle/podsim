// Package session owns the shared simulation and serializes commands with clock ticks.
package session

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// QueueLimit bounds pending work from both manual and generated requests.
const QueueLimit = 200

const (
	// clientLimit is the largest number of clients that a session records.
	// A command from one more client gets ClientLimit.
	clientLimit = 1024
	// maxClientBytes is the largest size of a client ID, in bytes.
	maxClientBytes = 100
)

// State is an authoritative, immutable copy sent to observers.
// Checkpoints lists the retained save points, oldest first. Build identifies
// the server build. It is empty when the server has no build ID. Restore
// tells how the server started the simulation when it had a state store.
// Its tier is empty when the server rejected or could not read the saved
// state. It is zero when no saved state existed, when the server has no
// store, and after a reset, a demo or a project apply.
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
	Checkpoints     []Checkpoint `json:"checkpoints,omitempty"`
	Build           string       `json:"build,omitempty"`
	Restore         RestoreInfo  `json:"restore,omitzero"`
}

// ProjectState contains a copied project and its edit revision.
type ProjectState struct {
	Revision uint64         `json:"revision"`
	Project  project.Config `json:"project"`
}

// Metrics contains low-cardinality operational measurements for one session.
// ActiveVehicles counts the pods with assigned work, as
// sim.Snapshot.WorkingVehicles defines it.
type Metrics struct {
	Tick                    int64
	Submitted               int
	Completed               int
	Pending                 int
	Vehicles                int
	ActiveVehicles          int
	PassengerVehicles       int
	StoppedVehicles         int
	PassengerDistanceMeters float64
	EmptyDistanceMeters     float64
	AverageWaitSeconds      float64
	MaximumWaitSeconds      float64
	Checkpoints             int

	// StateConfigured is true when the session has a state store. The other
	// state fields are zero without a state store.
	StateConfigured bool
	// StateEnabled is true while the session saves its state.
	StateEnabled bool
	// StateSaves counts the saves that wrote the state, and StateSaveErrors
	// counts the saves that failed.
	StateSaves      uint64
	StateSaveErrors uint64
	// StateBytes is the compressed size in bytes of the state that the last
	// good save wrote.
	StateBytes int64
	// StateUnsavedSeconds is 0 when the last good save has the current
	// revision. Otherwise it is the time since the last good save, or since
	// the start when no save succeeded.
	StateUnsavedSeconds float64
}

// Command describes an explicit mutation with a per-client sequence for safe retries.
// Checkpoint is the save point for a rewind. Other actions ignore it.
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
	Checkpoint      uint64          `json:"checkpoint,omitzero"`
}

// CommandErrorCode classifies a rejected command independently of its wording.
type CommandErrorCode string

// Command error codes are stable machine-readable rejection categories.
// When the session cannot apply a command, the reply gets CommandRejected.
// The exception is a project command to a paused session whose project
// revision is not the current project revision. Its reply gets StaleProject.
const (
	SessionChanged   CommandErrorCode = "session_changed"
	InvalidCommand   CommandErrorCode = "invalid_command"
	ExpiredCommand   CommandErrorCode = "expired_command"
	SequenceConflict CommandErrorCode = "sequence_conflict"
	ClientLimit      CommandErrorCode = "client_limit"
	CommandRejected  CommandErrorCode = "command_rejected"
	StaleProject     CommandErrorCode = "stale_project"
	ServerStopping   CommandErrorCode = "server_stopping"
)

// Reply acknowledges one command without repeating the current state frame.
// Only a checkpoint command sets Checkpoint, the ID of the new save point.
// Only a rewind that restores a different project sets ProjectRestored.
//
// StateSaved is set only when Apply tried to save the session state before
// the reply. This occurs for a project apply, for a rewind that restores a
// project, and for exact retries of both, when the session saves its state.
// StateSaved is true when the last successful save holds the state at
// Revision or at a later revision. A later state counts, because a restore
// of it cannot go back to the state before the command. StateSaved is false
// when no successful save holds such a state, for example after a failed
// save, or after Close when no earlier save holds the command. Then a
// server crash can undo the command. An exact retry keeps Revision, and it
// reports StateSaved after its own save.
type Reply struct {
	Epoch           string           `json:"epoch"`
	Revision        uint64           `json:"revision"`
	ProjectRevision uint64           `json:"projectRevision"`
	Generation      uint64           `json:"generation"`
	OrderID         int              `json:"orderID,omitempty"`
	Checkpoint      uint64           `json:"checkpoint,omitzero"`
	ProjectRestored bool             `json:"projectRestored,omitzero"`
	StateSaved      *bool            `json:"stateSaved,omitzero"`
	ErrorCode       CommandErrorCode `json:"errorCode,omitempty"`
	Error           string           `json:"error,omitempty"`
}

type receipt struct {
	command Command
	// reply has no StateSaved. Apply sets it for each call.
	reply Reply
	// saveState is true when Apply saves the state before the reply. An
	// exact retry of the command saves too.
	saveState bool
}

// Option configures a session constructor.
type Option func(*Session)

// WithProjectSaver saves project changes before the session applies them.
func WithProjectSaver(save func(project.Config) error) Option {
	return func(session *Session) { session.saveProject = save }
}

// WithLogger sets the logger for session events. The default is slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(session *Session) {
		if logger != nil {
			session.logger = logger
		}
	}
}

// WithBuildID sets the build ID that state frames carry. Without this
// option, the build ID is empty and frames omit it.
func WithBuildID(id string) Option {
	return func(session *Session) { session.build = id }
}

// Session contains one fleet and one simulation clock. Use Run once per session.
type Session struct {
	// Close sets closed without mu, so a slow command cannot block shutdown.
	closed     atomic.Bool
	mu         sync.Mutex
	simulation *sim.Simulation
	// project is the current project. Code replaces it whole and never
	// writes to it in place. Save points share it, and a state save encodes
	// it after it releases mu.
	project         project.Config
	epoch           string
	revision        uint64
	projectRevision uint64
	generation      uint64
	speed           int
	demand          demandRun
	receipts        map[string]receipt
	// restoredSequences holds the last command sequence of each client
	// before a restore that kept the epoch. The session does not have the
	// replies of these commands. A client leaves the map when the session
	// records a receipt for it, so a client is in receipts or in
	// restoredSequences, not in both.
	restoredSequences map[string]uint64
	saveProject       func(project.Config) error
	logger            *slog.Logger
	// build identifies the server build. A rewind does not change it.
	build string
	// checkpoints holds the retained save points, oldest first. lastCheckpoint
	// is the last issued ID. The session never uses an ID again in an epoch.
	checkpoints    []checkpoint
	lastCheckpoint uint64
	// projectOrigin is the projectRevision that installed the current project
	// value. A save point with another origin holds another project.
	projectOrigin uint64
	// restore tells how NewFromStore started the simulation. A reset, a
	// demo, and a project apply clear it. A rewind keeps it.
	restore RestoreInfo
	// persist saves the session state. It is nil without a state store.
	persist *persistence
}

// New creates the supplied example project with demand disabled.
func New() (*Session, error) { return NewWithProject(project.Default()) }

// NewWithProject validates and copies a project into a new session.
func NewWithProject(config project.Config, options ...Option) (*Session, error) {
	if err := project.Validate(config); err != nil {
		return nil, err
	}
	session := newSession(nil, options)
	if err := session.startProject(config); err != nil {
		return nil, err
	}
	return session, nil
}

// newSession returns a session with persist and the options, and without a
// simulation. persist is nil without a state store.
func newSession(persist *persistence, options []Option) *Session {
	session := &Session{receipts: make(map[string]receipt), logger: slog.Default(), persist: persist}
	for _, option := range options {
		option(session)
	}
	return session
}

// startProject starts a new simulation of a copy of config in a new epoch.
// config must be valid.
func (s *Session) startProject(config project.Config) error {
	owned := project.Clone(config)
	simulation, err := sim.NewFleet(owned.Network, owned.Fleet)
	if err != nil {
		return fmt.Errorf("create shared fleet: %w", err)
	}
	if err := simulation.SetSharedRidePartyLimit(project.EffectiveSharedRidePartyLimit(owned)); err != nil {
		return fmt.Errorf("configure shared rides: %w", err)
	}
	s.simulation, s.project, s.epoch = simulation, owned, rand.Text()
	s.projectRevision, s.projectOrigin, s.generation, s.speed = 1, 1, 1, 1
	s.demand = newDemand(demandInput{config: owned.Demand, network: owned.Network, profiles: owned.DemandProfiles})
	s.configureRedistribution()
	return nil
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

// Close stops the clock and rejects new commands. Reads continue. Close does not
// wait for a tick or a command that is already in progress. Close is idempotent.
func (s *Session) Close() { s.closed.Store(true) }

func (s *Session) advance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() || s.simulation.Snapshot().Paused {
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

// Topology returns detached geometry for the active project revision.
func (s *Session) Topology() TopologySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return TopologySnapshot{
		Epoch: s.epoch, ProjectRevision: s.projectRevision,
		Network: project.CloneNetwork(s.project.Network),
	}
}

// Frame returns recurring state without network geometry or complete route lanes.
func (s *Session) Frame() StateFrame {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stateFrame(s.stateWithoutNetwork())
}

// Metrics returns a compact session snapshot for operational monitoring.
// It does not wait for a state save.
func (s *Session) Metrics() Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.simulation.Snapshot()
	metrics := Metrics{
		Tick:                    state.Tick,
		Submitted:               state.Submitted,
		Completed:               state.Completed,
		Pending:                 len(state.Pending),
		Vehicles:                len(state.Vehicles),
		PassengerDistanceMeters: state.PassengerDistanceMeters,
		EmptyDistanceMeters:     state.EmptyDistanceMeters,
		AverageWaitSeconds:      state.Wait.AverageSeconds,
		MaximumWaitSeconds:      state.Wait.MaxSeconds,
		Checkpoints:             len(s.checkpoints),
	}
	metrics.ActiveVehicles = state.WorkingVehicles()
	for _, vehicle := range state.Vehicles {
		if vehicle.Pod.Occupied {
			metrics.PassengerVehicles++
		}
		if vehicle.Pod.WaitReason != sim.NoWait && vehicle.Pod.Speed < 0.01 {
			metrics.StoppedVehicles++
		}
	}
	if s.persist != nil {
		s.persist.setMetrics(&metrics, s.revision)
	}
	return metrics
}

func (s *Session) state() State {
	state := s.stateWithoutNetwork()
	state.Network = project.CloneNetwork(s.project.Network)
	return state
}

func (s *Session) stateWithoutNetwork() State {
	return State{
		Epoch:           s.epoch,
		Revision:        s.revision,
		ProjectRevision: s.projectRevision,
		Generation:      s.generation,
		Redistribution:  s.project.Redistribution && !s.simulation.Snapshot().Demo,
		Simulation:      s.simulation.Snapshot(),
		Speed:           s.speed,
		Demand:          s.demand.state,
		Checkpoints:     s.checkpointList(),
		Build:           s.build,
		Restore:         s.restore,
	}
}

// Project returns a detached project safe for concurrent observers.
func (s *Session) Project() ProjectState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ProjectState{Revision: s.projectRevision, Project: project.Clone(s.project)}
}

// Apply serializes commands. The latest sequence can be retried; older sequences never replay.
// After a restore that kept the epoch, a sequence from before the restore
// gets ExpiredCommand, because the session does not have its reply.
// After Close, a command that passes the epoch and sequence checks gets ServerStopping.
// An exact retry still gets its stored reply.
//
// When the session saves its state, Apply saves it before it replies to a
// project apply or to a rewind that restores a project. It waits at most
// 2 s for this save. A failed save does not reject the command, because the
// session applied the command. After the save, StateSaved tells whether the
// last successful save holds the state at the revision of the reply or at a
// later revision. An exact retry of such a command also saves before it
// replies. This save waits for the save of the first request, and it writes
// nothing when the state did not change after that save.
func (s *Session) Apply(command Command) Reply {
	result := s.applyCommand(cloneCommand(command))
	// Save a project change soon. A saved state with an earlier project
	// does not match the project file after a crash. When the save before
	// the reply fails, this save tries again.
	if result.projectChanged {
		s.nudgeSaver()
	}
	// Log after applyCommand releases the lock. A slow log sink must not stop
	// the clock or the readers. Commands carry no request context, so use
	// Info, which logs with context.Background.
	if result.event != nil {
		s.logger.Info(result.event.message, result.event.args()...)
	}
	// A project apply and a rewind that restores a project write the project
	// file at once. Save the state before the reply. Otherwise a crash after
	// the reply can restore the state from before the command. An exact
	// retry saves too, because the save of the first request can still run.
	if result.saveState {
		result.reply.StateSaved = s.saveBeforeReply(result.reply.Revision)
	}
	return result.reply
}

// commandResult is the result of applyCommand. event is the record to log,
// or nil. projectChanged is true when the command changed the project
// revision. saveState is true when Apply saves the state before it replies.
// An exact retry that gets its stored reply has no event and no project
// change. It gets saveState from its receipt.
type commandResult struct {
	reply          Reply
	event          *sessionEvent
	projectChanged bool
	saveState      bool
}

// applyCommand holds the lock while it checks and applies command.
func (s *Session) applyCommand(command Command) commandResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectRevision := s.projectRevision
	reply := s.reply()
	var event *sessionEvent
	var saveState bool
	switch {
	case command.Epoch != s.epoch:
		reply.reject(SessionChanged, "The server session changed. Review the current state and try again.")
	// A state save stores the client ID, and the JSON encoder accepts only
	// valid UTF-8.
	case command.Client == "" || len(command.Client) > maxClientBytes || !utf8.ValidString(command.Client) ||
		command.Sequence == 0:
		reply.reject(InvalidCommand, "Invalid client or command sequence.")
	default:
		previous, exists := s.receipts[command.Client]
		restored, isRestored := s.restoredSequences[command.Client]
		switch {
		case exists && command.Sequence < previous.command.Sequence:
			reply.reject(ExpiredCommand, "This command has expired. Review the current state.")
		case exists && command.Sequence == previous.command.Sequence:
			if !reflect.DeepEqual(previous.command, command) {
				reply.reject(SequenceConflict, "This sequence was already used for another command.")
			} else {
				reply, saveState = previous.reply, previous.saveState
			}
		case isRestored && command.Sequence <= restored:
			// The session can have applied the command before the restart,
			// but it lost the reply. Do not apply the command again.
			reply.reject(ExpiredCommand, "The server restarted after this command. Review the current state.")
		case s.closed.Load():
			reply.reject(ServerStopping, "The server is stopping. Try again after it restarts.")
		case !exists && !isRestored && len(s.receipts)+len(s.restoredSequences) >= clientLimit:
			reply.reject(ClientLimit, "The session client limit was reached. Restart the server.")
		default:
			started := time.Now()
			result, err := s.apply(command)
			if err == nil {
				s.revision++
			}
			reply = s.reply()
			reply.OrderID, reply.Checkpoint, reply.ProjectRestored = result.orderID, result.checkpoint, result.projectRestored
			if err != nil {
				code := CommandRejected
				if errors.Is(err, errStaleProject) {
					code = StaleProject
				}
				reply.reject(code, err.Error())
			}
			s.receipts[command.Client] = receipt{command: cloneCommand(command), reply: reply, saveState: result.saveState}
			delete(s.restoredSequences, command.Client)
			if result.event != nil {
				event = result.event
				event.client, event.duration = command.Client, time.Since(started)
			}
			saveState = result.saveState
		}
	}
	return commandResult{reply: reply, event: event, projectChanged: s.projectRevision != projectRevision, saveState: saveState}
}

func (s *Session) reply() Reply {
	return Reply{
		Epoch: s.epoch, Revision: s.revision,
		ProjectRevision: s.projectRevision, Generation: s.generation,
	}
}

func (r *Reply) reject(code CommandErrorCode, message string) {
	r.ErrorCode = code
	r.Error = message
}

func cloneCommand(command Command) Command {
	if command.Project != nil {
		command.Project = new(project.Clone(*command.Project))
	}
	return command
}

// outcome holds the reply values of an accepted command. event is nil when
// the command has no event to log. saveState is true when Apply saves the
// state before it replies.
type outcome struct {
	orderID         int
	checkpoint      uint64
	projectRestored bool
	saveState       bool
	event           *sessionEvent
}

// sessionEvent is a log record for an accepted command. Apply writes it after
// it releases the lock. details holds the slog.Attr values that describe the
// command. duration is the time to apply the command under the lock.
type sessionEvent struct {
	message  string
	client   string
	details  []any
	duration time.Duration
}

// args returns the client, the details, and the duration, in that order.
func (e *sessionEvent) args() []any {
	return slices.Concat(
		[]any{slog.String("client", e.client)},
		e.details,
		[]any{slog.Duration("duration", e.duration)},
	)
}

func (s *Session) apply(command Command) (outcome, error) {
	switch command.Action {
	case "trip":
		state := s.simulation.Snapshot()
		if state.Demo {
			return outcome{}, errors.New("wait for the demo to finish before requesting a journey")
		}
		if len(state.Pending) >= QueueLimit {
			return outcome{}, errors.New("the order queue is full; try again after a pickup")
		}
		if err := s.simulation.RequestTrip(command.Origin, command.Destination); err != nil {
			return outcome{}, err
		}
		return outcome{orderID: s.simulation.Snapshot().Submitted}, nil
	case "pause":
		s.simulation.SetPaused(command.Paused)
	case "speed":
		if command.Speed != 1 && command.Speed != 2 && command.Speed != 4 && command.Speed != 8 {
			return outcome{}, errors.New("speed must be 1, 2, 4, or 8")
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
		s.restore = RestoreInfo{}
	case "demo":
		defaults := project.Default()
		if !reflect.DeepEqual(s.project.Network, defaults.Network) || !reflect.DeepEqual(s.project.Fleet, defaults.Fleet) {
			return outcome{}, errors.New("the supplied demo is available only for the example project")
		}
		if err := s.simulation.StartDemo(); err != nil {
			return outcome{}, err
		}
		s.speed = 1
		disabled := s.project.Demand
		disabled.Enabled = false
		s.demand = newDemand(demandInput{config: disabled, network: s.project.Network, profiles: s.project.DemandProfiles})
		s.generation++
		s.restore = RestoreInfo{}
	case "demand":
		if s.simulation.Snapshot().Demo {
			return outcome{}, errors.New("wait for the demo to finish before changing demand")
		}
		return outcome{}, s.applyDemand(command.Demand, s.save)
	case "project":
		if err := s.applyProject(command); err != nil {
			return outcome{}, err
		}
		return outcome{saveState: true}, nil
	case "checkpoint":
		return s.captureCheckpoint(), nil
	case "rewind":
		return s.rewind(command.Checkpoint)
	default:
		return outcome{}, errors.New("unknown command")
	}
	return outcome{}, nil
}

// applyDemand makes config the demand settings of the project and of the
// demand stream. It checks config against the project. When save is not
// nil, applyDemand calls it with the changed project before it changes the
// session. The project revision increases, so clients load the project
// again. The demand command and a restore both use applyDemand. The caller
// holds s.mu, or no other goroutine uses the session yet.
func (s *Session) applyDemand(config DemandConfig, save func(project.Config) error) error {
	demandContext := project.DemandContext{Network: s.project.Network, Profiles: s.project.DemandProfiles}
	if err := project.ValidateDemand(config, demandContext); err != nil {
		return err
	}
	updated := project.Clone(s.project)
	updated.Demand = config
	if save != nil {
		if err := save(updated); err != nil {
			return err
		}
	}
	if err := s.demand.configure(demandInput{config: config, network: s.project.Network, profiles: s.project.DemandProfiles}); err != nil {
		return err
	}
	s.project = updated
	s.configureRedistribution()
	s.projectRevision++
	s.projectOrigin = s.projectRevision
	return nil
}

// errStaleProject rejects a project command whose project revision is not
// the current project revision. applyCommand gives it the StaleProject
// error code.
var errStaleProject = errors.New("the project changed; reload it before applying edits")

func (s *Session) applyProject(command Command) error {
	if !s.simulation.Snapshot().Paused {
		return errors.New("pause the simulation before applying a project")
	}
	if command.ProjectRevision != s.projectRevision {
		return errStaleProject
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
	if err := candidate.SetSharedRidePartyLimit(project.EffectiveSharedRidePartyLimit(config)); err != nil {
		return fmt.Errorf("configure shared rides: %w", err)
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
	s.projectOrigin = s.projectRevision
	s.generation++
	s.restore = RestoreInfo{}
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
