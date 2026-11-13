package editormodel

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// checkRan reports whether the preparation of check ran. It reads the
// unexported field of sim, so that sim exports no test entry for it.
func checkRan(check *sim.NetworkCheck) bool {
	return reflect.ValueOf(check).Elem().FieldByName("checked").Bool()
}

// TestKeptNetworkShared pins that the validation and the checks of one
// network read one set of server checks, in each order, and that the engine
// builds a new set when the bytes of the network change.
func TestKeptNetworkShared(t *testing.T) {
	t.Parallel()
	for name, config := range keptFixtures(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, order := range [][2]string{{"validate", "checks"}, {"checks", "validate"}} {
				k := newKeptEngine(t, config)
				if k.model.network != nil {
					t.Fatal("the synchronization built the server checks")
				}
				ask(k.model, `{"op":"`+order[0]+`"}`)
				kept := k.model.network
				// The checks of the small fixture and of the bank example
				// do not need the preparation of the server.
				if kept == nil || kept.check == nil || !checkRan(kept.check) && (order[0] == "validate" || name != "small" && name != "banks") {
					t.Fatalf("%s did not read the kept server checks", order[0])
				}
				if !bytes.Equal(kept.raw, k.model.branches["network"].raw) {
					t.Fatal("the kept server checks are for other bytes")
				}
				ask(k.model, `{"op":"`+order[1]+`"}`)
				if k.model.network != kept || !checkRan(kept.check) {
					t.Fatalf("%s built new server checks for the same network", order[1])
				}
				// The network of the project decoder is the network that
				// the checks decode from the draft.
				typed, err := decodeDraftNetwork(k.model.branches["network"].value)
				if err != nil || !reflect.DeepEqual(typed, kept.typed) {
					t.Fatalf("the draft decodes to another network: %v", err)
				}
				// A change of one byte gives new server checks. The
				// bytes of the start give new checks again.
				start := k.model.branches["network"].raw
				moved := config.Network
				moved.Nodes = append(moved.Nodes[:0:0], moved.Nodes...)
				moved.Nodes[0].Position.X += 0.125
				k.patch(map[string]any{"network": moved})
				k.compare("moved", order[0], order[1])
				if k.model.network == kept || !bytes.Equal(k.model.network.raw, k.model.branches["network"].raw) {
					t.Fatal("a changed network kept the server checks")
				}
				k.patchRaw(append(append([]byte(`{"network":`), start...), '}'))
				k.compare("back", order[0], order[1])
				if !bytes.Equal(k.model.network.raw, start) {
					t.Fatal("the first network did not get its server checks")
				}
			}
		})
	}
}
