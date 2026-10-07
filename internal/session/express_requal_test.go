package session

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"runtime"
	"runtime/metrics"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/sim"
)

// requalText returns valid UTF-8 text of exactly size bytes. It repeats
// an escaped control byte, a quote, a backslash and two multibyte runes.
func requalText(size int) string {
	const unit = "\x01\"\\é中"
	text := strings.Repeat(unit, size/len(unit))
	return text + strings.Repeat("x", size-len(text))
}

// expressRequalSave returns the save of an Express session with one
// waiting party of 20 and three Express pods. The first pod has 20 rider
// records with boarding tuples: 19 completed history records and one
// active party. The second pod has completed riders and the
// journeyOrigin form, without boarding tuples. The third pod has no
// riders. The floats sit on both sides of the exponent boundaries of the
// JSON encoder that the integer scan accepts, and the integers at the
// largest counter, sim.MaxCounter.
func expressRequalSave(t *testing.T) stateFile {
	t.Helper()
	shared := expressSession(t)
	newTestClient(shared, "requal").mustApply(t, Command{
		Action: "trip", Origin: "harbor", Destination: "market", PartySize: 20,
		SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market", OrderContract: sim.ExpressOrderContract,
	})
	file := sessionStateFile(t, shared)
	file.RestoreAttempts = 0
	// The pod takes the party at once, so its order becomes the waiting
	// trip.
	if len(file.Simulation.Pods) != 1 || file.Simulation.Pods[0].Class != sim.ExpressClass || len(file.Simulation.Pods[0].Riders) != 1 {
		t.Fatalf("the Express session has pods %+v", file.Simulation.Pods)
	}
	file.Simulation.Waiting = []sim.SavedTrip{{Request: file.Simulation.Pods[0].Riders[0]}}
	waiting := &file.Simulation.Waiting[0].Request
	waiting.ID, waiting.RequestedTick = sim.MaxCounter, sim.MaxCounter
	waiting.DispatchReason, waiting.PodID = requalText(1024), requalText(64)

	rider := sim.SavedRequest{
		ID: 1, From: "harbor", To: "market", PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice,
		ServiceID: "harbor-market", Completed: true, RequestedTick: sim.MaxCounter - 2, BoardedTick: sim.MaxCounter, DispatchReason: requalText(1024),
	}
	meters := []float64{0, 0.000001, 1e-07, 0.0000010000000000000002, math.SmallestNonzeroFloat64, 1e15, 1e21}
	tuplePod := file.Simulation.Pods[0]
	tuplePod.RiddenMeters = math.MaxFloat64
	tuplePod.Riders, tuplePod.Boardings = nil, nil
	for i := range sim.MaxExpressParties {
		r := rider
		r.ID = i + 2
		if i == sim.MaxExpressParties-1 {
			r.Completed, r.PartySize, r.BoardedTick = false, 1, 0
		}
		tuplePod.Riders = append(tuplePod.Riders, r)
		tuplePod.Boardings = append(tuplePod.Boardings, sim.RiderBoarding{BerthID: "harbor-1", MetersAtBoarding: meters[i%len(meters)]})
	}
	originPod := file.Simulation.Pods[0]
	originPod.ID, originPod.JourneyOrigin = "requal-origin", "harbor-1"
	originPod.Riders = []sim.SavedRequest{rider, rider}
	originPod.Riders[1].ID = 100
	emptyPod := file.Simulation.Pods[0]
	emptyPod.ID, emptyPod.Riders, emptyPod.Boardings = "requal-empty", nil, nil
	file.Simulation.Pods = []sim.SavedPod{tuplePod, originPod, emptyPod}
	file.Simulation.PassengerDistanceMeters = math.MaxFloat64
	file.Simulation.DirectDistanceMeters = math.SmallestNonzeroFloat64
	return file
}

