package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// MaxStateBytes limits the compressed and the decompressed saved state.
const MaxStateBytes = 16 << 20

const (
	// stateIOTimeout bounds each call to the state store.
	stateIOTimeout = 30 * time.Second
	// nudgeDelay is the time from a project change to its save.
	nudgeDelay = time.Second
	// commandSaveTimeout bounds the save before the reply to a command. The
	// HTTP server of cmd/serve has a write timeout of 30 s, which starts
	// before the server reads the command. A shorter bound lets the reply
	// get out when the store is slow. The browser client sends an exact
	// retry when it gets no reply in 3 s. A bound of 2 s lets the reply come
	// before that retry.
	commandSaveTimeout = 2 * time.Second
	// restoreLoopAttempts is the number of restores without a periodic or
	// final save that stops the next restore.
	restoreLoopAttempts = 2
)

// Restore tiers and reasons of RestoreInfo. The physical and logical tiers
// are the names of sim.RestoreTier.
const (
	restoreEmpty         = "empty"
	reasonProjectChanged = "project_changed"
	reasonRestoreLoop    = "restore_loop"
	reasonUnreadable     = "unreadable"
	reasonPhysicalFailed = "physical_failed"
)

var (
	// ErrStateTooLarge means that the saved state is larger than MaxStateBytes.
	ErrStateTooLarge = errors.New("saved session state is too large")
	// ErrStateSavingOff means that the session does not save its state.
	ErrStateSavingOff = errors.New("session state saving is off")

	errReadTimeout        = errors.New("read of the saved session state timed out")
	errSaveTimeout        = errors.New("save of the session state timed out")
	errMoveTimeout        = errors.New("move of the saved session state timed out")
	errCommandSaveTimeout = errors.New("save of the session state before a command reply timed out")
)

// StateStore keeps one saved session state. The session calls it from
// one goroutine at a time.
type StateStore interface {
	// Read returns the saved state. The error wraps fs.ErrNotExist when
	// there is none, and ErrStateTooLarge above MaxStateBytes.
	Read(ctx context.Context) ([]byte, error)
	// Write replaces the saved state. A failed write keeps the old state.
	// Write must not keep data after it returns.
	Write(ctx context.Context, data []byte) error
	// Reject moves the saved state aside. A later Read gets fs.ErrNotExist.
	Reject(ctx context.Context) error
	// Backup copies the saved state to a backup key and replaces an earlier backup.
	Backup(ctx context.Context) error
}

// StoreInput holds the inputs of NewFromStore.
type StoreInput struct {
	// Store keeps the saved session state.
	Store StateStore
	// Project is the project of the -project file, or nil. The caller
	// validates it, and NewFromStore does not validate it again. A saved
	// state with a different project is not restored. When only the demand
	// settings are different, NewFromStore restores the saved state and
	// then applies the demand settings of Project as the demand command
	// does. When the restored traffic demo runs, NewFromStore does not
	// restore the saved state, because the demand command refuses a change
	// during the demo. With nil, NewFromStore restores the saved project.
	Project *project.Config
	// Options configure the session, as in NewWithProject.
	Options []Option
}

// RestoreInfo tells how a server with a state store started the current
// simulation. Tier is physical when the pods kept their positions, logical
// when the pods started again at their initial berths, and empty when the
// server did not use the saved state. Reason tells why the tier is not
// physical: physical_failed or restore_loop for logical, and
// project_changed, unsupported_version, invalid_state, too_large,
// unreadable or restore_loop for empty. Demoted counts the pods that the
// physical tier moved to a berth. Requeued counts the orders that went back
// to the queue. Dropped counts the orders that the restore removed because
// they were not valid.
type RestoreInfo struct {
	Tier     string `json:"tier"`
	Reason   string `json:"reason,omitempty"`
	Demoted  int    `json:"demoted,omitzero"`
	Requeued int    `json:"requeued,omitzero"`
	Dropped  int    `json:"dropped,omitzero"`
}

// SaveKind tells why the session saves its state.
type SaveKind int

