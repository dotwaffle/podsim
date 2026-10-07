package session

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func withBankMetadata(config project.Config) project.Config {
	config = project.Clone(config)
	station := &config.Network.Stations[0]
	bank := sim.StationBank{ID: "a", Entry: station.Entry, Exit: station.Exit}
	for _, berth := range station.Berths {
		bank.BerthIDs = append(bank.BerthIDs, berth.ID)
	}
	station.Banks = []sim.StationBank{bank}
	return config
}

// Versions 2 through 8 came before version 9. The decoder rejects them by
// version before it reads the other members, also with a valid project.
// A saved project with an earlier project version is also refused.
func TestSavedBankVersionPairs(t *testing.T) {
	t.Parallel()
	base := newTestStateFile(t)
	for _, savedVersion := range []int{2, 5, 6, 8, 9} {
		for _, projectCase := range []string{"plain", "banks", "project3"} {
			t.Run(fmt.Sprintf("saved%d/%s", savedVersion, projectCase), func(t *testing.T) {
				t.Parallel()
				file := base
				file.Version = savedVersion
				switch projectCase {
				case "banks":
					file.Project = withBankMetadata(base.Project)
				case "project3":
					file.Project.Version = 3
				}
				got, err := decodeStateFile(compressTestJSON(t, marshalSavedJSON(t, file)))
				valid := savedVersion == stateVersion && projectCase != "project3"
				if (err == nil) != valid {
					t.Fatalf("accepted=%t want=%t: %v", err == nil, valid, err)
				}
				if valid && !reflect.DeepEqual(got, file) {
					t.Fatal("saved fields changed")
				}
				if !valid && file.validateProjectVersion() == nil {
					t.Fatal("file validation accepted an old saved version")
				}
				if !valid && savedVersion == stateVersion && stateReason(err) != reasonInvalidState {
					t.Fatalf("saved project version reason %s: %v", stateReason(err), err)
				}
				if !valid && savedVersion != stateVersion && (stateReason(err) != reasonUnsupportedVersion || !strings.Contains(err.Error(), fmt.Sprintf("version %d is older than version 9", savedVersion))) {
					t.Fatalf("reason %s: %v", stateReason(err), err)
				}
			})
		}
	}
}

func TestCaptureBankStateVersion(t *testing.T) {
	t.Parallel()
	shared, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	shared.project = withBankMetadata(shared.project)
	file, captured, err := shared.captureState(saveStartup)
	if err != nil || !captured || file.Version != stateVersion {
		t.Fatalf("capture version=%d captured=%t: %v", file.Version, captured, err)
	}
	got, err := decodeStateFile(encodeTestState(t, file))
	if err != nil || got.Project.Version != project.CurrentVersion || got.Project.Network.Stations[0].Banks == nil {
		t.Fatalf("decode captured bank state: %v", err)
	}
}

