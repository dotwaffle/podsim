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

// These counts are independent contract expectations, not production bounds.
func fleetBoundFixture(t *testing.T, contract sim.OrderContract, count int) (TopologySnapshot, StreamFrame) {
	t.Helper()
	shared := expressSession(t)
	frame, err := shared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	topology := shared.Topology()
	topology.OrderContract = contract
	frame.State.Simulation.OrderContract = contract
	if contract == "" {
		topology.ExpressServices = nil
	}
	vehicle := frame.State.Simulation.Vehicles[0]
	if contract == "" {
		vehicle.Pod.Class = sim.GroupClass
	}
	frame.State.Simulation.Vehicles = make([]VehicleFrame, count)
	frame.Routes = make([]sim.RoutePresentation, count)
	for i := range count {
		frame.State.Simulation.Vehicles[i] = vehicle
		frame.State.Simulation.Vehicles[i].Pod.ID = fmt.Sprintf("fleet-%04d", i)
		frame.Routes[i] = sim.RoutePresentation{Origin: -1}
	}
	for i := range frame.State.Simulation.Berths {
		frame.State.Simulation.Berths[i].Occupant = ""
		frame.State.Simulation.Berths[i].ReservedBy = ""
	}
	return topology, frame
}

func fleetBoundEnvelope(frame StreamFrame) StreamEnvelope {
	return StreamEnvelope{OrderContract: frame.State.Simulation.OrderContract, Kind: "full", Stream: "fleet", Sequence: 1,
		Source: sourceOf(frame), Build: frame.State.Build, Full: &frame}
}

func fleetBoundError(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
	} else if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}

func TestExpressFleetBoundScanTables(t *testing.T) {
	if sim.MaxExpressPods != 600 || sim.MaxExpressWaitingTrips != 17000 {
		t.Fatal("the separately qualified Express bounds changed")
	}
	for _, document := range []string{
		`{"full":{"routes":%s}}`,
		`{"full":{"state":{"simulation":{"vehicles":%s}}}}`,
		`{"frame":{"routes":%s}}`,
		`{"frame":{"state":{"simulation":{"vehicles":%s}}}}`,
		`{"delta":{"vehicles":%s}}`,
	} {
		for _, test := range []struct {
			contract sim.OrderContract
			count    int
			refuse   bool
		}{
			{sim.ExpressOrderContract, 600, false},
			{sim.ExpressOrderContract, 601, true},
			{"", 1000, false},
		} {
			list := "[" + strings.TrimSuffix(strings.Repeat("0,", test.count), ",") + "]"
			raw := fmt.Appendf(nil, document, list)
			err := prescanJSON(raw, streamLimits(contractMarkers{order: test.contract}))
			if (err != nil) != test.refuse || err != nil && !errors.Is(err, errJSONArrayTooLong) {
				t.Fatalf("%s, %q, count %d: %v", document, test.contract, test.count, err)
			}
			fleetBoundError(t, prescanJSON(raw, streamMarkerLimits()), "")
		}
	}
}