const (
	// SavePeriodic is a save while the session runs. It writes only when
	// the revision changed since the last periodic, command or final save.
	SavePeriodic SaveKind = iota + 1
	// SaveFinal is the last save after Close. The next start keeps the
	// epoch of a final save.
	SaveFinal
	// saveStartup is the save of NewFromStore. It counts the restores of
	// the saved state.
	saveStartup
	// saveCommand is the save of Apply before it replies to a project apply
	// or to a rewind that restores a project. It follows the rules of a
	// periodic save.
	saveCommand
)

// String returns the name of the kind in logs.
func (k SaveKind) String() string {
	switch k {
	case SavePeriodic:
		return "periodic"
	case SaveFinal:
		return "final"
	case saveStartup:
		return "startup"
	case saveCommand:
		return "command"
	default:
		return "SaveKind(" + strconv.Itoa(int(k)) + ")"
	}
}

// persistence saves the state of one session. mu serializes the saves. A
// save locks mu and then the session lock, and no code locks them in the
// other order. The atomic fields let a reader get the save results without
// a lock.
type persistence struct {
	store StateStore
	mu    sync.Mutex
	// enabled is false after NewFromStore turned saving off. It does not
	// change after NewFromStore returns.
	enabled atomic.Bool
	// lastRevision is the revision of the last periodic, command or final
	// save. saved is true after the first one. The startup save sets
	// neither, so the first periodic or command save always writes.
	lastRevision uint64
	saved        bool
	// attempts is the restore count of the startup save.
	attempts int
	// consecutiveFailures counts the failed saves since the last good one.
	consecutiveFailures int
	encoder             stateEncoder
	// nudge asks RunStateSaver for a save after a project change.
	nudge   chan struct{}
	now     func() time.Time
	started time.Time
	// saves and failures count the saves that wrote and that failed. bytes
	// is the size of the last saved state. savedAt is the time of the last
	// good save as a time.Duration after started, and savedRevision is its
	// revision. The difference of two clock readings uses the monotonic
	// clock, so a step of the wall clock does not change savedAt. SaveState
	// sets bytes, savedAt and savedRevision before it adds to saves, so a
	// reader that sees a good save also sees its results.
	saves         atomic.Uint64
	failures      atomic.Uint64
	bytes         atomic.Int64
	savedAt       atomic.Int64
	savedRevision atomic.Uint64
}

func newPersistence(store StateStore) *persistence {
	persist := &persistence{store: store, nudge: make(chan struct{}, 1), now: time.Now}
	persist.enabled.Store(true)
	return persist
}

// withClock sets the clock that gives the save times. Tests use it. It
// applies only to a session from NewFromStore.
func withClock(now func() time.Time) Option {
	return func(session *Session) {
		if session.persist != nil {
			session.persist.now = now
		}
	}
}

// setMetrics sets the state fields of metrics. revision is the current
// revision of the session. setMetrics reads only the atomic fields and does
// not lock mu, so the caller can hold the session lock.
func (p *persistence) setMetrics(metrics *Metrics, revision uint64) {
	// Load saves before the other fields. SaveState counts a save after it
	// sets its results, so the results are set for each counted save.
	saves := p.saves.Load()
	metrics.StateConfigured = true
	metrics.StateEnabled = p.enabled.Load()
	metrics.StateSaves, metrics.StateSaveErrors = saves, p.failures.Load()
	metrics.StateBytes = p.bytes.Load()
	metrics.StateUnsavedSeconds = p.unsavedSeconds(saves, revision)
}

// unsavedSeconds returns 0 when the last good save has revision. Otherwise
// it returns the seconds since the last good save, or since the start when
// saves is 0.
func (p *persistence) unsavedSeconds(saves, revision uint64) float64 {
	var savedAt time.Duration
	switch {
	case saves == 0:
	case p.savedRevision.Load() == revision:
		return 0
	default:
		savedAt = time.Duration(p.savedAt.Load())
	}
	// A clock without a monotonic reading can go back. Do not report a
	// negative time.
	return max(0, (p.now().Sub(p.started) - savedAt).Seconds())
}

// restoreSteps holds the steps of NewFromStore that tests replace.
type restoreSteps struct {
	validateProject   func(project.Config) error
	restoreSimulation func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error)
}

