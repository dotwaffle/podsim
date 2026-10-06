package parkride

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

type contextReader struct {
	check  func() error
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

// DecodeCheckpoint constructs and verifies an isolated replay before returning it.
// It never installs saved native state or restores through a degraded tier.
func DecodeCheckpoint(ctx context.Context, reader io.Reader, input ResumeInput) (*Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateImplementation(input.Implementation); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx.Err, reader}, MaxCheckpointBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read checkpoint: %w", err)
	}
	if len(data) > MaxCheckpointBytes {
		return nil, errors.New("checkpoint exceeds 80 MiB")
	}
	if err = scanCheckpoint(ctx, data); err != nil {
		return nil, fmt.Errorf("scan checkpoint: %w", err)
	}
	var file checkpointFile
	projectDecoder := json.UnmarshalFunc(func(raw []byte, target *project.Config) error {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		decoded, decodeErr := project.DecodeCanonicalJSON(raw)
		if decodeErr != nil {
			return decodeErr
		}
		*target = decoded
		return ctx.Err()
	})
	if err = json.Unmarshal(data, &file, json.RejectUnknownMembers(true), json.WithUnmarshalers(projectDecoder)); err != nil {
		return nil, fmt.Errorf("decode checkpoint: %w", err)
	}
	if file.Format != checkpointFormat || file.Version != 1 {
		return nil, errors.New("unsupported car checkpoint format or version")
	}
	if !hexText(file.CheckpointID, 64) {
		return nil, errors.New("invalid checkpoint identity")
	}
	hash, err := hashBounded(ctx, file.Payload, MaxCheckpointBytes)
	if err != nil {
		return nil, err
	}
	if hash != file.CheckpointID {
		return nil, errors.New("checkpoint payload hash mismatch")
	}
	payload := file.Payload
	if payload.Origin.Implementation != input.Implementation {
		return nil, errors.New("checkpoint executable identity mismatch")
	}
	if payloadErr := validatePayload(payload); payloadErr != nil {
		return nil, payloadErr
	}
	if originErr := validateOrigin(ctx, payload); originErr != nil {
		return nil, originErr
	}
	if budgetErr := checkByteBudget(ctx, payload); budgetErr != nil {
		return nil, budgetErr
	}
	for _, pair := range []struct {
		value any
		hash  string
	}{{payload.Native, payload.NativeHash}, {payload.Ledger, payload.LedgerHash}} {
		snapshotHash, snapshotErr := hashBounded(ctx, pair.value, MaxCheckpointBytes)
		if snapshotErr != nil {
			return nil, snapshotErr
		}
		if snapshotHash != pair.hash {
			return nil, errors.New("checkpoint snapshot hash mismatch")
		}
	}
	candidate, err := NewRunContext(ctx, RunInput{Project: payload.Origin.Project, Plan: payload.Origin.Plan, HorizonTicks: payload.Origin.HorizonTicks, QueueLimit: payload.Origin.QueueLimit, Build: payload.Origin.ReportBuild, Continuation: &input.Implementation})
	if err != nil {
		return nil, fmt.Errorf("replay origin: %w", err)
	}
	progress := func() {
		if input.Progress != nil {
			input.Progress(ResumeProgress{candidate.pods.Tick(), payload.Tick, file.CheckpointID})
		}
	}
	progress()
	for candidate.pods.Tick() < payload.Tick {
		if candidate.Done() {
			return nil, errors.New("replay ended before checkpoint tick")
		}
		if err = candidate.StepContext(ctx); err != nil {
			return nil, fmt.Errorf("replay at tick %d: %w", candidate.pods.Tick(), err)
		}
		if candidate.pods.Tick()%(60*sim.TicksPerSecond) == 0 {
			progress()
		}
	}
	progress()
	if !reflect.DeepEqual(candidate.pods.ExportState(), payload.Native) {
		return nil, errors.New("replayed native snapshot mismatch")
	}
	if !reflect.DeepEqual(candidate.checkpointLedger(), payload.Ledger) {
		return nil, errors.New("replayed car ledger mismatch")
	}
	observation, err := candidate.observationHash(ctx)
	if err != nil {
		return nil, err
	}
	if observation != payload.ObservationHash {
		return nil, errors.New("replayed observation mismatch")
	}
	if hex.EncodeToString(candidate.continuation.trace[:]) != payload.TraceHash {
		return nil, errors.New("replayed per-tick trace mismatch")
	}
	if candidate.continuation.runID != payload.RunID || candidate.provenance.ProjectHash != payload.Origin.ProjectHash || candidate.provenance.PlanHash != payload.Origin.PlanHash || candidate.ledger.lastTick != payload.Tick {
		return nil, errors.New("replayed origin or joint clock mismatch")
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	return candidate, nil
}
func validateOrigin(ctx context.Context, payload checkpointPayload) error {
	o := payload.Origin
	if !foundationProject(o.Project) {
		return errFoundationProject
	}
	if err := project.Validate(o.Project); err != nil {
		return fmt.Errorf("checkpoint project: %w", err)
	}
	plan, err := normalizePlan(o.Plan, o.Project.Network)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(plan, o.Plan) {
		return errors.New("checkpoint plan is not normalized")
	}
	for _, pair := range []struct {
		value any
		hash  string
	}{{o.Project, o.ProjectHash}, {o.Plan, o.PlanHash}, {o, payload.RunID}} {
		hash, err := hashBounded(ctx, pair.value, MaxCheckpointBytes)
		if err != nil {
			return err
		}
		if hash != pair.hash {
			return errors.New("checkpoint origin hash mismatch")
		}
	}
	return nil
}
func validatePayload(p checkpointPayload) error {
	if p.Phase != "post-tick" || p.NativeEncoding != nativeEncoding {
		return errors.New("checkpoint boundary or native encoding mismatch")
	}
	o := p.Origin
	if o.HorizonTicks < 1 || o.HorizonTicks > MaxHorizonTicks || o.QueueLimit < 1 || o.QueueLimit > MaxQueueLimit || p.Tick < 0 || p.Tick > o.HorizonTicks || p.Tick != p.Native.Tick || p.Tick != p.Ledger.LastTick {
		return errors.New("invalid checkpoint control or joint clock")
	}
	if err := validateImplementation(o.Implementation); err != nil {
		return err
	}
	for _, hash := range []string{p.RunID, o.ProjectHash, o.PlanHash, p.NativeHash, p.LedgerHash, p.ObservationHash, p.TraceHash} {
		if !hexText(hash, 64) {
			return errors.New("invalid checkpoint hash")
		}
	}
	if len(o.ReportBuild) > 256 {
		return errors.New("checkpoint report build exceeds bound")
	}
	if len(p.Ledger.Records) != len(o.Plan.Itineraries) || len(p.Ledger.Lots) != len(o.Plan.Lots) || !fitsStorage(int64(len(o.Plan.Lots)), int64(len(o.Plan.Itineraries))) {
		return errors.New("checkpoint ledger count mismatch")
	}
	if p.Native.Paused || p.Native.Demo != nil || p.Native.DemoError != "" {
		return errors.New("checkpoint contains paused or demo native state")
	}
	return validateConservation(p)
}
func validateConservation(p checkpointPayload) error {
	scan := newConservationScan(p.Origin.Plan, len(p.Ledger.Lots))
	for i, record := range p.Ledger.Records {
		if err := scan.addRecord(record, p.Origin.Plan.Itineraries[i], p.Tick); err != nil {
			return err
		}
	}
	for i, lot := range p.Ledger.Lots {
		if lot.Occupancy != scan.held[i] || lot.Occupancy < 0 || lot.Occupancy > lot.Peak || lot.Peak > p.Origin.Plan.Lots[i].Capacity {
			return errors.New("checkpoint held-slot conservation mismatch")
		}
	}
	if p.Native.RequestID != len(scan.requests) || p.Native.Completed != scan.completed || p.Native.Boarded < 0 || p.Native.Boarded > p.Native.RequestID {
		return errors.New("checkpoint native order counters mismatch")
	}
	return scan.checkNative(p.Native)
}

