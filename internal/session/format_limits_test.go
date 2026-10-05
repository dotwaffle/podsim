package session

import (
	"bytes"
	legacyJSON "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// assertExplicitArrayBounds fails when an array of data has no explicit
// limit in limits, so that only the general element limit bounds it. The
// limits are the table of the decoder that reads data.
func assertExplicitArrayBounds(t *testing.T, name string, data []byte, limits jsonLimits) {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	var missing []string
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if token.Kind() != jsontext.KindBeginArray {
			continue
		}
		path := arrayPath(decoder)
		if _, ok := limits.arrays[path]; !ok && !slices.Contains(missing, path) {
			missing = append(missing, path)
		}
	}
	if len(missing) != 0 {
		slices.Sort(missing)
		t.Errorf("%s: arrays without an explicit limit: %q", name, missing)
	}
}

// TestCouplingArraysHaveExplicitLimits walks each coupling phase fixture as
// a save, a full frame, a delta, an HTTP state and a coupling delta group.
// The maximum fixtures of the other formats call assertExplicitArrayBounds.
func TestCouplingArraysHaveExplicitLimits(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		t.Run("order="+string(order), func(t *testing.T) {
			t.Parallel()
			packed := order == sim.ExpressOrderContract
			for _, phase := range data.Frames {
				_, topology, frame := couplingStreamFixture(t, phase, order)
				input := couplingPhaseInput(t, data, phase)
				input.OrderContract, input.State.OrderContract = order, order
				file := couplingPhaseFile(t, input)
				file.OrderContract, file.TextEncoding = order, streamTextEncoding(order)
				assertExplicitArrayBounds(t, phase.Name+" save", decompressTestJSON(t, encodeTestState(t, file)), couplingSavedLimits(packed))
				full, err := EncodeStreamJSON(couplingFullEnvelope(frame))
				if err != nil {
					t.Fatal(err)
				}
				assertExplicitArrayBounds(t, phase.Name+" full", full, couplingStreamLimits(packed))
				_, empty := streamFixture(t)
				empty.State.Simulation.OrderContract = order
				empty.State.Simulation.CouplingContract = frame.State.Simulation.CouplingContract
				empty.State.Simulation.Vehicles = make([]VehicleFrame, len(frame.State.Simulation.Vehicles))
				empty.State.Simulation.Berths = make([]sim.BerthState, len(frame.State.Simulation.Berths))
				empty.Routes = make([]sim.RoutePresentation, len(frame.Routes))
				delta, err := makeDelta(empty, frame)
				if err != nil {
					t.Fatal(err)
				}
				envelope := couplingFullEnvelope(frame)
				envelope.Kind, envelope.Full, envelope.Delta, envelope.Base, envelope.Sequence = "delta", nil, &delta, 1, 2
				changed, err := EncodeStreamJSON(envelope)
				if err != nil {
					t.Fatal(err)
				}
				assertExplicitArrayBounds(t, phase.Name+" delta", changed, couplingStreamLimits(packed))
				assertExplicitArrayBounds(t, phase.Name+" coupling group", delta.Groups["coupling"], couplingStreamLimits(false))
				http, err := EncodeCouplingStateJSON(topology, frame)
				if err != nil {
					t.Fatal(err)
				}
				assertExplicitArrayBounds(t, phase.Name+" HTTP", http, couplingStreamLimits(packed))
			}
		})
	}
}

// familyLimits is the JSON form of a jsonLimits value.
type familyLimits struct {
	Depth            int              `json:"depth"`
	Elements         int64            `json:"elements"`
	Members          int64            `json:"members"`
	StringBytes      int              `json:"stringBytes"`
	AllowInvalidUTF8 bool             `json:"allowInvalidUTF8"`
	Arrays           map[string]int64 `json:"arrays"`
}

