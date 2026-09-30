package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
	"reflect"
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
		{"envelope", func() any { return &StreamEnvelope{} }, []string{`{"kind":"full","Kind":"delta"}`, `{"sequence":"42","source":{"epoch":"e"}}`}},
		{"controls", func() any {
			return &struct {
				Speed          int
				Redistribution bool
				SpeedReduction SpeedReduction
			}{}
		}, []string{`{"Speed":5,"Redistribution":true}`, `{"Speed":15,"speed":2}`}},
		{"global", func() any {
			return &struct {
				Submitted int
				Tick      int64
				Paused    bool
				Completed int
				Demo      bool
				DemoError string
			}{}
		}, []string{`{"Submitted":7,"Tick":123,"DemoError":"x"}`}},
		{"statistics", func() any { return &streamStatistics{} }, []string{`{"PassengerDistanceMeters":1.5}`}},
		{"demand", func() any { return &DemandState{} }, []string{`{"Config":{"Enabled":true,"PerMinute":12}}`}},
		{"restore", func() any { return &RestoreInfo{} }, []string{`{"Tier":"physical"}`}},
		{"checkpoints", func() any { return &[]Checkpoint{} }, []string{`[]`, `[{}]`}},
		{"pending", func() any { return &[]sim.Request{} }, []string{`[]`, `[{"ID":1,"From":"a","To":"b"}]`}},
		{"ws-control", func() any {
			return &struct {
				Kind     string `json:"kind"`
				Stream   string `json:"stream"`
				Sequence string `json:"sequence"`
				Token    string `json:"token"`
			}{}
		}, []string{`{"kind":"ack","sequence":"1"}`, `{"kind":"ack","Kind":"heartbeat"}`}},
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

func TestStreamDecoderLegacyCompatibility(t *testing.T) {
	common := []string{`null`, `{}`, `[]`, `1`, `"x"`, `true`, `{"unknown":1}`, `{"Unknown":1}`, `{"kind":12}`, `{"Speed":"bad"}`, `{"Submitted":1,"Tick":"bad","Completed":3}`, `{"Speed":5,"Redistribution":"x"}`, `[{"ID":1},{"ID":"bad"}]`, `{"Config":{"PerMinute":12,"Unknown":1}}`, `{"source":{"epoch":"e","Unknown":1}}`, `{} {}`, `{"kind":"x",}`, `{"kind":"\ud800"}`, `{"kind":"\uD83D\uDE00"}`, `{"kind":"\u0000"}`, `{"kind":"full","kind":"delta"}`, `{"source":{"epoch":"a","epoch":"b"}}`, `{"sequence":"01"}`, `{"sequence":""}`, `{"sequence":"+1"}`, `{"sequence":"-1"}`, `{"sequence":"1.5"}`, `{"sequence":"1e2"}`, `{"sequence":"18446744073709551616"}`, `{"kind":"delta","delta":{"groups":{"global": { "Tick":3 },"pending":[ {"ID":1} ]}}}`, `{"kind":"delta","delta":{"groups":{"custom":[ 1,2 ]}}}`, "{\"kind\":\"" + string([]byte{0xff}) + "\"}"}
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
