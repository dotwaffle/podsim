package session

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
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
// markers select the limits of the decoder.
type composedShape struct {
	name    string
	markers contractMarkers
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

// composedIncidentAllocation and composedExpressIncidentAllocation are the
// stage 1 save allocations of the incident contract (section 11.7), for a
// shape without and with the Express marker.
const (
	composedIncidentAllocation        = 129_516
	composedExpressIncidentAllocation = 373_116
)

// composedFaultSaveAllocation and composedFaultStreamAllocation are the
// stage 2 allocations of the incident suspension contract (section 13.6):
// the save sub-allocation, and the stream and HTTP allocation.
const (
	composedFaultSaveAllocation   = 65_536
	composedFaultStreamAllocation = 262_144
)

// composedEmergencySaveAllocation and composedEmergencyStreamAllocation
// are the stage 3 allocations of the incident emergency contract (section
// 11.6): the emergency records of the joint save allocation of the
// incident contract, and the stream and HTTP allocation.
const (
	composedEmergencySaveAllocation   = 65_536
	composedEmergencyStreamAllocation = 196_608
)

// widestID returns an ID of project.MaxIDLength characters: fill, and then
// the 5 digits of index. fill is one of sim.IDCharacters, and index is less
// than 100,000. Each encoder of the server writes each ID character as 1
// byte, so each ID of the largest length has the same size.
func widestID(fill byte, index int) string {
	return fmt.Sprintf("%s%05d", strings.Repeat(string([]byte{fill}), project.MaxIDLength-5), index)
}

// widestReason is a dispatch reason of the largest saved length. Order text
// is packed, so its characters do not change its size.
var widestReason = strings.Repeat("r", 1<<10)

// composedServiceID is the widest service ID of an order.
var composedServiceID = widestID('e', 0)

// composedIncidentProject gives config the incident marker and
// project.MaxStations stations, so that a station index has 3 digits. The
// added stations are passenger stations with short IDs. They come first,
// so that the stations of the fixture get the highest indexes. The name
// fills the project member to its byte cap again.
func composedIncidentProject(t *testing.T, config *project.Config) {
	t.Helper()
	*config = project.Clone(*config)
	config.IncidentContract = sim.IncidentV1Contract
	stations := make([]sim.Station, project.MaxStations-len(config.Network.Stations), project.MaxStations)
	for i := range stations {
		stations[i] = sim.Station{ID: fmt.Sprintf("i%03d", i)}
	}
	config.Network.Stations = append(stations, config.Network.Stations...)
	config.Name = ""
	config.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, *config))
	if size := jsonSize(t, *config); size != project.MaxFileBytes {
		t.Fatalf("project has %d bytes, want %d", size, project.MaxFileBytes)
	}
}

// composedLegFrom returns the leg origin with the highest station index
// that the save decoder accepts for request: a passenger station other
// than its destination. A rider with a boarding record boards at its leg
// origin, so its leg origin is its origin.
func composedLegFrom(stations []sim.Station, request sim.SavedRequest, boarded bool) string {
	if boarded {
		return request.From
	}
	for _, station := range slices.Backward(stations) {
		if !station.ParkingOnly && station.ID != request.To {
			return station.ID
		}
	}
	return ""
}

// composedIncidentSave adds the stage 1 members of the incident contract
// to file at the widest values that the save decoder accepts: the
// counters and the serial; holds 3 and the operational tuple [1, 2] on
// each pod; a leg origin with a 3-digit station index on each rider and
// each trip; and an excluded pod at index 299, or 298 for a trip with pod
// 299, on each trip. An interrupt bit needs an active rider, and an active
// rider omits 17 bytes of "completed":true, so no pod has an interrupt
// set. An exclusion needs a trip that did not board (X1), and
// "excludedPod":299 is wider than "boarded":true, so no trip is boarded.
func composedIncidentSave(t *testing.T, file stateFile) stateFile {
	t.Helper()
	composedIncidentProject(t, &file.Project)
	stations := file.Project.Network.Stations
	state := &file.Simulation
	state.Interrupted, state.InterruptedPassengers, state.IncidentSerial = -sim.MaxCounter, -sim.MaxCounter, sim.MaxCounter
	state.Pods = slices.Clone(state.Pods)
	for i := range state.Pods {
		pod := &state.Pods[i]
		pod.Riders = slices.Clone(pod.Riders)
		for j := range pod.Riders {
			pod.Riders[j].LegFrom = composedLegFrom(stations, pod.Riders[j], j < len(pod.Boardings))
		}
		pod.Withdrawn, pod.Purpose, pod.Owner = 3, 1, 2
	}
	last := state.Pods[len(state.Pods)-1].ID
	state.Waiting = slices.Clone(state.Waiting)
	for i := range state.Waiting {
		trip := &state.Waiting[i]
		trip.Boarded, trip.Request.LegFrom, trip.ExcludedPod = false, composedLegFrom(stations, trip.Request, false), last
		if trip.Request.PodID == last || trip.DeferPodID == last {
			trip.ExcludedPod = state.Pods[len(state.Pods)-2].ID
		}
	}
	return file
}

