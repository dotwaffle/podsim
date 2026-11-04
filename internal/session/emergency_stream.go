package session

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// errEmergencyStreamOff refuses a stage 3 value in a stream frame, a delta,
// or an HTTP state with the operations off (incident emergency contract,
// section 11.1).
var errEmergencyStreamOff = errors.New("stream state with operations off contains an emergency member")

// streamEmergencyPaths are the paths of the stage 3 members of a stream
// envelope and an HTTP state. Each member below one of them is also a
// stage 3 member. The emergencies group key is a member of its own.
var streamEmergencyPaths = []string{
	"/full/state/simulation/emergencies",
	"/frame/state/simulation/emergencies",
	"/delta/groups/emergencies",
}

// emergencyViewMembers are the members of an active emergency. Each one is
// required (section 11.4).
var emergencyViewMembers = []string{"id", "podID", "orderID", "phase", "startTick"}

// scanEmergencyMembers refuses null for each stage 3 member and each member
// below it, which no encoder writes. The typed decode reads null as no
// member, so only this scan sees it.
func scanEmergencyMembers(data []byte) error {
	return scanMemberPaths(data, streamEmergencyPaths, "emergency")
}

// decodeEmergencyView decodes one active emergency of a stream frame. Each
// member of section 11.4 is present, and no other member is. The typed
// decode cannot see an absent member, because it reads it as zero. Each
// caller scans the document for null with scanMemberPaths first.
// checkEmergencyFrame checks the values.
func decodeEmergencyView(decoder *jsontext.Decoder, view *sim.EmergencyView) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var members map[string]jsontext.Value
	if err := jsonv2.Unmarshal(value, &members); err != nil {
		return err
	}
	for _, name := range emergencyViewMembers {
		if members[name] == nil || members[name].Kind() == jsontext.KindNull {
			return fmt.Errorf("emergency has no %s", name)
		}
	}
	var next sim.EmergencyView
	if err := jsonv2.Unmarshal(value, &next, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	*view = next
	return nil
}

// emergencyGroupLimits bound the emergencies replacement group of a delta,
// which the bounded scan reads alone. The path "/active" bounds its
// records. The faults group alone has the same path with another bound,
// so this table replaces it.
func emergencyGroupLimits() jsonLimits {
	limits := streamLimits(contractMarkers{})
	limits.arrays["/active"] = sim.MaxEmergencies
	return limits
}

// decodeEmergenciesGroup decodes the emergencies replacement group of a
// delta. The group has no null member.
func decodeEmergenciesGroup(raw []byte) (sim.EmergenciesView, error) {
	if err := prescanJSON(raw, emergencyGroupLimits()); err != nil {
		return sim.EmergenciesView{}, err
	}
	if err := scanMemberPaths(raw, []string{""}, "emergency"); err != nil {
		return sim.EmergenciesView{}, err
	}
	var group sim.EmergenciesView
	options := jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(decodeEmergencyView))
	if err := decodeStreamJSON(raw, &group, options); err != nil {
		return sim.EmergenciesView{}, err
	}
	return group, nil
}

// checkEmergencyFrame checks the typed stage 3 members of a frame (section
// 11.5). With the operations off, it refuses each nonzero member.
// Otherwise the records have IDs of the form i<generation>.<serial> with a
// positive serial, in strictly increasing serial order, and no serial of
// a fault of the frame. There are at most sim.MaxEmergencies records and
// at most one for each pod. Each record names a vehicle of the frame, a
// positive order, a known phase, and a start tick from 0 to the tick of
// the frame. No counter is negative.
func checkEmergencyFrame(frame SimulationFrame) error {
	emergencies := frame.Emergencies
	if frame.OperationsOff {
		if emergencies.Active != nil || emergencies.Counters != (sim.EmergencyCounters{}) {
			return errEmergencyStreamOff
		}
		return nil
	}
	counters := emergencies.Counters
	if counters.Started < 0 || counters.Ended < 0 || counters.EmergencyTicks < 0 ||
		counters.Sampled < 0 || counters.Skipped < 0 || counters.EndedTicks < 0 || counters.MaxTicks < 0 {
		return errors.New("negative emergency counter")
	}
	if len(emergencies.Active) > sim.MaxEmergencies {
		return fmt.Errorf("%d emergency records, more than %d", len(emergencies.Active), sim.MaxEmergencies)
	}
	faults := make(map[uint64]bool, len(frame.Faults.Active))
	for _, fault := range frame.Faults.Active {
		if serial, ok := faultSerial(fault.ID); ok {
			faults[serial] = true
		}
	}
	pods := make(map[string]bool, len(emergencies.Active))
	var last uint64
	for index, emergency := range emergencies.Active {
		serial, ok := faultSerial(emergency.ID)
		if !ok || serial == 0 || index > 0 && serial <= last {
			return fmt.Errorf("emergency %q has no valid ID in serial order", emergency.ID)
		}
		last = serial
		if err := checkEmergencyView(frame, emergency, faults[serial], pods); err != nil {
			return fmt.Errorf("emergency %s: %w", emergency.ID, err)
		}
	}
	return nil
}

// checkEmergencyView checks the values of one record of frame. shared
// reports that a fault of the frame has the serial of the record. pods
// holds the pods of the records before it.
func checkEmergencyView(frame SimulationFrame, emergency sim.EmergencyView, shared bool, pods map[string]bool) error {
	vehicle := func(vehicle VehicleFrame) bool { return vehicle.Pod.ID == emergency.PodID }
	switch {
	case shared:
		return errors.New("a fault has the same serial")
	case !slices.ContainsFunc(frame.Vehicles, vehicle) || pods[emergency.PodID]:
		return fmt.Errorf("pod %q is not a vehicle, or has another record", emergency.PodID)
	case emergency.OrderID <= 0:
		return fmt.Errorf("order %d is not positive", emergency.OrderID)
	case emergency.Phase != sim.EmergencyPhaseDeferred && emergency.Phase != sim.EmergencyPhaseBound && emergency.Phase != sim.EmergencyPhaseUnloading:
		return fmt.Errorf("unknown phase %q", emergency.Phase)
	case emergency.StartTick < 0 || emergency.StartTick > frame.Tick:
		return fmt.Errorf("start tick %d is outside 0 to %d", emergency.StartTick, frame.Tick)
	}
	pods[emergency.PodID] = true
	return nil
}

// cloneEmergencies returns a copy of emergencies whose records share no
// memory with emergencies.
func cloneEmergencies(emergencies sim.EmergenciesView) sim.EmergenciesView {
	emergencies.Active = slices.Clone(emergencies.Active)
	return emergencies
}
