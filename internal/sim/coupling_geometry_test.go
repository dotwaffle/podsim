package sim

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func couplingGeometryFixture() CouplingGeometryInput {
	compact := classBit(string(CompactClass))
	return CouplingGeometryInput{
		Contract: CompactPairV1CouplingContract,
		Network: Network{
			Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 200}}, {ID: "c", Position: Point{X: 400}}},
			Lanes: []Lane{{ID: "ab", From: "a", To: "b", SpeedLimit: 14, VehicleClasses: compact}, {ID: "bc", From: "b", To: "c", SpeedLimit: 7, VehicleClasses: compact}},
		},
		Sites: []CouplingSite{
			{ID: "assembly", LaneID: "ab", StartMeters: 20, EndMeters: 100, RearStagingMeters: 40, FrontStagingMeters: 52},
			{ID: "split", LaneID: "bc", StartMeters: 50, EndMeters: 140, RearStagingMeters: 70, FrontStagingMeters: 82},
		},
		Corridors: []CouplingCorridor{{ID: "corridor", AssemblySiteID: "assembly", SplitSiteID: "split", LaneIDs: []string{"ab", "bc"}}},
	}
}

func TestCouplingGeometryValidAndUnchanged(t *testing.T) {
	t.Parallel()
	input := couplingGeometryFixture()
	before := couplingGeometryFixture()
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("validator changed authored records")
	}
	// Ordinary curved and large-admitting paths remain outside the descriptor.
	input.Network.Nodes = append(input.Network.Nodes, Node{ID: "d", Position: Point{X: 200, Y: 100}})
	input.Network.Lanes = append(input.Network.Lanes, Lane{ID: "foreign", From: "a", To: "d", SpeedLimit: 14,
		Control: &Point{X: 100, Y: 80}, VehicleClasses: classBit(string(GroupClass))})
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatal("validator changed unrelated lane eligibility", err)
	}
	// This passes authored geometry only. The later owner stage must reject
	// conflicting grants at the shared node before any train can move.
}

func TestCouplingGeometryGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*CouplingGeometryInput)
	}{
		{"marker", func(i *CouplingGeometryInput) { i.Contract = "unknown" }},
		{"site cap", func(i *CouplingGeometryInput) { *i = couplingManySites(MaxCouplingSites+1, MaxCouplingCorridors) }},
		{"corridor cap", func(i *CouplingGeometryInput) { *i = couplingManySites(2, MaxCouplingCorridors+1) }},
		{"empty sites", func(i *CouplingGeometryInput) { i.Sites = nil }},
		{"empty corridors", func(i *CouplingGeometryInput) { i.Corridors = nil }},
		{"long ID", func(i *CouplingGeometryInput) { i.Sites[0].ID = strings.Repeat("a", 65) }},
		{"invalid UTF8", func(i *CouplingGeometryInput) { i.Sites[0].ID = string([]byte{255}) }},
		{"duplicate site", func(i *CouplingGeometryInput) { i.Sites[1].ID = i.Sites[0].ID }},
		{"unknown lane", func(i *CouplingGeometryInput) { i.Sites[0].LaneID = "missing" }},
		{"curve", func(i *CouplingGeometryInput) { i.Network.Lanes[0].Control = &Point{X: 100} }},
		{"implicit mask", func(i *CouplingGeometryInput) { i.Network.Lanes[0].VehicleClasses = 0 }},
		{"legacy mask", func(i *CouplingGeometryInput) { i.Network.Lanes[0].VehicleClasses |= classBit(string(LegacyClass)) }},
		{"group mask", func(i *CouplingGeometryInput) { i.Network.Lanes[0].VehicleClasses |= classBit(string(GroupClass)) }},
		{"express mask", func(i *CouplingGeometryInput) { i.Network.Lanes[0].VehicleClasses |= classBit(string(ExpressClass)) }},
		{"nan site", func(i *CouplingGeometryInput) { i.Sites[0].StartMeters = math.NaN() }},
		{"short site", func(i *CouplingGeometryInput) { i.Sites[0].EndMeters = 25 }},
		{"before lane", func(i *CouplingGeometryInput) { i.Sites[0].StartMeters = -1 }},
		{"past lane", func(i *CouplingGeometryInput) { i.Sites[0].EndMeters = 201 }},
		{"staging gap", func(i *CouplingGeometryInput) { i.Sites[0].FrontStagingMeters++ }},
		{"rear margin", func(i *CouplingGeometryInput) { i.Sites[0].RearStagingMeters = 21; i.Sites[0].FrontStagingMeters = 33 }},
		{"front margin", func(i *CouplingGeometryInput) { i.Sites[0].RearStagingMeters = 80; i.Sites[0].FrontStagingMeters = 92 }},
		{"unknown site", func(i *CouplingGeometryInput) { i.Corridors[0].SplitSiteID = "missing" }},
		{"same site", func(i *CouplingGeometryInput) { i.Corridors[0].SplitSiteID = "assembly" }},
		{"empty path", func(i *CouplingGeometryInput) { i.Corridors[0].LaneIDs = nil }},
		{"path cap", func(i *CouplingGeometryInput) { i.Corridors[0].LaneIDs = make([]string, expressMaxLanes+1) }},
		{"duplicate path lane", func(i *CouplingGeometryInput) { i.Corridors[0].LaneIDs = []string{"ab", "ab", "bc"} }},
		{"wrong endpoint", func(i *CouplingGeometryInput) { i.Corridors[0].LaneIDs = []string{"bc", "ab"} }},
		{"bent", func(i *CouplingGeometryInput) { i.Network.Nodes[2].Position.Y = 20 }},
		{"reversed", func(i *CouplingGeometryInput) { i.Network.Nodes[2].Position.X = -200 }},
		{"disconnected", func(i *CouplingGeometryInput) {
			i.Network.Nodes = append(i.Network.Nodes, Node{ID: "other", Position: Point{X: 200}})
			i.Network.Lanes[1].From = "other"
		}},
		{"zero segment", func(i *CouplingGeometryInput) { i.Network.Nodes[2].Position = i.Network.Nodes[1].Position }},
		{"nan position", func(i *CouplingGeometryInput) { i.Network.Nodes[0].Position.X = math.NaN() }},
		{"coordinate cap", func(i *CouplingGeometryInput) { i.Network.Nodes[2].Position.X = 100001 }},
		{"bad speed", func(i *CouplingGeometryInput) { i.Network.Lanes[0].SpeedLimit = math.Inf(1) }},
		{"duplicate corridor", func(i *CouplingGeometryInput) { i.Corridors = append(i.Corridors, i.Corridors[0]) }},
		{"orphan site", func(i *CouplingGeometryInput) {
			extra := i.Sites[0]
			extra.ID = "orphan"
			extra.StartMeters = 110
			extra.EndMeters = 200
			extra.RearStagingMeters = 130
			extra.FrontStagingMeters = 142
			i.Sites = append(i.Sites, extra)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := couplingGeometryFixture()
			tc.change(&input)
			want := ErrInvalidCouplingGeometry
			if tc.name == "marker" {
				want = ErrUnknownCouplingContract
			}
			if err := ValidateCouplingGeometry(input); !errors.Is(err, want) {
				t.Fatalf("bad geometry accepted: %v", err)
			}
		})
	}
}

