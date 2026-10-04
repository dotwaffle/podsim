package sim

import (
	"errors"
	"maps"
	"slices"
	"testing"
)

// Both paths use independently validated stopped ordinary staging footprints.
// This fixture checks inactive adoption and actual native facts, not Run reachability.
func nativeForeignPairFixture(t *testing.T) (*Simulation, [2]*couplingMotionContext, [2]couplingNativeForeignPair) {
	t.Helper()
	inputs := [2]couplingReservationInput{couplingMotionFixture(t, false, false), couplingMotionFixture(t, true, false)}
	geometry := CouplingGeometryInput{Contract: CompactPairV1CouplingContract}
	for i := range inputs {
		nativeForeignRenamePairInput(&inputs[i], string(rune('a'+i))+"-", float64(i)*1000, &geometry)
	}
	p, err := PrepareNetwork(geometry.Network)
	if err != nil {
		t.Fatal(err)
	}
	geometry.Network = p.Network()
	n, err := prepareCouplingReservations(p, geometry)
	if err != nil {
		t.Fatal(err)
	}
	owners := make(map[resource]resourceOwner)
	var contexts [2]*couplingMotionContext
	var pairs [2]couplingNativeForeignPair
	var placements []Placement
	for i := range inputs {
		inputs[i].Network, inputs[i].Prepared = n, p
		for r, owner := range inputs[i].Owners {
			if !owners[r].isZero() {
				t.Fatal("independent pair footprints share an owner key")
			}
			owners[r] = owner
		}
		for member := range inputs[i].Members {
			m := &inputs[i].Members[member]
			for lane := range m.Vehicle.Route {
				m.Vehicle.Route[lane] = n.lanes[m.Vehicle.Route[lane].ID].lane
			}
			placements = append(placements, Placement{ID: m.Vehicle.Pod.ID, Class: CompactClass, StationID: m.DestinationStation, BerthID: m.Destination.ID})
		}
	}
	for i := range inputs {
		inputs[i].Owners = maps.Clone(owners)
		reservation, reservationErr := planCouplingReservation(inputs[i])
		if reservationErr != nil {
			t.Fatal(reservationErr)
		}
		other := inputs[1-i].Members
		contexts[i], err = prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: inputs[i], GroupID: string(rune('a'+i)) + "-pair", ForeignIDs: []string{other[0].Vehicle.Pod.ID, other[1].Vehicle.Pod.ID}})
		if err != nil {
			t.Fatal(err)
		}
		initial, initialErr := initialCouplingMotion(contexts[i], inputs[i])
		if initialErr != nil {
			t.Fatal(initialErr)
		}
		applyCouplingTestWrites(t, owners, initial.Writes)
		next, nextErr := contexts[i].stateAt(1)
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		pairs[i] = couplingNativeForeignPair{context: contexts[i], previous: initial.State, next: next}
	}
	s, err := p.NewFleet(placements)
	if err != nil {
		t.Fatal(err)
	}
	s.owners, s.tick = owners, pairs[0].next.Tick
	for group, c := range contexts {
		members := c.membersAt(pairs[group].previous)
		for member, m := range &inputs[group].Members {
			v := s.findVehicle(m.Vehicle.Pod.ID)
			v.Vehicle = m.Vehicle
			s.setVehicleRoute(v, m.Vehicle.Route)
			v.Pod = members[member].Pod
			v.RiddenMeters = -1
			v.distance, v.blockIndex, v.reservedThrough = pairs[group].previous.Distances[member], pairs[group].previous.Cells[member], c.through[member]
			v.origin, v.destination, v.destinationStation = m.Origin, m.Destination, m.DestinationStation
			if group == 1 {
				v.riddenBase = 256
			}
			v.routeReleases = make(map[resource]float64)
			for _, block := range v.blocks.span(0, v.reservedThrough+1) {
				for _, r := range block.resources {
					v.routeReleases[r] = max(v.routeReleases[r], resourceReleaseDistance(block, r))
				}
			}
		}
	}
	return s, contexts, pairs
}