// conservationScan holds what the ledger records give: the held slots of
// each lot, the native request of each accepted leg, and the count of
// completed legs. It then checks the native requests against them.
type conservationScan struct {
	plan      Plan
	lots      map[string]int
	held      []int64
	requests  map[int]sim.SavedRequest
	completed int
	retained  map[int]bool
}

func newConservationScan(plan Plan, ledgerLots int) *conservationScan {
	scan := &conservationScan{plan: plan, lots: make(map[string]int, len(plan.Lots)), held: make([]int64, ledgerLots), requests: make(map[int]sim.SavedRequest)}
	for i, lot := range plan.Lots {
		scan.lots[lot.ID] = i
	}
	return scan
}
func (scan *conservationScan) addRecord(record checkpointRecord, itinerary Itinerary, tick int64) error {
	if !slices.Contains([]string{"planned", "car-out", "at-hub", "outward-pod", "activity", "return-pod", "retrieval", "car-home", "terminal"}, record.Stage) || !slices.Contains([]string{"", "full-lot", "stranded", "recovered-refusal", "completed"}, record.Outcome) {
		return errors.New("invalid checkpoint car stage or outcome")
	}
	if (record.Stage == "terminal") != (record.Outcome != "") {
		return errors.New("checkpoint terminal stage mismatch")
	}
	lotIndex, ok := scan.lots[itinerary.Lot]
	if !ok {
		return errors.New("checkpoint itinerary has no lot")
	}
	if record.Held {
		scan.held[lotIndex]++
	}
	if err := scan.addLegs(record, itinerary, scan.plan.Lots[lotIndex].Hub, tick); err != nil {
		return err
	}
	if err := validateCarTicks(record, tick); err != nil {
		return err
	}
	if err := validateOutcome(record); err != nil {
		return err
	}
	if record.CarReleaseTick >= 0 && (record.Held || record.CarReleaseTick < record.CarArrivalTick) {
		return errors.New("checkpoint release contradicts held slot")
	}
	return nil
}