// NewFromStore restores the session that input.Store keeps when it can, and
// otherwise starts a new session. It returns an error when ctx ends, when
// the startup save fails, or when a new session cannot start.
//
// When the read fails or takes more than 30 s, the session starts empty and
// does not save, so that a temporary fault cannot replace a good saved
// state. When the saved state is not usable, NewFromStore moves it aside
// with Reject, and the session starts empty. When Reject fails, the session
// does not save. Otherwise NewFromStore saves the state of the session
// before it returns. This startup save counts the restores since the last
// periodic or final save. After one such restore, the next start uses the
// logical tier only. After two, the next start does not restore.
func NewFromStore(ctx context.Context, input StoreInput) (*Session, error) {
	return newFromStore(ctx, input, restoreSteps{validateProject: project.Validate, restoreSimulation: sim.RestoreState})
}

func newFromStore(ctx context.Context, input StoreInput, steps restoreSteps) (*Session, error) {
	if input.Store == nil {
		return nil, errors.New("start session: no state store")
	}
	started := time.Now()
	s := newSession(newPersistence(input.Store), input.Options)
	s.persist.started = s.persist.now()
	data, readErr := callStore(ctx, errReadTimeout, input.Store.Read)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("read saved session state: %w", context.Cause(ctx))
	}
	loaded, err := s.start(ctx, startInput{data: data, readErr: readErr, project: input.Project, steps: steps})
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("start session: %w", context.Cause(ctx))
	}
	if !s.persist.enabled.Load() {
		return s, nil
	}
	if loaded != nil {
		s.persist.attempts = loaded.file.RestoreAttempts + 1
	}
	if _, err := callStore(ctx, errSaveTimeout, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.SaveState(ctx, saveStartup)
	}); err != nil {
		return nil, fmt.Errorf("write startup session state: %w", err)
	}
	if loaded != nil {
		s.logRestored(restoredInput{loaded: loaded, bytes: len(data), duration: time.Since(started)})
	}
	return s, nil
}

// startInput holds the result of the read of the saved state, the project
// of the caller or nil, and the restore steps.
type startInput struct {
	data    []byte
	readErr error
	project *project.Config
	steps   restoreSteps
}

// start restores the saved state when it can. Otherwise it starts a new
// session, or an empty session in place of a saved state that it cannot
// use. It returns the restored state, or nil when it did not restore.
func (s *Session) start(ctx context.Context, input startInput) (*loadedState, error) {
	switch {
	case errors.Is(input.readErr, fs.ErrNotExist):
		s.logger.Info("No saved session state")
		return nil, s.startProject(emptyProject(input.project, nil))
	case errors.Is(input.readErr, ErrStateTooLarge):
		return nil, s.startRejected(ctx, rejectInput{
			err: input.readErr, reason: reasonTooLarge, project: emptyProject(input.project, nil),
		})
	case input.readErr != nil:
		return nil, s.startUnreadable(emptyProject(input.project, nil), input.readErr)
	}
	loaded, err := s.loadState(loadInput{data: input.data, project: input.project, steps: input.steps})
	if stateErr, ok := errors.AsType[*stateError](err); ok {
		return nil, s.startRejected(ctx, rejectInput{
			err: stateErr.err, reason: stateErr.reason, project: emptyProject(input.project, loaded.validProject),
		})
	}
	if err != nil {
		return nil, err
	}
	s.installRestored(loaded)
	// The demand command writes the project file at once, but the state
	// file about 1 s later. After a crash in that time, the project file has
	// newer demand settings. Apply them as the command did. A project apply
	// or a rewind that restores a project saves the state before its reply,
	// also before the reply to an exact retry. It can leave the same
	// difference only after a crash before the reply, or after a failed
	// save. The project file has the settings already, so do not write it.
	if loaded.projectDemand != nil {
		if err := s.applyDemand(*loaded.projectDemand, nil); err != nil {
			return nil, fmt.Errorf("apply demand settings of the project file: %w", err)
		}
	}
	s.backUpDegraded(ctx, loaded.result)
	return &loaded, nil
}