// A site or corridor lane with a speed limit at MaxCouplingCorridorSpeed is
// valid. A lane just above it is refused at each place in a three-lane
// corridor: the assembly lane, a middle lane, and the split lane.
func TestCouplingGeometryCorridorSpeedBound(t *testing.T) {
	t.Parallel()
	above := math.Nextafter(MaxCouplingCorridorSpeed, math.Inf(1))
	for _, tc := range []struct {
		name   string
		speeds [3]float64
		valid  bool
	}{
		{"at bound", [3]float64{MaxCouplingCorridorSpeed, MaxCouplingCorridorSpeed, MaxCouplingCorridorSpeed}, true},
		{"assembly lane above", [3]float64{above, 7, 14}, false},
		{"middle lane above", [3]float64{14, above, 7}, false},
		{"split lane above", [3]float64{14, 7, above}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			input := couplingGeometryFixture()
			input.Network.Nodes = append(input.Network.Nodes, Node{ID: "m", Position: Point{X: 100}})
			input.Network.Lanes[0].To = "m"
			input.Network.Lanes = slices.Insert(input.Network.Lanes, 1, Lane{ID: "mb", From: "m", To: "b", VehicleClasses: classBit(string(CompactClass))})
			input.Corridors[0].LaneIDs = []string{"ab", "mb", "bc"}
			for i := range tc.speeds {
				input.Network.Lanes[i].SpeedLimit = tc.speeds[i]
			}
			err := ValidateCouplingGeometry(input)
			if tc.valid {
				if err != nil {
					t.Fatal("corridor at the speed bound refused", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidCouplingGeometry) || !strings.Contains(err.Error(), "exceeds the coupling bound") {
				t.Fatal("corridor lane above the speed bound accepted", err)
			}
		})
	}
}

func couplingManySites(siteCount, corridorCount int) CouplingGeometryInput {
	input := couplingGeometryFixture()
	input.Network.Nodes = input.Network.Nodes[:2]
	input.Network.Nodes[1].Position.X = float64(siteCount) * 60
	input.Network.Lanes = input.Network.Lanes[:1]
	input.Sites = make([]CouplingSite, siteCount)
	for index := range input.Sites {
		start := float64(index) * 60
		input.Sites[index] = CouplingSite{ID: "s" + strconv.Itoa(index), LaneID: "ab", StartMeters: start, EndMeters: start + 60,
			RearStagingMeters: start + 15, FrontStagingMeters: start + 27}
	}
	input.Corridors = make([]CouplingCorridor, corridorCount)
	for index := range input.Corridors {
		input.Corridors[index] = CouplingCorridor{ID: "c" + strconv.Itoa(index), AssemblySiteID: "s0", SplitSiteID: "s" + strconv.Itoa(1+index%(siteCount-1)), LaneIDs: []string{"ab"}}
	}
	return input
}

func TestCouplingMaximumDescriptors(t *testing.T) {
	t.Parallel()
	input := couplingManySites(MaxCouplingSites, MaxCouplingCorridors)
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatalf("bounded descriptor maximum rejected: %v", err)
	}
	// This is authored encoding-independent geometry, not a fleet, admission,
	// motion, throughput, or format qualification claim.
}

