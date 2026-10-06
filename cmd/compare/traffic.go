package main

import "github.com/dotwaffle/podsim/internal/sim"

// stoppedSpeed is the speed in m/s below which a pod with a wait reason
// counts as stopped. Dispatch uses the same limit.
const stoppedSpeed = 0.1

// trafficWaits holds the pod-seconds that pods spent stopped.
type trafficWaits struct {
	// stopped counts each stopped pod, for any wait reason.
	stopped int
	// junction and track count the stopped pods that wait for junction
	// traffic and for a pod ahead. The other stopped pods wait for a
	// berth or for parking.
	junction, track int
}

// sampleWaits adds the stopped pods of a snapshot, as one second each.
func (waits *trafficWaits) sampleWaits(vehicles []sim.Vehicle) {
	for _, vehicle := range vehicles {
		pod := vehicle.Pod
		if pod.WaitReason == sim.NoWait || pod.Speed >= stoppedSpeed {
			continue
		}
		waits.stopped++
		switch pod.WaitReason {
		case sim.JunctionOccupied:
			waits.junction++
		case sim.TrackOccupied:
			waits.track++
		case sim.NoWait, sim.BerthOccupied, sim.ParkingUnavailable,
			sim.FaultBraking, sim.FaultStopped, sim.BlockedByIncident, sim.NoForwardRoute:
		}
	}
}

// couplingTime holds the pod-seconds that pods spent traveling, and the
// part of that time in a platoon.
type couplingTime struct {
	traveling, coupled int
}

// sample adds the traveling pods of a snapshot and the coupled pods, as one
// second each. coupled is the value of CoupledPods at the snapshot.
func (coupling *couplingTime) sample(vehicles []sim.Vehicle, coupled int) {
	for _, vehicle := range vehicles {
		if vehicle.Pod.Activity == sim.Traveling {
			coupling.traveling++
		}
	}
	coupling.coupled += coupled
}

// percent returns the coupled time as a percentage of the traveling time,
// or 0 when no pod traveled.
func (coupling *couplingTime) percent() float64 {
	if coupling.traveling == 0 {
		return 0
	}
	return 100 * float64(coupling.coupled) / float64(coupling.traveling)
}

// nodeFlowWindowTicks is the length of the node flow window, 60 s.
const nodeFlowWindowTicks = 60 * sim.TicksPerSecond

// nodeFlow finds the node that the most pods pass in 60 s.
type nodeFlow struct {
	// passes holds, for each node, the ticks of its passes in the window
	// that ends at the last pass, in time order.
	passes   map[string][]int64
	peak     int
	peakNode string
}

// peakNodeFlow gives the most passes of one node in 60 s, and that node.
// The passes must be in tick order, as NodePasses gives them.
func peakNodeFlow(passes []sim.NodePass) (int, string) {
	flow := nodeFlow{passes: make(map[string][]int64)}
	for _, pass := range passes {
		flow.pass(pass.Node, pass.Tick)
	}
	return flow.peak, flow.peakNode
}

// pass records a pass of node at tick. The window of a pass holds the
// passes of the node in the 60 s that end at the pass. A tie goes to the
// lower node ID.
func (flow *nodeFlow) pass(node string, tick int64) {
	passes := flow.passes[node]
	passes = append(passes, tick)
	first := 0
	for passes[first] <= tick-nodeFlowWindowTicks {
		first++
	}
	passes = passes[first:]
	flow.passes[node] = passes
	if count := len(passes); count > flow.peak || (count == flow.peak && node < flow.peakNode) {
		flow.peak, flow.peakNode = count, node
	}
}