// TestExpressRequalSaveShapes saves escaped and multibyte text at each
// decoded limit, both boarding forms, optional history, integers at
// sim.MaxCounter, and floats on both sides of each exponent boundary. The decode
// gives the same values, and the second encode gives the same bytes.
func TestExpressRequalSaveShapes(t *testing.T) {
	t.Parallel()
	file := expressRequalSave(t)
	data := encodeTestState(t, file)
	raw := decompressTestJSON(t, data)
	for _, want := range []string{
		`"boardings":[[0,0],[0,0.000001],[0,1e-7],[0,0.0000010000000000000002],[0,5e-324],[0,1000000000000000],[0,1e+21],`,
		`"journeyOrigin":"harbor-1"`, `"riddenMeters":1.7976931348623157e+308`, `"id":9007199254740991`, `"requestedTick":9007199254740991`,
		`"dispatchReason":"` + base64.StdEncoding.EncodeToString([]byte(requalText(1024))) + `"`,
		`"podID":"` + base64.StdEncoding.EncodeToString([]byte(requalText(64))) + `"`,
	} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Errorf("the save does not contain %s", want)
		}
	}
	// The project member holds the registry ID "harbor-market" as raw text.
	for _, text := range []string{"\\u0001", "é"} {
		if bytes.Contains(raw, []byte(text)) {
			t.Errorf("the save contains unpacked order text %q", text)
		}
	}
	decoded, err := decodeStateFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if err = decoded.resolveBoardings(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Simulation.Waiting, file.Simulation.Waiting) || !reflect.DeepEqual(decoded.Simulation.Pods, file.Simulation.Pods) {
		t.Fatal("the decoded orders, boarding records or pods differ from the saved values")
	}
	if decoded.Simulation.PassengerDistanceMeters != math.MaxFloat64 || decoded.Simulation.DirectDistanceMeters != math.SmallestNonzeroFloat64 {
		t.Fatal("the decoded floats differ from the saved values")
	}
	if !bytes.Equal(raw, decompressTestJSON(t, encodeTestState(t, decoded))) {
		t.Fatal("the reencoded save differs")
	}
}

// TestExpressRequalSavePackedTextRefusals changes one packed order field of
// an Express save. The waiting order and the rider each get each malformed
// form. The save also has a speed that only the typed decode refuses, and
// that member comes first. The packed text scan must refuse the file
// first, with its own error.
func TestExpressRequalSavePackedTextRefusals(t *testing.T) {
	t.Parallel()
	file := expressRequalSave(t)
	file.Simulation.Waiting[0].Request.DispatchReason = "requal-waiting"
	file.Simulation.Waiting[0].Request.PodID = "requal-podw"
	file.Simulation.Pods[0].Riders[0].DispatchReason = "requal-ridert"
	file.Simulation.Pods[0].Riders[0].PodID = "requal-podr"
	raw := decompressTestJSON(t, encodeTestState(t, file))
	speed := []byte(`"speed":` + strconv.Itoa(file.Speed) + `,`)
	if !bytes.Contains(raw, speed) {
		t.Fatal("the save has no speed member")
	}
	typed := bytes.Replace(raw, speed, []byte(`"speed":"fast",`), 1)
	if _, err := decodeStateFile(compressTestJSON(t, typed)); err == nil || strings.Contains(err.Error(), "order text") {
		t.Fatalf("the typed decode did not refuse the speed by itself: %v", err)
	}
	const (
		size      = "invalid packed order text size"
		literal   = "packed order text must use literal base64"
		canonical = "invalid canonical order text"
	)
	encode := func(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }
	for _, field := range []struct {
		name, text string
		limit      int
	}{
		{"waiting dispatchReason", "requal-waiting", 1024},
		{"waiting podID", "requal-podw", 64},
		{"rider dispatchReason", "requal-ridert", 1024},
		{"rider podID", "requal-podr", 64},
	} {
		token := encode(field.text)
		if !strings.HasSuffix(token, "=") || bytes.Count(raw, []byte(`"`+token+`"`)) != 1 {
			t.Fatalf("%s: token %s needs padding and one place in the save", field.name, token)
		}
		for _, test := range []struct{ name, value, err string }{
			{"escaped letter", `"\u00` + strconv.FormatInt(int64(token[0]), 16) + token[1:] + `"`, literal},
			{"escaped newline", `"` + token + `\n"`, literal},
			{"URL alphabet", `"_w=="`, literal},
			{"raw fallback", `"` + field.text + `"`, literal},
			{"missing padding", `"` + strings.TrimRight(token, "=") + `"`, canonical},
			{"padding bits", `"eB=="`, canonical},
			{"invalid UTF-8", `"` + encode("\xff") + `"`, canonical},
			{"decoded limit plus one", `"` + encode(strings.Repeat("x", field.limit+1)) + `"`, canonical},
			{"encoded limit plus one", `"` + encode(strings.Repeat("x", field.limit+3)) + `"`, size},
			{"null", `null`, size},
			{"number", `1`, size},
		} {
			t.Run(field.name+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				changed := bytes.Replace(typed, []byte(`"`+token+`"`), []byte(test.value), 1)
				_, err := decodeStateFile(compressTestJSON(t, changed))
				if stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), test.err) {
					t.Fatalf("got %v, want %s %q", err, reasonInvalidState, test.err)
				}
			})
		}
		limit := bytes.Replace(raw, []byte(`"`+token+`"`), []byte(`"`+encode(requalText(field.limit))+`"`), 1)
		if _, err := decodeStateFile(compressTestJSON(t, limit)); err != nil {
			t.Fatalf("%s: refused text at the decoded limit: %v", field.name, err)
		}
	}
}

