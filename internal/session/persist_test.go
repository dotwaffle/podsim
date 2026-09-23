package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// fakeStore keeps a saved state in memory and records its calls. An error
// field makes each matching call fail. A call with a block channel waits
// until the test closes the channel. It ignores ctx, as a file store does
// during a read. When checkContext is set, a write after the end of its
// context fails, as a write of the file store does.
type fakeStore struct {
	blockRead, blockWrite chan struct{}
	checkContext          bool

	mu                                      sync.Mutex
	readErr, writeErr, rejectErr, backupErr error
	// data is the saved state. It is nil when there is none.
	data  []byte
	calls []string
	// writes holds each state that Write saved, oldest first.
	writes [][]byte
	// backups holds each state that Backup copied, oldest first.
	backups [][]byte
}

func (f *fakeStore) Read(context.Context) ([]byte, error) {
	f.record("read")
	if f.blockRead != nil {
		<-f.blockRead
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.readErr != nil:
		return nil, f.readErr
	case f.data == nil:
		return nil, fmt.Errorf("read fake state: %w", fs.ErrNotExist)
	default:
		return bytes.Clone(f.data), nil
	}
}

func (f *fakeStore) Write(ctx context.Context, data []byte) error {
	f.record("write")
	if f.blockWrite != nil {
		<-f.blockWrite
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case f.writeErr != nil:
		return f.writeErr
	case f.checkContext && ctx.Err() != nil:
		return fmt.Errorf("write fake state: %w", context.Cause(ctx))
	}
	f.data = bytes.Clone(data)
	f.writes = append(f.writes, f.data)
	return nil
}

func (f *fakeStore) Reject(context.Context) error {
	f.record("reject")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectErr != nil {
		return f.rejectErr
	}
	f.data = nil
	return nil
}

func (f *fakeStore) Backup(context.Context) error {
	f.record("backup")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.backupErr != nil {
		return f.backupErr
	}
	f.backups = append(f.backups, bytes.Clone(f.data))
	return nil
}

func (f *fakeStore) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

// callList returns the calls, oldest first.
func (f *fakeStore) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// writeList returns the written states, oldest first.
func (f *fakeStore) writeList() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.writes)
}

// backupList returns the copies that Backup made, oldest first.
func (f *fakeStore) backupList() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.backups)
}

// setWriteErr makes the next writes fail with err, or succeed when err is
// nil.
func (f *fakeStore) setWriteErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeErr = err
}

// lastWrite returns the decoded form of the last written state.
func (f *fakeStore) lastWrite(t *testing.T) stateFile {
	t.Helper()
	writes := f.writeList()
	if len(writes) == 0 {
		t.Fatal("the store has no written state")
	}
	return decodeTestState(t, writes[len(writes)-1])
}

// decodeTestState decodes a saved state, checks its session members, and
// stops the test on an error.
func decodeTestState(t *testing.T, data []byte) stateFile {
	t.Helper()
	file, err := decodeCheckedState(data)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// startFromStore returns the session of NewFromStore and stops the test on
// an error.
func startFromStore(t *testing.T, input StoreInput) *Session {
	t.Helper()
	s, err := NewFromStore(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// realRestoreSteps returns the restore steps of NewFromStore.
func realRestoreSteps() restoreSteps {
	return restoreSteps{validateProject: project.Validate, restoreSimulation: sim.RestoreState}
}

// storedRun is a closed session and its final saved state.
type storedRun struct {
	session *Session
	// data is the final saved state, and file is its decoded form.
	data []byte
	file stateFile
}

// newStoredRun runs the example project with demand for 2 simulated
// minutes, with a journey, a save point, and a demand change. Then it runs
// until a pod travels with passengers, so that a logical restore puts an
// order back in the queue. Last, it closes the session and saves its final
// state.
func newStoredRun(t *testing.T) storedRun {
	t.Helper()
	config := project.Default()
	config.Demand.Enabled = true
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config, Options: []Option{WithBuildID(testBuildID)}})
	client := newTestClient(s, "run")
	client.mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: "market"})
	client.mustApply(t, Command{Action: "checkpoint"})
	changeTestDemand(t, client)
	advanceTicks(s, 2*60*sim.TicksPerSecond)
	for tick := 0; !carriesPassengers(s.State().Simulation); tick++ {
		if tick > 5*60*sim.TicksPerSecond {
			t.Fatal("no pod traveled with passengers in 5 simulated minutes")
		}
		s.advance()
	}
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	writes := store.writeList()
	data := writes[len(writes)-1]
	return storedRun{session: s, data: data, file: decodeTestState(t, data)}
}

// carriesPassengers reports whether a traveling pod carries passengers.
func carriesPassengers(snapshot sim.Snapshot) bool {
	return slices.ContainsFunc(snapshot.Vehicles, func(vehicle sim.Vehicle) bool {
		return vehicle.Pod.Activity == sim.Traveling && vehicle.Pod.Occupied
	})
}

// edited returns the final saved state of run after edit changed it. Each
// call decodes the state again, so edits do not share storage.
func (run storedRun) edited(t *testing.T, edit func(*stateFile)) []byte {
	t.Helper()
	file := decodeTestState(t, run.data)
	if edit != nil {
		edit(&file)
	}
	return encodeTestState(t, file)
}

// demotedPod returns an edit that makes the restore demote the first
// traveling pod of run, and the ID of that pod.
func (run storedRun) demotedPod(t *testing.T) (func(*stateFile), string) {
	t.Helper()
	index := slices.IndexFunc(run.file.Simulation.Pods, func(pod sim.SavedPod) bool {
		return pod.Activity == "traveling" && len(pod.Route) > 0
	})
	if index < 0 {
		t.Fatal("the saved state has no traveling pod")
	}
	// The restore demotes a traveling pod whose lane is not the lane of its
	// route.
	return func(file *stateFile) { file.Simulation.Pods[index].LaneID = "no-such-lane" }, run.file.Simulation.Pods[index].ID
}

// Edits that force a restore result.
var (
	nonFinal       = func(file *stateFile) { file.Final = false }
	logicalOnly    = func(file *stateFile) { file.RestoreAttempts = 1 }
	restoreLoop    = func(file *stateFile) { file.RestoreAttempts = 2 }
	invalidSpeed   = func(file *stateFile) { file.Speed = 3 }
	invalidBudget  = func(file *stateFile) { file.Demand.Budget = demandBudgetLimit }
	invalidProject = func(file *stateFile) { file.Project.Name = "" }
	bothTiersFail  = func(file *stateFile) { file.Simulation.Completed = file.Simulation.RequestID + 1 }
	newerVersion   = func(file *stateFile) { file.Version = stateVersion + 1 }
	pausedAtSpeed4 = func(file *stateFile) { file.Simulation.Paused, file.Speed = true, 4 }
	// sharedBerth puts the first two pods at one berth. The physical tier
	// then fails.
	sharedBerth = func(file *stateFile) {
		placement := file.Project.Fleet[0]
		for index := range 2 {
			pod := &file.Simulation.Pods[index]
			*pod = sim.SavedPod{ID: pod.ID, Activity: "idle", StationID: placement.StationID, BerthID: placement.BerthID}
		}
	}
)

// testRecord is a log record with its attributes by key.
type testRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

// recordHandler keeps the log records that it gets, at all levels. When
// session is set, it counts the records that the session logs while it
// holds its lock. Use it only while one goroutine uses the session.
type recordHandler struct {
	session   atomic.Pointer[Session]
	underLock atomic.Int64

	mu      sync.Mutex
	records []testRecord
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	if s := h.session.Load(); s != nil {
		if s.mu.TryLock() {
			s.mu.Unlock()
		} else {
			h.underLock.Add(1)
		}
	}
	record := testRecord{level: r.Level, message: r.Message, attrs: make(map[string]any)}
	r.Attrs(func(attr slog.Attr) bool {
		record.attrs[attr.Key] = attr.Value.Resolve().Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, record)
	return nil
}

func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *recordHandler) WithGroup(string) slog.Handler { return h }

// list returns the records, oldest first.
func (h *recordHandler) list() []testRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.records)
}

// reset removes the records.
func (h *recordHandler) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = nil
}

// wantRecord describes a log record. attrs holds the attributes to check. A
// value of type func(any) bool checks the attribute. Other values must be
// equal to the attribute.
type wantRecord struct {
	level   slog.Level
	message string
	attrs   map[string]any
}

// nonEmpty checks that an attribute is set.
func nonEmpty(value any) bool { return value != nil && value != "" }

// checkRecords compares the records of handler with want.
func checkRecords(t *testing.T, handler *recordHandler, want []wantRecord) {
	t.Helper()
	got := handler.list()
	messages := make([]string, len(got))
	for i, record := range got {
		messages[i] = record.level.String() + " " + record.message
	}
	if len(got) != len(want) {
		t.Fatalf("log records = %q, want %d records", messages, len(want))
	}
	for i, record := range got {
		if record.level != want[i].level || record.message != want[i].message {
			t.Fatalf("log record %d = %s %q, want %s %q (all: %q)",
				i, record.level, record.message, want[i].level, want[i].message, messages)
		}
		for key, value := range want[i].attrs {
			attr, ok := record.attrs[key]
			if check, isCheck := value.(func(any) bool); isCheck {
				if !ok || !check(attr) {
					t.Errorf("log record %q: %s = %v fails its check", record.message, key, attr)
				}
				continue
			}
			if !ok || !reflect.DeepEqual(attr, value) {
				t.Errorf("log record %q: %s = %#v, want %#v", record.message, key, attr, value)
			}
		}
	}
}

// Log records of the startup save and of a new session.
var (
	startupSaved = wantRecord{slog.LevelDebug, "Saved session state", map[string]any{"kind": "startup"}}
	noSavedState = wantRecord{slog.LevelInfo, "No saved session state", nil}
)

