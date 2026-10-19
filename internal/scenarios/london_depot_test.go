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

// londonVariantPreset is a London preset with its options and its builder.
type londonVariantPreset struct {
	name    string
	options func() LondonOptions
	build   func(LondonOptions) (project.Config, error)
	preset  func() project.Config
}

var londonVariantPresets = []londonVariantPreset{
	{name: "LondonCentral", options: DefaultLondonCentralOptions, build: LondonCentralWith, preset: LondonCentral},
	{name: "LondonFull", options: DefaultLondonFullOptions, build: LondonFullWith, preset: LondonFull},
}

// replacedLondonID reports whether a node or lane ID belongs to the inside
// of a replaced station: an ID of the station other than its diverge and
// merge nodes, its road lanes, and its movement lanes.
func replacedLondonID(id string, replaced []string) bool {
	return slices.ContainsFunc(replaced, func(station string) bool {
		rest, ok := strings.CutPrefix(id, station+"-")
		return ok && rest != "diverge" && rest != "merge" && !strings.HasPrefix(rest, "road-") && !strings.HasPrefix(rest, "move-")
	})
}

// checkLondonParity checks that the variant is the reference outside the
// replaced stations. Each node and lane outside them has the same ID, the
// same position or ends and control, and the same metadata, and each other
// station is the same. Each new node and lane belongs to a replaced
// station. The demand and the settings are the same, and the pods outside
// the replaced stations are the same but for their class.
func checkLondonParity(t *testing.T, reference, variant project.Config, replaced ...string) {
	t.Helper()
	nodes := make(map[string]sim.Node, len(variant.Network.Nodes))
	for _, node := range variant.Network.Nodes {
		nodes[node.ID] = node
	}
	for _, node := range reference.Network.Nodes {
		if !replacedLondonID(node.ID, replaced) && nodes[node.ID] != node {
			t.Fatalf("node %+v is %+v", node, nodes[node.ID])
		}
		delete(nodes, node.ID)
	}
	for id := range nodes {
		if !replacedLondonID(id, replaced) {
			t.Fatalf("new node %q outside the replaced stations", id)
		}
	}
	lanes := make(map[string]sim.Lane, len(variant.Network.Lanes))
	for _, lane := range variant.Network.Lanes {
		lanes[lane.ID] = lane
	}
	for _, lane := range reference.Network.Lanes {
		if got, ok := lanes[lane.ID]; !replacedLondonID(lane.ID, replaced) && (!ok || !reflect.DeepEqual(got, lane)) {
			t.Fatalf("lane %+v is %+v", lane, got)
		}
		delete(lanes, lane.ID)
	}
	for id := range lanes {
		if !replacedLondonID(id, replaced) {
			t.Fatalf("new lane %q outside the replaced stations", id)
		}
	}
	if len(variant.Network.Stations) != len(reference.Network.Stations) {
		t.Fatalf("%d stations, want %d", len(variant.Network.Stations), len(reference.Network.Stations))
	}
	for index, station := range reference.Network.Stations {
		got := variant.Network.Stations[index]
		if slices.Contains(replaced, station.ID) {
			got.Berths, station.Berths = nil, nil
		}
		if !reflect.DeepEqual(got, station) {
			t.Fatalf("station %+v is %+v", station, got)
		}
	}
	if !reflect.DeepEqual(variant.Demand, reference.Demand) || !reflect.DeepEqual(variant.DemandProfiles, reference.DemandProfiles) ||
		!reflect.DeepEqual(variant.Geo, reference.Geo) || variant.PlatoonLimit != reference.PlatoonLimit {
		t.Fatal("the demand or the settings differ")
	}
	pods := func(config project.Config) []sim.Placement {
		var kept []sim.Placement
		for _, pod := range config.Fleet {
			if !slices.Contains(replaced, pod.StationID) {
				pod.Class = ""
				kept = append(kept, pod)
			}
		}
		return kept
	}
	if !slices.Equal(pods(variant), pods(reference)) {
		t.Fatal("the pods outside the replaced stations differ")
	}
	if !strings.HasSuffix(variant.Name, customCapacitySuffix) {
		t.Fatalf("name %q", variant.Name)
	}
}

// londonFacilityIDs returns the IDs of the Parking facilities.
func londonFacilityIDs() []string {
	var ids []string
	for _, parking := range londonParkingFacilities {
		ids = append(ids, parking.ID)
	}
	return ids
}

