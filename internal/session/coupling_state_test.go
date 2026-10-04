package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	legacyJSON "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCouplingSavedVersionSelection(t *testing.T) {
	for _, packed := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("packed=%t/enabled=%t", packed, enabled), func(t *testing.T) {
				config := project.Default()
				if packed {
					config = expressConsumerProject(t)
				}
				config.Version, config.CouplingContract, config.CouplingEnabled = project.CouplingVersion, sim.CompactPairV1CouplingContract, enabled
				store := &fakeStore{}
				s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(s.Close)
				client := newTestClient(s, "save8")
				command := Command{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 1, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService}
				if packed {
					command.OrderContract, command.Service, command.ServiceID, command.SharingConsent = sim.ExpressOrderContract, sim.ExpressServiceChoice, "harbor-market", sim.SharedConsent
				}
				client.mustApply(t, command)
				if err := s.SaveState(t.Context(), SavePeriodic); err != nil {
					t.Fatal(err)
				}
				file := store.lastWrite(t)
				if file.Version != couplingStateVersion || file.CouplingContract != config.CouplingContract || file.Simulation.CouplingContract != config.CouplingContract || file.OrderContract != config.OrderContract {
					t.Fatal("inactive/empty project5 downgraded or changed order family")
				}
				raw := decompressTestJSON(t, store.data)
				from := "harbor"
				if packed {
					from = base64.StdEncoding.EncodeToString([]byte(from))
				}
				if !bytes.Contains(raw, []byte(`"from":"`+from+`"`)) {
					t.Fatal("save8 selected order text from version instead of marker")
				}
				if packed != (file.TextEncoding == ExpressTextEncoding) {
					t.Fatal("save8 text discriminator mismatch")
				}
			})
		}
	}
}

func TestCouplingSavedEncoderMarkers(t *testing.T) {
	data := couplingPhaseFixtures(t)
	base := couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0]))
	for _, edit := range []struct {
		name   string
		change func(*stateFile)
	}{
		{"root", func(f *stateFile) { f.CouplingContract = "" }},
		{"native", func(f *stateFile) { f.Simulation.CouplingContract = "" }},
		{"project", func(f *stateFile) { f.Project.CouplingContract = "" }},
		{"family", func(f *stateFile) { f.Version = serviceStateVersion }},
		{"text", func(f *stateFile) { f.TextEncoding = ExpressTextEncoding }},
	} {
		t.Run(edit.name, func(t *testing.T) {
			file := base
			edit.change(&file)
			var encoder stateEncoder
			if _, err := encoder.encode(file); err == nil {
				t.Fatal("encoder accepted contradictory contract markers")
			}
		})
	}
}

type couplingPhaseFrame struct {
	Name   string         `json:"name"`
	SHA256 string         `json:"sha256"`
	Cohort string         `json:"cohort"`
	State  sim.SavedState `json:"state"`
}

type couplingPhaseData struct {
	Provenance           string                           `json:"provenance"`
	SourceManifestSHA256 string                           `json:"sourceManifestSha256"`
	Cohorts              map[string]sim.RestoreStateInput `json:"cohorts"`
	Frames               []couplingPhaseFrame             `json:"frames"`
}

// couplingPhaseFixtures reads exact saved private certificates, not recruitment runs.
func couplingPhaseFixtures(t *testing.T) couplingPhaseData {
	t.Helper()
	raw, err := os.ReadFile("testdata/coupling_native_phases.json")
	if err != nil {
		t.Fatal(err)
	}
	var data couplingPhaseData
	if err := json.Unmarshal(raw, &data, json.RejectUnknownMembers(true)); err != nil {
		t.Fatal(err)
	}
	if len(data.Frames) != 14 || len(data.Cohorts) != 2 || data.Provenance == "" || len(data.SourceManifestSHA256) != 64 {
		t.Fatal("incomplete native fixture provenance")
	}
	return data
}