func TestNewFromStoreRoundTrip(t *testing.T) {
	t.Parallel()
	run := newStoredRun(t)
	store := &fakeStore{data: run.data}
	restored := startFromStore(t, StoreInput{Store: store, Options: []Option{WithBuildID(testBuildID)}})
	saved, got := run.session.State(), restored.State()
	if got.Epoch != saved.Epoch || got.Revision != saved.Revision+1 || got.Generation != saved.Generation+1 ||
		got.ProjectRevision != saved.ProjectRevision || saved.ProjectRevision != 2 {
		t.Fatalf("restored epoch %q, revision %d, generation %d, project revision %d; saved %q, %d, %d, %d",
			got.Epoch, got.Revision, got.Generation, got.ProjectRevision,
			saved.Epoch, saved.Revision, saved.Generation, saved.ProjectRevision)
	}
	if restored.lastCheckpoint != 1 || restored.projectOrigin != saved.ProjectRevision {
		t.Fatalf("restored last save point %d and project origin %d", restored.lastCheckpoint, restored.projectOrigin)
	}
	if got.Checkpoints != nil || restored.checkpoints != nil || len(restored.receipts) != 0 {
		t.Fatalf("restored %d save points and %d receipts, want none", len(restored.checkpoints), len(restored.receipts))
	}
	wantSequences := make(map[string]uint64)
	for client, stored := range run.session.receipts {
		wantSequences[client] = stored.command.Sequence
	}
	if len(wantSequences) == 0 || !reflect.DeepEqual(restored.restoredSequences, wantSequences) {
		t.Fatalf("restored command sequences %v, want %v", restored.restoredSequences, wantSequences)
	}
	if got.Restore != (RestoreInfo{Tier: "physical"}) || got.Build != testBuildID {
		t.Fatalf("restored state has restore %+v and build %q", got.Restore, got.Build)
	}
	simulation, want := got.Simulation, saved.Simulation
	if simulation.Tick != want.Tick || simulation.Submitted != want.Submitted || simulation.Completed != want.Completed ||
		len(simulation.Pending) != len(want.Pending) || simulation.Paused != want.Paused || got.Speed != saved.Speed ||
		got.Redistribution != saved.Redistribution || got.Demand != saved.Demand {
		t.Fatalf("restored simulation differs:\n got %+v\nwant %+v", got, saved)
	}
	if same, err := sameProject(restored.project, run.session.project); err != nil || !same {
		t.Fatalf("restored project differs: %v", err)
	}
	if calls := store.callList(); !slices.Equal(calls, []string{"read", "write"}) {
		t.Fatalf("store calls = %q, want read and write", calls)
	}
	startup := store.lastWrite(t)
	if startup.RestoreAttempts != 1 || startup.Final || startup.Epoch != saved.Epoch || startup.Revision != got.Revision {
		t.Fatalf("startup save has %d restores, final %t, epoch %q, revision %d",
			startup.RestoreAttempts, startup.Final, startup.Epoch, startup.Revision)
	}
	// The restored demand stream continues with the same orders.
	live, continued := run.session.demand.clone(), restored.demand.clone()
	if live.budget != continued.budget || live.state != continued.state {
		t.Fatalf("restored demand budget %d and state %+v, want %d and %+v",
			continued.budget, continued.state, live.budget, live.state)
	}
	for draw := range 200 {
		wantFrom, wantTo := live.nextPair()
		if from, to := continued.nextPair(); from != wantFrom || to != wantTo {
			t.Fatalf("draw %d = %s to %s, want %s to %s", draw, from, to, wantFrom, wantTo)
		}
	}
}

func TestNewFromStoreEpoch(t *testing.T) {
	t.Parallel()
	run := newStoredRun(t)
	tests := []struct {
		name string
		data []byte
		tier string
		kept bool
	}{
		{"final file", run.data, "physical", true},
		{"non-final file", run.edited(t, nonFinal), "physical", false},
		{"logical tier on a final file", run.edited(t, logicalOnly), "logical", true},
		{"logical tier on a non-final file", run.edited(t, func(file *stateFile) { nonFinal(file); logicalOnly(file) }), "logical", false},
		{"rejection of a final file", run.edited(t, invalidSpeed), "empty", false},
		// The saved clients stay in the client limit with the saved epoch.
		{"final file with one client free", run.edited(t, func(file *stateFile) {
			file.Sequences = testSequences(clientLimit - 1)
		}), "physical", true},
		{"final file at the client limit", run.edited(t, func(file *stateFile) {
			file.Sequences = testSequences(clientLimit)
		}), "physical", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := startFromStore(t, StoreInput{Store: &fakeStore{data: test.data}})
			state := s.State()
			if state.Restore.Tier != test.tier {
				t.Fatalf("restore tier = %q, want %q", state.Restore.Tier, test.tier)
			}
			if kept := state.Epoch == run.file.Epoch; kept != test.kept {
				t.Fatalf("epoch kept = %t, want %t", kept, test.kept)
			}
			// Only the saved epoch needs the saved command sequences.
			if restored := len(s.restoredSequences) > 0; restored != test.kept {
				t.Fatalf("restored command sequences = %t, want %t", restored, test.kept)
			}
		})
	}
}

func TestNewFromStoreRejects(t *testing.T) {
	t.Parallel()
	run := newStoredRun(t)
	other := customProject()
	tooLarge := fmt.Errorf("read fake state: %w", ErrStateTooLarge)
	tests := []struct {
		name    string
		data    []byte
		readErr error
		project *project.Config
		// panics makes the restore of the simulation panic.
		panics bool
		reason string
		// logs lists the records before the rejection record.
		logs []wantRecord
	}{
		{name: "project changed", data: run.data, project: &other, reason: reasonProjectChanged},
		{name: "restore loop", data: run.edited(t, restoreLoop), reason: reasonRestoreLoop},
		// The restore count comes before the other checks.
		{name: "restore loop and speed 3", data: run.edited(t, func(file *stateFile) {
			restoreLoop(file)
			invalidSpeed(file)
		}), reason: reasonRestoreLoop},
		{name: "speed 3", data: run.edited(t, invalidSpeed), reason: reasonInvalidState},
		{name: "budget 3600", data: run.edited(t, invalidBudget), reason: reasonInvalidState},
		{name: "both tiers fail", data: run.edited(t, bothTiersFail), reason: reasonInvalidState},
		{
			name: "panic", data: run.data, panics: true, reason: reasonInvalidState,
			logs: []wantRecord{{slog.LevelError, "Restore failed with a panic", map[string]any{
				"panic": "restore test panic",
				"stack": func(value any) bool { stack, _ := value.(string); return strings.Contains(stack, "loadState") },
			}}},
		},
		{name: "newer version", data: run.edited(t, newerVersion), reason: reasonUnsupportedVersion},
		{name: "truncated file", data: run.data[:len(run.data)/2], reason: reasonInvalidState},
		{name: "too large to read", data: run.data, readErr: tooLarge, reason: reasonTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, handler := &fakeStore{data: test.data, readErr: test.readErr}, &recordHandler{}
			steps := realRestoreSteps()
			if test.panics {
				steps.restoreSimulation = func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
					panic("restore test panic")
				}
			}
			s, err := newFromStore(t.Context(), StoreInput{
				Store: store, Project: test.project, Options: []Option{WithLogger(slog.New(handler))},
			}, steps)
			if err != nil {
				t.Fatal(err)
			}
			state := s.State()
			if want := (RestoreInfo{Tier: "empty", Reason: test.reason}); state.Restore != want {
				t.Fatalf("restore = %+v, want %+v", state.Restore, want)
			}
			if state.Epoch == run.file.Epoch || state.Revision != 0 || state.Generation != 1 || state.Simulation.Tick != 0 {
				t.Fatalf("empty session has epoch %q, revision %d, generation %d, tick %d",
					state.Epoch, state.Revision, state.Generation, state.Simulation.Tick)
			}
			if calls := store.callList(); !slices.Equal(calls, []string{"read", "reject", "write"}) {
				t.Fatalf("store calls = %q, want read, reject and write", calls)
			}
			if startup := store.lastWrite(t); startup.RestoreAttempts != 0 || startup.Epoch != state.Epoch {
				t.Fatalf("startup save has %d restores and epoch %q", startup.RestoreAttempts, startup.Epoch)
			}
			checkRecords(t, handler, append(test.logs,
				wantRecord{slog.LevelWarn, "Rejected saved session state", map[string]any{"reason": test.reason, "error": nonEmpty}},
				startupSaved))
		})
	}
}

func TestNewFromStoreFailures(t *testing.T) {
	t.Parallel()
	run := newStoredRun(t)
	diskErr := errors.New("disk failure")
	tests := []struct {
		name string
		// store returns the store of the test. Each test needs its own store.
		store   func() *fakeStore
		project *project.Config
		// restore is the restore info of the session.
		restore RestoreInfo
		// saving tells whether the session saves its state.
		saving bool
		// calls lists the store calls after the start and one periodic save.
		calls []string
		logs  []wantRecord
	}{
		{
			name:    "read error",
			store:   func() *fakeStore { return &fakeStore{data: run.data, readErr: diskErr} },
			restore: RestoreInfo{Tier: "empty", Reason: reasonUnreadable},
			calls:   []string{"read"},
			logs: []wantRecord{{slog.LevelError, "Read saved session state", map[string]any{
				"error": diskErr, "saving": false,
			}}},
		},
		{
			name:    "reject error",
			store:   func() *fakeStore { return &fakeStore{data: run.data, rejectErr: diskErr} },
			project: new(customProject()),
			restore: RestoreInfo{Tier: "empty", Reason: reasonProjectChanged},
			calls:   []string{"read", "reject"},
			logs: []wantRecord{
				{slog.LevelWarn, "Rejected saved session state", map[string]any{"reason": reasonProjectChanged}},
				{slog.LevelError, "Move rejected session state", map[string]any{"error": diskErr, "saving": false}},
			},
		},
		{
			name:    "backup error",
			store:   func() *fakeStore { return &fakeStore{data: run.edited(t, logicalOnly), backupErr: diskErr} },
			restore: RestoreInfo{Tier: "logical", Reason: reasonRestoreLoop, Requeued: 1},
			saving:  true,
			calls:   []string{"read", "backup", "write", "write"},
			logs: []wantRecord{
				{slog.LevelWarn, "Back up saved session state", map[string]any{"error": diskErr}},
				startupSaved,
				{slog.LevelInfo, "Restored session", map[string]any{"tier": "logical", "reason": reasonRestoreLoop}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, handler := test.store(), &recordHandler{}
			s := startFromStore(t, StoreInput{Store: store, Project: test.project, Options: []Option{WithLogger(slog.New(handler))}})
			if got := s.State().Restore; got != test.restore {
				t.Fatalf("restore = %+v, want %+v", got, test.restore)
			}
			checkRecords(t, handler, test.logs)
			err := s.SaveState(t.Context(), SavePeriodic)
			if test.saving && err != nil || !test.saving && !errors.Is(err, ErrStateSavingOff) {
				t.Fatalf("periodic save error = %v with saving %t", err, test.saving)
			}
			if calls := store.callList(); !slices.Equal(calls, test.calls) {
				t.Fatalf("store calls = %q, want %q", calls, test.calls)
			}
		})
	}
	t.Run("startup write error", func(t *testing.T) {
		t.Parallel()
		for _, data := range [][]byte{nil, run.data} {
			store := &fakeStore{data: data, writeErr: diskErr}
			if _, err := NewFromStore(t.Context(), StoreInput{Store: store}); !errors.Is(err, diskErr) {
				t.Fatalf("NewFromStore error = %v, want %v", err, diskErr)
			}
		}
	})
}

// TestNewFromStoreWaits checks the bounds of the read and of the startup
// save. The store ignores ctx.
func TestNewFromStoreWaits(t *testing.T) {
	t.Parallel()
	t.Run("read timeout", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			store := &fakeStore{blockRead: make(chan struct{})}
			defer close(store.blockRead)
			started := time.Now()
			s := startFromStore(t, StoreInput{Store: store})
			if waited := time.Since(started); waited != stateIOTimeout {
				t.Fatalf("NewFromStore waited %v, want %v", waited, stateIOTimeout)
			}
			if got := s.State().Restore; got != (RestoreInfo{Tier: "empty", Reason: reasonUnreadable}) {
				t.Fatalf("restore = %+v", got)
			}
			if err := s.SaveState(t.Context(), SavePeriodic); !errors.Is(err, ErrStateSavingOff) {
				t.Fatalf("periodic save error = %v, want %v", err, ErrStateSavingOff)
			}
			// The saver has nothing to do, so it returns at once.
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			started = time.Now()
			s.RunStateSaver(ctx, time.Second)
			if waited := time.Since(started); waited != 0 {
				t.Fatalf("the saver returned after %v", waited)
			}
			if calls := store.callList(); !slices.Equal(calls, []string{"read"}) {
				t.Fatalf("store calls = %q, want read", calls)
			}
		})
	})
	t.Run("parent context ends during the read", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			store := &fakeStore{blockRead: make(chan struct{})}
			defer close(store.blockRead)
			ctx, cancel := context.WithCancel(t.Context())
			time.AfterFunc(time.Second, cancel)
			if _, err := NewFromStore(ctx, StoreInput{Store: store}); !errors.Is(err, context.Canceled) {
				t.Fatalf("NewFromStore error = %v, want %v", err, context.Canceled)
			}
			if calls := store.callList(); !slices.Equal(calls, []string{"read"}) {
				t.Fatalf("store calls = %q, want read", calls)
			}
		})
	})
	t.Run("startup write timeout", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			store := &fakeStore{blockWrite: make(chan struct{})}
			started := time.Now()
			_, err := NewFromStore(t.Context(), StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
			if waited := time.Since(started); !errors.Is(err, errSaveTimeout) || waited != stateIOTimeout {
				t.Fatalf("NewFromStore error = %v after %v, want %v after %v", err, waited, errSaveTimeout, stateIOTimeout)
			}
			// Let the save that NewFromStore stopped waiting for end.
			close(store.blockWrite)
			synctest.Wait()
		})
	})
}