// longestLondonDepot returns the most slots of a depot of lanes lanes
// that is at most londonDepotLength long.
func longestLondonDepot(lanes int) int {
	slots := 1
	for (depotLayout{lanes: lanes, slots: slots + 1}).length() <= londonDepotLength {
		slots++
	}
	return slots
}

// TestLondonDepotLengthTable checks the most slots of each lane count at
// the 200 m limit, as docs/london-full.md gives them.
func TestLondonDepotLengthTable(t *testing.T) {
	t.Parallel()
	want := []int{16, 15, 15, 13, 12, 11, 10, 9}
	for lanes := 1; lanes <= sim.MaxDepotLanes; lanes++ {
		if got := longestLondonDepot(lanes); got != want[lanes-1] {
			t.Errorf("%d lanes: %d slots, want %d", lanes, got, want[lanes-1])
		}
	}
}

// TestLondonDepots builds each preset with depots of 1 to 8 lanes, at one
// slot and at the most slots of the length limit. With -short, it builds
// LondonCentral with 1 and 8 lanes and LondonFull with 4 lanes. Each
// project has the three depots with slot IDs of their stations, takes its
// Parking pods at the heads of the lanes first, and is the preset outside
// the Parking facilities.
func TestLondonDepots(t *testing.T) {
	t.Parallel()
	for _, preset := range londonVariantPresets {
		reference := preset.preset()
		for lanes := 1; lanes <= sim.MaxDepotLanes; lanes++ {
			if testing.Short() && (preset.name == "LondonCentral") != (lanes == 1 || lanes == sim.MaxDepotLanes) {
				continue
			}
			for _, slots := range []int{1, longestLondonDepot(lanes)} {
				if testing.Short() && slots == 1 {
					continue
				}
				t.Run(fmt.Sprintf("%s %dx%d", preset.name, lanes, slots), func(t *testing.T) {
					t.Parallel()
					options := preset.options()
					options.ParkingLanes, options.ParkingBerths = lanes, slots
					options.ParkingPods = min(lanes*slots, londonParkingPods)
					config, err := preset.build(options)
					if err != nil {
						t.Fatal(err)
					}
					checkLondonDepots(t, config, lanes, slots, options.ParkingPods)
					checkLondonParity(t, reference, config, londonFacilityIDs()...)
				})
			}
		}
	}
}

