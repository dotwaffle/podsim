package session

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// errFaultStreamUnmarked refuses a stage 2 member in a stream frame, a
// delta, or an HTTP state without the fault marker (incident suspension
// contract, section 13.1).
var errFaultStreamUnmarked = errors.New("stream state without the fault marker contains a fault member")

// streamFaultPaths are the paths of the stage 2 members of a stream
// envelope and an HTTP state. Each member below one of them is also a
// stage 2 member. The faults group key is a member of its own.
var streamFaultPaths = []string{
	"/full/state/simulation/faultContract", "/full/state/simulation/faults",
	"/frame/state/simulation/faultContract", "/frame/state/simulation/faults",
	"/delta/groups/faults",
}

// scanFaultMembers reports whether data has a stage 2 member or the faults
// group, with any value. The typed decode reads an explicit zero, null, or
// empty object as no member, so only this scan sees it. The marker of a
// delta is the marker of its base frame, so the caller checks the result.
// No encoder writes null, so the scan refuses null for each stage 2
// member and each member below it.
func scanFaultMembers(data []byte) (bool, error) {
	return scanFaultPaths(data, streamFaultPaths)
}

// scanFaultPaths is scanFaultMembers for the members at prefixes and
// below them. The prefix "" names each member of data.
func scanFaultPaths(data []byte, prefixes []string) (bool, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	found := false
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return found, nil
		}
		if err != nil {
			return false, err
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		path := arrayPath(decoder)
		matched := false
		for _, prefix := range prefixes {
			if path == prefix || strings.HasPrefix(path, prefix+"/") {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		found = true
		if decoder.PeekKind() == jsontext.KindNull {
			return false, fmt.Errorf("stream fault member %s is null", decoder.StackPointer())
		}
	}
}

// checkFaultPresence refuses an envelope whose bytes have a stage 2 member
// when the frame that it makes has no fault marker. A delta has the marker
// of its base frame.
func checkFaultPresence(e StreamEnvelope, previous StreamFrame) error {
	marker := previous.State.Simulation.FaultContract
	if e.Full != nil {
		marker = e.Full.State.Simulation.FaultContract
	}
	if e.faultMembers && marker == "" {
		return errFaultStreamUnmarked
	}
	return nil
}

// faultKindMembers are the members of each kind of fault record (section
// 13.4). A pod record needs the pod members and has no debris member, and
// debris the reverse. endTick is optional for each kind.
var faultKindMembers = map[string][]string{
	sim.FaultKindPod:    {"podID", "phase", "evacuateTick"},
	sim.FaultKindDebris: {"laneID", "fromMeters", "toMeters"},
}

// decodeFaultView decodes one active fault of a stream frame with the
// member table of section 13.4: each required member is present, and each
// member of the other kind is absent. A present endTick is after
// startTick. The typed decode cannot see these rules, because it reads an
// absent member and a zero alike. It refuses an unknown member. Each
// caller scans the document for null with scanFaultPaths first.
// checkFaultFrame checks the values.
func decodeFaultView(decoder *jsontext.Decoder, view *sim.FaultView) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var members map[string]jsontext.Value
	if err := jsonv2.Unmarshal(value, &members); err != nil {
		return err
	}
	var kind string
	if err := jsonv2.Unmarshal(members["kind"], &kind); err != nil {
		return fmt.Errorf("fault kind: %w", err)
	}
	// checkFaultFrame refuses an unknown kind, which has no member of a
	// kind.
	for _, name := range append([]string{"id", "startTick"}, faultKindMembers[kind]...) {
		if members[name] == nil {
			return fmt.Errorf("%s fault has no %s", kind, name)
		}
	}
	for other, names := range faultKindMembers {
		for _, name := range names {
			if other != kind && members[name] != nil {
				return fmt.Errorf("%s fault has the member %s", kind, name)
			}
		}
	}
	var next sim.FaultView
	if err := jsonv2.Unmarshal(value, &next, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if members["endTick"] != nil && next.EndTick <= next.StartTick {
		return fmt.Errorf("fault %s ends at tick %d, not after its start", next.ID, next.EndTick)
	}
	*view = next
	return nil
}

// faultDecodeOptions decode the faults of a stream frame with
// decodeFaultView.
func faultDecodeOptions() jsonv2.Options {
	return jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(decodeFaultView))
}