func nativeForeignRenamePairInput(input *couplingReservationInput, prefix string, y float64, geometry *CouplingGeometryInput) {
	rename := func(id string) string {
		if id == "" {
			return ""
		}
		return prefix + id
	}
	berth := func(b Berth) Berth { b.ID, b.Node = rename(b.ID), rename(b.Node); return b }
	network := input.Prepared.Network()
	for _, node := range network.Nodes {
		node.ID, node.Position.Y = rename(node.ID), node.Position.Y+y
		geometry.Network.Nodes = append(geometry.Network.Nodes, node)
	}
	for _, lane := range network.Lanes {
		lane.ID, lane.From, lane.To, lane.StationID = rename(lane.ID), rename(lane.From), rename(lane.To), rename(lane.StationID)
		geometry.Network.Lanes = append(geometry.Network.Lanes, lane)
	}
	for _, station := range network.Stations {
		station.ID, station.Entry, station.Exit = rename(station.ID), rename(station.Entry), rename(station.Exit)
		station.Berths = slices.Clone(station.Berths)
		for i := range station.Berths {
			station.Berths[i] = berth(station.Berths[i])
		}
		geometry.Network.Stations = append(geometry.Network.Stations, station)
	}
	corridor := input.Network.corridors[input.CorridorID]
	for _, id := range []string{corridor.AssemblySiteID, corridor.SplitSiteID} {
		site := input.Network.sites[id]
		site.ID, site.LaneID = rename(site.ID), rename(site.LaneID)
		geometry.Sites = append(geometry.Sites, site)
	}
	corridor.ID, corridor.AssemblySiteID, corridor.SplitSiteID = rename(corridor.ID), rename(corridor.AssemblySiteID), rename(corridor.SplitSiteID)
	corridor.LaneIDs = slices.Clone(corridor.LaneIDs)
	for i := range corridor.LaneIDs {
		corridor.LaneIDs[i] = rename(corridor.LaneIDs[i])
	}
	geometry.Corridors = append(geometry.Corridors, corridor)
	input.CorridorID = corridor.ID
	owners := make(map[resource]resourceOwner)
	for r, owner := range input.Owners {
		r.id, owner.id = rename(r.id), rename(owner.id)
		owners[r] = owner
	}
	input.Owners = owners
	for i := range input.Members {
		m := &input.Members[i]
		m.RouteVersion = 1
		m.Vehicle.Pod.ID, m.Vehicle.Pod.LaneID = rename(m.Vehicle.Pod.ID), rename(m.Vehicle.Pod.LaneID)
		m.Vehicle.Pod.Position.Y += y
		m.Vehicle.RelocatingTo, m.DestinationStation = rename(m.Vehicle.RelocatingTo), rename(m.DestinationStation)
		m.Origin, m.Destination = berth(m.Origin), berth(m.Destination)
		for j := range m.Vehicle.Route {
			m.Vehicle.Route[j].ID = rename(m.Vehicle.Route[j].ID)
		}
		for j := range m.Vehicle.Riders {
			r := &m.Vehicle.Riders[j]
			r.ID += int(prefix[0]-'a') * 10
			r.SharingConsent = SharedConsent
			r.From, r.To, r.PodID = rename(r.From), rename(r.To), rename(r.PodID)
		}
		for j := range m.Vehicle.Stops {
			m.Vehicle.Stops[j] = rename(m.Vehicle.Stops[j])
		}
		if len(m.Vehicle.Riders) > 0 {
			m.Vehicle.RiddenMeters = 256 + m.Distance
			m.Vehicle.Boardings = []RiderBoarding{{BerthID: m.Origin.ID, MetersAtBoarding: 128}}
		}
		retained := make(map[resource]float64)
		for r, release := range m.Retained {
			r.id = rename(r.id)
			retained[r] = release
		}
		m.Retained = retained
	}
}

