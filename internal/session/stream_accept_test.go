package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// streamChainScans runs the scans of decodeMarkedJSON in their order, with
// no typed decode, which could hide a scan that the pass answers wrongly.
func streamChainScans(data []byte) error {
	markers, err := scanRootMarkers(data)
	if err != nil {
		return err
	}
	express := markers.order == sim.ExpressOrderContract
	for _, scan := range []func() error{
		func() error { return scanContractMarkers(data, express) },
		func() error { return scanPackedOrders(data) },
		func() error { return scanStreamBoardingMembers(data, markers) },
		func() error { return scanStreamServiceMembers(data) },
		func() error { return scanIncidentMembers(data) },
		func() error { return scanFaultMembers(data) },
		func() error { return scanEmergencyMembers(data) },
	} {
		if err := scan(); err != nil {
			return err
		}
	}
	if express {
		return prescanJSON(data, streamLimits(markers))
	}
	return nil
}

// checkAcceptImpliesChain checks the guarantee of streamScansAccept on one
// document, and reports whether the pass accepts it. DecodeStreamJSON must
// also give the result of the chain.
func checkAcceptImpliesChain(t *testing.T, name string, doc []byte) bool {
	t.Helper()
	if !streamScansAccept(doc) {
		return false
	}
	if err := streamChainScans(doc); err != nil {
		t.Errorf("%s: the pass accepts a document that the chain refuses: %v\n%s", name, err, clipDocument(doc))
		return true
	}
	var want StreamEnvelope
	wantErr := decodeMarkedJSON(doc, &want)
	got, gotErr := DecodeStreamJSON(doc)
	if fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
		t.Errorf("%s: the decode gives %v, the chain gives %v\n%s", name, gotErr, wantErr, clipDocument(doc))
	} else if wantErr == nil && !reflect.DeepEqual(got, want) {
		t.Errorf("%s: the decode and the chain give different envelopes\n%s", name, clipDocument(doc))
	}
	return true
}

func clipDocument(doc []byte) []byte {
	return doc[:min(len(doc), 400)]
}

// jsonRepeat returns count copies of element, separated with commas.
func jsonRepeat(element string, count int) string {
	return strings.TrimSuffix(strings.Repeat(element+",", count), ",")
}

func acceptDeltaVehicle(member string) string {
	return `{"delta":{"vehicles":[{` + member + `}]}}`
}

func acceptFullVehicle(member string) string {
	return `{"full":{"state":{"simulation":{"vehicles":[{` + member + `}]}}}}`
}

func acceptOrder(member string) string {
	return `{"delta":{"groups":{"pending":[{` + member + `}]}}}`
}

func acceptMotion(value string) string {
	return acceptDeltaVehicle(`"motion":{"value":` + value + `}`)
}

func acceptMembers(count int) string {
	names := make([]string, count)
	for i := range names {
		names[i] = `"m` + strconv.Itoa(i) + `":0`
	}
	return `{` + strings.Join(names, ",") + `}`
}

// acceptMarked is a document with the Express root marker first.
func acceptMarked(rest string) string {
	return `{"orderContract":"express-v1",` + rest + `}`
}

// acceptBS is the backslash of a JSON escape.
const acceptBS = "\x5c"

const (
	acceptMotionOK = `{"position":{"x":-1,"y":0},"laneDistance":0,"speed":1.5e-7}`
	acceptRecordOK = `{"berthID":"b","metersAtBoarding":0}`
)

