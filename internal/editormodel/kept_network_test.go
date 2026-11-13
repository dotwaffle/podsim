package editormodel

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"reflect"
	"strings"
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
				if reflect.ValueOf(k.model.parking).IsZero() {
					t.Fatal("the validation did not keep the result of the parking route check")
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

// TestEditHandsOverServerChecks pins the key of the server checks that an
// edit hands to the engine: the network bytes of its reply. The engine takes
// them when it gets those bytes, and builds new checks when it gets other
// bytes. A refused edit hands over nothing.
func TestEditHandsOverServerChecks(t *testing.T) {
	t.Parallel()
	for name, config := range keptFixtures(t) {
		if name == "small" {
			// The edits of this fixture need no server check.
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := newKeptEngine(t, config)
			plain, _ := plainNodes(config.Network)
			// move sends a node move and returns the network of the reply.
			move := func(dx float64) []byte {
				t.Helper()
				for _, id := range plain {
					node, _ := k.model.config.Network.Node(id)
					var reply cycleReply
					text := ask(k.model, fmt.Sprintf(`{"op":"edit","edit":{"field":"geometry","value":{"action":"moveNode","id":%q,"point":{"x":%g,"y":%g}}}}`, id, node.Position.X+dx, node.Position.Y))
					if err := json.Unmarshal([]byte(text), &reply); err != nil {
						t.Fatal(err)
					}
					if network := reply.Change.Patch["network"]; reply.Error == "" && len(network) != 0 {
						return network
					}
				}
				t.Fatal("the engine refused each node move")
				return nil
			}
			network := move(0.25)
			handed := k.model.handed
			if handed == nil || !bytes.Equal(handed.raw, network) || handed.check == nil {
				t.Fatal("the edit did not hand over the server checks of its reply")
			}
			if name != "banks" && !checkRan(handed.check) {
				t.Fatal("the edit handed over server checks that did not run")
			}
			k.patchRaw(append(append([]byte(`{"network":`), network...), '}'))
			k.compare("handed", "validate", "checks")
			if k.model.network != handed || k.model.handed != nil {
				t.Fatal("the engine built new server checks for the bytes of the edit reply")
			}
			// The checks of the edit are for the network that the project
			// decoder reads from the bytes of the reply.
			if !reflect.DeepEqual(handed.typed, k.model.config.Network) {
				t.Fatal("the network of the edit differs from the network of its reply bytes")
			}
			// The browser discards the next edit, and sends other bytes.
			move(0.25)
			discarded := k.model.handed
			if discarded == nil || discarded == handed {
				t.Fatal("the second edit did not hand over its server checks")
			}
			other := config.Network
			other.Nodes = append(other.Nodes[:0:0], other.Nodes...)
			other.Nodes[0].Position.X += 0.125
			k.patch(map[string]any{"network": other})
			ask(k.model, `{"op":"validate"}`)
			if k.model.network == discarded || k.model.network == handed {
				t.Fatal("the engine took server checks of other bytes")
			}
			k.compare("discarded", "validate", "checks")
			// A refused edit hands over nothing, and drops the checks of
			// the edit before it.
			move(0.25)
			if k.model.handed == nil {
				t.Fatal("the third edit did not hand over its server checks")
			}
			if reply := ask(k.model, `{"op":"edit","edit":{"field":"geometry","value":{"action":"deleteLane","id":"no such lane"}}}`); !strings.Contains(reply, `"error"`) {
				t.Fatalf("the engine accepted the edit: %.200s", reply)
			}
			if k.model.handed != nil {
				t.Fatal("a refused edit kept handed server checks")
			}
		})
	}
}
