package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/sim"
)

const (
	streamConnections  = 64
	streamHistoryCount = 256
	streamHistoryBytes = 32 << 20
	streamCreditCount  = 64
	streamCreditBytes  = 8 << 20
	// Current full encoding and one previous full in flight have separate slots.
	streamRetainedBytes   = streamHistoryBytes + 2*MaxStreamMessage
	streamProgressTimeout = 30 * time.Second
	streamWriteTimeout    = 30 * time.Second
	streamCaptureInterval = 50 * time.Millisecond
)

type streamPayload struct {
	stream   string
	sequence uint64
	data     []byte
	refs     int
	full     bool
}
type streamSent struct {
	stream   string
	sequence uint64
	bytes    int
}
type streamSubscriber struct {
	orderContract sim.OrderContract
	conn          *websocket.Conn
	wake          chan struct{}
	sent          []streamSent
	bytes         int
	stream        string
	sequence      uint64
	progress      time.Time
	heartbeat     uint64
	heartbeats    uint64
	heartbeatAt   time.Time
	lastACK       streamSent
	writing       *streamPayload
	writingAt     time.Time
	cancel        context.CancelFunc
	shed          bool
}

// StreamMetrics exposes bounded aggregate transport measurements.
type StreamMetrics struct {
	Connections           int
	EncodingBytes         int
	PressureSheds         uint64
	Full                  uint64
	Delta                 uint64
	Bytes                 uint64
	RetainedBytes         int
	HistoryMessages       int
	OutstandingMessages   int
	OutstandingBytes      int
	ResyncHistory         uint64
	ResyncSource          uint64
	OldestACKMilliseconds int64
	HistoryFirst          uint64
	HistoryLast           uint64
}

type statePublisher struct {
	mu           sync.Mutex
	session      *Session
	cancel       context.CancelFunc
	done         chan struct{}
	workers      sync.WaitGroup
	clients      map[*streamSubscriber]bool
	wake         chan struct{}
	frame        StreamFrame
	stream       string
	sequence     uint64
	history      []*streamPayload
	historyBytes int
	retained     int
	encoding     int
	released     chan struct{}
	full         *streamPayload
	needFull     bool
	metrics      StreamMetrics
	stopped      bool
	// mu protects the pointer. Only run uses the encoder contents.
	encoder *streamEncoder
}

func (s *Session) publisher() *statePublisher {
	s.streamMu.Lock()
	defer s.streamMu.Unlock()
	if s.stream == nil && !s.closed.Load() {
		ctx, cancel := context.WithCancel(context.Background())
		p := &statePublisher{session: s, cancel: cancel, done: make(chan struct{}), clients: map[*streamSubscriber]bool{}, wake: make(chan struct{}, 1)}
		s.stream = p
		go p.run(ctx)
	}
	return s.stream
}