// composedFaultProject gives config the fault marker and the widest
// faults settings, which project.Validate measures for each project with
// the marker. The name fills the project member to its byte cap again.
func composedFaultProject(t *testing.T, config *project.Config) {
	t.Helper()
	*config = project.Clone(*config)
	config.FaultContract = sim.FaultV1Contract
	config.Faults = &project.FaultConfig{
		EvacuationSeconds: new(3600), PerHour: new(math.Copysign(0, -1)),
		DebrisShare: new(1.0000000000000002e-06), DebrisMeters: new(0.5000000000000001),
		Duration: &project.FaultDuration{Kind: "exponential", MinSeconds: new(86400), MaxSeconds: new(86400), MeanSeconds: new(86400)},
	}
	config.Name = ""
	config.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, *config))
	if size := jsonSize(t, *config); size != project.MaxFileBytes {
		t.Fatalf("project has %d bytes, want %d", size, project.MaxFileBytes)
	}
}

// composedFaultSave adds the stage 2 members of the incident suspension
// contract to file at the widest values that a server writes (section
// 13.6): the counters at sim.MaxCounter, and 300 pod records and 64
// debris records. Each record has a 16-digit generation and serial, and
// 16-digit ticks. Each pod record names pod index 299, and each debris
// record the last lane index and segment bounds with 23 bytes. The save
// decoder checks the shape of a tuple, and the restore checks its values.
func composedFaultSave(t *testing.T, file stateFile) stateFile {
	t.Helper()
	composedFaultProject(t, &file.Project)
	lane := len(file.Project.Network.Lanes) - 1
	records := make([]sim.SavedFault, maxFaultRecords)
	for i := range records {
		record := sim.SavedFault{Generation: sim.MaxCounter, Serial: sim.MaxCounter - uint64(len(records)-1-i), Start: sim.MaxCounter, End: sim.MaxCounter, Pod: project.MaxPods - 1}
		if i >= project.MaxPods {
			record.Debris, record.Pod, record.Lane = true, 0, lane
			record.From, record.To = composedDebrisFrom, composedDebrisTo
		}
		records[i] = record
	}
	counters := sim.FaultCounters{Started: sim.MaxCounter, Cleared: sim.MaxCounter, Evacuations: sim.MaxCounter, Reroutes: sim.MaxCounter, FaultWaitTicks: sim.MaxCounter}
	file.Simulation.Faults = &sim.SavedFaults{Records: records, Counters: counters}
	return file
}

// composedEmergencyProject gives config the emergency marker and the
// widest emergencies settings, which project.Validate measures for each
// project with the marker. The name fills the project member to its byte
// cap again.
func composedEmergencyProject(t *testing.T, config *project.Config) {
	t.Helper()
	*config = project.Clone(*config)
	config.EmergencyContract = sim.EmergencyV1Contract
	config.Emergencies = &project.EmergencyConfig{PerHour: new(math.Copysign(0, -1))}
	config.Name = ""
	config.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, *config))
	if size := jsonSize(t, *config); size != project.MaxFileBytes {
		t.Fatalf("project has %d bytes, want %d", size, project.MaxFileBytes)
	}
}

