package session

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// commandSaves is the number of command saves that each admission test
// sends while the store blocks.
const commandSaves = 32

// saveTestLabel is the pprof label key that marks the goroutines of a test.
const saveTestLabel = "podsim-save-test"

// labelGoroutines gives the current goroutine the pprof label of t. The
// goroutines that it starts after this call get the same label.
func labelGoroutines(t *testing.T) {
	t.Helper()
	pprof.SetGoroutineLabels(pprof.WithLabels(t.Context(), pprof.Labels(saveTestLabel, t.Name())))
}

// storeGoroutines returns the number of goroutines with the label of t that
// callStore started. Other tests run in parallel, so the test cannot
// use runtime.NumGoroutine.
func storeGoroutines(t *testing.T) int {
	t.Helper()
	var profile bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&profile, 1); err != nil {
		t.Fatal(err)
	}
	label := fmt.Sprintf("%q:%q", saveTestLabel, t.Name())
	count := 0
	for record := range strings.SplitSeq(profile.String(), "\n\n") {
		if !strings.Contains(record, label) || !strings.Contains(record, "session.callStore[...].func1") {
			continue
		}
		// The record starts with the goroutine count of its stack, after
		// the header line of the first record.
		for line := range strings.SplitSeq(record, "\n") {
			if text, _, found := strings.Cut(line, " @ "); found {
				n, err := strconv.Atoi(text)
				if err != nil {
					t.Fatalf("goroutine profile line %q: %v", line, err)
				}
				count += n
				break
			}
		}
	}
	return count
}

// testRevision returns the revision of the session.
func (s *Session) testRevision() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision
}

// blockedSaveRun is a session with a store that blocks each write until the
// test calls unblock. The first command save holds the save slot. copies
// counts the copies of the state that have something to write, because a
// save reads the clock after such a copy.
type blockedSaveRun struct {
	session *Session
	store   *fakeStore
	unblock func()
	copies  *atomic.Int64
	replies chan *bool
}

// newBlockedSaveRun starts the run in the synctest bubble of t. It returns
// when the first command save waits in its write.
func newBlockedSaveRun(t *testing.T) blockedSaveRun {
	t.Helper()
	labelGoroutines(t)
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
	release := make(chan struct{})
	run := blockedSaveRun{
		session: s, store: store, unblock: sync.OnceFunc(func() { close(release) }), copies: new(atomic.Int64),
		replies: make(chan *bool, commandSaves+1),
	}
	// A failed test must not leave blocked goroutines in the bubble.
	t.Cleanup(run.unblock)
	s.persist.now = func() time.Time { run.copies.Add(1); return time.Now() }
	store.blockWrite = release
	run.command()
	synctest.Wait()
	if copies := run.copies.Load(); copies != 1 || len(s.persist.slot) != 1 {
		t.Fatalf("the first command save made %d copies and the slot has %d values, want 1 and 1", copies, len(s.persist.slot))
	}
	return run
}

// command changes the state and starts its command save.
func (run blockedSaveRun) command() {
	run.session.advance()
	revision := run.session.testRevision()
	go func() { run.replies <- run.session.saveBeforeReply(revision) }()
}

// checkReplies reads count replies and checks that each has stateSaved want.
func (run blockedSaveRun) checkReplies(t *testing.T, count int, want bool) {
	t.Helper()
	for range count {
		select {
		case reply := <-run.replies:
			if reply == nil || *reply != want {
				t.Fatalf("a command save replied stateSaved %s, want %t", savedText(reply), want)
			}
		default:
			t.Fatalf("a command save did not reply")
		}
	}
}

// checkLastWrite checks that the store has writes states and that the last
// one has the revision of the session.
func (run blockedSaveRun) checkLastWrite(t *testing.T, writes int) {
	t.Helper()
	if got := len(run.store.writeList()); got != writes {
		t.Fatalf("the store has %d writes, want %d", got, writes)
	}
	if last, revision := run.store.lastWrite(t).Revision, run.session.testRevision(); last != revision {
		t.Fatalf("the last write has revision %d, want the latest revision %d", last, revision)
	}
}

// TestCommandSavesBehindBlockedStore sends command saves while a write
// blocks until their time ends. The saves that wait must stop at the end of
// their time. They must not stay as goroutines, and they must not copy the
// state after the store comes back.
func TestCommandSavesBehindBlockedStore(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		run := newBlockedSaveRun(t)
		for range commandSaves {
			run.command()
		}
		synctest.Wait()
		// The goroutines are the blocked write and the one queued save.
		if got := storeGoroutines(t); got != 2 {
			t.Fatalf("%d store goroutines wait, want 2", got)
		}
		time.Sleep(commandSaveTimeout)
		synctest.Wait()
		run.checkReplies(t, commandSaves+1, false)
		if got := storeGoroutines(t); got != 1 {
			t.Fatalf("%d store goroutines stay after the timeout, want only the blocked write", got)
		}
		run.unblock()
		synctest.Wait()
		if copies := run.copies.Load(); copies != 1 {
			t.Fatalf("saves made %d copies of the state, want only the copy of the first save", copies)
		}
		if got := len(run.store.writeList()); got != 2 {
			t.Fatalf("the store has %d writes, want the startup save and the first command save", got)
		}
		// The next save writes the latest state.
		if err := run.session.SaveState(t.Context(), SavePeriodic); err != nil {
			t.Fatal(err)
		}
		run.checkLastWrite(t, 3)
	})
}