func TestExpressFleetBoundFullAndHTTP(t *testing.T) {
	for _, test := range []struct {
		name     string
		contract sim.OrderContract
		count    int
		refuse   bool
	}{
		{"express600", sim.ExpressOrderContract, 600, false},
		{"express601", sim.ExpressOrderContract, 601, true},
		{"plain1000", "", 1000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology, frame := fleetBoundFixture(t, test.contract, test.count)
			envelope := fleetBoundEnvelope(frame)
			// The stream encoder does not enforce semantic fleet counts.
			raw, err := EncodeStreamJSON(envelope)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeStreamJSON(raw)
			if test.refuse {
				if !errors.Is(err, errJSONArrayTooLong) {
					t.Fatalf("decode: got %v, want array limit", err)
				}
			} else {
				fleetBoundError(t, err, "")
				applied, applyErr := ApplyStream(StreamFrame{}, "", 0, decoded)
				fleetBoundError(t, applyErr, "")
				if len(applied.State.Simulation.Vehicles) != test.count {
					t.Fatal("stream lost vehicles")
				}
			}
			wantCounts, wantIdentity := "", ""
			if test.refuse {
				wantCounts, wantIdentity = "invalid presentation counts", "stream project exceeds fleet identity limit"
			}
			_, err = ApplyStream(StreamFrame{}, "", 0, envelope)
			fleetBoundError(t, err, wantCounts)
			assembler, err := NewStreamAssembler(topology)
			fleetBoundError(t, err, "")
			state, err := assembler.State(frame)
			fleetBoundError(t, err, wantIdentity)
			if !test.refuse && len(state.Simulation.Vehicles) != test.count {
				t.Fatal("assembler lost vehicles")
			}
			_, err = EncodeStateJSON(topology, frame)
			fleetBoundError(t, err, wantIdentity)
			// Bypass the producer guard so decoder coverage is independent.
			httpRaw, err := jsonv2.Marshal(StateEnvelope{OrderContract: test.contract, Topology: topology, Frame: frame}, json.DefaultOptionsV1(), packedRequestOptions())
			fleetBoundError(t, err, "")
			httpState, err := DecodeStateJSON(httpRaw)
			if test.refuse {
				if !errors.Is(err, errJSONArrayTooLong) {
					t.Fatalf("HTTP decode: got %v, want array limit", err)
				}
			} else {
				fleetBoundError(t, err, "")
				if len(httpState.Simulation.Vehicles) != test.count {
					t.Fatal("HTTP lost vehicles")
				}
			}
		})
	}
}

// Keep HTTP checks independently runnable so a mutation cannot be caught by
// an earlier stream or standalone assembler assertion instead.
func TestExpressFleetBoundHTTP(t *testing.T) {
	for _, test := range []struct {
		name     string
		contract sim.OrderContract
		count    int
		refuse   bool
	}{
		{"express600", sim.ExpressOrderContract, 600, false},
		{"express601", sim.ExpressOrderContract, 601, true},
		{"plain1000", "", 1000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology, frame := fleetBoundFixture(t, test.contract, test.count)
			t.Run("encode", func(t *testing.T) {
				_, err := EncodeStateJSON(topology, frame)
				want := ""
				if test.refuse {
					want = "stream project exceeds fleet identity limit"
				}
				fleetBoundError(t, err, want)
			})
			t.Run("decode", func(t *testing.T) {
				raw, err := jsonv2.Marshal(StateEnvelope{OrderContract: test.contract, Topology: topology, Frame: frame}, json.DefaultOptionsV1(), packedRequestOptions())
				fleetBoundError(t, err, "")
				state, err := DecodeStateJSON(raw)
				if test.refuse {
					if !errors.Is(err, errJSONArrayTooLong) {
						t.Fatalf("HTTP decode: got %v, want array limit", err)
					}
					return
				}
				fleetBoundError(t, err, "")
				if len(state.Simulation.Vehicles) != test.count {
					t.Fatalf("HTTP decoded %d vehicles, want %d", len(state.Simulation.Vehicles), test.count)
				}
			})
		})
	}
}