// checkLondonDepots checks the depots of each Parking facility and the
// order of their pods.
func checkLondonDepots(t *testing.T, config project.Config, lanes, slots, pods int) {
	t.Helper()
	if len(config.Network.Depots) != len(londonParkingFacilities) {
		t.Fatalf("%d depots", len(config.Network.Depots))
	}
	for index, parking := range londonParkingFacilities {
		depot := config.Network.Depots[index]
		if depot.Station != parking.ID || len(depot.Lanes) != lanes {
			t.Fatalf("depot %d is %+v", index, depot)
		}
		for lane, ids := range depot.Lanes {
			for slot, id := range ids.BerthIDs {
				if want := fmt.Sprintf("%s-%d-%02d", parking.ID, lane+1, slot+1); id != want || len(ids.BerthIDs) != slots {
					t.Fatalf("depot %s lane %d slot %d is %q, want %q of %d", parking.ID, lane+1, slot+1, id, want, slots)
				}
			}
		}
		var got []string
		for _, pod := range config.Fleet {
			if pod.StationID == parking.ID {
				got = append(got, pod.BerthID)
			}
		}
		var want []string
		for slot := 0; len(want) < pods; slot++ {
			for lane := 0; lane < lanes && len(want) < pods; lane++ {
				want = append(want, depot.Lanes[lane].BerthIDs[slot])
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s pods at %q, want %q", parking.ID, got, want)
		}
	}
	for index, pod := range config.Fleet {
		if want := fmt.Sprintf("london-pod-%03d", index+1); pod.ID != want {
			t.Fatalf("pod %d is %q, want %q", index, pod.ID, want)
		}
	}
}

// TestLondonDepotSlotsOfOneFacility checks that a -berths override of one
// facility sets the slots of its lanes only.
func TestLondonDepotSlotsOfOneFacility(t *testing.T) {
	t.Parallel()
	options := DefaultLondonCentralOptions()
	options.ParkingLanes, options.ParkingBerths = 2, 6
	options.Berths = map[string]int{"parking-north": 9}
	config, err := LondonCentralWith(options)
	if err != nil {
		t.Fatal(err)
	}
	for _, depot := range config.Network.Depots {
		want := 6
		if depot.Station == "parking-north" {
			want = 9
		}
		if len(depot.Lanes) != 2 || len(depot.Lanes[1].BerthIDs) != want {
			t.Fatalf("depot %+v, want 2 lanes of %d slots", depot, want)
		}
	}
}

// TestLondonDepotRefusals checks the refusals of the depot options and
// their order: a negative lane count after the station IDs, then for each
// facility in order the lane count, the slots, the length, and the pods.
func TestLondonDepotRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*LondonOptions)
		err    string
	}{
		{"negative lanes", func(options *LondonOptions) { options.ParkingLanes = -1 }, "parking lanes must not be negative"},
		{"unknown station before negative lanes", func(options *LondonOptions) {
			options.ParkingLanes, options.Berths = -1, map[string]int{"parking-south": 4}
		}, `unknown London station ID "parking-south"`},
		{"pitch before negative lanes", func(options *LondonOptions) { options.ParkingLanes, options.BerthPitch = -1, 10 }, "berth pitch must be"},
		{"nine lanes", func(options *LondonOptions) { options.ParkingLanes = 9 }, "West London Parking (parking-west): parking lanes must be at most 8"},
		{"nine lanes before the length", func(options *LondonOptions) { options.ParkingLanes, options.ParkingBerths = 9, 17 }, "West London Parking (parking-west): parking lanes must be at most 8"},
		{"no slots", func(options *LondonOptions) { options.ParkingLanes, options.ParkingBerths = 2, 0 },
			"West London Parking (parking-west): the depot has 2 lanes of 0 slots, want 1 to 200 slots in all"},
		{"slots before the length", func(options *LondonOptions) { options.ParkingLanes, options.ParkingBerths = 8, 26 },
			"West London Parking (parking-west): the depot has 8 lanes of 26 slots, want 1 to 200 slots in all"},
		{"17 slots in one lane", func(options *LondonOptions) { options.ParkingLanes, options.ParkingBerths = 1, 17 },
			"West London Parking (parking-west): the depot of 1 lanes of 17 slots is 204.03 meters long, more than 200 meters"},
		{"six lanes of 12 slots", func(options *LondonOptions) { options.ParkingLanes = 6 },
			"West London Parking (parking-west): the depot of 6 lanes of 12 slots is 202.03 meters long, more than 200 meters"},
		{"one long facility", func(options *LondonOptions) {
			options.ParkingLanes, options.ParkingBerths, options.Berths = 2, 6, map[string]int{"parking-east": 16}
		}, "East London Parking (parking-east): the depot of 2 lanes of 16 slots is 200.03 meters long, more than 200 meters"},
		{"length before the pods", func(options *LondonOptions) {
			options.ParkingLanes, options.ParkingBerths, options.ParkingPods = 1, 17, 18
		}, "West London Parking (parking-west): the depot of 1 lanes of 17 slots"},
		{"more pods than slots", func(options *LondonOptions) {
			options.ParkingLanes, options.ParkingBerths, options.ParkingPods = 1, 4, 5
		},
			"West London Parking (parking-west) has 5 pods, want 0 to its 4 berths"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := DefaultLondonCentralOptions()
			test.change(&options)
			if _, err := LondonCentralWith(options); err == nil || !strings.HasPrefix(err.Error(), test.err) {
				t.Fatalf("error %v, want %q", err, test.err)
			}
		})
	}
}

// TestLondonDepotCapacityIsNotThePreset checks that depots with the
// default slot count of a facility do not give the preset.
func TestLondonDepotCapacityIsNotThePreset(t *testing.T) {
	t.Parallel()
	for _, preset := range londonVariantPresets {
		options := preset.options()
		options.ParkingLanes, options.ParkingBerths = 1, londonParkingBerths
		config, err := preset.build(options)
		if err != nil {
			t.Fatal(err)
		}
		if len(config.Network.Depots) != len(londonParkingFacilities) || config.Name != preset.name+customCapacitySuffix {
			t.Fatalf("%s: %d depots, name %q", preset.name, len(config.Network.Depots), config.Name)
		}
	}
}

