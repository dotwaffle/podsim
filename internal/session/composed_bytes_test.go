package session

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"math"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// composedShape is one shape of the single save and stream family. Its
// markers select the limits of the decoder. A save of a shape with the
// coupling marker has groups coupling groups.
type composedShape struct {
	name    string
	markers contractMarkers
	groups  int
}

// composedSize is the measurement of one composed fixture. BoundBytes is
// RawBytes, except for the HTTP state: there it is RawBytes with the
// topology member at its cap. HeadroomBytes is CapBytes minus BoundBytes.
type composedSize struct {
	Shape         string `json:"shape"`
	Format        string `json:"format"`
	RawBytes      int    `json:"raw_bytes"`
	GzipBytes     int    `json:"gzip_bytes"`
	TopologyBytes int    `json:"topology_bytes,omitempty"`
	BoundBytes    int    `json:"bound_bytes"`
	CapBytes      int    `json:"cap_bytes"`
	HeadroomBytes int    `json:"headroom_bytes"`
}

// composedServiceID is the widest service ID of an order.
var composedServiceID = strings.Repeat("\x03", 64)

// composedCouplingGroups is the largest number of coupling groups in a save,
// a frame or a delta.
const composedCouplingGroups = project.MaxPods / 2

// widestCouplingID returns an ID of 64 control bytes, 6 JSON bytes each. The
// last 2 bytes make the IDs of index differ. index is less than 256.
func widestCouplingID(fill byte, index int) string {
	return strings.Repeat(string([]byte{fill}), 62) + string([]byte{byte(0x10 + index/16), byte(0x10 + index%16)})
}

// widestSavedCouplingGroup returns group index with members front and rear,
// and the widest value of each other member that the decoder accepts.
func widestSavedCouplingGroup(index int, front, rear string) sim.SavedCouplingGroup {
	return sim.SavedCouplingGroup{
		ID: widestCouplingID(0x08, index), Members: [2]string{front, rear}, FormationTick: math.MinInt64,
		CorridorID: widestCouplingID(0x0e, index), AssemblySiteID: widestCouplingID(0x0f, index), SplitSiteID: widestCouplingID(0x10, index),
		Phase: sim.CouplingPhase(strings.Repeat("\x11", 64)), DwellTicks: math.MinInt,
		Progress: sim.SavedCouplingProgress{Leg: math.MinInt, DrainFirstMember: math.MinInt},
	}
}

// composedCouplingSave adds the coupling members to file at their widest.
// The first 2*groups pods become the members of groups coupling groups.
// The save encoder and decoder accept only a member that is a traveling
// compact pod without a queue or a platoon link, on a lane chain from its
// origin berth to its destination berth. So each member pod drops these
// members, and keeps at most the stored riders of a compact pod. The
// project network must be the network of widestSavedBase. The project has
// no coupling sites or corridors: with them the decoder checks the
// geometry of the network, and this network has no valid geometry. The
// project member has its byte cap whatever it contains.
func composedCouplingSave(t *testing.T, file stateFile, groups int) stateFile {
	t.Helper()
	const wide = 0.0000010000000000000002
	contract := sim.CompactPairV1CouplingContract
	file.CouplingContract = contract
	file.Simulation.CouplingContract = contract
	file.Project = project.Clone(file.Project)
	file.Project.CouplingContract, file.Project.CouplingEnabled = contract, true
	network := &file.Project.Network
	nodes, lanes := len(network.Nodes), len(network.Lanes)
	// Lane i goes from node i%nodes to node (i+1)%nodes. The route takes
	// lanes+nodes lanes, the saved route limit. Each lane index has 4
	// digits, as the index lanes-1 of the other pods has.
	route := make([]int, lanes+nodes)
	for step := range route {
		node := step % nodes
		route[step] = node
		if node < lanes-nodes {
			route[step] = nodes + node
		}
	}
	origin, destination := strings.Repeat("\x05", 64), strings.Repeat("\x06", 64)
	network.Stations = append(network.Stations, sim.Station{
		ID: strings.Repeat("\x07", 64), Name: "coupling", Entry: network.Nodes[0].ID, Exit: network.Nodes[len(route)%nodes].ID,
		Berths: []sim.Berth{{ID: origin, Node: network.Nodes[0].ID}, {ID: destination, Node: network.Nodes[len(route)%nodes].ID}},
	})
	current := &network.Lanes[route[len(route)-1]]
	current.ID = strings.Repeat("\x04", 64)
	file.Project.Name = ""
	file.Project.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, file.Project))

	riders := sim.MaxStoredRidersForOrderContract(sim.CompactClass, file.OrderContract)
	file.Simulation.Pods = slices.Clone(file.Simulation.Pods)
	file.Simulation.CouplingGroups = make([]sim.SavedCouplingGroup, groups)
	for i := range file.Simulation.CouplingGroups {
		members := file.Simulation.Pods[2*i : 2*i+2]
		for j := range members {
			pod := &members[j]
			pod.Class, pod.Activity = sim.CompactClass, "traveling"
			pod.Platoon, pod.CompactQueue = nil, nil
			pod.Origin, pod.Destination = origin, destination
			pod.Route, pod.RouteIndex, pod.LaneID = route, len(route)-1, current.ID
			pod.Distance, pod.LaneDistance = wide, wide
			pod.Riders = pod.Riders[:min(len(pod.Riders), riders)]
			if pod.Boardings != nil {
				pod.Boardings = pod.Boardings[:len(pod.Riders)]
			}
		}
		file.Simulation.CouplingGroups[i] = widestSavedCouplingGroup(i, members[0].ID, members[1].ID)
	}
	return file
}

