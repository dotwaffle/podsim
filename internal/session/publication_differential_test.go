package session

import (
	"bytes"
	"encoding/json/jsontext"
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// replayClient applies each envelope, as a client does.
type replayClient struct {
	frame    StreamFrame
	stream   string
	sequence uint64
}

// apply decodes payload, applies it, and checks that the result encodes as
// reference does. A delta that drops an update fails here also when the
// reference delta drops it too.
func (c *replayClient) apply(t *testing.T, tick int, payload replayPayload, reference StreamFrame) {
	t.Helper()
	e, err := DecodeStreamJSON(payload.json)
	if err != nil {
		t.Fatal(err)
	}
	if c.frame, err = ApplyStream(c.frame, c.stream, c.sequence, e); err != nil {
		t.Fatalf("tick %d: %v", tick, err)
	}
	c.stream, c.sequence = e.Stream, e.Sequence
	got, err := EncodeStreamJSON(fullStreamEnvelope(c.frame))
	if err != nil {
		t.Fatal(err)
	}
	want, err := EncodeStreamJSON(fullStreamEnvelope(reference))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("tick %d: the client frame differs after a %s envelope", tick, payload.kind)
	}
}

// checkReplayPublication checks one capture against a reference that uses
// none of the shortcuts: new route windows, reflect.DeepEqual on each route,
// and a new encoding of the groups of both frames. It returns the reference
// frame.
func checkReplayPublication(t *testing.T, r *replay, tick int, previous StreamFrame, client *replayClient, payloads []replayPayload) StreamFrame {
	t.Helper()
	reference, err := r.session.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	if reference.Routes, err = r.session.simulation.RebuiltPresentationRoutesForTest(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.p.frame, reference) {
		t.Fatalf("tick %d: the published frame differs from a new build", tick)
	}
	if r.p.groups != nil {
		groups, groupErr := frameGroups(r.p.frame)
		if groupErr != nil {
			t.Fatal(groupErr)
		}
		if !maps.EqualFunc(r.p.groups, groups, func(a, b jsontext.Value) bool { return bytes.Equal(a, b) }) {
			t.Fatalf("tick %d: the kept groups are not the groups of the frame", tick)
		}
	}
	for _, payload := range payloads {
		e := StreamEnvelope{OrderContract: reference.State.Simulation.OrderContract, Kind: payload.kind, Stream: r.p.stream, Sequence: r.p.sequence, Source: sourceOf(reference), Build: reference.State.Build}
		if payload.kind == "full" {
			e.Full = &reference
		} else {
			if r.p.groups == nil {
				t.Fatalf("tick %d: a delta kept no groups", tick)
			}
			delta, deltaErr := makeDelta(previous, reference)
			if deltaErr != nil {
				t.Fatal(deltaErr)
			}
			for i := range reference.Routes {
				route := previous.Routes[i]
				if sim.SameRoutePresentation(route, reference.Routes[i]) != reflect.DeepEqual(route, reference.Routes[i]) {
					t.Fatalf("tick %d: the compare of route %d differs from reflect.DeepEqual", tick, i)
				}
			}
			e.Base, e.Delta = r.p.sequence-1, &delta
		}
		want, encodeErr := EncodeStreamJSON(e)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if !bytes.Equal(payload.json, want) {
			t.Fatalf("tick %d: %s envelope differs from the reference\ngot  %.2000s\nwant %.2000s", tick, payload.kind, payload.json, want)
		}
		client.apply(t, tick, payload, reference)
	}
	return reference
}

// TestPublicationMatchesReference runs the publisher with fixed scripts and
// checks each capture against the reference. Another run of this driver on an
// earlier tree gives the same envelopes, byte for byte, which is the check of
// the reference itself.
func TestPublicationMatchesReference(t *testing.T) {
	t.Parallel()
	minutes := 2
	if testing.Short() {
		minutes = 1
	}
	for _, c := range replayCases(minutes) {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r, commands := c.start(t)
			var previous StreamFrame
			var client replayClient
			var deltas, fulls, routeChanges, faulted int
			r.run(c.ticks, commands, func(tick int, payloads []replayPayload) {
				reference := checkReplayPublication(t, r, tick, previous, &client, payloads)
				for i := range reference.Routes {
					if i < len(previous.Routes) && previous.Routes[i].Identity != reference.Routes[i].Identity {
						routeChanges++
					}
				}
				faulted += len(reference.State.Simulation.Faults.Active)
				for _, payload := range payloads {
					if payload.kind == "delta" {
						deltas++
					} else {
						fulls++
					}
				}
				previous = reference
			})
			t.Logf("%d deltas, %d fulls, %d route changes, %d fault records seen, %d save points, command errors %q", deltas, fulls, routeChanges, faulted, len(r.checkpoints), slices.Compact(slices.Clone(r.errors)))
			if deltas == 0 || routeChanges == 0 {
				t.Fatal("the run moved no route")
			}
			if commands == nil {
				return
			}
			if faulted == 0 || len(r.checkpoints) < 3 {
				t.Fatal("the run did not reach its faults or its save points")
			}
		})
	}
}