// acceptRefusals has one document for each check of the pass. The pass must
// not accept any of them. chain tells whether the chain refuses the
// document: each row with false pins a rule that is stricter than its scan,
// and needs no refusal to hold the guarantee.
var acceptRefusals = []struct {
	name  string
	doc   string
	chain bool
}{
	{"duplicate name", `{"a":1,"a":2}`, true},
	{"duplicate name in an array", `{"a":[{"b":1,"b":2}]}`, true},
	{"invalid UTF-8", "{\"a\":\"\xff\"}", true},
	{"invalid UTF-8 in a name", "{\"\xff\":1}", true},
	{"root array", `[]`, true},
	{"root scalar", `1`, true},
	{"root null", `null`, false},
	{"second root value", `{} {}`, true},
	{"trailing text", `{} x`, true},
	{"cut document", `{"a":1`, true},
	{"cut after a name", `{"a"`, true},
	{"empty document", ``, true},
	{"escaped refused name", `{"text` + acceptBS + `u0045ncoding":"x"}`, true},
	{"escaped order name", `{"delta":{"groups":{"pending":[{"fro` + acceptBS + `u006d":"!"}]}}}`, true},
	{"escaped container name", `{"rider` + acceptBS + `u0073":[{"from":"!"}]}`, true},
	{"escaped marker", `{"orderContract":"express` + acceptBS + `u002dv1"}`, false},
	{"escaped class", `{"class":"compac` + acceptBS + `u0074"}`, false},
	{"escaped consent", `{"sharingConsent":"share` + acceptBS + `u0064"}`, false},
	{"escaped service", `{"service":"expres` + acceptBS + `u0073"}`, false},

	{"root marker of another contract", `{"orderContract":"other"}`, true},
	{"root marker null", `{"orderContract":null}`, true},
	{"root marker number", `{"orderContract":1}`, true},
	{"root marker object", `{"orderContract":{}}`, true},
	{"nested marker without a root marker", `{"a":{"orderContract":"express-v1"}}`, true},
	{"first nested marker without a root marker", `{"delta":{"orderContract":"express-v1"},"a":1}`, true},
	{"nested marker of another contract", acceptMarked(`"a":{"orderContract":"other"}`), true},
	{"text encoding", `{"textEncoding":"x"}`, true},
	{"nested text encoding", `{"a":[{"textEncoding":1}]}`, true},
	{"late marker", `{"a":1,"orderContract":"express-v1"}`, false},
	{"late marker after a long fleet", `{"delta":{"vehicles":[` + jsonRepeat(`{}`, sim.MaxExpressPods+1) + `]},"orderContract":"express-v1"}`, true},

	{"depth", `{"a":` + strings.Repeat("[", 64) + strings.Repeat("]", 64) + `}`, true},
	{"members", acceptMembers(257), true},
	{"elements", `{"a":[` + jsonRepeat("0", 65537) + `]}`, true},
	{"integer above", `{"a":9007199254740992}`, true},
	{"integer below", `{"a":-9007199254740992}`, true},
	{"integer in an array", `{"a":[9007199254740992]}`, true},
	{"seed integer", `{"seed":9007199254740992}`, false},
	{"plain fleet", `{"delta":{"vehicles":[` + jsonRepeat(`{}`, project.MaxPods+1) + `]}}`, true},
	{"Express fleet", acceptMarked(`"delta":{"vehicles":[` + jsonRepeat(`{}`, sim.MaxExpressPods+1) + `]}`), true},
	{"plain riders", acceptDeltaVehicle(`"riders":{"value":[` + jsonRepeat(`{}`, 9) + `]}`), true},
	{"Express riders", acceptMarked(`"delta":{"vehicles":[{"riders":{"value":[` + jsonRepeat(`{}`, 21) + `]}}]}`), true},
	{"empty route bound", acceptFullVehicle(`"routeLaneIDs":[1]`), true},
	{"object at the fleet path", `{"delta":{"vehicles":{"0":{"motion":null}}}}`, true},
	{"object at a bound path", `{"full":{"routes":{"*":{"lanes":[` + jsonRepeat("0", sim.MotionRouteLimit+1) + `]}}}}`, true},

	{"motion null", acceptDeltaVehicle(`"motion":null`), true},
	{"motion array", acceptDeltaVehicle(`"motion":[]`), true},
	{"motion without value", acceptDeltaVehicle(`"motion":{}`), true},
	{"motion with another member", acceptDeltaVehicle(`"motion":{"value":` + acceptMotionOK + `,"other":1}`), true},
	{"motion value null", acceptMotion(`null`), true},
	{"motion value with another member", acceptMotion(`{"position":{"x":0,"y":0},"laneDistance":0,"speed":0,"other":0}`), true},
	{"motion without speed", acceptMotion(`{"position":{"x":0,"y":0},"laneDistance":0}`), true},
	{"motion without lane distance", acceptMotion(`{"position":{"x":0,"y":0},"speed":0}`), true},
	{"motion without position", acceptMotion(`{"laneDistance":0,"speed":0}`), true},
	{"motion position null", acceptMotion(`{"position":null,"laneDistance":0,"speed":0}`), true},
	{"motion without y", acceptMotion(`{"position":{"x":0},"laneDistance":0,"speed":0}`), true},
	{"motion x null", acceptMotion(`{"position":{"x":null,"y":0},"laneDistance":0,"speed":0}`), true},
	{"motion y text", acceptMotion(`{"position":{"x":0,"y":"0"},"laneDistance":0,"speed":0}`), true},
	{"motion speed null", acceptMotion(`{"position":{"x":0,"y":0},"laneDistance":0,"speed":null}`), true},
	{"motion negative speed", acceptMotion(`{"position":{"x":0,"y":0},"laneDistance":0,"speed":-1}`), true},
	{"motion negative lane distance", acceptMotion(`{"position":{"x":0,"y":0},"laneDistance":-1,"speed":0}`), true},
	{"motion x above the range", acceptMotion(`{"position":{"x":1e999,"y":0},"laneDistance":0,"speed":0}`), true},
	{"motion x below the range", acceptMotion(`{"position":{"x":-1e999,"y":0},"laneDistance":0,"speed":0}`), true},

	{"full boardings null", acceptFullVehicle(`"boardings":null`), true},
	{"full boardings empty", acceptFullVehicle(`"boardings":[]`), true},
	{"frame boardings empty", `{"frame":{"state":{"simulation":{"vehicles":[{"boardings":[]}]}}}}`, true},
	{"full boardings object", acceptFullVehicle(`"boardings":{"value":[]}`), true},
	{"plain boardings", acceptFullVehicle(`"boardings":[` + jsonRepeat(acceptRecordOK, 9) + `]`), true},
	{"Express boardings", acceptMarked(`"full":{"state":{"simulation":{"vehicles":[{"boardings":[` + jsonRepeat(acceptRecordOK, 21) + `]}]}}}`), true},
	{"delta boardings array", acceptDeltaVehicle(`"boardings":[]`), true},
	{"delta boardings without value", acceptDeltaVehicle(`"boardings":{}`), true},
	{"delta boardings another member", acceptDeltaVehicle(`"boardings":{"value":[],"other":1}`), true},
	{"delta boardings value null", acceptDeltaVehicle(`"boardings":{"value":null}`), true},
	{"record null", acceptFullVehicle(`"boardings":[null]`), true},
	{"record without meters", acceptFullVehicle(`"boardings":[{"berthID":"b"}]`), true},
	{"record without berth", acceptFullVehicle(`"boardings":[{"metersAtBoarding":0}]`), true},
	{"record with another member", acceptFullVehicle(`"boardings":[{"berthID":"b","metersAtBoarding":0,"other":0}]`), true},
	{"record berth null", acceptFullVehicle(`"boardings":[{"berthID":null,"metersAtBoarding":0}]`), true},
	{"record berth empty", acceptFullVehicle(`"boardings":[{"berthID":"","metersAtBoarding":0}]`), true},
	{"record berth long", acceptFullVehicle(`"boardings":[{"berthID":"` + strings.Repeat("b", 65) + `","metersAtBoarding":0}]`), true},
	{"record meters null", acceptFullVehicle(`"boardings":[{"berthID":"b","metersAtBoarding":null}]`), true},
	{"record meters negative", acceptFullVehicle(`"boardings":[{"berthID":"b","metersAtBoarding":-1}]`), true},
	{"record meters text", acceptFullVehicle(`"boardings":[{"berthID":"b","metersAtBoarding":"0"}]`), true},

	{"version text", `{"projectVersion":"` + strconv.Itoa(project.CurrentVersion) + `"}`, true},
	{"version null", `{"projectVersion":null}`, true},
	{"version old", `{"a":{"projectVersion":` + strconv.Itoa(project.CurrentVersion+1) + `}}`, true},
	{"class unknown", `{"class":"nothing"}`, true},
	{"class empty", `{"class":""}`, true},
	{"class null", `{"class":null}`, true},
	{"class number", `{"class":1}`, true},
	{"consent unknown", `{"sharingConsent":"nothing"}`, true},
	{"consent empty", `{"sharingConsent":""}`, true},
	{"consent null", `{"sharingConsent":null}`, true},
	{"service unknown", `{"service":"nothing"}`, true},
	{"service empty", `{"service":""}`, true},
	{"service array", `{"service":[]}`, true},
	{"service ID empty", `{"serviceID":""}`, true},
	{"service ID number", `{"serviceID":1}`, true},

	{"order from text", acceptOrder(`"from":"!"`), true},
	{"order to null", acceptOrder(`"to":null`), true},
	{"order pod object", acceptOrder(`"podID":{}`), true},
	{"order reason number", acceptOrder(`"dispatchReason":1`), true},
	{"order reason not canonical", acceptOrder(`"dispatchReason":"QQ"`), true},
	{"order ID with a reason byte", acceptOrder(`"from":"ICA="`), true},
	{"order reason control character", acceptOrder(`"dispatchReason":"AQ=="`), true},
	{"order service ID text", acceptOrder(`"serviceID":"!"`), true},
	{"order leg origin text", acceptOrder(`"legFrom":"!"`), true},
	{"order leg origin number", acceptOrder(`"legFrom":1`), true},
	{"order leg origin object", acceptOrder(`"legFrom":{}`), true},
	{"order below riders", `{"riders":[{"from":"!"}]}`, true},
	{"order directly below riders", `{"riders":{"from":"!"}}`, true},
	{"order below a nested object", `{"pending":{"a":[{"b":{"to":"!"}}]}}`, true},
	{"order below waiting and request", `{"waiting":[{"request":{"from":"!"}}]}`, true},
	{"order below waiting only", `{"waiting":[{"from":"!"}]}`, false},
	{"order below request only", `{"request":{"from":"!"}}`, false},

	{"leg origin null", `{"legFrom":null}`, false},
	{"leg origin empty", `{"legFrom":""}`, false},
	{"order leg origin null", acceptOrder(`"legFrom":null`), true},
	{"order leg origin empty", acceptOrder(`"legFrom":""`), true},
	{"incident group null", `{"delta":{"groups":{"incident":null}}}`, true},
	{"incident group empty", `{"delta":{"groups":{"incident":""}}}`, true},
	{"interrupted null", `{"delta":{"groups":{"incident":{"interrupted":null}}}}`, true},
	{"passengers null", `{"full":{"state":{"simulation":{"interruptedPassengers":null}}}}`, true},
	{"withdrawn null", acceptFullVehicle(`"withdrawn":null`), true},
	{"operational empty", acceptFullVehicle(`"operational":""`), true},
	{"operational null", acceptDeltaVehicle(`"metadata":{"value":{"operational":null}}`), true},

	{"faults null", `{"delta":{"groups":{"faults":null}}}`, true},
	{"fault member null", `{"delta":{"groups":{"faults":{"counters":null}}}}`, true},
	{"fault record member null", `{"full":{"state":{"simulation":{"faults":{"active":[{"id":null}]}}}}}`, true},
	{"fault nested member null", `{"delta":{"groups":{"faults":{"a":{"b":[{"c":{"d":null}}]}}}}}`, true},
	{"fault element null", `{"delta":{"groups":{"faults":{"active":[null]}}}}`, false},
	{"emergencies null", `{"full":{"state":{"simulation":{"emergencies":null}}}}`, true},
	{"emergency member null", `{"delta":{"groups":{"emergencies":{"active":[{"podID":null}]}}}}`, true},
}