// callStore runs call in a new goroutine and waits at most stateIOTimeout
// for it. A store can ignore ctx, for example a file store during a read.
// When ctx ends or the time passes first, callStore does not wait for call
// and returns the cause. The cause of the timeout is timeout.
func callStore[T any](ctx context.Context, timeout error, call func(context.Context) (T, error)) (T, error) {
	callCtx, cancel := context.WithTimeoutCause(ctx, stateIOTimeout, timeout)
	defer cancel()
	type result struct {
		value T
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := call(callCtx)
		done <- result{value: value, err: err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-callCtx.Done():
		var zero T
		return zero, context.Cause(callCtx)
	}
}

// emptyProject returns the project of a session that does not restore a
// saved state. This is the project of the caller when there is one. Else it
// is the saved project when that project is valid, and else the example
// project.
func emptyProject(input, saved *project.Config) project.Config {
	switch {
	case input != nil:
		return *input
	case saved != nil:
		return *saved
	default:
		return project.Default()
	}
}

// startUnreadable starts an empty session with config after the read of
// the saved state failed. The session does not save, so the saved state
// stays for the next start.
func (s *Session) startUnreadable(config project.Config, readErr error) error {
	if err := s.startProject(config); err != nil {
		return err
	}
	s.restore = RestoreInfo{Tier: restoreEmpty, Reason: reasonUnreadable}
	s.persist.enabled.Store(false)
	s.logger.Error("Read saved session state", slog.Any("error", readErr), slog.Bool("saving", false))
	return nil
}

// rejectInput describes a saved state that the session cannot use. err
// tells why, and reason is its code. project is the project of the empty
// session.
type rejectInput struct {
	err     error
	reason  string
	project project.Config
}

// startRejected starts an empty session in place of a saved state that the
// session cannot use, and moves the saved state aside. When the move fails,
// the session does not save, so the saved state stays.
func (s *Session) startRejected(ctx context.Context, input rejectInput) error {
	if err := s.startProject(input.project); err != nil {
		return err
	}
	s.restore = RestoreInfo{Tier: restoreEmpty, Reason: input.reason}
	s.logger.Warn("Rejected saved session state", slog.String("reason", input.reason), slog.Any("error", input.err))
	rejectCtx, cancel := context.WithTimeoutCause(ctx, stateIOTimeout, errMoveTimeout)
	defer cancel()
	if err := s.persist.store.Reject(rejectCtx); err != nil {
		s.persist.enabled.Store(false)
		s.logger.Error("Move rejected session state", slog.Any("error", err), slog.Bool("saving", false))
	}
	return nil
}

// loadInput holds a saved state, the project of the caller or nil, and the
// restore steps.
type loadInput struct {
	data    []byte
	project *project.Config
	steps   restoreSteps
}

// loadedState is a decoded saved state and the session values that
// loadState restored from it.
type loadedState struct {
	file        stateFile
	config      project.Config
	simulation  *sim.Simulation
	result      sim.RestoreResult
	demand      demandRun
	logicalOnly bool
	// validProject is the saved project after it passed validation. It can
	// be set when loadState fails, so that the empty session can use it.
	validProject *project.Config
	// projectDemand holds the demand settings of the project of the caller
	// when they are not the saved demand settings. It is nil when they are
	// the same, and without a project of the caller.
	projectDemand *DemandConfig
}

// loadState decodes a saved state and restores its simulation and demand.
// It checks the project before the session members, so that an empty
// session can use a valid saved project when only the other members are not
// valid. Each error is a *stateError. A saved state can be damaged or made
// by an attacker, so a panic also gives an error with reason invalid_state.
func (s *Session) loadState(input loadInput) (loaded loadedState, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("Restore failed with a panic",
				slog.Any("panic", recovered), slog.String("stack", string(debug.Stack())))
			err = invalidState(fmt.Errorf("restore panicked: %v", recovered))
		}
	}()
	loaded.file, err = decodeStateFile(input.data)
	if err != nil {
		return loadedState{}, err
	}
	file := loaded.file
	if file.RestoreAttempts >= restoreLoopAttempts {
		return loaded, &stateError{
			reason: reasonRestoreLoop,
			err:    fmt.Errorf("the saved state had %d restores without a periodic or final save", file.RestoreAttempts),
		}
	}
	// One restore without a save can be the cause of a crash. Do not put the
	// pods back where they were.
	loaded.logicalOnly = file.RestoreAttempts == restoreLoopAttempts-1
	if loaded.config, err = restoreProject(input, file.Project); err != nil {
		return loaded, err
	}
	switch {
	case input.project == nil:
		loaded.validProject = new(file.Project)
	case input.project.Demand != file.Project.Demand:
		loaded.projectDemand = new(input.project.Demand)
	}
	if err = file.validate(); err != nil {
		return loaded, invalidState(err)
	}
	loaded.simulation, loaded.result, err = input.steps.restoreSimulation(sim.RestoreStateInput{
		Network: loaded.config.Network, Fleet: loaded.config.Fleet, State: file.Simulation, LogicalOnly: loaded.logicalOnly,
	})
	if err != nil {
		return loaded, invalidState(err)
	}
	// The demand command refuses a change while the traffic demo runs, so
	// a restored demo cannot apply other demand settings. The restore then
	// rejects the saved state, as for a different project. The saved state
	// can be older than the end of the demo, so this can lose the session.
	// But that session started as a demo run, which the user can start
	// again.
	if loaded.projectDemand != nil && loaded.simulation.Snapshot().Demo {
		return loaded, &stateError{
			reason: reasonProjectChanged,
			err:    errors.New("the demand settings of the project file are different, and the saved traffic demo runs"),
		}
	}
	if loaded.demand, err = restoreDemand(file.Demand, loaded.config); err != nil {
		return loaded, invalidState(err)
	}
	return loaded, nil
}

