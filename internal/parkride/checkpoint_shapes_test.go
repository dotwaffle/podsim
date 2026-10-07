package parkride

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// shapeCounter retains no encoded data. Its limit bounds test work only;
// admission and output still use the unchanged production byte limits.
type shapeCounter struct {
	bytes int64
	limit int64
}

func (w *shapeCounter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.limit-w.bytes {
		return 0, errors.New("shape measurement exceeds its work bound")
	}
	w.bytes += int64(len(p))
	return len(p), nil
}

func shapeBytes(t *testing.T, value any) int64 {
	t.Helper()
	// Eight times the existing ledger storage allowance bounds the largest
	// rejected named-type envelope below. This is not an admission limit.
	w := &shapeCounter{limit: 8 * storageLimit}
	if err := json.MarshalWrite(w, value, json.Deterministic(true)); err != nil {
		t.Fatal(err)
	}
	return w.bytes
}

// shapeID returns an ID of 64 ID characters, which JSON writes as 1 byte
// each. Five suffix digits distinguish the bounded collections in this
// test.
func shapeID(index int) string {
	return fmt.Sprintf("%s%05d", strings.Repeat("z", 59), index)
}

// shapeName is a name of the largest length. JSON writes each quote of it
// as 2 bytes, the widest character of a name.
var shapeName = strings.Repeat(`"`, project.MaxNameLength)

