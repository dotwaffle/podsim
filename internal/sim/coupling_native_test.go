package sim

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCouplingNativeIndependentContracts(t *testing.T) {
	t.Parallel()
	for _, order := range []OrderContract{"", ExpressOrderContract} {
		t.Run(string(order), func(t *testing.T) {
			t.Parallel()
			network := expressNetwork(largeRestoreNetwork())
			fleet := []Placement{{ID: "01", Class: CompactClass, StationID: "harbor", BerthID: "harbor-1"}}
			contracts := FleetContracts{OrderContract: order, CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true}
			s, err := NewFleetWithContracts(network, fleet, contracts)
			if err != nil {
				t.Fatal(err)
			}
			if s.OrderContract() != order || s.CouplingContract() != CompactPairV1CouplingContract || !s.CouplingEnabled() {
				t.Fatal("independent markers or policy changed")
			}
			clone := s.Clone()
			if policyErr := clone.SetCouplingEnabled(false); policyErr != nil || !s.CouplingEnabled() || clone.CouplingEnabled() {
				t.Fatal("clone policy storage is shared", policyErr)
			}
			s.Reset()
			if s.OrderContract() != order || s.CouplingContract() != CompactPairV1CouplingContract || !s.CouplingEnabled() {
				t.Fatal("reset changed immutable markers or policy")
			}
			if validationErr := ValidateFleetWithContracts(network, fleet, contracts); validationErr != nil {
				t.Fatal(validationErr)
			}
			fleet[0].Class = ExpressClass
			_, err = NewFleetWithContracts(network, fleet, contracts)
			if (err == nil) != (order == ExpressOrderContract) {
				t.Fatal("coupling marker reinterpreted Express class eligibility", err)
			}
		})
	}
}

func TestCouplingNativeHistoricalBytes(t *testing.T) {
	t.Parallel()
	for _, order := range []OrderContract{"", ExpressOrderContract} {
		t.Run(string(order), func(t *testing.T) {
			t.Parallel()
			network := expressNetwork(largeRestoreNetwork())
			fleet := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
			old, err := NewFleetWithOrderContract(network, fleet, order)
			if err != nil {
				t.Fatal(err)
			}
			current, err := NewFleetWithContracts(network, fleet, FleetContracts{OrderContract: order})
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range []*Simulation{old, current} {
				if _, submitErr := s.SubmitTrip("harbor", "market"); submitErr != nil {
					t.Fatal(submitErr)
				}
				advance(s, 12*TicksPerSecond)
			}
			a, err := json.Marshal(old.ExportState())
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(current.ExportState())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, b) || bytes.Contains(b, []byte("coupling")) {
				t.Fatal("absent coupling changed historical native bytes")
			}
			before := current.ExportState()
			if err := current.SetCouplingEnabled(true); !errors.Is(err, ErrUnknownCouplingContract) || !reflect.DeepEqual(before, current.ExportState()) {
				t.Fatal("unmarked policy admission changed state", err)
			}
		})
	}
}

func TestCouplingNativeConstructorGuards(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*Network, *[]Placement, *FleetContracts)
	}{
		{"order", func(_ *Network, _ *[]Placement, c *FleetContracts) { c.OrderContract = "unknown" }},
		{"coupling", func(_ *Network, _ *[]Placement, c *FleetContracts) { c.CouplingContract = "unknown" }},
		{"unmarked enabled", func(_ *Network, _ *[]Placement, c *FleetContracts) { c.CouplingContract = ""; c.CouplingEnabled = true }},
		{"unmarked empty registry", func(_ *Network, _ *[]Placement, c *FleetContracts) {
			c.CouplingContract = ""
			c.CouplingSites = []CouplingSite{}
		}},
		{"one sided", func(_ *Network, _ *[]Placement, c *FleetContracts) { c.CouplingSites = couplingGeometryFixture().Sites }},
		{"fleet cap", func(_ *Network, f *[]Placement, _ *FleetContracts) { *f = make([]Placement, expressMaxPods+1) }},
		{"node cap", func(n *Network, _ *[]Placement, _ *FleetContracts) { n.Nodes = make([]Node, expressMaxNodes+1) }},
		{"placement id", func(_ *Network, f *[]Placement, _ *FleetContracts) { (*f)[0].ID = strings.Repeat("x", 65) }},
		{"coordinate", func(n *Network, _ *[]Placement, _ *FleetContracts) { n.Nodes[0].Position.X = expressMaxCoordinate + 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			network := largeRestoreNetwork()
			fleet := []Placement{{ID: "01", StationID: "harbor", BerthID: "harbor-1"}}
			contracts := FleetContracts{CouplingContract: CompactPairV1CouplingContract}
			tc.change(&network, &fleet, &contracts)
			if _, err := NewFleetWithContracts(network, fleet, contracts); err == nil {
				t.Fatal("invalid contract input created state")
			}
			if err := ValidateFleetWithContracts(network, fleet, contracts); err == nil {
				t.Fatal("validator accepted invalid constructor input")
			}
		})
	}
}