// addLegs checks the outward leg and then the return leg. It binds each
// accepted leg to the native request that it expects. The outward leg goes
// from the hub to the destination, and the return leg goes back.
func (scan *conservationScan) addLegs(record checkpointRecord, itinerary Itinerary, hub string, tick int64) error {
	from, to := hub, itinerary.Destination
	for _, leg := range []checkpointLeg{record.Outward, record.Return} {
		if err := validateLeg(leg, tick); err != nil {
			return err
		}
		if leg.RequestID > 0 {
			if _, exists := scan.requests[leg.RequestID]; exists {
				return errors.New("duplicate checkpoint request binding")
			}
			scan.requests[leg.RequestID] = sim.SavedRequest{ID: leg.RequestID, From: from, To: to, PartySize: itinerary.PartySize, SharingConsent: itinerary.SharingConsent, Service: sim.OnDemandService, RequestedTick: leg.OfferedTick, BoardedTick: leg.BoardedTick, Completed: leg.AlightedTick >= 0}
		}
		if leg.AlightedTick >= 0 {
			scan.completed++
		}
		from, to = to, from
	}
	return nil
}
func validateCarTicks(record checkpointRecord, tick int64) error {
	for _, carTick := range []int64{record.CarArrivalTick, record.ReturnEligibleTick, record.CarReleaseTick, record.HomeArrivalTick, record.DoorToDoorTicks} {
		if carTick < -1 {
			return errors.New("invalid checkpoint car tick")
		}
	}
	if record.CarArrivalTick > tick || record.CarReleaseTick > tick || record.HomeArrivalTick > tick {
		return errors.New("checkpoint car event lies in the future")
	}
	return nil
}

