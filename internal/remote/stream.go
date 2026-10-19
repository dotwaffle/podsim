package remote

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
)

// ConnectionError reports the current connection or unsupported-browser error.
func (c *Client) ConnectionError() string { c.mu.Lock(); defer c.mu.Unlock(); return c.connectionError }

func (c *Client) stream(ctx context.Context) {
	if err := streamSupported(); err != nil {
		c.mu.Lock()
		c.connectionError = err.Error()
		c.mu.Unlock()
		return
	}
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		started := time.Now()
		err := c.receiveStream(ctx)
		if time.Since(started) >= 30*time.Second {
			backoff = 250 * time.Millisecond
		}
		c.mu.Lock()
		c.connected = false
		if err != nil {
			c.connectionError = err.Error()
		}
		c.mu.Unlock()
		timer := time.NewTimer(backoff/2 + time.Duration(rand.Int64N(int64(backoff/2)+1)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, 10*time.Second)
	}
}
func (c *Client) receiveStream(ctx context.Context) error {
	streamDiagnostic("connecting")
	defer streamDiagnostic("disconnected")
	url := strings.Replace(strings.Replace(c.url, "https://", "wss://", 1), "http://", "ws://", 1) + "/api/state/stream"
	conn, response, err := dialStream(ctx, url, c.http)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.socket = conn
	c.mu.Unlock()
	defer func() { _ = conn.CloseNow(); c.mu.Lock(); c.socket = nil; c.mu.Unlock() }()
	conn.SetReadLimit(session.MaxStreamMessage)
	hello, err := c.readHello(ctx, conn)
	if err != nil {
		return err
	}
	return c.receiveFrames(ctx, conn, hello)
}

// readStream reads one stream message within the read deadline.
func readStream(ctx context.Context, conn *websocket.Conn) (websocket.MessageType, []byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	kind, data, err := conn.Read(bounded)
	if err == nil {
		streamDiagnostic("received", len(data))
	}
	return kind, data, err
}

// readHello reads and decodes the first message of a connection.
func (c *Client) readHello(ctx context.Context, conn *websocket.Conn) (session.StreamHello, error) {
	kind, data, err := readStream(ctx, conn)
	if err != nil {
		return session.StreamHello{}, err
	}
	if kind != websocket.MessageText || len(data) > 4096 {
		return session.StreamHello{}, errors.New("missing state stream hello")
	}
	hello, err := session.DecodeStreamHello(data)
	c.noteBuild(hello.Build)
	return hello, err
}

// receiveFrames applies the publications of a connection after its hello
// and answers its heartbeats. It returns only with an error.
func (c *Client) receiveFrames(ctx context.Context, conn *websocket.Conn, hello session.StreamHello) error {
	var frame session.StreamFrame
	var stream string
	var sequence uint64
	cache := streamTopology{hello: hello}
	for {
		data, err := c.readPublication(ctx, conn)
		if err != nil {
			return err
		}
		processingStarted := time.Now()
		envelope, err := c.decodePublication(ctx, data, hello)
		if err != nil {
			return err
		}
		candidate, err := session.ApplyStream(frame, stream, sequence, envelope)
		if err != nil {
			return err
		}
		state, err := cache.state(ctx, c, candidate)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := c.publish(state); err != nil {
			return err
		}
		streamDiagnostic("applied", float64(time.Since(processingStarted))/float64(time.Millisecond), envelope.Kind)
		frame, stream, sequence = candidate, envelope.Stream, envelope.Sequence
		if err := writeControl(ctx, conn, map[string]string{"kind": "ack", "stream": stream, "sequence": strconv.FormatUint(sequence, 10)}); err != nil {
			return err
		}
	}
}

// readPublication reads messages until a binary publication arrives. It
// answers each heartbeat before it.
func (c *Client) readPublication(ctx context.Context, conn *websocket.Conn) ([]byte, error) {
	for {
		kind, data, err := readStream(ctx, conn)
		if err != nil {
			return nil, err
		}
		if kind == websocket.MessageText {
			if err := c.answerHeartbeat(ctx, conn, data); err != nil {
				return nil, err
			}
			continue
		}
		if kind != websocket.MessageBinary {
			return nil, errors.New("invalid state message")
		}
		return data, nil
	}
}