// shapeProject reaches the collection limits with a connected passenger
// loop, authored separate path planes, and an unused directed component.
// It proves initialization
// and encoding, not traffic safety on the unused component.
func shapeProject(t *testing.T) project.Config {
	t.Helper()
	c := project.Default()
	c.Name = shapeName
	c.Network, c.Fleet, c.DemandProfiles = sim.Network{}, nil, nil
	c.Redistribution = false
	node := func(id string, x, y float64) {
		c.Network.Nodes = append(c.Network.Nodes, sim.Node{ID: id, Position: sim.Point{X: x, Y: y}})
	}
	lane := func(id, from, to, station string, role sim.StationLaneRole) {
		c.Network.Lanes = append(c.Network.Lanes, sim.Lane{ID: id, From: from, To: to, SpeedLimit: 14, SeparationGroup: id, StationID: station, StationRole: role})
	}
	// The stations are 160 meters apart, so the loop stays within
	// project.MaxCoordinate.
	for i := range project.MaxStations {
		id := fmt.Sprintf("s%03d", i)
		entry, exit, berth := id+"e", id+"x", id+"b"
		x := float64(i) * 160
		node(entry, x, 0)
		node(exit, x+100, 0)
		node(berth, x+50, 60)
		lane(id+"t", entry, exit, id, sim.StationThroughRole)
		lane(id+"i", entry, berth, id, sim.StationBerthAccessRole)
		lane(id+"o", berth, exit, id, sim.StationDepartureRole)
		if i > 0 {
			lane(id+"n", fmt.Sprintf("s%03dx", i-1), entry, "", "")
		}
		c.Network.Stations = append(c.Network.Stations, sim.Station{ID: id, Name: shapeName, Entry: entry, Exit: exit, Berths: []sim.Berth{{ID: berth, Node: berth, SeparationGroup: berth}}})
		c.Fleet = append(c.Fleet, sim.Placement{ID: shapeID(i), StationID: id, BerthID: berth})
	}
	node("re", project.MaxStations*160, 400)
	node("rw", -150, 400)
	lane("rd", fmt.Sprintf("s%03dx", project.MaxStations-1), "re", "", "")
	lane("rr", "re", "rw", "", "")
	lane("ru", "rw", "s000e", "s000", sim.StationEntryRole)
	// One banked station reaches both the 200-berth and eight-bank limits.
	// Separate gates keep each node below the unchanged 64-lane bound.
	station := &c.Network.Stations[0]
	for i := range c.Network.Lanes {
		if c.Network.Lanes[i].ID == "s001n" {
			c.Network.Lanes[i].StationID, c.Network.Lanes[i].StationRole = station.ID, sim.StationExitRole
		}
	}
	for bank := range sim.MaxStationBanks {
		entry, exit := station.Entry, station.Exit
		y := float64(bank) * 1000
		if bank != 0 {
			entry, exit = fmt.Sprintf("b%de", bank), fmt.Sprintf("b%dx", bank)
			node(entry, 0, y)
			node(exit, 150, y)
			lane(fmt.Sprintf("b%dt", bank), entry, exit, station.ID, sim.StationThroughRole)
			lane(fmt.Sprintf("b%da", bank), "rw", entry, station.ID, sim.StationEntryRole)
			lane(fmt.Sprintf("b%dd", bank), exit, "s001e", station.ID, sim.StationExitRole)
		}
		group := sim.StationBank{ID: shapeID(bank), Entry: entry, Exit: exit}
		for berth := range project.MaxBerths / sim.MaxStationBanks {
			id := "s000b"
			if bank != 0 || berth != 0 {
				id = fmt.Sprintf("b%d-%d", bank, berth)
				node(id, 75, y+60+float64(berth)*30)
				lane(id+"i", entry, id, station.ID, sim.StationBerthAccessRole)
				lane(id+"o", id, exit, station.ID, sim.StationDepartureRole)
				station.Berths = append(station.Berths, sim.Berth{ID: id, Node: id, SeparationGroup: id})
			}
			group.BerthIDs = append(group.BerthIDs, id)
		}
		station.Banks = append(station.Banks, group)
	}
	// The unused nodes are in rows of 200 nodes 30 meters apart. The rows
	// alternate direction, so that each node is 30 meters from the next.
	remaining := project.MaxNodes - len(c.Network.Nodes)
	for i := range remaining {
		column := i % 200
		if i/200%2 == 1 {
			column = 199 - column
		}
		node(fmt.Sprintf("u%d", i), -90000+float64(column)*30, -90000+float64(i/200)*30)
	}
	for i := range remaining - 1 {
		lane(fmt.Sprintf("u%d", i), fmt.Sprintf("u%d", i), fmt.Sprintf("u%d", i+1), "", "")
	}
	for i := 0; len(c.Network.Lanes) < project.MaxLanes; i++ {
		lane(fmt.Sprintf("v%d", i), fmt.Sprintf("u%d", i), fmt.Sprintf("u%d", i+2), "", "")
	}
	for p := range project.MaxProfiles {
		profile := project.DemandProfile{ID: shapeID(p), Name: shapeName}
		for b := range project.MaxBands {
			profile.Bands = append(profile.Bands, project.DemandBand{ID: shapeID(b), Name: shapeName, DurationMinutes: 60})
		}
		for from := range 90 {
			for to := range 90 {
				if from == to {
					continue
				}
				weights := make([]float64, project.MaxBands)
				for i := range weights {
					weights[i] = 1
				}
				profile.Flows = append(profile.Flows, project.DemandFlow{From: fmt.Sprintf("s%03d", from), To: fmt.Sprintf("s%03d", to), Weights: weights})
			}
		}
		c.DemandProfiles = append(c.DemandProfiles, profile)
	}
	// Match the demand reserve in project.Validate. Positive finite weights
	// fill the remaining bytes without an unknown field or opaque padding.
	// A weight is at most sim.MaxCounter. The weight 1.0000000000000002e-6
	// adds 23 bytes, and 10^k adds k bytes for k from 0 to 15.
	widest := project.DemandConfig{PerMinute: 120, Pattern: "rail-arrivals", Seed: math.MaxUint64,
		Destination: shapeID(0), Profile: shapeID(0), Band: shapeID(0), DailyStartMinute: 1439}
	reserve := shapeBytes(t, widest) - shapeBytes(t, c.Demand)
	target := int64(project.MaxFileBytes) - reserve
	extra := target - shapeBytes(t, c)
	if extra < 0 {
		t.Fatal("initial maximum collections exceed the project bound")
	}
	for _, profile := range c.DemandProfiles {
		for _, flow := range profile.Flows {
			for i := range flow.Weights {
				if extra >= 23 {
					flow.Weights[i] = 0.0000010000000000000002
					extra -= 23
					continue
				}
				digits := min(extra, 15)
				flow.Weights[i] = math.Pow10(int(digits))
				extra -= digits
			}
		}
	}
	if extra != 0 || shapeBytes(t, c) != target {
		t.Fatal("weights did not fill the reserved canonical project bound")
	}
	if err := project.Validate(c); err != nil {
		t.Fatal(err)
	}
	return c
}

