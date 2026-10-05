package session

import (
	"encoding/json"
	"errors"
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

type couplingReplacement struct {
	Contract sim.CouplingContract    `json:"couplingContract"`
	Enabled  bool                    `json:"couplingEnabled"`
	Groups   []sim.CouplingGroupView `json:"couplingGroups"`
}

func applyCouplingReplacement(frame *SimulationFrame, raw json.RawMessage) error {
	if frame.CouplingContract != sim.CompactPairV1CouplingContract {
		return errors.New("unmarked state contains a coupling replacement")
	}
	if err := scanCouplingReplacement(raw); err != nil {
		return err
	}
	var replacement couplingReplacement
	if err := decodeStreamJSON(raw, &replacement); err != nil {
		return err
	}
	if replacement.Contract != frame.CouplingContract {
		return errors.New("coupling replacement changes the contract")
	}
	if len(replacement.Groups) == 0 {
		replacement.Groups = nil
	}
	frame.CouplingEnabled, frame.CouplingGroups = replacement.Enabled, replacement.Groups
	return nil
}

func newCouplingFrameValidator(topology TopologySnapshot) (*sim.CouplingViewValidator, error) {
	return sim.NewCouplingViewValidator(sim.CouplingGeometryInput{Contract: topology.CouplingContract,
		Network: topology.Network, Sites: topology.CouplingSites, Corridors: topology.CouplingCorridors})
}

func couplingFrameBinding(topology TopologySnapshot, frame SimulationFrame) error {
	if topology.CouplingContract != frame.CouplingContract {
		return errors.New("topology coupling contract does not match state")
	}
	if topology.CouplingContract != "" {
		return nil
	}
	if hasCouplingTopology(topology) || frame.CouplingEnabled || frame.CouplingGroups != nil {
		return errors.New("unmarked topology or state contains coupling fields")
	}
	for _, cabin := range frame.Vehicles {
		if cabin.CouplingID != "" {
			return errors.New("unmarked cabin contains mechanical membership")
		}
	}
	return nil
}

func cloneCouplingGroups(groups []sim.CouplingGroupView) []sim.CouplingGroupView {
	groups = slices.Clone(groups)
	for i := range groups {
		group := &groups[i]
		if group.CommonSpeed != nil {
			group.CommonSpeed = new(*group.CommonSpeed)
		}
		if group.Connector != nil {
			group.Connector = new(*group.Connector)
		}
		if group.ManeuverEnvelope != nil {
			group.ManeuverEnvelope = new(*group.ManeuverEnvelope)
		}
	}
	return groups
}

func (a *StreamAssembler) couplingView(candidate State) error {
	state := candidate.Simulation
	if a.coupling == nil {
		return nil // frameState rejects unmarked train fields.
	}
	if err := a.coupling.Validate(state); err != nil {
		return err
	}
	if a.previous.State.Epoch == "" || a.previous.State.Generation != candidate.Generation {
		return nil
	}
	previous := make(map[string]sim.SavedCouplingGroup, len(a.state.Simulation.CouplingGroups))
	for _, group := range a.state.Simulation.CouplingGroups {
		previous[group.ID] = group.SavedCouplingGroup
	}
	for _, group := range state.CouplingGroups {
		if old, exists := previous[group.ID]; exists && (old.Members != group.Members || old.FormationTick != group.FormationTick ||
			old.CorridorID != group.CorridorID || old.AssemblySiteID != group.AssemblySiteID || old.SplitSiteID != group.SplitSiteID ||
			old.Progress.DrainFirstMember != group.Progress.DrainFirstMember) {
			return errors.New("committed coupling identity changed within the stream")
		}
	}
	return couplingRetirement(a.state.Simulation, state)
}

func couplingRetirement(previous, current sim.Snapshot) error {
	groups := make(map[string]sim.CouplingGroupView, len(current.CouplingGroups))
	pods := make(map[string]sim.Vehicle, len(current.Vehicles))
	for _, group := range current.CouplingGroups {
		groups[group.ID] = group
	}
	for _, cabin := range current.Vehicles {
		pods[cabin.Pod.ID] = cabin
	}
	for _, group := range previous.CouplingGroups {
		if _, remains := groups[group.ID]; remains {
			continue
		}
		front, frontExists := pods[group.Members[0]]
		rear, rearExists := pods[group.Members[1]]
		if !frontExists || !rearExists {
			return errors.New("retired coupling group lost a cabin")
		}
		if next, formed := groups[front.CouplingID]; formed && rear.CouplingID == next.ID && next.FormationTick > previous.Tick {
			continue // A later sample can include a complete new formation.
		}
		distance := math.Hypot(front.Pod.Position.X-rear.Pod.Position.X, front.Pod.Position.Y-rear.Pod.Position.Y)
		if !(distance >= sim.Clearance-1e-9) {
			return errors.New("retired coupling cabins lack ordinary separation")
		}
	}
	return nil
}