// auditPairNetwork returns a network with an ordinary station "a" and a
// depot station "b", with no roads, and the hard conflicts of the London
// audit of the network. With depotFirst, b comes first in the network.
func auditPairNetwork(a, b londonStationShape, layout depotLayout, depotFirst ...bool) (sim.Network, []layoutConflict) {
	var network sim.Network
	addLondonStation(&network, londonStationInput{id: "a", name: "A", shape: a})
	addLondonStation(&network, londonStationInput{id: "b", name: "B", shape: b, parking: true, depot: layout})
	if len(depotFirst) > 0 && depotFirst[0] {
		network.Stations[0], network.Stations[1] = network.Stations[1], network.Stations[0]
	}
	var hard []layoutConflict
	for _, conflict := range auditLondonLayout(network, londonAuditInput{}) {
		if !conflict.soft {
			hard = append(hard, conflict)
		}
	}
	return network, hard
}

// hasConflict reports whether a conflict has the station, the reason, and
// the first item.
func hasConflict(conflicts []layoutConflict, station, first, reason string) bool {
	return slices.ContainsFunc(conflicts, func(conflict layoutConflict) bool {
		return conflict.station == station && conflict.first == first && conflict.reason == reason
	})
}

// TestLondonDepotAudit checks the London audit of a depot station. The
// stations lie far from the origin, so an area with a corner at the origin
// shows. The depot b of 2 lanes of 6 slots is 140.03 m long. Its through
// lane is 200 m out, and its lanes are 212 and 218 m out.
func TestLondonDepotAudit(t *testing.T) {
	t.Parallel()
	layout := depotLayout{lanes: 2, slots: 6}
	center := sim.Point{X: 5000, Y: 5000}
	at := func(x, y float64) sim.Point { return sim.Point{X: center.X + x, Y: center.Y + y} }
	b := londonStationShape{center: center, berths: londonParkingBerths, pitch: londonBerthPitch}
	t.Run("clear", func(t *testing.T) {
		t.Parallel()
		// The diverge node of a is 50 m outside depot lane 2.
		a := londonStationShape{center: at(188, 0), berths: 2, pitch: londonBerthPitch}
		if _, hard := auditPairNetwork(a, b, layout); len(hard) != 0 {
			t.Fatalf("hard conflicts %+v", hard)
		}
	})
	t.Run("depot lane near a core lane", func(t *testing.T) {
		t.Parallel()
		// The diverge node of a is 30 m outside depot lane 2 and 48 m
		// outside the through lane of b.
		a := londonStationShape{center: at(168, 0), berths: 2, pitch: londonBerthPitch}
		_, hard := auditPairNetwork(a, b, layout)
		if !slices.ContainsFunc(hard, func(conflict layoutConflict) bool {
			return conflict.station == "a" && conflict.reason == "is nearer than 36 meters to" &&
				strings.HasPrefix(conflict.second, `B lane "b-`) && !strings.Contains(conflict.second, `"b-through"`) && !strings.Contains(conflict.second, `"b-access-`)
		}) {
			t.Fatalf("no depot lane conflict in %+v", hard)
		}
		// The audit finds the same pair when the depot station comes
		// first.
		_, hard = auditPairNetwork(a, b, layout, true)
		if !slices.ContainsFunc(hard, func(conflict layoutConflict) bool {
			return conflict.station == "b" && conflict.reason == "is nearer than 36 meters to" && strings.HasPrefix(conflict.first, `lane "b-`) &&
				!strings.HasPrefix(conflict.first, `lane "b-through"`) && !strings.HasPrefix(conflict.first, `lane "b-access-`)
		}) {
			t.Fatalf("no depot lane conflict with the depot first in %+v", hard)
		}
	})
	t.Run("depot lane across a guideway", func(t *testing.T) {
		t.Parallel()
		network, _ := auditPairNetwork(londonStationShape{center: at(-5000, 0), berths: 2, pitch: londonBerthPitch}, b, layout)
		// The guideway crosses both lead-in lanes 35 m along the axis
		// from the entry, and not the through lane.
		network.Nodes = append(network.Nodes, sim.Node{ID: "g-1", Position: at(209, -35)}, sim.Node{ID: "g-2", Position: at(240, -35)})
		network.Lanes = append(network.Lanes, sim.Lane{ID: "guideway", From: "g-1", To: "g-2", SpeedLimit: speedLimit, SeparationGroup: "guideway"})
		conflicts := auditLondonLayout(network, londonAuditInput{})
		for _, lane := range []string{"b-in-2", "b-in-1"} {
			if !hasConflict(conflicts, "b", fmt.Sprintf("lane %q", lane), "crosses") {
				t.Fatalf("no crossing of %s in %+v", lane, conflicts)
			}
		}
	})
	t.Run("depot node inside the other area", func(t *testing.T) {
		t.Parallel()
		// The area of a runs from 200 to 290 m out of its center, and 100
		// m to each side. Depot lane 1 is 10 m inside it, and the entry
		// and exit of b are outside it.
		a := londonStationShape{center: at(2, 0), berths: 1, pitch: londonBerthPitch}
		_, hard := auditPairNetwork(a, b, layout)
		if !hasConflict(hard, "b", `node "b-slot-1-01"`, "is inside the area of") || hasConflict(hard, "b", `node "b-entry"`, "is inside the area of") {
			t.Fatalf("conflicts %+v", hard)
		}
	})
	t.Run("other node inside the depot area", func(t *testing.T) {
		t.Parallel()
		// The diverge and merge nodes of a are 9 m outside the through
		// lane of b, inside the depot.
		a := londonStationShape{center: at(129, 0), berths: 2, pitch: londonBerthPitch}
		_, hard := auditPairNetwork(a, b, layout)
		for _, node := range []string{"a-diverge", "a-merge"} {
			if !hasConflict(hard, "a", fmt.Sprintf("node %q", node), "is inside the area of") {
				t.Fatalf("no conflict of %s in %+v", node, hard)
			}
		}
	})
}