// TestStreamAcceptRefusals checks the documents of acceptRefusals.
func TestStreamAcceptRefusals(t *testing.T) {
	t.Parallel()
	for _, test := range acceptRefusals {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			doc := []byte(test.doc)
			if streamScansAccept(doc) {
				t.Errorf("the pass accepts %s", clipDocument(doc))
			}
			if err := streamChainScans(doc); (err != nil) != test.chain {
				t.Errorf("the chain refuses it: %v, want %v", err, test.chain)
			}
		})
	}
}

// TestStreamAcceptBoardingBound checks that the boarding bound of the pass
// is also a bound when the table of the array bound allows more, and that
// it is the bound of the boarding scan.
func TestStreamAcceptBoardingBound(t *testing.T) {
	t.Parallel()
	node := acceptNode{limit: [2]int64{1000, 1000}, bounded: true}
	node.needBoardingRecords(false)
	if want := [2]int64{boardingRecordsPlain, boardingRecordsExpress}; node.limit != want {
		t.Errorf("the bound is %v, want %v", node.limit, want)
	}
	records := func(count int) []byte { return []byte(`[` + jsonRepeat(acceptRecordOK, count) + `]`) }
	for _, test := range []struct {
		markers contractMarkers
		bound   int
	}{{contractMarkers{}, boardingRecordsPlain}, {contractMarkers{order: sim.ExpressOrderContract}, boardingRecordsExpress}} {
		target := boardingScanTarget{}
		if err := checkBoardingScanTarget(records(test.bound), target, test.markers); err != nil {
			t.Errorf("%d records: %v", test.bound, err)
		}
		if checkBoardingScanTarget(records(test.bound+1), target, test.markers) == nil {
			t.Errorf("the scan accepts %d records", test.bound+1)
		}
	}
}