func TestNewFromStoreRestoreAttempts(t *testing.T) {
	t.Parallel()
	run := newStoredRun(t)
	demote, _ := run.demotedPod(t)
	tests := []struct {
		name     string
		data     []byte
		restore  RestoreInfo
		attempts int
		backup   bool
	}{
		{"no restore before", run.data, RestoreInfo{Tier: "physical"}, 1, false},
		{"paused at speed 4", run.edited(t, pausedAtSpeed4), RestoreInfo{Tier: "physical"}, 1, false},
		{"one restore before", run.edited(t, logicalOnly), RestoreInfo{Tier: "logical", Reason: reasonRestoreLoop, Requeued: 1}, 2, true},
		{"physical tier fails", run.edited(t, sharedBerth), RestoreInfo{Tier: "logical", Reason: reasonPhysicalFailed}, 1, true},
		{"demoted pod", run.edited(t, demote), RestoreInfo{Tier: "physical", Demoted: 1}, 1, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{data: test.data}
			s := startFromStore(t, StoreInput{Store: store})
			state := s.State()
			if state.Restore != test.restore {
				t.Fatalf("restore = %+v, want %+v", state.Restore, test.restore)
			}
			if startup := store.lastWrite(t); startup.RestoreAttempts != test.attempts {
				t.Fatalf("startup save has %d restores, want %d", startup.RestoreAttempts, test.attempts)
			}
			// The backup holds the saved state from before the startup save.
			var wantBackups [][]byte
			if test.backup {
				wantBackups = [][]byte{test.data}
			}
			if backups := store.backupList(); !reflect.DeepEqual(backups, wantBackups) {
				t.Fatalf("store has %d backups, want %d", len(backups), len(wantBackups))
			}
			saved := decodeTestState(t, test.data)
			if state.Simulation.Paused != saved.Simulation.Paused || state.Speed != saved.Speed {
				t.Fatalf("restored paused %t at speed %d, want %t at %d",
					state.Simulation.Paused, state.Speed, saved.Simulation.Paused, saved.Speed)
			}
			// The first periodic save writes, also when the revision did not
			// change, and it stores no restore.
			if err := s.SaveState(t.Context(), SavePeriodic); err != nil {
				t.Fatal(err)
			}
			periodic := store.lastWrite(t)
			if writes := len(store.writeList()); writes != 2 || periodic.RestoreAttempts != 0 || periodic.Revision != state.Revision {
				t.Fatalf("after %d writes, the periodic save has %d restores and revision %d, want 0 and %d",
					writes, periodic.RestoreAttempts, periodic.Revision, state.Revision)
			}
		})
	}
}

func TestNewFromStoreProject(t *testing.T) {
	t.Parallel()
	run := newStoredRun(t)
	other := customProject()
	saved := run.file.Project
	tests := []struct {
		name    string
		data    []byte
		project *project.Config
		want    project.Config
		// validates is the number of project validations.
		validates int
		tier      string
	}{
		{"matching project file", run.data, new(project.Clone(saved)), saved, 0, "physical"},
		{"saved project", run.data, nil, saved, 1, "physical"},
		{"rejection with a project file", run.data, &other, other, 0, "empty"},
		{"invalid simulation", run.edited(t, bothTiersFail), nil, saved, 1, "empty"},
		{"invalid session member", run.edited(t, invalidSpeed), nil, saved, 1, "empty"},
		{"invalid saved project", run.edited(t, invalidProject), nil, project.Default(), 1, "empty"},
		{"restore loop without a project file", run.edited(t, restoreLoop), nil, project.Default(), 0, "empty"},
		{"restore loop with a project file", run.edited(t, restoreLoop), &other, other, 0, "empty"},
		{"unreadable with a project file", nil, &other, other, 0, "empty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			validates := 0
			steps := realRestoreSteps()
			steps.validateProject = func(config project.Config) error {
				validates++
				return project.Validate(config)
			}
			store := &fakeStore{data: test.data}
			if test.data == nil {
				store.readErr = errors.New("disk failure")
			}
			s, err := newFromStore(t.Context(), StoreInput{Store: store, Project: test.project}, steps)
			if err != nil {
				t.Fatal(err)
			}
			if tier := s.State().Restore.Tier; tier != test.tier {
				t.Fatalf("restore tier = %q, want %q", tier, test.tier)
			}
			if same, err := sameProject(s.project, test.want); err != nil || !same {
				t.Fatalf("session project %q, want %q", s.project.Name, test.want.Name)
			}
			if validates != test.validates {
				t.Fatalf("project validations = %d, want %d", validates, test.validates)
			}
		})
	}
}

// TestNewFromStoreDemandChange restores the state that a session saved
// before a demand command, with the project file that the command wrote.
// This is a crash after the command wrote the project file and before the
// state save. No tick runs between the save and the command, so the result
// must be the session that a restore of a save after the command gives.
func TestNewFromStoreDemandChange(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Demand.Enabled = true
	store, file := &fakeStore{}, &projectFile{}
	live := startFromStore(t, StoreInput{Store: store, Project: &config, Options: []Option{WithProjectSaver(file.save)}})
	client := newTestClient(live, "live")
	client.mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: "market"})
	// Run until the demand stream has made orders and has budget for the
	// next one.
	advanceTicks(live, 90*sim.TicksPerSecond+17)
	periodicSave := func() []byte {
		t.Helper()
		if err := live.SaveState(t.Context(), SavePeriodic); err != nil {
			t.Fatal(err)
		}
		writes := store.writeList()
		return writes[len(writes)-1]
	}
	before := periodicSave()
	changeTestDemand(t, client)
	after := periodicSave()
	if len(file.saved) != 1 {
		t.Fatalf("the demand command wrote %d project files, want 1", len(file.saved))
	}
	written := file.saved[0]
	beforeFile, afterFile := decodeTestState(t, before), decodeTestState(t, after)
	if beforeFile.Demand.Budget == 0 || beforeFile.Demand.State.Generated == 0 {
		t.Fatalf("saved demand stream %+v made no orders or has no budget", beforeFile.Demand)
	}
	// The command changes only the project and the demand stream.
	if !reflect.DeepEqual(beforeFile.Simulation, afterFile.Simulation) {
		t.Fatal("the demand command changed the saved simulation")
	}

	crashStore, crashFile, handler := &fakeStore{data: before}, &projectFile{}, &recordHandler{}
	crash := startFromStore(t, StoreInput{Store: crashStore, Project: &written, Options: []Option{
		WithLogger(slog.New(handler)), WithProjectSaver(crashFile.save),
	}})
	// The project file has the demand settings already.
	if len(crashFile.saved) != 0 {
		t.Fatalf("the restore wrote %d project files, want 0", len(crashFile.saved))
	}
	plain := startFromStore(t, StoreInput{Store: &fakeStore{data: after}, Project: &written})
	unchanged := startFromStore(t, StoreInput{Store: &fakeStore{data: before}, Project: &config})
	for _, restored := range []*Session{crash, plain, unchanged} {
		if got := restored.State().Restore; got != (RestoreInfo{Tier: "physical"}) {
			t.Fatalf("restore = %+v, want the physical tier", got)
		}
	}
	// The demand change increases the project revision once more than a
	// restore without it, as the command did.
	if unchanged.projectRevision != beforeFile.ProjectRevision || crash.projectRevision != beforeFile.ProjectRevision+1 ||
		crash.projectRevision != plain.projectRevision || crash.projectOrigin != crash.projectRevision {
		t.Fatalf("project revisions: crash %d with origin %d, plain %d, unchanged %d; saved %d",
			crash.projectRevision, crash.projectOrigin, plain.projectRevision, unchanged.projectRevision,
			beforeFile.ProjectRevision)
	}
	if same, err := sameProject(crash.project, written); err != nil || !same {
		t.Fatalf("restored project has demand %+v, want %+v", crash.project.Demand, written.Demand)
	}
	// The simulation is the saved one.
	if exported := crash.simulation.ExportState(); !reflect.DeepEqual(exported, unchanged.simulation.ExportState()) ||
		!reflect.DeepEqual(exported, plain.simulation.ExportState()) {
		t.Fatal("the restored simulation is not the saved simulation")
	}
	// The demand stream is the stream of the live session after the
	// command.
	for _, other := range []*Session{live, plain} {
		want, got := other.demand.clone(), crash.demand.clone()
		wantRandom, err := want.pcg.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		gotRandom, err := got.pcg.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if got.state != want.state || got.budget != want.budget || !bytes.Equal(gotRandom, wantRandom) ||
			!reflect.DeepEqual(got.pickupWeights, want.pickupWeights) {
			t.Fatalf("restored demand %+v with budget %d, want %+v with budget %d",
				got.state, got.budget, want.state, want.budget)
		}
		for draw := range 200 {
			wantFrom, wantTo := want.nextPair()
			if from, to := got.nextPair(); from != wantFrom || to != wantTo {
				t.Fatalf("draw %d = %s to %s, want %s to %s", draw, from, to, wantFrom, wantTo)
			}
		}
	}
	// The next orders are the same as after a restore of a save after the
	// command.
	advanceTicks(crash, 2*60*sim.TicksPerSecond)
	advanceTicks(plain, 2*60*sim.TicksPerSecond)
	crashState, plainState := crash.State(), plain.State()
	if crashState.Demand != plainState.Demand || !reflect.DeepEqual(crashState.Simulation, plainState.Simulation) {
		t.Fatalf("after 2 simulated minutes, demand %+v and %d orders, want %+v and %d",
			crashState.Demand, crashState.Simulation.Submitted, plainState.Demand, plainState.Simulation.Submitted)
	}
	if crashState.Demand.Generated == 0 {
		t.Fatal("the restored demand stream made no orders in 2 simulated minutes")
	}
	checkRecords(t, handler, []wantRecord{
		startupSaved,
		{slog.LevelInfo, "Restored session", map[string]any{"tier": "physical"}},
		{slog.LevelInfo, "Applied demand settings of the project file", map[string]any{
			"savedDemand": config.Demand, "demand": written.Demand,
		}},
	})
	// The startup save has the project of the project file, so the next
	// start does not change the demand settings again.
	startup := crashStore.lastWrite(t)
	if startup.ProjectRevision != beforeFile.ProjectRevision+1 || startup.Project.Demand != written.Demand {
		t.Fatalf("startup save has project revision %d and demand %+v, want %d and %+v",
			startup.ProjectRevision, startup.Project.Demand, beforeFile.ProjectRevision+1, written.Demand)
	}
	next := startFromStore(t, StoreInput{Store: crashStore, Project: &written})
	if next.projectRevision != startup.ProjectRevision {
		t.Fatalf("next restore has project revision %d, want %d", next.projectRevision, startup.ProjectRevision)
	}
}

