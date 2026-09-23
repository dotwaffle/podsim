package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/statestore"
)

const (
	// stateSaveInterval is the time between two periodic saves.
	stateSaveInterval = time.Minute
	// saverStopTimeout limits the wait for the saver at shutdown.
	saverStopTimeout = time.Second
	// finalSaveTimeout limits the final save. finalSaveWait is 1 s longer,
	// so that a store that stops at the timeout can return its error.
	finalSaveTimeout = 5 * time.Second
	finalSaveWait    = finalSaveTimeout + time.Second
)

// errFinalSaveTimeout is the cause when the final save takes too long.
var errFinalSaveTimeout = errors.New("final save of the session state timed out")

// openInput holds the settings of the shared session. config is the
// project to use. projectSet is true when config comes from -project, which
// validated it. stateURL is the bucket URL from -state, or empty. logger
// gets the records about the store.
type openInput struct {
	config     project.Config
	projectSet bool
	stateURL   string
	logger     *slog.Logger
	options    []session.Option
}

// openSession starts the shared session. Without a state URL, it starts a
// new session with input.config and returns a close function that does
// nothing. With a state URL, it opens the store, removes the temporary
// files of interrupted writes, and restores the saved session through
// session.NewFromStore. Without -project, the session can restore the
// saved project. The close function then closes the store. Call it after
// the final save. When openSession returns an error, it has closed the
// store.
func openSession(ctx context.Context, input openInput) (*session.Session, func() error, error) {
	if input.stateURL == "" {
		shared, err := session.NewWithProject(input.config, input.options...)
		if err != nil {
			return nil, nil, err
		}
		return shared, func() error { return nil }, nil
	}
	store, err := statestore.Open(ctx, input.stateURL)
	if err != nil {
		return nil, nil, err
	}
	input.logger.Info("Opened session state store",
		slog.String("url", statestore.Redact(input.stateURL)), slog.String("location", store.Location()))
	removed, err := store.RemoveTemporaries(ctx)
	switch {
	case err != nil:
		input.logger.Warn("Remove temporary state files", slog.Any("error", err))
	case removed > 0:
		input.logger.Info("Removed temporary state files", slog.Int("count", removed))
	}
	storeInput := session.StoreInput{Store: store, Options: input.options}
	if input.projectSet {
		storeInput.Project = new(input.config)
	}
	shared, err := session.NewFromStore(ctx, storeInput)
	if err != nil {
		return nil, nil, errors.Join(err, store.Close())
	}
	return shared, store.Close, nil
}

// stopInput holds the session and its saver for stopStateSaving.
// clockStopped is true when the clock goroutine returned. stopSaver cancels
// the context of the saver, and saver waits for it.
type stopInput struct {
	session      *session.Session
	clockStopped bool
	saver        *sync.WaitGroup
	stopSaver    context.CancelFunc
	logger       *slog.Logger
}

// stopStateSaving stops the saver and waits up to 1 s for it. Then it calls
// saveFinal, but only when the clock and the saver stopped. Otherwise the
// state can still change, or an earlier write can still use the store.
// stopStateSaving logs a skipped save.
func stopStateSaving(ctx context.Context, input stopInput) {
	input.stopSaver()
	saverStopped := waitFor(waitInput{
		group: input.saver, name: "State saver", timeout: saverStopTimeout, logger: input.logger,
	})
	switch {
	case !input.clockStopped:
		input.logger.Warn("Skipped final save", slog.String("reason", "clock"))
	case !saverStopped:
		input.logger.Warn("Skipped final save", slog.String("reason", "saver"))
	default:
		saveFinal(ctx, input.session, input.logger)
	}
}

// saveFinal saves the state of the closed session shared a last time. The
// save ignores the cancellation of ctx and gets 5 s. saveFinal waits at most
// 6 s for it, because a store can block in a system call that the timeout
// cannot stop. SaveState logs the result of each write. saveFinal logs a
// save that failed or did not return, and a session that does not save.
func saveFinal(ctx context.Context, shared *session.Session, logger *slog.Logger) {
	finalCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), finalSaveTimeout, errFinalSaveTimeout)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- shared.SaveState(finalCtx, session.SaveFinal) }()
	var err error
	select {
	case err = <-done:
	case <-time.After(finalSaveWait):
		err = fmt.Errorf("the final save did not return in %s", finalSaveWait)
	}
	switch {
	case err == nil:
	case errors.Is(err, session.ErrStateSavingOff):
		logger.Warn("Skipped final save", slog.String("reason", "off"))
	default:
		logger.Warn("Final save failed", slog.Any("error", err), slog.Any("cause", context.Cause(finalCtx)))
	}
}