// decodeFaultsGroup decodes the faults replacement group of a delta. The
// bounded scan reads the group alone, so the path "/active" bounds its
// records. The group has no null member.
func decodeFaultsGroup(raw []byte) (sim.FaultsView, error) {
	if err := prescanJSON(raw, streamLimits(contractMarkers{})); err != nil {
		return sim.FaultsView{}, err
	}
	if _, err := scanFaultPaths(raw, []string{""}); err != nil {
		return sim.FaultsView{}, err
	}
	var group sim.FaultsView
	if err := decodeStreamJSON(raw, &group, faultDecodeOptions()); err != nil {
		return sim.FaultsView{}, err
	}
	return group, nil
}

// checkFaultFrame checks the typed stage 2 members of a frame (section
// 13.5). Without the fault marker, it refuses each nonzero member. With
// it, the records have unique IDs of the form i<generation>.<serial> in
// strictly increasing serial order, at most one record for each pod, and
// at most maxDebrisRecords debris records.
// Each record has the values of its kind, and no tick above the tick of
// the frame. StreamAssembler checks the lanes against the topology.
func checkFaultFrame(frame SimulationFrame) error {
	if err := sim.ValidateFaultContracts(frame.FaultContract, frame.IncidentContract); err != nil {
		return err
	}
	faults := frame.Faults
	if frame.FaultContract == "" {
		if faults.Active != nil || faults.Counters != (sim.FaultCounters{}) {
			return errFaultStreamUnmarked
		}
		return nil
	}
	counters := faults.Counters
	if counters.Started < 0 || counters.Cleared < 0 || counters.Evacuations < 0 || counters.Reroutes < 0 || counters.FaultWaitTicks < 0 {
		return errors.New("negative fault counter")
	}
	vehicles := make(map[string]bool, len(frame.Vehicles))
	for _, vehicle := range frame.Vehicles {
		vehicles[vehicle.Pod.ID] = true
	}
	pods := make(map[string]bool)
	debris := 0
	var last uint64
	for index, fault := range faults.Active {
		serial, ok := faultSerial(fault.ID)
		if !ok || index > 0 && serial <= last {
			return fmt.Errorf("fault %q has no valid ID in serial order", fault.ID)
		}
		last = serial
		if fault.StartTick < 0 || fault.StartTick > frame.Tick || fault.EndTick < 0 || fault.EndTick != 0 && fault.EndTick <= fault.StartTick {
			return fmt.Errorf("fault %s has a tick out of range", fault.ID)
		}
		var err error
		switch fault.Kind {
		case sim.FaultKindPod:
			err = checkPodFaultView(fault, vehicles, pods)
		case sim.FaultKindDebris:
			debris++
			err = checkDebrisFaultView(fault)
		default:
			err = fmt.Errorf("unknown fault kind %q", fault.Kind)
		}
		if err != nil {
			return fmt.Errorf("fault %s: %w", fault.ID, err)
		}
	}
	// ApplyStream bounds the vehicles, and so the pod records, but the
	// encoders of a typed frame do not.
	switch {
	case debris > maxDebrisRecords:
		return errors.New("too many debris records")
	case len(pods) > project.MaxPods:
		return errors.New("too many pod records")
	}
	return nil
}

