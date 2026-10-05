package session

import (
	legacyJSON "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const couplingStateVersion = 8

// preservedStateError stops recovery before an archive or startup write.
type preservedStateError struct{ err error }

func (e *preservedStateError) Error() string { return e.err.Error() }
func (e *preservedStateError) Unwrap() error { return e.err }

func preserveStateError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := errors.AsType[*preservedStateError](err); ok {
		return err
	}
	return &preservedStateError{err: err}
}

func protectCouplingDecodeError(raw []byte, err error) error {
	if _, ok := errors.AsType[*preservedStateError](err); ok {
		return err
	}
	recognized, opaque := couplingPresence(raw)
	if recognized || opaque {
		return preserveStateError(err)
	}
	return err
}

func decodeCouplingState(raw []byte) (file stateFile, err error) {
	protected := true
	defer func() {
		if protected && err != nil {
			err = preserveStateError(err)
		}
	}()
	scan, err := scanCouplingJSON(raw, false)
	if err != nil {
		return file, invalidState(err)
	}
	if err := prescanJSON(raw, couplingSavedLimits(scan.packed)); err != nil {
		return file, invalidState(fmt.Errorf("scan session state: %w", err))
	}
	if err := scanContractMarkers(raw, scan.packed, scan.packed); err != nil {
		return file, invalidState(err)
	}
	if scan.packed {
		if err := scanPackedOrders(raw); err != nil {
			return file, invalidState(err)
		}
	}
	if err := scanStateCompactFields(raw); err != nil {
		return file, invalidState(err)
	}
	if err := scanStateOrderFieldsContract(raw, couplingStateVersion, scan.packed); err != nil {
		return file, invalidState(err)
	}
	var tuples [][]boardingTuple
	decodePod := func(d *jsontext.Decoder, pod *sim.SavedPod) error {
		var records []boardingTuple
		var err error
		if scan.packed {
			records, err = decodeExpressBoardingPod(d, pod)
		} else {
			records, err = decodeBoardingPod(d, pod)
		}
		if err == nil {
			tuples = append(tuples, records)
		}
		return err
	}
	unmarshalers := json.JoinUnmarshalers(json.UnmarshalFromFunc(decodeSavedProject), json.UnmarshalFromFunc(decodeV6Platoon),
		json.UnmarshalFromFunc(decodeCompactQueue), json.UnmarshalFromFunc(decodePod))
	if scan.packed {
		unmarshalers = json.JoinUnmarshalers(unmarshalers, json.UnmarshalFromFunc(decodePackedSavedRequest))
	}
	if err := json.Unmarshal(raw, &file, json.RejectUnknownMembers(true), json.WithUnmarshalers(unmarshalers)); err != nil {
		return stateFile{}, invalidState(fmt.Errorf("decode session state: %w", err))
	}
	if slices.ContainsFunc(tuples, func(records []boardingTuple) bool { return len(records) != 0 }) {
		file.boardingTuples = tuples
	}
	if err := validateSavedCompactMembers(file.Simulation); err != nil {
		return file, invalidState(err)
	}
	if err := file.validateProjectVersion(); err != nil {
		return file, invalidState(err)
	}
	if err := validateCouplingRoutes(file); err != nil {
		return file, invalidState(err)
	}
	protected = scan.groups
	file.protected = protected
	return file, nil
}

// couplingSavedLimits bound a version 8 state file. packed reports the
// Express marker.
func couplingSavedLimits(packed bool) jsonLimits {
	markers := contractMarkers{coupling: sim.CompactPairV1CouplingContract}
	if packed {
		markers.order = sim.ExpressOrderContract
	}
	return savedLimits(markers)
}

func (file *stateFile) packedOrders() bool {
	return file.Version == expressStateVersion || file.Version == couplingStateVersion && file.OrderContract == sim.ExpressOrderContract
}

func (file *stateFile) validateCouplingContract() error {
	if file.Version != couplingStateVersion {
		if file.CouplingContract != "" || file.Simulation.CouplingContract != "" || file.Simulation.CouplingGroups != nil || file.Project.CouplingContract != "" {
			return errors.New("legacy saved version contains coupling fields")
		}
		return nil
	}
	if file.CouplingContract != sim.CompactPairV1CouplingContract ||
		file.Project.CouplingContract != file.CouplingContract || file.Simulation.CouplingContract != file.CouplingContract {
		return errors.New("saved coupling contract markers disagree")
	}
	return nil
}

func fleetContracts(config project.Config) sim.FleetContracts {
	return sim.FleetContracts{
		OrderContract: config.OrderContract, CouplingContract: config.CouplingContract,
		CouplingEnabled: config.CouplingEnabled, CouplingSites: config.CouplingSites,
		CouplingCorridors: config.CouplingCorridors,
	}
}

func cloneCouplingCorridors(corridors []sim.CouplingCorridor) []sim.CouplingCorridor {
	owned := slices.Clone(corridors)
	for i := range owned {
		owned[i].LaneIDs = slices.Clone(owned[i].LaneIDs)
	}
	return owned
}