func couplingPhaseInput(t *testing.T, data couplingPhaseData, frame couplingPhaseFrame) sim.RestoreStateInput {
	t.Helper()
	input, found := data.Cohorts[frame.Cohort]
	if !found || len(frame.SHA256) != 64 || !strings.HasPrefix(frame.Name, "occupied-"+strconv.FormatBool(frame.Cohort == "occupied")) {
		t.Fatal("fixture uses a different cohort", frame.Name)
	}
	input.State = frame.State
	raw, err := legacyJSON.Marshal(input) //nolint:musttag // Preserve the frozen native API fixture names, not public wire names.
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != frame.SHA256 {
		t.Fatal("fixture changed source input bytes", frame.Name, err)
	}
	if len(input.State.CouplingGroups) != 1 || len(input.State.Pods) != 2 {
		t.Fatal("fixture lost group members", frame.Name)
	}
	for _, pod := range input.State.Pods {
		if pod.Occupied != (frame.Cohort == "occupied") {
			t.Fatal("fixture lost cabin occupancy", frame.Name)
		}
	}
	return input
}

func couplingProject(input sim.RestoreStateInput) project.Config {
	config := project.Default()
	config.Version, config.Name = project.CouplingVersion, "Private coupling certificate"
	config.Network, config.Fleet = input.Network, input.Fleet
	// Add return connections for project passenger reachability. The private
	// certificate network and all saved lane indices remain unchanged.
	config.Network.Lanes = slices.Clone(input.Network.Lanes)
	for _, side := range []string{"front", "rear"} {
		for _, lane := range input.Network.Lanes {
			if lane.ID != side+"-out" {
				continue
			}
			lane.ID, lane.From, lane.To = side+"-return", lane.To, "origin-entry"
			lane.StationID, lane.StationRole = "", ""
			lane.Control = &sim.Point{X: -1000, Y: 2000}
			if side == "rear" {
				lane.Control.Y = -2000
			}
			config.Network.Lanes = append(config.Network.Lanes, lane)
		}
	}
	config.OrderContract = input.OrderContract
	config.CouplingContract, config.CouplingEnabled = input.CouplingContract, input.CouplingEnabled
	config.CouplingSites, config.CouplingCorridors = input.CouplingSites, input.CouplingCorridors
	config.ExpressServices = input.ExpressServices
	return config
}

func couplingPhaseFile(t *testing.T, input sim.RestoreStateInput) stateFile {
	t.Helper()
	s, err := NewWithProject(couplingProject(input))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	file := sessionStateFile(t, s)
	file.Version, file.CouplingContract, file.RestoreAttempts = couplingStateVersion, input.CouplingContract, 0
	file.Simulation = input.State
	return file
}

func TestCouplingSavedPhaseRoundTrip(t *testing.T) {
	data := couplingPhaseFixtures(t)
	for _, frame := range data.Frames {
		t.Run(frame.Name, func(t *testing.T) {
			input := couplingPhaseInput(t, data, frame)
			file := couplingPhaseFile(t, input)
			encoded := encodeTestState(t, file)
			decoded, err := decodeStateFile(encoded)
			if err != nil || !decoded.protected || !reflect.DeepEqual(decoded.Simulation, input.State) {
				t.Fatalf("saved group or cabin facts changed: %v", err)
			}
			if !bytes.Equal(encoded, encodeTestState(t, decoded)) {
				t.Fatal("save8 reencode changed deterministic bytes")
			}
		})
	}
}