// faultSerial returns the serial of a fault ID of the form
// i<generation>.<serial>, with each number in its canonical decimal form.
func faultSerial(id string) (uint64, bool) {
	generation, serial, found := strings.Cut(strings.TrimPrefix(id, "i"), ".")
	if !found || !strings.HasPrefix(id, "i") {
		return 0, false
	}
	g, err := strconv.ParseUint(generation, 10, 64)
	if err != nil || strconv.FormatUint(g, 10) != generation {
		return 0, false
	}
	n, err := strconv.ParseUint(serial, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != serial {
		return 0, false
	}
	return n, true
}

// checkPodFaultView checks a pod record: the pod is a vehicle of the frame
// with no other record, the phase is known, the evacuation tick is not
// before the start, and no debris member is set.
func checkPodFaultView(fault sim.FaultView, vehicles, pods map[string]bool) error {
	switch {
	case !vehicles[fault.PodID] || pods[fault.PodID]:
		return fmt.Errorf("pod %q is not a vehicle, or has another record", fault.PodID)
	case fault.Phase != sim.FaultPhaseBraking && fault.Phase != sim.FaultPhaseStopped && fault.Phase != sim.FaultPhaseEvacuated:
		return fmt.Errorf("unknown phase %q", fault.Phase)
	case fault.EvacuateTick == nil || *fault.EvacuateTick < fault.StartTick:
		return errors.New("the evacuation tick is missing or before the start")
	case fault.LaneID != "" || fault.FromMeters != nil || fault.ToMeters != nil:
		return errors.New("a pod record has a debris member")
	}
	pods[fault.PodID] = true
	return nil
}

// checkDebrisFaultView checks the segment of a debris record and that no
// pod member is set.
func checkDebrisFaultView(fault sim.FaultView) error {
	switch {
	case fault.LaneID == "" || fault.FromMeters == nil || fault.ToMeters == nil:
		return errors.New("a debris record has no lane or segment")
	case fault.PodID != "" || fault.Phase != "" || fault.EvacuateTick != nil:
		return errors.New("a debris record has a pod member")
	}
	from, to := *fault.FromMeters, *fault.ToMeters
	// Each comparison is false for NaN, and an infinite bound fails the 0
	// test or the length test.
	if !(0 <= from && from < to && to-from <= maxDebrisMeters) {
		return fmt.Errorf("debris segment %g to %g is out of range", from, to)
	}
	return nil
}

// maxDebrisMeters is the longest debris segment (section 7.3).
const maxDebrisMeters = 50

// faultFrameBinding refuses a frame whose fault marker is not the marker
// of its topology, and a topology whose markers are not valid. A delta
// carries no marker, so the marker of a stream changes only with a new
// topology.
func faultFrameBinding(topology TopologySnapshot, frame SimulationFrame) error {
	if err := sim.ValidateFaultContracts(topology.FaultContract, topology.IncidentContract); err != nil {
		return err
	}
	if topology.FaultContract != frame.FaultContract {
		return errors.New("topology fault contract does not match state")
	}
	return nil
}

// faultLanes checks each debris record of a frame against the topology:
// the lane is known, and the segment ends within the lane.
func (a *StreamAssembler) faultLanes(frame SimulationFrame) error {
	for _, fault := range frame.Faults.Active {
		if fault.Kind != sim.FaultKindDebris {
			continue
		}
		index, ok := a.laneIndexes[fault.LaneID]
		if !ok {
			return fmt.Errorf("fault %s names an unknown lane", fault.ID)
		}
		if *fault.ToMeters > a.topology.Network.Length(a.topology.Network.Lanes[index]) {
			return fmt.Errorf("fault %s ends past its lane", fault.ID)
		}
	}
	return nil
}

// cloneFaults returns a copy of faults whose records share no memory with
// faults.
func cloneFaults(faults sim.FaultsView) sim.FaultsView {
	if faults.Active == nil {
		return faults
	}
	active := make([]sim.FaultView, len(faults.Active))
	for index, fault := range faults.Active {
		if fault.FromMeters != nil {
			fault.FromMeters = new(*fault.FromMeters)
		}
		if fault.ToMeters != nil {
			fault.ToMeters = new(*fault.ToMeters)
		}
		if fault.EvacuateTick != nil {
			fault.EvacuateTick = new(*fault.EvacuateTick)
		}
		active[index] = fault
	}
	faults.Active = active
	return faults
}

// faultIDs returns the IDs of the active faults of a frame.
func faultIDs(frame SimulationFrame) map[string]bool {
	ids := make(map[string]bool, len(frame.Faults.Active))
	for _, fault := range frame.Faults.Active {
		ids[fault.ID] = true
	}
	return ids
}
