package scenarios

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// suggestedCompact returns the default Compact stations of a preset.
func suggestedCompact(preset londonVariantPreset) []string {
	if preset.name == "LondonCentral" {
		return slices.Clone(londonCompactStations[:7])
	}
	return slices.Clone(londonCompactStations)
}

// checkLondonCompact checks the Compact stations of a project: one
// maneuver group each, in station order, with four bays, every pod
// Compact, the pods of each station in its first bays, and each station a
// rigid copy of the compact station with its bays outward of the aisle and
// its bypass toward the throat.
func checkLondonCompact(t *testing.T, config project.Config, ids []string, pods int) {
	t.Helper()
	var groups []string
	for _, station := range config.Network.Stations {
		if slices.Contains(ids, station.ID) {
			groups = append(groups, station.ID+"-"+compactGroupID)
		}
	}
	var got []string
	for _, group := range config.Network.ManeuverGroups {
		got = append(got, group.ID)
		if len(group.BerthIDs) != len(compactBays) {
			t.Fatalf("group %s has %d bays", group.ID, len(group.BerthIDs))
		}
	}
	if !slices.Equal(got, groups) {
		t.Fatalf("groups %q, want %q", got, groups)
	}
	for _, pod := range config.Fleet {
		if pod.Class != sim.CompactClass {
			t.Fatalf("pod %s has the class %q", pod.ID, pod.Class)
		}
	}
	fixture := compactStation(nil, "", "")
	nodes := make(map[string]sim.Point, len(config.Network.Nodes))
	for _, node := range config.Network.Nodes {
		nodes[node.ID] = node.Position
	}
	lanes := make(map[string]sim.Lane, len(config.Network.Lanes))
	for _, lane := range config.Network.Lanes {
		lanes[lane.ID] = lane
	}
	distance := func(a, b sim.Point) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }
	for _, id := range ids {
		station, ok := config.Network.Station(id)
		if !ok || len(station.Berths) != len(compactBays) {
			t.Fatalf("station %s is %+v", id, station)
		}
		var at []string
		for _, pod := range config.Fleet {
			if pod.StationID == id {
				at = append(at, pod.BerthID)
			}
		}
		for index, berth := range at {
			if berth != station.Berths[index].ID {
				t.Fatalf("%s pods at %q", id, at)
			}
		}
		if len(at) != pods {
			t.Fatalf("%s has %d pods, want %d", id, len(at), pods)
		}
		// Each two node positions, and each control point and the start
		// of its lane, are as far apart as in the compact station.
		points := func(name string) (sim.Point, bool) {
			point, ok := nodes[id+"-"+name]
			return point, ok
		}
		for i, a := range fixture.nodes {
			first, ok := points(a.ID)
			if !ok {
				t.Fatalf("%s has no node %s", id, a.ID)
			}
			for _, b := range fixture.nodes[i+1:] {
				second, _ := points(b.ID)
				if math.Abs(distance(first, second)-distance(a.Position, b.Position)) > 1e-9 {
					t.Fatalf("%s: nodes %s and %s are %g m apart, want %g", id, a.ID, b.ID, distance(first, second), distance(a.Position, b.Position))
				}
			}
		}
		for _, lane := range fixture.lanes {
			moved := lanes[id+"-"+lane.ID]
			if (moved.Control == nil) != (lane.Control == nil) {
				t.Fatalf("%s lane %s has the control %v", id, lane.ID, moved.Control)
			}
			if lane.Control == nil {
				continue
			}
			for _, end := range []string{lane.From, lane.To} {
				fixtureEnd := fixture.nodes[slices.IndexFunc(fixture.nodes, func(node sim.Node) bool { return node.ID == end })].Position
				if math.Abs(distance(*moved.Control, nodes[id+"-"+end])-distance(*lane.Control, fixtureEnd)) > 1e-9 {
					t.Fatalf("%s lane %s: the control point moved", id, lane.ID)
				}
			}
		}
		// outward is the distance of a point outward of the aisle, away
		// from the diverge node.
		entry, exit, diverge := nodes[id+"-entry"], nodes[id+"-exit"], nodes[id+"-diverge"]
		length := distance(entry, exit)
		normal := sim.Point{X: -(exit.Y - entry.Y) / length, Y: (exit.X - entry.X) / length}
		outward := func(point sim.Point) float64 {
			return (point.X-entry.X)*normal.X + (point.Y-entry.Y)*normal.Y
		}
		if outward(diverge) > 0 {
			normal = scale(normal, -1)
		}
		for bay := range compactBays {
			if berth := nodes[fmt.Sprintf("%s-berth-%d", id, bay+1)]; outward(berth) < 3 {
				t.Fatalf("%s bay %d is %g m outward of the aisle", id, bay+1, outward(berth))
			}
		}
		if bypass := lanes[id+"-bypass"]; outward(*bypass.Control) > -30 || bypass.StationRole != sim.StationThroughRole || bypass.StationID != id {
			t.Fatalf("%s bypass %+v with its control %g m outward", id, bypass, outward(*bypass.Control))
		}
		for name, role := range map[string]sim.StationLaneRole{"approach": sim.StationBerthAccessRole, "leave": sim.StationDepartureRole} {
			if lane := lanes[id+"-"+name]; lane.StationRole != role || lane.StationID != id || lane.SeparationGroup != "" {
				t.Fatalf("%s lane %+v", id, lane)
			}
		}
	}
}