func shapePlan(t *testing.T) Plan {
	t.Helper()
	lot := Lot{ID: shapeID(0), Hub: "s000", Capacity: math.MaxInt64}
	prototype := Itinerary{ID: "i000000", CarID: "c000000", Lot: lot.ID, Destination: "s001", CarSeats: math.MaxInt64,
		PartySize: 1, SharingConsent: sim.PrivateConsent, DepartureSeconds: 10000, OutwardRefusal: "drive-home", ReturnRefusal: "retain-car"}
	empty := shapeBytes(t, Plan{Lots: []Lot{lot}, Itineraries: []Itinerary{}})
	width := shapeBytes(t, prototype) + 1
	count := int((MaxPlanBytes - empty + 1) / width)
	p := Plan{Lots: []Lot{lot}, Itineraries: make([]Itinerary, count)}
	for i := range p.Itineraries {
		p.Itineraries[i] = prototype
		p.Itineraries[i].ID, p.Itineraries[i].CarID = fmt.Sprintf("i%06d", i), fmt.Sprintf("c%06d", i)
	}
	if size := shapeBytes(t, p); size > MaxPlanBytes || size+width <= MaxPlanBytes {
		t.Fatalf("plan count is not maximal for the frozen field widths: %d bytes", size)
	}
	return p
}

