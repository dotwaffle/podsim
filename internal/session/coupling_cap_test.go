package session

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestCouplingSaveCapRejectsAtomically pads the composed coupling save,
// with 150 coupling groups, to the save cap and to one byte more. The
// decoder accepts the save at the cap with each group. At one byte more,
// startup keeps the file, writes nothing and starts no session. The
// padding is JSON whitespace after the root value, which each scan
// accepts, so only the cap refuses the larger file. The encoder has the
// same boundary: a demo error that brings the JSON form to the cap
// encodes, and one more byte gives ErrStateTooLarge and no data.
func TestCouplingSaveCapRejectsAtomically(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the cap fixture runs without -short and without the race detector")
	}
	shape := composedShape{"coupling", contractMarkers{coupling: sim.CompactPairV1CouplingContract}, composedCouplingGroups}
	file := composedSave(t, shape)
	groups := len(file.Simulation.CouplingGroups)
	if groups != composedCouplingGroups {
		t.Fatalf("composed save has %d coupling groups, want %d", groups, composedCouplingGroups)
	}

	t.Run("decoder", func(t *testing.T) {
		raw := composedSaveJSON(t, file)
		if len(raw) >= MaxStateBytes {
			t.Fatalf("composed coupling save has %d bytes, cap %d", len(raw), MaxStateBytes)
		}
		padded := make([]byte, MaxStateBytes+1)
		copy(padded, raw)
		for i := len(raw); i < len(padded); i++ {
			padded[i] = ' '
		}
		decoded, err := decodeStateFile(compressTestJSON(t, padded[:MaxStateBytes]))
		if err != nil {
			t.Fatal("decoder refused the save at the cap", err)
		}
		if len(decoded.Simulation.CouplingGroups) != groups || len(decoded.Simulation.Pods) != project.MaxPods {
			t.Fatal("decoder lost records at the cap")
		}
		store := &fakeStore{data: compressTestJSON(t, padded)}
		before := bytes.Clone(store.data)
		s, err := NewFromStore(t.Context(), StoreInput{Store: store})
		assertPreserved(t, s, err, store, before)
		if stateReason(err) != reasonTooLarge {
			t.Fatal("save over the cap is not too_large", err)
		}
	})

	t.Run("encoder", func(t *testing.T) {
		file := file
		file.Simulation.DemoError = "x"
		size := len(composedSaveJSON(t, file))
		file.Simulation.DemoError = strings.Repeat("x", 1+MaxStateBytes-size)
		var encoder stateEncoder
		data, err := encoder.encode(file)
		if err != nil {
			t.Fatal("encoder refused the save at the cap", err)
		}
		if got := len(decompressTestJSON(t, data)); got != MaxStateBytes {
			t.Fatalf("save has %d bytes, want %d", got, MaxStateBytes)
		}
		file.Simulation.DemoError += "x"
		data, err = encoder.encode(file)
		if !errors.Is(err, ErrStateTooLarge) || data != nil {
			t.Fatalf("encoder gave %d bytes and %v over the cap, want no data and %v", len(data), err, ErrStateTooLarge)
		}
	})
}

// TestCouplingStartupCancelAfterRestore cancels the start after the
// restore of committed coupling groups. Startup returns the cause, and it
// does not move the file aside or write it. A store call runs in its own
// goroutine and can end after startup returns, so the test waits until
// each goroutine of the bubble has ended or blocks before it reads the
// store.
func TestCouplingStartupCancelAfterRestore(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	encoded := encodeTestState(t, couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0])))
	synctest.Test(t, func(t *testing.T) {
		store := &fakeStore{data: bytes.Clone(encoded)}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		steps := realRestoreSteps()
		restored := false
		steps.restoreSimulation = func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
			simulation, result, err := sim.RestoreState(input)
			restored = err == nil && len(input.State.CouplingGroups) == 1
			cancel()
			return simulation, result, err
		}
		s, err := newFromStore(ctx, StoreInput{Store: store}, steps)
		synctest.Wait()
		if !restored {
			t.Fatal("the coupling groups did not restore", err)
		}
		if s != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("got session %v and %v, want no session and %v", s != nil, err, context.Canceled)
		}
		if !slices.Equal(store.callList(), []string{"read"}) || !bytes.Equal(store.data, encoded) {
			t.Fatal("canceled start changed the store", store.callList())
		}
	})
}

// TestCouplingPublishCanceled cancels a delta publication of committed
// coupling groups before the publisher encodes it. The publisher keeps
// its frame, its sequence and its history.
func TestCouplingPublishCanceled(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	s, _, _ := couplingStreamFixture(t, data.Frames[0], "")
	p := &statePublisher{session: s, clients: map[*streamSubscriber]bool{}}
	if err := p.publish(t.Context(), true, true); err != nil {
		t.Fatal(err)
	}
	frame, sequence, stream := p.frame, p.sequence, p.stream
	newTestClient(s, "cancel").mustApply(t, Command{Action: "pause", Paused: false})
	s.advance()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.publish(ctx, false, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want %v", err, context.Canceled)
	}
	if p.sequence != sequence || p.stream != stream || len(p.history) != 0 || !bytes.Equal(mustCouplingJSON(t, p.frame), mustCouplingJSON(t, frame)) {
		t.Fatal("canceled publication changed the publisher")
	}
}

// TestCouplingEncodeStateRejectsInvalidView checks that the HTTP state
// encoder of the server refuses a coupling frame that the assembler
// refuses, and gives no bytes. The server makes its frame from a checked
// snapshot, so only this test reaches the check.
func TestCouplingEncodeStateRejectsInvalidView(t *testing.T) {
	t.Parallel()
	data := couplingPhaseFixtures(t)
	_, topology, frame := couplingStreamFixture(t, data.Frames[0], "")
	if _, err := EncodeStateJSON(topology, frame); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*StreamFrame){
		"cabin binding": func(f *StreamFrame) { f.State.Simulation.Vehicles[0].CouplingID = "other" },
		"membership":    func(f *StreamFrame) { f.State.Simulation.CouplingGroups[0].Members[0] = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bad := ownStreamBoardings(frame)
			edit(&bad)
			if raw, err := EncodeStateJSON(topology, bad); err == nil || raw != nil {
				t.Fatalf("encoder gave %d bytes and %v for an invalid coupling view", len(raw), err)
			}
		})
	}
}