func hasCouplingTopology(topology TopologySnapshot) bool {
	return topology.CouplingContract != "" || topology.CouplingEnabled ||
		topology.CouplingSites != nil || topology.CouplingCorridors != nil
}

func decodeCouplingTopology(data []byte) (TopologySnapshot, error) {
	scan, err := scanCouplingJSON(data, true)
	if err != nil {
		return TopologySnapshot{}, err
	}
	if err := scanContractMarkers(data, scan.packed, false); err != nil {
		return TopologySnapshot{}, err
	}
	version := FoundationStreamVersion
	if scan.packed {
		version = ExpressStreamVersion
	}
	if err := scanStreamServiceMembersContract(data, version, true); err != nil {
		return TopologySnapshot{}, err
	}
	type plainTopology TopologySnapshot
	var decoded plainTopology
	if err := json.Unmarshal(data, &decoded, legacyJSON.DefaultOptionsV1(), json.MatchCaseInsensitiveNames(false), json.RejectUnknownMembers(true)); err != nil {
		return TopologySnapshot{}, err
	}
	if decoded.CouplingContract != sim.CompactPairV1CouplingContract {
		return TopologySnapshot{}, sim.ErrUnknownCouplingContract
	}
	if err := decoded.Network.ValidateStationBanks(); err != nil {
		return TopologySnapshot{}, err
	}
	if decoded.OrderContract == "" && len(decoded.ExpressServices) != 0 {
		return TopologySnapshot{}, errors.New("foundation coupling topology contains Express services")
	}
	if err := sim.ValidateExpressServicesWithOrderContract(decoded.Network, decoded.ExpressServices, decoded.OrderContract); err != nil {
		return TopologySnapshot{}, err
	}
	if err := sim.ValidateCouplingGeometry(sim.CouplingGeometryInput{
		Contract: decoded.CouplingContract, Network: decoded.Network, Sites: decoded.CouplingSites, Corridors: decoded.CouplingCorridors,
	}); err != nil {
		return TopologySnapshot{}, err
	}
	return TopologySnapshot(decoded), nil
}

// validateCouplingRoutes rejects incomplete committed continuations before encoding.
// Native restore remains responsible for positions, footprints, and route work.
func validateCouplingRoutes(file stateFile) error {
	if len(file.Simulation.CouplingGroups) == 0 {
		return nil
	}
	if len(file.Simulation.CouplingGroups) > len(file.Simulation.Pods)/2 || len(file.Simulation.CouplingGroups) > project.MaxPods/2 {
		return errors.New("saved coupling groups exceed the pair bound")
	}
	network := file.Project.Network
	berths := make(map[string]string)
	for _, station := range network.Stations {
		for _, berth := range station.Berths {
			berths[berth.ID] = berth.Node
		}
	}
	pods := make(map[string]sim.SavedPod, len(file.Simulation.Pods))
	for _, pod := range file.Simulation.Pods {
		pods[pod.ID] = pod
	}
	members := make(map[string]bool, 2*len(file.Simulation.CouplingGroups))
	for _, group := range file.Simulation.CouplingGroups {
		for _, id := range group.Members {
			pod, found := pods[id]
			if !found || members[id] || pod.Class != sim.CompactClass || pod.Activity != "traveling" ||
				pod.Platoon != nil || pod.CompactQueue != nil || len(pod.Route) == 0 || len(pod.Route) > len(network.Lanes)+len(network.Nodes) ||
				pod.RouteIndex < 0 || pod.RouteIndex >= len(pod.Route) {
				return fmt.Errorf("coupling member %q has no complete continuation", id)
			}
			members[id] = true
			previous := berths[pod.Origin]
			if previous == "" || berths[pod.Destination] == "" {
				return errors.New("coupling route has no known endpoint berths")
			}
			for _, index := range pod.Route {
				if index < 0 || index >= len(network.Lanes) || network.Lanes[index].From != previous {
					return errors.New("coupling route omits its origin or a continuation lane")
				}
				previous = network.Lanes[index].To
			}
			if previous != berths[pod.Destination] || network.Lanes[pod.Route[pod.RouteIndex]].ID != pod.LaneID ||
				!finiteCompactNumber(pod.Distance) || pod.Distance < 0 || !finiteCompactNumber(pod.LaneDistance) || pod.LaneDistance < 0 {
				return errors.New("coupling route omits its destination or current lane")
			}
		}
	}
	return nil
}

func validateCouplingRestoreResult(result sim.RestoreResult) error {
	if result.Tier != sim.RestorePhysical || result.PhysicalError != nil || len(result.Demoted) != 0 || len(result.Requeued) != 0 ||
		len(result.Dropped) != 0 || len(result.LogicalCompleted) != 0 || result.DroppedParties != 0 || result.Unaccounted != 0 || result.OverCap != 0 || result.OverBudget != 0 {
		return errors.New("committed coupling state requires lossless physical restore")
	}
	return nil
}
