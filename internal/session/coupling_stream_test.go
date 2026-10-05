package session

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func couplingStreamFixture(t *testing.T, phase couplingPhaseFrame, order sim.OrderContract) (*Session, TopologySnapshot, StreamFrame) {
	t.Helper()
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, phase)
	input.OrderContract = order
	input.State.OrderContract = order
	file := couplingPhaseFile(t, input)
	file.OrderContract = order
	file.TextEncoding = streamTextEncoding(order)
	store := &fakeStore{data: encodeTestState(t, file)}
	s, err := NewFromStore(t.Context(), StoreInput{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	frame, err := s.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	topology := s.Topology()
	return s, topology, frame
}

func couplingFullEnvelope(frame StreamFrame) StreamEnvelope {
	return StreamEnvelope{CouplingContract: frame.State.Simulation.CouplingContract,
		OrderContract: frame.State.Simulation.OrderContract, TextEncoding: streamTextEncoding(frame.State.Simulation.OrderContract),
		Kind: "full", Stream: "coupling-test", Sequence: 1, Build: frame.State.Build, Source: sourceOf(frame), Full: &frame}
}

func TestCouplingStreamPhaseRoundTrips(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	for _, phase := range data.Frames {
		for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
			t.Run(fmt.Sprintf("%s/order=%s", phase.Name, order), func(t *testing.T) {
				t.Parallel()
				s, topology, frame := couplingStreamFixture(t, phase, order)
				assembler, err := NewStreamAssemblerVersion(topology, CouplingStreamVersion)
				if err != nil {
					t.Fatal(err)
				}
				envelope := couplingFullEnvelope(frame)
				raw, err := EncodeStreamJSON(envelope)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := DecodeStreamJSONVersion(raw, CouplingStreamVersion)
				if err != nil {
					t.Fatal("decode full", err)
				}
				encoded, err := EncodeStreamJSON(decoded)
				if err != nil || !bytes.Equal(raw, encoded) {
					t.Fatal("full bytes changed", err)
				}
				accepted, err := ApplyStream(StreamFrame{}, "", 0, decoded)
				if err != nil || !reflect.DeepEqual(accepted, frame) {
					t.Fatal("full changed frame facts", err)
				}
				state, err := assembler.State(accepted)
				if err != nil || !reflect.DeepEqual(state.Simulation.CouplingGroups, frame.State.Simulation.CouplingGroups) {
					t.Fatal("assembled full lost coherent trains", err)
				}
				httpRaw, err := EncodeCouplingStateJSON(topology, frame)
				if err != nil {
					t.Fatal("HTTP encoding", err)
				}
				httpState, err := DecodeCouplingStateJSON(httpRaw)
				if err != nil || !reflect.DeepEqual(httpState, state) {
					t.Fatal("HTTP and stream states differ", err)
				}
				client := newTestClient(s, "coupling-stream")
				client.mustApply(t, Command{Action: "pause", Paused: false})
				s.advance()
				next, err := s.presentationFrame()
				if err != nil {
					t.Fatal(err)
				}
				delta, err := makeDelta(frame, next)
				if err != nil {
					t.Fatal(err)
				}
				envelope.Kind, envelope.Full, envelope.Delta = "delta", nil, &delta
				envelope.Sequence, envelope.Base, envelope.Source = 2, 1, sourceOf(next)
				raw, err = EncodeStreamJSON(envelope)
				if err != nil {
					t.Fatal(err)
				}
				decoded, err = DecodeStreamJSONVersion(raw, CouplingStreamVersion)
				if err != nil {
					t.Fatal("decode delta", err)
				}
				applied, err := ApplyStream(accepted, envelope.Stream, 1, decoded)
				if err != nil || !reflect.DeepEqual(applied, next) {
					t.Fatal("delta changed frame facts", err)
				}
				if _, err := assembler.State(applied); err != nil {
					t.Fatal("assemble delta", err)
				}
				if !reflect.DeepEqual(state.Simulation.CouplingGroups, frame.State.Simulation.CouplingGroups) {
					t.Fatal("delta mutated the previous returned state")
				}
			})
		}
	}
}