// TestExpressRequalSavePrescanBounds checks the array bounds that the
// root Express marker selects in a save: 8,600 waiting trips and 20 riders
// and boarding tuples on a pod. The scan refuses one more of each before
// the typed decode. Without the marker, the plain bounds apply.
func TestExpressRequalSavePrescanBounds(t *testing.T) {
	t.Parallel()
	express := expressRequalSave(t)
	plain := express
	plain.OrderContract, plain.Simulation.OrderContract = "", ""
	plain.Project.OrderContract, plain.Project.ExpressServices = "", nil
	trips := func(file stateFile, count int) stateFile {
		file.Simulation.Waiting = slices.Repeat(file.Simulation.Waiting[:1], count)
		return file
	}
	riders := func(file stateFile, count int) stateFile {
		file.Simulation.Pods = slices.Clone(file.Simulation.Pods)
		pod := &file.Simulation.Pods[1]
		pod.Riders = slices.Repeat(pod.Riders[:1], count)
		return file
	}
	for _, test := range []struct {
		name    string
		file    stateFile
		refused bool
	}{
		{"Express 8,600 waiting trips", trips(express, sim.MaxExpressWaitingTrips), false},
		{"Express 8,601 waiting trips", trips(express, sim.MaxExpressWaitingTrips+1), true},
		{"Express 20 riders", riders(express, sim.MaxExpressParties), false},
		{"Express 21 riders", riders(express, sim.MaxExpressParties+1), true},
		{"plain 2,600 waiting trips", trips(plain, maxSavedTrips), false},
		{"plain 2,601 waiting trips", trips(plain, maxSavedTrips+1), true},
		{"plain 8 riders", riders(plain, sim.MaxSharedRideParties), false},
		{"plain 9 riders", riders(plain, sim.MaxSharedRideParties+1), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.file.OrderContract == "" {
				// A plain save has no Express pods or services. The
				// cases share the pods of the fixture, so copy them.
				test.file.Simulation.Pods = slices.Clone(test.file.Simulation.Pods)
				for i := range test.file.Simulation.Pods {
					test.file.Simulation.Pods[i].Class = sim.GroupClass
				}
				test.file.Simulation.Pods[0].Riders, test.file.Simulation.Pods[0].Boardings = nil, nil
			}
			raw := decompressTestJSON(t, encodeTestState(t, test.file))
			err := prescanJSON(raw, savedLimits(contractMarkers{order: test.file.OrderContract}))
			_, decodeErr := decodeStateFile(compressTestJSON(t, raw))
			if test.refused {
				if !errors.Is(err, errJSONArrayTooLong) || !errors.Is(decodeErr, errJSONArrayTooLong) || stateReason(decodeErr) != reasonInvalidState {
					t.Fatalf("scan %v, decode %v, want %v", err, decodeErr, errJSONArrayTooLong)
				}
				return
			}
			if err != nil || errors.Is(decodeErr, errJSONArrayTooLong) {
				t.Fatalf("scan %v, decode %v", err, decodeErr)
			}
		})
	}
	// The encoder writes at most 20 boarding tuples, so add the 21st tuple
	// to the bytes.
	raw := decompressTestJSON(t, encodeTestState(t, express))
	tuples := []byte(`"boardings":[[0,0],`)
	if !bytes.Contains(raw, tuples) {
		t.Fatal("the save has no boarding tuples")
	}
	extra := bytes.Replace(raw, tuples, []byte(`"boardings":[[0,0],[0,0],`), 1)
	if _, err := decodeStateFile(compressTestJSON(t, extra)); !errors.Is(err, errJSONArrayTooLong) {
		t.Fatalf("21 boarding tuples: %v, want %v", err, errJSONArrayTooLong)
	}
}

