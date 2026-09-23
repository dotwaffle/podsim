package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/statestore"
)

// fileURL returns the file URL of an absolute directory.
func fileURL(directory string) string {
	return (&url.URL{Scheme: "file", Path: directory}).String()
}

// logBuffer collects JSON log records. Several goroutines can log to it.
type logBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *logBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

// records returns the records with each value as text.
func (b *logBuffer) records(t *testing.T) []map[string]string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var records []map[string]string
	for line := range bytes.Lines(b.buffer.Bytes()) {
		var decoded map[string]any
		if err := json.Unmarshal(line, &decoded); err != nil {
			t.Fatalf("decode log record %q: %v", line, err)
		}
		record := make(map[string]string, len(decoded))
		for key, value := range decoded {
			record[key] = fmt.Sprint(value)
		}
		records = append(records, record)
	}
	return records
}

// messages returns the message of each record at level or above.
func (b *logBuffer) messages(t *testing.T, level slog.Level) []string {
	t.Helper()
	var messages []string
	for _, record := range b.records(t) {
		var recordLevel slog.Level
		if err := recordLevel.UnmarshalText([]byte(record["level"])); err != nil {
			t.Fatalf("log record %v: %v", record, err)
		}
		if recordLevel >= level {
			messages = append(messages, record["msg"])
		}
	}
	return messages
}

// find returns the first record with the message msg.
func (b *logBuffer) find(t *testing.T, msg string) map[string]string {
	t.Helper()
	for _, record := range b.records(t) {
		if record["msg"] == msg {
			return record
		}
	}
	t.Fatalf("no log record %q in %v", msg, b.records(t))
	return nil
}

