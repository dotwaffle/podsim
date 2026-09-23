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
// during a read.
type fakeStore struct {
	blockRead, blockWrite chan struct{}

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

func (f *fakeStore) Write(_ context.Context, data []byte) error {
	f.record("write")
	if f.blockWrite != nil {
		<-f.blockWrite
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := startFromStore(t, StoreInput{Store: &fakeStore{data: test.data}}).State()
			if state.Restore.Tier != test.tier {
				t.Fatalf("restore tier = %q, want %q", state.Restore.Tier, test.tier)
			}
			if kept := state.Epoch == run.file.Epoch; kept != test.kept {
				t.Fatalf("epoch kept = %t, want %t", kept, test.kept)
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

func TestSaveStateRules(t *testing.T) {
	t.Parallel()
	type step func(context.Context, *Session) error
	periodic := func(ctx context.Context, s *Session) error { return s.SaveState(ctx, SavePeriodic) }
	final := func(ctx context.Context, s *Session) error { return s.SaveState(ctx, SaveFinal) }
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
	if revision, savedAt := s.persist.savedRevision.Load(), s.persist.savedAt.Load(); revision != 3 || savedAt != now.UnixNano() {
		t.Fatalf("last good save at revision %d and time %d", revision, savedAt)
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
	// the restore went.
	"projectOrigin": persistDerive, "restore": persistDerive,
	// A receipt can hold a large project. Save points stay in memory only.
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