// newDemoRun runs the traffic demo of the example project for 1 simulated
// second. Then it closes the session and saves its final state.
func newDemoRun(t *testing.T) storedRun {
	t.Helper()
	config := project.Default()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	newTestClient(s, "demo").mustApply(t, Command{Action: "demo"})
	advanceTicks(s, sim.TicksPerSecond)
	if !s.State().Simulation.Demo {
		t.Fatal("the traffic demo does not run")
	}
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	writes := store.writeList()
	data := writes[len(writes)-1]
	return storedRun{session: s, data: data, file: decodeTestState(t, data)}
}

// TestNewFromStoreProjectDifferences restores a saved state with project
// files that differ from the saved project. Only a change of the demand
// settings keeps the saved state, and not while the traffic demo runs.
func TestNewFromStoreProjectDifferences(t *testing.T) {
	t.Parallel()
	run, demo := newStoredRun(t), newDemoRun(t)
	otherDemand := DemandConfig{Enabled: true, PerMinute: 30, Pattern: "balanced", Seed: 9}
	changeDemand := func(config *project.Config) { config.Demand = otherDemand }
	disableDemand := func(config *project.Config) { config.Demand.Enabled = false }
	renameStation := func(config *project.Config) { config.Network.Stations[0].Name = "Renamed station" }
	addProfile := func(config *project.Config) { config.DemandProfiles = profileDemandProject().DemandProfiles }
	tests := []struct {
		name string
		data []byte
		// change makes the project file from the saved project.
		change func(*project.Config)
		// tier is the restore tier. An empty tier has the reason
		// project_changed.
		tier string
		// changed tells whether the restore applies the demand settings of
		// the project file.
		changed bool
	}{
		{"same project", run.data, nil, "physical", false},
		{"demand settings", run.data, changeDemand, "physical", true},
		{"demand turned off", run.data, disableDemand, "physical", true},
		{"logical tier and demand settings", run.edited(t, logicalOnly), changeDemand, "logical", true},
		{"network", run.data, renameStation, "empty", false},
		{"demand profiles", run.data, addProfile, "empty", false},
		{"demand settings and network", run.data, func(config *project.Config) {
			changeDemand(config)
			renameStation(config)
		}, "empty", false},
		{"traffic demo and same project", demo.data, nil, "physical", false},
		{"traffic demo and demand settings", demo.data, changeDemand, "empty", false},
		// The logical tier stops the demo, so a demand command can run.
		{"traffic demo ended by the logical tier", demo.edited(t, logicalOnly), changeDemand, "logical", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			saved := decodeTestState(t, test.data)
			config := project.Clone(saved.Project)
			if test.change != nil {
				test.change(&config)
			}
			if err := project.Validate(config); err != nil {
				t.Fatalf("test project file is not valid: %v", err)
			}
			store := &fakeStore{data: test.data}
			s := startFromStore(t, StoreInput{Store: store, Project: &config})
			restore := s.State().Restore
			if restore.Tier != test.tier || test.tier == "empty" && restore.Reason != reasonProjectChanged {
				t.Fatalf("restore = %+v, want tier %q", restore, test.tier)
			}
			// Each start uses the project file.
			if same, err := sameProject(s.project, config); err != nil || !same {
				t.Fatalf("session project has demand %+v, want %+v", s.project.Demand, config.Demand)
			}
			if startup := store.lastWrite(t); startup.Project.Demand != config.Demand {
				t.Fatalf("startup save has demand %+v, want %+v", startup.Project.Demand, config.Demand)
			}
			if test.tier == "empty" {
				return
			}
			wantRevision := saved.ProjectRevision
			if test.changed {
				wantRevision++
			}
			if s.projectRevision != wantRevision || s.projectOrigin != wantRevision {
				t.Fatalf("project revision %d and origin %d, want %d", s.projectRevision, s.projectOrigin, wantRevision)
			}
			// The demand stream changes as the demand command changes it.
			// Enabled settings start a new stream. Settings that turn demand
			// off keep the stream and its counts.
			want, wantBudget := saved.Demand.State, saved.Demand.Budget
			switch {
			case !test.changed:
			case config.Demand.Enabled:
				want, wantBudget = DemandState{Config: config.Demand}, 0
			default:
				want.Config = config.Demand
			}
			if s.demand.state != want || s.demand.budget != wantBudget {
				t.Fatalf("demand %+v with budget %d, want %+v with budget %d", s.demand.state, s.demand.budget, want, wantBudget)
			}
		})
	}
}

// TestNewFromStoreRedistribution checks that a restored project with
// redistribution moves an idle pod, as the live session does. The saved
// simulation does not hold the setting, so the restore sets it from the
// project.
func TestNewFromStoreRedistribution(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Redistribution = true
	store := &fakeStore{}
	live := startFromStore(t, StoreInput{Store: store, Project: &config})
	// The trip frees a pickup station, so redistribution has a station to
	// send an empty pod to.
	newTestClient(live, "trip").mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: "market"})
	if err := live.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatal(err)
	}
	restored := startFromStore(t, StoreInput{Store: &fakeStore{data: store.writeList()[1]}})
	if tier := restored.State().Restore.Tier; tier != "physical" {
		t.Fatalf("restore tier = %q, want physical", tier)
	}
	sessions := []struct {
		name    string
		session *Session
	}{{"live", live}, {"restored", restored}}
	for _, run := range sessions {
		moves := 0
		for second := 0; moves == 0 && second < 300; second++ {
			advanceTicks(run.session, sim.TicksPerSecond)
			moves = run.session.State().Simulation.RebalanceMoves
		}
		if moves == 0 {
			t.Fatalf("the %s session moved no idle pod in 300 simulated seconds", run.name)
		}
	}
}

// retryRun is the final saved state of a session in which the client
// "api" ordered a trip with sequence 1, and the client "idle" paused the
// session with sequences 1 to 3.
type retryRun struct {
	config project.Config
	trip   Command
	data   []byte
}

// newRetryRun makes a retryRun of the example project.
func newRetryRun(t *testing.T) retryRun {
	t.Helper()
	config := project.Default()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	trip := newTestClient(s, "api").next(Command{Action: "trip", Origin: "harbor", Destination: "market"})
	if reply := s.Apply(trip); reply.Error != "" || reply.OrderID != 1 {
		t.Fatalf("trip reply = %+v, want order 1", reply)
	}
	idle := newTestClient(s, "idle")
	for range 3 {
		idle.mustApply(t, Command{Action: "pause", Paused: true})
	}
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	writes := store.writeList()
	return retryRun{config: config, trip: trip, data: writes[len(writes)-1]}
}

// restore restores the final state of run after edit changed it. edit can
// be nil. It stops the test when the restore does not keep the epoch.
func (run retryRun) restore(t *testing.T, edit func(*stateFile)) (*Session, *fakeStore) {
	t.Helper()
	file := decodeTestState(t, run.data)
	if edit != nil {
		edit(&file)
	}
	store := &fakeStore{data: encodeTestState(t, file)}
	s := startFromStore(t, StoreInput{Store: store, Project: &run.config})
	if state := s.State(); state.Epoch != run.trip.Epoch || state.Restore.Tier != "physical" {
		t.Fatalf("restore %+v in epoch %q, want the physical tier in epoch %q", state.Restore, state.Epoch, run.trip.Epoch)
	}
	return s, store
}

// restartRefusal is the reply message for a command from before a restore.
const restartRefusal = "The server restarted after this command. Review the current state."

// TestNewFromStoreRefusesReplays restores a final state with the saved epoch.
// A command from before the restore then gets ExpiredCommand and changes
// nothing. A command with a higher sequence runs as usual.
func TestNewFromStoreRefusesReplays(t *testing.T) {
	t.Parallel()
	run := newRetryRun(t)
	restoredSequences := []savedSequence{{Client: "api", Sequence: 1}, {Client: "idle", Sequence: 3}}
	if final := decodeTestState(t, run.data); !reflect.DeepEqual(final.Sequences, restoredSequences) {
		t.Fatalf("final save has sequences %+v, want %+v", final.Sequences, restoredSequences)
	}
	s, store := run.restore(t, nil)
	if startup := store.lastWrite(t); !reflect.DeepEqual(startup.Sequences, restoredSequences) {
		t.Fatalf("startup save has sequences %+v, want %+v", startup.Sequences, restoredSequences)
	}
	otherTrip := run.trip
	otherTrip.Destination = "garden"
	idleCommand := func(sequence uint64) Command {
		return Command{Client: "idle", Sequence: sequence, Epoch: run.trip.Epoch, Action: "pause"}
	}
	before := s.State()
	if before.Simulation.Submitted != 1 {
		t.Fatalf("restored session has %d orders, want 1", before.Simulation.Submitted)
	}
	replays := []struct {
		name    string
		command Command
	}{
		{"exact retry", run.trip},
		{"other command with the same sequence", otherTrip},
		{"exact retry of the last sequence", idleCommand(3)},
		{"lower sequence", idleCommand(2)},
	}
	for _, replay := range replays {
		reply := s.Apply(replay.command)
		if reply.ErrorCode != ExpiredCommand || reply.Error != restartRefusal {
			t.Fatalf("%s: reply = %+v, want %s", replay.name, reply, ExpiredCommand)
		}
	}
	if after := s.State(); !reflect.DeepEqual(after, before) {
		t.Fatalf("refused commands changed the state:\n got %+v\nwant %+v", after, before)
	}

	next := run.trip
	next.Sequence = 2
	accepted := s.Apply(next)
	if accepted.Error != "" || accepted.OrderID != 2 {
		t.Fatalf("trip with sequence 2: reply = %+v, want order 2", accepted)
	}
	if retry := s.Apply(next); retry != accepted {
		t.Fatalf("retry of sequence 2: reply = %+v, want %+v", retry, accepted)
	}
	if reply := s.Apply(run.trip); reply.ErrorCode != ExpiredCommand {
		t.Fatalf("trip with sequence 1 after sequence 2: reply = %+v, want %s", reply, ExpiredCommand)
	}
	newTestClient(s, "new").mustApply(t, Command{Action: "pause", Paused: true})
	if submitted := s.State().Simulation.Submitted; submitted != 2 {
		t.Fatalf("session has %d orders, want 2", submitted)
	}
	// A save keeps the restored sequence of a client that sent no command
	// after the restore.
	if err := s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatal(err)
	}
	want := []savedSequence{{Client: "api", Sequence: 2}, {Client: "idle", Sequence: 3}, {Client: "new", Sequence: 1}}
	if periodic := store.lastWrite(t); !reflect.DeepEqual(periodic.Sequences, want) {
		t.Fatalf("periodic save has sequences %+v, want %+v", periodic.Sequences, want)
	}
}