func TestCouplingNativeDescriptorStorage(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, false, false)
	geometry := input.Network
	sites := []CouplingSite{geometry.sites["assembly"], geometry.sites["split"]}
	corridors := []CouplingCorridor{geometry.corridors["corridor"]}
	fleet := []Placement{{ID: "front", Class: CompactClass, StationID: "origin"}}
	s, err := NewFleetWithContracts(input.Prepared.Network(), fleet, FleetContracts{
		CouplingContract: CompactPairV1CouplingContract, CouplingSites: sites, CouplingCorridors: corridors,
	})
	if err != nil {
		t.Fatal(err)
	}
	sites[0].FrontStagingMeters = -1
	corridors[0].LaneIDs[0] = "changed"
	if s.couplingNetwork.sites["assembly"].FrontStagingMeters < 0 || s.couplingNetwork.corridors["corridor"].LaneIDs[0] != "ab" {
		t.Fatal("constructor retained mutable authored descriptors")
	}
	if s.Clone().couplingNetwork != s.couplingNetwork {
		t.Fatal("clone copied immutable prepared geometry")
	}
}

func TestCouplingNativeInactiveRestore(t *testing.T) {
	t.Parallel()
	network := largeRestoreNetwork()
	fleet := []Placement{{ID: "01", Class: CompactClass, StationID: "harbor", BerthID: "harbor-1"}}
	s, err := NewFleetWithContracts(network, fleet, FleetContracts{CouplingContract: CompactPairV1CouplingContract})
	if err != nil {
		t.Fatal(err)
	}
	input := RestoreStateInput{Network: network, Fleet: fleet, CouplingContract: CompactPairV1CouplingContract, CouplingEnabled: true, State: s.ExportState()}
	restored, result, err := RestoreState(input)
	if err != nil || result.Tier != RestorePhysical || restored.CouplingContract() != CompactPairV1CouplingContract || !restored.CouplingEnabled() {
		t.Fatal("inactive contract restore lost marker or policy", result, err)
	}
	prepared, err := PrepareNetwork(network)
	if err != nil {
		t.Fatal(err)
	}
	restored, result, err = prepared.RestoreState(PreparedRestoreInput{
		Fleet: fleet, CouplingContract: input.CouplingContract, CouplingEnabled: true, State: input.State,
	})
	if err != nil || result.Tier != RestorePhysical || restored.CouplingContract() != input.CouplingContract || !restored.CouplingEnabled() {
		t.Fatal("prepared restore lost marker or policy", result, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*RestoreStateInput)
	}{
		{"missing input marker", func(i *RestoreStateInput) { i.CouplingContract = "" }},
		{"unknown input marker", func(i *RestoreStateInput) { i.CouplingContract = "unknown" }},
		{"unknown saved marker", func(i *RestoreStateInput) { i.State.CouplingContract = "unknown" }},
		{"missing saved marker", func(i *RestoreStateInput) { i.State.CouplingContract = "" }},
		{"group count", func(i *RestoreStateInput) { i.State.CouplingGroups = []SavedCouplingGroup{{ID: "group"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bad := input
			tc.change(&bad)
			if _, _, err := RestoreState(bad); err == nil {
				t.Fatal("malformed native coupling state accepted")
			}
		})
	}
}