// TestLondonDepotArea checks the area of a depot at turned and mirrored
// headings. It is the rectangle from the entry to the exit along the
// station axis, and from the through lane to the outer depot lane, with
// its corners in order round it.
func TestLondonDepotArea(t *testing.T) {
	t.Parallel()
	layout := depotLayout{lanes: 3, slots: 5}
	half, outer := layout.length()/2, depotAcross(layout.lanes)
	for _, degrees := range []float64{0, 37, 90, 200, 315} {
		for _, mirrored := range []bool{false, true} {
			shape := londonStationShape{center: sim.Point{X: -3000, Y: 7000}, direction: degrees * math.Pi / 180, mirrored: mirrored, berths: 12, pitch: londonBerthPitch}
			var network sim.Network
			addLondonStation(&network, londonStationInput{id: "b", name: "B", shape: shape, parking: true, depot: layout})
			owners := londonLaneOwners(network)
			var inner []auditLane
			for _, lane := range newAuditLanes(network) {
				if owners[lane.lane.ID] == "b" && lane.lane.StationRole != sim.StationEntryRole && lane.lane.StationRole != sim.StationExitRole {
					inner = append(inner, lane)
				}
			}
			nodes := make(map[string]sim.Point)
			for _, node := range network.Nodes {
				nodes[node.ID] = node.Position
			}
			area := laneArea(nodes["b-entry"], nodes["b-exit"], inner)
			frame := shape.frame()
			want := []sim.Point{frame.position(-half, 200), frame.position(half, 200), frame.position(half, 200+outer), frame.position(-half, 200+outer)}
			for _, corner := range want {
				if !slices.ContainsFunc(area[:], func(point sim.Point) bool { return math.Hypot(point.X-corner.X, point.Y-corner.Y) < 1e-6 }) {
					t.Fatalf("%g degrees, mirrored %t: area %+v has no corner at %+v", degrees, mirrored, area, corner)
				}
			}
			for along := -half; along <= half; along += half / 4 {
				for _, out := range []float64{200.001, 200 + outer/2, 200 + outer - 0.001} {
					if !inArea(frame.position(along*0.9999, out), area) {
						t.Fatalf("%g degrees, mirrored %t: point %g, %g is outside", degrees, mirrored, along, out)
					}
				}
				for _, out := range []float64{199, 201 + outer} {
					if inArea(frame.position(along, out), area) {
						t.Fatalf("%g degrees, mirrored %t: point %g, %g is inside", degrees, mirrored, along, out)
					}
				}
			}
			// The slot nodes of the outer lane lie on the edge.
			for _, node := range network.Nodes {
				if strings.HasPrefix(node.ID, "b-slot-") && !strings.HasPrefix(node.ID, fmt.Sprintf("b-slot-%d-", layout.lanes)) && !inArea(node.Position, area) {
					t.Fatalf("%g degrees, mirrored %t: slot node %s is outside", degrees, mirrored, node.ID)
				}
			}
		}
	}
}