// TestRestoredSequenceRules checks that the restored sequences stay after
// each change that keeps the command receipts.
func TestRestoredSequenceRules(t *testing.T) {
	t.Parallel()
	run := newRetryRun(t)
	tests := []struct {
		name   string
		change func(*testing.T, *testClient)
	}{
		{"reset", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "reset"})
		}},
		{"demo", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "demo"})
		}},
		{"project apply", applyTestProject},
		{"demand change", changeTestDemand},
		{"rewind", func(t *testing.T, client *testClient) {
			t.Helper()
			id := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
			advanceTicks(client.session, 20)
			client.mustApply(t, Command{Action: "rewind", Checkpoint: id})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, _ := run.restore(t, nil)
			test.change(t, newTestClient(s, "other"))
			submitted := s.State().Simulation.Submitted
			if reply := s.Apply(run.trip); reply.ErrorCode != ExpiredCommand || reply.Error != restartRefusal {
				t.Fatalf("retry after the %s: reply = %+v, want %s", test.name, reply, ExpiredCommand)
			}
			if got := s.State().Simulation.Submitted; got != submitted {
				t.Fatalf("retry after the %s: %d orders, want %d", test.name, got, submitted)
			}
		})
	}
}

// TestRestoredClientLimit checks that the restored clients count toward the
// client limit.
func TestRestoredClientLimit(t *testing.T) {
	t.Parallel()
	sequences := testSequences(clientLimit - 1)
	s, _ := newRetryRun(t).restore(t, func(file *stateFile) { file.Sequences = sequences })
	epoch, restored := s.State().Epoch, sequences[0]
	steps := []struct {
		name    string
		command Command
		want    CommandErrorCode
	}{
		{"new client in the free place", Command{Client: "new0", Sequence: 1}, ""},
		{"new client at the limit", Command{Client: "new1", Sequence: 1}, ClientLimit},
		// A restored client with a higher sequence does not use one more
		// place.
		{"restored client at the limit", Command{Client: restored.Client, Sequence: restored.Sequence + 1}, ""},
	}
	for _, step := range steps {
		command := step.command
		command.Epoch, command.Action = epoch, "pause"
		if reply := s.Apply(command); reply.ErrorCode != step.want {
			t.Fatalf("%s: reply = %+v, want error code %q", step.name, reply, step.want)
		}
	}
	if clients := len(s.receipts) + len(s.restoredSequences); clients != clientLimit {
		t.Fatalf("session records %d clients, want %d", clients, clientLimit)
	}
}

// TestClientLimitRestart fills the client limit with commands and restarts
// the session after a final save. The restart uses a new epoch, so that the
// session accepts new clients again.
func TestClientLimitRestart(t *testing.T) {
	t.Parallel()
	config := project.Default()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	first := newTestClient(s, "page0000")
	firstCommand := first.next(Command{Action: "pause", Paused: true})
	if reply := s.Apply(firstCommand); reply.Error != "" {
		t.Fatalf("first client: reply = %+v", reply)
	}
	for index := 1; index < clientLimit; index++ {
		newTestClient(s, fmt.Sprintf("page%04d", index)).mustApply(t, Command{Action: "pause", Paused: true})
	}
	late := newTestClient(s, "late").next(Command{Action: "pause"})
	if reply := s.Apply(late); reply.ErrorCode != ClientLimit ||
		reply.Error != "The session client limit was reached. Restart the server." {
		t.Fatalf("client after the limit: reply = %+v, want %s", reply, ClientLimit)
	}
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	if final := store.lastWrite(t); !final.Final || len(final.Sequences) != clientLimit {
		t.Fatalf("final save has final %t and %d client sequences, want %d", final.Final, len(final.Sequences), clientLimit)
	}

	restarted := startFromStore(t, StoreInput{Store: store, Project: &config})
	if state := restarted.State(); state.Restore.Tier != "physical" || state.Epoch == firstCommand.Epoch {
		t.Fatalf("restore %+v in epoch %q, want the physical tier in a new epoch", state.Restore, state.Epoch)
	}
	if clients := len(restarted.receipts) + len(restarted.restoredSequences); clients != 0 {
		t.Fatalf("restarted session records %d clients, want 0", clients)
	}
	if startup := store.lastWrite(t); startup.Sequences != nil {
		t.Fatalf("startup save has %d client sequences, want none", len(startup.Sequences))
	}
	if reply := restarted.Apply(firstCommand); reply.ErrorCode != SessionChanged {
		t.Fatalf("retry from before the restart: reply = %+v, want %s", reply, SessionChanged)
	}
	newTestClient(restarted, "late").mustApply(t, Command{Action: "pause"})
}

func TestSaveStateRules(t *testing.T) {
	t.Parallel()
	type step func(context.Context, *Session) error
	periodic := func(ctx context.Context, s *Session) error { return s.SaveState(ctx, SavePeriodic) }
	final := func(ctx context.Context, s *Session) error { return s.SaveState(ctx, SaveFinal) }
	command := func(ctx context.Context, s *Session) error { return s.SaveState(ctx, saveCommand) }
	unknown := func(ctx context.Context, s *Session) error { return s.SaveState(ctx, 0) }
	closeSession := func(_ context.Context, s *Session) error { s.Close(); return nil }
	advance := func(_ context.Context, s *Session) error { s.advance(); return nil }
	tests := []struct {
		name string
		// steps change the session and save it. The test checks the error of
		// the last step.
		steps []step
		// writes counts the writes after the startup save.
		writes int
		final  bool
		failed bool
	}{
		{"first periodic save", []step{periodic}, 1, false, false},
		{"unchanged revision", []step{periodic, periodic}, 1, false, false},
		{"changed revision", []step{periodic, advance, periodic}, 2, false, false},
		{"final save of an open session", []step{final}, 0, false, true},
		{"periodic save after Close", []step{closeSession, periodic}, 0, false, false},
		{"final save without a change", []step{periodic, closeSession, final}, 2, true, false},
		{"first command save", []step{command}, 1, false, false},
		{"command save after a periodic save", []step{periodic, command}, 1, false, false},
		{"periodic save after a command save", []step{command, periodic}, 1, false, false},
		{"command save after a change", []step{command, advance, command}, 2, false, false},
		{"command save after Close", []step{closeSession, command}, 0, false, false},
		{"unknown kind", []step{unknown}, 0, false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{}
			s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
			var err error
			for _, step := range test.steps {
				err = step(t.Context(), s)
			}
			if (err != nil) != test.failed {
				t.Fatalf("save error = %v, want failure %t", err, test.failed)
			}
			if writes := len(store.writeList()) - 1; writes != test.writes {
				t.Fatalf("saves wrote %d times, want %d", writes, test.writes)
			}
			if last := store.lastWrite(t); last.Final != test.final || last.Revision != s.State().Revision {
				t.Fatalf("last save is final %t at revision %d, want %t at %d",
					last.Final, last.Revision, test.final, s.State().Revision)
			}
		})
	}
	t.Run("no store", func(t *testing.T) {
		t.Parallel()
		if err := newTestSession(t).SaveState(t.Context(), SavePeriodic); !errors.Is(err, ErrStateSavingOff) {
			t.Fatalf("save error = %v, want %v", err, ErrStateSavingOff)
		}
	})
}

func TestSaveStateLogs(t *testing.T) {
	t.Parallel()
	diskErr := errors.New("disk full")
	store, handler := &fakeStore{}, &recordHandler{}
	now := time.Date(2026, time.September, 23, 9, 0, 0, 0, time.UTC)
	s := startFromStore(t, StoreInput{
		Store: store, Options: []Option{WithLogger(slog.New(handler)), withClock(func() time.Time { return now })},
	})
	handler.session.Store(s)
	checkRecords(t, handler, []wantRecord{noSavedState, startupSaved})
	handler.reset()
	store.setWriteErr(diskErr)
	for range 2 {
		s.advance()
		if err := s.SaveState(t.Context(), SavePeriodic); !errors.Is(err, diskErr) {
			t.Fatalf("save error = %v, want %v", err, diskErr)
		}
	}
	store.setWriteErr(nil)
	s.advance()
	if err := s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	failed := func(failures int64) wantRecord {
		return wantRecord{slog.LevelWarn, "Save session state", map[string]any{
			"kind": "periodic", "error": func(value any) bool { err, _ := value.(error); return errors.Is(err, diskErr) },
			"cause": nil, "failures": failures,
		}}
	}
	positive := func(value any) bool { size, _ := value.(int64); return size > 0 }
	saved := func(level slog.Level, message, kind string) wantRecord {
		return wantRecord{level, message, map[string]any{
			"kind": kind, "revision": uint64(3), "tick": int64(3), "bytes": positive,
			"lock": nonEmpty, "encode": nonEmpty, "write": nonEmpty,
		}}
	}
	checkRecords(t, handler, []wantRecord{
		failed(1), failed(2),
		saved(slog.LevelDebug, "Saved session state", "periodic"),
		saved(slog.LevelInfo, "Saved final session state", "final"),
	})
	if count := handler.underLock.Load(); count != 0 {
		t.Fatalf("the session logged %d records while it held its lock", count)
	}
	if final := store.lastWrite(t); !final.SavedAt.Equal(now) {
		t.Fatalf("final save time = %v, want %v", final.SavedAt, now)
	}
	if saves, failures := s.persist.saves.Load(), s.persist.failures.Load(); saves != 3 || failures != 2 {
		t.Fatalf("the session counted %d saves and %d failures, want 3 and 2", saves, failures)
	}
	wantSavedAt := int64(now.Sub(s.persist.started))
	if revision, savedAt := s.persist.savedRevision.Load(), s.persist.savedAt.Load(); revision != 3 || savedAt != wantSavedAt {
		t.Fatalf("last good save at revision %d and time %d", revision, savedAt)
	}
}

