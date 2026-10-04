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
	config.Version = project.BankVersion
	station := &config.Network.Stations[0]
	bank := sim.StationBank{ID: "a", Entry: station.Entry, Exit: station.Exit}
	for _, berth := range station.Berths {
		bank.BerthIDs = append(bank.BerthIDs, berth.ID)
	}
	station.Banks = []sim.StationBank{bank}
	return config
}

// Versions 2 through 5 came before version 6. The decoder rejects them by
// version before it reads the other members, also with a valid project.
func TestSavedBankVersionPairs(t *testing.T) {
	t.Parallel()
	base := newTestStateFile(t)
	for _, savedVersion := range []int{2, 3, 4, 5, 6} {
		for _, projectVersion := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("saved%d/project%d", savedVersion, projectVersion), func(t *testing.T) {
				t.Parallel()
				file := base
				file.Version = savedVersion
				switch projectVersion {
				case project.BankVersion:
					file.Project = withBankMetadata(base.Project)
				case project.ServiceVersion:
					file.Project.Version = project.ServiceVersion
				}
				raw, err := json.Marshal(file)
				if err != nil {
					t.Fatal(err)
				}
				got, err := decodeStateFile(compressTestJSON(t, raw))
				valid := savedVersion == serviceStateVersion
				if (err == nil) != valid {
					t.Fatalf("accepted=%t want=%t: %v", err == nil, valid, err)
				}
				if valid && !reflect.DeepEqual(got, file) {
					t.Fatal("saved fields changed")
				}
				if !valid && file.validateProjectVersion() == nil {
					t.Fatal("file validation accepted an old saved version")
				}
				if !valid && (stateReason(err) != reasonUnsupportedVersion || !strings.Contains(err.Error(), fmt.Sprintf("version %d is older than version 6", savedVersion))) {
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
	if err != nil || !captured || file.Version != serviceStateVersion {
		t.Fatalf("capture version=%d captured=%t: %v", file.Version, captured, err)
	}
	got, err := decodeStateFile(encodeTestState(t, file))
	if err != nil || got.Project.Version != project.BankVersion {
		t.Fatalf("decode captured bank state: %v", err)
	}
}

func TestTopologyBankDecoding(t *testing.T) {
	t.Parallel()
	base := TopologySnapshot{Network: sim.BankExample()}
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
		{"201 members", `[{"BerthIDs":[` + strings.Repeat(`"b",`, project.MaxBerths) + `"b"]}]`, false},
		{"unknown bank member", `[{"extra":1}]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, member := range []string{"Banks", "banks"} {
				raw := bytes.Replace(rawBase, append([]byte(`"Banks":`), bankJSON...), fmt.Appendf(nil, `%q:%s`, member, test.banks), 1)
				var topology TopologySnapshot
				if err := json.Unmarshal(raw, &topology); (err == nil) != test.valid {
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

func TestCommandBankShapeBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		limit int
		value func(int) string
	}{
		{"banks", sim.MaxStationBanks, func(count int) string { return "[" + strings.Repeat("{},", count-1) + "{}]" }},
		{"berth IDs", project.MaxBerths, func(count int) string { return `[{"BerthIDs":[` + strings.Repeat(`"b",`, count-1) + `"b"]}]` }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, count := range []int{test.limit, test.limit + 1} {
				for _, member := range []string{"Banks", "banks"} {
					raw := fmt.Sprintf(`{"action":"pause","project":{"network":{"Stations":[{%q:%s}]}}}`, member, test.value(count))
					err := prescanCommand([]byte(raw))
					if count == test.limit && err != nil {
						t.Fatalf("rejected bound: %v", err)
					}
					if count > test.limit && (!errors.Is(err, errCommandShape) || !errors.Is(err, errJSONArrayTooLong)) {
						t.Fatalf("accepted oversized array: %v", err)
					}
				}
			}
		})
	}
}