func TestExpressFleetBoundDelta(t *testing.T) {
	for _, test := range []struct {
		name     string
		contract sim.OrderContract
		count    int
		refuse   bool
	}{
		{"express600", sim.ExpressOrderContract, 600, false},
		{"express601", sim.ExpressOrderContract, 601, true},
		{"plain1000", "", 1000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, previous := fleetBoundFixture(t, test.contract, test.count)
			before := ownStreamBoardings(previous)
			items := make([]VehicleDelta, test.count)
			for i, v := range previous.State.Simulation.Vehicles {
				items[i] = VehicleDelta{ID: v.Pod.ID}
			}
			envelope := StreamEnvelope{OrderContract: test.contract, Kind: "delta", Stream: "fleet", Sequence: 2, Base: 1,
				Source: sourceOf(previous), Build: previous.State.Build, Delta: &StreamDelta{Vehicles: items}}
			raw, err := EncodeStreamJSON(envelope)
			fleetBoundError(t, err, "")
			decoded, err := DecodeStreamJSON(raw)
			if test.refuse {
				if !errors.Is(err, errJSONArrayTooLong) {
					t.Fatalf("decode: got %v, want array limit", err)
				}
			} else {
				fleetBoundError(t, err, "")
				_, err = ApplyStream(previous, "fleet", 1, decoded)
				fleetBoundError(t, err, "")
			}
			want := ""
			if test.refuse {
				want = "invalid presentation counts"
			}
			_, err = ApplyStream(previous, "fleet", 1, envelope)
			fleetBoundError(t, err, want)
			// An empty delta must not bless an oversized caller-supplied base.
			envelope.Delta = &StreamDelta{}
			_, err = ApplyStream(previous, "fleet", 1, envelope)
			fleetBoundError(t, err, want)
			if !reflect.DeepEqual(previous, before) {
				t.Fatal("delta mutated its predecessor")
			}
		})
	}
}

func TestExpressFleetBoundRetainedIdentities(t *testing.T) {
	for _, test := range []struct {
		name     string
		contract sim.OrderContract
		count    int
	}{
		{"express600", sim.ExpressOrderContract, 600},
		{"plain1000", "", 1000},
	} {
		t.Run(test.name, func(t *testing.T) {
			topology, frame := fleetBoundFixture(t, test.contract, test.count)
			assembler, err := NewStreamAssembler(topology)
			fleetBoundError(t, err, "")
			_, err = assembler.State(frame)
			fleetBoundError(t, err, "")
			// A full replacement removes one live ID, but must retain its class.
			shrunk := frame
			shrunk.State.Simulation.Vehicles = slices.Clone(frame.State.Simulation.Vehicles[1:])
			shrunk.Routes = slices.Clone(frame.Routes[1:])
			_, err = assembler.State(shrunk)
			fleetBoundError(t, err, "")
			emptyDelta := StreamEnvelope{OrderContract: test.contract, Kind: "delta", Stream: "fleet", Sequence: 2, Base: 1,
				Source: sourceOf(shrunk), Build: shrunk.State.Build, Delta: &StreamDelta{}}
			deltaFrame, err := ApplyStream(shrunk, "fleet", 1, emptyDelta)
			fleetBoundError(t, err, "")
			_, err = assembler.State(deltaFrame)
			fleetBoundError(t, err, "")
			if len(assembler.classes) != test.count {
				t.Fatalf("retained %d identities, want %d", len(assembler.classes), test.count)
			}
			beforeFrame, beforeState, beforeClasses := assembler.previous, assembler.state, maps.Clone(assembler.classes)
			replaced := ownStreamBoardings(frame)
			replaced.State.Simulation.Vehicles[0].Pod.ID = "new-identity"
			// Each live frame fits. Only the retained union exceeds the cap.
			full, err := ApplyStream(shrunk, "fleet", 1, fleetBoundEnvelope(replaced))
			fleetBoundError(t, err, "")
			_, err = assembler.State(full)
			fleetBoundError(t, err, "stream project exceeds fleet identity limit")
			if !reflect.DeepEqual(beforeFrame, assembler.previous) || !reflect.DeepEqual(beforeState, assembler.state) || !maps.Equal(beforeClasses, assembler.classes) {
				t.Fatal("identity refusal changed retained state")
			}
			// Reintroducing the original ID is allowed at the exact bound.
			_, err = assembler.State(frame)
			fleetBoundError(t, err, "")
			// Delta omission cannot remove IDs; an unknown ID cannot add one.
			for _, attempt := range []struct {
				name string
				item VehicleDelta
				want string
			}{
				{"add", VehicleDelta{ID: "new-identity"}, "invalid delta vehicle"},
				{"rename", VehicleDelta{ID: frame.State.Simulation.Vehicles[0].Pod.ID, Pod: &Replacement[sim.Pod]{Value: replaced.State.Simulation.Vehicles[0].Pod}}, "changed pod ID"},
			} {
				t.Run(attempt.name, func(t *testing.T) {
					e := StreamEnvelope{OrderContract: test.contract, Kind: "delta", Stream: "fleet", Sequence: 2, Base: 1,
						Source: sourceOf(frame), Build: frame.State.Build, Delta: &StreamDelta{Vehicles: []VehicleDelta{attempt.item}}}
					raw, err := EncodeStreamJSON(e)
					fleetBoundError(t, err, "")
					decoded, err := DecodeStreamJSON(raw)
					fleetBoundError(t, err, "")
					_, err = ApplyStream(frame, "fleet", 1, decoded)
					fleetBoundError(t, err, attempt.want)
				})
			}
		})
	}
}