func TestCouplingSavedRequiredMembers(t *testing.T) {
	data := couplingPhaseFixtures(t)
	file := couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0]))
	base := decompressTestJSON(t, encodeTestState(t, file))
	for _, member := range couplingRecordFields("group") {
		for _, mode := range []string{"missing", "null", "folded duplicate"} {
			t.Run(member+"/"+mode, func(t *testing.T) {
				changed := mutateCouplingGroup(t, base, func(group map[string]jsontext.Value) {
					var key string
					for candidate := range group {
						if strings.EqualFold(candidate, member) {
							key = candidate
						}
					}
					switch mode {
					case "missing":
						delete(group, key)
					case "null":
						group[key] = jsontext.Value("null")
					default:
						group[strings.ToUpper(key)] = group[key]
					}
				})
				if _, err := decodeStateFile(compressTestJSON(t, changed)); err == nil {
					t.Fatal("accepted malformed committed group")
				} else if _, preserved := errors.AsType[*preservedStateError](err); !preserved {
					t.Fatal("malformed committed group can enter fallback", err)
				}
			})
		}
	}
	for _, value := range []string{`[]`, `["front"]`, `["front","rear","third"]`, `["front",null]`, `false`} {
		t.Run("members="+value, func(t *testing.T) {
			changed := mutateCouplingGroup(t, base, func(g map[string]jsontext.Value) { g["members"] = jsontext.Value(value) })
			if _, err := decodeStateFile(compressTestJSON(t, changed)); err == nil {
				t.Fatal("accepted invalid exact pair")
			}
		})
	}
	for _, value := range []string{`{}`, `{"leg":0}`, `{"leg":null,"drainFirstMember":0}`, `{"leg":0,"drainFirstMember":0,"LEG":0}`} {
		t.Run("progress="+value, func(t *testing.T) {
			changed := mutateCouplingGroup(t, base, func(g map[string]jsontext.Value) { g["progress"] = jsontext.Value(value) })
			if _, err := decodeStateFile(compressTestJSON(t, changed)); err == nil {
				t.Fatal("accepted missing or duplicated progress")
			}
		})
	}
}

func mutateCouplingGroup(t *testing.T, raw []byte, change func(map[string]jsontext.Value)) []byte {
	t.Helper()
	var root, simulation, group map[string]jsontext.Value
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(root["simulation"], &simulation); err != nil {
		t.Fatal(err)
	}
	var groups []jsontext.Value
	if err := json.Unmarshal(simulation["couplingGroups"], &groups); err != nil || len(groups) != 1 {
		t.Fatal("missing source group", err)
	}
	if err := json.Unmarshal(groups[0], &group); err != nil {
		t.Fatal(err)
	}
	change(group)
	groups[0] = mustCouplingJSON(t, group)
	simulation["couplingGroups"] = mustCouplingJSON(t, groups)
	root["simulation"] = mustCouplingJSON(t, simulation)
	return mustCouplingJSON(t, root)
}

func mustCouplingJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCouplingSavedOldFamiliesRejectPresence(t *testing.T) {
	base := newTestStateFile(t)
	for version := serviceStateVersion; version <= expressStateVersion; version++ {
		file := base
		if version == expressStateVersion {
			s := expressSession(t)
			file = sessionStateFile(t, s)
			file.Version, file.OrderContract, file.TextEncoding = expressStateVersion, sim.ExpressOrderContract, ExpressTextEncoding
		}
		raw := decompressTestJSON(t, encodeTestState(t, file))
		if _, err := decodeStateFile(compressTestJSON(t, raw)); err != nil {
			t.Fatal("invalid historical-family control", version, err)
		}
		for _, path := range []string{"root", "simulation", "project"} {
			for _, name := range []string{"couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors", "couplingGroups"} {
				for _, value := range []string{"null", "[]", "false"} {
					t.Run(fmt.Sprintf("v%d/%s/%s/%s", version, path, name, value), func(t *testing.T) {
						var root map[string]jsontext.Value
						if err := json.Unmarshal(raw, &root); err != nil {
							t.Fatal(err)
						}
						object := root
						if path != "root" {
							object = nil
							if err := json.Unmarshal(root[path], &object); err != nil {
								t.Fatal(err)
							}
						}
						object[name] = jsontext.Value(value)
						if path != "root" {
							root[path] = mustCouplingJSON(t, object)
						}
						if _, err := decodeStateFile(compressTestJSON(t, mustCouplingJSON(t, root))); err == nil {
							t.Fatal("old family accepted reserved member presence")
						}
					})
				}
			}
		}
	}
}