func TestOpenSession(t *testing.T) {
	t.Parallel()
	changed := project.Default()
	changed.Demand = project.DemandConfig{Enabled: true, PerMinute: 7, Pattern: "balanced", Seed: 3}
	renamed := changed
	renamed.Name = "Other project"
	invalid := project.Default()
	invalid.Fleet = nil
	defaultRate := project.Default().Demand.PerMinute
	tests := []struct {
		name string
		// saved is the project of a saved state that the test makes first, or nil.
		saved      *project.Config
		config     project.Config
		projectSet bool
		noState    bool
		// temporary is a temporary file of an earlier write, or empty.
		temporary     string
		wantErr       string
		wantRestore   session.RestoreInfo
		wantPerMinute int
		wantLog       string
	}{
		{name: "no state option", config: changed, projectSet: true, noState: true, wantPerMinute: 7},
		{name: "no state option and a project that is not valid", config: invalid, noState: true, wantErr: "fleet"},
		{name: "no saved state", config: project.Default(), wantPerMinute: defaultRate, wantLog: "Opened session state store"},
		{
			name: "temporary file", config: project.Default(), temporary: statestore.StateKey + ".1a2b.tmp",
			wantPerMinute: defaultRate, wantLog: "Removed temporary state files",
		},
		{
			name: "saved project without -project", saved: &changed, config: project.Default(),
			wantRestore: session.RestoreInfo{Tier: "physical"}, wantPerMinute: 7,
		},
		{
			name: "other -project", saved: &changed, config: renamed, projectSet: true,
			wantRestore: session.RestoreInfo{Tier: "empty", Reason: "project_changed"}, wantPerMinute: 7,
		},
		// A crash after a demand command can leave newer demand settings in
		// the project file.
		{
			name: "-project with other demand settings", saved: &changed, config: project.Default(), projectSet: true,
			wantRestore: session.RestoreInfo{Tier: "physical"}, wantPerMinute: defaultRate,
			wantLog: "Applied demand settings of the project file",
		},
		{
			name: "same -project", saved: &changed, config: changed, projectSet: true,
			wantRestore: session.RestoreInfo{Tier: "physical"}, wantPerMinute: 7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			input := openInput{
				config: test.config, projectSet: test.projectSet, stateURL: fileURL(directory),
				logger: slog.New(slog.DiscardHandler),
			}
			if test.noState {
				input.stateURL = ""
			}
			if test.saved != nil {
				writeFinalState(t, openInput{config: *test.saved, projectSet: true, stateURL: input.stateURL, logger: input.logger})
			}
			if test.temporary != "" {
				if err := os.WriteFile(filepath.Join(directory, test.temporary), []byte("partial"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var logs logBuffer
			input.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			input.options = []session.Option{session.WithLogger(input.logger)}
			shared, closeStore, err := openSession(t.Context(), input)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("openSession error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := closeStore(); closeErr != nil {
					t.Error(closeErr)
				}
			}()
			state := shared.State()
			if state.Restore != test.wantRestore || state.Demand.Config.PerMinute != test.wantPerMinute {
				t.Fatalf("restore %+v and %d orders per minute, want %+v and %d",
					state.Restore, state.Demand.Config.PerMinute, test.wantRestore, test.wantPerMinute)
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			if test.noState {
				if !errors.Is(shared.SaveState(t.Context(), session.SavePeriodic), session.ErrStateSavingOff) || len(entries) != 0 {
					t.Fatalf("session without a store saves, or the directory has %v", entries)
				}
				return
			}
			if test.wantLog != "" {
				logs.find(t, test.wantLog)
			}
			if i := slices.IndexFunc(entries, func(entry os.DirEntry) bool {
				return strings.HasSuffix(entry.Name(), ".tmp")
			}); i >= 0 {
				t.Fatalf("temporary file %s stays", entries[i].Name())
			}
			assertStateFileMode(t, directory)
		})
	}
}

// writeFinalState starts a session through openSession, closes it and saves
// its final state. It returns the epoch of the session.
func writeFinalState(t *testing.T, input openInput) string {
	t.Helper()
	shared, closeStore, err := openSession(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	shared.Close()
	if err := shared.SaveState(t.Context(), session.SaveFinal); err != nil {
		t.Fatal(err)
	}
	if err := closeStore(); err != nil {
		t.Fatal(err)
	}
	return shared.State().Epoch
}

// assertStateFileMode checks that the saved state in directory has mode 0600.
func assertStateFileMode(t *testing.T, directory string) {
	t.Helper()
	info, err := os.Stat(filepath.Join(directory, statestore.StateKey))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o, want 600", info.Mode().Perm())
	}
}

// finalStore is a state store without a saved state. Its first write, the
// startup save, succeeds. Each later write is a final save, and finalWrite
// gives its result.
type finalStore struct {
	readErr    error
	finalWrite func(ctx context.Context) error

	mu     sync.Mutex
	writes int
	// finalCtxErr is the error of the context at the start of the last
	// final save.
	finalCtxErr error
}

func (f *finalStore) Read(context.Context) ([]byte, error) {
	if f.readErr != nil {
		return nil, f.readErr
	}
	return nil, fmt.Errorf("read test state: %w", fs.ErrNotExist)
}

func (f *finalStore) Write(ctx context.Context, _ []byte) error {
	f.mu.Lock()
	f.writes++
	startup := f.writes == 1
	if !startup {
		f.finalCtxErr = ctx.Err()
	}
	f.mu.Unlock()
	if startup || f.finalWrite == nil {
		return nil
	}
	return f.finalWrite(ctx)
}

func (*finalStore) Reject(context.Context) error { return nil }

func (*finalStore) Backup(context.Context) error { return nil }

func TestStopStateSaving(t *testing.T) {
	t.Parallel()
	timeout := errFinalSaveTimeout.Error()
	tests := []struct {
		name         string
		readErr      error
		clockRunning bool
		saverStuck   bool
		// finalWrite gives the result of the final save. release closes
		// at the end of the test.
		finalWrite  func(ctx context.Context, release <-chan struct{}) error
		wantWrites  int
		wantElapsed time.Duration
		// wantWarnings holds the messages of the records at level WARN and
		// above. wantReason is the reason of a skipped save, and wantCause
		// is the cause of a failed save.
		wantWarnings []string
		wantReason   string
		wantCause    string
	}{
		{name: "saved", wantWrites: 2},
		{
			name: "clock running", clockRunning: true, wantWrites: 1,
			wantWarnings: []string{"Skipped final save"}, wantReason: "clock",
		},
		{
			name: "saver stuck", saverStuck: true, wantWrites: 1, wantElapsed: saverStopTimeout,
			wantWarnings: []string{"State saver did not stop", "Skipped final save"}, wantReason: "saver",
		},
		{
			name: "clock running and saver stuck", clockRunning: true, saverStuck: true, wantWrites: 1,
			wantElapsed:  saverStopTimeout,
			wantWarnings: []string{"State saver did not stop", "Skipped final save"}, wantReason: "clock",
		},
		{
			name: "write stops at the timeout",
			finalWrite: func(ctx context.Context, _ <-chan struct{}) error {
				<-ctx.Done()
				return ctx.Err()
			},
			wantWrites: 2, wantElapsed: finalSaveTimeout,
			wantWarnings: []string{"Save session state", "Final save failed"}, wantCause: timeout,
		},
		{
			name: "write blocked",
			finalWrite: func(_ context.Context, release <-chan struct{}) error {
				<-release
				return nil
			},
			wantWrites: 2, wantElapsed: finalSaveWait,
			wantWarnings: []string{"Final save failed"}, wantCause: timeout,
		},
		{
			name: "saving off", readErr: errors.New("disk fault"), wantWrites: 0,
			wantWarnings: []string{"Read saved session state", "Skipped final save"}, wantReason: "off",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				defer close(release)
				store := &finalStore{readErr: test.readErr}
				if test.finalWrite != nil {
					store.finalWrite = func(ctx context.Context) error { return test.finalWrite(ctx, release) }
				}
				var logs logBuffer
				logger := slog.New(slog.NewJSONHandler(&logs, nil))
				shared, err := session.NewFromStore(t.Context(), session.StoreInput{
					Store: store, Options: []session.Option{session.WithLogger(logger)},
				})
				if err != nil {
					t.Fatal(err)
				}
				saverCtx, stopSaver := context.WithCancel(t.Context())
				defer stopSaver()
				var saver sync.WaitGroup
				saver.Go(func() {
					if test.saverStuck {
						<-release
						return
					}
					shared.RunStateSaver(saverCtx, stateSaveInterval)
				})
				shared.Close()
				// run passes the signal context, which is done at shutdown.
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				started := time.Now()
				stopStateSaving(ctx, stopInput{
					session: shared, clockStopped: !test.clockRunning, saver: &saver, stopSaver: stopSaver, logger: logger,
				})
				if elapsed := time.Since(started); elapsed != test.wantElapsed {
					t.Errorf("stopStateSaving took %v, want %v", elapsed, test.wantElapsed)
				}
				store.mu.Lock()
				writes, finalCtxErr := store.writes, store.finalCtxErr
				store.mu.Unlock()
				if writes != test.wantWrites || finalCtxErr != nil {
					t.Errorf("writes = %d with final context error %v, want %d and nil", writes, finalCtxErr, test.wantWrites)
				}
				if got := logs.messages(t, slog.LevelWarn); !slices.Equal(got, test.wantWarnings) {
					t.Errorf("warnings = %q, want %q", got, test.wantWarnings)
				}
				switch {
				case test.wantWrites == 2 && test.wantWarnings == nil:
					logs.find(t, "Saved final session state")
				case test.wantReason != "":
					if got := logs.find(t, "Skipped final save")["reason"]; got != test.wantReason {
						t.Errorf("reason = %q, want %q", got, test.wantReason)
					}
				case test.wantCause != "":
					for _, message := range test.wantWarnings {
						if record := logs.find(t, message); record["cause"] != test.wantCause {
							t.Errorf("%s cause = %q, want %q", message, record["cause"], test.wantCause)
						}
					}
				}
			})
		})
	}
}