// shapeNative is an encoding envelope only. Its incompatible activities,
// negative times, long routes, and request counters are not an admitted
// simulation, a physical state, or a replay witness.
func shapeNative(waiting int) sim.SavedState {
	const wideFloat = -0.0000010000000000000002
	request := sim.SavedRequest{SharingConsent: sim.SharedConsent, Service: sim.OnDemandService, ServiceID: shapeID(0),
		ID: math.MaxInt, From: shapeID(0), To: shapeID(1), PartySize: math.MaxInt, PodID: shapeID(0), Completed: true,
		RequestedTick: math.MinInt64, BoardedTick: math.MinInt64, DispatchReason: strings.Repeat(`"`, 1024)}
	podRoute, waitingRoute := make([]int, project.MaxLanes+project.MaxNodes), make([]int, project.MaxNodes)
	for i := range podRoute {
		podRoute[i] = project.MaxLanes - 1
	}
	for i := range waitingRoute {
		waitingRoute[i] = project.MaxLanes - 1
	}
	state := sim.SavedState{Tick: math.MinInt64, Paused: true, Completed: math.MaxInt, RequestID: math.MaxInt, Boarded: math.MaxInt,
		TotalWaitTicks: math.MinInt64, MaxWaitTicks: math.MinInt64, NextRedistributionTick: math.MinInt64,
		PassengerDistanceMeters: wideFloat, EmptyDistanceMeters: wideFloat, RebalanceMoves: math.MaxInt, SharedParties: math.MaxInt,
		SharedRidePartyLimit: math.MaxInt, SharedRideMode: sim.DefaultSharedRideMode, SharedRideMaxStops: math.MaxInt,
		SharedRideJoin: sim.DefaultSharedRideJoin, Journeys: math.MaxInt, TotalJourneyTicks: math.MinInt64, MaxJourneyTicks: math.MinInt64,
		RiderDistanceMeters: wideFloat, DirectDistanceMeters: wideFloat, MaxDetourRatio: wideFloat,
		Demo: &sim.SavedDemo{SecondSent: true, FollowupsSent: true}, DemoError: strings.Repeat("\x01", 1024), Pods: make([]sim.SavedPod, project.MaxPods), Waiting: make([]sim.SavedTrip, waiting)}
	for i := range state.Pods {
		pod := sim.SavedPod{ID: shapeID(i), Class: sim.LegacyClass, Activity: "continuing", StationID: shapeID(0), BerthID: shapeID(0),
			Occupied: true, RelocatingTo: shapeID(0), Rebalancing: true, RebalanceAfter: math.MinInt64, PhaseTicks: math.MaxInt,
			Origin: shapeID(0), Destination: shapeID(1), DestinationStation: shapeID(1), RiddenMeters: wideFloat, JourneyOrigin: shapeID(0),
			ClaimsDestination: true, Released: true, Route: podRoute, RouteIndex: math.MaxInt, LaneID: shapeID(0),
			LaneDistance: wideFloat, Distance: wideFloat, Waiting: true, WaitSince: math.MinInt64}
		for range 8 {
			pod.Riders = append(pod.Riders, request)
			pod.Boardings = append(pod.Boardings, sim.RiderBoarding{BerthID: shapeID(0), MetersAtBoarding: wideFloat})
			pod.Stops = append(pod.Stops, shapeID(1))
		}
		pod.Platoon = &sim.SavedPlatoonLink{Leader: shapeID((i + project.MaxPods - 1) % project.MaxPods), Lane: math.MaxInt,
			LeaderLane: math.MaxInt, Lanes: math.MaxInt, Turn: wideFloat, Draining: true}
		state.Pods[i] = pod
	}
	for i := range state.Waiting {
		state.Waiting[i] = sim.SavedTrip{Request: request, Boarded: true, DeferUntil: math.MinInt64, DeferCheck: math.MinInt64, DeferPodID: shapeID(0)}
		// Only existing bound pods can carry waiting route caches. At most
		// MaxPods such records contribute the maximum MaxNodes-value cache here.
		if i < project.MaxPods {
			state.Waiting[i].Route = waitingRoute
			state.Waiting[i].Request.PodID = shapeID(i)
		} else {
			state.Waiting[i].Request.PodID = ""
		}
	}
	return state
}

func shapeLedger(n, lots int) checkpointLedger {
	leg := checkpointLeg{RequestID: math.MinInt, OfferedTick: math.MinInt64, BoardedTick: math.MinInt64, AlightedTick: math.MinInt64, Reason: "queue-limit"}
	row := checkpointRecord{Stage: "outward-pod", Outcome: "recovered-refusal", CarArrivalTick: math.MinInt64, Outward: leg, Return: leg,
		ReturnEligibleTick: math.MinInt64, CarReleaseTick: math.MinInt64, HomeArrivalTick: math.MinInt64, DoorToDoorTicks: math.MinInt64}
	ledger := checkpointLedger{LastTick: math.MinInt64, Records: make([]checkpointRecord, n), Lots: make([]checkpointLot, lots)}
	for i := range ledger.Records {
		ledger.Records[i] = row
	}
	for i := range ledger.Lots {
		ledger.Lots[i] = checkpointLot{Occupancy: math.MinInt64, Peak: math.MinInt64}
	}
	return ledger
}