// TestLondonCompactStations builds each preset in the opt-out layout with
// its default Compact stations, with and without the default depots. Each
// project is the opt-out project outside the replaced stations, so the
// heading search and the mirror pass keep every other station. With the
// depots, the project is the preset.
func TestLondonCompactStations(t *testing.T) {
	t.Parallel()
	for _, preset := range londonVariantPresets {
		for _, lanes := range []int{0, londonDepotLanes} {
			t.Run(fmt.Sprintf("%s with %d depot lanes", preset.name, lanes), func(t *testing.T) {
				t.Parallel()
				options := ordinaryLondonOptions(preset.options())
				options.CompactStations = suggestedCompact(preset)
				if lanes != 0 {
					options.ParkingLanes, options.ParkingBerths = lanes, londonDepotSlots
				}
				config, err := preset.build(options)
				if err != nil {
					t.Fatal(err)
				}
				checkLondonCompact(t, config, options.CompactStations, londonStationPods)
				replaced := slices.Clone(options.CompactStations)
				if lanes != 0 {
					replaced = append(replaced, londonFacilityIDs()...)
					checkLondonDepots(t, config, lanes, londonDepotSlots, londonParkingPods)
					if !reflect.DeepEqual(config, preset.preset()) {
						t.Fatal("the default layout is not the preset")
					}
				} else if !strings.HasSuffix(config.Name, customCapacitySuffix) {
					t.Fatalf("name %q", config.Name)
				}
				checkLondonParity(t, preset.ordinary(t), config, replaced...)
			})
		}
	}
}

// TestLondonCompactEachStation builds LondonFull in the opt-out layout
// with each default Compact station alone as a Compact station, and with
// Paddington, whose four berths are the bay count. With -short, it builds
// Borough and Paddington. Each project is the opt-out project outside the
// station.
func TestLondonCompactEachStation(t *testing.T) {
	t.Parallel()
	reference := londonVariantPresets[1].ordinary(t)
	for _, id := range append(slices.Clone(londonCompactStations), "940GZZLUPAC") {
		if testing.Short() && id != "940GZZLUBOR" && id != "940GZZLUPAC" {
			continue
		}
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			options := ordinaryLondonOptions(DefaultLondonFullOptions())
			options.CompactStations = []string{id}
			config, err := LondonFullWith(options)
			if err != nil {
				t.Fatal(err)
			}
			checkLondonCompact(t, config, []string{id}, londonStationPods)
			checkLondonParity(t, reference, config, id)
			if !strings.HasSuffix(config.Name, customCapacitySuffix) {
				t.Fatalf("name %q", config.Name)
			}
		})
	}
}