// TestCommandSavesJoinQueuedSave sends command saves while a write blocks,
// and lets the write end before their time ends. The waiting saves share
// one queued save, which writes the latest state once.
func TestCommandSavesJoinQueuedSave(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		run := newBlockedSaveRun(t)
		for range commandSaves {
			run.command()
		}
		synctest.Wait()
		if got := storeGoroutines(t); got != 2 {
			t.Fatalf("%d store goroutines wait, want the blocked write and one queued save", got)
		}
		run.unblock()
		synctest.Wait()
		run.checkReplies(t, commandSaves+1, true)
		if copies := run.copies.Load(); copies != 2 {
			t.Fatalf("saves made %d copies of the state, want 2", copies)
		}
		run.checkLastWrite(t, 3)
	})
}

// TestCommandSaveRetriesEndedQueuedSave checks a command save that joins a
// queued save with an earlier end. When the queued save stops, the command
// save queues a save of its own and writes in its own time.
func TestCommandSaveRetriesEndedQueuedSave(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		run := newBlockedSaveRun(t)
		run.command()
		time.Sleep(commandSaveTimeout / 2)
		run.command()
		time.Sleep(commandSaveTimeout / 2)
		synctest.Wait()
		// The first command save and the save that it queued ended.
		run.checkReplies(t, 2, false)
		run.unblock()
		synctest.Wait()
		run.checkReplies(t, 1, true)
		run.checkLastWrite(t, 3)
	})
}

// TestSaveStateEndedContext checks that a save with an ended context does
// not copy the state, also when the save slot is free. A select picks at
// random when the slot and the context are both ready, so the test saves
// many times, and about half of them get the slot.
func TestSaveStateEndedContext(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
	var copies atomic.Int64
	s.persist.now = func() time.Time { copies.Add(1); return time.Now() }
	s.advance()
	cause := errors.New("test ended the save")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	for range 64 {
		if err := s.SaveState(ctx, SavePeriodic); !errors.Is(err, errSaveEnded) || !errors.Is(err, cause) {
			t.Fatalf("save error = %v, want %v and %v", err, errSaveEnded, cause)
		}
	}
	if got := copies.Load(); got != 0 {
		t.Fatalf("saves with an ended context made %d copies of the state", got)
	}
	if got := len(store.writeList()); got != 1 {
		t.Fatalf("the store has %d writes, want only the startup save", got)
	}
}

// TestSaveStateEndsWhileWaitingForLock ends the context of a save that has
// the save slot and waits for the session lock. The save must not copy the
// state after it gets the lock.
func TestSaveStateEndsWhileWaitingForLock(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(&recordHandler{}))}})
	var copies atomic.Int64
	s.persist.now = func() time.Time { copies.Add(1); return time.Now() }
	s.advance()
	cause := errors.New("test ended the save")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	s.mu.Lock()
	saved := make(chan error, 1)
	go func() { saved <- s.SaveState(ctx, SavePeriodic) }()
	// The save has the slot. It waits for the session lock, or it goes to
	// the lock next.
	for len(s.persist.slot) == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel(cause)
	s.mu.Unlock()
	if err := <-saved; !errors.Is(err, errSaveEnded) || !errors.Is(err, cause) {
		t.Fatalf("save error = %v, want %v and %v", err, errSaveEnded, cause)
	}
	if got := copies.Load(); got != 0 {
		t.Fatalf("the save made %d copies of the state after its context ended", got)
	}
	if got := len(store.writeList()); got != 1 {
		t.Fatalf("the store has %d writes, want only the startup save", got)
	}
}

// TestSaveStateEndsBeforeEncode ends the context of a save while it copies
// the state. The save must not encode or write the copy, and it logs a
// failed save.
func TestSaveStateEndsBeforeEncode(t *testing.T) {
	t.Parallel()
	store, handler := &fakeStore{}, &recordHandler{}
	s := startFromStore(t, StoreInput{Store: store, Options: []Option{WithLogger(slog.New(handler))}})
	handler.reset()
	s.advance()
	cause := errors.New("test ended the save")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	// A save reads the clock after the copy and before the encoding.
	s.persist.now = func() time.Time { cancel(cause); return time.Now() }
	if err := s.SaveState(ctx, SavePeriodic); !errors.Is(err, errSaveEnded) || !errors.Is(err, cause) {
		t.Fatalf("save error = %v, want %v and %v", err, errSaveEnded, cause)
	}
	if got := len(store.writeList()); got != 1 {
		t.Fatalf("the store has %d writes, want only the startup save", got)
	}
	checkRecords(t, handler, []wantRecord{{slog.LevelWarn, "Save session state", map[string]any{
		"kind": "periodic", "cause": cause, "failures": int64(1),
		"error": func(value any) bool { err, _ := value.(error); return errors.Is(err, errSaveEnded) },
	}}})
}

// TestCommandSaveJoinerStopsAtDeadline checks a command save that joins a
// queued save that gets the slot and then blocks in its write. The joined
// save must reply at the end of its own time, and not wait for the write.
func TestCommandSaveJoinerStopsAtDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		run := newBlockedSaveRun(t)
		second := make(chan struct{})
		t.Cleanup(sync.OnceFunc(func() { close(second) }))
		run.store.blockWrite = second
		run.command()
		run.command()
		synctest.Wait()
		// The first save writes and replies. The queued save gets the slot
		// and blocks in its write.
		run.unblock()
		synctest.Wait()
		run.checkReplies(t, 1, true)
		time.Sleep(commandSaveTimeout)
		synctest.Wait()
		run.checkReplies(t, 2, false)
	})
}
