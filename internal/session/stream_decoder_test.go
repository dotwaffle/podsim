package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func decodeStreamJSONWithLegacyDecoder(data []byte, target any) error {
	if len(data) > MaxStreamJSON {
		return errors.New("state JSON too large")
	}
	if !jsontext.Value(data).IsValid() {
		return errors.New("invalid or duplicate state JSON")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing state JSON")
	}
	return nil
}

type streamDecodeTestTarget struct {
	Name  string
	New   func() any
	Valid []string
}

func streamDecodeTestTargets() []streamDecodeTestTarget {
	return []streamDecodeTestTarget{
		{"envelope", func() any { return &StreamEnvelope{} }, []string{`{"sequence":"42","source":{"epoch":"e"}}`}},
		{"controls", func() any { return &controlsGroup{} }, []string{`{"speed":5,"redistribution":true}`, `{"speed":15,"speed":2}`}},
		{"global", func() any { return &globalGroup{} }, []string{`{"submitted":7,"tick":123,"demoError":"x"}`}},
		{"statistics", func() any { return &streamStatistics{} }, []string{`{"passengerDistanceMeters":1.5}`}},
		{"demand", func() any { return &DemandState{} }, []string{`{"config":{"enabled":true,"perMinute":12}}`}},
		{"restore", func() any { return &RestoreInfo{} }, []string{`{"tier":"physical"}`}},
		{"checkpoints", func() any { return &[]Checkpoint{} }, []string{`[]`, `[{}]`}},
		{"pending", func() any { return &[]sim.Request{} }, []string{`[]`, `[{"id":1,"from":"a","to":"b"}]`}},
		{"ws-control", func() any {
			return &struct {
				Kind     string `json:"kind"`
				Stream   string `json:"stream"`
				Sequence string `json:"sequence"`
				Token    string `json:"token"`
			}{}
		}, []string{`{"kind":"ack","sequence":"1"}`}},
	}
}

// streamDecodeErrorsEqual checks public error values and typed byte offsets.
func streamDecodeErrorsEqual(legacy, candidate error) bool {
	if legacy == nil || candidate == nil {
		return legacy == nil && candidate == nil
	}
	if reflect.TypeOf(legacy) != reflect.TypeOf(candidate) || legacy.Error() != candidate.Error() {
		return false
	}
	var legacyType, candidateType *json.UnmarshalTypeError
	if errors.As(legacy, &legacyType) && errors.As(candidate, &candidateType) {
		return legacyType.Value == candidateType.Value && legacyType.Type == candidateType.Type &&
			legacyType.Offset == candidateType.Offset && legacyType.Struct == candidateType.Struct &&
			legacyType.Field == candidateType.Field && streamDecodeErrorsEqual(legacyType.Err, candidateType.Err)
	}
	var legacySyntax, candidateSyntax *json.SyntaxError
	if errors.As(legacy, &legacySyntax) && errors.As(candidate, &candidateSyntax) {
		return legacySyntax.Offset == candidateSyntax.Offset
	}
	return true
}

// The legacy decoder matches member names without case, so the parity
// inputs use exact names. The stream decoder refuses a member whose case
// differs from the declared name.
func TestStreamDecoderRefusesCaseVariants(t *testing.T) {
	t.Parallel()
	variants := map[string][]string{
		"envelope":    {`{"Kind":"full"}`, `{"kind":"full","Kind":"delta"}`, `{"source":{"Epoch":"e"}}`},
		"controls":    {`{"Speed":5}`, `{"speedreduction":{}}`},
		"global":      {`{"Tick":1}`, `{"demoerror":"x"}`},
		"statistics":  {`{"PassengerDistanceMeters":1.5}`},
		"demand":      {`{"Config":{}}`, `{"config":{"PerMinute":12}}`},
		"restore":     {`{"Tier":"physical"}`},
		"checkpoints": {`[{"ID":1}]`},
		"pending":     {`[{"ID":1}]`},
		"ws-control":  {`{"Kind":"ack"}`, `{"kind":"ack","Kind":"heartbeat"}`},
	}
	for _, target := range streamDecodeTestTargets() {
		if len(variants[target.Name]) == 0 {
			t.Errorf("no case variant for %s", target.Name)
		}
		for _, raw := range variants[target.Name] {
			if err := decodeStreamJSON([]byte(raw), target.New()); err == nil {
				t.Errorf("%s accepted %s", target.Name, raw)
			}
		}
	}
}