// TestMetricsReportStateSaves checks the state fields of Metrics at 30 s and
// at 90 s after the start on the clock of the saves.
func TestMetricsReportStateSaves(t *testing.T) {
	t.Parallel()
	started := time.Date(2026, time.September, 23, 9, 0, 0, 0, time.UTC)
	diskErr := errors.New("disk full")
	type startFunc func(t *testing.T, store *fakeStore, clock func() time.Time) *Session
	fromStore := func(t *testing.T, store *fakeStore, clock func() time.Time) *Session {
		t.Helper()
		return startFromStore(t, StoreInput{
			Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{})), withClock(clock)},
		})
	}
	// save saves once for each write error in errs. Before each save, it
	// advances the session one tick.
	save := func(errs ...error) func(*testing.T, *Session, *fakeStore) {
		return func(t *testing.T, s *Session, store *fakeStore) {
			t.Helper()
			for _, err := range errs {
				store.setWriteErr(err)
				s.advance()
				if got := s.SaveState(t.Context(), SavePeriodic); !errors.Is(got, err) {
					t.Fatalf("save error = %v, want %v", got, err)
				}
			}
		}
	}
	tests := []struct {
		name  string
		start startFunc
		// change changes the session at changeAt after the start. It can be
		// nil.
		change              func(t *testing.T, s *Session, store *fakeStore)
		changeAt            time.Duration
		configured, enabled bool
		saves, failures     uint64
		// unsaved holds StateUnsavedSeconds at 30 s and at 90 s.
		unsaved [2]float64
	}{
		{name: "no store", start: func(t *testing.T, _ *fakeStore, clock func() time.Time) *Session {
			t.Helper()
			s, err := NewWithProject(project.Default(), withClock(clock))
			if err != nil {
				t.Fatal(err)
			}
			return s
		}},
		// NewFromStore saves before it returns, so this session starts
		// without it.
		{name: "no good save", start: func(t *testing.T, store *fakeStore, clock func() time.Time) *Session {
			t.Helper()
			s := newSession(newPersistence(store), []Option{withClock(clock)})
			s.persist.started = s.persist.now()
			if err := s.startProject(project.Default()); err != nil {
				t.Fatal(err)
			}
			return s
		}, configured: true, enabled: true, unsaved: [2]float64{30, 90}},
		{
			name: "saved revision", start: fromStore, change: save(nil),
			configured: true, enabled: true, saves: 2,
		},
		{
			name: "good and bad save", start: fromStore, change: save(diskErr),
			configured: true, enabled: true, saves: 1, failures: 1, unsaved: [2]float64{30, 90},
		},
		{
			name: "good save later", start: fromStore, change: save(nil, diskErr), changeAt: 20 * time.Second,
			configured: true, enabled: true, saves: 2, failures: 1, unsaved: [2]float64{10, 70},
		},
		// At 30 s, the clock is before the last good save.
		{
			name: "clock went back", start: fromStore, change: save(nil, diskErr), changeAt: 60 * time.Second,
			configured: true, enabled: true, saves: 2, failures: 1, unsaved: [2]float64{0, 30},
		},
		{name: "saving off", start: func(t *testing.T, store *fakeStore, clock func() time.Time) *Session {
			t.Helper()
			store.readErr = diskErr
			return fromStore(t, store, clock)
		}, configured: true, unsaved: [2]float64{30, 90}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			now := started
			store := &fakeStore{}
			s := test.start(t, store, func() time.Time { return now })
			if test.change != nil {
				now = started.Add(test.changeAt)
				test.change(t, s, store)
			}
			want := Metrics{
				StateConfigured: test.configured, StateEnabled: test.enabled,
				StateSaves: test.saves, StateSaveErrors: test.failures,
			}
			if writes := store.writeList(); len(writes) > 0 {
				want.StateBytes = int64(len(writes[len(writes)-1]))
			}
			for index, elapsed := range []time.Duration{30 * time.Second, 90 * time.Second} {
				now = started.Add(elapsed)
				want.StateUnsavedSeconds = test.unsaved[index]
				if got := stateMetrics(s.Metrics()); got != want {
					t.Fatalf("state metrics at %v = %+v, want %+v", elapsed, got, want)
				}
			}
		})
	}
}

// stateMetrics returns the state fields of metrics.
func stateMetrics(metrics Metrics) Metrics {
	return Metrics{
		StateConfigured: metrics.StateConfigured, StateEnabled: metrics.StateEnabled,
		StateSaves: metrics.StateSaves, StateSaveErrors: metrics.StateSaveErrors,
		StateBytes: metrics.StateBytes, StateUnsavedSeconds: metrics.StateUnsavedSeconds,
	}
}

// TestMetricsDuringBlockedSave checks that Metrics does not wait for a save
// that holds persist.mu. Metrics must not lock persist.mu, because a save
// holds persist.mu while it waits for the session lock.
func TestMetricsDuringBlockedSave(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store})
	release := make(chan struct{})
	store.blockWrite = release
	s.advance()
	saved := make(chan error, 1)
	go func() { saved <- s.SaveState(t.Context(), SavePeriodic) }()
	// Wait until the save holds persist.mu. It holds it until the test
	// closes release.
	for s.persist.mu.TryLock() {
		s.persist.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	got := make(chan Metrics, 1)
	go func() { got <- s.Metrics() }()
	var metrics Metrics
	select {
	case metrics = <-got:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("Metrics waited for the save")
	}
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if metrics.StateSaves != 1 || metrics.StateSaveErrors != 0 {
		t.Fatalf("metrics during the save = %+v, want the startup save only", metrics)
	}
}

// TestMetricsDuringSaves reads Metrics while saves run. Run it with the race
// detector. When Metrics locks persist.mu, this test locks up until the test
// timeout, and TestMetricsDuringBlockedSave reports the cause.
func TestMetricsDuringSaves(t *testing.T) {
	t.Parallel()
	diskErr := errors.New("disk full")
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	const steps = 50
	var group sync.WaitGroup
	group.Go(func() { s.Run(ctx) })
	group.Go(func() { s.RunStateSaver(ctx, time.Millisecond) })
	group.Go(func() {
		defer cancel()
		for step := range steps {
			var err error
			if step%2 == 1 {
				err = diskErr
			}
			store.setWriteErr(err)
			s.advance()
			_ = s.SaveState(ctx, SavePeriodic)
			time.Sleep(time.Millisecond)
		}
	})
	var previous Metrics
	var failure string
	for ctx.Err() == nil && failure == "" {
		metrics := s.Metrics()
		switch {
		case metrics.StateSaves < previous.StateSaves || metrics.StateSaveErrors < previous.StateSaveErrors:
			failure = fmt.Sprintf("save counts went back from %+v to %+v", previous, metrics)
		case metrics.StateUnsavedSeconds < 0:
			failure = fmt.Sprintf("unsaved seconds = %v", metrics.StateUnsavedSeconds)
		}
		previous = metrics
	}
	group.Wait()
	if failure != "" {
		t.Fatal(failure)
	}
	metrics, writes := s.Metrics(), store.writeList()
	writeCalls := uint64(len(slices.DeleteFunc(store.callList(), func(call string) bool { return call != "write" })))
	if metrics.StateSaves != uint64(len(writes)) || metrics.StateSaves+metrics.StateSaveErrors != writeCalls ||
		metrics.StateBytes != int64(len(writes[len(writes)-1])) {
		t.Fatalf("metrics %+v do not match %d good writes of %d", metrics, len(writes), writeCalls)
	}
	// Each odd step fails, because no good save has its revision.
	if metrics.StateSaveErrors < steps/2 {
		t.Fatalf("%d failed saves, want at least %d", metrics.StateSaveErrors, steps/2)
	}
}

func TestNewFromStoreLogs(t *testing.T) {
	t.Parallel()
	run := newStoredRun(t)
	demote, demoted := run.demotedPod(t)
	savedAt := func(value any) bool { at, _ := value.(time.Time); return at.Equal(run.file.SavedAt) }
	tests := []struct {
		name string
		data []byte
		want []wantRecord
	}{
		{"no saved state", nil, []wantRecord{noSavedState, startupSaved}},
		{"physical restore", run.data, []wantRecord{startupSaved, {slog.LevelInfo, "Restored session", map[string]any{
			"tier": "physical", "reason": "", "demoted": int64(0), "requeued": int64(0), "dropped": int64(0),
			"droppedParties": int64(0), "overCap": int64(0), "overBudget": int64(0), "tick": run.file.Simulation.Tick,
			"epochKept": true, "final": true, "savedAt": savedAt, "savedBuild": testBuildID, "build": "",
			"restoreAttempts": int64(0), "bytes": int64(len(run.data)), "duration": nonEmpty,
		}}}},
		{"demotion", run.edited(t, demote), []wantRecord{
			{slog.LevelInfo, "Backed up saved session state", nil},
			startupSaved,
			{slog.LevelDebug, "Demoted pod", map[string]any{"pod": demoted}},
			{slog.LevelInfo, "Restored session", map[string]any{"tier": "physical", "demoted": int64(1)}},
		}},
		{"logical tier", run.edited(t, func(file *stateFile) { nonFinal(file); logicalOnly(file) }), []wantRecord{
			{slog.LevelInfo, "Backed up saved session state", nil},
			startupSaved,
			{slog.LevelInfo, "Restored session", map[string]any{
				"tier": "logical", "reason": reasonRestoreLoop, "requeued": int64(1),
				"epochKept": false, "final": false, "restoreAttempts": int64(1),
			}},
		}},
		{"physical tier fails", run.edited(t, sharedBerth), []wantRecord{
			{slog.LevelInfo, "Backed up saved session state", nil},
			startupSaved,
			{slog.LevelInfo, "Restored session", map[string]any{
				"tier": "logical", "reason": reasonPhysicalFailed, "physicalError": nonEmpty,
			}},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &recordHandler{}
			startFromStore(t, StoreInput{Store: &fakeStore{data: test.data}, Options: []Option{WithLogger(slog.New(handler))}})
			checkRecords(t, handler, test.want)
		})
	}
}

func TestStateSaverNudge(t *testing.T) {
	t.Parallel()
	demand := func(change int) Command {
		return Command{Action: "demand", Demand: DemandConfig{
			Enabled: true, PerMinute: 1 + change, Pattern: "balanced", Seed: uint64(change),
		}}
	}
	trip := func(int) Command { return Command{Action: "trip", Origin: "harbor", Destination: "market"} }
	tests := []struct {
		name string
		// command returns the command of each change.
		command func(change int) Command
		// changes is the number of commands, 100 ms apart.
		changes int
		// wait is the time from the first command to the check.
		wait time.Duration
		// minWrites and maxWrites bound the writes after the startup save.
		minWrites, maxWrites int
	}{
		{"before the delay", demand, 1, nudgeDelay - time.Millisecond, 0, 0},
		{"after the delay", demand, 1, nudgeDelay, 1, 1},
		// The delay starts at the first change, and later changes do not
		// start it again.
		{"ten changes at the delay", demand, 10, nudgeDelay + 50*time.Millisecond, 1, 1},
		{"ten changes in one second", demand, 10, 3 * time.Second, 1, 2},
		// A command that does not change the project waits for the interval.
		{"trips", trip, 10, 3 * time.Second, 0, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				store := &fakeStore{}
				s := startFromStore(t, StoreInput{Store: store})
				ctx, cancel := context.WithCancel(t.Context())
				var saver sync.WaitGroup
				saver.Go(func() { s.RunStateSaver(ctx, time.Minute) })
				defer saver.Wait()
				defer cancel()
				started := time.Now()
				client := newTestClient(s, "nudge")
				for change := range test.changes {
					client.mustApply(t, test.command(change))
					time.Sleep(100 * time.Millisecond)
				}
				time.Sleep(test.wait - time.Since(started))
				synctest.Wait()
				writes := len(store.writeList()) - 1
				if writes < test.minWrites || writes > test.maxWrites {
					t.Fatalf("the saver wrote %d times, want %d to %d", writes, test.minWrites, test.maxWrites)
				}
				if last := store.lastWrite(t); writes > 0 && last.ProjectRevision != s.State().ProjectRevision {
					t.Fatalf("last save has project revision %d, want %d", last.ProjectRevision, s.State().ProjectRevision)
				}
			})
		})
	}
}

