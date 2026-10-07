package parkride

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// MaxCheckpointBytes bounds the complete file and each transient observation encoding.
const MaxCheckpointBytes = 100 << 20
const checkpointFormat = "podsim-car-continuation"
const nativeEncoding = "sim-saved-state-v1"

// Implementation identifies the clean executable that owns a replay lineage.
// The CLI verifies compiled build facts and the executable bytes. Go callers
// must provide those verified facts rather than claiming a source identity.
type Implementation struct {
	SourceRevision   string `json:"sourceRevision"`
	ExecutableSHA256 string `json:"executableSHA256"`
	GoVersion        string `json:"goVersion"`
	GoExperiment     string `json:"goExperiment"`
	GoOS             string `json:"goOS"`
	GoArch           string `json:"goArch"`
}

// ResumeProgress describes an isolated candidate, before acceptance.
type ResumeProgress struct {
	Tick, TargetTick int64
	CheckpointID     string
}

// ResumeInput supplies verified executable facts and optional replay progress.
type ResumeInput struct {
	Implementation Implementation
	Progress       func(ResumeProgress)
}

type origin struct {
	Project        project.Config `json:"project"`
	Plan           Plan           `json:"plan"`
	ProjectHash    string         `json:"projectHash"`
	PlanHash       string         `json:"planHash"`
	HorizonTicks   int64          `json:"horizonTicks"`
	QueueLimit     int            `json:"queueLimit"`
	ReportBuild    string         `json:"reportBuild"`
	Implementation Implementation `json:"implementation"`
}
type checkpointLeg struct {
	RequestID    int    `json:"requestID"`
	OfferedTick  int64  `json:"offeredTick"`
	BoardedTick  int64  `json:"boardedTick"`
	AlightedTick int64  `json:"alightedTick"`
	Reason       string `json:"reason"`
}
type checkpointRecord struct {
	Stage              string        `json:"stage"`
	Outcome            string        `json:"outcome"`
	Held               bool          `json:"held"`
	CarArrivalTick     int64         `json:"carArrivalTick"`
	Outward            checkpointLeg `json:"outward"`
	Return             checkpointLeg `json:"return"`
	ReturnEligibleTick int64         `json:"returnEligibleTick"`
	CarReleaseTick     int64         `json:"carReleaseTick"`
	HomeArrivalTick    int64         `json:"homeArrivalTick"`
	DoorToDoorTicks    int64         `json:"doorToDoorTicks"`
}
type checkpointLot struct {
	Occupancy int64 `json:"occupancy"`
	Peak      int64 `json:"peak"`
}
type checkpointLedger struct {
	LastTick int64              `json:"lastTick"`
	Records  []checkpointRecord `json:"records"`
	Lots     []checkpointLot    `json:"lots"`
}
type checkpointPayload struct {
	RunID           string           `json:"runID"`
	Origin          origin           `json:"origin"`
	Tick            int64            `json:"tick"`
	Phase           string           `json:"phase"`
	NativeEncoding  string           `json:"nativeEncoding"`
	Native          sim.SavedState   `json:"native"`
	NativeHash      string           `json:"nativeHash"`
	Ledger          checkpointLedger `json:"ledger"`
	LedgerHash      string           `json:"ledgerHash"`
	ObservationHash string           `json:"observationHash"`
	TraceHash       string           `json:"traceHash"`
}
type checkpointFile struct {
	Format       string            `json:"format"`
	Version      int               `json:"version"`
	CheckpointID string            `json:"checkpointID"`
	Payload      checkpointPayload `json:"payload"`
}
type continuation struct {
	origin                                  origin
	runID                                   string
	boarded                                 map[int]int64
	trace                                   [32]byte
	nativeHash, ledgerHash, observationHash string
	invalid                                 error
}