// TestExpressRequalSaveRejectedAtomically starts from an Express save that
// the decoder refuses. Startup moves the file aside and starts a new
// session. Each other saved-state version of an Express save also moves
// aside, so an old file never reaches a reader of this version.
func TestExpressRequalSaveRejectedAtomically(t *testing.T) {
	t.Parallel()
	raw := decompressTestJSON(t, encodeTestState(t, expressRequalSave(t)))
	token := []byte(`"from":"` + base64.StdEncoding.EncodeToString([]byte("harbor")) + `"`)
	if !bytes.Contains(raw, token) {
		t.Fatal("the save has no packed origin")
	}
	cases := map[string]struct {
		data   []byte
		reason string
	}{
		"raw origin": {compressTestJSON(t, bytes.Replace(raw, token, []byte(`"from":"harbor"`), 1)), reasonInvalidState},
	}
	for _, version := range []int{1, 2, 3, 4, 5, 6, 7, 8, 10} {
		cases["version "+strconv.Itoa(version)] = struct {
			data   []byte
			reason string
		}{compressTestJSON(t, setRootMember(t, raw, "version", strconv.Itoa(version))), reasonUnsupportedVersion}
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &fakeStore{data: test.data}
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			assertMovedAside(t, s, err, store, test.reason)
			if s.project.OrderContract != "" || len(s.simulation.ExportState().Waiting) != 0 {
				t.Fatal("the new session kept a part of the refused save")
			}
		})
	}
}

// expressRequalChain returns the topology of an Express session, an
// accepted full frame with a pending party of 20, and its successor that
// replaces the pending group.
func expressRequalChain(t *testing.T) (TopologySnapshot, StreamFrame, StreamFrame) {
	t.Helper()
	shared := expressSession(t)
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	frame.State.Simulation.Pending = []sim.Request{markerSectionRequest("express", 1)}
	next := ownStreamBoardings(frame)
	next.State.Revision++
	next.State.Simulation.Tick++
	next.State.Simulation.Pending = []sim.Request{markerSectionRequest("express", 2), markerSectionRequest("express", 3)}
	next.State.Simulation.Pending[1].DispatchReason = requalText(1024)
	return shared.Topology(), frame, next
}