// composedEmergencyRecord returns saved emergency record index of count
// at its widest (section 11.6 of the incident emergency contract): a
// 16-digit generation and serial, a 16-digit start and order, and pod
// index 299. The serials increase with index.
func composedEmergencyRecord(index, count int) sim.SavedEmergency {
	return sim.SavedEmergency{
		Generation: sim.MaxCounter, Serial: sim.MaxCounter - uint64(count-1-index),
		Start: sim.MaxCounter, Pod: project.MaxPods - 1, Order: sim.MaxCounter,
	}
}

// composedEmergencyCounters are the widest emergency counters.
var composedEmergencyCounters = sim.EmergencyCounters{Started: sim.MaxCounter, Ended: sim.MaxCounter, EmergencyTicks: sim.MaxCounter}

// composedEmergencySave adds the stage 3 members of the incident emergency
// contract to file at the widest values that a server writes:
// sim.MaxEmergencies records of composedEmergencyRecord and the counters
// at sim.MaxCounter. The save decoder checks the shape of a tuple,
// and the restore checks its values.
func composedEmergencySave(t *testing.T, file stateFile) stateFile {
	t.Helper()
	composedEmergencyProject(t, &file.Project)
	records := make([]sim.SavedEmergency, sim.MaxEmergencies)
	for i := range records {
		records[i] = composedEmergencyRecord(i, len(records))
	}
	file.Simulation.Emergencies = &sim.SavedEmergencies{Records: records, Counters: composedEmergencyCounters}
	return file
}

// composedEmergencyView returns the stream row of an active emergency at
// its widest, for the pod podID: a 16-digit generation and serial, a
// 16-digit order and start tick, and the phase unloading. The serial is
// below the serial of each composed fault, and it increases with index.
func composedEmergencyView(index, count int, podID string) sim.EmergencyView {
	serial := sim.MaxCounter - uint64(maxFaultRecords) - uint64(count-1-index)
	return sim.EmergencyView{
		ID: fmt.Sprintf("i%d.%d", uint64(sim.MaxCounter), serial), PodID: podID,
		OrderID: sim.MaxCounter, Phase: sim.EmergencyPhaseUnloading, StartTick: sim.MaxCounter,
	}
}

// composedEmergencyFrame adds the stage 3 stream members of the incident
// emergency contract to frame at the widest values that ApplyStream
// accepts: the emergency marker, the counters at sim.MaxCounter, and
// sim.MaxEmergencies rows of composedEmergencyView, each for a vehicle of
// its own. The start of a row is not after the frame tick, so the frame
// tick is sim.MaxCounter.
func composedEmergencyFrame(frame StreamFrame) StreamFrame {
	simulation := &frame.State.Simulation
	simulation.EmergencyContract = sim.EmergencyV1Contract
	simulation.Tick = sim.MaxCounter
	active := make([]sim.EmergencyView, sim.MaxEmergencies)
	for i := range active {
		active[i] = composedEmergencyView(i, len(active), simulation.Vehicles[i].Pod.ID)
	}
	simulation.Emergencies = sim.EmergenciesView{Active: active, Counters: composedEmergencyCounters}
	return frame
}

// composedDebrisFrom and composedDebrisTo are the widest debris segment
// that the decoders accept: 0 <= from < to <= from+50, and each bound has
// 23 bytes in its shortest form.
const (
	composedDebrisFrom = 2.2250738585072014e-308
	composedDebrisTo   = 2.2250738585072024e-308
)

// composedSave returns the composed save of shape with the stage 1,
// stage 2, and stage 3 members of the incident contracts. See
// composedBaseSave.
func composedSave(t *testing.T, shape composedShape) stateFile {
	t.Helper()
	return composedEmergencySave(t, composedFaultSave(t, composedIncidentSave(t, composedBaseSave(t, shape))))
}

// composedBaseSave returns the composed save of shape without the members
// of the incident contract. The widest builders omit two members that the
// decoder accepts: sharedRideJoin, and the service ID of a plain order.
// The composed save adds them.
func composedBaseSave(t *testing.T, shape composedShape) stateFile {
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
		file = compactClassWorstCaseFile(base, pod, trip)
	}
	file.Simulation.SharedRideJoin = sim.SharedRideJoin(strings.Repeat("\x01", 1<<10))
	return file
}