func TestNativeForeignOtherPairs(t *testing.T) {
	t.Parallel()
	s, contexts, pairs := nativeForeignPairFixture(t)
	fleet, err := prepareNativeForeignFleet(s, contexts[0], contexts[:]...)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := buildNativeForeignTick(s, fleet, pairs[:]...)
	if err != nil {
		t.Fatal(err)
	}
	view, err := sealCouplingMotionOwners(contexts[0], pairs[0].previous, s.owners)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = planCouplingMotion(couplingMotionInput{Context: contexts[0], Previous: pairs[0].previous, Owners: view, Foreign: frame.sweeps()}); err != nil {
		t.Fatal("actual other pair certificate denied disjoint motion", err)
	}
	for _, sweep := range frame.sweeps() {
		if sweep.native.pair.proof == nil || sweep.Path.class != CompactClass {
			t.Fatal("actual other pair became an ordinary owner exemption")
		}
	}
	for _, test := range []struct {
		name   string
		change func(*Simulation, *[2]couplingNativeForeignPair)
	}{
		{"next_state", func(_ *Simulation, p *[2]couplingNativeForeignPair) { p[1].next.Positions[0].X++ }},
		{"cabin_consent", func(s *Simulation, _ *[2]couplingNativeForeignPair) {
			s.findVehicle("b-front").Riders[0].SharingConsent = PrivateConsent
		}},
		{"pose", func(s *Simulation, _ *[2]couplingNativeForeignPair) { s.findVehicle("b-front").Pod.Position.X++ }},
		{"group_owner", func(s *Simulation, _ *[2]couplingNativeForeignPair) {
			for r, owner := range s.owners {
				if owner == contexts[1].owner {
					s.owners[r] = podResourceOwner(owner.id)
					return
				}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			bad := s.Clone()
			copyPairs := pairs
			test.change(bad, &copyPairs)
			f, preparationErr := prepareNativeForeignFleet(bad, contexts[0], contexts[:]...)
			if preparationErr != nil {
				t.Fatal("fixture was denied before its intended frame guard", preparationErr)
			}
			if _, frameErr := buildNativeForeignTick(bad, f, copyPairs[:]...); !errors.Is(frameErr, errCouplingMotionInvariant) {
				t.Fatal("changed actual pair binding was accepted")
			}
		})
	}
	if _, err = buildNativeForeignTick(s, fleet, pairs[0]); !errors.Is(err, errCouplingMotionInvariant) {
		t.Fatal("complete registry allowed an omitted other pair")
	}
}

func TestNativeForeignPairCanonicalBlocks(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"a-front", "b-front"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			s, contexts, _ := nativeForeignPairFixture(t)
			v := s.findVehicle(id)
			v.blocks.lanes = slices.Clone(v.blocks.lanes)
			v.blocks.lanes[0].start++
			if _, err := prepareNativeForeignFleet(s, contexts[0], contexts[:]...); !errors.Is(err, errCouplingMotionInvariant) {
				t.Fatal("actual pair with corrupt canonical block layout was accepted")
			}
		})
	}
}

func TestNativeForeignActualApproachMetadata(t *testing.T) {
	t.Parallel()
	s, contexts, _ := nativeForeignPairFixture(t)
	c := contexts[0]
	if c.ticks > 30000 {
		t.Fatal("approach fixture exceeds its frozen oracle cap")
	}
	for elapsed := uint64(1); elapsed < c.ticks; elapsed++ {
		state, err := c.stateAt(elapsed)
		if err != nil {
			t.Fatal(err)
		}
		if state.Phase != couplingDraining || state.Lanes[0] != "a-front-road" {
			continue
		}
		member := c.membersAt(state)[0]
		v := s.findVehicle("a-front")
		v.Pod, v.distance, v.blockIndex = member.Pod, state.Distances[0], state.Cells[0]
		s.updateStationPhase(v)
		if v.Pod.StationPhase != ApproachingStation || v.Pod.ManeuverStationID != "a-front-goal" {
			t.Fatal("actual native last-drain controller did not publish approach metadata")
		}
		if !nativeForeignCabinEqual(nativeForeignCabin(v), member) {
			t.Fatal("actual approach metadata changed the bound native cabin or physical facts")
		}
		return
	}
	t.Fatal("approach fixture did not reach its actual last-drain route lane")
}