func TestStreamDecoderLegacyCompatibility(t *testing.T) {
	common := []string{`null`, `{}`, `[]`, `1`, `"x"`, `true`, `{"unknown":1}`, `{"Unknown":1}`, `{"kind":12}`, `{"speed":"bad"}`, `{"submitted":1,"tick":"bad","completed":3}`, `{"speed":5,"redistribution":"x"}`, `[{"id":1},{"id":"bad"}]`, `{"config":{"perMinute":12,"unknown":1}}`, `{"source":{"epoch":"e","Unknown":1}}`, `{} {}`, `{"kind":"x",}`, `{"kind":"\ud800"}`, `{"kind":"\uD83D\uDE00"}`, `{"kind":"\u0000"}`, `{"kind":"full","kind":"delta"}`, `{"source":{"epoch":"a","epoch":"b"}}`, `{"sequence":"01"}`, `{"sequence":""}`, `{"sequence":"+1"}`, `{"sequence":"-1"}`, `{"sequence":"1.5"}`, `{"sequence":"1e2"}`, `{"sequence":"18446744073709551616"}`, `{"kind":"delta","delta":{"groups":{"global": { "tick":3 },"pending":[ {"id":1} ]}}}`, `{"kind":"delta","delta":{"groups":{"custom":[ 1,2 ]}}}`, "{\"kind\":\"" + string([]byte{0xff}) + "\"}"}
	for _, target := range streamDecodeTestTargets() {
		t.Run(target.Name, func(t *testing.T) {
			for _, raw := range append(common, target.Valid...) {
				for _, prefix := range []string{"", " ", "\t\r\n  "} {
					for _, suffix := range []string{"", " \n"} {
						data := []byte(prefix + raw + suffix)
						a, b := target.New(), target.New()
						legacyErr, candidateErr := decodeStreamJSONWithLegacyDecoder(data, a), decodeStreamJSON(data, b)
						if !reflect.DeepEqual(a, b) || !streamDecodeErrorsEqual(legacyErr, candidateErr) {
							t.Fatalf("input %q oldtarget=%#v newtarget=%#v olderr=%#v newerr=%#v", data, a, b, legacyErr, candidateErr)
						}
					}
				}
			}
		})
	}
}

// TestStreamIntegerRange checks the stream integers at sim.MaxCounter and
// one above it: the integer scan of a document, a stream sequence in its
// text form, and the numbers of a fault ID. A demand seed is not a counter
// and can have 64 bits.
func TestStreamIntegerRange(t *testing.T) {
	t.Parallel()
	_, frame := streamFixture(t)
	frame.State.Demand.Config.Seed = math.MaxUint64
	envelope := StreamEnvelope{Kind: "full", Stream: "range", Sequence: sim.MaxCounter, Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
	raw, err := EncodeStreamJSON(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeStreamJSON(raw); err != nil {
		t.Fatal("refused the largest sequence and a seed of 64 bits:", err)
	}
	// The sequence is a string, so ApplyStream checks it. The tick is a
	// number, so the integer scan checks it.
	above := bytes.Replace(raw, []byte(`"tick":`), []byte(`"tick":9007199254740992,"x":`), 1)
	if bytes.Equal(above, raw) {
		t.Fatal("the document has no tick")
	}
	if _, err = DecodeStreamJSON(above); !errors.Is(err, errJSONIntegerRange) ||
		!strings.HasPrefix(err.Error(), "JSON integer is out of range: more than 9007199254740991 at byte ") {
		t.Fatalf("tick above the largest counter: %v", err)
	}
	if n, err := ParseStreamSequence("9007199254740991"); err != nil || n != sim.MaxCounter {
		t.Fatal("refused the largest stream sequence", err)
	}
	if _, err := ParseStreamSequence("9007199254740992"); err == nil || err.Error() != "invalid stream sequence" {
		t.Fatalf("stream sequence above the largest counter: %v", err)
	}
	for id, want := range map[string]bool{
		"i9007199254740991.9007199254740991": true,
		"i9007199254740992.1":                false,
		"i1.9007199254740992":                false,
	} {
		if _, ok := faultSerial(id); ok != want {
			t.Errorf("fault ID %s: valid %t, want %t", id, ok, want)
		}
	}
}