func validateImplementation(identity Implementation) error {
	if !hexText(identity.SourceRevision, 40) || !hexText(identity.ExecutableSHA256, 64) || identity.GoExperiment != "jsonv2" {
		return errors.New("car continuation requires an identified clean jsonv2 executable")
	}
	for _, text := range []string{identity.GoVersion, identity.GoExperiment} {
		if text == "" || len(text) > 128 || !utf8.ValidString(text) {
			return errors.New("invalid continuation Go build identity")
		}
	}
	for _, text := range []string{identity.GoOS, identity.GoArch} {
		if text == "" || len(text) > 32 {
			return errors.New("invalid continuation platform identity")
		}
		for _, char := range text {
			if char < 'a' || char > 'z' {
				if char < '0' || char > '9' {
					return errors.New("invalid continuation platform identity")
				}
			}
		}
	}
	return nil
}
func hexText(text string, length int) bool {
	if len(text) != length {
		return false
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}
func (r *Run) enableContinuation(ctx context.Context, config project.Config, plan Plan, identity Implementation) error {
	if len(r.provenance.Build) > 256 || !utf8.ValidString(r.provenance.Build) {
		return errors.New("report build exceeds continuation identity bound")
	}
	c := &continuation{origin: origin{Project: config, Plan: plan, ProjectHash: r.provenance.ProjectHash, PlanHash: r.provenance.PlanHash,
		HorizonTicks: r.provenance.HorizonTicks, QueueLimit: r.provenance.QueueLimit, ReportBuild: r.provenance.Build, Implementation: identity}, boarded: make(map[int]int64)}
	for _, value := range []any{config, plan} {
		if _, err := hashBounded(ctx, value, project.MaxFileBytes); err != nil {
			return fmt.Errorf("continuation origin: %w", err)
		}
	}
	id, err := hashBounded(ctx, c.origin, MaxCheckpointBytes)
	if err != nil {
		return err
	}
	c.runID = id
	seed := append([]byte("podsim-car-trace-v1"), decodeHash(id)...)
	c.trace = sha256.Sum256(seed)
	r.continuation = c
	return nil
}
func (c *continuation) clone() *continuation {
	if c == nil {
		return nil
	}
	clone := *c
	clone.boarded = maps.Clone(c.boarded)
	return &clone
}
func (c *continuation) observeBoardings(pods *sim.Simulation) {
	for _, vehicle := range pods.MetricsSnapshot().Vehicles {
		for _, rider := range vehicle.Riders {
			if _, ok := c.boarded[rider.ID]; !ok {
				c.boarded[rider.ID] = rider.BoardedTick
			}
		}
	}
}

type continuationService struct {
	pods         *sim.Simulation
	continuation *continuation
}

// PendingCount returns native queue usage.
func (p continuationService) PendingCount() int { return p.pods.PendingCount() }

// SubmitTripOptions records actual membership after synchronous native dispatch.
func (p continuationService) SubmitTripOptions(options sim.TripOptions) (int, error) {
	id, err := p.pods.SubmitTripOptions(options)
	if err == nil {
		p.continuation.observeBoardings(p.pods)
	}
	return id, err
}
func (r *Run) service() podService {
	if r.continuation == nil {
		return r.pods
	}
	return continuationService{r.pods, r.continuation}
}
func (r *Run) checkpointLedger() checkpointLedger {
	state := checkpointLedger{LastTick: r.ledger.lastTick, Records: make([]checkpointRecord, len(r.ledger.records)), Lots: make([]checkpointLot, len(r.ledger.lots))}
	for i, record := range r.ledger.records {
		duration := int64(-1)
		if record.DoorToDoorTicks != nil {
			duration = *record.DoorToDoorTicks
		}
		state.Records[i] = checkpointRecord{record.Stage, record.Outcome, record.Held, record.CarArrivalTick, r.checkpointLeg(record.Outward), r.checkpointLeg(record.Return), record.ReturnEligibleTick, record.CarReleaseTick, record.HomeArrivalTick, duration}
	}
	for i, lot := range r.ledger.lots {
		state.Lots[i] = checkpointLot{lot.Occupancy, lot.Peak}
	}
	return state
}
func (r *Run) checkpointLeg(leg Leg) checkpointLeg {
	boarded := int64(-1)
	if value, ok := r.continuation.boarded[leg.RequestID]; ok {
		boarded = value
	}
	return checkpointLeg{leg.RequestID, leg.OfferedTick, boarded, leg.AlightedTick, leg.Reason}
}
func (r *Run) captureBoundary(ctx context.Context) error {
	c := r.continuation
	if c == nil {
		return nil
	}
	if c.invalid != nil {
		return c.invalid
	}
	nativeHash, err := hashBounded(ctx, r.pods.ExportState(), MaxCheckpointBytes)
	if err == nil {
		c.ledgerHash, err = hashBounded(ctx, r.checkpointLedger(), MaxCheckpointBytes)
	}
	if err == nil {
		c.observationHash, err = r.observationHash(ctx)
	}
	if err != nil {
		c.invalid = fmt.Errorf("continuation capture at tick %d: %w", r.pods.Tick(), err)
		return c.invalid
	}
	c.nativeHash = nativeHash
	clock := r.pods.Tick()
	if clock < 0 || clock > MaxHorizonTicks {
		return errors.New("continuation clock is outside its bound")
	}
	var tick [8]byte
	binary.BigEndian.PutUint64(tick[:], uint64(clock))
	chain := append(c.trace[:len(c.trace):len(c.trace)], tick[:]...)
	for _, hash := range []string{c.nativeHash, c.ledgerHash, c.observationHash} {
		chain = append(chain, decodeHash(hash)...)
	}
	c.trace = sha256.Sum256(chain)
	return nil
}
func decodeHash(text string) []byte { value, _ := hex.DecodeString(text); return value }
func (r *Run) observationHash(ctx context.Context) (string, error) {
	observation := struct {
		Snapshot sim.Snapshot `json:"snapshot"`
		Policies Policies     `json:"policies"`
		Report   Report       `json:"report"`
	}{r.pods.Snapshot(), r.provenance.Policies, r.Report()}
	return hashBounded(ctx, observation, MaxCheckpointBytes)
}

type boundedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *boundedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, errors.New("continuation encoding exceeds byte bound")
	}
	n, err := w.writer.Write(data)
	w.remaining -= int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}