// StreamStats returns aggregate stream accounting without client labels.
func (s *Session) StreamStats() StreamMetrics {
	s.streamMu.Lock()
	p := s.stream
	s.streamMu.Unlock()
	if p == nil {
		return StreamMetrics{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.metrics
	m.EncodingBytes = p.encoding
	m.Connections = len(p.clients)
	m.RetainedBytes = p.retained
	m.HistoryMessages = len(p.history)
	if len(p.history) > 0 {
		m.HistoryFirst = p.history[0].sequence
		m.HistoryLast = p.history[len(p.history)-1].sequence
	}
	for c := range p.clients {
		m.OutstandingBytes += c.bytes
		m.OutstandingMessages += len(c.sent)
		if len(c.sent) > 0 {
			m.OldestACKMilliseconds = max(m.OldestACKMilliseconds, time.Since(c.progress).Milliseconds())
		}
	}
	return m
}

func (s *Session) stopStreams() {
	s.streamMu.Lock()
	p := s.stream
	s.streamMu.Unlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	p.stopped = true
	p.cancel()
	for c := range p.clients {
		if c.conn != nil {
			_ = c.conn.CloseNow()
		}
	}
	p.mu.Unlock()
}

// WaitStreams waits for upgraded sockets and publisher work after Close.
func (s *Session) WaitStreams(ctx context.Context) error {
	s.streamMu.Lock()
	p := s.stream
	s.streamMu.Unlock()
	if p == nil {
		return nil
	}
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func wakeStream(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
func (p *statePublisher) release(b *streamPayload) {
	if b == nil {
		return
	}
	b.refs--
	if b.refs == 0 {
		p.retained -= cap(b.data)
		b.data = nil
	}
}
func (p *statePublisher) retain(ctx context.Context, e StreamEnvelope) (*streamPayload, error) {
	p.mu.Lock()
	if p.encoder == nil {
		p.encoder = new(streamEncoder)
	}
	encoder := p.encoder
	p.mu.Unlock()
	data, err := encoder.encode(e)
	if err != nil {
		return nil, err
	}
	return p.admitEncoded(ctx, &streamPayload{stream: e.Stream, sequence: e.Sequence, data: data, full: e.Kind == "full"}, streamRetainedBytes)
}

// admitEncoded owns the publisher's single pending compressed encoding.
// Publisher ownership is dropped before writers are canceled for pressure.
func (p *statePublisher) admitEncoded(ctx context.Context, b *streamPayload, limit int) (*streamPayload, error) {
	p.mu.Lock()
	p.encoding = cap(b.data)
	if p.released == nil {
		p.released = make(chan struct{}, 1)
	}
	defer func() { p.encoding = 0; p.mu.Unlock() }()
	for {
		for len(p.history) > 0 && (p.retained+cap(b.data) > limit || p.historyBytes+cap(b.data) > streamHistoryBytes || len(p.history) >= streamHistoryCount) {
			p.evict()
		}
		if p.retained+cap(b.data) <= limit {
			b.refs = 1
			p.retained += cap(b.data)
			return b, nil
		}
		if p.full != nil {
			p.release(p.full)
			p.full = nil
			p.needFull = true
			continue
		}
		var oldest *streamSubscriber
		for c := range p.clients {
			if c.writing != nil && (oldest == nil || c.writingAt.Before(oldest.writingAt)) {
				oldest = c
			}
		}
		if oldest == nil {
			return nil, errors.New("stream retention has no lease owner")
		}
		payload := oldest.writing
		for c := range p.clients {
			if c.writing == payload && !c.shed {
				c.shed = true
				p.metrics.PressureSheds++
				c.cancel()
			}
		}
		released := p.released
		p.mu.Unlock()
		select {
		case <-ctx.Done():
			p.mu.Lock()
			return nil, ctx.Err()
		case <-released:
		}
		p.mu.Lock()
	}
}
func (p *statePublisher) evict() {
	b := p.history[0]
	p.history[0] = nil
	p.history = p.history[1:]
	p.historyBytes -= cap(b.data)
	p.release(b)
}
func (p *statePublisher) notify() {
	for c := range p.clients {
		wakeStream(c.wake)
	}
}

func (p *statePublisher) run(ctx context.Context) {
	defer func() {
		p.workers.Wait()
		p.mu.Lock()
		for len(p.history) > 0 {
			p.evict()
		}
		p.release(p.full)
		p.full = nil
		p.encoder = nil
		p.mu.Unlock()
		close(p.done)
	}()
	timer := time.NewTimer(streamCaptureInterval)
	defer timer.Stop()
	var captured time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-p.wake:
		}
		if ctx.Err() != nil {
			return
		}
		p.mu.Lock()
		active := len(p.clients) > 0
		need := p.needFull
		p.mu.Unlock()
		if !active {
			p.mu.Lock()
			if len(p.clients) == 0 {
				for len(p.history) > 0 {
					p.evict()
				}
				p.release(p.full)
				p.full = nil
				p.frame = StreamFrame{}
				p.stream = ""
				p.sequence = 0
				p.needFull = false
				p.encoder = nil
			}
			p.mu.Unlock()
			timer.Reset(streamCaptureInterval)
			continue
		}
		now := time.Now()
		capture := p.sequence == 0 || now.Sub(captured) >= streamCaptureInterval
		if capture {
			captured = now
		}
		if err := p.publish(ctx, need, capture); err != nil {
			p.session.logger.Warn("State stream publication failed", "error", err)
			p.mu.Lock()
			for c := range p.clients {
				if c.conn != nil {
					_ = c.conn.CloseNow()
				}
			}
			p.mu.Unlock()
		}
		// Keep the capture deadline across early recovery wakes. A fixed ticker
		// can skip its next event when scheduling jitter makes it arrive early.
		timer.Reset(streamCaptureDelay(captured, time.Now()))
	}
}

func streamCaptureDelay(captured, now time.Time) time.Duration {
	if captured.IsZero() {
		return streamCaptureInterval
	}
	return max(0, streamCaptureInterval-now.Sub(captured))
}

func (p *statePublisher) publish(ctx context.Context, need, capture bool) error {
	frame := p.frame
	if capture {
		var err error
		frame, err = p.session.presentationFrame()
		if err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	changed := p.sequence == 0 || p.sequence == sim.MaxCounter || !reflect.DeepEqual(p.frame, frame)
	reset := p.sequence == 0 || p.sequence == sim.MaxCounter || !sameChain(p.frame, frame)
	if changed {
		stream, seq := p.stream, p.sequence+1
		if reset {
			stream = rand.Text()
			seq = 1
		}
		e := StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "delta", Stream: stream, Sequence: seq, Base: p.sequence, Source: sourceOf(frame), Build: frame.State.Build}
		if reset {
			e.Kind = "full"
			e.Base = 0
			e.Full = &frame
		} else {
			d, err := makeDelta(p.frame, frame)
			if err != nil {
				return err
			}
			e.Delta = &d
		}
		b, err := p.retain(ctx, e)
		if err != nil {
			return err
		}
		full := reset
		if !full && cap(b.data) > streamHistoryBytes {
			p.mu.Lock()
			p.release(b)
			p.mu.Unlock()
			e.Kind, e.Base, e.Full, e.Delta = "full", 0, &frame, nil
			b, err = p.retain(ctx, e)
			if err != nil {
				return err
			}
			full = true
		}
		p.mu.Lock()
		if full {
			for len(p.history) > 0 {
				p.evict()
			}
		}
		p.release(p.full)
		p.full = nil
		p.frame, p.stream, p.sequence = frame, stream, seq
		if full {
			p.full = b
			p.needFull = false
			p.metrics.Full++
		} else {
			p.history = append(p.history, b)
			p.historyBytes += cap(b.data)
			p.metrics.Delta++
		}
		p.metrics.Bytes += uint64(len(b.data))
		p.notify()
		p.mu.Unlock()
	}
	p.mu.Lock()
	need = need || p.needFull
	hasFull := p.full != nil
	if hasFull {
		p.needFull = false
	}
	p.mu.Unlock()
	if need && !hasFull {
		e := StreamEnvelope{OrderContract: p.frame.State.Simulation.OrderContract, Kind: "full", Stream: p.stream, Sequence: p.sequence, Source: sourceOf(p.frame), Build: p.frame.State.Build, Full: &p.frame}
		b, err := p.retain(ctx, e)
		if err != nil {
			return err
		}
		p.mu.Lock()
		p.full = b
		p.metrics.Full++
		p.metrics.Bytes += uint64(len(b.data))
		p.needFull = false
		p.notify()
		p.mu.Unlock()
	}
	return nil
}

//nolint:contextcheck // The shared publisher belongs to the session, not a request.
func (s *Session) streamHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, "use GET", http.StatusMethodNotAllowed)
		return
	}
	if !s.originAllowed(r) {
		writeError(w, "cross-origin state streams are not allowed", http.StatusForbidden)
		return
	}
	p := s.publisher()
	if p == nil {
		writeError(w, "server stopping", http.StatusServiceUnavailable)
		return
	}
	// Reserve admission through the upgrade to keep concurrent handshakes bounded.
	p.mu.Lock()
	if p.stopped || len(p.clients) >= streamConnections {
		p.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		writeError(w, "state stream busy", http.StatusServiceUnavailable)
		return
	}
	c := &streamSubscriber{wake: make(chan struct{}, 1), progress: time.Now()}
	p.clients[c] = true
	p.workers.Add(1)
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.clients, c); p.mu.Unlock(); p.workers.Done() }()
	// The shared policy above validates the complete normalized origin.
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled, InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(1024)
	p.mu.Lock()
	c.conn = conn
	stopped := p.stopped
	p.needFull = true
	p.mu.Unlock()
	if stopped {
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	p.mu.Lock()
	c.cancel = cancel
	p.mu.Unlock()
	watchDone := make(chan struct{})
	go func() { defer close(watchDone); p.watchProgress(ctx, c, cancel) }()
	defer func() { cancel(); <-watchDone }()
	readerDone := make(chan struct{})
	go func() { defer close(readerDone); defer cancel(); _ = p.readControls(ctx, c) }()
	defer func() { cancel(); _ = conn.CloseNow(); <-readerDone }()
	s.mu.Lock()
	contract := s.project.OrderContract
	hello := StreamHello{Kind: "hello", Version: StreamVersion, Build: s.build, ServerStart: s.serverStart, OrderContract: contract}
	s.mu.Unlock()
	p.mu.Lock()
	c.orderContract = contract
	p.mu.Unlock()
	helloData, marshalErr := jsonv2.Marshal(hello, json.DefaultOptionsV1())
	if marshalErr != nil {
		return
	}
	if err = streamWrite(ctx, conn, websocket.MessageText, helloData); err != nil {
		return
	}
	wakeStream(p.wake)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		currentContract := s.project.OrderContract
		s.mu.Unlock()
		if currentContract != contract {
			return
		}
		if err := p.sendAvailable(ctx, c); err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case now := <-ticker.C:
			p.mu.Lock()
			expired := streamExpired(c, now)
			var heartbeat uint64
			if !expired && c.heartbeat == 0 && len(c.sent) == 0 {
				heartbeat = c.startHeartbeat(now)
			}
			p.mu.Unlock()
			if expired {
				return
			}
			if heartbeat != 0 {
				body, marshalErr := jsonv2.Marshal(map[string]string{"kind": "heartbeat", "token": strconv.FormatUint(heartbeat, 10)}, json.DefaultOptionsV1())
				if marshalErr != nil || streamWrite(ctx, conn, websocket.MessageText, body) != nil {
					return
				}
			}
		}
	}
}