func TestRunFailsWithUnusableState(t *testing.T) {
	t.Parallel()
	directory := browserDirectory(t)
	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
	for _, test := range []struct {
		name     string
		stateURL string
		root     bool // The case needs a user without root rights.
		wantErr  error
		wantText string
	}{
		{name: "relative file URL", stateURL: "file://podsim-state", wantText: "open state store file://podsim-state"},
		{name: "unknown scheme", stateURL: "s3://bucket", wantText: `no driver registered for scheme "s3"`},
		{
			name: "read-only directory", stateURL: fileURL(readOnly), root: true,
			wantErr: fs.ErrPermission, wantText: "write startup session state",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.root && os.Geteuid() == 0 {
				t.Skip("root can write to a directory with mode 0555")
			}
			// If run starts to serve, ready stops it, so that the test fails
			// at once.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var readyCalls int
			args := []string{"-addr", "127.0.0.1:0", "-dir", directory, "-state", test.stateURL}
			err := run(ctx, runInput{args: args, ready: func(string) {
				readyCalls++
				cancel()
			}})
			if err == nil || !strings.Contains(err.Error(), test.wantText) || (test.wantErr != nil && !errors.Is(err, test.wantErr)) {
				t.Fatalf("run error = %v, want %q and %v", err, test.wantText, test.wantErr)
			}
			if readyCalls != 0 {
				t.Fatalf("ready calls = %d, want 0", readyCalls)
			}
		})
	}
}

