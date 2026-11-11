// Package remote connects a presentation client to the shared session.
package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
)

// Result reports the outcome of a submitted command.
type Result struct {
	Command session.Command
	Reply   session.Reply
	Err     error
}

// ErrSessionChanged reports a command from a control of a session that the
// client no longer follows.
var ErrSessionChanged = errors.New("the session changed; check the map and press again")

// SessionID identifies one generation of one shared session. A fault ID or
// a lane ID has its meaning only in the session that showed it: a new server
// process starts its IDs again, and a project apply replaces the lanes.
type SessionID struct {
	ServerStart, Epoch string
	Generation         uint64
}

// SessionOf returns the identity of the session of state.
func SessionOf(state session.State) SessionID {
	return SessionID{ServerStart: state.ServerStart, Epoch: state.Epoch, Generation: state.Generation}
}

// Client streams state and serializes commands. All returned state is immutable.
type Client struct {
	mu              sync.Mutex
	url             string
	http            *http.Client
	state           session.State
	connected       bool
	connectionError string
	socket          *websocket.Conn
	// lastFrame is the time of the last verified state or heartbeat.
	lastFrame time.Time
	pending   bool
	client    string
	sequence  uint64
	// build is the first non-empty build ID that a state frame gave.
	// buildChanged becomes true when a later frame gives a different
	// non-empty build ID, and then it stays true.
	build        string
	buildChanged bool
	commands     chan session.Command
	results      chan Result
}

// New starts the state stream and command processing until ctx is canceled.
func New(ctx context.Context, serverURL string) *Client {
	c := &Client{url: strings.TrimRight(serverURL, "/"), http: &http.Client{Timeout: 3 * time.Second}, client: rand.Text(), commands: make(chan session.Command, 1), results: make(chan Result, 1)}
	go c.stream(ctx)
	go c.runCommands(ctx)
	return c
}

// View returns the latest state and whether a new command can be submitted.
func (c *Client) View() (session.State, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state, c.connected, c.pending
}

// LastFrame returns the time of the last verified state or heartbeat.
// A game uses it to give the age of the shown state while
// the connection is lost. Before the first frame, it returns the zero time.
func (c *Client) LastFrame() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastFrame
}

// BuildChanged reports whether a stream hello or frame gave a build that differs
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
	return c.submitLocked(command)
}

// SubmitIn is Submit for a command with a target from the session shown. It
// refuses the command with ErrSessionChanged when the client follows another
// session. Submit gives a command the current epoch, so the server cannot
// find this difference. The stream can change the session at any time, so
// the check holds the lock that Submit uses to read the epoch.
func (c *Client) SubmitIn(shown SessionID, command session.Command) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if SessionOf(c.state) != shown {
		return ErrSessionChanged
	}
	return c.submitLocked(command)
}

func (c *Client) submitLocked(command session.Command) error {
	if !c.connected {
		return errors.New("waiting for the server connection")
	}
	if c.pending {
		return errors.New("waiting for the previous command")
	}
	if command.Project != nil {
		command.Project = new(project.Clone(*command.Project))
	}
	if command.Action == "trip" {
		command.OrderContract = c.state.Simulation.OrderContract
	}
	c.sequence++
	command.Client, command.Sequence, command.Epoch = c.client, c.sequence, c.state.Epoch
	c.pending = true
	c.commands <- command
	return nil
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
	body, err := jsonv2.Marshal(command, json.DefaultOptionsV1())
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

// errResponseTooLarge reports a response body above the limit of its endpoint.
var errResponseTooLarge = errors.New("server response exceeds supported limit")

// responseLimit returns the largest response body for path. The server
// writes a JSON body with an encoder that adds a newline, so a limit is one
// byte more than the largest document.
func responseLimit(path string) int64 {
	if path == "/api/topology" {
		return session.MaxTopologyJSON + 1
	}
	// A command reply has a short error text and a few numbers.
	return session.MaxCommandBytes
}

// readResponse reads body, but not more than limit bytes.
func readResponse(body io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: more than %d bytes", errResponseTooLarge, limit)
	}
	return raw, nil
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
	raw, err := readResponse(response.Body, responseLimit(path))
	if err != nil {
		return err
	}
	switch target := target.(type) {
	case *session.TopologySnapshot:
		// The topology decoder bounds the document before it parses it. A
		// JSON decoder would walk the whole document first.
		return target.UnmarshalJSON(bytes.TrimSpace(raw))
	case *session.Reply:
		if target != nil {
			return decodeReply(raw, target)
		}
	}
	return errors.New("server response needs a non-nil pointer target")
}

// decodeReply decodes one JSON document in raw into target. It decodes into
// a new reply first, so that a body that fails to decode, or has data after
// the document, leaves target unchanged.
func decodeReply(raw []byte, target *session.Reply) error {
	var reply session.Reply
	// Member names match exactly. A client ignores a member that it does
	// not know, and the other encoding/json rules stay.
	decoder := jsontext.NewDecoder(bytes.NewReader(raw), json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false))
	if err := jsonv2.UnmarshalDecode(decoder, &reply); err != nil {
		return fmt.Errorf("read server state: %w", err)
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		return errors.New("invalid server response ending")
	}
	*target = reply
	return nil
}