// TestStreamAcceptDocuments has the documents that the pass must accept, so
// that a rule that is too strict shows here and not as a slow client.
func TestStreamAcceptDocuments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, doc string }{
		{"empty", `{}`},
		{"padding", " {\n} \t\n"},
		{"depth", `{"a":` + strings.Repeat("[", 63) + strings.Repeat("]", 63) + `}`},
		{"members", acceptMembers(256)},
		{"elements", `{"a":[` + jsonRepeat("0", 65536) + `]}`},
		{"largest integers", `{"a":9007199254740991,"b":-9007199254740991,"c":1e300,"d":1.5}`},
		{"escape in another string", `{"a":"` + acceptBS + `u0041"}`},
		{"escape in a berth", acceptFullVehicle(`"boardings":[{"berthID":"` + acceptBS + `u0041","metersAtBoarding":0}]`)},
		{"plain fleet", `{"delta":{"vehicles":[` + jsonRepeat(`{}`, project.MaxPods) + `]}}`},
		{"Express fleet", acceptMarked(`"delta":{"vehicles":[` + jsonRepeat(`{}`, sim.MaxExpressPods) + `]}`)},
		{"nested marker", acceptMarked(`"a":{"orderContract":"express-v1"}`)},
		{"plain riders", acceptDeltaVehicle(`"riders":{"value":[` + jsonRepeat(`{}`, 8) + `]}`)},
		{"Express riders", acceptMarked(`"delta":{"vehicles":[{"riders":{"value":[` + jsonRepeat(`{}`, 20) + `]}}]}`)},
		{"motion", acceptMotion(acceptMotionOK)},
		{"motion in another order", acceptMotion(`{"speed":0,"laneDistance":0,"position":{"y":-0,"x":1e300}}`)},
		{"full boardings", acceptFullVehicle(`"boardings":[` + jsonRepeat(acceptRecordOK, 8) + `]`)},
		{"Express boardings", acceptMarked(`"full":{"state":{"simulation":{"vehicles":[{"boardings":[` + jsonRepeat(acceptRecordOK, 20) + `]}]}}}`)},
		{"delta boardings empty", acceptDeltaVehicle(`"boardings":{"value":[]}`)},
		{"delta boardings", acceptDeltaVehicle(`"boardings":{"value":[` + acceptRecordOK + `]}`)},
		{"longest berth", acceptFullVehicle(`"boardings":[{"berthID":"` + strings.Repeat("b", 64) + `","metersAtBoarding":0}]`)},
		{"service members", `{"projectVersion":` + strconv.Itoa(project.CurrentVersion) + `,"class":"compact","sharingConsent":"shared","service":"express","serviceID":"s"}`},
		{"other consent and service", `{"sharingConsent":"private","service":"on-demand"}`},
		{"order", acceptOrder(`"from":"QQ==","to":"","podID":"","dispatchReason":"ICA=","serviceID":"QQ==","legFrom":"QQ=="`)},
		{"order member outside an order", `{"from":"!","to":1,"podID":{},"dispatchReason":null}`},
		{"incident members", `{"interrupted":1,"withdrawn":1,"operational":"a","legFrom":"a","incident":{}}`},
		{"null outside the fault members", `{"a":null,"delta":{"berths":null}}`},
		{"faults", `{"delta":{"groups":{"faults":{"active":[{"id":"i1.1"}],"counters":{}},"emergencies":{}}}}`},
		{"empty route bound", acceptFullVehicle(`"routeLaneIDs":[]`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if !checkAcceptImpliesChain(t, test.name, []byte(test.doc)) {
				t.Errorf("the pass is not sure of %s", clipDocument([]byte(test.doc)))
			}
		})
	}
}