// composedIncidentFrame adds the stage 1 stream members of the incident
// contract to frame at the widest values that ApplyStream accepts: the
// counters, holds 3 and the longest purpose on each vehicle, and a leg
// origin of 64 bytes on each rider and each pending order. Packed text of
// 64 bytes always has 88 bytes.
func composedIncidentFrame(frame StreamFrame) StreamFrame {
	legFrom := widestID('g', 0)
	simulation := &frame.State.Simulation
	simulation.IncidentContract = sim.IncidentV1Contract
	simulation.Interrupted, simulation.InterruptedPassengers = sim.MaxCounter, sim.MaxCounter
	simulation.Vehicles = slices.Clone(simulation.Vehicles)
	for i := range simulation.Vehicles {
		vehicle := &simulation.Vehicles[i]
		vehicle.Withdrawn, vehicle.Operational = 3, sim.OperationalEmergencyUnload
		vehicle.Riders = slices.Clone(vehicle.Riders)
		for j := range vehicle.Riders {
			vehicle.Riders[j].LegFrom = legFrom
		}
	}
	simulation.Pending = slices.Clone(simulation.Pending)
	for i := range simulation.Pending {
		simulation.Pending[i].LegFrom = legFrom
	}
	return frame
}

// composedFaultFrame adds the stage 2 stream members of the incident
// suspension contract to frame at the widest values that ApplyStream
// accepts: the fault marker, the counters at sim.MaxCounter, and 300
// pod records and 64 debris records. Each record has a 16-digit generation
// and serial, and 16-digit ticks. A pod record names a vehicle of its own,
// which has an ID of 66 JSON bytes, and has the phase evacuated. A debris
// record has a lane ID of 66 JSON bytes and the widest segment. The start
// of a record is not after the frame tick, so the frame tick is
// sim.MaxCounter. The vehicle IDs of frame must differ.
func composedFaultFrame(frame StreamFrame) StreamFrame {
	simulation := &frame.State.Simulation
	simulation.FaultContract = sim.FaultV1Contract
	simulation.Tick = sim.MaxCounter
	start, end := int64(sim.MaxCounter-1), int64(sim.MaxCounter)
	from, to := composedDebrisFrom, composedDebrisTo
	active := make([]sim.FaultView, maxFaultRecords)
	for i := range active {
		fault := sim.FaultView{ID: fmt.Sprintf("i%d.%d", uint64(sim.MaxCounter), uint64(sim.MaxCounter)-uint64(len(active)-1-i)), StartTick: start, EndTick: end}
		if i < len(simulation.Vehicles) {
			fault.Kind, fault.PodID, fault.Phase, fault.EvacuateTick = sim.FaultKindPod, simulation.Vehicles[i].Pod.ID, sim.FaultPhaseEvacuated, &end
		} else {
			fault.Kind, fault.LaneID, fault.FromMeters, fault.ToMeters = sim.FaultKindDebris, widestID('l', i-len(simulation.Vehicles)), &from, &to
		}
		active[i] = fault
	}
	counters := sim.FaultCounters{Started: sim.MaxCounter, Cleared: sim.MaxCounter, Evacuations: sim.MaxCounter, Reroutes: sim.MaxCounter, FaultWaitTicks: sim.MaxCounter}
	simulation.Faults = sim.FaultsView{Active: active, Counters: counters}
	return frame
}

// composedVehicleID returns the ID of vehicle index, of the size of the
// IDs of the widest builders. Vehicle 0 keeps the pod ID of the widest
// builders, which the other members of the frame name.
func composedVehicleID(index int) string {
	return widestID('p', index)
}

// composedStreamFrame returns the composed frame of shape with the stage
// 1, stage 2, and stage 3 members of the incident contracts. The plain frame adds the
// service ID of each order, which the widest builder omits and the
// decoder accepts. Each vehicle gets an ID of its own, of the same size,
// so that each pod record names another vehicle.
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
	for i := range frame.State.Simulation.Vehicles {
		frame.State.Simulation.Vehicles[i].Pod.ID = composedVehicleID(i)
	}
	return composedEmergencyFrame(composedFaultFrame(composedIncidentFrame(frame)))
}