// composedSave returns the composed save of shape. The widest builders
// omit two members that the decoder accepts: sharedRideJoin, and the
// service ID of a plain order. The composed save adds them.
func composedSave(t *testing.T, shape composedShape) stateFile {
	t.Helper()
	var file stateFile
	if shape.markers.order == sim.ExpressOrderContract {
		file = widestExpressSave(t)
	} else {
		base := widestSavedBase(t)
		pod, trip := base.Simulation.Pods[0], base.Simulation.Waiting[0]
		pod.Riders = slices.Clone(pod.Riders)
		for i := range pod.Riders {
			pod.Riders[i].ServiceID = composedServiceID
		}
		trip.Request.ServiceID = composedServiceID
		file = compactWorstCaseFile(base, pod, trip, 1)
	}
	file.Simulation.SharedRideJoin = sim.SharedRideJoin(strings.Repeat("\x01", 1<<10))
	if shape.markers.coupling != "" {
		file = composedCouplingSave(t, file, shape.groups)
	}
	return file
}

// composedCouplingFrame adds the coupling members to frame at their widest.
// Each vehicle is a member of one of composedCouplingGroups groups. The
// stream decoder checks the shape of the members, not their placement.
func composedCouplingFrame(frame StreamFrame) StreamFrame {
	contract := sim.CompactPairV1CouplingContract
	corner := sim.Point{X: -math.MaxFloat64, Y: -math.MaxFloat64}
	rectangle := sim.CouplingRectangle{Corners: [4]sim.Point{corner, corner, corner, corner}}
	speed := -math.MaxFloat64
	simulation := &frame.State.Simulation
	simulation.CouplingContract, simulation.CouplingEnabled = contract, true
	simulation.Vehicles = slices.Clone(simulation.Vehicles)
	simulation.CouplingGroups = make([]sim.CouplingGroupView, composedCouplingGroups)
	for i := range simulation.CouplingGroups {
		front, rear := &simulation.Vehicles[2*i], &simulation.Vehicles[2*i+1]
		group := sim.CouplingGroupView{
			SavedCouplingGroup: widestSavedCouplingGroup(i, front.Pod.ID, rear.Pod.ID),
			Profile:            sim.CouplingContract(strings.Repeat("\x12", 64)), OwnerID: widestCouplingID(0x13, i),
			ResourceClaims: math.MinInt, CommonSpeed: &speed, Bodies: [2]sim.CouplingRectangle{rectangle, rectangle},
			Connector: &rectangle, ManeuverEnvelope: &rectangle,
		}
		simulation.CouplingGroups[i] = group
		front.CouplingID, rear.CouplingID = group.ID, group.ID
	}
	return frame
}