type streamMessageWriter interface {
	Write(context.Context, websocket.MessageType, []byte) error
}

func streamWrite(ctx context.Context, c streamMessageWriter, kind websocket.MessageType, data []byte) error {
	return streamWriteWithin(ctx, c, kind, data, streamWriteTimeout)
}

func streamWriteWithin(ctx context.Context, c streamMessageWriter, kind websocket.MessageType, data []byte, timeout time.Duration) error {
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return c.Write(bounded, kind, data)
}

func (p *statePublisher) nextPayload(c *streamSubscriber) *streamPayload {
	if c.stream == p.stream && c.sequence == p.sequence {
		return nil
	}
	if c.stream == p.stream {
		for _, b := range p.history {
			if b.sequence == c.sequence+1 {
				return b
			}
		}
	}
	if p.full == nil {
		p.needFull = true
		wakeStream(p.wake)
		return nil
	}
	if c.stream != "" {
		if c.stream != p.stream {
			p.metrics.ResyncSource++
		} else {
			p.metrics.ResyncHistory++
		}
	}
	return p.full
}
func (p *statePublisher) sendAvailable(ctx context.Context, c *streamSubscriber) error {
	for {
		p.mu.Lock()
		if p.stopped {
			p.mu.Unlock()
			return errors.New("stream stopped")
		}
		if p.sequence != 0 && p.frame.State.Simulation.OrderContract != c.orderContract {
			p.mu.Unlock()
			return errors.New("stream contract changed; negotiate a new hello")
		}
		if len(c.sent) >= streamCreditCount || c.bytes >= streamCreditBytes {
			p.mu.Unlock()
			return nil
		}
		b := p.nextPayload(c)
		if b == nil {
			p.mu.Unlock()
			return nil
		}
		size := len(b.data)
		if len(c.sent) > 0 && c.bytes+size > streamCreditBytes {
			p.mu.Unlock()
			return nil
		}
		b.refs++
		c.writing = b
		c.writingAt = time.Now()
		c.sent = append(c.sent, streamSent{b.stream, b.sequence, size})
		c.bytes += size
		c.stream, c.sequence = b.stream, b.sequence
		if len(c.sent) == 1 {
			c.progress = time.Now()
		}
		p.mu.Unlock()
		err := streamWrite(ctx, c.conn, websocket.MessageBinary, b.data)
		p.mu.Lock()
		p.release(b)
		c.writing = nil
		wakeStream(p.released)
		p.mu.Unlock()
		if err != nil {
			return err
		}
	}
}
func (p *statePublisher) readControls(ctx context.Context, c *streamSubscriber) error {
	for {
		kind, data, err := c.conn.Read(ctx)
		if err != nil {
			return err
		}
		if kind != websocket.MessageText {
			return errors.New("binary control")
		}
		var control struct {
			Kind     string `json:"kind"`
			Stream   string `json:"stream"`
			Sequence string `json:"sequence"`
			Token    string `json:"token"`
		}
		if decodeErr := decodeStreamJSON(data, &control); decodeErr != nil {
			return decodeErr
		}
		p.mu.Lock()
		err = p.control(c, control.Kind, control.Stream, control.Sequence, control.Token, time.Now())
		p.mu.Unlock()
		if err != nil {
			return err
		}
		wakeStream(c.wake)
	}
}

