package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestCouplingTopologyStandalone(t *testing.T) {
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	for _, knownEmpty := range []bool{false, true} {
		config := couplingProject(input)
		if knownEmpty {
			config.CouplingSites, config.CouplingCorridors = nil, nil
		}
		for _, enabled := range []bool{false, true} {
			config.CouplingEnabled = enabled
			s, err := NewWithProject(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			topology := s.Topology()
			raw, err := json.Marshal(topology)
			if err != nil {
				t.Fatal(err)
			}
			var decoded TopologySnapshot
			if err := json.Unmarshal(raw, &decoded); err != nil || !reflect.DeepEqual(topology, decoded) {
				t.Fatal("standalone topology5 facts changed", err)
			}
			for version := 1; version < CouplingStreamVersion; version++ {
				if _, err := NewStreamAssemblerVersion(decoded, version); err == nil {
					t.Fatal("standalone topology5 enabled a live stream family", version)
				}
			}
			if _, err := NewStreamAssemblerVersion(decoded, CouplingStreamVersion); err != nil {
				t.Fatal("topology5 rejected its qualified assembler", err)
			}
			if _, err := FrameState(decoded, s.Frame()); err != nil {
				t.Fatal("topology5 frame assembly failed", err)
			}
			if len(topology.CouplingCorridors) != 0 {
				topology.CouplingCorridors[0].LaneIDs[0] = "mutated caller slice"
				if s.Topology().CouplingCorridors[0].LaneIDs[0] != input.CouplingCorridors[0].LaneIDs[0] {
					t.Fatal("topology shares retained coupling geometry")
				}
			}
		}
	}
}

func TestCouplingTopologyOldPresence(t *testing.T) {
	banked := project.Default()
	banked.Version, banked.Network = project.BankVersion, sim.BankExample()
	banked.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}}
	configs := []project.Config{project.Default(), banked, groupConsumerProject(t), expressConsumerProject(t)}
	for _, config := range configs {
		s, err := NewWithProject(config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(s.Close)
		base := mustCouplingJSON(t, s.Topology())
		var control TopologySnapshot
		if err := json.Unmarshal(base, &control); err != nil {
			t.Fatal("invalid old control", err)
		}
		for _, name := range []string{"couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors", "couplingGroups"} {
			for _, literal := range []string{"null", "[]", "false"} {
				t.Run(fmt.Sprintf("v%d/%s/%s", config.Version, name, literal), func(t *testing.T) {
					var root map[string]jsontext.Value
					if err := jsonv2.Unmarshal(base, &root); err != nil {
						t.Fatal(err)
					}
					root[name] = jsontext.Value(literal)
					previous := control
					if err := json.Unmarshal(mustCouplingJSON(t, root), &previous); err == nil {
						t.Fatal("old topology accepted reserved member presence")
					}
					if !reflect.DeepEqual(previous, control) {
						t.Fatal("failed topology parse replaced prior state")
					}
				})
			}
		}
	}
}

func TestCouplingTopologyBoundsAndRequired(t *testing.T) {
	data := couplingPhaseFixtures(t)
	input := couplingPhaseInput(t, data, data.Frames[0])
	topology := TopologySnapshot{ProjectVersion: project.CouplingVersion, CouplingContract: input.CouplingContract,
		CouplingSites: input.CouplingSites, CouplingCorridors: input.CouplingCorridors, Network: input.Network,
		ServerStart: "source", Epoch: "epoch", ProjectRevision: 1}
	base := mustCouplingJSON(t, topology)
	for _, test := range []struct {
		name       string
		edit       func(map[string]jsontext.Value)
		arrayBound bool
	}{
		{"null version", func(m map[string]jsontext.Value) { m["projectVersion"] = jsontext.Value("null") }, false},
		{"string version", func(m map[string]jsontext.Value) { m["projectVersion"] = jsontext.Value(`"5"`) }, false},
		{"object version", func(m map[string]jsontext.Value) { m["projectVersion"] = jsontext.Value("{}") }, false},
		{"array version", func(m map[string]jsontext.Value) { m["projectVersion"] = jsontext.Value("[]") }, false},
		{"missing marker", func(m map[string]jsontext.Value) { delete(m, "couplingContract") }, false},
		{"null marker", func(m map[string]jsontext.Value) { m["couplingContract"] = jsontext.Value("null") }, false},
		{"unknown marker", func(m map[string]jsontext.Value) { m["couplingContract"] = jsontext.Value(`"unknown"`) }, false},
		{"folded marker", func(m map[string]jsontext.Value) { m["COUPLINGCONTRACT"] = m["couplingContract"] }, false},
		{"null enabled", func(m map[string]jsontext.Value) { m["couplingEnabled"] = jsontext.Value("null") }, false},
		{"one-sided", func(m map[string]jsontext.Value) { delete(m, "couplingCorridors") }, false},
		{"missing scalar", func(m map[string]jsontext.Value) {
			m["couplingSites"] = jsontext.Value(`[{"id":"assembly","laneId":"ab"}]`)
		}, false},
		{"null registry", func(m map[string]jsontext.Value) { m["couplingSites"] = jsontext.Value("null") }, false},
		{"site array bound", func(m map[string]jsontext.Value) {
			m["couplingSites"] = mustCouplingJSON(t, repeatedCouplingSites(input.CouplingSites[0], sim.MaxCouplingSites+1))
		}, true},
		{"corridor array bound", func(m map[string]jsontext.Value) {
			m["couplingCorridors"] = mustCouplingJSON(t, repeatedCouplingCorridors(input.CouplingCorridors[0], sim.MaxCouplingCorridors+1))
		}, true},
		{"path array bound", func(m map[string]jsontext.Value) {
			corridor := input.CouplingCorridors[0]
			corridor.LaneIDs = make([]string, project.MaxLanes+1)
			for i := range corridor.LaneIDs {
				corridor.LaneIDs[i] = "ab"
			}
			m["couplingCorridors"] = mustCouplingJSON(t, []sim.CouplingCorridor{corridor})
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var root map[string]jsontext.Value
			if err := jsonv2.Unmarshal(base, &root); err != nil {
				t.Fatal(err)
			}
			test.edit(root)
			previous := topology
			err := json.Unmarshal(mustCouplingJSON(t, root), &previous)
			if err == nil || !reflect.DeepEqual(previous, topology) {
				t.Fatal("invalid topology replaced previous value", err)
			}
			if test.arrayBound && !errors.Is(err, errJSONArrayTooLong) {
				t.Fatal("array did not reach bounded preallocation guard", err)
			}
		})
	}
	if !bytes.Contains(base, []byte("compact-pair-v1")) {
		t.Fatal("control omits coupling marker")
	}
}

func repeatedCouplingSites(site sim.CouplingSite, count int) []sim.CouplingSite {
	sites := make([]sim.CouplingSite, count)
	for i := range sites {
		sites[i] = site
	}
	return sites
}

func repeatedCouplingCorridors(corridor sim.CouplingCorridor, count int) []sim.CouplingCorridor {
	corridors := make([]sim.CouplingCorridor, count)
	for i := range corridors {
		corridors[i] = corridor
	}
	return corridors
}