func TestCouplingStreamAssemblerRetainsValidState(t *testing.T) { //nolint:tparallel // Subtests check one retained assembler in order.
	t.Parallel()
	data := couplingPhaseFixtures(t)
	var phase couplingPhaseFrame
	for _, candidate := range data.Frames {
		if candidate.State.CouplingGroups[0].Phase == sim.CouplingConnected {
			phase = candidate
			break
		}
	}
	_, topology, frame := couplingStreamFixture(t, phase, "")
	assembler, err := NewStreamAssemblerVersion(topology, CouplingStreamVersion)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := assembler.State(frame)
	if err != nil {
		t.Fatal(err)
	}
	before := ownAssemblerState(assembler.state)
	before.Simulation.CouplingGroups = cloneCouplingGroups(before.Simulation.CouplingGroups)
	previous := ownStreamBoardings(assembler.previous)
	for _, test := range []struct {
		name string
		edit func(*StreamFrame)
	}{
		{"body", func(f *StreamFrame) { f.State.Simulation.CouplingGroups[0].Bodies[0].Corners[0].X++ }},
		{"connector", func(f *StreamFrame) { f.State.Simulation.CouplingGroups[0].Connector.Corners[0].X++ }},
		{"speed", func(f *StreamFrame) { *f.State.Simulation.CouplingGroups[0].CommonSpeed++ }},
		{"membership", func(f *StreamFrame) { f.State.Simulation.CouplingGroups[0].Members[0] = "other" }},
		{"missing registry", func(f *StreamFrame) { f.State.Simulation.CouplingGroups = nil }},
		{"missing cabin binding", func(f *StreamFrame) { f.State.Simulation.Vehicles[0].CouplingID = "" }},
		{"wrong cabin binding", func(f *StreamFrame) { f.State.Simulation.Vehicles[0].CouplingID = "other" }},
		{"detach close cabins", func(f *StreamFrame) {
			f.State.Simulation.CouplingGroups = nil
			for i := range f.State.Simulation.Vehicles {
				f.State.Simulation.Vehicles[i].CouplingID = ""
			}
		}},
		{"formation", func(f *StreamFrame) { f.State.Simulation.CouplingGroups[0].FormationTick-- }},
		{"drain identity", func(f *StreamFrame) {
			f.State.Simulation.CouplingGroups[0].Progress.DrainFirstMember = 1 - f.State.Simulation.CouplingGroups[0].Progress.DrainFirstMember
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := ownStreamBoardings(frame)
			test.edit(&bad)
			if _, err := assembler.State(bad); err == nil {
				t.Fatal("invalid mechanical update replaced a valid state")
			}
			if !reflect.DeepEqual(before, assembler.state) || !reflect.DeepEqual(previous, assembler.previous) {
				t.Fatal("rejected candidate changed assembler storage")
			}
		})
	}
	accepted.Simulation.CouplingGroups[0].Bodies[0].Corners[0].X++
	*accepted.Simulation.CouplingGroups[0].CommonSpeed++
	accepted.Simulation.CouplingGroups[0].Connector.Corners[0].X++
	if !reflect.DeepEqual(before, assembler.state) {
		t.Fatal("returned state shares retained mechanical storage")
	}
	if _, err := assembler.State(frame); err != nil {
		t.Fatal("valid retry failed", err)
	}
}