// acceptSpan is the position of one value in a document.
type acceptSpan struct {
	path               string // the path, with "*" for each array index
	nameStart, nameEnd int    // the name of a member, or -1
	start, end         int
	kind               jsontext.Kind
	first              []byte // the first element of an array
}

// acceptSpans returns the first two values at each path of doc. It returns
// nil when doc is not one valid JSON value.
func acceptSpans(doc []byte) []acceptSpan {
	decoder := jsontext.NewDecoder(bytes.NewReader(doc), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	var spans []acceptSpan
	seen := map[string]int{}
	var walk func(path string, nameStart, nameEnd int) (int, int, bool)
	walk = func(path string, nameStart, nameEnd int) (int, int, bool) {
		span := acceptSpan{path: path, nameStart: nameStart, nameEnd: nameEnd, kind: decoder.PeekKind()}
		index := len(spans)
		spans = append(spans, span)
		switch span.kind {
		case jsontext.KindBeginObject, jsontext.KindBeginArray:
			if _, err := decoder.ReadToken(); err != nil {
				return 0, 0, false
			}
			span.start = int(decoder.InputOffset()) - 1
			for count := 0; ; count++ {
				kind := decoder.PeekKind()
				if kind == jsontext.KindEndObject || kind == jsontext.KindEndArray {
					break
				}
				childPath, childStart, childEnd := path+"/*", -1, -1
				if span.kind == jsontext.KindBeginObject {
					name, err := decoder.ReadValue()
					if err != nil {
						return 0, 0, false
					}
					childEnd = int(decoder.InputOffset())
					childStart = childEnd - len(name)
					childPath = path + "/" + string(name[1:len(name)-1])
				}
				start, end, ok := walk(childPath, childStart, childEnd)
				if !ok {
					return 0, 0, false
				}
				if count == 0 && span.kind == jsontext.KindBeginArray {
					span.first = doc[start:end]
				}
			}
			if _, err := decoder.ReadToken(); err != nil {
				return 0, 0, false
			}
			span.end = int(decoder.InputOffset())
		default:
			raw, err := decoder.ReadValue()
			if err != nil {
				return 0, 0, false
			}
			span.end = int(decoder.InputOffset())
			span.start = span.end - len(raw)
		}
		spans[index] = span
		return span.start, span.end, true
	}
	if _, _, ok := walk("", -1, -1); !ok {
		return nil
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		return nil
	}
	return slices.DeleteFunc(spans, func(span acceptSpan) bool {
		seen[span.path]++
		return seen[span.path] > 2
	})
}

func acceptSplice(doc []byte, start, end int, text ...string) []byte {
	out := slices.Grow(slices.Clone(doc[:start]), len(doc)-end+len(strings.Join(text, "")))
	for _, part := range text {
		out = append(out, part...)
	}
	return append(out, doc[end:]...)
}

// acceptMutation is a token mutation of one value of a document. It returns
// nil when it does not apply to the value.
type acceptMutation struct {
	name  string
	apply func(doc []byte, span acceptSpan) []byte
}

// acceptReplace replaces the values of the kinds. No kind means each kind.
func acceptReplace(text string, kinds ...jsontext.Kind) acceptMutation {
	return acceptMutation{"replace with " + text, func(doc []byte, span acceptSpan) []byte {
		if len(kinds) > 0 && !slices.Contains(kinds, span.kind) {
			return nil
		}
		return acceptSplice(doc, span.start, span.end, text)
	}}
}

// acceptInsert adds a first member to the objects.
func acceptInsert(member string) acceptMutation {
	return acceptMutation{"insert " + member, func(doc []byte, span acceptSpan) []byte {
		if span.kind != jsontext.KindBeginObject {
			return nil
		}
		separator := ","
		if span.end-span.start == 2 {
			separator = ""
		}
		return acceptSplice(doc, span.start+1, span.start+1, member, separator)
	}}
}

// acceptRename renames a member.
func acceptRename(name string) acceptMutation {
	return acceptMutation{"rename to " + name, func(doc []byte, span acceptSpan) []byte {
		if span.nameStart < 0 {
			return nil
		}
		return acceptSplice(doc, span.nameStart, span.nameEnd, `"`, name, `"`)
	}}
}

// acceptEnlarge repeats the first element of an array to count elements.
func acceptEnlarge(name string, count func(path string) int) acceptMutation {
	return acceptMutation{"enlarge to " + name, func(doc []byte, span acceptSpan) []byte {
		if span.kind != jsontext.KindBeginArray {
			return nil
		}
		element := "0"
		if span.first != nil {
			element = string(span.first)
		}
		total := count(span.path)
		if total*len(element) > 1<<20 {
			return nil
		}
		return acceptSplice(doc, span.start, span.end, "[", jsonRepeat(element, total), "]")
	}}
}

// acceptPastBound returns the size just past the bound of an array path in
// the table of the markers.
func acceptPastBound(markers contractMarkers) func(string) int {
	arrays := streamLimits(markers).arrays
	return func(path string) int {
		if bound, ok := arrays[path]; ok {
			return int(bound) + 1
		}
		return 65537
	}
}

// acceptMutations are the token mutations of the differential test: null a
// member, duplicate a member, change a type, enlarge an array, add a marker,
// damage text and numbers, nest deeply, and rename a member to each name
// that has a rule.
var acceptMutations = func() []acceptMutation {
	allMembers := []string{
		"orderContract", "textEncoding", "from", "to", "podID", "dispatchReason", "serviceID", "legFrom",
		"projectVersion", "class", "sharingConsent", "service", "incident", "interrupted", "interruptedPassengers",
		"withdrawn", "operational", "faults", "emergencies", "riders", "pending", "waiting", "request", "boardings",
		"motion", "value", "vehicles", "metadata", "seed", "berthID", "metersAtBoarding", "*", "0",
		"bOaRdInGs", "other",
	}
	strs, nums := jsontext.KindString, jsontext.KindNumber
	objs, arrs := jsontext.KindBeginObject, jsontext.KindBeginArray
	mutations := []acceptMutation{
		acceptReplace(`null`),
		acceptReplace(`true`),
		acceptReplace(`7`, strs, objs, arrs, jsontext.KindNull, jsontext.KindTrue, jsontext.KindFalse),
		acceptReplace(`"x"`, nums, objs, arrs, jsontext.KindNull, jsontext.KindTrue, jsontext.KindFalse),
		acceptReplace(`[]`, objs, strs, nums),
		acceptReplace(`{}`, arrs, strs, nums),
		acceptReplace(`""`, strs),
		acceptReplace(`"!!!!"`, strs),
		acceptReplace(`"QQ"`, strs),
		acceptReplace(`"QQ=="`, strs),
		acceptReplace(`"AQ=="`, strs),
		acceptReplace(`"Q`+acceptBS+`u0051=="`, strs),
		acceptReplace("\"\xff\"", strs),
		acceptReplace(`"`+strings.Repeat("Q", 65)+`"`, strs),
		acceptReplace(`-1`, nums),
		acceptReplace(`-0`, nums),
		acceptReplace(`0`, nums),
		acceptReplace(`1.5`, nums),
		acceptReplace(`9007199254740992`, nums),
		acceptReplace(`-9007199254740992`, nums),
		acceptReplace(`1e999`, nums),
		acceptReplace(`-1e999`, nums),
		acceptReplace(`1e-999`, nums),
		acceptReplace(strings.Repeat("[", 70) + strings.Repeat("]", 70)),
		acceptReplace(strings.Repeat(`{"a":`, 70) + "0" + strings.Repeat("}", 70)),
		acceptInsert(`"textEncoding":"x"`),
		acceptInsert(`"text` + acceptBS + `u0045ncoding":"x"`),
		acceptInsert(`"orderContract":"express-v1"`),
		acceptInsert(`"orderContract":"other"`),
		acceptInsert(`"orderContract":null`),
		acceptInsert(`"other":0`),
		acceptEnlarge("9", func(string) int { return 9 }),
		acceptEnlarge("21", func(string) int { return 21 }),
		acceptEnlarge("past the plain bound", acceptPastBound(contractMarkers{})),
		acceptEnlarge("past the Express bound", acceptPastBound(contractMarkers{order: sim.ExpressOrderContract})),
		{"duplicate", func(doc []byte, span acceptSpan) []byte {
			if span.nameStart < 0 {
				return nil
			}
			return acceptSplice(doc, span.nameStart, span.nameStart, string(doc[span.nameStart:span.end]), ",")
		}},
		{"remove", func(doc []byte, span acceptSpan) []byte {
			start, end := span.nameStart, span.end
			switch {
			case start < 0:
				return nil
			case end < len(doc) && doc[end] == ',':
				end++
			case doc[start-1] == ',':
				start--
			}
			return acceptSplice(doc, start, end)
		}},
		{"escape the name", func(doc []byte, span acceptSpan) []byte {
			if span.nameStart < 0 || span.nameEnd-span.nameStart < 3 {
				return nil
			}
			return acceptSplice(doc, span.nameStart+1, span.nameStart+2, fmt.Sprintf(acceptBS+"u%04x", doc[span.nameStart+1]))
		}},
	}
	for _, name := range allMembers {
		mutations = append(mutations, acceptRename(name))
	}
	return mutations
}()

// acceptDocumentMutations change the document as a whole.
var acceptDocumentMutations = []func(doc []byte) []byte{
	func(doc []byte) []byte { return slices.Concat(doc, []byte(" \n")) },
	func(doc []byte) []byte { return slices.Concat([]byte(" \n"), doc) },
	func(doc []byte) []byte { return slices.Concat(doc, []byte(" {}")) },
	func(doc []byte) []byte { return slices.Concat(doc, []byte("x")) },
	func(doc []byte) []byte { return doc[:len(doc)-1] },
	func(doc []byte) []byte { return slices.Concat([]byte("["), doc, []byte("]")) },
	func(doc []byte) []byte { return slices.Concat([]byte(`{"a":`), doc, []byte("}")) },
	func(doc []byte) []byte { return slices.Concat([]byte(`{"riders":`), doc, []byte("}")) },
	func(doc []byte) []byte { return slices.Concat([]byte(`{"faults":`), doc, []byte("}")) },
}

// acceptEnvelopeJSON encodes the full envelope of frame and the delta from
// base to frame.
func acceptEnvelopeJSON(t *testing.T, base, frame StreamFrame) (full, delta []byte) {
	t.Helper()
	return faultEnvelopeJSON(t, base, frame, "full"), faultEnvelopeJSON(t, base, frame, "delta")
}

type acceptDocument struct {
	name string
	raw  []byte
}

// streamAcceptCorpus returns real envelopes: rail-hub, london-central,
// Express, faults, emergencies, incident members, boarding records, and
// the golden files.
func streamAcceptCorpus(t *testing.T) []acceptDocument {
	t.Helper()
	var corpus []acceptDocument
	add := func(name string, full, delta []byte) {
		corpus = append(corpus, acceptDocument{name + "/full", full}, acceptDocument{name + "/delta", delta})
	}
	for _, file := range []string{"plain_full", "plain_delta", "express_full", "express_delta", "motion_delta"} {
		raw, err := os.ReadFile("testdata/stream_" + file + ".json")
		if err != nil {
			t.Fatal(err)
		}
		corpus = append(corpus, acceptDocument{"golden/" + file, raw})
	}
	full, delta := streamDecodeEnvelopes(t, scenarios.RailHub(), 240*sim.TicksPerSecond)
	add("rail-hub", full, delta)
	full, delta = streamDecodeEnvelopes(t, scenarios.LondonCentral(), 60*sim.TicksPerSecond)
	add("london-central", full, delta)

	_, faultBase, faultNext := faultStreamFrames(t)
	full, delta = acceptEnvelopeJSON(t, faultBase, faultNext)
	add("faults", full, delta)
	_, emergencyBase, emergencyNext := emergencyStreamFrames(t, true)
	full, delta = acceptEnvelopeJSON(t, emergencyBase, emergencyNext)
	add("emergencies", full, delta)
	_, incident := incidentStreamFrames(t)
	full, delta = acceptEnvelopeJSON(t, incident[0], withIncidentMembers(incident[1]))
	add("incident", full, delta)
	_, boarding := boardingStreamFixture(t)
	boardingNext := ownStreamBoardings(boarding)
	boardingNext.State.Revision++
	boardingNext.State.Simulation.Vehicles[0].Boardings = nil
	boardingNext.State.Simulation.Vehicles[0].Riders = nil
	full, delta = acceptEnvelopeJSON(t, boardingNext, boarding)
	add("boarding", full, delta)
	_, expressBase, expressNext := expressRequalChain(t)
	full, err := EncodeStreamJSON(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "full", Stream: "express", Sequence: 1, Build: expressNext.State.Build, Source: sourceOf(expressNext), Full: &expressNext})
	if err != nil {
		t.Fatal(err)
	}
	changes, err := makeDelta(expressBase, expressNext)
	if err != nil {
		t.Fatal(err)
	}
	delta, err = EncodeStreamJSON(StreamEnvelope{OrderContract: sim.ExpressOrderContract, Kind: "delta", Stream: "express", Sequence: 2, Base: 1, Build: expressNext.State.Build, Source: sourceOf(expressNext), Delta: &changes})
	if err != nil {
		t.Fatal(err)
	}
	add("express", full, delta)
	return corpus
}