// TestLondonCompactIsNotThePreset checks that a Compact station with the
// default berth count of the preset does not give the cached preset.
func TestLondonCompactIsNotThePreset(t *testing.T) {
	t.Parallel()
	tests := []struct {
		preset londonVariantPreset
		id     string
	}{{londonVariantPresets[0], "940GZZLUBOR"}, {londonVariantPresets[1], "940GZZLUPAC"}}
	for _, test := range tests {
		options := test.preset.options()
		options.CompactStations = []string{test.id}
		config, err := test.preset.build(options)
		if err != nil {
			t.Fatal(err)
		}
		if len(config.Network.ManeuverGroups) != 1 || config.Name != test.preset.name+customCapacitySuffix {
			t.Fatalf("%s: %d groups, name %q", test.preset.name, len(config.Network.ManeuverGroups), config.Name)
		}
	}
}

// TestLondonCompactPods checks the pods of a Compact station: up to its
// four bays, at the first bays.
func TestLondonCompactPods(t *testing.T) {
	t.Parallel()
	options := DefaultLondonCentralOptions()
	options.CompactStations = []string{"940GZZLUBOR"}
	options.Pods = map[string]int{"940GZZLUBOR": 4}
	config, err := LondonCentralWith(options)
	if err != nil {
		t.Fatal(err)
	}
	checkLondonCompact(t, config, options.CompactStations, 4)
}

// TestLondonCompactRefusals checks the refusals of the Compact stations
// and their order: after the station IDs of the overrides and the depot
// lanes, each listed ID in order is checked, then the count, then the
// pods of each station.
func TestLondonCompactRefusals(t *testing.T) {
	t.Parallel()
	nine := append(slices.Clone(londonCompactStations[:7]), "940GZZLUEMB", "940GZZLUKSX")
	tests := []struct {
		name   string
		change func(*LondonOptions)
		err    string
	}{
		{"unknown", func(options *LondonOptions) { options.CompactStations = []string{"940GZZLURGP", "940GZZLUXXX"} }, `unknown London station ID "940GZZLUXXX"`},
		{"Parking", func(options *LondonOptions) { options.CompactStations = []string{"parking-west"} },
			"West London Parking (parking-west) is a Parking facility, not a passenger station for Compact bays"},
		{"repeated", func(options *LondonOptions) {
			options.CompactStations = []string{"940GZZLURGP", "940GZZLUBOR", "940GZZLURGP"}
		},
			"Regent's Park (940GZZLURGP) is listed more than once as a Compact station"},
		{"nine", func(options *LondonOptions) { options.CompactStations = nine }, "9 Compact stations, want at most 8"},
		{"nine with a repeated ID", func(options *LondonOptions) { options.CompactStations = append(slices.Clone(nine[:8]), nine[0]) },
			"Regent's Park (940GZZLURGP) is listed more than once as a Compact station"},
		{"overrides first", func(options *LondonOptions) {
			options.CompactStations, options.Berths = []string{"940GZZLUXXX"}, map[string]int{"940GZZLUYYY": 2}
		}, `unknown London station ID "940GZZLUYYY"`},
		{"depot lanes first", func(options *LondonOptions) { options.CompactStations, options.ParkingLanes = nine, -1 }, "parking lanes must not be negative"},
		{"five pods", func(options *LondonOptions) {
			options.CompactStations, options.Pods = []string{"940GZZLUBOR"}, map[string]int{"940GZZLUBOR": 5}
		}, "Borough (940GZZLUBOR) has 5 pods, want 0 to its 4 berths"},
		{"berths before pods", func(options *LondonOptions) {
			options.CompactStations, options.Berths, options.Pods = []string{"940GZZLUBOR"}, map[string]int{"940GZZLUBOR": 0}, map[string]int{"940GZZLUBOR": 5}
		}, "Borough (940GZZLUBOR) has 0 berths, want 1 to 200"},
		{"count before pods", func(options *LondonOptions) { options.CompactStations, options.StationPods = nine, 5 }, "9 Compact stations, want at most 8"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := DefaultLondonCentralOptions()
			test.change(&options)
			if _, err := LondonCentralWith(options); err == nil || err.Error() != test.err {
				t.Fatalf("error %v, want %q", err, test.err)
			}
		})
	}
}