// TestStateSaverConcurrency runs the clock, commands, readers, and saves at
// the same time. Run it with the race detector. Each saved state must
// restore.
func TestStateSaverConcurrency(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Demand.Enabled = true
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config})
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	var group sync.WaitGroup
	group.Go(func() { s.Run(ctx) })
	group.Go(func() { s.RunStateSaver(ctx, time.Millisecond) })
	group.Go(func() {
		client := newTestClient(s, "commands")
		stations := []string{"harbor", "garden", "market"}
		for index := 0; ctx.Err() == nil; index++ {
			command := Command{Action: "trip", Origin: stations[index%3], Destination: stations[(index+1)%3]}
			if index%5 == 0 {
				command = Command{Action: "demand", Demand: DemandConfig{
					Enabled: true, PerMinute: 1 + index%120, Pattern: "balanced", Seed: uint64(index),
				}}
			}
			s.Apply(client.next(command))
			time.Sleep(time.Millisecond)
		}
	})
	group.Go(func() {
		for ctx.Err() == nil {
			s.Frame()
			s.Metrics()
			time.Sleep(time.Millisecond)
		}
	})
	group.Wait()
	s.Close()
	if err := s.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	writes := store.writeList()
	if len(writes) < 3 {
		t.Fatalf("the store has %d writes, want at least 3", len(writes))
	}
	for index, data := range writes {
		restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: data}})
		if err != nil {
			t.Fatalf("write %d: %v", index, err)
		}
		if tier := restored.State().Restore.Tier; tier != "physical" && tier != "logical" {
			t.Fatalf("write %d restored with tier %q", index, tier)
		}
	}
	t.Logf("restored %d saved states", len(writes))
}

// TestApplySavesBeforeReply checks which commands save the session state
// before Apply returns. No saver runs, so each write after the startup save
// comes from Apply.
func TestApplySavesBeforeReply(t *testing.T) {
	t.Parallel()
	pause := Command{Action: "pause", Paused: true}
	// send returns a command that needs no other change.
	send := func(command Command) func(*testing.T, *testClient) Command {
		return func(_ *testing.T, client *testClient) Command { return client.next(command) }
	}
	// runFrom makes a save point, runs 1 simulated second, and returns the
	// ID of the save point.
	runFrom := func(t *testing.T, client *testClient) uint64 {
		t.Helper()
		id := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
		advanceTicks(client.session, sim.TicksPerSecond)
		return id
	}
	projectCommand := func(client *testClient) Command {
		config := customProject()
		return client.next(Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &config})
	}
	// appliedProject pauses the session, applies a project, and returns the
	// project apply. Its save writes.
	appliedProject := func(t *testing.T, client *testClient) Command {
		t.Helper()
		client.mustApply(t, pause)
		command := projectCommand(client)
		if reply := client.session.Apply(command); reply.Error != "" {
			t.Fatalf("project: %s", reply.Error)
		}
		return command
	}
	// changeOther changes the session with another client. A save after
	// this change writes.
	changeOther := func(t *testing.T, client *testClient) {
		t.Helper()
		newTestClient(client.session, "other").mustApply(t, Command{Action: "speed", Speed: 2})
	}
	tests := []struct {
		name string
		// command changes the session and returns the command to check.
		command func(*testing.T, *testClient) Command
		// writes tells whether Apply writes a state before it replies.
		writes bool
		// code is the error code of the reply, or empty.
		code CommandErrorCode
	}{
		{"project apply", func(t *testing.T, client *testClient) Command {
			t.Helper()
			client.mustApply(t, pause)
			return projectCommand(client)
		}, true, ""},
		{"rewind that restores a project", func(t *testing.T, client *testClient) Command {
			t.Helper()
			id := runFrom(t, client)
			applyTestProject(t, client)
			return client.next(Command{Action: "rewind", Checkpoint: id})
		}, true, ""},
		{"rewind without a project restore", func(t *testing.T, client *testClient) Command {
			t.Helper()
			return client.next(Command{Action: "rewind", Checkpoint: runFrom(t, client)})
		}, false, ""},
		{"second rewind to the same save point", func(t *testing.T, client *testClient) Command {
			t.Helper()
			id := runFrom(t, client)
			applyTestProject(t, client)
			client.mustApply(t, Command{Action: "rewind", Checkpoint: id})
			advanceTicks(client.session, sim.TicksPerSecond)
			return client.next(Command{Action: "rewind", Checkpoint: id})
		}, false, ""},
		{"exact retry of a project apply", func(t *testing.T, client *testClient) Command {
			t.Helper()
			command := appliedProject(t, client)
			changeOther(t, client)
			return command
		}, true, ""},
		// The save of the first request holds the current state, so the save
		// of the retry writes nothing.
		{"exact retry of a project apply without a later change", func(t *testing.T, client *testClient) Command {
			t.Helper()
			return appliedProject(t, client)
		}, false, ""},
		{"exact retry of a rewind that restores a project", func(t *testing.T, client *testClient) Command {
			t.Helper()
			id := runFrom(t, client)
			applyTestProject(t, client)
			command := client.next(Command{Action: "rewind", Checkpoint: id})
			if reply := client.session.Apply(command); reply.Error != "" || !reply.ProjectRestored {
				t.Fatalf("rewind = %+v, want a project restore", reply)
			}
			changeOther(t, client)
			return command
		}, true, ""},
		{"sequence conflict with a project apply", func(t *testing.T, client *testClient) Command {
			t.Helper()
			command := appliedProject(t, client)
			changeOther(t, client)
			command.ProjectRevision++
			return command
		}, false, SequenceConflict},
		// The receipt of the client is the second project apply, and its
		// saveState is true.
		{"expired project apply", func(t *testing.T, client *testClient) Command {
			t.Helper()
			command := appliedProject(t, client)
			if reply := client.session.Apply(projectCommand(client)); reply.Error != "" {
				t.Fatalf("second project: %s", reply.Error)
			}
			changeOther(t, client)
			return command
		}, false, ExpiredCommand},
		{"project apply while the clock runs", func(_ *testing.T, client *testClient) Command {
			return projectCommand(client)
		}, false, CommandRejected},
		{"rewind to an unknown save point", send(Command{Action: "rewind", Checkpoint: 99}), false, CommandRejected},
		{"demand change", send(Command{Action: "demand", Demand: DemandConfig{
			Enabled: true, PerMinute: 6, Pattern: "market", Seed: 3,
		}}), false, ""},
		{"trip", send(Command{Action: "trip", Origin: "harbor", Destination: "market"}), false, ""},
		{"pause", send(pause), false, ""},
		{"speed", send(Command{Action: "speed", Speed: 2}), false, ""},
		{"reset", send(Command{Action: "reset"}), false, ""},
		{"demo", send(Command{Action: "demo"}), false, ""},
		{"checkpoint", send(Command{Action: "checkpoint"}), false, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, handler := &fakeStore{}, &recordHandler{}
			s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(handler))}})
			client := newTestClient(s, "commands")
			advanceTicks(s, sim.TicksPerSecond)
			command := test.command(t, client)
			writes := len(store.writeList())
			handler.reset()
			reply := s.Apply(command)
			if reply.ErrorCode != test.code {
				t.Fatalf("reply = %+v, want error code %q", reply, test.code)
			}
			saved := store.writeList()[writes:]
			var kinds []any
			for _, record := range handler.list() {
				if record.message == "Saved session state" {
					kinds = append(kinds, record.attrs["kind"])
				}
			}
			if !test.writes {
				if len(saved) != 0 || len(kinds) != 0 {
					t.Fatalf("Apply wrote %d states and logged saves of kinds %v, want none", len(saved), kinds)
				}
				return
			}
			if len(saved) != 1 || !reflect.DeepEqual(kinds, []any{"command"}) {
				t.Fatalf("Apply wrote %d states and logged saves of kinds %v, want 1 of kind command", len(saved), kinds)
			}
			// The saved state is the state after Apply. After an exact retry,
			// it also holds the later change.
			current := s.reply()
			file := decodeTestState(t, saved[0])
			if file.Final || file.Revision != current.Revision || file.ProjectRevision != current.ProjectRevision ||
				file.Generation != current.Generation {
				t.Fatalf("saved state is final %t with revisions %d, %d and generation %d, want state %+v",
					file.Final, file.Revision, file.ProjectRevision, file.Generation, current)
			}
			if same, err := sameProject(file.Project, s.project); err != nil || !same {
				t.Fatalf("saved project %q, want %q", file.Project.Name, s.project.Name)
			}
			if !reflect.DeepEqual(file.Simulation, s.simulation.ExportState()) {
				t.Fatalf("saved simulation at tick %d is not the simulation after the command", file.Simulation.Tick)
			}
		})
	}
	t.Run("saving off", func(t *testing.T) {
		t.Parallel()
		store := &fakeStore{readErr: errors.New("disk failure")}
		s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
		applyTestProject(t, newTestClient(s, "off"))
		if calls := store.callList(); !reflect.DeepEqual(calls, []string{"read"}) {
			t.Fatalf("store calls = %q, want only the read", calls)
		}
	})
}

// TestApplySaveFailures applies a project while the store fails or blocks.
// The reply reports success. The saver writes the state about 1 s after
// the reply.
func TestApplySaveFailures(t *testing.T) {
	t.Parallel()
	diskErr := errors.New("disk full")
	isErr := func(target error) func(any) bool {
		return func(value any) bool { err, _ := value.(error); return errors.Is(err, target) }
	}
	tests := []struct {
		name     string
		writeErr error
		block    bool
		// wait is the time that Apply takes.
		wait time.Duration
		// failed is the log record of the failed save.
		failed wantRecord
	}{
		{"write error", diskErr, false, 0, wantRecord{slog.LevelWarn, "Save session state", map[string]any{
			"kind": "command", "error": isErr(diskErr), "cause": nil, "failures": int64(1),
		}}},
		// The save continues after the reply with an ended context, so its
		// write fails.
		{"blocked write", nil, true, commandSaveTimeout, wantRecord{slog.LevelWarn, "Save session state", map[string]any{
			"kind": "command", "error": isErr(errCommandSaveTimeout), "cause": errCommandSaveTimeout, "failures": int64(1),
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				store, handler := &fakeStore{checkContext: true}, &recordHandler{}
				s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(handler))}})
				client := newTestClient(s, "failure")
				client.mustApply(t, Command{Action: "pause", Paused: true})
				handler.reset()
				store.setWriteErr(test.writeErr)
				release := make(chan struct{})
				if test.block {
					store.blockWrite = release
				}
				started := time.Now()
				config := customProject()
				reply := client.mustApply(t, Command{Action: "project", ProjectRevision: s.Project().Revision, Project: &config})
				if elapsed := time.Since(started); elapsed != test.wait {
					t.Fatalf("Apply took %v, want %v", elapsed, test.wait)
				}
				if writes := len(store.writeList()); writes != 1 {
					t.Fatalf("the store has %d writes after the reply, want only the startup save", writes)
				}
				store.setWriteErr(nil)
				close(release)
				// Start the saver after the reply. A goroutine that waits for a
				// mutex stops the clock of the bubble, and the saver would wait
				// for the blocked save. The nudge of the command stays in its
				// channel until the saver starts.
				ctx, cancel := context.WithCancel(t.Context())
				var saver sync.WaitGroup
				saver.Go(func() { s.RunStateSaver(ctx, time.Minute) })
				defer saver.Wait()
				defer cancel()
				time.Sleep(nudgeDelay)
				synctest.Wait()
				if last := store.lastWrite(t); last.Revision != reply.Revision || last.ProjectRevision != reply.ProjectRevision {
					t.Fatalf("last save has revisions %d and %d, want reply %+v", last.Revision, last.ProjectRevision, reply)
				}
				checkRecords(t, handler, []wantRecord{
					test.failed,
					{slog.LevelDebug, "Saved session state", map[string]any{"kind": "periodic"}},
				})
			})
		})
	}
}