// TestRunFailedStartKeepsSavedState checks that a start that fails after
// the restore does not count as a restore. Without the final save of the
// failed start, two failed starts give reason restore_loop.
func TestRunFailedStartKeepsSavedState(t *testing.T) {
	t.Parallel()
	occupied := loopbackListener(t).Addr().String()
	directory := browserDirectory(t)
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "application address in use", args: []string{"-addr", occupied}},
		{name: "pprof address in use", args: []string{"-addr", "127.0.0.1:0", "-pprof-addr", occupied}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stateDirectory := t.TempDir()
			input := openInput{config: project.Default(), stateURL: fileURL(stateDirectory), logger: slog.New(slog.DiscardHandler)}
			epoch := writeFinalState(t, input)
			args := slices.Concat(test.args, []string{"-dir", directory, "-state", input.stateURL})
			for range 2 {
				// If run starts to serve, ready stops it, so that the test
				// fails at once.
				ctx, cancel := context.WithCancel(t.Context())
				err := run(ctx, runInput{args: args, ready: func(string) { cancel() }})
				cancel()
				if !errors.Is(err, syscall.EADDRINUSE) {
					t.Fatalf("run error = %v, want %v", err, syscall.EADDRINUSE)
				}
			}
			shared, closeStore, err := openSession(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}
			if err := closeStore(); err != nil {
				t.Fatal(err)
			}
			state := shared.State()
			if state.Restore != (session.RestoreInfo{Tier: "physical"}) || state.Epoch != epoch {
				t.Fatalf("after two failed starts: restore %+v and epoch %s, want the physical tier and epoch %s",
					state.Restore, state.Epoch, epoch)
			}
			assertStateFileMode(t, stateDirectory)
		})
	}
}

// savedHeader holds the members of a saved state that the saver test checks.
type savedHeader struct {
	Final           bool   `json:"final"`
	ProjectRevision uint64 `json:"projectRevision"`
}

// readSavedHeader reads the saved state in directory.
func readSavedHeader(t *testing.T, directory string) savedHeader {
	t.Helper()
	compressed, err := os.ReadFile(filepath.Join(directory, statestore.StateKey))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var header savedHeader
	if err := json.Unmarshal(data, &header); err != nil {
		t.Fatal(err)
	}
	return header
}