// compactAuditNetwork returns a network with an ordinary station "a" and
// a Compact station "c", with no roads, and the hard conflicts of the
// London audit of the network.
func compactAuditNetwork(a, c londonStationShape) []layoutConflict {
	var network sim.Network
	addLondonStation(&network, londonStationInput{id: "a", name: "A", shape: a})
	addLondonStation(&network, londonStationInput{id: "c", name: "C", shape: c, compact: true})
	var hard []layoutConflict
	for _, conflict := range auditLondonLayout(network, londonAuditInput{}) {
		if !conflict.soft {
			hard = append(hard, conflict)
		}
	}
	return hard
}

// TestLondonCompactAudit checks the London audit of a Compact station c at
// heading 0, far from the origin. Its aisle is 200 m out, its bays reach
// about 204 m out, and its bypass about 184 m out.
func TestLondonCompactAudit(t *testing.T) {
	t.Parallel()
	center := sim.Point{X: -6000, Y: 4000}
	at := func(x, y float64) sim.Point { return sim.Point{X: center.X + x, Y: center.Y + y} }
	c := londonStationShape{center: center, berths: 2, pitch: londonBerthPitch}
	t.Run("clear", func(t *testing.T) {
		t.Parallel()
		if hard := compactAuditNetwork(londonStationShape{center: at(180, 0), berths: 2, pitch: londonBerthPitch}, c); len(hard) != 0 {
			t.Fatalf("hard conflicts %+v", hard)
		}
	})
	t.Run("group lane near a core lane", func(t *testing.T) {
		t.Parallel()
		// The diverge node of a is beside bay 2, about 33.5 m outward of
		// it and 37 m outward of the aisle. The merge node is more than 36
		// m from the leave lane.
		hard := compactAuditNetwork(londonStationShape{center: at(157, 30), berths: 2, pitch: londonBerthPitch}, c)
		if !slices.ContainsFunc(hard, func(conflict layoutConflict) bool {
			return conflict.station == "a" && conflict.reason == "is nearer than 36 meters to" && strings.HasPrefix(conflict.second, `C lane "c-bay-`)
		}) || slices.ContainsFunc(hard, func(conflict layoutConflict) bool {
			return strings.Contains(conflict.second, `"c-approach"`) || strings.Contains(conflict.second, `"c-leave"`) || strings.Contains(conflict.second, `"c-bypass"`)
		}) {
			t.Fatalf("conflicts %+v", hard)
		}
	})
	t.Run("other node inside the Compact area", func(t *testing.T) {
		t.Parallel()
		// The diverge and merge nodes of a are 2 m outward of the aisle,
		// between the bays and the bypass.
		hard := compactAuditNetwork(londonStationShape{center: at(122, 0), berths: 2, pitch: londonBerthPitch}, c)
		for _, node := range []string{"a-diverge", "a-merge"} {
			if !hasConflict(hard, "a", fmt.Sprintf("node %q", node), "is inside the area of") {
				t.Fatalf("no conflict of %s in %+v", node, hard)
			}
		}
	})
}

