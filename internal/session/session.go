// Package session owns the shared simulation and serializes commands with clock ticks.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	// maxErrorBytes is the largest size of the error text of a reply, in
	// bytes. The session keeps the last reply of each client, so a long
	// error for each of clientLimit clients would use much memory.
	maxErrorBytes = 1024
)

// State is an authoritative, immutable copy sent to observers.
// Checkpoints lists the retained save points, oldest first. Build identifies
// the server build. It is empty when the server has no build ID. ServerStart
// identifies the server process. Restore tells how the server started the
// simulation when it had a state store. Its tier is empty when the server
// rejected or could not read the saved state. It is zero when no saved state
// existed, when the server has no store, and after a reset, a demo or a
// project apply that replaces the fleet.
type State struct {
	Epoch           string                 `json:"epoch"`
	Revision        uint64                 `json:"revision"`
	ProjectRevision uint64                 `json:"projectRevision"`
	Generation      uint64                 `json:"generation"`
	Redistribution  bool                   `json:"redistribution"`
	Network         sim.Network            `json:"network"`
	Geo             *project.Geo           `json:"geo,omitzero"`
	Map             *project.MapBackground `json:"map,omitzero"`
	Simulation      sim.Snapshot           `json:"simulation"`
	Speed           int                    `json:"speed"`
	SpeedReduction  SpeedReduction         `json:"speedReduction,omitzero"`
	Demand          DemandState            `json:"demand"`
	Checkpoints     []Checkpoint           `json:"checkpoints,omitempty"`
	Build           string                 `json:"build,omitempty"`
	ServerStart     string                 `json:"serverStart,omitempty"`
	Restore         RestoreInfo            `json:"restore,omitzero"`
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
	Stream                  StreamMetrics
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
// ServerStart is the server start ID of the state that a project command
// is based on. When it is not empty and it is not the ID of this session,
// the project command gets SessionChanged. Other actions ignore it.
type Command struct {
	OrderContract   sim.OrderContract  `json:"orderContract,omitzero"`
	Client          string             `json:"client"`
	Sequence        uint64             `json:"sequence"`
	Epoch           string             `json:"epoch"`
	Action          string             `json:"action"`
	Origin          string             `json:"origin,omitempty"`
	Destination     string             `json:"destination,omitempty"`
	PartySize       int                `json:"partySize,omitzero"`
	SharingConsent  sim.SharingConsent `json:"sharingConsent,omitempty"`
	Service         sim.ServiceChoice  `json:"service,omitempty"`
	ServiceID       string             `json:"serviceID,omitempty"`
	Paused          bool               `json:"paused,omitempty"`
	Speed           int                `json:"speed,omitempty"`
	Demand          DemandConfig       `json:"demand,omitzero"`
	Project         *project.Config    `json:"project,omitempty"`
	ProjectRevision uint64             `json:"projectRevision,omitempty"`
	Checkpoint      uint64             `json:"checkpoint,omitzero"`
	ServerStart     string             `json:"serverStart,omitempty"`
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
// the reply. This occurs for a project apply that changes the project, for
// a rewind that restores a project, and for exact retries of both, when the session saves its state.
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

// receipt is the result of the last command of a client. It keeps the
// sequence and the digest of the command, not the command, so that each
// receipt has a fixed size. A command can hold a project of some megabytes,
// and the session keeps a receipt for each of clientLimit clients.
type receipt struct {
	sequence uint64
	digest   commandDigest
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
	publicOrigin *publicOrigin
	streamMu     sync.Mutex
	stream       *statePublisher
	// Close sets closed without mu, so a slow command cannot block shutdown.
	closed     atomic.Bool
	mu         sync.Mutex
	simulation *sim.Simulation
	// couplingObservation retains the last valid read or command boundary.
	// Failed native ticks never replace it. The caller holds mu.
	couplingObservation *State
	couplingViewError   error
	// project is the current project. Code replaces it whole and never
	// writes to it in place. Save points share it, and a state save encodes
	// it after it releases mu.
	project         project.Config
	epoch           string
	revision        uint64
	projectRevision uint64
	generation      uint64
	speed           int
	clock           playbackClock
	speedReduction  SpeedReduction
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
	// serverStart is a random ID that the session gets when it is made.
	// A server makes one session in each process, so a new process has a
	// new ID. No command changes it, and the state file does not save it.
	serverStart string
	// checkpoints holds the retained save points, oldest first. lastCheckpoint
	// is the last issued ID. The session never uses an ID again in an epoch.
	checkpoints    []checkpoint
	lastCheckpoint uint64
	// projectOrigin is the projectRevision that installed the current project
	// value. A save point with another origin holds another project.
	projectOrigin uint64
	// restore tells how NewFromStore started the simulation. A reset, a
	// demo, and a project apply that replaces the fleet clear it. A rewind
	// keeps it.
	restore RestoreInfo
	// largeCommands is the large command guard. It has space for one
	// value. A request puts a value in it before it decodes command JSON
	// of more than largeCommandBytes, and removes the value after it
	// applies the command.
	largeCommands chan struct{}
	// largeBodies holds the admission places of command bodies. It has
	// space for maxLargeBodies values. A request puts a value in it before
	// it reads a body that needs a place, and removes the value after it
	// applies the command.
	largeBodies chan struct{}
	// places owns the native server geocoding limiter and cache.
	places *placeSearch
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
	session := &Session{
		receipts: make(map[string]receipt), logger: slog.Default(), persist: persist,
		serverStart: newServerStart(), largeCommands: make(chan struct{}, 1),
		largeBodies: make(chan struct{}, maxLargeBodies),
	}
	for _, option := range options {
		option(session)
	}
	return session
}

// newServerStart returns a random ID of 16 hexadecimal characters.
func newServerStart() string {
	var id [8]byte
	_, _ = rand.Read(id[:])
	return hex.EncodeToString(id[:])
}

// startProject starts a new simulation of a copy of config in a new epoch.
// config must be valid.
func (s *Session) startProject(config project.Config) error {
	owned := project.Clone(config)
	simulation, err := sim.NewFleetWithContracts(owned.Network, owned.Fleet, fleetContracts(owned))
	if err != nil {
		return fmt.Errorf("create shared fleet: %w", err)
	}
	if err := project.ConfigureSharedRides(simulation, owned); err != nil {
		return fmt.Errorf("configure shared rides: %w", err)
	}
	if err := project.ConfigurePlatoons(simulation, owned); err != nil {
		return fmt.Errorf("configure platoons: %w", err)
	}
	if err := project.ConfigureExperiments(simulation, owned); err != nil {
		return fmt.Errorf("configure experimental policies: %w", err)
	}
	epoch := rand.Text()
	if err := preflightExpressTopology(owned, s.serverStart, epoch, 1); err != nil {
		return err
	}
	s.simulation, s.project, s.epoch = simulation, owned, epoch
	s.couplingViewError = nil
	s.projectRevision, s.projectOrigin, s.generation, s.speed = 1, 1, 1, 1
	s.demand = newDemand(demandInput{config: owned.Demand, network: owned.Network, profiles: owned.DemandProfiles, arrivals: owned.RailArrivals, departures: owned.RailDepartures})
	s.configureRedistribution()
	s.refreshCouplingObservation()
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
			s.liveAdvance(ctx)
		}
	}
}