// composedStreamFrame returns the composed frame of shape. The plain
// frame adds the service ID of each order, which the widest builder omits
// and the decoder accepts.
func composedStreamFrame(t *testing.T, shape composedShape) StreamFrame {
	t.Helper()
	var frame StreamFrame
	if shape.markers.order == sim.ExpressOrderContract {
		frame = widestExpressStreamFrame(t)
	} else {
		frame = maximumStreamRepresentation(t, "modern")
		simulation := &frame.State.Simulation
		for i := range simulation.Pending {
			simulation.Pending[i].ServiceID = composedServiceID
		}
		for i := range simulation.Vehicles {
			for j := range simulation.Vehicles[i].Riders {
				simulation.Vehicles[i].Riders[j].ServiceID = composedServiceID
			}
		}
	}
	if shape.markers.coupling != "" {
		frame = composedCouplingFrame(frame)
	}
	return frame
}

// TestComposedWorstCaseFormats measures one composed fixture for each save
// shape: plain, Express, coupling, and Express with coupling. For each
// shape, it also measures the full frame, the delta and the HTTP state of
// one composed frame. Each fixture combines the widest value of each
// member that the decoder accepts. The values are independent maxima, not
// reachable placement or motion. Each fixture must fit its cap, and the
// bounded scan and the decoder must accept it. When
// PODSIM_COMPOSED_FORMATS_RECORD names a file, the test writes the
// measurement record to it.
func TestComposedWorstCaseFormats(t *testing.T) { //nolint:tparallel // Subtests measure one shape at a time to bound memory, in record order.
	t.Parallel()
	if testing.Short() || raceEnabled {
		t.Skip("measurement runs without -short and without the race detector")
	}
	expressCoupling := contractMarkers{order: sim.ExpressOrderContract, coupling: sim.CompactPairV1CouplingContract}
	// Express with coupling has no group at its widest: each member is a
	// compact pod, which keeps 8 of the 20 riders of an Express pod, so
	// each group makes the save smaller.
	shapes := []composedShape{
		{"plain", contractMarkers{}, 0},
		{"express", contractMarkers{order: sim.ExpressOrderContract}, 0},
		{"coupling", contractMarkers{coupling: sim.CompactPairV1CouplingContract}, composedCouplingGroups},
		{"express-coupling", expressCoupling, 0},
	}
	var sizes []composedSize
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			sizes = append(sizes, measureComposedSave(t, shape))
			runtime.GC()
			sizes = append(sizes, measureComposedStream(t, shape)...)
			runtime.GC()
		})
	}
	// The decode path of an Express save with a group. It is smaller than
	// the widest Express with coupling save, so the record leaves it out.
	t.Run("express-coupling-group", func(t *testing.T) {
		measureComposedSave(t, composedShape{"express-coupling-group", expressCoupling, 1})
		runtime.GC()
	})
	if path := os.Getenv("PODSIM_COMPOSED_FORMATS_RECORD"); path != "" {
		record := struct {
			Format string         `json:"format"`
			Test   string         `json:"test"`
			Method string         `json:"method"`
			Sizes  []composedSize `json:"sizes"`
		}{
			Format: "podsim-composed-worst-case-formats-v1",
			Test:   "internal/session TestComposedWorstCaseFormats",
			Method: "Each fixture has the widest value of each member that a server can write and that the version 9 save decoder or the hello 6 stream decoder accepts. " +
				"The values are independent maxima, not reachable placement or motion. " +
				"The decoder accepts a route on each waiting trip, but a server writes at most 300: dispatch gives a waiting trip a route only when it assigns a pod, each pod has at most one assigned trip, and a physical restore keeps waiting routes within its block budget. " +
				"Plain save: widestSavedBase with 300 compact pods, each the leader of a one-pod queue, and 2,600 trips, 300 of them with a route. " +
				"Express save: widestExpressSave, with 300 Express pods that have 20 riders and 20 boarding records, and 8,600 trips. " +
				"Each save also has sharedRideJoin, and each plain order has a service ID. " +
				"Coupling save: the coupling markers and 150 coupling groups; the 300 member pods are traveling compact pods with a route of 13,000 lanes. " +
				"Express with coupling has no group, because a member pod keeps 8 of the 20 riders of an Express pod; the test also decodes it with 1 group. " +
				"The saved project and the HTTP topology have no coupling sites or corridors, because their network has no valid geometry; each member is at its byte cap. " +
				"Streams: plain is maximumStreamFrame with compact vehicles, boarding records and service IDs; Express is widestExpressStreamFrame; coupling adds 150 coupling groups and a coupling ID on each vehicle. " +
				"The delta changes each vehicle, berth and group from an empty base. " +
				"The HTTP state has the widest topology that fits the topology cap of 10,489,856 bytes; bound_bytes sets the topology member to that cap. " +
				"Gzip bytes use gzip level 1, as the server does. " +
				"Saves decode with decodeStateFile and resolveBoardings, and streams with DecodeStreamJSON. " +
				"The HTTP state decodes with the bounded scans and the typed decode of DecodeStateJSON, without the state checks of the stream assembler.",
			Sizes: sizes,
		}
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// composedSaveJSON returns the JSON form of file that the save encoder
// writes, without the size limit of the encoder.
func composedSaveJSON(t *testing.T, file stateFile) []byte {
	t.Helper()
	data, err := jsonv2.Marshal(file, jsonv2.Deterministic(true), jsonv2.WithMarshalers(file.simulationMarshalers()))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// measureComposedSave measures the composed save of shape. A save over the
// cap stops the shape after its size is recorded.
func measureComposedSave(t *testing.T, shape composedShape) composedSize {
	t.Helper()
	file := composedSave(t, shape)
	raw := composedSaveJSON(t, file)
	size := composedSize{Shape: shape.name, Format: "save", RawBytes: len(raw), BoundBytes: len(raw), CapBytes: MaxStateBytes, HeadroomBytes: MaxStateBytes - len(raw)}
	if len(raw) > MaxStateBytes {
		t.Errorf("%s save exceeds the cap: raw=%d cap=%d", shape.name, len(raw), MaxStateBytes)
		return size
	}
	data := encodeTestState(t, file)
	size.GzipBytes = len(data)
	t.Logf("%s save raw=%d gzip=%d cap=%d headroom=%d", shape.name, len(raw), len(data), MaxStateBytes, size.HeadroomBytes)
	if !bytes.Equal(decompressTestJSON(t, data), raw) {
		t.Fatalf("%s save differs from the save encoder", shape.name)
	}
	limits := savedLimits(shape.markers)
	if err := prescanJSON(raw, limits); err != nil {
		t.Fatalf("%s save failed the bounded scan: %v", shape.name, err)
	}
	assertExplicitArrayBounds(t, shape.name+" save", raw, limits)
	decoded, err := decodeStateFile(data)
	if err != nil {
		t.Fatalf("%s save decode: %v", shape.name, err)
	}
	if err := decoded.resolveBoardings(); err != nil {
		t.Fatalf("%s save boardings: %v", shape.name, err)
	}
	orders, _ := shape.markers.orderBounds()
	if len(decoded.Simulation.Pods) != project.MaxPods || int64(len(decoded.Simulation.Waiting)) != orders ||
		len(decoded.Simulation.CouplingGroups) != len(file.Simulation.CouplingGroups) {
		t.Fatalf("%s save lost records", shape.name)
	}
	return size
}

// measureComposedStream measures the full frame, the delta and the HTTP
// state of the composed frame of shape. A document over the cap is
// recorded, and the test does not decode it.
func measureComposedStream(t *testing.T, shape composedShape) []composedSize {
	t.Helper()
	frame := composedStreamFrame(t, shape)
	delta := maximumStreamDelta(t, frame)
	full := StreamEnvelope{
		CouplingContract: shape.markers.coupling, OrderContract: shape.markers.order,
		Kind: "full", Stream: strings.Repeat("x", 32), Sequence: math.MaxUint64, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame,
	}
	changed := full
	changed.Kind, changed.Full, changed.Delta, changed.Base = "delta", nil, &delta, math.MaxUint64-1
	limits := streamLimits(shape.markers)
	orders, _ := shape.markers.orderBounds()
	var sizes []composedSize
	for _, envelope := range []StreamEnvelope{full, changed} {
		raw, err := EncodeStreamJSON(envelope)
		size := composedStreamSize(t, shape, envelope.Kind, raw, 0)
		sizes = append(sizes, size)
		if err != nil {
			t.Errorf("%s %s encode: %v", shape.name, envelope.Kind, err)
			continue
		}
		if scanErr := prescanJSON(raw, limits); scanErr != nil {
			t.Fatalf("%s %s failed the bounded scan: %v", shape.name, envelope.Kind, scanErr)
		}
		assertExplicitArrayBounds(t, shape.name+" "+envelope.Kind, raw, limits)
		decoded, err := DecodeStreamJSON(raw)
		if err != nil {
			t.Fatalf("%s %s decode: %v", shape.name, envelope.Kind, err)
		}
		var vehicles, pending, groups int
		if decoded.Full != nil {
			simulation := decoded.Full.State.Simulation
			vehicles, pending, groups = len(simulation.Vehicles), len(simulation.Pending), len(simulation.CouplingGroups)
		} else {
			var replacement couplingReplacement
			if raw := decoded.Delta.Groups["coupling"]; raw != nil {
				if err := json.Unmarshal(raw, &replacement); err != nil {
					t.Fatal(err)
				}
			}
			var group []json.RawMessage
			if err := json.Unmarshal(decoded.Delta.Groups["pending"], &group); err != nil {
				t.Fatal(err)
			}
			vehicles, pending, groups = len(decoded.Delta.Vehicles), len(group), len(replacement.Groups)
		}
		if vehicles != project.MaxPods || int64(pending) != orders || groups != len(frame.State.Simulation.CouplingGroups) {
			t.Fatalf("%s %s lost records: %d vehicles, %d orders, %d coupling groups", shape.name, envelope.Kind, vehicles, pending, groups)
		}
		runtime.GC()
	}

	// The HTTP state of a widest frame does not pass the state checks of
	// DecodeStateJSON, because its values are independent maxima. The test
	// encodes it with the options of EncodeStateJSON, and it decodes it with
	// the bounded scans and the typed decode of DecodeStateJSON.
	topology, _ := fitWidestTopology(t, shape.markers)
	topologyRaw, err := json.Marshal(topology)
	if err != nil {
		t.Fatal(err)
	}
	envelope := StateEnvelope{CouplingContract: shape.markers.coupling, OrderContract: shape.markers.order, Topology: topology, Frame: frame}
	raw, err := jsonv2.Marshal(envelope, json.DefaultOptionsV1(), packedRequestOptions())
	if err != nil {
		t.Fatal(err)
	}
	sizes = append(sizes, composedStreamSize(t, shape, "http", raw, len(topologyRaw)))
	if len(raw) > MaxStreamJSON {
		return sizes
	}
	if scanErr := prescanJSON(raw, limits); scanErr != nil {
		t.Fatalf("%s HTTP state failed the bounded scan: %v", shape.name, scanErr)
	}
	assertExplicitArrayBounds(t, shape.name+" HTTP state", raw, limits)
	var decoded StateEnvelope
	if err := decodeMarkedJSON(raw, true, &decoded); err != nil {
		t.Fatalf("%s HTTP state decode: %v", shape.name, err)
	}
	if len(decoded.Frame.State.Simulation.Vehicles) != project.MaxPods || int64(len(decoded.Frame.State.Simulation.Pending)) != orders ||
		len(decoded.Frame.State.Simulation.CouplingGroups) != len(frame.State.Simulation.CouplingGroups) {
		t.Fatalf("%s HTTP state lost records", shape.name)
	}
	return sizes
}

// composedStreamSize measures raw, a stream document or an HTTP state of
// shape, and checks it against MaxStreamJSON. topologyBytes is the size of
// the topology member of an HTTP state, or 0.
func composedStreamSize(t *testing.T, shape composedShape, format string, raw []byte, topologyBytes int) composedSize {
	t.Helper()
	size := composedSize{Shape: shape.name, Format: format, RawBytes: len(raw), BoundBytes: len(raw), CapBytes: MaxStreamJSON}
	if topologyBytes != 0 {
		size.TopologyBytes = topologyBytes
		size.BoundBytes += project.MaxFileBytes + 4096 - topologyBytes
	}
	size.HeadroomBytes = MaxStreamJSON - size.BoundBytes
	if size.BoundBytes > MaxStreamJSON {
		t.Errorf("%s %s exceeds the cap: raw=%d bound=%d cap=%d", shape.name, format, len(raw), size.BoundBytes, MaxStreamJSON)
	} else {
		compressed, err := compressStreamJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		inflated, err := InflateStream(compressed)
		if err != nil || !bytes.Equal(inflated, raw) {
			t.Fatalf("%s %s gzip round trip: %v", shape.name, format, err)
		}
		size.GzipBytes = len(compressed)
	}
	t.Logf("%s %s raw=%d gzip=%d bound=%d cap=%d headroom=%d", shape.name, format, size.RawBytes, size.GzipBytes, size.BoundBytes, MaxStreamJSON, size.HeadroomBytes)
	return size
}