func TestExpressFleetBoundMarkerScan(t *testing.T) {
	for _, contract := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		_, frame := fleetBoundFixture(t, contract, 1000)
		raw, err := EncodeStreamJSON(fleetBoundEnvelope(frame))
		fleetBoundError(t, err, "")
		members := splitObject(t, raw)
		// Place the marker after the fleet, not before it.
		slices.SortStableFunc(members, func(a, b rootMember) int {
			if a.name == "orderContract" && b.name != "orderContract" {
				return 1
			}
			if b.name == "orderContract" && a.name != "orderContract" {
				return -1
			}
			return 0
		})
		_, err = scanRootMarkers(joinObject(t, members))
		fleetBoundError(t, err, "")
	}
	// A malformed root marker must still fail typed marker decoding before
	// the exact fleet scan, even when the fleet precedes the marker.
	_, frame := fleetBoundFixture(t, sim.ExpressOrderContract, 601)
	raw, err := EncodeStreamJSON(fleetBoundEnvelope(frame))
	fleetBoundError(t, err, "")
	members := splitObject(t, raw)
	for i := range members {
		if members[i].name == "orderContract" {
			members[i].value = []byte("1")
			members = append(append(slices.Clone(members[:i]), members[i+1:]...), members[i])
			break
		}
	}
	_, err = scanRootMarkers(joinObject(t, members))
	if err == nil || errors.Is(err, errJSONArrayTooLong) {
		t.Fatalf("marker refusal must precede the exact fleet scan: %v", err)
	}
	// The preliminary scan still wins for syntax errors later in the input.
	_, err = scanRootMarkers(bytes.TrimSuffix(raw, []byte("}")))
	if err == nil || errors.Is(err, errJSONArrayTooLong) {
		t.Fatalf("syntax refusal must precede the exact fleet scan: %v", err)
	}
}

// fleetBoundSetMember edits JSON without converting numbers through float64.
func fleetBoundSetMember(t *testing.T, raw []byte, value string, path ...string) []byte {
	t.Helper()
	members := splitObject(t, raw)
	for i := range members {
		if members[i].name != path[0] {
			continue
		}
		if len(path) == 1 {
			members[i].value = []byte(value)
		} else {
			members[i].value = fleetBoundSetMember(t, members[i].value, value, path[1:]...)
		}
		return joinObject(t, members)
	}
	if len(path) != 1 {
		t.Fatalf("missing parent member %q", path[0])
	}
	return joinObject(t, append(members, rootMember{name: path[0], value: []byte(value)}))
}