// Close stops the clock and rejects new commands. Reads continue. Close does not
// wait for a tick or a command that is already in progress. Close is idempotent.
func (s *Session) Close() {
	s.closed.Store(true)
	s.stopStreams()
	if s.places != nil {
		s.places.client.CloseIdleConnections()
	}
}

func (s *Session) advance() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() || s.simulation.Paused() {
		return
	}
	completed := 0
	for range s.speed {
		if err := s.step(); err != nil {
			break
		}
		completed++
	}
	if completed > 0 {
		s.revision++
	}
}

// State returns a detached snapshot safe for concurrent observers.
// After a coupling fault it returns the last valid observation. CouplingError
// reports why the session cannot publish a new observation.
func (s *Session) State() State { s.mu.Lock(); defer s.mu.Unlock(); return s.state() }

// Topology returns detached geometry for the active project revision.
func (s *Session) Topology() TopologySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topologyLocked()
}

func (s *Session) topologyLocked() TopologySnapshot {
	topology := TopologySnapshot{
		ProjectVersion: s.project.Version, OrderContract: s.project.OrderContract,
		CouplingContract: s.project.CouplingContract, CouplingEnabled: s.project.CouplingEnabled,
		CouplingSites: slices.Clone(s.project.CouplingSites), CouplingCorridors: cloneCouplingCorridors(s.project.CouplingCorridors),
		ServerStart: s.serverStart, Epoch: s.epoch, ProjectRevision: s.projectRevision,
		Network: project.CloneNetwork(s.project.Network),
	}
	if s.project.OrderContract == sim.ExpressOrderContract {
		topology.ExpressServices = slices.Clone(s.project.ExpressServices)
	}
	if s.project.Geo != nil {
		topology.Geo = new(*s.project.Geo)
	}
	if s.project.Map != nil {
		topology.Map = new(*s.project.Map)
	}
	return topology
}

