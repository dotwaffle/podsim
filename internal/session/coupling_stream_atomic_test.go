package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCouplingStreamRejectsPartialMemberDelta(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	for _, phase := range data.Frames {
		if phase.State.CouplingGroups[0].Phase != sim.CouplingConnected {
			continue
		}
		s, topology, frame := couplingStreamFixture(t, phase, "")
		a, err := NewStreamAssembler(topology)
		if err != nil {
			t.Fatal(err)
		}
		if _, stateErr := a.State(frame); stateErr != nil {
			t.Fatal(stateErr)
		}
		newTestClient(s, "atomic").mustApply(t, Command{Action: "pause", Paused: false})
		s.advance()
		next, err := s.presentationFrame()
		if err != nil {
			t.Fatal(err)
		}
		delta, err := makeDelta(frame, next)
		if err != nil {
			t.Fatal(err)
		}
		if delta.Groups["coupling"] == nil {
			t.Fatal("control lacks train replacement")
		}
		e := couplingFullEnvelope(frame)
		e.Kind, e.Full, e.Delta = "delta", nil, &delta
		e.Sequence, e.Base, e.Source = 2, 1, sourceOf(next)
		good, err := ApplyStream(frame, e.Stream, 1, e)
		if err != nil {
			t.Fatal(err)
		}
		if _, stateErr := a.State(good); stateErr != nil {
			t.Fatal("valid complete delta", stateErr)
		}
		before := ownStreamBoardings(a.previous)
		for i := range delta.Vehicles {
			if delta.Vehicles[i].ID == next.State.Simulation.CouplingGroups[0].Members[1] {
				delta.Vehicles[i].Pod = nil
				break
			}
		}
		partial, err := ApplyStream(frame, e.Stream, 1, e)
		if err != nil {
			t.Fatal("control did not reach final assembly", err)
		}
		if _, err := a.State(partial); err == nil {
			t.Fatal("partial cabin update replaced a coherent train")
		}
		if !reflect.DeepEqual(a.previous, before) {
			t.Fatal("partial update changed retained frame")
		}
	}
}

// Without the coupling marker, the decoder refuses each coupling member,
// also with a null, false or empty value.
func TestCouplingStreamOldFieldPresence(t *testing.T) {
	t.Parallel()
	for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
		for _, name := range []string{"couplingContract", "couplingEnabled", "couplingGroups", "COUPLINGSITES", "couplingCorridors", "COUPLINGID"} {
			for _, literal := range []string{"null", "false", "[]"} {
				raw := fmt.Appendf(nil, `{"full":{"state":{"simulation":{"%s":%s}}}}`, name, literal)
				if order != "" {
					raw = append(fmt.Appendf(nil, `{"orderContract":%q,`, order), raw[1:]...)
				}
				if _, err := DecodeStreamJSON(raw); err == nil {
					t.Fatal("unmarked envelope accepted reserved field presence", order, name, literal)
				}
			}
		}
	}
}

func TestCouplingStreamArrayBounds(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	_, _, frame := couplingStreamFixture(t, data.Frames[0], "")
	group := frame.State.Simulation.CouplingGroups[0]
	frame.State.Simulation.CouplingGroups = make([]sim.CouplingGroupView, project.MaxPods/2+1)
	for i := range frame.State.Simulation.CouplingGroups {
		frame.State.Simulation.CouplingGroups[i] = group
	}
	raw, err := EncodeStreamJSON(couplingFullEnvelope(frame))
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeErr := DecodeStreamJSON(raw); !errors.Is(decodeErr, errJSONArrayTooLong) {
		t.Fatal("group array did not reach preallocation bound", decodeErr)
	}
	var replacement couplingReplacement
	replacement.Contract, replacement.Groups = sim.CompactPairV1CouplingContract, frame.State.Simulation.CouplingGroups
	raw, err = json.Marshal(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := scanCouplingReplacement(raw); !errors.Is(err, errJSONArrayTooLong) {
		t.Fatal("replacement array did not reach preallocation bound", err)
	}
}

// These journeys start from strict private restores, not natural recruitment.
func TestCouplingStreamRetiresNativeMembers(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	for _, phase := range data.Frames {
		if phase.State.CouplingGroups[0].Phase != sim.CouplingClosing {
			continue
		}
		for _, order := range []sim.OrderContract{"", sim.ExpressOrderContract} {
			t.Run(phase.Cohort+"/"+string(order), func(t *testing.T) {
				t.Parallel()
				s, topology, frame := couplingStreamFixture(t, phase, order)
				a, err := NewStreamAssembler(topology)
				if err != nil {
					t.Fatal(err)
				}
				if _, stateErr := a.State(frame); stateErr != nil {
					t.Fatal(stateErr)
				}
				newTestClient(s, "retirement").mustApply(t, Command{Action: "pause", Paused: false})
				seen := map[sim.CouplingPhase]bool{sim.CouplingClosing: true}
				for step := range 18000 {
					s.advance()
					if step%100 != 0 {
						continue
					}
					frame, err = s.presentationFrame()
					if err != nil {
						t.Fatal("native phase failed", err)
					}
					state, err := a.State(frame)
					if err != nil {
						t.Fatal("native train observation rejected", err)
					}
					if len(state.Simulation.CouplingGroups) != 0 {
						seen[state.Simulation.CouplingGroups[0].Phase] = true
						continue
					}
					for _, cabin := range state.Simulation.Vehicles {
						if cabin.CouplingID != "" {
							t.Fatal("retirement retained mechanical cabin membership")
						}
					}
					for _, phase := range []sim.CouplingPhase{sim.CouplingClosing, sim.CouplingLatching, sim.CouplingConnected, sim.CouplingUnlatching, sim.CouplingOpening, sim.CouplingDraining} {
						if !seen[phase] {
							t.Fatal("retirement skipped a public mechanical phase", phase)
						}
					}
					return
				}
				t.Fatal("native group did not retire within its journey bound")
			})
		}
	}
}