func TestExpressFleetBoundPriorRefusals(t *testing.T) {
	for _, count := range []int{600, 601, 1000, 1001} {
		topology, frame := fleetBoundFixture(t, sim.ExpressOrderContract, count)
		for _, httpState := range []bool{false, true} {
			name, frameKey := "stream", "full"
			var target any = new(StreamEnvelope)
			var input any = fleetBoundEnvelope(frame)
			decode := func(raw []byte) error { _, err := DecodeStreamJSON(raw); return err }
			if httpState {
				name, frameKey = "HTTP", "frame"
				target = new(StateEnvelope)
				input = StateEnvelope{OrderContract: sim.ExpressOrderContract, Topology: topology, Frame: frame}
				decode = func(raw []byte) error { _, err := DecodeStateJSON(raw); return err }
			}
			raw, err := jsonv2.Marshal(input, json.DefaultOptionsV1(), packedRequestOptions())
			fleetBoundError(t, err, "")
			for _, test := range []struct {
				name  string
				value string
				path  []string
				stage string
				want  string
			}{
				{"forbidden marker", `"old"`, []string{"textEncoding"}, "", "document contains a text encoding marker"},
				{"invalid marker", `"other"`, []string{"orderContract"}, "", "document without the root Express marker contains an order marker"},
				{"typed root marker", `1`, []string{"orderContract"}, "root", ""},
				{"unknown member", `true`, []string{"unexpectedFleetMember"}, "typed", ""},
				{"invalid typed member", `{"bad":true}`, []string{frameKey, "state", "speed"}, "typed", ""},
				{"syntax", "", nil, "syntax", ""},
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", name, count, test.name), func(t *testing.T) {
					var bad []byte
					if test.stage == "syntax" {
						bad = bytes.TrimSuffix(raw, []byte("}"))
					} else {
						bad = fleetBoundSetMember(t, raw, test.value, test.path...)
					}
					if count > 1000 {
						if err := decode(bad); !errors.Is(err, errJSONArrayTooLong) {
							t.Fatalf("original preliminary fleet refusal lost: %v", err)
						}
						return
					}
					want := test.want
					var prior error
					switch test.stage {
					case "root":
						var header struct {
							OrderContract sim.OrderContract `json:"orderContract"`
						}
						prior = jsonv2.Unmarshal(bad, &header, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false))
					case "typed":
						// Compare the exact legacy strict decoder error on this
						// same input, including its JSON pointer and type details.
						prior = decodePackedStreamJSON(bad, target)
					case "syntax":
						prior = prescanJSON(bad, streamMarkerLimits())
					}
					if test.stage != "" {
						if prior == nil || errors.Is(prior, errJSONArrayTooLong) {
							t.Fatalf("fixture did not reach the intended old refusal: %v", prior)
						}
						want = prior.Error()
					}
					fleetBoundError(t, decode(bad), want)
				})
			}
		}
	}
}

func TestExpressFleetBoundPriorArrayRefusals(t *testing.T) {
	// Keep the original order bounds at their original scan positions,
	// ahead of forbidden markers and strict decoding, not just ahead of the
	// newly introduced fleet scan. These are deliberately incomplete frames.
	for _, test := range []struct {
		name     string
		contract string
		count    int
		member   string
		entries  int
	}{
		{"plain orders", "", 1000, "pending", 8201},
		{"Express orders", `"orderContract":"express-v1",`, 601, "pending", 17001},
		{"Express berths", `"orderContract":"express-v1",`, 601, "berths", 12001},
	} {
		t.Run(test.name, func(t *testing.T) {
			list := func(n int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", n), ",") + "]" }
			raw := fmt.Appendf(nil, `{%s"textEncoding":"old","full":{"state":{"simulation":{"vehicles":%s,%q:%s}}}}`,
				test.contract, list(test.count), test.member, list(test.entries))
			if _, err := DecodeStreamJSON(raw); !errors.Is(err, errJSONArrayTooLong) {
				t.Fatalf("original array refusal lost: %v", err)
			}
			// Isolate the old array error from the new fleet guard: the
			// original root scan itself must still refuse the order/berth list.
			if _, err := scanRootMarkers(raw); !errors.Is(err, errJSONArrayTooLong) {
				t.Fatalf("array refusal moved past the original root scan: %v", err)
			}
		})
	}
}