// expressRequalRoundTrip encodes an envelope and decodes it again.
func expressRequalRoundTrip(t *testing.T, e StreamEnvelope) StreamEnvelope {
	t.Helper()
	raw, err := EncodeStreamJSON(e)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeStreamJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

// TestExpressRequalChainRecovery applies an Express full frame and a delta
// that replaces the pending group. A delta after a gap, a delta of another
// stream, and a delta of another epoch fail and leave the accepted frame
// and the assembler as they were. A full frame of the new epoch then starts
// a new chain, and only an assembler of the new topology accepts it.
func TestExpressRequalChainRecovery(t *testing.T) {
	t.Parallel()
	topology, frame, next := expressRequalChain(t)
	assembler, err := NewStreamAssembler(topology)
	if err != nil {
		t.Fatal(err)
	}
	full := StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "first", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	first, err := ApplyStream(StreamFrame{}, "", 0, expressRequalRoundTrip(t, full))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assembler.State(first); err != nil {
		t.Fatal(err)
	}
	delta, err := makeDelta(first, next)
	if err != nil {
		t.Fatal(err)
	}
	if _, replaced := delta.Groups["pending"]; !replaced {
		t.Fatal("the delta does not replace the pending group")
	}
	envelope := StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: "first", Sequence: 2, Base: 1, Source: sourceOf(next), Build: next.State.Build, Delta: &delta}
	second, err := ApplyStream(first, "first", 1, expressRequalRoundTrip(t, envelope))
	if err != nil {
		t.Fatal(err)
	}
	state, err := assembler.State(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.Simulation.Pending, next.State.Simulation.Pending) {
		t.Fatal("the pending replacement group changed the orders")
	}
	accepted, previous := ownStreamBoardings(second), assembler.previous

	other := ownStreamBoardings(next)
	other.State.Epoch, other.State.Revision = "other-epoch", 1
	crossed, err := makeDelta(second, other)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		stream   string
		sequence uint64
		e        StreamEnvelope
		err      string
	}{
		{"gap", "first", 2, StreamEnvelope{Stream: "first", Sequence: 4, Base: 3, Source: sourceOf(next), Build: next.State.Build, Delta: &StreamDelta{}}, "delta base mismatch"},
		{"other stream", "first", 2, StreamEnvelope{Stream: "second", Sequence: 3, Base: 2, Source: sourceOf(next), Build: next.State.Build, Delta: &StreamDelta{}}, "delta base mismatch"},
		{"other epoch", "first", 2, StreamEnvelope{Stream: "first", Sequence: 3, Base: 2, Source: sourceOf(other), Build: other.State.Build, Delta: &crossed}, "delta crosses source boundary"},
	} {
		test.e.OrderContract, test.e.Kind = sim.ExpressOrderContract, "delta"
		if _, applyErr := ApplyStream(second, test.stream, test.sequence, expressRequalRoundTrip(t, test.e)); applyErr == nil || applyErr.Error() != test.err {
			t.Fatalf("%s: got %v, want %q", test.name, applyErr, test.err)
		}
		if !reflect.DeepEqual(second, accepted) || !reflect.DeepEqual(assembler.previous, previous) {
			t.Fatalf("%s: the refused delta changed the accepted frame", test.name)
		}
	}

	recovery := StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "second", Sequence: 1, Source: sourceOf(other), Build: other.State.Build, Full: &other}
	recovered, err := ApplyStream(second, "first", 2, expressRequalRoundTrip(t, recovery))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = assembler.State(recovered); err == nil || err.Error() != "topology revision does not match state frame" {
		t.Fatalf("the assembler of the old epoch: %v", err)
	}
	if !reflect.DeepEqual(assembler.previous, previous) {
		t.Fatal("the refused frame changed the assembler")
	}
	topology.Epoch = "other-epoch"
	renewed, err := NewStreamAssembler(topology)
	if err != nil {
		t.Fatal(err)
	}
	state, err = renewed.State(recovered)
	if err != nil {
		t.Fatal(err)
	}
	if state.Epoch != "other-epoch" || state.Simulation.OrderContract != sim.ExpressOrderContract || !reflect.DeepEqual(state.Simulation.Pending, next.State.Simulation.Pending) {
		t.Fatal("the recovered state lost its epoch, its marker or its orders")
	}
}

// TestExpressRequalTopologyBinding binds an Express frame to the topology
// of its source and to the service registry of that topology. The HTTP
// state binds the same identity.
func TestExpressRequalTopologyBinding(t *testing.T) {
	t.Parallel()
	topology, frame, _ := expressRequalChain(t)
	for _, test := range []struct {
		name   string
		change func(*TopologySnapshot)
		err    string
	}{
		{"server start", func(topology *TopologySnapshot) { topology.ServerStart = "other" }, "topology revision does not match state frame"},
		{"epoch", func(topology *TopologySnapshot) { topology.Epoch = "other" }, "topology revision does not match state frame"},
		{"project revision", func(topology *TopologySnapshot) { topology.ProjectRevision++ }, "topology revision does not match state frame"},
		{"other registry entry", func(topology *TopologySnapshot) {
			topology.ExpressServices = slices.Clone(topology.ExpressServices)
			topology.ExpressServices[0].ID = "harbor-market-2"
		}, "unknown Express service or directed pair"},
		{"reversed registry pair", func(topology *TopologySnapshot) {
			topology.ExpressServices = slices.Clone(topology.ExpressServices)
			topology.ExpressServices[0].From, topology.ExpressServices[0].To = "market", "harbor"
		}, "unknown Express service or directed pair"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			changed := topology
			test.change(&changed)
			assembler, err := NewStreamAssembler(changed)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = assembler.State(frame); err == nil || err.Error() != test.err {
				t.Fatalf("got %v, want %q", err, test.err)
			}
		})
	}
	raw, err := EncodeStateJSON(topology, frame)
	if err != nil {
		t.Fatal(err)
	}
	epoch := []byte(`"epoch":"` + topology.Epoch + `"`)
	if bytes.Index(raw, epoch) > bytes.Index(raw, []byte(`"frame":`)) {
		t.Fatal("the HTTP state does not start with the topology")
	}
	if _, err = DecodeStateJSON(bytes.Replace(raw, epoch, []byte(`"epoch":"other"`), 1)); err == nil || err.Error() != "topology revision does not match state frame" {
		t.Fatalf("HTTP state of another epoch: %v", err)
	}
}