// TestLondonCompactArea checks the area of a Compact station at turned and
// mirrored headings: each node of the station other than the entry and
// the exit is inside it, and points 1 m outside it are not.
func TestLondonCompactArea(t *testing.T) {
	t.Parallel()
	for _, degrees := range []float64{0, 37, 90, 200, 315} {
		for _, mirrored := range []bool{false, true} {
			shape := londonStationShape{center: sim.Point{X: 2500, Y: -900}, direction: degrees * math.Pi / 180, mirrored: mirrored, berths: 2, pitch: londonBerthPitch}
			var network sim.Network
			addLondonStation(&network, londonStationInput{id: "c", name: "C", shape: shape, compact: true})
			owners := londonLaneOwners(network)
			var inner []auditLane
			for _, lane := range newAuditLanes(network) {
				if owners[lane.lane.ID] == "c" && lane.lane.StationRole != sim.StationEntryRole && lane.lane.StationRole != sim.StationExitRole {
					inner = append(inner, lane)
				}
			}
			nodes := make(map[string]sim.Point)
			for _, node := range network.Nodes {
				nodes[node.ID] = node.Position
			}
			area := laneArea(nodes["c-entry"], nodes["c-exit"], inner)
			frame := shape.frame()
			for _, node := range network.Nodes {
				if strings.HasPrefix(node.ID, "c-bay-") || strings.HasPrefix(node.ID, "c-branch-") || node.ID == "c-hold" || node.ID == "c-receive" {
					if !inArea(node.Position, area) {
						t.Fatalf("%g degrees, mirrored %t: node %s is outside %+v", degrees, mirrored, node.ID, area)
					}
				}
			}
			for _, outside := range []sim.Point{frame.position(0, 205), frame.position(0, 183), frame.position(56, 200), frame.position(-56, 200)} {
				if inArea(outside, area) {
					t.Fatalf("%g degrees, mirrored %t: point %+v is inside", degrees, mirrored, outside)
				}
			}
			if !inArea(frame.position(0, 195), area) {
				t.Fatalf("%g degrees, mirrored %t: the middle is outside", degrees, mirrored)
			}
		}
	}
}

// TestLondonSizeEstimate checks the node and lane counts that the size
// check estimates against the counts of the network that the builder makes,
// for the default layout, and for the opt-out layout alone, with Compact
// stations, with depots, and with both. Limits at the
// estimate let the build go on. A lane limit one below the estimate, with
// the node limit at its estimate, refuses the network.
func TestLondonSizeEstimate(t *testing.T) {
	t.Parallel()
	full, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	unlimited := math.MaxInt
	for _, preset := range londonVariantPresets {
		source := full
		if preset.name == "LondonCentral" {
			source = londonSource{}
			if err := decodeLondonSource(&source); err != nil {
				t.Fatal(err)
			}
		}
		layouts := map[string]func(*LondonOptions){
			"default": func(options *LondonOptions) { *options = preset.options() },
			"opt-out": func(*LondonOptions) {},
			"compact": func(options *LondonOptions) { options.CompactStations = suggestedCompact(preset) },
			"depots":  func(options *LondonOptions) { options.ParkingLanes = 2 },
			"both": func(options *LondonOptions) {
				options.CompactStations, options.ParkingLanes = suggestedCompact(preset), 3
			},
		}
		for name, change := range layouts {
			t.Run(preset.name+" "+name, func(t *testing.T) {
				t.Parallel()
				options := ordinaryLondonOptions(preset.options())
				change(&options)
				capacity, err := options.resolve(source)
				if err != nil {
					t.Fatal(err)
				}
				estimate := func(limits londonLimits, unit string) int {
					t.Helper()
					_, sizeErr := londonNetworkWithHeadings(source, capacity, nil, limits)
					var count int
					if sizeErr == nil {
						t.Fatalf("limits %+v: no error", limits)
					}
					if _, scanErr := fmt.Sscanf(sizeErr.Error(), "generated London network needs %d "+unit, &count); scanErr != nil {
						t.Fatalf("limits %+v: error %v", limits, sizeErr)
					}
					return count
				}
				nodes := estimate(londonLimits{nodes: 0, lanes: unlimited}, "nodes")
				lanes := estimate(londonLimits{nodes: unlimited, lanes: 0}, "lanes")
				if got := estimate(londonLimits{nodes: nodes, lanes: lanes - 1}, "lanes"); got != lanes {
					t.Fatalf("lane limit %d: estimate %d lanes, want %d", lanes-1, got, lanes)
				}
				network, err := londonNetworkWithHeadings(source, capacity, nil, londonLimits{nodes: nodes, lanes: lanes})
				if err != nil {
					t.Fatal(err)
				}
				if len(network.Nodes) != nodes || len(network.Lanes) != lanes {
					t.Fatalf("estimate %d nodes and %d lanes, network has %d and %d", nodes, lanes, len(network.Nodes), len(network.Lanes))
				}
			})
		}
	}
}