// restoreProject returns the project that a restore uses. saved is the
// project of the saved state. With a project of the caller, the two projects
// must be the same, but their demand settings can be different. The restore
// then uses a copy of the project of the caller with the saved demand
// settings. Without a project of the caller, saved must be valid. Each
// error is a *stateError.
func restoreProject(input loadInput, saved project.Config) (project.Config, error) {
	if input.project == nil {
		if err := input.steps.validateProject(saved); err != nil {
			return project.Config{}, invalidState(fmt.Errorf("saved project: %w", err))
		}
		return saved, nil
	}
	config := project.Clone(*input.project)
	config.Demand = saved.Demand
	same, err := sameProject(config, saved)
	switch {
	case err != nil:
		return project.Config{}, invalidState(err)
	case !same:
		return project.Config{}, &stateError{reason: reasonProjectChanged, err: errors.New("the saved project is not the project file")}
	}
	return config, nil
}

// sameProject reports whether two projects have the same canonical JSON
// form.
func sameProject(first, second project.Config) (bool, error) {
	options := json.Deterministic(true)
	a, err := json.Marshal(first, options)
	if err != nil {
		return false, fmt.Errorf("encode project: %w", err)
	}
	b, err := json.Marshal(second, options)
	if err != nil {
		return false, fmt.Errorf("encode saved project: %w", err)
	}
	return bytes.Equal(a, b), nil
}

// restoreDemand makes the saved demand stream of config again. The stream
// continues with the same draws.
func restoreDemand(saved savedDemand, config project.Config) (demandRun, error) {
	run := newDemand(demandInput{config: saved.State.Config, network: config.Network, profiles: config.DemandProfiles})
	// run.rng draws from run.pcg, and rand.Rand has no other state.
	if err := run.pcg.UnmarshalBinary(saved.Random); err != nil {
		return demandRun{}, fmt.Errorf("restore demand random source: %w", err)
	}
	run.state, run.budget = saved.State, saved.Budget
	return run, nil
}