func TestCouplingSavedRoutePreflight(t *testing.T) {
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	for _, change := range []struct {
		name string
		edit func(*sim.SavedPod)
	}{
		{"missing", func(p *sim.SavedPod) { p.Route = nil }},
		{"lost origin", func(p *sim.SavedPod) { p.Route = p.Route[1:] }},
		{"lost destination", func(p *sim.SavedPod) { p.Route = p.Route[:len(p.Route)-1] }},
		{"current lane", func(p *sim.SavedPod) { p.LaneID = "unknown" }},
		{"current index", func(p *sim.SavedPod) { p.RouteIndex = len(p.Route) }},
	} {
		t.Run(change.name, func(t *testing.T) {
			file := couplingPhaseFile(t, input)
			file.Simulation.Pods = slices.Clone(file.Simulation.Pods)
			file.Simulation.Pods[0].Route = slices.Clone(file.Simulation.Pods[0].Route)
			change.edit(&file.Simulation.Pods[0])
			var encoder stateEncoder
			if _, err := encoder.encode(file); err == nil {
				t.Fatal("encoded incomplete committed route")
			}
		})
	}
}

func TestCouplingSavedBoundsAndScalars(t *testing.T) {
	data := couplingPhaseFixtures(t)
	file := couplingPhaseFile(t, couplingPhaseInput(t, data, data.Frames[0]))
	base := decompressTestJSON(t, encodeTestState(t, file))
	for _, test := range []struct {
		name  string
		field string
		value string
	}{
		{"ID width", "id", `"` + strings.Repeat("x", 65) + `"`},
		{"ID empty", "id", `""`},
		{"unknown field", "future", `0`},
		{"tick overflow", "formationTick", `9223372036854775808`},
		{"fractional tick", "formationTick", `0.5`},
		{"null integer", "dwellTicks", `null`},
		{"invalid UTF8", "id", "\"\xff\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The malformed UTF-8 value must reach the decoder without reencoding.
			var changed []byte
			if test.name == "invalid UTF8" {
				changed = bytes.Replace(base, []byte(`"physical-pair"`), []byte(test.value), 1)
			} else {
				changed = mutateCouplingGroup(t, base, func(g map[string]jsontext.Value) { g[test.field] = jsontext.Value(test.value) })
			}
			if _, err := decodeStateFile(compressTestJSON(t, changed)); err == nil {
				t.Fatal("accepted invalid bounded scalar")
			}
		})
	}
	for _, target := range []string{"groups", "pods", "route"} {
		t.Run(target+" preallocation", func(t *testing.T) {
			var root, state map[string]jsontext.Value
			if err := json.Unmarshal(base, &root); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(root["simulation"], &state); err != nil {
				t.Fatal(err)
			}
			switch target {
			case "groups":
				groups := make([]sim.SavedCouplingGroup, project.MaxPods/2+1)
				for i := range groups {
					groups[i] = file.Simulation.CouplingGroups[0]
				}
				state["couplingGroups"] = mustCouplingJSON(t, groups)
			case "pods":
				pods := make([]sim.SavedPod, project.MaxPods+1)
				for i := range pods {
					pods[i] = file.Simulation.Pods[0]
				}
				state["pods"] = mustCouplingJSON(t, pods)
			case "route":
				pods := slices.Clone(file.Simulation.Pods)
				pods[0].Route = make([]int, project.MaxLanes+project.MaxNodes+1)
				state["pods"] = mustCouplingJSON(t, pods)
			}
			root["simulation"] = mustCouplingJSON(t, state)
			_, err := decodeStateFile(compressTestJSON(t, mustCouplingJSON(t, root)))
			if !errors.Is(err, errJSONArrayTooLong) {
				t.Fatal("did not reject at array preallocation guard", err)
			}
		})
	}
}