// TestApplySaveRetryWaits sends an exact retry of a project apply while the
// save of the first request blocks in its write. The browser client sends
// such a retry when it gets no reply in 3 s. The retry must reply only after
// that write completes. A goroutine that waits for persist.mu does not block
// durably, so the clock of a synctest bubble stops while the retry waits.
// Thus the test uses real time.
func TestApplySaveRetryWaits(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
	client := newTestClient(s, "retry")
	client.mustApply(t, Command{Action: "pause", Paused: true})
	release := make(chan struct{})
	store.blockWrite = release
	config := customProject()
	command := client.next(Command{Action: "project", ProjectRevision: s.Project().Revision, Project: &config})
	first := make(chan Reply, 1)
	go func() { first <- s.Apply(command) }()
	// Wait until the save of the first request holds persist.mu. It holds
	// it until the test closes release.
	for s.persist.mu.TryLock() {
		s.persist.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	type result struct {
		reply Reply
		// writes is the number of written states when the retry replied.
		writes int
	}
	retried := make(chan result, 1)
	go func() {
		reply := s.Apply(command)
		retried <- result{reply: reply, writes: len(store.writeList())}
	}()
	// A retry that does not wait for the save replies in this time. A retry
	// that waits cannot reply before commandSaveTimeout.
	select {
	case early := <-retried:
		close(release)
		t.Fatalf("the retry replied with %d written states while the first save wrote", early.writes)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	var retry result
	select {
	case retry = <-retried:
	case <-time.After(10 * time.Second):
		t.Fatal("the retry did not reply after the write")
	}
	reply := <-first
	if reply.Error != "" || retry.reply != reply {
		t.Fatalf("retry = %+v, first reply = %+v, want the same successful reply", retry.reply, reply)
	}
	// These are the startup save and the save of the first request. The
	// save of the retry writes nothing, because the state did not change.
	if writes := len(store.writeList()); retry.writes != 2 || writes != 2 {
		t.Fatalf("the store had %d writes at the retry reply and %d after it, want 2", retry.writes, writes)
	}
}

// TestApplySaveRestores restores the state that Apply saved before its
// reply, with the project file that the command wrote. This is a crash
// right after the reply. The command changes only the demand settings of
// the project. A restore of the state from before the command would thus
// keep the earlier simulation and apply the new demand settings. The
// restore must give the session after the command.
func TestApplySaveRestores(t *testing.T) {
	t.Parallel()
	config := project.Default()
	config.Demand.Enabled = true
	// The live session starts after one restore without a later save, so
	// its startup save counts two restores. A command save that kept this
	// count would make the next start reject the saved state.
	first := &fakeStore{}
	previous := startFromStore(t, StoreInput{Store: first, Project: &config})
	advanceTicks(previous, sim.TicksPerSecond)
	previous.Close()
	if err := previous.SaveState(t.Context(), SaveFinal); err != nil {
		t.Fatal(err)
	}
	restoredOnce := decodeTestState(t, first.writeList()[1])
	restoredOnce.RestoreAttempts = 1
	startData := encodeTestState(t, restoredOnce)
	tests := []struct {
		name string
		// prepare changes the live session before the save of the earlier
		// state. It can be nil.
		prepare func(*testing.T, *testClient)
		// command changes the live session after that save, and returns the
		// command to check.
		command func(*testing.T, *testClient) Command
	}{
		{"project apply", nil, func(t *testing.T, client *testClient) Command {
			t.Helper()
			client.mustApply(t, Command{Action: "pause", Paused: true})
			current := client.session.Project()
			changed := current.Project
			changed.Demand = DemandConfig{Enabled: true, PerMinute: 30, Pattern: "balanced", Seed: 9}
			return Command{Action: "project", ProjectRevision: current.Revision, Project: &changed}
		}},
		{"rewind that restores a project", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "checkpoint"})
			changeTestDemand(t, client)
			advanceTicks(client.session, 10*sim.TicksPerSecond)
		}, func(_ *testing.T, client *testClient) Command {
			return Command{Action: "rewind", Checkpoint: client.session.State().Checkpoints[0].ID}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, file := &fakeStore{data: startData}, &projectFile{}
			live := startFromStore(t, StoreInput{Store: store, Project: &config, Options: []Option{WithProjectSaver(file.save)}})
			if restore := live.State().Restore; restore.Tier != "logical" || restore.Reason != reasonRestoreLoop {
				t.Fatalf("live restore = %+v, want the logical tier with reason %s", restore, reasonRestoreLoop)
			}
			client := newTestClient(live, "live")
			client.mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: "market"})
			advanceTicks(live, 10*sim.TicksPerSecond)
			if test.prepare != nil {
				test.prepare(t, client)
			}
			if err := live.SaveState(t.Context(), SavePeriodic); err != nil {
				t.Fatal(err)
			}
			earlier := store.lastWrite(t)
			reply := client.mustApply(t, test.command(t, client))
			after := live.State()
			written := file.saved[len(file.saved)-1]
			if written.Demand == earlier.Project.Demand {
				t.Fatal("the command did not change the demand settings of the project file")
			}
			if after.Simulation.Tick == earlier.Simulation.Tick {
				t.Fatalf("the command kept tick %d", after.Simulation.Tick)
			}

			writes := store.writeList()
			crash := startFromStore(t, StoreInput{Store: &fakeStore{data: writes[len(writes)-1]}, Project: &written})
			state := crash.State()
			if state.Restore.Tier != "physical" {
				t.Fatalf("restore = %+v, want the physical tier", state.Restore)
			}
			if same, err := sameProject(crash.project, written); err != nil || !same {
				t.Fatalf("restored project has demand %+v, want %+v", crash.project.Demand, written.Demand)
			}
			// The saved project is the project file, so the restore does not
			// apply demand settings, and the project revision stays.
			if state.ProjectRevision != reply.ProjectRevision || state.Generation != reply.Generation+1 ||
				state.Simulation.Tick != after.Simulation.Tick || state.Simulation.Submitted != after.Simulation.Submitted {
				t.Fatalf("restored project revision %d, generation %d, tick %d and %d orders, want %d, %d, %d and %d",
					state.ProjectRevision, state.Generation, state.Simulation.Tick, state.Simulation.Submitted,
					reply.ProjectRevision, reply.Generation+1, after.Simulation.Tick, after.Simulation.Submitted)
			}
		})
	}
}

func TestRestoreInfoRules(t *testing.T) {
	t.Parallel()
	restored := RestoreInfo{Tier: "logical", Reason: reasonPhysicalFailed, Requeued: 1}
	tests := []struct {
		name   string
		change func(*testing.T, *testClient)
		want   RestoreInfo
	}{
		{"reset", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "reset"})
		}, RestoreInfo{}},
		{"demo", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "demo"})
		}, RestoreInfo{}},
		{"project apply", applyTestProject, RestoreInfo{}},
		{"demand change", changeTestDemand, restored},
		{"rewind", func(t *testing.T, client *testClient) {
			t.Helper()
			id := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
			advanceTicks(client.session, 20)
			client.mustApply(t, Command{Action: "rewind", Checkpoint: id})
		}, restored},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := newTestSession(t)
			s.restore = restored
			test.change(t, newTestClient(s, "test"))
			if got := s.State().Restore; got != test.want {
				t.Fatalf("restore = %+v, want %+v", got, test.want)
			}
		})
	}
}

type persistRule int

const (
	// persistSave marks state that the state file saves in a member with
	// the same name.
	persistSave persistRule = iota + 1
	// persistDerive marks state that a restore sets from the saved members.
	persistDerive
	// persistReset marks state that starts empty after a restore.
	persistReset
	// persistInfrastructure marks locks, shutdown, I/O, logs, the server
	// build, and the state saves. The options and NewFromStore set them.
	persistInfrastructure
)

// sessionPersistRules gives a rule for each Session field.
var sessionPersistRules = map[string]persistRule{
	"simulation": persistSave, "project": persistSave, "demand": persistSave,
	"epoch": persistSave, "revision": persistSave, "projectRevision": persistSave,
	"generation": persistSave, "speed": persistSave, "lastCheckpoint": persistSave,
	// projectOrigin is projectRevision after a restore. restore tells how
	// the restore went. restoredSequences comes from the sequences member.
	"projectOrigin": persistDerive, "restore": persistDerive, "restoredSequences": persistDerive,
	// A receipt can hold a large project, so the state file keeps only its
	// sequence. Save points stay in memory only.
	"receipts": persistReset, "checkpoints": persistReset,
	"closed": persistInfrastructure, "mu": persistInfrastructure,
	"saveProject": persistInfrastructure, "logger": persistInfrastructure,
	"build": persistInfrastructure, "persist": persistInfrastructure,
}

func TestSessionFieldsHavePersistRules(t *testing.T) {
	t.Parallel()
	session, file := reflect.TypeFor[Session](), reflect.TypeFor[stateFile]()
	for field := range session.Fields() {
		rule, ok := sessionPersistRules[field.Name]
		if !ok {
			t.Errorf("Session.%s has no persist rule", field.Name)
			continue
		}
		if rule != persistSave {
			continue
		}
		if member := strings.ToUpper(field.Name[:1]) + field.Name[1:]; !hasField(file, member) {
			t.Errorf("a state file saves Session.%s, but stateFile has no %s field", field.Name, member)
		}
	}
	for name := range sessionPersistRules {
		if !hasField(session, name) {
			t.Errorf("the persist rule for Session.%s names a missing field", name)
		}
	}
}

// hasField reports whether the struct type typ has a field with name.
func hasField(typ reflect.Type, name string) bool {
	_, ok := typ.FieldByName(name)
	return ok
}

// TestLondonStateSave saves and restores a London session with demand. It
// reports the time that SaveState holds the session lock. It does not check
// the time, because the time depends on the machine.
func TestLondonStateSave(t *testing.T) {
	t.Parallel()
	config := scenarios.London()
	config.Demand.Enabled = true
	config.Demand.PerMinute = 20
	config.Redistribution = true
	store, handler := &fakeStore{}, &recordHandler{}
	s := startFromStore(t, StoreInput{Store: store, Project: &config, Options: []Option{WithLogger(slog.New(handler))}})
	client := newTestClient(s, "london")
	client.mustApply(t, Command{Action: "speed", Speed: 8})
	advanceTicks(s, 60*sim.TicksPerSecond/8)
	handler.reset()
	if err := s.SaveState(t.Context(), SavePeriodic); err != nil {
		t.Fatal(err)
	}
	records := handler.list()
	if len(records) != 1 {
		t.Fatalf("the save logged %d records, want 1", len(records))
	}
	attrs := records[0].attrs
	t.Logf("London save at tick %v: %v bytes, lock %v, encode %v, write %v",
		attrs["tick"], attrs["bytes"], attrs["lock"], attrs["encode"], attrs["write"])
	started := time.Now()
	restored := startFromStore(t, StoreInput{Store: &fakeStore{data: store.writeList()[1]}, Project: &config})
	if got := restored.State().Restore; got.Tier != "physical" {
		t.Fatalf("London restore = %+v, want the physical tier", got)
	}
	t.Logf("London restore: %v, %d pods demoted", time.Since(started), restored.State().Restore.Demoted)
}