// validateOutcome checks that a terminal record has the receipts of its
// outcome.
func validateOutcome(record checkpointRecord) error {
	switch record.Outcome {
	case "full-lot":
		if record.Held || record.Outward.OfferedTick != -1 || record.Return.OfferedTick != -1 {
			return errors.New("full-lot checkpoint contains an offer or held slot")
		}
	case "stranded":
		if !record.Held || record.Return.Reason != "queue-limit" || record.Outward.AlightedTick < 0 {
			return errors.New("stranded checkpoint lost its held car")
		}
	case "completed":
		if record.Held || record.Outward.AlightedTick < 0 || record.Return.AlightedTick < 0 || record.HomeArrivalTick < 0 || record.DoorToDoorTicks < 0 {
			return errors.New("completed checkpoint lacks whole journey receipts")
		}
	case "recovered-refusal":
		if record.Held || record.Outward.Reason != "queue-limit" || record.HomeArrivalTick < 0 || record.DoorToDoorTicks != -1 {
			return errors.New("recovered refusal checkpoint lacks recovery receipts")
		}
	}
	return nil
}

// checkNative checks the waiting trips and then the pod riders. Each
// native request must match the request that the ledger expects, once, and
// every accepted party that has not completed must be in one of them.
func (scan *conservationScan) checkNative(native sim.SavedState) error {
	scan.retained = make(map[int]bool)
	for _, trip := range native.Waiting {
		if trip.Boarded || trip.Request.Completed {
			return errors.New("checkpoint contains historical requeue")
		}
		if err := scan.retain(trip.Request); err != nil {
			return err
		}
	}
	for _, pod := range native.Pods {
		if pod.Class == sim.ExpressClass {
			return errors.New("checkpoint contains unsupported native class")
		}
		for _, rider := range pod.Riders {
			if err := scan.retain(rider); err != nil {
				return err
			}
		}
	}
	for id, request := range scan.requests {
		if !request.Completed && !scan.retained[id] {
			return errors.New("checkpoint lost an accepted party")
		}
	}
	return nil
}
func (scan *conservationScan) retain(request sim.SavedRequest) error {
	expected, ok := scan.requests[request.ID]
	if !ok || scan.retained[request.ID] || request.ServiceID != "" || request.From != expected.From || request.To != expected.To || request.PartySize != expected.PartySize || request.SharingConsent != expected.SharingConsent || request.Service != expected.Service || request.RequestedTick != expected.RequestedTick || request.Completed != expected.Completed {
		return errors.New("checkpoint immutable native request mismatch")
	}
	if expected.BoardedTick >= 0 && request.BoardedTick != expected.BoardedTick {
		return errors.New("checkpoint boarding receipt mismatch")
	}
	scan.retained[request.ID] = true
	return nil
}
func validateLeg(leg checkpointLeg, tick int64) error {
	if leg.RequestID < 0 || leg.OfferedTick < -1 || leg.OfferedTick > tick || leg.BoardedTick < -1 || leg.BoardedTick > tick || leg.AlightedTick < -1 || leg.AlightedTick > tick {
		return errors.New("invalid checkpoint pod leg tick")
	}
	if leg.RequestID == 0 {
		if leg.BoardedTick != -1 || leg.AlightedTick != -1 {
			return errors.New("unaccepted checkpoint leg has native receipts")
		}
		if leg.OfferedTick == -1 && leg.Reason == "" || leg.OfferedTick >= 0 && leg.Reason == "queue-limit" {
			return nil
		}
		return errors.New("invalid checkpoint offer refusal")
	}
	if leg.OfferedTick < 0 || leg.Reason != "" || leg.BoardedTick >= 0 && leg.BoardedTick < leg.OfferedTick || leg.AlightedTick >= 0 && (leg.BoardedTick < 0 || leg.AlightedTick < leg.BoardedTick) {
		return errors.New("invalid checkpoint accepted leg")
	}
	return nil
}