func TestCouplingOneLaneSiteOrder(t *testing.T) {
	t.Parallel()
	input := couplingGeometryFixture()
	input.Network.Nodes = input.Network.Nodes[:2]
	input.Network.Lanes = input.Network.Lanes[:1]
	input.Sites[1] = CouplingSite{ID: "split", LaneID: "ab", StartMeters: 110, EndMeters: 200, RearStagingMeters: 130, FrontStagingMeters: 142}
	input.Corridors[0].LaneIDs = []string{"ab"}
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatal(err)
	}
	input.Corridors[0].AssemblySiteID, input.Corridors[0].SplitSiteID = "split", "assembly"
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrInvalidCouplingGeometry) {
		t.Fatal("reversed site order accepted", err)
	}
	input.Corridors[0].AssemblySiteID, input.Corridors[0].SplitSiteID = "assembly", "split"
	input.Sites[1] = input.Sites[0]
	input.Sites[1].ID = "split"
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrInvalidCouplingGeometry) {
		t.Fatal("overlapping sites accepted", err)
	}
}

func TestCouplingGeometryRejectedInputUnchanged(t *testing.T) {
	t.Parallel()
	input := couplingGeometryFixture()
	input.Network.Nodes[2].Position.Y = 20
	before := input
	before.Network.Nodes = slices.Clone(input.Network.Nodes)
	before.Network.Lanes = slices.Clone(input.Network.Lanes)
	before.Sites = slices.Clone(input.Sites)
	before.Corridors = slices.Clone(input.Corridors)
	before.Corridors[0].LaneIDs = slices.Clone(input.Corridors[0].LaneIDs)
	if err := ValidateCouplingGeometry(input); err == nil {
		t.Fatal("bent path accepted")
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("rejection changed authored records")
	}
}

func TestCouplingLongPathAxisGuard(t *testing.T) {
	t.Parallel()
	input := couplingGeometryFixture()
	input.Network.Nodes = make([]Node, 11)
	input.Network.Lanes = make([]Lane, 10)
	input.Corridors[0].LaneIDs = make([]string, 10)
	for index := range input.Network.Nodes {
		input.Network.Nodes[index] = Node{ID: "n" + strconv.Itoa(index), Position: Point{X: float64(index) * 1000}}
	}
	for index := range input.Network.Lanes {
		id := "l" + strconv.Itoa(index)
		input.Network.Lanes[index] = Lane{ID: id, From: input.Network.Nodes[index].ID, To: input.Network.Nodes[index+1].ID,
			SpeedLimit: 14, VehicleClasses: classBit(string(CompactClass))}
		input.Corridors[0].LaneIDs[index] = id
	}
	input.Sites[0].LaneID, input.Sites[1].LaneID = "l0", "l9"
	if err := ValidateCouplingGeometry(input); err != nil {
		t.Fatal("straight long path rejected", err)
	}
	for index := 2; index < len(input.Network.Nodes); index++ {
		input.Network.Nodes[index].Position.Y = float64(index-1) * 2.5e-7
	}
	// Every segment's normalized heading fits the old 1e-9 cross bound.
	// The endpoints nevertheless drift more than 1e-9 m from the first axis.
	for index := 1; index < len(input.Network.Nodes); index++ {
		a, b := input.Network.Nodes[index-1].Position, input.Network.Nodes[index].Position
		if math.Abs((b.Y-a.Y)/math.Hypot(b.X-a.X, b.Y-a.Y)) > 1e-9 {
			t.Fatal("adversarial heading exceeds the old bound")
		}
	}
	if err := ValidateCouplingGeometry(input); !errors.Is(err, ErrInvalidCouplingGeometry) {
		t.Fatal("cumulative off-axis path accepted", err)
	}
}
