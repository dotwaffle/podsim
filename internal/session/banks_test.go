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

func TestSavedBankVersionPairs(t *testing.T) {
	t.Parallel()
	base := legacyTestState(newTestStateFile(t), stateVersion)
	for _, savedVersion := range []int{2, 3, 4, 5, 6} {
		for _, projectVersion := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("saved%d/project%d", savedVersion, projectVersion), func(t *testing.T) {
				t.Parallel()
				file := base
				if savedVersion == serviceStateVersion {
					file = newTestStateFile(t)
				}
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
				valid := savedVersion >= 2 && savedVersion <= 4 && projectVersion == 1 || savedVersion == 5 && projectVersion == 2 || savedVersion == serviceStateVersion
				if (err == nil) != valid {
					t.Fatalf("accepted=%t want=%t: %v", err == nil, valid, err)
				}
				if valid && !reflect.DeepEqual(got, file) {
					t.Fatal("saved fields changed")
				}
				if !valid {
					wantReason := reasonInvalidState
					if savedVersion == 6 {
						wantReason = reasonUnsupportedVersion
					}
					if stateReason(err) != wantReason {
						t.Fatalf("reason %s", stateReason(err))
					}
				}
			})
		}
	}
}

func TestSavedBankRejectsLegacyPresence(t *testing.T) {
	t.Parallel()
	base := legacyTestState(newTestStateFile(t), stateVersion)
	for _, version := range []int{2, 3, 4, 5} {
		for _, banks := range []string{`null`, `[]`, `[{}]`} {
			if version == bankStateVersion && banks == `[{}]` {
				continue
			}
			t.Run(fmt.Sprintf("v%d/%s", version, banks), func(t *testing.T) {
				t.Parallel()
				file := base
				file.Version = version
				if version == 5 {
					file.Project.Version = 2
				}
				raw, err := json.Marshal(file)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.Replace(raw, []byte(`"Stations":[{`), []byte(`"Stations":[{"Banks":`+banks+`,`), 1)
				_, err = decodeStateFile(compressTestJSON(t, raw))
				if stateReason(err) != reasonInvalidState {
					t.Fatalf("accepted invalid bank field: %v", err)
				}
			})
		}
	}
}

func TestSavedBankBufferFieldCompatibility(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, fields string
		valid        bool
	}{
		{"omitted", "", true},
		{"empty kind", `"kind":"",`, true},
		{"buffer", `"kind":"buffer","terminalCell":4,`, true},
		{"null kind", `"kind":null,`, false},
		{"null endpoint", `"kind":"buffer","terminalCell":null,`, false},
		{"fractional endpoint", `"kind":"buffer","terminalCell":1.5,`, false},
		{"legacy endpoint", `"terminalCell":4,`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := legacyTestState(platoonStateFile(t), bankStateVersion)
			file.Version, file.Project = bankStateVersion, withBankMetadata(file.Project)
			file.Simulation.Pods[0].StationBuffered = true
			file.Simulation.Pods[1].Platoon.Lanes = 1
			raw, err := json.Marshal(file)
			if err != nil {
				t.Fatal(err)
			}
			raw = bytes.Replace(raw, []byte(`"platoon":{`), []byte(`"platoon":{`+test.fields), 1)
			got, err := decodeStateFile(compressTestJSON(t, raw))
			if (err == nil) != test.valid {
				t.Fatalf("decode error: %v", err)
			}
			if test.valid && !got.Simulation.Pods[0].StationBuffered {
				t.Fatal("lost buffer field")
			}
			if test.name == "buffer" && (got.Simulation.Pods[1].Platoon.TerminalCell == nil || *got.Simulation.Pods[1].Platoon.TerminalCell != 4) {
				t.Fatal("lost fixed entry certificate")
			}
		})
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

func TestBankRestoreVersionGatesBeforeSteps(t *testing.T) {
	t.Parallel()
	file := legacyTestState(newTestStateFile(t), stateVersion)
	file.Project = withBankMetadata(file.Project)
	raw, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	steps := restoreSteps{
		validateProject: func(project.Config) error { t.Error("validated mismatched project"); return nil },
		restoreSimulation: func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
			t.Error("restored mismatched state")
			return nil, sim.RestoreResult{}, nil
		},
	}
	_, err = shared.loadState(loadInput{data: compressTestJSON(t, raw), steps: steps})
	if stateReason(err) != reasonInvalidState {
		t.Fatalf("version mismatch: %v", err)
	}
	file.Version = bankStateVersion
	raw, err = json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop after checking restore gates")
	called := false
	steps.validateProject = func(project.Config) error { return nil }
	steps.restoreSimulation = func(input sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
		called = true
		if !input.StationBuffers || !input.BufferPlatoons {
			t.Fatal("version 5 blocked existing buffer fields")
		}
		return nil, sim.RestoreResult{}, stop
	}
	_, err = shared.loadState(loadInput{data: compressTestJSON(t, raw), steps: steps})
	if !called || !errors.Is(err, stop) {
		t.Fatalf("restore gate call=%t: %v", called, err)
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