// TestExpressRequalClassLaneCache checks the lanes that the assembler
// admits for each class. A lane that admits Express by itself does not
// admit it when it ends at a berth of a station that refuses Express, or
// when it belongs to such a station. A vehicle without a class has the
// legacy class.
func TestExpressRequalClassLaneCache(t *testing.T) {
	t.Parallel()
	_, frame, _ := expressRequalChain(t)
	frame.State.Simulation.Pending = nil
	topology := expressSession(t).Topology()
	topology.Epoch, topology.ServerStart, topology.ProjectRevision = frame.State.Epoch, frame.State.ServerStart, frame.State.ProjectRevision
	classes := func(names ...string) sim.ClassSet {
		set, err := sim.NewClassSet(names...)
		if err != nil {
			t.Fatal(err)
		}
		return set
	}
	all := classes("legacy", "group", "express")
	network := &topology.Network
	network.Nodes = append(slices.Clone(network.Nodes), sim.Node{ID: "requal-berth-node"})
	network.Stations = append(slices.Clone(network.Stations),
		sim.Station{ID: "requal-group", VehicleClasses: classes("group"), Berths: []sim.Berth{{ID: "requal-group-1", Node: "requal-berth-node", VehicleClasses: all}}},
		sim.Station{ID: "requal-group-lanes", VehicleClasses: classes("group")})
	from, to := network.Nodes[0].ID, network.Nodes[1].ID
	network.Lanes = append(slices.Clone(network.Lanes),
		sim.Lane{ID: "requal-open", From: from, To: to, VehicleClasses: all},
		sim.Lane{ID: "requal-berth", From: from, To: "requal-berth-node", VehicleClasses: all},
		sim.Lane{ID: "requal-berth-out", From: "requal-berth-node", To: to, VehicleClasses: all},
		sim.Lane{ID: "requal-station", From: from, To: to, StationID: "requal-group-lanes", VehicleClasses: all},
		sim.Lane{ID: "requal-express", From: from, To: to, VehicleClasses: classes("group", "express")})
	for i := range network.Stations[:3] {
		network.Stations[i].VehicleClasses = all
		network.Stations[i].Berths = slices.Clone(network.Stations[i].Berths)
		for j := range network.Stations[i].Berths {
			network.Stations[i].Berths[j].VehicleClasses = all
		}
	}
	index := func(id string) int {
		return slices.IndexFunc(network.Lanes, func(lane sim.Lane) bool { return lane.ID == id })
	}
	for _, test := range []struct {
		lane  string
		class sim.VehicleClass
		err   string
	}{
		{"requal-open", sim.ExpressClass, ""},
		{"requal-berth", sim.ExpressClass, "route lane does not admit vehicle class"},
		{"requal-berth-out", sim.ExpressClass, "route lane does not admit vehicle class"},
		{"requal-station", sim.ExpressClass, "route lane does not admit vehicle class"},
		{"requal-open", "", ""},
		{"requal-express", "", "route lane does not admit vehicle class"},
	} {
		t.Run(test.lane+"/"+string(test.class), func(t *testing.T) {
			t.Parallel()
			assembler, err := NewStreamAssembler(topology)
			if err != nil {
				t.Fatal(err)
			}
			candidate := ownStreamBoardings(frame)
			candidate.State.Simulation.Vehicles[0].Pod.Class = test.class
			candidate.Routes = slices.Clone(candidate.Routes)
			lane := index(test.lane)
			candidate.Routes[0] = sim.RoutePresentation{Origin: 0, Display: []int{lane}, Lanes: []int{lane}}
			_, err = assembler.State(candidate)
			if test.err == "" && err != nil || test.err != "" && (err == nil || err.Error() != test.err) {
				t.Fatalf("got %v, want %q", err, test.err)
			}
		})
	}
}