// Frame returns recurring state without network geometry or complete route lanes.
// After a coupling fault it retains the same observation as State.
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
	observation := s.stateWithoutNetwork()
	state := observation.Simulation
	metrics := Metrics{
		Stream:                  s.StreamStats(),
		Tick:                    state.Tick,
		Submitted:               state.Submitted,
		Completed:               state.Completed,
		Pending:                 len(state.Pending),
		Vehicles:                len(state.Vehicles),
		PassengerDistanceMeters: state.PassengerDistanceMeters,
		EmptyDistanceMeters:     state.EmptyDistanceMeters,
		AverageWaitSeconds:      state.Wait.AverageSeconds,
		MaximumWaitSeconds:      state.Wait.MaxSeconds,
		Checkpoints:             len(observation.Checkpoints),
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
	if state.ServerStart == "" {
		return state
	}
	state.Network = project.CloneNetwork(s.project.Network)
	if s.project.Geo != nil {
		state.Geo = new(*s.project.Geo)
	}
	if s.project.Map != nil {
		state.Map = new(*s.project.Map)
	}
	return state
}

func (s *Session) stateWithoutNetwork() State {
	if s.couplingError() != nil {
		return s.lastCouplingObservation()
	}
	snapshot, err := s.simulation.CheckedSnapshot()
	if err != nil {
		_ = s.retainCouplingViewError(err)
		return s.lastCouplingObservation()
	}
	state := State{
		Epoch:           s.epoch,
		Revision:        s.revision,
		ProjectRevision: s.projectRevision,
		Generation:      s.generation,
		Redistribution:  s.project.Redistribution && !snapshot.Demo,
		Simulation:      snapshot,
		Speed:           s.speed,
		SpeedReduction:  s.speedReduction,
		Demand:          s.demand.state,
		Checkpoints:     s.checkpointList(),
		Build:           s.build,
		ServerStart:     s.serverStart,
		Restore:         s.restore,
	}
	if s.project.Version == project.CouplingVersion {
		owned := cloneCouplingObservation(state)
		s.couplingObservation = &owned
	}
	return state
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
// project apply that changes the project or to a rewind that restores a
// project. It waits at most
// 2 s for this save. A failed save does not reject the command, because the
// session applied the command. After the save, StateSaved tells whether the
// last successful save holds the state at the revision of the reply or at a
// later revision. An exact retry of such a command also saves before it
// replies. This save waits for the save of the first request, and it writes
// nothing when the state did not change after that save.
func (s *Session) Apply(command Command) Reply {
	// The digest includes the project of each action, so a retry with
	// another project gets SequenceConflict, also for an action that does
	// not use the project. Only a project action keeps the project, and
	// applyProject copies it.
	digest := digestCommand(command)
	if command.Action != "project" {
		command.Project = nil
	}
	result := s.applyCommand(command, digest)
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
	// A project apply that changes the project and a rewind that restores a
	// project write the project file at once. Save the state before the reply. Otherwise a crash after
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

// applyCommand holds the lock while it checks and applies command. digest
// is the digest of the command that the client sent.
func (s *Session) applyCommand(command Command, digest commandDigest) commandResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	projectRevision := s.projectRevision
	reply := s.reply()
	var event *sessionEvent
	var saveState bool
	switch {
	case command.Epoch != s.epoch:
		reply.reject(SessionChanged, "The server session changed. Review the current state and try again.")
	// A restore of an older final save can keep the epoch and the project
	// revision with a different project. A project command from before the
	// restart then has the ID of the earlier server process.
	case command.Action == "project" && command.ServerStart != "" && command.ServerStart != s.serverStart:
		reply.reject(SessionChanged, "The server restarted. Review the current state and try again.")
	// A state save stores the client ID, and the JSON encoder accepts only
	// valid UTF-8.
	case command.Client == "" || len(command.Client) > maxClientBytes || !utf8.ValidString(command.Client) ||
		command.Sequence == 0:
		reply.reject(InvalidCommand, "Invalid client or command sequence.")
	default:
		previous, exists := s.receipts[command.Client]
		restored, isRestored := s.restoredSequences[command.Client]
		switch {
		case exists && command.Sequence < previous.sequence:
			reply.reject(ExpiredCommand, "This command has expired. Review the current state.")
		case exists && command.Sequence == previous.sequence:
			if !previous.digest.matches(digest) {
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
				if command.Action == "reset" || command.Action == "project" || command.Action == "rewind" {
					s.couplingViewError = nil
				}
				s.refreshCouplingObservation()
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
			s.receipts[command.Client] = receipt{sequence: command.Sequence, digest: digest, reply: reply, saveState: result.saveState}
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

// reject sets the error of r. When message has more than maxErrorBytes
// bytes, reject keeps the start of message and adds "...", to a total of at
// most maxErrorBytes bytes.
func (r *Reply) reject(code CommandErrorCode, message string) {
	r.ErrorCode = code
	r.Error = truncateError(message)
}

func truncateError(message string) string {
	const marker = "..."
	if len(message) <= maxErrorBytes {
		return message
	}
	// The cut goes back to the start of a rune, but by less than one rune.
	// The concatenation copies the bytes, so the reply does not keep
	// message.
	limit := maxErrorBytes - len(marker)
	end := limit
	for end > limit-utf8.UTFMax+1 && !utf8.RuneStart(message[end]) {
		end--
	}
	return message[:end] + marker
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
	if command.Action != "reset" && command.Action != "project" && command.Action != "rewind" {
		if err := s.couplingError(); err != nil {
			return outcome{}, err
		}
	}
	if err := sim.ValidateOrderContract(command.OrderContract); err != nil {
		return outcome{}, err
	}
	if command.OrderContract != "" && command.Action != "trip" {
		return outcome{}, errors.New("order contract requires a trip command")
	}
	switch command.Action {
	case "pause", "speed", "reset", "demo", "project", "rewind":
		defer s.clock.reset()
	}
	switch command.Action {
	case "trip":
		if command.OrderContract != s.project.OrderContract {
			return outcome{}, errors.New("trip order contract does not match project")
		}
		state := s.simulation.Snapshot()
		if state.Demo {
			return outcome{}, errors.New("wait for the demo to finish before requesting a journey")
		}
		if len(state.Pending) >= QueueLimit {
			return outcome{}, errors.New("the order queue is full; try again after a pickup")
		}
		orderID, err := s.simulation.SubmitTripOptions(sim.TripOptions{From: command.Origin, To: command.Destination,
			PartySize: command.PartySize, SharingConsent: command.SharingConsent, Service: command.Service, ServiceID: command.ServiceID})
		if err != nil {
			return outcome{}, err
		}
		return outcome{orderID: orderID}, nil
	case "pause":
		s.simulation.SetPaused(command.Paused)
	case "speed":
		if !validSpeed(command.Speed) {
			return outcome{}, fmt.Errorf("speed %d is not supported; use 1, 2, 5, 15, or 60", command.Speed)
		}
		s.speed = command.Speed
	case "reset":
		paused := s.simulation.Snapshot().Paused
		s.simulation.Reset()
		if err := project.ConfigureExperiments(s.simulation, s.project); err != nil {
			return outcome{}, fmt.Errorf("configure experimental policies: %w", err)
		}
		s.simulation.SetPaused(paused)
		s.speed = 1
		s.demand = newDemand(demandInput{config: s.project.Demand, network: s.project.Network, profiles: s.project.DemandProfiles, arrivals: s.project.RailArrivals, departures: s.project.RailDepartures})
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
		// The demo makes a new fleet with the default settings. A reset
		// after the demo keeps them, so apply the project settings again.
		if err := project.ConfigureSharedRides(s.simulation, s.project); err != nil {
			return outcome{}, fmt.Errorf("configure shared rides: %w", err)
		}
		if err := project.ConfigurePlatoons(s.simulation, s.project); err != nil {
			return outcome{}, fmt.Errorf("configure platoons: %w", err)
		}
		if err := project.ConfigureExperiments(s.simulation, s.project); err != nil {
			return outcome{}, fmt.Errorf("configure experimental policies: %w", err)
		}
		s.speed = 1
		disabled := s.project.Demand
		disabled.Enabled = false
		s.demand = newDemand(demandInput{config: disabled, network: s.project.Network, profiles: s.project.DemandProfiles, arrivals: s.project.RailArrivals, departures: s.project.RailDepartures})
		s.generation++
		s.restore = RestoreInfo{}
	case "demand":
		if s.simulation.Snapshot().Demo {
			return outcome{}, errors.New("wait for the demo to finish before changing demand")
		}
		return outcome{}, s.applyDemand(command.Demand, s.save)
	case "project":
		saved, err := s.applyProject(command)
		if err != nil {
			return outcome{}, err
		}
		return outcome{saveState: saved}, nil
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
	demandContext := project.DemandContext{Network: s.project.Network, Profiles: s.project.DemandProfiles, RailArrivals: s.project.RailArrivals, RailDepartures: s.project.RailDepartures}
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
	if err := s.demand.configure(demandInput{config: config, network: s.project.Network, profiles: s.project.DemandProfiles, arrivals: s.project.RailArrivals, departures: s.project.RailDepartures, tick: s.simulation.Tick()}); err != nil {
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

// applyProject applies the project of command. It returns true when it
// saved a changed project. A project that is the same as the current
// project changes nothing. A project that changes only CouplingEnabled of a
// version 5 project changes the policy in place. Each other project
// replaces the simulation.
func (s *Session) applyProject(command Command) (bool, error) {
	if !s.simulation.Snapshot().Paused {
		return false, errors.New("pause the simulation before applying a project")
	}
	if command.ProjectRevision != s.projectRevision {
		return false, errStaleProject
	}
	if command.Project == nil {
		return false, errors.New("project command requires a project")
	}
	config := project.Clone(*command.Project)
	if err := project.Validate(config); err != nil {
		return false, err
	}
	// A retained coupling fault takes the full path, also for the same
	// project. The full path installs a new controller, and applyCommand
	// then clears the fault. A no-op or a change in place keeps the failed
	// controller.
	if s.couplingError() == nil && sameExceptCouplingEnabled(s.project, config) {
		if config.CouplingEnabled == s.project.CouplingEnabled {
			return false, nil
		}
		// The demo fleet has no coupling contract, so the demo takes the
		// full path.
		if s.project.Version == project.CouplingVersion && s.simulation.CouplingContract() == config.CouplingContract {
			if err := s.applyCouplingToggle(config); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	candidate, err := sim.NewFleetWithContracts(config.Network, config.Fleet, fleetContracts(config))
	if err != nil {
		return false, fmt.Errorf("create project fleet: %w", err)
	}
	if err := project.ConfigureSharedRides(candidate, config); err != nil {
		return false, fmt.Errorf("configure shared rides: %w", err)
	}
	if err := project.ConfigurePlatoons(candidate, config); err != nil {
		return false, fmt.Errorf("configure platoons: %w", err)
	}
	if err := project.ConfigureExperiments(candidate, config); err != nil {
		return false, fmt.Errorf("configure experimental policies: %w", err)
	}
	if err := preflightExpressTopology(config, s.serverStart, s.epoch, s.projectRevision+1); err != nil {
		return false, err
	}
	candidate.SetPaused(true)
	if err := s.save(config); err != nil {
		return false, err
	}
	s.project = config
	s.simulation = candidate
	s.speed = 1
	s.demand = newDemand(demandInput{config: config.Demand, network: config.Network, profiles: config.DemandProfiles, arrivals: config.RailArrivals, departures: config.RailDepartures})
	s.configureRedistribution()
	s.projectRevision++
	s.projectOrigin = s.projectRevision
	s.generation++
	s.restore = RestoreInfo{}
	return true, nil
}

// sameExceptCouplingEnabled reports whether next is the same as current
// when CouplingEnabled is not part of the comparison. It compares the
// canonical JSON of the two projects, so a new project field is part of
// the comparison without a change here. A nil and an empty list of
// coupling sites, coupling corridors, or corridor lanes are the same. An
// encoding error gives false.
func sameExceptCouplingEnabled(current, next project.Config) bool {
	current.CouplingEnabled = next.CouplingEnabled
	same, err := sameProject(withoutEmptyCoupling(current), withoutEmptyCoupling(next))
	return err == nil && same
}

// withoutEmptyCoupling returns config with nil in place of an empty list
// of coupling sites or coupling corridors. JSON omits these fields only
// when they are nil. For the other lists, which include the corridor lanes,
// JSON writes nil and empty lists the same.
func withoutEmptyCoupling(config project.Config) project.Config {
	if len(config.CouplingSites) == 0 {
		config.CouplingSites = nil
	}
	if len(config.CouplingCorridors) == 0 {
		config.CouplingCorridors = nil
	}
	return config
}

// applyCouplingToggle installs config, which differs from the current
// project only in CouplingEnabled. It keeps the simulation, the committed
// coupling groups, the generation, the speed, the demand stream, and the
// restore information. The simulation stops or starts new recruitment. If
// the save fails, the session does not change.
func (s *Session) applyCouplingToggle(config project.Config) error {
	if err := preflightExpressTopology(config, s.serverStart, s.epoch, s.projectRevision+1); err != nil {
		return err
	}
	previous := s.simulation.CouplingEnabled()
	if err := s.simulation.SetCouplingEnabled(config.CouplingEnabled); err != nil {
		return err
	}
	if err := s.save(config); err != nil {
		// The previous value was valid, so this cannot fail.
		_ = s.simulation.SetCouplingEnabled(previous)
		return err
	}
	s.project = config
	s.projectRevision++
	s.projectOrigin = s.projectRevision
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

// configureRedistribution sets the demand weights, the positioning mode and
// the demand rate of the simulation. Expected pickup weights follow the
// configured arrival pattern. Redistribution selects guarded positioning.
// The demand rate is the rate of the live demand stream while it runs, and
// 0 otherwise, so that the gate then reads the mean rate. It reads the
// stream and not the project, because after the demo the stream is off
// while the project can keep demand on.
func (s *Session) configureRedistribution() {
	demand := s.demand
	if demand.daily == nil || demand.state.Config.Pattern != "profile-daily" {
		demand = newDemand(demandInput{config: s.project.Demand, network: s.project.Network, profiles: s.project.DemandProfiles, arrivals: s.project.RailArrivals, departures: s.project.RailDepartures, tick: s.simulation.Tick()})
	}
	// Project validation guarantees at least two passenger stations and valid settings.
	if err := s.simulation.SetDemandWeights(demand.pickupWeights); err != nil {
		panic(err)
	}
	mode := sim.PositioningOff
	if s.project.Redistribution {
		mode = sim.PositioningGuarded
	}
	rate := 0
	if live := s.demand.state.Config; live.Enabled && live.Pattern != "rail-arrivals" && live.Pattern != "rail-services" {
		rate = live.PerMinute
		if demand.daily != nil {
			rate = demand.daily.Rate(demand.dailyBand)
		}
	}
	if demand.daily != nil && rate == 0 {
		// Zero SetDemandRate selects the historical mean. Turn positioning
		// off so a daily gap does not use that fallback to start moves.
		mode = sim.PositioningOff
	}
	// The mode is valid, and demand validation rejects a negative rate.
	if err := s.simulation.SetPositioning(mode); err != nil {
		panic(err)
	}
	if err := s.simulation.SetDemandRate(rate); err != nil {
		panic(err)
	}
}