// TestLimitTablesNotLooser compares the table that decodes each family
// with the table that it replaces. testdata/family_limit_tables.json holds
// the tables of each family before savedLimits and streamLimits, from
// commit 163bbe1. A path that had no explicit limit had the general element
// limit.
func TestLimitTablesNotLooser(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/family_limit_tables.json")
	if err != nil {
		t.Fatal(err)
	}
	var previous map[string]familyLimits
	if err := json.Unmarshal(raw, &previous, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	plain, express := contractMarkers{}, contractMarkers{order: sim.ExpressOrderContract}
	coupling := contractMarkers{coupling: sim.CompactPairV1CouplingContract}
	expressCoupling := contractMarkers{order: sim.ExpressOrderContract, coupling: sim.CompactPairV1CouplingContract}
	merged := map[string]jsonLimits{
		"save 6":          serviceStateLimits(),
		"save 7":          expressSavedLimits(),
		"save 8":          couplingSavedLimits(false),
		"save 8 express":  couplingSavedLimits(true),
		"save header":     expressSavedLimits(),
		"hello 3":         unpackedStreamLimits(),
		"hello 4":         expressStreamLimits(),
		"hello 5":         couplingStreamLimits(false),
		"hello 5 express": couplingStreamLimits(true),
		"plain HTTP":      stateFrameLimits(),
	}
	if !slices.Equal(slices.Sorted(maps.Keys(previous)), slices.Sorted(maps.Keys(merged))) {
		t.Fatal("the families differ from the recorded families")
	}
	for name, old := range previous {
		now := merged[name]
		if now.depth > old.Depth || now.elements > old.Elements || now.members > old.Members ||
			old.StringBytes != 0 && (now.stringBytes == 0 || now.stringBytes > old.StringBytes) ||
			now.allowInvalidUTF8 && !old.AllowInvalidUTF8 {
			t.Errorf("%s: a general limit is looser than before", name)
		}
		paths := maps.Clone(old.Arrays)
		maps.Copy(paths, now.arrays)
		for path := range paths {
			before, ok := old.Arrays[path]
			if !ok {
				before = old.Elements
			}
			after, ok := now.arrays[path]
			if !ok {
				after = now.elements
			}
			if after > before {
				t.Errorf("%s: %s has limit %d, more than %d before", name, path, after, before)
			}
		}
	}
	// The decoder scans a state file with the Express table before it
	// reads the version, so that table must contain each other saved table.
	header := savedLimits(express)
	for _, markers := range []contractMarkers{plain, coupling, expressCoupling} {
		for path, bound := range savedLimits(markers).arrays {
			if header.arrays[path] < bound {
				t.Errorf("the header table limits %s to %d, less than %d for %+v", path, header.arrays[path], bound, markers)
			}
		}
	}
}

// rootMember is one member of a JSON object, in input order.
type rootMember struct {
	name  string
	value jsontext.Value
}

func splitObject(t *testing.T, raw []byte) []rootMember {
	t.Helper()
	decoder := jsontext.NewDecoder(bytes.NewReader(raw), jsontext.AllowDuplicateNames(true))
	if start, err := decoder.ReadToken(); err != nil || start.Kind() != jsontext.KindBeginObject {
		t.Fatalf("not a JSON object: %v", err)
	}
	var members []rootMember
	for decoder.PeekKind() != jsontext.KindEndObject {
		token, err := decoder.ReadToken()
		if err != nil {
			t.Fatal(err)
		}
		name := token.String()
		value, err := decoder.ReadValue()
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, rootMember{name, value.Clone()})
	}
	return members
}

func joinObject(t *testing.T, members []rootMember) []byte {
	t.Helper()
	raw := []byte{'{'}
	for i, member := range members {
		if i > 0 {
			raw = append(raw, ',')
		}
		var err error
		if raw, err = jsontext.AppendQuote(raw, member.name); err != nil {
			t.Fatal(err)
		}
		raw = append(append(raw, ':'), member.value...)
	}
	return append(raw, '}')
}

// dropMember removes the member at path, a list of member names.
func dropMember(t *testing.T, raw []byte, path ...string) []byte {
	t.Helper()
	members := splitObject(t, raw)
	for i, member := range members {
		if member.name != path[0] {
			continue
		}
		if len(path) == 1 {
			return joinObject(t, slices.Delete(members, i, i+1))
		}
		members[i].value = dropMember(t, member.value, path[1:]...)
		return joinObject(t, members)
	}
	t.Fatalf("no member %q", path[0])
	return nil
}