func TestCouplingStreamReservedFieldsAndShape(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	_, _, frame := couplingStreamFixture(t, data.Frames[0], "")
	raw, err := EncodeStreamJSON(couplingFullEnvelope(frame))
	if err != nil {
		t.Fatal(err)
	}
	for version := FoundationStreamVersion; version < CouplingStreamVersion; version++ {
		if _, err := DecodeStreamJSONVersion(raw, version); err == nil {
			t.Fatal("old stream accepted physical train fields", version)
		}
	}
	for _, test := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{"missing root marker", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"couplingContract":"compact-pair-v1",`), nil, 1)
		}},
		{"null marker", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"couplingContract":"compact-pair-v1"`), []byte(`"couplingContract":null`), 1)
		}},
		{"folded duplicate", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"couplingContract":"compact-pair-v1"`), []byte(`"couplingContract":"compact-pair-v1","COUPLINGCONTRACT":"compact-pair-v1"`), 1)
		}},
		{"null cabin binding", func(raw []byte) []byte {
			member := append([]byte(`"couplingID":`), mustCouplingJSON(t, frame.State.Simulation.Vehicles[0].CouplingID)...)
			return bytes.Replace(raw, member, []byte(`"couplingID":null`), 1)
		}},
		{"folded duplicate cabin binding", func(raw []byte) []byte {
			value := mustCouplingJSON(t, frame.State.Simulation.Vehicles[0].CouplingID)
			member := append([]byte(`"couplingID":`), value...)
			double := append(append([]byte{}, member...), []byte(`,"COUPLINGID":`)...)
			return bytes.Replace(raw, member, append(double, value...), 1)
		}},
		{"null groups", func(raw []byte) []byte {
			var e StreamEnvelope
			if err := json.Unmarshal(raw, &e); err != nil {
				t.Fatal(err)
			}
			e.Full.State.Simulation.CouplingGroups = nil
			encoded, err := EncodeStreamJSON(e)
			if err != nil {
				t.Fatal(err)
			}
			return bytes.Replace(encoded, []byte(`"simulation":{"couplingContract":"compact-pair-v1"`), []byte(`"simulation":{"couplingContract":"compact-pair-v1","couplingGroups":null`), 1)
		}},
		{"short members", func(raw []byte) []byte {
			return bytes.Replace(raw, mustCouplingJSON(t, frame.State.Simulation.CouplingGroups[0].Members), []byte(`[]`), 1)
		}},
		{"short bodies", func(raw []byte) []byte {
			return bytes.Replace(raw, mustCouplingJSON(t, frame.State.Simulation.CouplingGroups[0].Bodies), []byte(`[]`), 1)
		}},
		{"short corners", func(raw []byte) []byte {
			return bytes.Replace(raw, mustCouplingJSON(t, frame.State.Simulation.CouplingGroups[0].Bodies[0].Corners), []byte(`[]`), 1)
		}},
		{"missing bodies", func(raw []byte) []byte {
			member := append([]byte(`,"bodies":`), mustCouplingJSON(t, frame.State.Simulation.CouplingGroups[0].Bodies)...)
			return bytes.Replace(raw, member, nil, 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeStreamJSONVersion(test.edit(raw), CouplingStreamVersion); err == nil {
				t.Fatal("accepted an incomplete mechanical wire shape")
			}
		})
	}
}

// streamFamilyFrames returns one frame of each stream family and a changed
// successor for deltas.
func streamFamilyFrames(t *testing.T) map[string][2]StreamFrame {
	t.Helper()
	frames := map[string][2]StreamFrame{}
	advance := func(s *Session, frame StreamFrame) [2]StreamFrame {
		t.Helper()
		s.Apply(Command{Client: "family", Sequence: 1, Epoch: frame.State.Epoch, Action: "pause", Paused: !frame.State.Simulation.Paused})
		next, err := s.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		return [2]StreamFrame{frame, next}
	}
	foundation, frame := streamFixture(t)
	t.Cleanup(foundation.Close)
	frames["foundation"] = advance(foundation, frame)
	express := expressSession(t)
	frame, err := express.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	frames["express"] = advance(express, frame)
	data := couplingPhaseFixtures(t)
	for name, order := range map[string]sim.OrderContract{"coupling raw": "", "coupling packed": sim.ExpressOrderContract} {
		s, _, frame := couplingStreamFixture(t, data.Frames[0], order)
		frames[name] = advance(s, frame)
	}
	return frames
}

func streamFamilyEnvelope(t *testing.T, frames [2]StreamFrame, kind string) StreamEnvelope {
	t.Helper()
	e := couplingFullEnvelope(frames[0])
	if kind == "full" {
		return e
	}
	delta, err := makeDelta(frames[0], frames[1])
	if err != nil {
		t.Fatal(err)
	}
	e.Kind, e.Full, e.Delta, e.Sequence, e.Base, e.Source = "delta", nil, &delta, 2, 1, sourceOf(frames[1])
	return e
}

func TestEncodeStreamJSONKeepsFamilyBytes(t *testing.T) {
	frames := streamFamilyFrames(t)
	for _, family := range slices.Sorted(maps.Keys(frames)) {
		for _, kind := range []string{"full", "delta"} {
			t.Run(family+"/"+kind, func(t *testing.T) {
				e := streamFamilyEnvelope(t, frames[family], kind)
				want, err := json.Marshal(e)
				if e.OrderContract == sim.ExpressOrderContract {
					want, err = jsonv2.Marshal(e, json.DefaultOptionsV1(), packedRequestOptions())
				}
				if err != nil {
					t.Fatal(err)
				}
				got, err := EncodeStreamJSON(e)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("encoder changed a valid publication", err)
				}
				if family == "foundation" || family == "express" {
					for _, member := range []string{"couplingContract", "couplingEnabled", "couplingGroups", "couplingID"} {
						if e.CouplingContract != "" || bytes.Contains(got, []byte(`"`+member+`"`)) {
							t.Fatal("unmarked family carries coupling fields", member)
						}
					}
				} else if e.CouplingContract != sim.CompactPairV1CouplingContract {
					t.Fatal("coupling family lost its marker")
				}
			})
		}
	}
}

func TestEncodeStreamJSONBindsCouplingMarker(t *testing.T) {
	frames := streamFamilyFrames(t)
	couplingGroup := json.RawMessage(`{"couplingContract":"compact-pair-v1","couplingEnabled":false,"couplingGroups":[]}`)
	tests := []struct {
		name   string
		family string
		kind   string
		edit   func(*StreamEnvelope)
		want   string
	}{
		{"unknown profile/full", "coupling raw", "full", func(e *StreamEnvelope) {
			e.CouplingContract = "unknown"
			e.Full.State.Simulation.CouplingContract = "unknown"
		}, sim.ErrUnknownCouplingContract.Error()},
		{"unknown profile/delta", "coupling packed", "delta", func(e *StreamEnvelope) { e.CouplingContract = "unknown" }, sim.ErrUnknownCouplingContract.Error()},
		{"marked envelope/unmarked raw full", "foundation", "full", func(e *StreamEnvelope) { e.CouplingContract = sim.CompactPairV1CouplingContract }, "publication coupling contract mismatch"},
		{"marked envelope/unmarked packed full", "express", "full", func(e *StreamEnvelope) { e.CouplingContract = sim.CompactPairV1CouplingContract }, "publication coupling contract mismatch"},
		{"unmarked envelope/marked raw full", "coupling raw", "full", func(e *StreamEnvelope) { e.CouplingContract = "" }, "publication coupling contract mismatch"},
		{"unmarked envelope/marked packed full", "coupling packed", "full", func(e *StreamEnvelope) { e.CouplingContract = "" }, "publication coupling contract mismatch"},
		{"unmarked envelope/raw delta coupling group", "coupling raw", "delta", func(e *StreamEnvelope) {
			e.CouplingContract = ""
			e.Delta.Groups["coupling"] = couplingGroup
		}, "unmarked publication contains coupling fields"},
		{"unmarked envelope/packed delta coupling group", "coupling packed", "delta", func(e *StreamEnvelope) {
			e.CouplingContract = ""
			e.Delta.Groups["coupling"] = couplingGroup
		}, "unmarked publication contains coupling fields"},
		{"unmarked envelope/raw delta cabin binding", "foundation", "delta", func(e *StreamEnvelope) {
			metadata := Replacement[vehicleMetadata]{vehicleMetadata{CouplingID: "train"}}
			e.Delta.Vehicles = append(e.Delta.Vehicles, VehicleDelta{ID: "cabin", Metadata: &metadata})
		}, "unmarked publication contains coupling fields"},
		{"unmarked envelope/packed delta cabin binding", "express", "delta", func(e *StreamEnvelope) {
			metadata := Replacement[vehicleMetadata]{vehicleMetadata{CouplingID: "train"}}
			e.Delta.Vehicles = append(e.Delta.Vehicles, VehicleDelta{ID: "cabin", Metadata: &metadata})
		}, "unmarked publication contains coupling fields"},
		{"unmarked full/enabled", "foundation", "full", func(e *StreamEnvelope) { e.Full.State.Simulation.CouplingEnabled = true }, "unmarked publication contains coupling fields"},
		{"unmarked full/empty groups", "express", "full", func(e *StreamEnvelope) {
			e.Full.State.Simulation.CouplingGroups = []sim.CouplingGroupView{}
		}, "unmarked publication contains coupling fields"},
		{"unmarked full/cabin binding", "foundation", "full", func(e *StreamEnvelope) {
			e.Full.State.Simulation.Vehicles[0].CouplingID = "train"
		}, "unmarked publication contains coupling fields"},
		{"marker cleared with frame/groups", "coupling raw", "full", func(e *StreamEnvelope) {
			e.CouplingContract = ""
			e.Full.State.Simulation.CouplingContract = ""
		}, "unmarked publication contains coupling fields"},
		{"raw envelope/packed full", "express", "full", func(e *StreamEnvelope) { e.OrderContract, e.TextEncoding = "", "" }, "publication order contract mismatch"},
		{"packed envelope/raw full", "foundation", "full", func(e *StreamEnvelope) {
			e.OrderContract, e.TextEncoding = sim.ExpressOrderContract, ExpressTextEncoding
		}, "publication order contract mismatch"},
		{"packed envelope/raw coupling full", "coupling raw", "full", func(e *StreamEnvelope) {
			e.OrderContract, e.TextEncoding = sim.ExpressOrderContract, ExpressTextEncoding
		}, "publication order contract mismatch"},
		{"marked raw delta", "coupling raw", "delta", func(*StreamEnvelope) {}, ""},
		{"marked packed delta", "coupling packed", "delta", func(*StreamEnvelope) {}, ""},
		{"unmarked delta without coupling fields", "coupling raw", "delta", func(e *StreamEnvelope) {
			e.CouplingContract = ""
			delete(e.Delta.Groups, "coupling")
			for i := range e.Delta.Vehicles {
				e.Delta.Vehicles[i].Metadata = nil
			}
		}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := streamFamilyEnvelope(t, frames[test.family], test.kind)
			if e.Full != nil {
				frame := ownStreamBoardings(*e.Full)
				frame.State.Simulation.Vehicles = slices.Clone(frame.State.Simulation.Vehicles)
				e.Full = &frame
			}
			test.edit(&e)
			_, err := EncodeStreamJSON(e)
			if test.want == "" {
				if err != nil {
					t.Fatal("rejected a consistent publication", err)
				}
				return
			}
			if err == nil || err.Error() != test.want {
				t.Fatalf("got %v, want %s", err, test.want)
			}
		})
	}
}

func TestCouplingStreamLimitsPackedContainUnpacked(t *testing.T) {
	t.Parallel()
	wide, exact := couplingStreamLimits(true), couplingStreamLimits(false)
	if wide.depth != exact.depth || wide.elements != exact.elements || wide.members != exact.members ||
		wide.stringBytes != exact.stringBytes || wide.allowInvalidUTF8 != exact.allowInvalidUTF8 {
		t.Fatal("packed and unpacked coupling limits differ outside array bounds")
	}
	if !maps.Equal(maps.Collect(func(yield func(string, bool) bool) {
		for path := range wide.arrays {
			if !yield(path, true) {
				return
			}
		}
	}), maps.Collect(func(yield func(string, bool) bool) {
		for path := range exact.arrays {
			if !yield(path, true) {
				return
			}
		}
	})) {
		t.Fatal("packed and unpacked coupling limits bound different paths")
	}
	for path, bound := range exact.arrays {
		if wide.arrays[path] < bound {
			t.Fatalf("packed bound %d of %s is below unpacked bound %d", wide.arrays[path], path, bound)
		}
	}
}

func couplingDecoders() map[string]func([]byte) error {
	return map[string]func([]byte) error{
		"stream": func(raw []byte) error {
			_, err := DecodeStreamJSONVersion(raw, CouplingStreamVersion)
			return err
		},
		"http": func(raw []byte) error {
			_, err := DecodeCouplingStateJSON(raw)
			return err
		},
	}
}

// The bounded scan runs before the header decode, so a document deeper than
// the jsontext decoder's own cap fails with the scan error.
func TestCouplingDecodeBoundsBeforeHeader(t *testing.T) {
	t.Parallel()
	zeros := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
	members := func(n int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, `,"m%d":0`, i)
		}
		return b.String()
	}
	packed := `"orderContract":"` + string(sim.ExpressOrderContract) + `","textEncoding":"` + ExpressTextEncoding + `",`
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"deeper than the decoder cap", `{"couplingContract":"compact-pair-v1","x":` + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + `}`, errJSONTooDeep},
		{"deeper than the decoder cap under the marker", `{"orderContract":` + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + `}`, errJSONTooDeep},
		{"deeper than the stream limit", `{"x":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`, errJSONTooDeep},
		{"array past the element limit", `{"x":` + zeros(65537) + `}`, errJSONArrayTooLong},
		{"object past the member limit", `{"x":0` + members(256) + `}`, errJSONObjectTooLong},
		{"unpacked full pending past the unpacked bound", `{"full":{"state":{"simulation":{"pending":` + zeros(maxSavedTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"unpacked frame pending past the unpacked bound", `{"frame":{"state":{"simulation":{"pending":` + zeros(maxSavedTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"unpacked delta riders past the unpacked bound", `{"delta":{"vehicles":[{"riders":{"value":` + zeros(9) + `}}]}}`, errJSONArrayTooLong},
		{"packed full pending past the packed bound", `{` + packed + `"full":{"state":{"simulation":{"pending":` + zeros(sim.MaxExpressWaitingTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"packed frame pending past the packed bound", `{` + packed + `"frame":{"state":{"simulation":{"pending":` + zeros(sim.MaxExpressWaitingTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"folded marker pending past the packed bound", `{"ORDERCONTRACT":"` + string(sim.ExpressOrderContract) + `","full":{"state":{"simulation":{"pending":` + zeros(sim.MaxExpressWaitingTrips+1) + `}}}}`, errJSONArrayTooLong},
		{"groups past the coupling bound", `{"couplingGroups":` + zeros(151) + `}`, errJSONArrayTooLong},
	}
	for name, decode := range couplingDecoders() {
		for _, test := range tests {
			t.Run(name+"/"+test.name, func(t *testing.T) {
				t.Parallel()
				if err := decode([]byte(test.raw)); !errors.Is(err, test.want) {
					t.Fatalf("got %v, want %v", err, test.want)
				}
			})
		}
	}
}

func TestScanCouplingOrderContract(t *testing.T) {
	t.Parallel()
	express := string(sim.ExpressOrderContract)
	tests := []struct {
		name   string
		raw    string
		packed bool
		err    bool
	}{
		{"absent", `{"kind":"full"}`, false, false},
		{"express", `{"orderContract":"` + express + `"}`, true, false},
		{"folded express", `{"ORDERCONTRACT":"` + express + `"}`, false, false},
		{"escaped express", `{"order\u0043ontract":"` + express + `"}`, true, false},
		{"other contract", `{"orderContract":"other"}`, false, false},
		{"null", `{"orderContract":null}`, false, false},
		{"nested", `{"full":{"orderContract":"` + express + `"}}`, false, false},
		{"duplicate last wins", `{"orderContract":"` + express + `","orderContract":"other"}`, false, false},
		{"duplicate last wins express", `{"orderContract":"other","orderContract":"` + express + `"}`, true, false},
		{"number", `{"orderContract":1}`, false, true},
		{"array document", `[]`, false, true},
		{"too deep", `{"a":` + strings.Repeat("[", 65) + strings.Repeat("]", 65) + `}`, false, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			packed, err := scanCouplingOrderContract([]byte(test.raw))
			if (err != nil) != test.err || packed != test.packed {
				t.Fatalf("got packed=%t err=%v, want packed=%t err=%t", packed, err, test.packed, test.err)
			}
		})
	}
}

func TestCouplingDecodeMarkerNamesAndRoundTrip(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		t.Run(string(order), func(t *testing.T) {
			t.Parallel()
			_, topology, frame := couplingStreamFixture(t, data.Frames[0], order)
			stream, err := EncodeStreamJSON(couplingFullEnvelope(frame))
			if err != nil {
				t.Fatal(err)
			}
			http, err := EncodeCouplingStateJSON(topology, frame)
			if err != nil {
				t.Fatal(err)
			}
			marker := []byte(`"orderContract":"` + string(order) + `"`)
			if order == "" {
				marker = []byte(`"couplingContract":"compact-pair-v1"`)
			}
			for name, raw := range map[string][]byte{"stream": stream, "http": http} {
				if !bytes.Contains(raw, marker) {
					t.Fatal("fixture lost its root marker", name)
				}
				wantStream, streamErr := DecodeStreamJSONVersion(raw, CouplingStreamVersion)
				wantHTTP, httpErr := DecodeCouplingStateJSON(raw)
				if name == "stream" && streamErr != nil || name == "http" && httpErr != nil {
					t.Fatal("canonical document rejected", streamErr, httpErr)
				}
				for _, test := range []struct {
					name   string
					edit   func([]byte) []byte
					accept bool
				}{
					{"duplicate marker", func(raw []byte) []byte {
						return bytes.Replace(raw, marker, append(append(append([]byte{}, marker...), ','), marker...), 1)
					}, false},
					{"folded duplicate marker", func(raw []byte) []byte {
						folded := bytes.ToUpper(marker[:bytes.IndexByte(marker, ':')])
						return bytes.Replace(raw, marker, append(append(append(append([]byte{}, marker...), ','), folded...), marker[bytes.IndexByte(marker, ':'):]...), 1)
					}, false},
					{"folded marker name", func(raw []byte) []byte {
						folded := bytes.ToUpper(marker[:bytes.IndexByte(marker, ':')])
						return bytes.Replace(raw, marker, append(folded, marker[bytes.IndexByte(marker, ':'):]...), 1)
					}, false},
					{"null marker", func(raw []byte) []byte {
						return bytes.Replace(raw, marker, append(slices.Clone(marker[:bytes.IndexByte(marker, ':')+1]), []byte("null")...), 1)
					}, false},
				} {
					edited := test.edit(raw)
					if bytes.Equal(edited, raw) {
						t.Fatal("edit missed the marker", name, test.name)
					}
					gotStream, streamErr := DecodeStreamJSONVersion(edited, CouplingStreamVersion)
					gotHTTP, httpErr := DecodeCouplingStateJSON(edited)
					err := httpErr
					if name == "stream" {
						err = streamErr
					}
					if (err == nil) != test.accept {
						t.Fatalf("%s %s: accept=%t err=%v", name, test.name, test.accept, err)
					}
					if test.accept && (name == "stream" && !reflect.DeepEqual(gotStream, wantStream) || name == "http" && !reflect.DeepEqual(gotHTTP, wantHTTP)) {
						t.Fatal("folded marker changed decoded values", name)
					}
				}
			}
		})
	}
}
