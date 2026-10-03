package parkride

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func testImplementation() Implementation {
	return Implementation{strings.Repeat("a", 40), strings.Repeat("b", 64), "go1.27.1", "jsonv2", "linux", "amd64"}
}
func continuationInput() RunInput {
	input := runInput()
	identity := testImplementation()
	input.Continuation = &identity
	input.Plan.Itineraries[0].DepartureSeconds = 1
	input.Plan.Itineraries[0].OutwardSeconds = 1
	input.Plan.Itineraries[0].ActivitySeconds = 1
	input.Plan.Itineraries[0].RetrievalSeconds = 1
	input.Plan.Itineraries[0].HomeboundSeconds = 1
	return input
}
func encodeCheckpoint(t *testing.T, r *Run) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := r.EncodeCheckpoint(t.Context(), &encoded); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}
func restoreCheckpoint(t *testing.T, data []byte) *Run {
	t.Helper()
	r, err := DecodeCheckpoint(t.Context(), bytes.NewReader(data), ResumeInput{Implementation: testImplementation()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func assertJointEqual(t *testing.T, a, b *Run) {
	t.Helper()
	if !reflect.DeepEqual(a.pods.ExportState(), b.pods.ExportState()) || !reflect.DeepEqual(a.pods.Snapshot(), b.pods.Snapshot()) || !reflect.DeepEqual(a.Report(), b.Report()) || !reflect.DeepEqual(a.checkpointLedger(), b.checkpointLedger()) || a.continuation.trace != b.continuation.trace {
		t.Fatalf("joint continuation differs at tick %d", a.pods.Tick())
	}
}
func TestCheckpointLifecycleAndFuture(t *testing.T) {
	t.Parallel()
	input := continuationInput()
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryInput := input
	ordinaryInput.Continuation = nil
	ordinary, err := NewRun(ordinaryInput)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for {
		stage := run.ledger.records[0].Stage
		if !seen[stage] {
			seen[stage] = true
			data := encodeCheckpoint(t, run)
			restored := restoreCheckpoint(t, data)
			assertJointEqual(t, run, restored)
			clone := run.Clone()
			for range 30 {
				if clone.Done() {
					break
				}
				if err := clone.Step(); err != nil {
					t.Fatal(err)
				}
				if err := restored.Step(); err != nil {
					t.Fatal(err)
				}
				assertJointEqual(t, clone, restored)
			}
		}
		if !reflect.DeepEqual(run.Report(), ordinary.Report()) || !reflect.DeepEqual(run.pods.Snapshot(), ordinary.pods.Snapshot()) {
			t.Fatal("checkpoint tracing changed ordinary execution")
		}
		if run.Done() {
			break
		}
		if err := run.Step(); err != nil {
			t.Fatal(err)
		}
		if err := ordinary.Step(); err != nil {
			t.Fatal(err)
		}
	}
	for _, stage := range []string{"planned", "car-out", "outward-pod", "activity", "return-pod", "retrieval", "car-home", "terminal"} {
		if !seen[stage] {
			t.Fatal("missing lifecycle checkpoint", stage)
		}
	}
	if run.Report().Itineraries[0].Outcome != "completed" {
		t.Fatal("round trip did not complete")
	}
}
func TestCheckpointTickZeroAndEndpoint(t *testing.T) {
	t.Parallel()
	input := runInput()
	identity := testImplementation()
	input.Continuation = &identity
	r, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	if leg := r.checkpointLedger().Records[0].Outward; leg.RequestID <= 0 || leg.BoardedTick != 0 {
		t.Fatalf("tick-zero boarding lost: %+v", leg)
	}
	restored := restoreCheckpoint(t, encodeCheckpoint(t, r))
	assertJointEqual(t, r, restored)
	input.Plan.Itineraries[0].DepartureSeconds = 1
	input.HorizonTicks = 60
	boundary, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	finishRun(t, boundary)
	record := boundary.Report().Itineraries[0]
	if !record.Held || record.Outward.OfferedTick != -1 || record.Outcome != "censored" {
		t.Fatal("endpoint offer or held-slot violation")
	}
	restored = restoreCheckpoint(t, encodeCheckpoint(t, boundary))
	assertJointEqual(t, boundary, restored)
	if !restored.Done() {
		t.Fatal("endpoint extended its horizon")
	}
	if err := restored.Step(); err != nil {
		t.Fatal(err)
	}
	assertJointEqual(t, boundary, restored)
}
func TestCheckpointRefusalsAndConsent(t *testing.T) {
	t.Parallel()
	for _, consent := range []sim.SharingConsent{sim.PrivateConsent, sim.SharedConsent} {
		t.Run(string(consent), func(t *testing.T) {
			t.Parallel()
			input := continuationInput()
			input.QueueLimit = 1
			input.Project.Fleet = input.Project.Fleet[:1]
			input.Plan.Lots[0].Capacity = 3
			base := input.Plan.Itineraries[0]
			base.DepartureSeconds = 0
			base.OutwardSeconds = 0
			base.SharingConsent = consent
			input.Plan.Itineraries = []Itinerary{base, base, base}
			for i, id := range []string{"a", "b", "c"} {
				input.Plan.Itineraries[i].ID = id
				input.Plan.Itineraries[i].CarID = id
			}
			r, err := NewRun(input)
			if err != nil {
				t.Fatal(err)
			}
			fresh := restoreCheckpoint(t, encodeCheckpoint(t, r))
			finishRun(t, r)
			finishRun(t, fresh)
			assertJointEqual(t, r, fresh)
			outcomes := make(map[string]bool)
			for _, record := range r.Report().Itineraries {
				outcomes[record.Outcome] = true
				if record.Itinerary.SharingConsent != consent {
					t.Fatal("consent changed")
				}
			}
			if !outcomes["recovered-refusal"] {
				t.Fatal("outward refusal not exercised", outcomes)
			}
			restoreCheckpoint(t, encodeCheckpoint(t, r))
		})
	}
}
func rehashCheckpoint(t *testing.T, file checkpointFile) []byte {
	t.Helper()
	file.Payload.NativeHash, _ = canonicalHash(file.Payload.Native)
	file.Payload.LedgerHash, _ = canonicalHash(file.Payload.Ledger)
	file.CheckpointID, _ = canonicalHash(file.Payload)
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestCheckpointIndependentAcceptanceGuards(t *testing.T) {
	t.Parallel()
	r, err := NewRun(continuationInput())
	if err != nil {
		t.Fatal(err)
	}
	data := encodeCheckpoint(t, r)
	for _, tc := range []struct {
		name, reason string
		mutate       func(*checkpointFile)
	}{
		{"native metric", "replayed native snapshot mismatch", func(f *checkpointFile) { f.Payload.Native.EmptyDistanceMeters++ }},
		{"car derived time", "replayed car ledger mismatch", func(f *checkpointFile) { f.Payload.Ledger.Records[0].ReturnEligibleTick = 90 }},
		{"observation", "replayed observation mismatch", func(f *checkpointFile) { f.Payload.ObservationHash = strings.Repeat("c", 64) }},
		{"trace", "replayed per-tick trace mismatch", func(f *checkpointFile) { f.Payload.TraceHash = strings.Repeat("c", 64) }},
		{"executable", "checkpoint executable identity mismatch", func(f *checkpointFile) { f.Payload.Origin.Implementation.ExecutableSHA256 = strings.Repeat("d", 64) }},
		{"held slot", "held-slot conservation", func(f *checkpointFile) { f.Payload.Ledger.Records[0].Held = true }},
		{"joint clock", "joint clock", func(f *checkpointFile) { f.Payload.Native.Tick++ }},
		{"future project", "foundation project", func(f *checkpointFile) { f.Payload.Origin.Project.Version = 4 }},
		{"censored outcome", "invalid checkpoint car stage", func(f *checkpointFile) { f.Payload.Ledger.Records[0].Outcome = "censored" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var file checkpointFile
			if err := json.Unmarshal(data, &file); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&file)
			candidate, err := DecodeCheckpoint(t.Context(), bytes.NewReader(rehashCheckpoint(t, file)), ResumeInput{Implementation: testImplementation()})
			if candidate != nil || err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("guard %s: candidate %v error %v", tc.name, candidate, err)
			}
		})
	}
}
func TestCheckpointParserAndLineage(t *testing.T) {
	t.Parallel()
	r, err := NewRun(continuationInput())
	if err != nil {
		t.Fatal(err)
	}
	data := encodeCheckpoint(t, r)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"missing", bytes.Replace(data, []byte(`"phase":"post-tick",`), nil, 1)},
		{"null", bytes.Replace(data, []byte(`"tick":0`), []byte(`"tick":null`), 1)},
		{"wrong scalar", bytes.Replace(data, []byte(`"tick":0`), []byte(`"tick":"0"`), 1)},
		{"escaped duplicate", append([]byte(`{"\u0066ormat":"x",`), data[1:]...)},
		{"unknown observation", append([]byte(`{"observation":{},`), data[1:]...)},
		{"future order contract", bytes.Replace(data, []byte(`"native":{`), []byte(`"native":{"orderContract":null,`), 1)},
		{"invalid utf8", append([]byte{'{', '"', 0xff, '"', ':', '0', ','}, data[1:]...)},
		{"trailing", append(slicesClone(data), []byte(` {}`)...)},
		{"deep", []byte(strings.Repeat("[", 65) + strings.Repeat("]", 65))},
		{"oversize", []byte(strings.Repeat(" ", MaxCheckpointBytes+1))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidate, decodeErr := DecodeCheckpoint(t.Context(), bytes.NewReader(tc.data), ResumeInput{Implementation: testImplementation()})
			if decodeErr == nil || candidate != nil {
				t.Fatal("invalid parser input accepted")
			}
		})
	}
	ordinary, err := NewRun(runInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := ordinary.EncodeCheckpoint(t.Context(), io.Discard); err == nil {
		t.Fatal("old run upgraded")
	}
	for _, bad := range []Implementation{{}, {strings.Repeat("a", 40), strings.Repeat("b", 64), "go1.27.1", "", "linux", "amd64"}} {
		input := runInput()
		input.Continuation = &bad
		if run, err := NewRun(input); run != nil || err == nil {
			t.Fatal("unidentified lineage accepted")
		}
	}
	r.pods.SetPaused(true)
	if err := r.Step(); err == nil {
		t.Fatal("paused native failure missed")
	}
	if err := r.EncodeCheckpoint(t.Context(), io.Discard); err == nil {
		t.Fatal("fault checkpoint accepted")
	}
}
func slicesClone(data []byte) []byte { return append([]byte(nil), data...) }
func TestCheckpointCancellationAndWriteFailure(t *testing.T) {
	t.Parallel()
	input := continuationInput()
	r, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	for range 61 {
		if stepErr := r.Step(); stepErr != nil {
			t.Fatal(stepErr)
		}
	}
	data := encodeCheckpoint(t, r)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	candidate, err := DecodeCheckpoint(ctx, bytes.NewReader(data), ResumeInput{Implementation: testImplementation(), Progress: func(ResumeProgress) { cancel() }})
	if candidate != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation accepted candidate: %v", err)
	}
	if err := r.EncodeCheckpoint(ctx, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled encode accepted", err)
	}
	if err := r.EncodeCheckpoint(t.Context(), shortCheckpointWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("short write accepted", err)
	}
}