// installRestored makes the session run a restored state. The revision and
// the generation increase, so clients see a new state and reset motion.
// The project revision stays, so the topology caches of clients stay
// valid. The session keeps the saved epoch only when the saved state came
// from a final save, because a crash can lose changes that clients saw.
// Command receipts and save points start empty. With the saved epoch, the
// session keeps the saved command sequences, so that it does not apply a
// command from before the restore again. A command from another epoch
// gets SessionChanged, so a new epoch does not need them. The saved
// clients stay in the client limit with the saved epoch. Thus a saved
// state at the client limit gets a new epoch, so that a restart makes
// the session accept new clients again.
func (s *Session) installRestored(loaded loadedState) {
	file, result := loaded.file, loaded.result
	s.project, s.simulation, s.demand = loaded.config, loaded.simulation, loaded.demand
	s.epoch = rand.Text()
	if file.Final && len(file.Sequences) < clientLimit &&
		(result.Tier == sim.RestorePhysical || result.Tier == sim.RestoreLogical) {
		s.epoch = file.Epoch
		s.restoredSequences = make(map[string]uint64, len(file.Sequences))
		for _, saved := range file.Sequences {
			s.restoredSequences[saved.Client] = saved.Sequence
		}
	}
	s.revision, s.generation = file.Revision+1, file.Generation+1
	s.projectRevision, s.projectOrigin = file.ProjectRevision, file.ProjectRevision
	s.speed, s.lastCheckpoint = file.Speed, file.LastCheckpoint
	s.restore = RestoreInfo{
		Tier: string(result.Tier), Demoted: len(result.Demoted), Requeued: len(result.Requeued), Dropped: len(result.Dropped),
	}
	switch {
	case result.Tier != sim.RestoreLogical:
	case loaded.logicalOnly:
		s.restore.Reason = reasonRestoreLoop
	default:
		s.restore.Reason = reasonPhysicalFailed
	}
	// The demo sets the redistribution again when it ends.
	if !s.simulation.Snapshot().Demo {
		s.configureRedistribution()
	}
}

// backUpDegraded copies the saved state to the backup key before the
// startup save replaces it, when the restore did not keep each pod in
// place. An operator can then use the backup with an earlier build. A
// failure does not stop the start.
func (s *Session) backUpDegraded(ctx context.Context, result sim.RestoreResult) {
	if result.Tier != sim.RestoreLogical && len(result.Demoted) == 0 {
		return
	}
	backupCtx, cancel := context.WithTimeoutCause(ctx, stateIOTimeout, errMoveTimeout)
	defer cancel()
	if err := s.persist.store.Backup(backupCtx); err != nil {
		s.logger.Warn("Back up saved session state", slog.Any("error", err))
		return
	}
	s.logger.Info("Backed up saved session state")
}

// restoredInput holds a restored state, the size of its saved form, and the
// time from the read to the end of the startup save.
type restoredInput struct {
	loaded   *loadedState
	bytes    int
	duration time.Duration
}

// logRestored logs the result of a restore.
func (s *Session) logRestored(input restoredInput) {
	file, result, info := input.loaded.file, input.loaded.result, s.restore
	for _, pod := range result.Demoted {
		s.logger.Debug("Demoted pod", slog.String("pod", pod))
	}
	attrs := []any{
		slog.String("tier", info.Tier), slog.String("reason", info.Reason),
		slog.Int("demoted", info.Demoted), slog.Int("requeued", info.Requeued), slog.Int("dropped", info.Dropped),
		slog.Int("droppedParties", result.DroppedParties), slog.Int("overCap", result.OverCap),
		slog.Int("overBudget", result.OverBudget), slog.Int64("tick", file.Simulation.Tick),
		slog.Bool("epochKept", s.epoch == file.Epoch), slog.Bool("final", file.Final),
		slog.Time("savedAt", file.SavedAt), slog.String("savedBuild", file.Build), slog.String("build", s.build),
		slog.Int("restoreAttempts", file.RestoreAttempts), slog.Int("bytes", input.bytes),
		slog.Duration("duration", input.duration),
	}
	if result.PhysicalError != nil {
		attrs = append(attrs, slog.Any("physicalError", result.PhysicalError))
	}
	s.logger.Info("Restored session", attrs...)
	if demand := input.loaded.projectDemand; demand != nil {
		s.logger.Info("Applied demand settings of the project file",
			slog.Any("savedDemand", file.Project.Demand), slog.Any("demand", *demand))
	}
}

