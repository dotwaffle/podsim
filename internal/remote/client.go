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

// Client polls state and serializes commands. All returned state is immutable.
type Client struct {
	mu        sync.Mutex
	url       string
	http      *http.Client
	state     session.State
	connected bool
	pending   bool
	client    string
	sequence  uint64
	oldEpochs map[string]bool
	commands  chan session.Command
	results   chan Result
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

func (c *Client) accept(state session.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state.Epoch == "" || c.oldEpochs[state.Epoch] {
		return
	}
	if state.Epoch != c.state.Epoch {
		if c.state.Epoch != "" {
			c.oldEpochs[c.state.Epoch] = true
		}
	} else if state.Revision < c.state.Revision {
		return
	}
	c.state = state
}

func (c *Client) poll(ctx context.Context) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state session.State
		err := c.exchange(ctx, "GET", "/api/state", nil, &state)
		if err == nil {
			c.accept(state)
		}
		c.mu.Lock()
		c.connected = err == nil
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
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
		if result.Reply.State.Epoch != "" {
			c.accept(result.Reply.State)
		}
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