type shortCheckpointWriter struct{}

func (shortCheckpointWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }
func TestCheckpointNamedEncodingCharges(t *testing.T) {
	t.Parallel()
	wide := int64(math.MaxInt64)
	leg := checkpointLeg{math.MaxInt, wide, wide, wide, "queue-limit"}
	record := checkpointRecord{"outward-pod", "recovered-refusal", false, wide, leg, leg, wide, wide, wide, wide}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 1024 {
		t.Fatal("record charge exceeded", len(data))
	}
	recordBytes := len(data)
	data, err = json.Marshal(checkpointLot{wide, wide})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 128 {
		t.Fatal("lot charge exceeded", len(data))
	}
	t.Logf("widest named record=%d, lot=%d", recordBytes, len(data))
	input := continuationInput()
	input.Build = strings.Repeat("\x00", 256)
	identity := *input.Continuation
	identity.GoVersion = strings.Repeat("\x00", 128)
	identity.GoOS, identity.GoArch = strings.Repeat("a", 32), strings.Repeat("b", 32)
	input.Continuation = &identity
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodeCheckpoint(t, run)
	var file checkpointFile
	if decodeErr := json.Unmarshal(encoded, &file); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	components := []any{file.Payload.Origin.Project, file.Payload.Origin.Plan, file.Payload.Native}
	for _, value := range file.Payload.Ledger.Records {
		components = append(components, value)
	}
	for _, value := range file.Payload.Ledger.Lots {
		components = append(components, value)
	}
	envelopeBytes := len(encoded)
	for _, value := range components {
		component, marshalErr := json.Marshal(value, json.Deterministic(true))
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		envelopeBytes -= len(component)
	}
	if envelopeBytes > 8192 {
		t.Fatal("actual checkpoint envelope charge exceeded", envelopeBytes)
	}
	restored, err := DecodeCheckpoint(t.Context(), bytes.NewReader(encoded), ResumeInput{Implementation: identity})
	if err != nil {
		t.Fatal(err)
	}
	assertJointEqual(t, run, restored)
	t.Logf("actual encoder envelope with widest escaped metadata=%d bytes", envelopeBytes)
}
func FuzzCheckpointBoundedDecode(f *testing.F) {
	f.Add([]byte(`{"format":"podsim-car-continuation","version":1}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"payload":{"native":{"orderContract":null}}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			t.Skip()
		}
		_, _ = DecodeCheckpoint(t.Context(), bytes.NewReader(data), ResumeInput{Implementation: testImplementation()})
	})
}

func TestCheckpointTerminalCarOutcomes(t *testing.T) {
	t.Parallel()
	input := continuationInput()
	input.Plan.Lots[0].Capacity = 1
	base := input.Plan.Itineraries[0]
	base.DepartureSeconds, base.OutwardSeconds = 0, 0
	second := base
	second.ID, second.CarID = "b", "b"
	input.Plan.Itineraries = []Itinerary{base, second}
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	finishRun(t, run)
	full := restoreCheckpoint(t, encodeCheckpoint(t, run))
	assertJointEqual(t, run, full)
	if full.Report().Itineraries[1].Outcome != "full-lot" {
		t.Fatal("full-lot outcome missing")
	}

	input = continuationInput()
	input.Project.Fleet = input.Project.Fleet[:1]
	input.Plan.Itineraries[0].DepartureSeconds, input.Plan.Itineraries[0].OutwardSeconds = 0, 0
	input.Plan.Itineraries[0].ActivitySeconds = 10
	probe, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	for probe.ledger.records[0].Outward.AlightedTick < 0 && !probe.Done() {
		if stepErr := probe.Step(); stepErr != nil {
			t.Fatal(stepErr)
		}
	}
	arrival := probe.ledger.records[0].Outward.AlightedTick
	if arrival < 0 {
		t.Fatal("probe never unloaded")
	}
	input.QueueLimit = 1
	input.Plan.Lots[0].Capacity = 3
	base = input.Plan.Itineraries[0]
	second = base
	second.ID, second.CarID = "b", "b"
	third := base
	third.ID, third.CarID = "c", "c"
	third.DepartureSeconds = arrival/sim.TicksPerSecond + 1
	input.Plan.Itineraries = []Itinerary{base, second, third}
	run, err = NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	finishRun(t, run)
	stranded := restoreCheckpoint(t, encodeCheckpoint(t, run))
	assertJointEqual(t, run, stranded)
	record := stranded.Report().Itineraries[0]
	if record.Outcome != "stranded" || !record.Held || record.Return.Reason != "queue-limit" || stranded.Report().Lots[0].Occupancy < 1 {
		t.Fatalf("stranded car not retained: %+v", record)
	}
}

func TestCheckpointOccupiedPickupReceipts(t *testing.T) {
	t.Parallel()
	input := continuationInput()
	input.Project.Version = 3
	input.Project.Fleet = input.Project.Fleet[:1]
	input.Project.SharedRidePartyLimit = 4
	input.Project.OnboardPickups = true
	input.HorizonTicks = 300 * sim.TicksPerSecond
	input.Plan.Lots = []Lot{{ID: "harbor-cars", Hub: "harbor", Capacity: 2}, {ID: "garden-cars", Hub: "garden", Capacity: 1}}
	base := input.Plan.Itineraries[0]
	base.DepartureSeconds, base.OutwardSeconds = 0, 0
	base.ActivitySeconds = 600
	base.SharingConsent = sim.SharedConsent
	input.Plan.Itineraries = []Itinerary{base, base, base}
	for i, id := range []string{"a", "b", "c"} {
		input.Plan.Itineraries[i].ID = id
		input.Plan.Itineraries[i].CarID = id
		input.Plan.Itineraries[i].Lot = "harbor-cars"
	}
	input.Plan.Itineraries[1].Destination = "garden"
	input.Plan.Itineraries[2].Lot = "garden-cars"
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	for !run.Done() {
		state := run.pods.ExportState()
		for _, pod := range state.Pods {
			for i, boarding := range pod.Boardings {
				if boarding.MetersAtBoarding <= 0 || pod.Riders[i].Completed {
					continue
				}
				rider := pod.Riders[i]
				if rider.From != "garden" || rider.ID != run.ledger.records[2].Outward.RequestID || run.checkpointLedger().Records[2].Outward.BoardedTick != rider.BoardedTick {
					t.Fatal("occupied native boarding receipt lost")
				}
				restored := restoreCheckpoint(t, encodeCheckpoint(t, run))
				clone := run.Clone()
				finishRun(t, run)
				finishRun(t, restored)
				finishRun(t, clone)
				assertJointEqual(t, run, restored)
				assertJointEqual(t, run, clone)
				if run.Report().Pods.Completed != 3 {
					t.Fatal("occupied parties did not all unload")
				}
				return
			}
		}
		if stepErr := run.Step(); stepErr != nil {
			t.Fatal(stepErr)
		}
	}
	t.Fatal("authored finite plan did not exercise occupied pickup")
}

// This origin uses the slow entry and second berths of the native buffer fixture.
// Every request is authored through the car plan; no native state is installed.
func TestCheckpointCompactQueueFuture(t *testing.T) {
	t.Parallel()
	input := continuationInput()
	input.Project.Version = 3
	input.Project.Redistribution = false
	input.Project.StationBuffers = true
	input.Project.StationQueueSpacing = sim.StationQueueCompactV1
	input.Project.PlatoonLimit = 4
	network := &input.Project.Network
	for i := range network.Nodes {
		network.Nodes[i].Position.X *= 8
		network.Nodes[i].Position.Y *= 8
	}
	for i := range network.Lanes {
		network.Lanes[i].SpeedLimit = 2.5
		if network.Lanes[i].ID == "market-approach" {
			network.Lanes[i].StationRole = sim.StationEntryRole
		}
	}
	for i := range network.Stations {
		station := &network.Stations[i]
		if station.ID != "harbor" && station.ID != "garden" {
			continue
		}
		node, _ := network.Node(station.Berths[0].Node)
		node.ID = station.ID + "-berth-2"
		node.Position.Y -= 400
		network.Nodes = append(network.Nodes, node)
		station.Berths = append(station.Berths, sim.Berth{ID: station.ID + "-2", Node: node.ID})
		network.Lanes = append(network.Lanes,
			sim.Lane{ID: station.ID + "-in-2", From: station.Entry, To: node.ID, SpeedLimit: 2.5, StationID: station.ID, StationRole: sim.StationBerthAccessRole},
			sim.Lane{ID: station.ID + "-out-2", From: node.ID, To: station.Exit, SpeedLimit: 2.5, StationID: station.ID, StationRole: sim.StationDepartureRole})
	}
	input.Project.Fleet = []sim.Placement{
		{ID: "01", StationID: "harbor", BerthID: "harbor-1"},
		{ID: "02", StationID: "garden", BerthID: "garden-1"},
		{ID: "03", StationID: "harbor", BerthID: "harbor-2"},
		{ID: "04", StationID: "garden", BerthID: "garden-2"},
		{ID: "05", StationID: "market", BerthID: "market-1"},
	}
	input.HorizonTicks = 1200 * sim.TicksPerSecond
	input.Plan.Lots = []Lot{{ID: "harbor-cars", Hub: "harbor", Capacity: 2}, {ID: "garden-cars", Hub: "garden", Capacity: 2}}
	base := input.Plan.Itineraries[0]
	base.DepartureSeconds, base.OutwardSeconds = 0, 0
	base.ActivitySeconds = 1200
	input.Plan.Itineraries = make([]Itinerary, 4)
	for i := range input.Plan.Itineraries {
		value := base
		value.ID = fmt.Sprintf("party-%02d", i)
		value.CarID = value.ID
		value.Lot = "harbor-cars"
		if i%2 == 1 {
			value.Lot = "garden-cars"
		}
		input.Plan.Itineraries[i] = value
	}
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	for !run.Done() {
		for _, pod := range run.pods.ExportState().Pods {
			if pod.CompactQueue == nil {
				continue
			}
			t.Logf("compact certificate at tick %d", run.pods.Tick())
			restored := restoreCheckpoint(t, encodeCheckpoint(t, run))
			clone := run.Clone()
			for range 1200 {
				if clone.Done() {
					break
				}
				if stepErr := clone.Step(); stepErr != nil {
					t.Fatal(stepErr)
				}
				if stepErr := restored.Step(); stepErr != nil {
					t.Fatal(stepErr)
				}
				assertJointEqual(t, clone, restored)
			}
			return
		}
		if stepErr := run.Step(); stepErr != nil {
			t.Fatal(stepErr)
		}
	}
	t.Fatal("authored native buffer origin did not produce a compact certificate")
}

// This directed loop follows the native positioning and pickup test geometry.
func checkpointPickupLoop() sim.Network {
	var network sim.Network
	node := func(id string, x, y float64) {
		network.Nodes = append(network.Nodes, sim.Node{ID: id, Position: sim.Point{X: x, Y: y}})
	}
	lane := func(id, from, to string) {
		network.Lanes = append(network.Lanes, sim.Lane{ID: id, From: from, To: to, SpeedLimit: 14})
	}
	for i := range 5 {
		id := fmt.Sprintf("s%d", i)
		x := float64(i) * 300
		station := sim.Station{ID: id, Name: id, Entry: id + "-entry", Exit: id + "-exit"}
		node(station.Entry, x, 0)
		node(station.Exit, x+150, 0)
		lane(id+"-through", station.Entry, station.Exit)
		for berth := range 2 {
			berthID := fmt.Sprintf("%s-%d", id, berth+1)
			y := 60.0
			if berth == 1 {
				y = -y
			}
			node(berthID, x+75, y)
			lane(berthID+"-in", station.Entry, berthID)
			lane(berthID+"-out", berthID, station.Exit)
			station.Berths = append(station.Berths, sim.Berth{ID: berthID, Node: berthID})
		}
		network.Stations = append(network.Stations, station)
		if i > 0 {
			lane(fmt.Sprintf("s%d-link", i-1), fmt.Sprintf("s%d-exit", i-1), station.Entry)
		}
	}
	node("return-east", 1500, 400)
	node("return-west", -150, 400)
	lane("return-down", "s4-exit", "return-east")
	lane("return", "return-east", "return-west")
	lane("return-up", "return-west", "s0-entry")
	return network
}
func TestCheckpointPickupCooldownFuture(t *testing.T) {
	t.Parallel()
	input := continuationInput()
	input.Project.Version = 3
	input.Project.Redistribution = false
	input.Project.PickupReassignment = true
	input.Project.Network = checkpointPickupLoop()
	input.Project.Fleet = []sim.Placement{{ID: "01", StationID: "s0", BerthID: "s0-1"}, {ID: "02", StationID: "s2", BerthID: "s2-1"}}
	input.HorizonTicks = 600 * sim.TicksPerSecond
	input.Plan.Lots = []Lot{{ID: "cars-s2", Hub: "s2", Capacity: 1}, {ID: "cars-s4", Hub: "s4", Capacity: 1}}
	base := input.Plan.Itineraries[0]
	base.DepartureSeconds, base.OutwardSeconds = 0, 0
	base.ReturnNotBeforeSeconds = 3600
	first, second := base, base
	first.ID, first.CarID, first.Lot, first.Destination = "a-busy", "car-a", "cars-s2", "s3"
	second.ID, second.CarID, second.Lot, second.Destination = "b-pickup", "car-b", "cars-s4", "s0"
	input.Plan.Itineraries = []Itinerary{first, second}
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	for !run.Done() {
		if run.pods.PickupSwapStats().Transfers+run.pods.PickupSwapStats().Swaps > 0 {
			t.Logf("pickup replacement at tick %d stats=%+v", run.pods.Tick(), run.pods.PickupSwapStats())
			restored := restoreCheckpoint(t, encodeCheckpoint(t, run))
			clone := run.Clone()
			for range 1200 {
				if clone.Done() {
					break
				}
				if stepErr := clone.Step(); stepErr != nil {
					t.Fatal(stepErr)
				}
				if stepErr := restored.Step(); stepErr != nil {
					t.Fatal(stepErr)
				}
				assertJointEqual(t, clone, restored)
				if clone.pods.PickupSwapStats() != restored.pods.PickupSwapStats() {
					t.Fatal("pickup cooldown continuation diverged")
				}
			}
			if restored.pods.PickupSwapStats().CooldownPairs == 0 {
				t.Fatal("actual cooldown guard was not exercised")
			}
			return
		}
		if stepErr := run.Step(); stepErr != nil {
			t.Fatal(stepErr)
		}
	}
	t.Fatalf("authored pickup loop did not replace assignment: %+v", run.pods.PickupSwapStats())
}