// SaveState saves the session state in the store. It copies the state while
// it holds the session lock. It encodes, compresses and writes the copy
// after it releases the lock. Saves run one at a time.
//
// A periodic save writes nothing when the revision did not change since the
// last periodic, command or final save, and nothing after Close. Apply makes
// a command save before it replies to some commands, with the same rules.
// The first periodic or command save after the start always writes.
// SaveFinal needs a closed session and always writes. SaveState returns
// ErrStateSavingOff when the session has no store, or when NewFromStore
// turned saving off. It logs each failure.
func (s *Session) SaveState(ctx context.Context, kind SaveKind) error {
	persist := s.persist
	if persist == nil || !persist.enabled.Load() {
		return ErrStateSavingOff
	}
	if kind != SavePeriodic && kind != SaveFinal && kind != saveStartup && kind != saveCommand {
		return fmt.Errorf("save session state: unknown kind %d", kind)
	}
	persist.mu.Lock()
	defer persist.mu.Unlock()
	// lock is the time to lock the session, copy the state, and unlock it.
	copyStarted := time.Now()
	file, write, err := s.captureState(kind)
	lock := time.Since(copyStarted)
	if err != nil || !write {
		return err
	}
	file.SavedAt = persist.now()
	if kind == saveStartup {
		file.RestoreAttempts = persist.attempts
	}
	encodeStarted := time.Now()
	data, err := persist.encoder.encode(file)
	encode := time.Since(encodeStarted)
	var writeTime time.Duration
	if err == nil {
		writeStarted := time.Now()
		err = persist.store.Write(ctx, data)
		writeTime = time.Since(writeStarted)
	}
	if err != nil {
		persist.failures.Add(1)
		persist.consecutiveFailures++
		// A state that is too large fails each time, so it needs action.
		level := slog.LevelWarn
		if errors.Is(err, ErrStateTooLarge) {
			level = slog.LevelError
		}
		s.logger.LogAttrs(ctx, level, "Save session state", slog.String("kind", kind.String()), slog.Any("error", err),
			slog.Any("cause", context.Cause(ctx)), slog.Int("failures", persist.consecutiveFailures))
		return fmt.Errorf("save %s session state: %w", kind, err)
	}
	persist.bytes.Store(int64(len(data)))
	persist.savedAt.Store(int64(file.SavedAt.Sub(persist.started)))
	persist.savedRevision.Store(file.Revision)
	persist.saves.Add(1)
	persist.consecutiveFailures = 0
	if kind != saveStartup {
		persist.lastRevision, persist.saved = file.Revision, true
	}
	message, level := "Saved session state", slog.LevelDebug
	if kind == SaveFinal {
		message, level = "Saved final session state", slog.LevelInfo
	}
	s.logger.LogAttrs(ctx, level, message, slog.String("kind", kind.String()), slog.Int("bytes", len(data)),
		slog.Uint64("revision", file.Revision), slog.Int64("tick", file.Simulation.Tick),
		slog.Duration("lock", lock), slog.Duration("encode", encode), slog.Duration("write", writeTime))
	return nil
}

// captureState copies the state that a save of kind writes. It holds the
// session lock only while it copies. The copy shares no storage that the
// session changes, because the session replaces its project whole. It
// reports false when the save has nothing to write. The caller holds
// persist.mu.
func (s *Session) captureState(kind SaveKind) (stateFile, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	closed := s.closed.Load()
	switch {
	case kind == SaveFinal && !closed:
		return stateFile{}, false, errors.New("save session state: a final save needs a closed session")
	case kind != SaveFinal && closed:
		return stateFile{}, false, nil
	case (kind == SavePeriodic || kind == saveCommand) && s.persist.saved && s.revision == s.persist.lastRevision:
		return stateFile{}, false, nil
	}
	random, err := s.demand.pcg.MarshalBinary()
	if err != nil {
		return stateFile{}, false, fmt.Errorf("save demand random source: %w", err)
	}
	file := stateFile{
		Format: stateFormat, Version: stateVersion, Final: kind == SaveFinal, Epoch: s.epoch,
		Revision: s.revision, ProjectRevision: s.projectRevision, Generation: s.generation,
		LastCheckpoint: s.lastCheckpoint, Speed: s.speed, Sequences: s.commandSequences(),
		Demand:     savedDemand{State: s.demand.state, Random: random, Budget: s.demand.budget},
		Simulation: s.simulation.ExportState(),
		Project:    s.project,
	}
	// A restore rejects a file with a build of another form.
	if isBuildID(s.build) {
		file.Build = s.build
	}
	return file, true, nil
}