func TestTopologyBankDecoding(t *testing.T) {
	t.Parallel()
	base := TopologySnapshot{ProjectVersion: project.CurrentVersion, Network: sim.BankExample()}
	rawBase, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	bankJSON, err := json.Marshal(base.Network.Stations[1].Banks)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, banks string
		valid       bool
	}{
		{"two banks", string(bankJSON), true},
		{"null", `null`, false},
		{"empty", `[]`, false},
		{"nine banks", `[` + strings.Repeat(`{},`, sim.MaxStationBanks) + `{}]`, false},
		{"201 members", `[{"berthIDs":[` + strings.Repeat(`"b",`, project.MaxBerths) + `"b"]}]`, false},
		{"unknown bank member", `[{"extra":1}]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, member := range []string{"Banks", "banks"} {
				raw := bytes.Replace(rawBase, append([]byte(`"banks":`), bankJSON...), fmt.Appendf(nil, `%q:%s`, member, test.banks), 1)
				var topology TopologySnapshot
				// A folded member name is unknown, so the decoder refuses it.
				if err := json.Unmarshal(raw, &topology); (err == nil) != (test.valid && member == "banks") {
					t.Fatalf("topology decoder: %v", err)
				}
			}
		})
	}
}

func TestTopologyBankRejectsInvalidMembership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*sim.Network)
	}{
		{"duplicate bank ID", func(n *sim.Network) { n.Stations[1].Banks[1].ID = n.Stations[1].Banks[0].ID }},
		{"duplicate membership", func(n *sim.Network) { n.Stations[1].Banks[1].BerthIDs[0] = n.Stations[1].Banks[0].BerthIDs[0] }},
		{"missing membership", func(n *sim.Network) { n.Stations[1].Banks[0].BerthIDs = nil }},
		{"unknown membership", func(n *sim.Network) { n.Stations[1].Banks[0].BerthIDs[0] = "unknown" }},
		{"wrong alias", func(n *sim.Network) { n.Stations[1].Entry = n.Stations[1].Banks[1].Entry }},
		{"unknown gate", func(n *sim.Network) { n.Stations[1].Banks[1].Entry = "unknown" }},
		{"orphan local lane", func(n *sim.Network) {
			n.Lanes = append(n.Lanes, sim.Lane{ID: "orphan", From: "bank-a-entry", To: "bank-b-exit", StationID: "hub", StationRole: sim.StationThroughRole, SpeedLimit: 14})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			topology := TopologySnapshot{Network: sim.BankExample()}
			original, err := json.Marshal(topology)
			if err != nil {
				t.Fatal(err)
			}
			changed := TopologySnapshot{Network: project.CloneNetwork(topology.Network)}
			test.change(&changed.Network)
			raw, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if decodeErr := json.Unmarshal(raw, &topology); decodeErr == nil {
				t.Fatal("decoded invalid bank topology")
			}
			after, err := json.Marshal(topology)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatal("failed decode changed topology")
			}
		})
	}
}

// TestTopologyIDCharacters pins the refusals of a topology with an ID or a
// station name that a valid project cannot have. They come after the bank
// check.
func TestTopologyIDCharacters(t *testing.T) {
	t.Parallel()
	const characters = "has a character other than A-Z, a-z, 0-9, '.', '+' or '-'"
	for _, test := range []struct {
		name   string
		change func(*TopologySnapshot)
		want   string
	}{
		{"banks before characters", func(topology *TopologySnapshot) {
			topology.Network.Stations[1].Banks[0].BerthIDs = nil
			topology.Network.Lanes[0].ID = "l 0"
		}, ""},
		{"lane ID", func(topology *TopologySnapshot) { topology.Network.Lanes[0].ID = "l 0" }, `ID "l 0" ` + characters},
		{"station name", func(topology *TopologySnapshot) { topology.Network.Stations[0].Name = "Hub\x01" },
			`name "Hub\x01" has a control character or is not UTF-8`},
		{"network before identity", func(topology *TopologySnapshot) {
			topology.Network.Lanes[0].ID, topology.Epoch = "l_0", "e 1"
		}, `ID "l_0" ` + characters},
		{"identity", func(topology *TopologySnapshot) { topology.ServerStart = "s&1" }, "topology identity " + characters},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			topology := TopologySnapshot{ProjectVersion: project.CurrentVersion, Network: sim.BankExample(), ServerStart: "server", Epoch: "epoch"}
			var decoded TopologySnapshot
			if raw, err := json.Marshal(topology); err != nil || json.Unmarshal(raw, &decoded) != nil {
				t.Fatalf("the base topology does not decode: %v", err)
			}
			test.change(&topology)
			raw, err := json.Marshal(topology)
			if err != nil {
				t.Fatal(err)
			}
			err = json.Unmarshal(raw, &decoded)
			if test.want == "" {
				if err == nil || strings.Contains(err.Error(), characters) {
					t.Fatalf("got %v, want the bank refusal", err)
				}
				return
			}
			// The JSON decoder adds its own prefix.
			if err == nil || !strings.HasSuffix(err.Error(), ": "+test.want) {
				t.Fatalf("got %v, want %q", err, test.want)
			}
		})
	}
}

func TestCommandBankShapeBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		limit int
		value func(int) string
	}{
		{"banks", sim.MaxStationBanks, func(count int) string { return "[" + strings.Repeat("{},", count-1) + "{}]" }},
		{"berth IDs", project.MaxBerths, func(count int) string { return `[{"berthIDs":[` + strings.Repeat(`"b",`, count-1) + `"b"]}]` }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, count := range []int{test.limit, test.limit + 1} {
				for _, member := range []string{"Banks", "banks"} {
					raw := fmt.Sprintf(`{"action":"pause","project":{"network":{"stations":[{%q:%s}]}}}`, member, test.value(count))
					err := prescanCommand([]byte(raw))
					// A folded name is an unknown path, which has no array.
					refused := count > test.limit || member != "banks"
					if !refused && err != nil {
						t.Fatalf("rejected bound: %v", err)
					}
					if refused && (!errors.Is(err, errCommandShape) || !errors.Is(err, errJSONArrayTooLong)) {
						t.Fatalf("accepted oversized array: %v", err)
					}
				}
			}
		})
	}
}