// TestComposedWorstCaseFormats measures one composed fixture for each save
// shape: plain and Express. For each
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
	shapes := []composedShape{
		{"plain", contractMarkers{}},
		{"express", contractMarkers{order: sim.ExpressOrderContract}},
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
				"Each integer is from -9007199254740991 to 9007199254740991, the range that the decoders accept; only a demand seed can have 64 bits. " +
				"Each ID has 64 characters from A-Z, a-z, 0-9, '.', '+' and '-', which each encoder writes as 1 byte, and each dispatch reason has 1,024 bytes. " +
				"Error text and enum members keep control bytes, which JSON writes as 6 bytes each, because the decoders do not check their characters. " +
				"The decoder accepts a route on each waiting trip, but a server writes at most 300: dispatch gives a waiting trip a route only when it assigns a pod, each pod has at most one assigned trip, and a physical restore keeps waiting routes within its block budget. " +
				"Plain save: widestSavedBase with 300 compact-class pods, each with the widest platoon link, and 2,600 trips, 300 of them with a route. " +
				"Express save: widestExpressSave, with 300 Express pods that have 20 riders and 20 boarding records, and 8,600 trips. " +
				"Each save also has sharedRideJoin, and each plain order has a service ID. " +
				"Each shape has the incident marker and the stage 1 members at the widest values that the decoders accept. " +
				"The saved project has 300 stations, so each station index has 3 digits. " +
				"Each saved pod has holds 3 and the operational tuple [1,2]: an interrupt bit needs an active rider, which omits the 17 bytes of completed. " +
				"Each rider and each trip has a leg origin with a 3-digit station index; a rider with a boarding record has its origin, where the record is. " +
				"Each trip has the excluded pod index 299, or 298 for a trip with pod 299, so no trip is boarded. " +
				"The saved counters are -9007199254740991, the most negative integer that the decoders accept, and the serial is 9007199254740991. " +
				"In the streams, the counters are 9007199254740991, each vehicle has holds 3 and emergency-unload, and each rider and pending order has a leg origin of 64 bytes. " +
				"Each shape also has the fault marker and the stage 2 members at the widest values that a server writes. " +
				"The saved project has the widest faults settings, and the save has 300 pod records and 64 debris records with 16-digit generations and serials, 16-digit ticks, pod index 299, the last lane index, segment bounds of 23 bytes, and the largest counters. " +
				"In the streams, the faults have the largest counters, 300 pod records with the phase evacuated, and 64 debris records with lane IDs of 64 bytes and segment bounds of 23 bytes; each record has a 16-digit generation and serial and 16-digit ticks. " +
				"Each vehicle has an ID of its own with 64 bytes, and the frame tick is sim.MaxCounter, because a fault does not start after the frame tick. " +
				"Each shape also has the emergency marker and the stage 3 members at the widest values that a server writes. " +
				"The saved project has the widest emergencies settings, and the save has 4 records with 16-digit generations and serials, a 16-digit start and order, pod index 299, and the largest counters. " +
				"In the streams, the emergencies have the largest counters and 4 rows with the phase unloading, each for a vehicle of its own, with a 16-digit generation and serial, and a 16-digit order and start tick. " +
				"Streams: plain is maximumStreamFrame with compact vehicles, boarding records and service IDs; Express is widestExpressStreamFrame. " +
				"The delta changes each vehicle, berth and group from an empty base. " +
				"The HTTP state has the widest topology of a valid project, with 64-byte IDs and station names of 80 characters '&'; bound_bytes sets the topology member to the topology cap of 10,489,856 bytes. " +
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
	base := composedBaseSave(t, shape)
	baseBytes := len(composedSaveJSON(t, base))
	runtime.GC()
	incident := composedIncidentSave(t, base)
	incidentBytes := len(composedSaveJSON(t, incident))
	runtime.GC()
	fault := composedFaultSave(t, incident)
	faultBytes := len(composedSaveJSON(t, fault))
	runtime.GC()
	file := composedEmergencySave(t, fault)
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
	if len(decoded.Simulation.Pods) != project.MaxPods || int64(len(decoded.Simulation.Waiting)) != orders {
		t.Fatalf("%s save lost records", shape.name)
	}
	pod, trip := decoded.Simulation.Pods[0], decoded.Simulation.Waiting[0]
	if pod.Withdrawn != 3 || pod.Purpose != 1 || pod.Riders[0].LegFrom == "" || trip.Request.LegFrom == "" || trip.ExcludedPod == "" {
		t.Fatalf("%s save lost incident members", shape.name)
	}
	if faults := decoded.Simulation.Faults; faults == nil || !slices.Equal(faults.Records, file.Simulation.Faults.Records) || faults.Counters != file.Simulation.Faults.Counters {
		t.Fatalf("%s save lost fault members", shape.name)
	}
	if emergencies := decoded.Simulation.Emergencies; emergencies == nil || !slices.Equal(emergencies.Records, file.Simulation.Emergencies.Records) ||
		emergencies.Counters != file.Simulation.Emergencies.Counters {
		t.Fatalf("%s save lost emergency members", shape.name)
	}
	// The stage 1 members fit the stage 1 save allocation of section 11.7
	// of the incident contract (section 14.4).
	allocation := composedIncidentAllocation
	if shape.markers.order == sim.ExpressOrderContract {
		allocation = composedExpressIncidentAllocation
	}
	growth := incidentBytes - baseBytes
	if growth > allocation {
		t.Errorf("%s save: the stage 1 members add %d bytes, more than the allocation of %d", shape.name, growth, allocation)
	}
	t.Logf("%s save stage 1 growth=%d allocation=%d", shape.name, growth, allocation)
	// The stage 2 members fit the save sub-allocation of section 13.6 of
	// the incident suspension contract.
	growth = faultBytes - incidentBytes
	if growth > composedFaultSaveAllocation {
		t.Errorf("%s save: the stage 2 members add %d bytes, more than the allocation of %d", shape.name, growth, composedFaultSaveAllocation)
	}
	t.Logf("%s save stage 2 growth=%d allocation=%d", shape.name, growth, composedFaultSaveAllocation)
	// The stage 3 members fit the emergency records of the joint save
	// allocation (section 11.6 of the incident emergency contract).
	growth = len(raw) - faultBytes
	if growth > composedEmergencySaveAllocation {
		t.Errorf("%s save: the stage 3 members add %d bytes, more than the allocation of %d", shape.name, growth, composedEmergencySaveAllocation)
	}
	t.Logf("%s save stage 3 growth=%d allocation=%d", shape.name, growth, composedEmergencySaveAllocation)
	return size
}