// The bytes of mutants for each document of the differential test.
const (
	acceptMutantBytes      = 16 << 20
	acceptMutantBytesShort = 2 << 20
)

// TestStreamAcceptDifferential checks the guarantee of streamScansAccept on
// real envelopes and on token mutations of them: when the pass accepts a
// document, the chain accepts it, and the decode result is the result of
// the chain. Each real envelope must be accepted. A byte budget for each
// document bounds the run.
func TestStreamAcceptDifferential(t *testing.T) {
	t.Parallel()
	budget := acceptMutantBytes
	if testing.Short() || raceEnabled {
		budget = acceptMutantBytesShort
	}
	corpus := streamAcceptCorpus(t)
	var all []byte
	for _, document := range corpus {
		all = append(all, document.raw...)
	}
	for _, name := range []string{"motion", "boardings", "riders", "pending", "faults", "emergencies", "legFrom", "withdrawn", "operational", "orderContract", "class", "sharingConsent", "service", "serviceID", "dispatchReason"} {
		if !bytes.Contains(all, []byte(`"`+name+`"`)) {
			t.Errorf("no document of the corpus has the member %s", name)
		}
	}
	var mutants, accepted int
	for _, document := range corpus {
		if !checkAcceptImpliesChain(t, document.name, document.raw) {
			t.Errorf("%s: the pass is not sure of a real envelope", document.name)
		}
		for index, mutate := range acceptDocumentMutations {
			if checkAcceptImpliesChain(t, fmt.Sprintf("%s document mutation %d", document.name, index), mutate(document.raw)) {
				accepted++
			}
			mutants++
		}
		spans := acceptSpans(document.raw)
		if spans == nil {
			t.Fatalf("%s: no values", document.name)
		}
		// A shuffle with a fixed seed samples each mutation within the budget.
		pairs := make([][2]int, 0, len(spans)*len(acceptMutations))
		for s := range spans {
			for m := range acceptMutations {
				pairs = append(pairs, [2]int{s, m})
			}
		}
		random := rand.New(rand.NewPCG(1, uint64(len(document.raw))))
		random.Shuffle(len(pairs), func(i, j int) { pairs[i], pairs[j] = pairs[j], pairs[i] })
		spent := 0
		for _, pair := range pairs {
			if spent > budget {
				break
			}
			span, mutation := spans[pair[0]], acceptMutations[pair[1]]
			mutant := mutation.apply(document.raw, span)
			if mutant == nil {
				continue
			}
			spent += len(mutant)
			mutants++
			if checkAcceptImpliesChain(t, document.name+" "+span.path+" "+mutation.name, mutant) {
				accepted++
			}
			if t.Failed() {
				t.FailNow()
			}
		}
	}
	t.Logf("%d documents, %d mutants, %d accepted", len(corpus), mutants, accepted)
	if accepted == 0 || accepted == mutants {
		t.Errorf("the pass accepts %d of %d mutants", accepted, mutants)
	}
}