type contextWriter struct {
	check  func() error
	writer io.Writer
}

func (w contextWriter) Write(data []byte) (int, error) {
	if err := w.check(); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}
func hashBounded(ctx context.Context, value any, limit int64) (string, error) {
	digest := sha256.New()
	writer := &boundedWriter{writer: contextWriter{ctx.Err, digest}, remaining: limit}
	if err := json.MarshalWrite(writer, value, json.Deterministic(true)); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), ctx.Err()
}

// EncodeCheckpoint writes an owned stable checkpoint with cancellation.
// Ordinary runs cannot upgrade to a continuation lineage.
func (r *Run) EncodeCheckpoint(ctx context.Context, writer io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c := r.continuation
	if c == nil {
		return errors.New("run has no version-1 continuation lineage")
	}
	if r.fault != "" || c.invalid != nil || r.pods.Tick() != r.ledger.lastTick {
		return errors.New("faulted or partial run cannot produce a checkpoint")
	}
	payload := checkpointPayload{c.runID, c.origin, r.pods.Tick(), "post-tick", nativeEncoding, r.pods.ExportState(), c.nativeHash, r.checkpointLedger(), c.ledgerHash, c.observationHash, hex.EncodeToString(c.trace[:])}
	if err := validatePayload(payload); err != nil {
		return err
	}
	if err := checkCurrentHashes(ctx, r, payload); err != nil {
		return err
	}
	if err := checkByteBudget(ctx, payload); err != nil {
		return err
	}
	id, err := hashBounded(ctx, payload, MaxCheckpointBytes)
	if err != nil {
		return err
	}
	file := checkpointFile{checkpointFormat, 1, id, payload}
	return json.MarshalWrite(&boundedWriter{contextWriter{ctx.Err, writer}, MaxCheckpointBytes}, file, json.Deterministic(true))
}
func checkCurrentHashes(ctx context.Context, r *Run, payload checkpointPayload) error {
	for _, pair := range []struct {
		value any
		hash  string
	}{{payload.Native, payload.NativeHash}, {payload.Ledger, payload.LedgerHash}} {
		hash, err := hashBounded(ctx, pair.value, MaxCheckpointBytes)
		if err != nil {
			return err
		}
		if hash != pair.hash {
			return errors.New("joint checkpoint snapshot mismatch")
		}
	}
	hash, err := r.observationHash(ctx)
	if err != nil {
		return err
	}
	if hash != payload.ObservationHash {
		return errors.New("checkpoint observable mismatch")
	}
	return nil
}
func checkByteBudget(ctx context.Context, payload checkpointPayload) error {
	sizes := int64(8192 + 1024*len(payload.Ledger.Records) + 128*len(payload.Ledger.Lots))
	for i, value := range []any{payload.Origin.Project, payload.Origin.Plan, payload.Native} {
		counter := &boundedWriter{io.Discard, MaxCheckpointBytes}
		if err := json.MarshalWrite(contextWriter{ctx.Err, counter}, value, json.Deterministic(true)); err != nil {
			return err
		}
		size := int64(MaxCheckpointBytes) - counter.remaining
		if i < 2 && size > project.MaxFileBytes {
			return errors.New("canonical origin exceeds 10 MiB")
		}
		sizes += size
	}
	if sizes > MaxCheckpointBytes {
		return errors.New("joint checkpoint exceeds conservative byte budget")
	}
	return nil
}