// measureComposedStream measures the full frame, the delta and the HTTP
// state of the composed frame of shape. A document over the cap is
// recorded, and the test does not decode it. Each document also fits the
// stage 2 and the stage 3 stream allocations: the stage 2 members add at
// most the stage 2 allocation to the document without the stage 2 and
// stage 3 members, and the stage 3 members add at most the stage 3
// allocation to the document without them.
func measureComposedStream(t *testing.T, shape composedShape) []composedSize {
	t.Helper()
	frame := composedStreamFrame(t, shape)
	// The HTTP state of a widest frame does not pass the state checks of
	// DecodeStateJSON, because its values are independent maxima. The test
	// encodes it with the options of EncodeStateJSON, and it decodes it with
	// the bounded scans and the typed decode of DecodeStateJSON.
	topology := widestTopology(t, shape.markers)
	fault := frame
	fault.State.Simulation.EmergencyContract, fault.State.Simulation.Emergencies = "", sim.EmergenciesView{}
	incident := fault
	incident.State.Simulation.FaultContract, incident.State.Simulation.Faults = "", sim.FaultsView{}
	incident.State.Simulation.Tick = -sim.MaxCounter
	var incidentBytes, faultBytes []int
	for _, document := range composedStreamDocuments(t, shape, incident, topology) {
		incidentBytes = append(incidentBytes, len(document))
	}
	runtime.GC()
	for _, document := range composedStreamDocuments(t, shape, fault, topology) {
		faultBytes = append(faultBytes, len(document))
	}
	runtime.GC()
	limits := streamLimits(shape.markers)
	orders, _ := shape.markers.orderBounds()
	var sizes []composedSize
	for index, raw := range composedStreamDocuments(t, shape, frame, topology) {
		format := [...]string{"full", "delta", "http"}[index]
		if raw == nil {
			sizes = append(sizes, composedSize{Shape: shape.name, Format: format})
			continue
		}
		growth := faultBytes[index] - incidentBytes[index]
		if growth > composedFaultStreamAllocation {
			t.Errorf("%s %s: the stage 2 members add %d bytes, more than the allocation of %d", shape.name, format, growth, composedFaultStreamAllocation)
		}
		t.Logf("%s %s stage 2 growth=%d allocation=%d", shape.name, format, growth, composedFaultStreamAllocation)
		growth = len(raw) - faultBytes[index]
		if growth > composedEmergencyStreamAllocation {
			t.Errorf("%s %s: the stage 3 members add %d bytes, more than the allocation of %d", shape.name, format, growth, composedEmergencyStreamAllocation)
		}
		t.Logf("%s %s stage 3 growth=%d allocation=%d", shape.name, format, growth, composedEmergencyStreamAllocation)
		var size composedSize
		if format == "http" {
			size = composedStreamSize(t, shape, format, raw, len(composedTopologyJSON(t, topology, frame)))
		} else {
			size = composedStreamSize(t, shape, format, raw, 0)
		}
		sizes = append(sizes, size)
		if size.BoundBytes > MaxStreamJSON {
			continue
		}
		if scanErr := prescanJSON(raw, limits); scanErr != nil {
			t.Fatalf("%s %s failed the bounded scan: %v", shape.name, format, scanErr)
		}
		assertExplicitArrayBounds(t, shape.name+" "+format, raw, limits)
		var vehicles, pending, faults, emergencies int
		switch format {
		case "http":
			var decoded StateEnvelope
			if members, err := decodeMarkedJSON(raw, &decoded); err != nil || !members.incident || !members.fault || !members.emergency {
				t.Fatalf("%s HTTP state decode: %v, marked members %+v", shape.name, err, members)
			}
			simulation := decoded.Frame.State.Simulation
			vehicles, pending, faults = len(simulation.Vehicles), len(simulation.Pending), len(simulation.Faults.Active)
			emergencies = len(simulation.Emergencies.Active)
		case "full":
			decoded, err := DecodeStreamJSON(raw)
			if err != nil || !decoded.incidentMembers || !decoded.faultMembers || !decoded.emergencyMembers {
				t.Fatalf("%s full decode: %v, incident members %v, fault members %v, emergency members %v", shape.name, err, decoded.incidentMembers, decoded.faultMembers, decoded.emergencyMembers)
			}
			simulation := decoded.Full.State.Simulation
			vehicles, pending, faults = len(simulation.Vehicles), len(simulation.Pending), len(simulation.Faults.Active)
			emergencies = len(simulation.Emergencies.Active)
		default:
			decoded, err := DecodeStreamJSON(raw)
			if err != nil || !decoded.incidentMembers || !decoded.faultMembers || !decoded.emergencyMembers {
				t.Fatalf("%s delta decode: %v, incident members %v, fault members %v, emergency members %v", shape.name, err, decoded.incidentMembers, decoded.faultMembers, decoded.emergencyMembers)
			}
			var group []json.RawMessage
			if err := json.Unmarshal(decoded.Delta.Groups["pending"], &group); err != nil {
				t.Fatal(err)
			}
			var active sim.FaultsView
			if err := json.Unmarshal(decoded.Delta.Groups["faults"], &active); err != nil {
				t.Fatal(err)
			}
			emergencyGroup, groupErr := decodeEmergenciesGroup(decoded.Delta.Groups["emergencies"])
			if groupErr != nil {
				t.Fatal(groupErr)
			}
			vehicles, pending, faults = len(decoded.Delta.Vehicles), len(group), len(active.Active)
			emergencies = len(emergencyGroup.Active)
		}
		if vehicles != project.MaxPods || int64(pending) != orders || faults != maxFaultRecords || emergencies != sim.MaxEmergencies {
			t.Fatalf("%s %s lost records: %d vehicles, %d orders, %d faults, %d emergencies", shape.name, format, vehicles, pending, faults, emergencies)
		}
		runtime.GC()
	}
	return sizes
}

