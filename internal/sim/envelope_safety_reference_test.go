package sim

import (
	"slices"
	"testing"
)

// referenceVehicleSafetyLocations is the construction of
// vehicleSafetyLocations before the berth and node indexes. It scans every
// station berth and every network lane.
func referenceVehicleSafetyLocations(s *Simulation, v *vehicle, distance float64) []SafetyLocation {
	var locations []SafetyLocation
	add := func(location SafetyLocation) {
		if !slices.Contains(locations, location) {
			locations = append(locations, location)
		}
	}
	if v.Pod.LaneID != "" {
		add(s.laneSafety[v.Pod.LaneID])
	}
	if v.Pod.BerthID != "" {
		add(s.berthSafety[v.Pod.BerthID])
	}
	if v.Pod.Activity == Traveling {
		front := largeEnvelopeRadius
		for _, entry := range v.blocks.lanes {
			if entry.cells != nil {
				front = max(front, entry.cells.front)
			}
		}
		for index, lane := range v.Route {
			start, end := v.blocks.lanes[index].start, v.blocks.lanes[index+1].start
			if start > distance+front {
				break
			}
			tail := largeClearance
			if cells := v.blocks.lanes[index].cells; cells != nil {
				tail = max(tail, cells.tail)
			}
			if end >= distance-tail {
				add(s.laneSafety[lane.ID])
			}
		}
		if v.originReleased {
			return locations
		}
		if v.origin.ID != "" {
			add(s.berthSafety[v.origin.ID])
		}
	}
	node := v.origin.Node
	if v.Pod.BerthID != "" {
		for _, station := range s.network.Stations {
			for _, berth := range station.Berths {
				if berth.ID == v.Pod.BerthID {
					node = berth.Node
				}
			}
		}
	}
	for _, lane := range s.network.Lanes {
		if lane.From == node || lane.To == node {
			add(s.laneSafety[lane.ID])
		}
	}
	return locations
}

// checkSafetyLocationParity requires the indexed envelope locations of
// each pod to equal the reference construction, in the same order. It also
// requires each per-pod entry of the observation to belong to the pod at
// its index.
func checkSafetyLocationParity(t *testing.T, s *Simulation) {
	t.Helper()
	o := s.SafetyObservation()
	if len(o.Locations) != len(s.vehicles) || len(o.bound) != len(s.vehicles) {
		t.Fatalf("tick %d: %d locations and %d bound pods for %d pods", s.tick, len(o.Locations), len(o.bound), len(s.vehicles))
	}
	for index := range s.vehicles {
		v := &s.vehicles[index]
		got, want := s.appendVehicleSafetyLocations(nil, v, v.distance), referenceVehicleSafetyLocations(s, v, v.distance)
		if !slices.Equal(got, want) {
			t.Fatalf("tick %d pod %s safety locations %v, reference %v", s.tick, v.Pod.ID, got, want)
		}
		if o.Pods[index] != v.Pod || o.bound[index] != v.Pod {
			t.Fatalf("tick %d pod %s: the observation has %+v bound to %+v", s.tick, v.Pod.ID, o.Pods[index], o.bound[index])
		}
		var location SafetyLocation
		if v.Pod.LaneID != "" {
			location = s.laneSafety[v.Pod.LaneID]
		} else if v.Pod.BerthID != "" {
			location = s.berthSafety[v.Pod.BerthID]
		}
		if o.Locations[index] != location {
			t.Fatalf("tick %d pod %s: location %+v, want %+v", s.tick, v.Pod.ID, o.Locations[index], location)
		}
		if &o.Pods[index] == &o.bound[index] {
			t.Fatalf("tick %d: the pods and the bound pods share storage", s.tick)
		}
		if len(o.envelopes) > 0 {
			if envelope := o.envelopes[index]; !slices.Equal(envelope, want) {
				t.Fatalf("tick %d pod %s: observation envelope %v, reference %v", s.tick, v.Pod.ID, envelope, want)
			} else if cap(envelope) != len(envelope) {
				t.Fatalf("tick %d pod %s: the envelope has spare capacity in the shared array", s.tick, v.Pod.ID)
			}
		}
		if len(o.keys) > 0 {
			if key, _ := s.separationKey(v); o.keys[index] != key {
				t.Fatalf("tick %d pod %s: key %+v, want %+v", s.tick, v.Pod.ID, o.keys[index], key)
			}
		}
		if len(o.chains) > 0 {
			var chain safetyChain
			if !largeVehicleClass(v.Pod.Class) {
				node, speed := "", v.Pod.Speed
				if v.Pod.LaneID == "" {
					node, speed = s.berthSafety[v.Pod.BerthID].From, 0
				}
				if positions := s.retention.chainPositions(v.Pod, node); len(positions) > 0 {
					chain = safetyChain{positions: positions, speed: speed}
				}
			}
			if got := o.chains[index]; got.speed != chain.speed || !slices.Equal(got.positions, chain.positions) {
				t.Fatalf("tick %d pod %s: chain %+v, want %+v", s.tick, v.Pod.ID, got, chain)
			}
		}
	}
}

// TestBerthEnvelopeMatchesReference compares vehicleSafetyLocations with
// the reference on the pods that the berth envelopes can serve wrongly.
// Two lanes into the berth node have one location. A traveling pod that is
// still at its berth, and a stopped pod that has a lane, do not take the
// berth envelope.
func TestBerthEnvelopeMatchesReference(t *testing.T) {
	t.Parallel()
	n := Network{Nodes: []Node{{ID: "a"}, {ID: "b", Position: Point{X: 200}}, {ID: "c", Position: Point{X: 100}}},
		Lanes: []Lane{
			{ID: "in-1", From: "a", To: "c", SeparationGroup: "g"},
			{ID: "in-2", From: "a", To: "c", SeparationGroup: "g"},
			{ID: "out", From: "c", To: "b", SeparationGroup: "h"},
		},
		Stations: []Station{{ID: "s", Berths: []Berth{{ID: "x", Node: "c", SeparationGroup: "g"}, {ID: "y", Node: "b", SeparationGroup: "h"}}}}}
	indexes := &networkIndexes{network: n}
	indexes.indexSafety()
	s := &Simulation{networkIndexes: indexes}
	parked := vehicle{
		Pod: Pod{ID: "parked", Class: GroupClass, Activity: Idle, BerthID: "x"}}
	leaving := vehicle{origin: n.Stations[0].Berths[0], blocks: blockList{lanes: []routeLaneCells{{start: 0}, {start: 100}}}}
	leaving.Pod, leaving.Route = Pod{ID: "leaving", Class: GroupClass, Activity: Traveling, BerthID: "x"}, n.Lanes[2:]
	stopped := vehicle{
		Pod: Pod{ID: "stopped", Class: GroupClass, Activity: Idle, LaneID: "out", BerthID: "y"}}
	for _, v := range []*vehicle{&parked, &leaving, &stopped} {
		got, want := s.appendVehicleSafetyLocations(nil, v, 0), referenceVehicleSafetyLocations(s, v, 0)
		if !slices.Equal(got, want) {
			t.Errorf("pod %s: safety locations %v, reference %v", v.Pod.ID, got, want)
		}
	}
	if got := s.appendVehicleSafetyLocations(nil, &parked, 0); len(got) != 3 {
		t.Errorf("parked pod: %d locations, want 3: %v", len(got), got)
	}
}
