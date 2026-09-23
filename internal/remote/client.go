// Package remote connects a presentation client to the shared session.
package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
)

// Result reports the outcome of a submitted command.
type Result struct {
	Command session.Command
	Reply   session.Reply
	Err     error
}

// retiredEpochPolls is the number of polls in a row that must return the same
// retired epoch before the client uses that epoch again. The client polls
// every 50 ms, so this is 1 s. A server can send a retired epoch again after
// a restart that restores an older state file.
const retiredEpochPolls = 20

// Client polls state and serializes commands. All returned state is immutable.
type Client struct {
	mu        sync.Mutex
	url       string
	http      *http.Client
	state     session.State
	topology  session.TopologySnapshot
	connected bool
	// lastFrame is the time of the last poll that read a state frame
	// without an error.
	lastFrame time.Time
	pending   bool
	client    string
	sequence  uint64
	oldEpochs map[string]bool
	// retiredPolls counts the last frames in a row that carry retiredEpoch,
	// an epoch in oldEpochs.
	retiredEpoch string
	retiredPolls int
	// build is the first non-empty build ID that a state frame gave.
	// buildChanged becomes true when a later frame gives a different
	// non-empty build ID, and then it stays true.
	build        string
	buildChanged bool
	commands     chan session.Command
	results      chan Result
}

// New starts polling and command processing until ctx is canceled.
func New(ctx context.Context, serverURL string) *Client {
	c := &Client{url: strings.TrimRight(serverURL, "/"), http: &http.Client{Timeout: 3 * time.Second}, client: rand.Text(), oldEpochs: make(map[string]bool), commands: make(chan session.Command, 1), results: make(chan Result, 1)}
	go c.poll(ctx)
	go c.runCommands(ctx)
	return c
}

// View returns the latest state and whether a new command can be submitted.
func (c *Client) View() (session.State, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, c.connected, c.pending
}

// LastFrame returns the time of the last poll that read a state frame
// without an error. A game uses it to give the age of the shown state while
// the connection is lost. Before the first frame, it returns the zero time.
func (c *Client) LastFrame() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastFrame
}

// BuildChanged reports whether a state frame gave a build that differs
// from the first non-empty build this client saw.
func (c *Client) BuildChanged() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buildChanged
}

// Results returns completed commands. Consume results before submitting more work.
func (c *Client) Results() <-chan Result { return c.results }

// Submit accepts one command at a time to preserve sequence and explicit control values.
func (c *Client) Submit(command session.Command) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connected {
		return errors.New("waiting for the server connection")
	}
	if c.pending {
		return errors.New("waiting for the previous command")
	}
	if command.Project != nil {
		command.Project = new(project.Clone(*command.Project))
	}
	c.sequence++
	command.Client, command.Sequence, command.Epoch = c.client, c.sequence, c.state.Epoch
	c.pending = true
	c.commands <- command
	return nil
}

// acceptLocked keeps a state from a new epoch, or a state from the current
// epoch with the same or a higher revision. It drops a state from a retired
// epoch until retiredEpochPolls frames in a row carry that epoch. Then that
// epoch becomes the current epoch again. The caller must hold c.mu.
func (c *Client) acceptLocked(state session.State) {
	if state.Epoch == "" {
		return
	}
	if c.oldEpochs[state.Epoch] {
		if state.Epoch != c.retiredEpoch {
			c.retiredEpoch, c.retiredPolls = state.Epoch, 0
		}
		c.retiredPolls++
		if c.retiredPolls < retiredEpochPolls {
			return
		}
		delete(c.oldEpochs, state.Epoch)
	}
	c.retiredEpoch, c.retiredPolls = "", 0
	if state.Epoch != c.state.Epoch {
		if c.state.Epoch != "" {
			c.oldEpochs[c.state.Epoch] = true
		}
	} else if state.Revision < c.state.Revision {
		return
	}
	c.state = state
}

// noteBuild records the build ID of a state frame. The first non-empty ID
// is the baseline. An empty ID does not set the baseline and is not a change.
func (c *Client) noteBuild(build string) {
	if build == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.build == "" {
		c.build = build
	} else if build != c.build {
		c.buildChanged = true
	}
}

func (c *Client) poll(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var frame session.StateFrame
		err := c.exchange(ctx, "GET", "/api/state", nil, &frame)
		// Record the build before the error check and the topology read.
		// A frame from a new server can have a member with a type that
		// this client does not know. The decoder then returns an error,
		// but it still fills the other members. The topology read can also
		// fail on a new server. The build change must show in these cases.
		// A failed request leaves the build empty, and noteBuild ignores it.
		c.noteBuild(frame.Build)
		var state session.State
		if err == nil {
			state, err = c.stateForFrame(ctx, frame)
		}
		// Change the state, the connection state, and the frame time in one
		// step. A reader then does not see a new state together with the
		// connection state of an earlier poll.
		c.mu.Lock()
		c.connected = err == nil
		if c.connected {
			c.acceptLocked(state)
			c.lastFrame = time.Now()
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Client) stateForFrame(ctx context.Context, frame session.StateFrame) (session.State, error) {
	c.mu.Lock()
	topology := c.topology
	c.mu.Unlock()
	if topology.Epoch != frame.Epoch || topology.ProjectRevision != frame.ProjectRevision {
		if err := c.exchange(ctx, "GET", "/api/topology", nil, &topology); err != nil {
			return session.State{}, err
		}
		if topology.Epoch != frame.Epoch || topology.ProjectRevision != frame.ProjectRevision {
			return session.State{}, errors.New("topology changed while reading state")
		}
		c.mu.Lock()
		c.topology = topology
		c.mu.Unlock()
	}
	return session.FrameState(topology, frame)
}

func (c *Client) runCommands(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case command := <-c.commands:
			result := c.send(ctx, command)
			select {
			case c.results <- result:
			case <-ctx.Done():
				return
			}
			c.mu.Lock()
			c.pending = false
			c.mu.Unlock()
		}
	}
}

func (c *Client) send(ctx context.Context, command session.Command) Result {
	result := Result{Command: command}
	body, err := json.Marshal(command)
	if err != nil {
		result.Err = err
		return result
	}
	for range 3 {
		result.Reply = session.Reply{}
		result.Err = c.exchange(ctx, "POST", "/api/command", body, &result.Reply)
		if result.Err == nil || result.Reply.Error != "" {
			return result
		}
		var status *statusError
		if errors.As(result.Err, &status) && status.code < 500 {
			return result
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result
		case <-timer.C:
		}
	}
	result.Err = fmt.Errorf("confirmation unavailable; check Orders before retrying: %w", result.Err)
	return result
}

type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("server returned HTTP %d", e.code) }

func (c *Client) exchange(ctx context.Context, method, path string, body []byte, target any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusConflict {
		return &statusError{code: response.StatusCode}
	}
	decoder := json.NewDecoder(response.Body)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("read server state: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid server response ending")
	}
	return nil
}