func TestCheckpointCombinedEncodingShapes(t *testing.T) {
	// This serial byte proof adds no shared-state coverage under -race.
	// The required test:bounds task runs every assertion without
	// instrumentation and without -short.
	if testing.Short() || raceEnabled {
		t.Skip("maximum checkpoint byte proof runs in the required test:bounds task")
	}
	config, plan := shapeProject(t), shapePlan(t)
	identity := testImplementation()
	input := RunInput{Project: config, Plan: plan, HorizonTicks: 600 * sim.TicksPerSecond, QueueLimit: MaxQueueLimit, Build: strings.Repeat("\x01", 256), Continuation: &identity}
	run, err := NewRun(input)
	if err != nil {
		t.Fatal(err)
	}
	public := &shapeCounter{limit: MaxCheckpointBytes}
	if err := run.EncodeCheckpoint(t.Context(), public); err != nil {
		t.Fatal(err)
	}
	payload := checkpointPayload{RunID: run.continuation.runID, Origin: run.continuation.origin, Tick: run.pods.Tick(), Phase: "post-tick", NativeEncoding: nativeEncoding,
		Native: run.pods.ExportState(), NativeHash: run.continuation.nativeHash, Ledger: run.checkpointLedger(), LedgerHash: run.continuation.ledgerHash,
		ObservationHash: run.continuation.observationHash, TraceHash: fmt.Sprintf("%x", run.continuation.trace)}
	file := checkpointFile{Format: checkpointFormat, Version: 1, CheckpointID: strings.Repeat("a", 64), Payload: payload}
	if shapeBytes(t, file) != public.bytes {
		t.Fatal("public checkpoint size differs from its named-type encoding")
	}
	t.Logf("SHAPE public-initial-tick-only bytes=%d project=%d plan=%d native=%d ledger=%d nodes=%d lanes=%d stations=%d pods=%d records=%d lots=%d max-station-berths=200 max-station-banks=8 profiles=8 bands=24 flows-per-profile=%d", public.bytes,
		shapeBytes(t, config), shapeBytes(t, plan), shapeBytes(t, payload.Native), shapeBytes(t, payload.Ledger), len(config.Network.Nodes), len(config.Network.Lanes), len(config.Network.Stations), len(config.Fleet), len(plan.Itineraries), len(plan.Lots), len(config.DemandProfiles[0].Flows))
	// An ID has only ID characters, which JSON writes as 1 byte each, so
	// 64-byte node and lane endpoint IDs fit a 32 MiB project with the
	// collection maxima. The shape keeps short node and lane IDs and fills
	// the project to its byte cap with weights.
	idBytes := shapeBytes(t, shapeID(0))
	allLongIDs := int64(project.MaxNodes+3*project.MaxLanes) * idBytes
	if allLongIDs > project.MaxFileBytes {
		t.Fatal("64-byte node and lane IDs no longer fit the project limit")
	}
	t.Logf("SHAPE all-long-node-lane-ids bytes=%d project-cap=%d", allLongIDs, project.MaxFileBytes)
	// Each berth needs a distinct native node. All stations cannot have
	// 200 berths under the shared 12000-node budget.
	if project.MaxStations*project.MaxBerths <= project.MaxNodes {
		t.Fatal("independent berth maxima unexpectedly fit the node budget")
	}
	flow := project.DemandFlow{From: "a", To: "b", Weights: make([]float64, project.MaxBands)}
	flowLowerBound := shapeBytes(t, flow) * project.MaxProfiles * project.MaxFlows
	if flowLowerBound <= project.MaxFileBytes {
		t.Fatal("independent 24-band flow maxima unexpectedly fit the project byte budget")
	}
	t.Logf("SHAPE impossible-independent-project-maxima berth-nodes=%d node-cap=%d full-band-flow-lower-bound=%d project-cap=%d", project.MaxStations*project.MaxBerths, project.MaxNodes, flowLowerBound, project.MaxFileBytes)
	// A lot is required, so 65536 itinerary rows alone are not admissible.
	if fitsStorage(1, storageLimit/itineraryBytes) {
		t.Fatal("independent storage extrema unexpectedly coexist")
	}
	widths := shapeLedger(1, 1)
	t.Logf("SHAPE signed-envelope-widths record=%d lot=%d signed-integer-fields=13", shapeBytes(t, widths.Records[0]), shapeBytes(t, widths.Lots[0]))
	// Widen the frozen metadata only for the serializer envelopes. These
	// strings do not claim verified executable facts or a replay lineage.
	file.Payload.Tick = math.MinInt64
	file.Payload.Origin.HorizonTicks = MaxHorizonTicks
	file.Payload.Origin.Implementation.GoVersion = strings.Repeat("\x01", 128)
	file.Payload.Origin.Implementation.GoOS = strings.Repeat("o", 32)
	file.Payload.Origin.Implementation.GoArch = strings.Repeat("a", 32)
	for _, counts := range []struct {
		name    string
		n, lots int
	}{
		{"canonical-origin-combined-envelope", len(plan.Itineraries), 1},
		{"independent-itinerary-maximum-envelope", storageLimit / itineraryBytes, 1},
		{"independent-lot-maximum-envelope", 1, storageLimit / lotBytes},
		{"joint-storage-bound-envelope", storageLimit / (2 * itineraryBytes), storageLimit / (2 * lotBytes)},
	} {
		t.Run(counts.name, func(t *testing.T) {
			envelope := file
			waiting := max(0, 2*counts.n-project.MaxPods*8)
			envelope.Payload.Native = shapeNative(waiting)
			envelope.Payload.Ledger = shapeLedger(counts.n, counts.lots)
			if counts.name != "canonical-origin-combined-envelope" {
				envelope.Payload.Origin.Plan = Plan{Lots: make([]Lot, counts.lots), Itineraries: make([]Itinerary, counts.n)}
				for i := range envelope.Payload.Origin.Plan.Lots {
					envelope.Payload.Origin.Plan.Lots[i] = Lot{ID: shapeID(0), Hub: shapeID(0), Capacity: math.MaxInt64}
				}
				for i := range envelope.Payload.Origin.Plan.Itineraries {
					row := plan.Itineraries[0]
					row.ID, row.CarID, row.Lot, row.Destination = shapeID(0), shapeID(1), shapeID(0), shapeID(1)
					row.DepartureSeconds, row.OutwardSeconds, row.ReturnNotBeforeSeconds = math.MaxInt64, math.MaxInt64, math.MaxInt64
					row.ActivitySeconds, row.RetrievalSeconds, row.HomeboundSeconds = math.MaxInt64, math.MaxInt64, math.MaxInt64
					envelope.Payload.Origin.Plan.Itineraries[i] = row
				}
			}
			total := shapeBytes(t, envelope)
			if total <= MaxCheckpointBytes {
				t.Fatal("adversarial combined envelope unexpectedly fits")
			}
			if err := checkByteBudget(t.Context(), envelope.Payload); err == nil {
				t.Fatal("actual checkpoint preflight accepted an oversized shape")
			}
			// The sink's independent work bound exceeds the admission cap,
			// so only the actual production writer enforces the 100 MiB bound.
			sink := &shapeCounter{limit: 8 * storageLimit}
			bounded := &boundedWriter{writer: sink, remaining: MaxCheckpointBytes}
			if err := json.MarshalWrite(bounded, envelope, json.Deterministic(true)); err == nil {
				t.Fatal("actual named checkpoint writer truncated or accepted an oversized shape")
			}
			if sink.bytes > MaxCheckpointBytes || sink.bytes+bounded.remaining != MaxCheckpointBytes {
				t.Fatal("bounded output accounting differs")
			}
			t.Logf("SHAPE encoding-envelope-only bytes=%d project=%d plan=%d native=%d ledger=%d records=%d lots=%d waiting=%d pods=600 pod-route=%d bound-waiting-routes=%dx%d boardings=600x8 platoon-links=600 emitted-before-rejection=%d storage-fits=%t", total,
				shapeBytes(t, config), shapeBytes(t, envelope.Payload.Origin.Plan), shapeBytes(t, envelope.Payload.Native), shapeBytes(t, envelope.Payload.Ledger), counts.n, counts.lots, waiting, project.MaxLanes+project.MaxNodes, min(waiting, project.MaxPods), project.MaxNodes, sink.bytes, fitsStorage(int64(counts.lots), int64(counts.n)))
		})
	}
	// The public caller must propagate output failure even for the valid
	// maximum-collection run. It cannot report successful partial output.
	if err := run.EncodeCheckpoint(t.Context(), shortCheckpointWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("public writer error = %v, want short write", err)
	}
}