// TestExpressRequalStreamCaps decodes Express publications at the exact
// stream caps: MaxStreamJSON bytes of JSON and MaxStreamMessage bytes of
// gzip. One more byte of either, or a second gzip member, fails.
func TestExpressRequalStreamCaps(t *testing.T) {
	_, frame, _ := expressRequalChain(t)
	raw, err := EncodeStreamJSON(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "caps", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame})
	if err != nil {
		t.Fatal(err)
	}
	padded := append(slices.Clone(raw), bytes.Repeat([]byte(" "), MaxStreamJSON-len(raw))...)
	if _, err = DecodeStreamJSON(padded); err != nil {
		t.Fatalf("refused %d bytes of JSON: %v", len(padded), err)
	}
	if _, err = DecodeStreamJSON(append(padded, ' ')); err == nil || err.Error() != "state JSON too large" {
		t.Fatalf("one byte past the JSON cap: %v", err)
	}
	compressed, err := compressStreamJSON(padded)
	if err != nil {
		t.Fatal(err)
	}
	inflated, err := InflateStream(compressed)
	if err != nil || !bytes.Equal(inflated, padded) {
		t.Fatalf("the JSON cap did not inflate: %v", err)
	}
	gzipped := func(parts ...[]byte) []byte {
		var buffer bytes.Buffer
		for _, part := range parts {
			writer := gzip.NewWriter(&buffer)
			if _, writeErr := writer.Write(part); writeErr != nil {
				t.Fatal(writeErr)
			}
			if closeErr := writer.Close(); closeErr != nil {
				t.Fatal(closeErr)
			}
		}
		return buffer.Bytes()
	}
	for name, test := range map[string]struct {
		data []byte
		err  string
	}{
		"one byte past the JSON cap": {gzipped(append(padded, ' ')), "invalid state gzip size or trailing member"},
		"second member":              {gzipped(raw, raw), "invalid state gzip size or trailing member"},
		"one byte past the gzip cap": {append(gzipped(raw), make([]byte, MaxStreamMessage+1-len(gzipped(raw)))...), "compressed state too large"},
	} {
		if _, err = InflateStream(test.data); err == nil || err.Error() != test.err {
			t.Fatalf("%s: got %v, want %q", name, err, test.err)
		}
	}
	// A message of exactly MaxStreamMessage bytes reaches the gzip reader.
	exact := append(gzipped(raw), make([]byte, MaxStreamMessage-len(gzipped(raw)))...)
	if _, err = InflateStream(exact); err == nil || err.Error() == "compressed state too large" {
		t.Fatalf("a message at the gzip cap: %v", err)
	}
}

// requalPeak samples the bytes of live and unswept heap objects every
// millisecond until stop returns the largest sample.
func requalPeak() (stop func() uint64) {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	done, result := make(chan struct{}), make(chan uint64)
	go func() {
		var peak uint64
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			metrics.Read(sample)
			peak = max(peak, sample[0].Value.Uint64())
			select {
			case <-done:
				result <- peak
				return
			case <-ticker.C:
			}
		}
	}()
	return func() uint64 { close(done); return <-result }
}

// requalLive returns the live heap bytes after a full collection.
func requalLive() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// requalStage runs step and logs its time, its peak heap, and the live
// heap after it, in MiB.
func requalStage(t *testing.T, name string, step func()) {
	t.Helper()
	before := requalLive()
	stop := requalPeak()
	started := time.Now()
	step()
	elapsed := time.Since(started)
	peak := stop()
	t.Logf("cost %s elapsed=%.3fs live-before=%.1fMiB peak-heap=%.1fMiB live-after=%.1fMiB", name, elapsed.Seconds(),
		float64(before)/(1<<20), float64(peak)/(1<<20), float64(requalLive())/(1<<20))
}