// markerForms changes the root orderContract marker of a document.
var markerForms = []struct {
	name string
	edit func([]rootMember, int) []rootMember
}{
	{"first", func(members []rootMember, i int) []rootMember {
		marker := members[i]
		return slices.Insert(slices.Delete(members, i, i+1), 0, marker)
	}},
	{"last", func(members []rootMember, i int) []rootMember {
		marker := members[i]
		return append(slices.Delete(members, i, i+1), marker)
	}},
	{"omitted", func(members []rootMember, i int) []rootMember { return slices.Delete(members, i, i+1) }},
	{"duplicated", func(members []rootMember, i int) []rootMember { return slices.Insert(members, i, members[i]) }},
	{"null", func(members []rootMember, i int) []rootMember {
		members[i].value = jsontext.Value("null")
		return members
	}},
	{"empty", func(members []rootMember, i int) []rootMember {
		members[i].value = jsontext.Value(`""`)
		return members
	}},
	{"unknown", func(members []rootMember, i int) []rootMember {
		members[i].value = jsontext.Value(`"express-v2"`)
		return members
	}},
}

// markerFormat is a valid document with the Express marker and more
// orders than the plain table admits, with its decoder. selected reports a
// format whose table follows the marker. The version of the other formats
// requires the marker. Each contradiction decodes a document whose markers
// disagree.
type markerFormat struct {
	name           string
	raw            []byte
	decode         func([]byte) error
	selected       bool
	contradictions map[string]func() error
}

// TestMarkerFormsSelectLimits changes the root order marker of saves, full
// frames, deltas and HTTP states. Each form selects the table of its
// marker or is refused. No form selects the Express table without a valid
// Express marker.
func TestMarkerFormsSelectLimits(t *testing.T) {
	t.Parallel()
	for _, format := range markerFormats(t) {
		t.Run(format.name, func(t *testing.T) {
			t.Parallel()
			if err := format.decode(format.raw); err != nil {
				t.Fatalf("control: %v", err)
			}
			members := splitObject(t, format.raw)
			index := slices.IndexFunc(members, func(member rootMember) bool { return member.name == "orderContract" })
			if index < 0 {
				t.Fatal("control has no root order marker")
			}
			for _, form := range markerForms {
				err := format.decode(joinObject(t, form.edit(slices.Clone(members), index)))
				switch {
				case form.name == "first" || form.name == "last":
					if err != nil {
						t.Errorf("%s: %v", form.name, err)
					}
				case form.name == "omitted" && format.selected:
					if !errors.Is(err, errJSONArrayTooLong) {
						t.Errorf("%s: got %v, want the plain table", form.name, err)
					}
				case err == nil:
					t.Errorf("%s: accepted", form.name)
				}
			}
			for name, decode := range format.contradictions {
				if err := decode(); err == nil {
					t.Errorf("contradiction %s: accepted", name)
				}
			}
		})
	}
}