// composedTopologyJSON returns the JSON form of topology with the markers
// of frame.
func composedTopologyJSON(t *testing.T, topology TopologySnapshot, frame StreamFrame) []byte {
	t.Helper()
	topology.IncidentContract, topology.FaultContract = frame.State.Simulation.IncidentContract, frame.State.Simulation.FaultContract
	topology.EmergencyContract = frame.State.Simulation.EmergencyContract
	raw, err := json.Marshal(topology)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// composedStreamDocuments returns the full frame, the delta and the HTTP
// state of frame, with topology in the HTTP state. A stream document that
// the encoder refuses is nil, and the test records an error for it.
func composedStreamDocuments(t *testing.T, shape composedShape, frame StreamFrame, topology TopologySnapshot) [3][]byte {
	t.Helper()
	delta := maximumStreamDelta(t, frame)
	full := StreamEnvelope{
		OrderContract: shape.markers.order, Kind: "full", Stream: strings.Repeat("x", 32), Sequence: sim.MaxCounter, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame,
	}
	changed := full
	changed.Kind, changed.Full, changed.Delta, changed.Base = "delta", nil, &delta, sim.MaxCounter-1
	var documents [3][]byte
	for index, envelope := range []StreamEnvelope{full, changed} {
		raw, err := EncodeStreamJSON(envelope)
		if err != nil {
			t.Errorf("%s %s encode: %v", shape.name, envelope.Kind, err)
			continue
		}
		documents[index] = raw
	}
	topology.IncidentContract, topology.FaultContract = frame.State.Simulation.IncidentContract, frame.State.Simulation.FaultContract
	topology.EmergencyContract = frame.State.Simulation.EmergencyContract
	envelope := StateEnvelope{OrderContract: shape.markers.order, Topology: topology, Frame: frame}
	raw, err := jsonv2.Marshal(envelope, json.DefaultOptionsV1(), packedRequestOptions())
	if err != nil {
		t.Fatal(err)
	}
	documents[2] = raw
	return documents
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

// TestEmergencyByteAllocation encodes the stage 3 members with 300 records
// at their widest, the pod limit, with no prescan and no decode (section
// 14.4 of the incident emergency contract). Section 11.6 gives the totals
// for 20-digit numbers and pod IDs of control bytes. The decoders accept
// at most 16 digits (sim.MaxCounter), and a pod ID has only ID
// characters, so the totals are smaller. They fit the stage 3
// allocations, so the budget holds for any record cap up to the pod limit.
func TestEmergencyByteAllocation(t *testing.T) {
	t.Parallel()
	const count = project.MaxPods
	records := make([]sim.SavedEmergency, count)
	views := make([]sim.EmergencyView, count)
	for i := range count {
		records[i] = composedEmergencyRecord(i, count)
		views[i] = composedEmergencyView(i, count, composedVehicleID(i))
	}
	member := func(name string, value any, options ...jsonv2.Options) int {
		t.Helper()
		data, err := jsonv2.Marshal(value, options...)
		if err != nil {
			t.Fatal(err)
		}
		// The member has a separator and its name before the value.
		return len(`,"`+name+`":`) + len(data)
	}
	file := stateFile{}
	saved := member("emergencies", sim.SavedEmergencies{Records: records, Counters: composedEmergencyCounters},
		jsonv2.Deterministic(true), jsonv2.WithMarshalers(file.simulationMarshalers()))
	if saved != 22_327 || saved > composedEmergencySaveAllocation {
		t.Errorf("the save member has %d bytes, want 22,327 within the allocation of %d", saved, composedEmergencySaveAllocation)
	}
	widest, err := jsonv2.Marshal(views[count-1])
	if err != nil {
		t.Fatal(err)
	}
	if len(widest) != 194 {
		t.Errorf("the widest row has %d bytes, want 194", len(widest))
	}
	stream := member("emergencies", sim.EmergenciesView{Active: views, Counters: composedEmergencyCounters}, json.DefaultOptionsV1())
	marker := member("emergencyContract", sim.EmergencyV1Contract)
	if stream != 58_626 || marker != 35 || stream+2*marker > composedEmergencyStreamAllocation {
		t.Errorf("the stream member has %d bytes and each marker %d, want 58,626 and 35 within the allocation of %d", stream, marker, composedEmergencyStreamAllocation)
	}
	t.Logf("save member=%d allocation=%d stream member=%d markers=%d allocation=%d", saved, composedEmergencySaveAllocation, stream, 2*marker, composedEmergencyStreamAllocation)
}