// startHeartbeat records a new pending heartbeat and returns its token.
// The token counts the heartbeats of the connection, so it stays within
// the sequence range that ParseStreamSequence accepts.
func (c *streamSubscriber) startHeartbeat(now time.Time) uint64 {
	c.heartbeats++
	c.heartbeat, c.heartbeatAt = c.heartbeats, now
	return c.heartbeat
}

func (p *statePublisher) control(c *streamSubscriber, kind, stream, sequence, token string, now time.Time) error {
	switch kind {
	case "heartbeat":
		n, err := ParseStreamSequence(token)
		if err != nil || n == 0 || n != c.heartbeat {
			return errors.New("invalid heartbeat")
		}
		c.heartbeat = 0
		return nil
	case "ack":
		n, err := ParseStreamSequence(sequence)
		if err != nil {
			return err
		}
		if c.lastACK.stream == stream && c.lastACK.sequence == n {
			return nil
		}
		index := -1
		for i, s := range c.sent {
			if s.stream == stream && s.sequence == n {
				index = i
				break
			}
		}
		if index < 0 {
			return errors.New("acknowledgment was not sent")
		}
		for _, s := range c.sent[:index+1] {
			c.bytes -= s.bytes
		}
		c.lastACK = c.sent[index]
		copy(c.sent, c.sent[index+1:])
		clear(c.sent[len(c.sent)-index-1:])
		c.sent = c.sent[:len(c.sent)-index-1]
		c.progress = now
		return nil
	default:
		return errors.New("unknown stream control")
	}
}

func streamExpired(c *streamSubscriber, now time.Time) bool {
	return len(c.sent) > 0 && now.Sub(c.progress) > streamProgressTimeout || c.heartbeat != 0 && now.Sub(c.heartbeatAt) > streamProgressTimeout
}

// watchProgress runs independently of writes, which can block on a slow reader.
func (p *statePublisher) watchProgress(ctx context.Context, c *streamSubscriber, cancel context.CancelFunc) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			p.mu.Lock()
			expired := streamExpired(c, now)
			p.mu.Unlock()
			if expired {
				cancel()
				return
			}
		}
	}
}