// FuzzStreamScansAccept checks the guarantee of streamScansAccept on a
// document, and on a token mutation of the document that op and at select.
func FuzzStreamScansAccept(f *testing.F) {
	f.Add([]byte(`{}`), uint16(0), uint16(0))
	for _, file := range []string{"plain_delta", "express_delta", "motion_delta"} {
		raw, err := os.ReadFile("testdata/stream_" + file + ".json")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw, uint16(len(file)), uint16(1))
	}
	_, delta := streamDecodeEnvelopes(f, scenarios.RailHub(), 120*sim.TicksPerSecond)
	f.Add(delta, uint16(3), uint16(5))
	for _, seed := range []string{
		acceptMotion(acceptMotionOK), acceptFullVehicle(`"boardings":[` + acceptRecordOK + `]`),
		acceptDeltaVehicle(`"boardings":{"value":[` + acceptRecordOK + `]},"metadata":{"value":{"operational":"a"}}`),
		acceptOrder(`"from":"QQ==","to":"","podID":"","dispatchReason":"ICA=","serviceID":"QQ==","legFrom":"QQ=="`),
		acceptMarked(`"delta":{"groups":{"faults":{"active":[{"id":"i1.1"}]},"emergencies":{"active":[]}}}`),
		`{"projectVersion":` + strconv.Itoa(project.CurrentVersion) + `,"class":"compact","sharingConsent":"shared","service":"express"}`,
	} {
		f.Add([]byte(seed), uint16(2), uint16(3))
	}
	for _, test := range acceptRefusals {
		if len(test.doc) <= 1<<12 {
			f.Add([]byte(test.doc), uint16(0), uint16(0))
		}
	}
	f.Fuzz(func(t *testing.T, doc []byte, op, at uint16) {
		if len(doc) > 1<<16 {
			t.Skip()
		}
		checkAcceptImpliesChain(t, "document", doc)
		spans := acceptSpans(doc)
		if len(spans) == 0 {
			return
		}
		mutation := acceptMutations[int(op)%len(acceptMutations)]
		if mutant := mutation.apply(doc, spans[int(at)%len(spans)]); len(mutant) <= 1<<20 && mutant != nil {
			checkAcceptImpliesChain(t, mutation.name, mutant)
		}
	})
}