// markerFormats returns the documents of TestMarkerFormsSelectLimits. Each
// has one order more than the plain table admits.
func markerFormats(t *testing.T) []markerFormat {
	t.Helper()
	const count = maxSavedTrips + 1
	express := sim.ExpressOrderContract
	expressRequest := func(id int) sim.Request {
		return sim.Request{ID: id, From: "harbor", To: "market", PartySize: 20, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "harbor-market"}
	}
	// The stations are those of the coupling fixture. The save decoder
	// does not check them.
	couplingRequest := func(id int) sim.Request {
		return sim.Request{ID: id, From: "origin", To: "front-goal", PartySize: 1, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService}
	}
	trips := func() []sim.SavedTrip {
		waiting := make([]sim.SavedTrip, count)
		for i := range waiting {
			waiting[i].Request = sim.SavedRequest(couplingRequest(i + 1))
		}
		return waiting
	}
	pending := func(request func(int) sim.Request) []sim.Request {
		orders := make([]sim.Request, count)
		for i := range orders {
			orders[i] = request(i + 1)
		}
		return orders
	}
	decodeSave := func(raw []byte) error {
		_, err := decodeStateFile(compressTestJSON(t, raw))
		return err
	}
	// decodeStream decodes an envelope and binds it to its frame, or to
	// previous for a delta.
	decodeStream := func(version int, previous StreamFrame) func([]byte) error {
		return func(raw []byte) error {
			envelope, err := DecodeStreamJSONVersion(raw, version)
			if err != nil {
				return err
			}
			return validateEnvelopeContract(envelope, previous)
		}
	}
	marshal := func(value any) []byte {
		raw, err := json.Marshal(value, legacyJSON.DefaultOptionsV1(), packedRequestOptions())
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	encode := func(envelope StreamEnvelope) []byte {
		raw, err := EncodeStreamJSON(envelope)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	deltaOf := func(previous, next StreamFrame) StreamEnvelope {
		delta, err := makeDelta(previous, next)
		if err != nil {
			t.Fatal(err)
		}
		return StreamEnvelope{CouplingContract: next.State.Simulation.CouplingContract, OrderContract: express, TextEncoding: ExpressTextEncoding,
			Kind: "delta", Stream: "s", Sequence: 2, Base: 1, Source: sourceOf(next), Build: next.State.Build, Delta: &delta}
	}
	fullOf := func(frame StreamFrame) StreamEnvelope {
		return StreamEnvelope{CouplingContract: frame.State.Simulation.CouplingContract, OrderContract: express, TextEncoding: ExpressTextEncoding,
			Kind: "full", Stream: "s", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	}

	// Version 7 and hello 4 require the marker.
	shared := expressSession(t)
	saved := sessionStateFile(t, shared)
	saved.Version, saved.OrderContract, saved.TextEncoding = expressStateVersion, express, ExpressTextEncoding
	saved.Simulation.Waiting = trips()
	save7 := decompressTestJSON(t, encodeTestState(t, saved))
	topology, previous := expressGuardFrame(t)
	frame := previous
	frame.State.Simulation.Pending = pending(expressRequest)
	full4 := encode(fullOf(frame))
	delta4 := encode(deltaOf(previous, frame))
	frame.State.Speed = 60
	http4 := marshal(ExpressStateEnvelope{express, ExpressTextEncoding, topology, frame})

	// Version 8 and hello 5 select the table by the marker.
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	input.OrderContract, input.State.OrderContract = express, express
	coupled := couplingPhaseFile(t, input)
	coupled.OrderContract, coupled.TextEncoding = express, ExpressTextEncoding
	coupled.Simulation.Waiting = trips()
	save8 := decompressTestJSON(t, encodeTestState(t, coupled))
	_, couplingTopology, couplingPrevious := couplingStreamFixture(t, data.Frames[0], express)
	couplingFrame := couplingPrevious
	couplingFrame.State.Simulation.Pending = pending(couplingRequest)
	full5 := encode(fullOf(couplingFrame))
	delta5 := encode(deltaOf(couplingPrevious, couplingFrame))
	http5 := marshal(CouplingStateEnvelope{couplingTopology.CouplingContract, express, ExpressTextEncoding, couplingTopology, couplingFrame})

	plainPrevious, plainCouplingPrevious := previous, couplingPrevious
	plainPrevious.State.Simulation.OrderContract = ""
	plainCouplingPrevious.State.Simulation.OrderContract = ""
	decodeHTTP4 := func(raw []byte) error {
		_, err := DecodeExpressStateJSON(raw)
		return err
	}
	decodeHTTP5 := func(raw []byte) error {
		_, err := DecodeCouplingStateJSON(raw)
		return err
	}
	drop := func(decode func([]byte) error, raw []byte, path ...string) func() error {
		return func() error { return decode(dropMember(t, raw, path...)) }
	}
	full4Decode := decodeStream(ExpressStreamVersion, StreamFrame{})
	full5Decode := decodeStream(CouplingStreamVersion, StreamFrame{})
	return []markerFormat{
		{name: "save 7", raw: save7, decode: decodeSave, contradictions: map[string]func() error{
			"project":    drop(decodeSave, save7, "project", "orderContract"),
			"simulation": drop(decodeSave, save7, "simulation", "orderContract"),
		}},
		{name: "save 8", raw: save8, decode: decodeSave, selected: true, contradictions: map[string]func() error{
			"project":    drop(decodeSave, save8, "project", "orderContract"),
			"simulation": drop(decodeSave, save8, "simulation", "orderContract"),
		}},
		{name: "hello 4 full", raw: full4, decode: full4Decode, contradictions: map[string]func() error{
			"frame":   drop(full4Decode, full4, "full", "state", "simulation", "orderContract"),
			"hello 3": func() error { return decodeStream(FoundationStreamVersion, StreamFrame{})(full4) },
		}},
		{name: "hello 4 delta", raw: delta4, decode: decodeStream(ExpressStreamVersion, previous), contradictions: map[string]func() error{
			"plain frame": func() error { return decodeStream(ExpressStreamVersion, plainPrevious)(delta4) },
		}},
		{name: "hello 5 full", raw: full5, decode: full5Decode, selected: true, contradictions: map[string]func() error{
			"frame":   drop(full5Decode, full5, "full", "state", "simulation", "orderContract"),
			"hello 4": func() error { return decodeStream(ExpressStreamVersion, StreamFrame{})(full5) },
		}},
		{name: "hello 5 delta", raw: delta5, decode: decodeStream(CouplingStreamVersion, couplingPrevious), selected: true, contradictions: map[string]func() error{
			"plain frame": func() error { return decodeStream(CouplingStreamVersion, plainCouplingPrevious)(delta5) },
		}},
		{name: "Express HTTP", raw: http4, decode: decodeHTTP4, contradictions: map[string]func() error{
			"frame":    drop(decodeHTTP4, http4, "frame", "state", "simulation", "orderContract"),
			"topology": drop(decodeHTTP4, http4, "topology", "orderContract"),
		}},
		{name: "coupling HTTP", raw: http5, decode: decodeHTTP5, selected: true, contradictions: map[string]func() error{
			"frame":    drop(decodeHTTP5, http5, "frame", "state", "simulation", "orderContract"),
			"topology": drop(decodeHTTP5, http5, "topology", "orderContract"),
		}},
	}
}

// TestCheckpointArrayCeiling checks the checkpoints bound at each position
// of each stream family and of the plain HTTP state: checkpointLimit
// entries pass the scan, and one more fails it.
func TestCheckpointArrayCeiling(t *testing.T) {
	t.Parallel()
	list := func(count int) string {
		return "[" + strings.TrimSuffix(strings.Repeat("{},", count), ",") + "]"
	}
	documents := []string{`{"full":{"state":{"checkpoints":%s}}}`, `{"frame":{"state":{"checkpoints":%s}}}`, `{"delta":{"groups":{"checkpoints":%s}}}`}
	families := map[string]jsonLimits{
		"hello 3": unpackedStreamLimits(), "hello 4": expressStreamLimits(),
		"hello 5": couplingStreamLimits(false), "hello 5 express": couplingStreamLimits(true),
	}
	for name, limits := range families {
		for _, document := range documents {
			for _, count := range []int{checkpointLimit, checkpointLimit + 1} {
				err := prescanJSON(fmt.Appendf(nil, document, list(count)), limits)
				if (err == nil) != (count == checkpointLimit) || err != nil && !errors.Is(err, errJSONArrayTooLong) {
					t.Errorf("%s %s with %d checkpoints: %v", name, document, count, err)
				}
			}
		}
	}
	for _, count := range []int{checkpointLimit, checkpointLimit + 1} {
		err := prescanJSON(fmt.Appendf(nil, `{"checkpoints":%s}`, list(count)), stateFrameLimits())
		if (err == nil) != (count == checkpointLimit) || err != nil && !errors.Is(err, errJSONArrayTooLong) {
			t.Errorf("plain HTTP with %d checkpoints: %v", count, err)
		}
	}
}