// TestRunSavesStateWhileServing checks that run starts the state saver. A
// demand change makes the saver save about 1 s later, before the final save.
func TestRunSavesStateWhileServing(t *testing.T) {
	if testing.Short() {
		t.Skip("the saver waits 1 s after a project change")
	}
	t.Parallel()
	stateDirectory := t.TempDir()
	args := []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t), "-state", fileURL(stateDirectory)}
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()

	address, stop := startRun(t, args)
	defer stop()
	frame := getFrame(t, client, address)
	demand := project.Default().Demand
	demand.PerMinute++
	reply := postCommand(t, client, address, session.Command{
		Client: "saver-test", Sequence: 1, Epoch: frame.Epoch, Action: "demand", Demand: demand,
	})
	if reply.Error != "" {
		t.Fatalf("demand reply = %+v, want no error", reply)
	}
	timeout := time.After(10 * time.Second)
	for {
		header := readSavedHeader(t, stateDirectory)
		if !header.Final && header.ProjectRevision == reply.ProjectRevision {
			return
		}
		select {
		case <-timeout:
			t.Fatalf("saved state %+v, want a periodic save with project revision %d", header, reply.ProjectRevision)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// restartFrame holds the members of a state frame that the restart test
// checks.
type restartFrame struct {
	Epoch      string `json:"epoch"`
	Simulation struct {
		Submitted int `json:"Submitted"`
	} `json:"simulation"`
	Restore session.RestoreInfo `json:"restore"`
}

// TestRunRestoresSavedSession starts the server with -state, orders a trip,
// and stops the server. Then it starts the server again and checks that the
// session continues.
func TestRunRestoresSavedSession(t *testing.T) {
	if testing.Short() {
		t.Skip("the restart test starts the server twice")
	}
	t.Parallel()
	stateDirectory := t.TempDir()
	args := []string{"-addr", "127.0.0.1:0", "-dir", browserDirectory(t), "-state", fileURL(stateDirectory)}
	client := &http.Client{Transport: &http.Transport{}}
	defer client.CloseIdleConnections()

	address, stop := startRun(t, args)
	first := getFrame(t, client, address)
	if first.Restore != (session.RestoreInfo{}) {
		t.Fatalf("first start restore = %+v, want none", first.Restore)
	}
	reply := postCommand(t, client, address, session.Command{
		Client: "restart-test", Sequence: 1, Epoch: first.Epoch, Action: "trip", Origin: "harbor", Destination: "market",
	})
	if reply.Error != "" || reply.OrderID != 1 {
		t.Fatalf("trip reply = %+v, want order 1", reply)
	}
	before := getFrame(t, client, address)
	stop()
	client.CloseIdleConnections()
	assertStateFileMode(t, stateDirectory)

	address, stop = startRun(t, args)
	after := getFrame(t, client, address)
	stop()
	if after.Epoch != first.Epoch || after.Restore.Tier != "physical" || after.Simulation.Submitted != before.Simulation.Submitted {
		t.Fatalf("after the restart: epoch %s, restore %+v, %d orders; want epoch %s, the physical tier, %d orders",
			after.Epoch, after.Restore, after.Simulation.Submitted, first.Epoch, before.Simulation.Submitted)
	}
	assertStateFileMode(t, stateDirectory)
}

// startRun starts run with args and waits until it serves. It returns the
// application address and a function that stops run and checks that run
// returned nil.
func startRun(t *testing.T, args []string) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	addresses := make(chan string, 1)
	returned := make(chan error, 1)
	go func() { returned <- run(ctx, runInput{args: args, ready: func(addr string) { addresses <- addr }}) }()
	var address string
	select {
	case address = <-addresses:
	case err := <-returned:
		cancel()
		t.Fatalf("run returned before ready: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("run did not call ready")
	}
	stop := func() {
		t.Helper()
		cancel()
		select {
		case err := <-returned:
			if err != nil {
				t.Fatalf("run after cancellation = %v, want nil", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("run did not return after cancellation")
		}
	}
	return address, stop
}

// getFrame reads the state frame from the server at address.
func getFrame(t *testing.T, client *http.Client, address string) restartFrame {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/api/state", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	var frame restartFrame
	doJSON(t, client, request, &frame)
	return frame
}

// postCommand sends command to the server at address and returns the reply.
func postCommand(t *testing.T, client *http.Client, address string, command session.Command) session.Reply {
	t.Helper()
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+address+"/api/command", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	var reply session.Reply
	doJSON(t, client, request, &reply)
	return reply
}

// doJSON sends request and decodes the JSON body of a 200 response into value.
func doJSON(t *testing.T, client *http.Client, request *http.Request, value any) {
	t.Helper()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s %s = %d %s", request.Method, request.URL.Path, response.StatusCode, body)
	}
	if err := json.Unmarshal(body, value); err != nil {
		t.Fatalf("decode %s: %v", request.URL.Path, err)
	}
}