// TestExpressRequalCost measures the encode and decode time and the heap
// of the widest Express assets that TestExpressWidestSaveAdapters,
// TestExpressWidestStreamAdapters and TestExpressWidestTopologyHTTPAdapters
// export. It then keeps the accepted reference frame while it decodes and
// applies a replacement delta, and while it decodes the HTTP state. The
// reference frame of the export has the speed of the stream fixture, so the
// test sets the playback speed 60 before the assembler reads it.
func TestExpressRequalCost(t *testing.T) {
	dir := os.Getenv("PODSIM_EXPRESS_PUBLIC_ASSET_DIR")
	if dir == "" || raceEnabled {
		t.Skip("external widest assets are not requested")
	}
	read := func(name string) []byte {
		data, err := os.ReadFile(dir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	saved := read("save-modern.json.gz")
	var file stateFile
	requalStage(t, "save-decode", func() {
		var err error
		file, err = decodeStateFile(saved)
		must(err)
		must(file.resolveBoardings())
	})
	requalStage(t, "save-encode", func() {
		data, err := new(stateEncoder).encode(file)
		must(err)
		t.Logf("cost save raw=%d gzip=%d", len(decompressTestJSON(t, data)), len(data))
	})
	file, saved = stateFile{}, nil
	for _, name := range []string{"full", "delta"} {
		compressed := read(name + ".json.gz")
		var envelope StreamEnvelope
		requalStage(t, name+"-decode", func() {
			raw, err := InflateStream(compressed)
			must(err)
			envelope, err = DecodeStreamJSON(raw)
			must(err)
		})
		requalStage(t, name+"-encode", func() {
			data, err := encodeStream(envelope)
			must(err)
			t.Logf("cost %s gzip=%d", name, len(data))
		})
	}

	var topology TopologySnapshot
	must(json.Unmarshal(read("topology.json"), &topology))
	reference := read("reference-full.json")
	var candidate StreamFrame
	var first State
	var assembler *StreamAssembler
	var stream string
	var sequence uint64
	requalStage(t, "reference-full-decode-assemble", func() {
		decoded, err := DecodeStreamJSON(reference)
		must(err)
		decoded.Full.State.Speed = 60
		candidate, err = ApplyStream(StreamFrame{}, "", 0, decoded)
		must(err)
		assembler, err = NewStreamAssembler(topology)
		must(err)
		first, err = assembler.State(candidate)
		must(err)
		stream, sequence = decoded.Stream, decoded.Sequence
	})
	reference = nil
	// The successor and its delta go out of scope before the measured
	// stage, so that only the encoded delta stays.
	deltaRaw := func() []byte {
		successor := expressReferenceSuccessor(candidate)
		delta, err := makeDelta(candidate, successor)
		must(err)
		raw, err := EncodeStreamJSON(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: stream, Sequence: sequence + 1, Base: sequence, Source: sourceOf(successor), Build: successor.State.Build, Delta: &delta})
		must(err)
		return raw
	}()
	t.Logf("cost replacement-delta raw=%d", len(deltaRaw))
	var second State
	requalStage(t, "replacement-with-predecessor", func() {
		replacement, decodeErr := DecodeStreamJSON(deltaRaw)
		must(decodeErr)
		applied, applyErr := ApplyStream(candidate, stream, sequence, replacement)
		must(applyErr)
		var stateErr error
		second, stateErr = assembler.State(applied)
		must(stateErr)
	})
	withPredecessor := requalLive()
	if len(first.Simulation.Pending) != 8600 || len(second.Simulation.Pending) != 8599 {
		t.Fatal("the replacement changed the retained predecessor")
	}
	candidate, first = StreamFrame{}, State{}
	withoutPredecessor := requalLive()
	t.Logf("cost previous-frame-retention live-with=%.1fMiB live-without=%.1fMiB retained=%.1fMiB", float64(withPredecessor)/(1<<20),
		float64(withoutPredecessor)/(1<<20), (float64(withPredecessor)-float64(withoutPredecessor))/(1<<20))
	httpRaw := read("http.json")
	requalStage(t, "http-decode-with-stream-state", func() {
		state, decodeErr := DecodeStateJSON(httpRaw)
		must(decodeErr)
		if len(state.Simulation.Vehicles) != 300 {
			t.Fatal("the HTTP state lost vehicles")
		}
	})
	runtime.KeepAlive(assembler)
	runtime.KeepAlive(second)
}
