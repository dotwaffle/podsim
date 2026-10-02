package remote

import (
	"context"
	"encoding/json"
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
	read := func() (websocket.MessageType, []byte, error) {
		bounded, cancel := context.WithTimeout(ctx, 35*time.Second)
		defer cancel()
		kind, data, readErr := conn.Read(bounded)
		if readErr == nil {
			streamDiagnostic("received", len(data))
		}
		return kind, data, readErr
	}
	kind, data, err := read()
	if err != nil {
		return err
	}
	if kind != websocket.MessageText || len(data) > 4096 {
		return errors.New("missing state stream hello")
	}
	var hello struct {
		Kind        string `json:"kind"`
		Version     int    `json:"version"`
		Build       string `json:"build"`
		ServerStart string `json:"serverStart"`
	}
	err = json.Unmarshal(data, &hello)
	c.noteBuild(hello.Build)
	if err != nil || hello.Kind != "hello" || (hello.Version != 1 && hello.Version != session.StreamVersion) || hello.ServerStart == "" {
		return errors.New("unsupported state stream protocol")
	}
	var frame session.StreamFrame
	var stream string
	var sequence uint64
	cache := streamTopology{version: hello.Version}
	for {
		kind, data, err = read()
		if err != nil {
			return err
		}
		if kind == websocket.MessageText {
			if len(data) > 1024 {
				return errors.New("state control too large")
			}
			var heartbeat struct {
				Kind  string `json:"kind"`
				Token string `json:"token"`
			}
			if err = json.Unmarshal(data, &heartbeat); err != nil || heartbeat.Kind != "heartbeat" {
				return errors.New("invalid heartbeat")
			}
			if _, err = session.ParseStreamSequence(heartbeat.Token); err != nil {
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
			continue
		}
		if kind != websocket.MessageBinary {
			return errors.New("invalid state message")
		}
		processingStarted := time.Now()
		inflated, err := inflatePublication(ctx, data)
		if err != nil {
			return err
		}
		envelope, err := session.DecodeStreamJSON(inflated)
		c.noteBuild(envelope.Build)
		if err != nil {
			return err
		}
		if envelope.Source.ServerStart != hello.ServerStart {
			return errors.New("state server identity changed within connection")
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
		streamDiagnostic("applied", float64(time.Since(processingStarted))/float64(time.Millisecond), envelope.Kind)
		frame, stream, sequence = candidate, envelope.Stream, envelope.Sequence
		if err := writeControl(ctx, conn, map[string]string{"kind": "ack", "stream": stream, "sequence": strconv.FormatUint(sequence, 10)}); err != nil {
			return err
		}
	}
}
func writeControl(ctx context.Context, conn *websocket.Conn, value any) error {
	body, err := json.Marshal(value)
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
type streamTopology struct {
	version   int
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
		if cache.version == 1 {
			for _, station := range topology.Network.Stations {
				if station.Banks != nil {
					return session.State{}, errors.New("version 1 stream cannot contain Banks")
				}
			}
		}
		if topology.ServerStart != identity.ServerStart || topology.Epoch != identity.Epoch || topology.ProjectRevision != identity.ProjectRevision {
			return session.State{}, errors.New("topology changed while reading stream")
		}
		assembler, err := session.NewStreamAssembler(topology)
		if err != nil {
			return session.State{}, err
		}
		cache.topology, cache.assembler = topology, assembler
	}
	return cache.assembler.State(candidate)
}