// commandSequences returns the last command sequence of each client, in
// increasing order of client ID. These are the clients with a receipt and
// the restored clients without one. commandSequences returns nil when
// there are no clients. The caller holds s.mu.
func (s *Session) commandSequences() []savedSequence {
	count := len(s.receipts) + len(s.restoredSequences)
	if count == 0 {
		return nil
	}
	sequences := make([]savedSequence, 0, count)
	for client, stored := range s.receipts {
		sequences = append(sequences, savedSequence{Client: client, Sequence: stored.command.Sequence})
	}
	for client, sequence := range s.restoredSequences {
		sequences = append(sequences, savedSequence{Client: client, Sequence: sequence})
	}
	slices.SortFunc(sequences, func(a, b savedSequence) int { return strings.Compare(a.Client, b.Client) })
	return sequences
}

// RunStateSaver saves the session state every interval, and 1 s after a
// command that changes the project, until ctx ends. It saves at most once
// for all the changes in that second. Each save has a 30 s timeout that
// derives from ctx. RunStateSaver returns at once when the session does
// not save its state. interval must be more than 0.
func (s *Session) RunStateSaver(ctx context.Context, interval time.Duration) {
	if s.persist == nil || !s.persist.enabled.Load() {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// pending fires 1 s after the first nudge since the last save.
	var pending <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.persist.nudge:
			if pending == nil {
				pending = time.After(nudgeDelay)
			}
			continue
		case <-pending:
		case <-ticker.C:
		}
		pending = nil
		s.savePeriodic(ctx)
	}
}

// savePeriodic runs one periodic save with a timeout. SaveState logs a
// failure, and the next save tries again.
func (s *Session) savePeriodic(ctx context.Context) {
	saveCtx, cancel := context.WithTimeoutCause(ctx, stateIOTimeout, errSaveTimeout)
	defer cancel()
	_ = s.SaveState(saveCtx, SavePeriodic)
}

// saveBeforeReply saves the session state before Apply replies to a
// command. It waits at most commandSaveTimeout, also when an earlier save
// still uses the store. After that time, Apply replies, and the save
// continues with an ended context. SaveState logs a failure. The caller
// does not hold the session lock.
//
// saveBeforeReply returns the StateSaved value of the reply to the command
// at revision. It returns nil when the session does not save its state. It
// gets the value after the save, also when the save writes nothing, fails
// or takes more time. The value is true when the last successful save of
// any kind holds the state at revision or at a later revision. A later
// state counts, because a restore of it cannot go back to the state before
// the command. The value is false when no successful save holds such a
// state, for example after a failed save. After Close, a command save
// writes nothing, and the final save can still fail. Thus the value is then
// true only when an earlier save, for example the final save, holds such a
// state.
//
// The value comes from persist.savedRevision. SaveState sets it after each
// successful write. Saves run one at a time under persist.mu, and the
// revision of a session only increases, so savedRevision does not
// decrease. persist.lastRevision needs persist.mu. After the timeout, the
// save continues and holds persist.mu until its write ends. The reply must
// not wait for it, so saveBeforeReply reads the atomic value. The reply can
// then report false, and the save can succeed later. The startup save runs
// before the first command, so its revision is lower than the revision of
// each command.
//
// Commands carry no request context, and a canceled request must not stop
// the save, so the save uses context.Background. contextcheck skips a
// function with a directive in its doc comment. nolintlint does not see
// that use, because contextcheck reports the callers of Apply.
//
//nolint:contextcheck,nolintlint // The save uses context.Background on purpose.
func (s *Session) saveBeforeReply(revision uint64) *bool {
	if s.persist == nil || !s.persist.enabled.Load() {
		return nil
	}
	ctx, cancel := context.WithTimeoutCause(context.Background(), commandSaveTimeout, errCommandSaveTimeout)
	defer cancel()
	_, _ = callStore(ctx, errCommandSaveTimeout, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.SaveState(ctx, saveCommand)
	})
	return new(s.persist.savedRevision.Load() >= revision)
}

// nudgeSaver asks RunStateSaver for a save. It does not wait. The caller
// does not hold the session lock.
func (s *Session) nudgeSaver() {
	if s.persist == nil {
		return
	}
	select {
	case s.persist.nudge <- struct{}{}:
	default:
	}
}
