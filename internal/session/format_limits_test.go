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

// httpFrameLimits returns the stream limits of markers for the frame of an
// HTTP state, without the "/frame/state" prefix. The plain HTTP state of
// hello 3 sent that frame as the document root.
func httpFrameLimits(markers contractMarkers) jsonLimits {
	stream := streamLimits(markers)
	limits := stream
	limits.arrays = map[string]int64{}
	for path, bound := range stream.arrays {
		if rest, found := strings.CutPrefix(path, "/frame/state/"); found {
			limits.arrays["/"+rest] = bound
		}
	}
	return limits
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
// commit 163bbe1, with the fleet and order bounds of the fleet limit of
// 600 pods, and the station, node, lane and flow limits of the New York
// scale limits. A path that had no explicit limit had the general element
// limit. Version 9 replaced saves 6, 7 and 8. Its table for the markers
// of each earlier family must not be looser than the table of that family.
// The weights array of a flow, with 24 values, became the flow array, with
// the two station indexes and the weights, so the file records 26 values
// at /project/demandProfiles/*/flows/*.
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
	merged := map[string]jsonLimits{
		"save 6":      savedLimits(plain),
		"save 7":      savedLimits(express),
		"save header": savedLimits(express),
		"hello 3":     streamLimits(plain),
		"hello 4":     streamLimits(express),
		"plain HTTP":  httpFrameLimits(plain),
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
	for path, bound := range savedLimits(plain).arrays {
		if header.arrays[path] < bound {
			t.Errorf("the header table limits %s to %d, less than %d for %+v", path, header.arrays[path], bound, plain)
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
// orders than the plain table admits, with its decoder. Each contradiction
// decodes a document whose markers disagree.
type markerFormat struct {
	name           string
	raw            []byte
	decode         func([]byte) error
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
				case form.name == "omitted":
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
	// The save decoder does not check the stations.
	savedRequest := func(id int) sim.Request {
		return sim.Request{ID: id, From: "origin", To: "front-goal", PartySize: 1, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService}
	}
	trips := func() []sim.SavedTrip {
		waiting := make([]sim.SavedTrip, count)
		for i := range waiting {
			waiting[i].Request = sim.SavedRequest(savedRequest(i + 1))
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
	decodeStream := func(previous StreamFrame) func([]byte) error {
		return func(raw []byte) error {
			envelope, err := DecodeStreamJSON(raw)
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
		return StreamEnvelope{OrderContract: express,
			Kind: "delta", Stream: "s", Sequence: 2, Base: 1, Source: sourceOf(next), Build: next.State.Build, Delta: &delta}
	}
	fullOf := func(frame StreamFrame) StreamEnvelope {
		return StreamEnvelope{OrderContract: express,
			Kind: "full", Stream: "s", Sequence: 1, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	}

	// The save, the stream and the HTTP state select the table by the
	// marker.
	shared := expressSession(t)
	saved := sessionStateFile(t, shared)
	saved.OrderContract = express
	saved.Simulation.Waiting = trips()
	expressSave := decompressTestJSON(t, encodeTestState(t, saved))
	topology, previous := expressGuardFrame(t)
	frame := previous
	frame.State.Simulation.Pending = pending(expressRequest)
	expressFull := encode(fullOf(frame))
	expressDelta := encode(deltaOf(previous, frame))
	frame.State.Speed = 60
	expressHTTP := marshal(StateEnvelope{OrderContract: express, Topology: topology, Frame: frame})

	plainPrevious := previous
	plainPrevious.State.Simulation.OrderContract = ""
	decodeHTTP := func(raw []byte) error {
		_, err := DecodeStateJSON(raw)
		return err
	}
	drop := func(decode func([]byte) error, raw []byte, path ...string) func() error {
		return func() error { return decode(dropMember(t, raw, path...)) }
	}
	fullDecode := decodeStream(StreamFrame{})
	return []markerFormat{
		{name: "Express save", raw: expressSave, decode: decodeSave, contradictions: map[string]func() error{
			"project":    drop(decodeSave, expressSave, "project", "orderContract"),
			"simulation": drop(decodeSave, expressSave, "simulation", "orderContract"),
		}},
		{name: "Express full", raw: expressFull, decode: fullDecode, contradictions: map[string]func() error{
			"frame": drop(fullDecode, expressFull, "full", "state", "simulation", "orderContract"),
		}},
		{name: "Express delta", raw: expressDelta, decode: decodeStream(previous), contradictions: map[string]func() error{
			"plain frame": func() error { return decodeStream(plainPrevious)(expressDelta) },
		}},
		{name: "Express HTTP", raw: expressHTTP, decode: decodeHTTP, contradictions: map[string]func() error{
			"frame":    drop(decodeHTTP, expressHTTP, "frame", "state", "simulation", "orderContract"),
			"topology": drop(decodeHTTP, expressHTTP, "topology", "orderContract"),
		}},
	}
}

// TestCheckpointArrayCeiling checks the checkpoints bound at each position
// of the stream and HTTP state tables of each marker: checkpointLimit
// entries pass the scan, and one more fails it.
func TestCheckpointArrayCeiling(t *testing.T) {
	t.Parallel()
	list := func(count int) string {
		return "[" + strings.TrimSuffix(strings.Repeat("{},", count), ",") + "]"
	}
	documents := []string{`{"full":{"state":{"checkpoints":%s}}}`, `{"frame":{"state":{"checkpoints":%s}}}`, `{"delta":{"groups":{"checkpoints":%s}}}`}
	families := map[string]jsonLimits{
		"plain": streamLimits(contractMarkers{}), "Express": streamLimits(contractMarkers{order: sim.ExpressOrderContract}),
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
}