// answerHeartbeat checks a heartbeat control message and echoes it.
func (c *Client) answerHeartbeat(ctx context.Context, conn *websocket.Conn, data []byte) error {
	if len(data) > 1024 {
		return errors.New("state control too large")
	}
	var heartbeat struct {
		Kind  string `json:"kind"`
		Token string `json:"token"`
	}
	if err := jsonv2.Unmarshal(data, &heartbeat, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false)); err != nil || heartbeat.Kind != "heartbeat" {
		return errors.New("invalid heartbeat")
	}
	if _, err := session.ParseStreamSequence(heartbeat.Token); err != nil {
		return err
	}
	if err := writeControl(ctx, conn, heartbeat); err != nil {
		return err
	}
	c.mu.Lock()
	if c.connected {
		c.lastFrame = time.Now()
		streamDiagnostic("heartbeat")
	}
	c.mu.Unlock()
	return nil
}

// decodePublication inflates and decodes a binary publication and checks
// it against the hello of the connection.
func (c *Client) decodePublication(ctx context.Context, data []byte, hello session.StreamHello) (session.StreamEnvelope, error) {
	inflated, err := inflatePublication(ctx, data)
	if err != nil {
		return session.StreamEnvelope{}, err
	}
	envelope, err := session.DecodeStreamJSON(inflated)
	c.noteBuild(envelope.Build)
	if err != nil {
		return session.StreamEnvelope{}, err
	}
	if envelope.OrderContract != hello.OrderContract {
		return session.StreamEnvelope{}, errors.New("stream publication contract differs from hello")
	}
	if envelope.Source.ServerStart != hello.ServerStart {
		return session.StreamEnvelope{}, errors.New("state server identity changed within connection")
	}
	return envelope, nil
}

// publish stores an assembled state unless it is older than the current
// state of the same server and epoch.
func (c *Client) publish(state session.State) error {
	c.mu.Lock()
	// This worker owns connections sequentially. No old connection can publish
	// after its successor. A verified baseline can restore an earlier epoch.
	stale := state.ServerStart == c.state.ServerStart && state.Epoch == c.state.Epoch && state.Revision < c.state.Revision
	if !stale {
		c.state = state
		c.connected = true
		c.connectionError = ""
		c.lastFrame = time.Now()
	}
	c.mu.Unlock()
	if stale {
		return errors.New("stale same-server state")
	}
	return nil
}

func writeControl(ctx context.Context, conn *websocket.Conn, value any) error {
	body, err := jsonv2.Marshal(value, json.DefaultOptionsV1())
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err = conn.Write(bounded, websocket.MessageText, body); err != nil {
		return fmt.Errorf("acknowledge state: %w", err)
	}
	return nil
}

// streamTopology belongs to one connection and retains immutable geometry.
// The contract markers of each topology must be the markers of the hello.
type streamTopology struct {
	hello     session.StreamHello
	topology  session.TopologySnapshot
	assembler *session.StreamAssembler
}

func (cache *streamTopology) state(ctx context.Context, c *Client, candidate session.StreamFrame) (session.State, error) {
	identity := candidate.State
	topology := cache.topology
	if cache.assembler == nil || topology.ServerStart != identity.ServerStart || topology.Epoch != identity.Epoch || topology.ProjectRevision != identity.ProjectRevision {
		// Published states share cached geometry, so decode into fresh storage.
		topology = session.TopologySnapshot{}
		if err := c.exchange(ctx, http.MethodGet, "/api/topology", nil, &topology); err != nil {
			return session.State{}, err
		}
		if topology.ServerStart != identity.ServerStart || topology.Epoch != identity.Epoch || topology.ProjectRevision != identity.ProjectRevision {
			return session.State{}, errors.New("topology changed while reading stream")
		}
		if topology.OrderContract != cache.hello.OrderContract {
			return session.State{}, errors.New("topology contract differs from hello")
		}
		assembler, err := session.NewStreamAssembler(topology)
		if err != nil {
			return session.State{}, err
		}
		state, err := assembler.State(candidate)
		if err != nil {
			return session.State{}, err
		}
		cache.topology, cache.assembler = topology, assembler
		return state, nil
	}
	return cache.assembler.State(candidate)
}
