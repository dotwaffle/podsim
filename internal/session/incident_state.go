package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// The wire values of the stage 1 save members (incident contract, sections
// 4.1, 9.1, and 11.5).
const (
	// knownIncidentHolds are the hold bits: 1 for a fault, 2 for an
	// emergency.
	knownIncidentHolds = 1 | 2
	// incidentEmergencyUnload and incidentEmptyRecovery are the first and
	// the last operational purpose code. Code 0 is service, which has no
	// tuple.
	incidentEmergencyUnload = 1
	incidentEmptyRecovery   = 3
)

// errIncidentMemberUnmarked refuses a stage 1 member in a saved state
// whose project has no incident marker.
var errIncidentMemberUnmarked = errors.New("saved state without the incident marker contains an incident member")

// savedIncidentPaths are the paths of the stage 1 members of a save, with
// "*" for each array index (section 11.5).
var savedIncidentPaths = map[string]bool{
	"/simulation/interrupted":               true,
	"/simulation/interruptedPassengers":     true,
	"/simulation/incidentSerial":            true,
	"/simulation/pods/*/withdrawn":          true,
	"/simulation/pods/*/operational":        true,
	"/simulation/pods/*/riders/*/legFrom":   true,
	"/simulation/waiting/*/request/legFrom": true,
	"/simulation/waiting/*/excludedPod":     true,
}

// scanSavedIncidentMembers reads the tokens of a saved state. Without the
// incident marker, it refuses each stage 1 member with any value, also 0,
// null, or an empty array: the typed decode reads such a value as no
// member, so only this scan sees it. With the marker, it refuses null,
// which no encoder writes.
func scanSavedIncidentMembers(data []byte, marked bool) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 || !savedIncidentPaths[arrayPath(decoder)] {
			continue
		}
		if !marked {
			return fmt.Errorf("%w: %s", errIncidentMemberUnmarked, decoder.StackPointer())
		}
		if decoder.PeekKind() == 'n' {
			return fmt.Errorf("saved incident member %s is null", decoder.StackPointer())
		}
	}
}

// checkIncidentMembers checks the raw stage 1 members of a decoded saved
// state against the incident marker of its project.
func checkIncidentMembers(data []byte, file stateFile) error {
	return scanSavedIncidentMembers(data, file.Project.IncidentContract != "")
}

// validateIncidentValues refuses a stage 1 value in a state file whose
// project has no incident marker. The encoder calls it, so that no save
// without the marker has a stage 1 member.
func (file *stateFile) validateIncidentValues() error {
	if file.Project.IncidentContract != "" {
		return nil
	}
	state := file.Simulation
	if state.Interrupted != 0 || state.InterruptedPassengers != 0 || state.IncidentSerial != 0 {
		return errIncidentMemberUnmarked
	}
	legFrom := func(request sim.SavedRequest) bool { return request.LegFrom != "" }
	for _, pod := range state.Pods {
		if pod.Withdrawn != 0 || pod.Purpose != 0 || pod.Owner != 0 || pod.Interrupt != 0 || slices.ContainsFunc(pod.Riders, legFrom) {
			return errIncidentMemberUnmarked
		}
	}
	for _, trip := range state.Waiting {
		if trip.ExcludedPod != "" || legFrom(trip.Request) {
			return errIncidentMemberUnmarked
		}
	}
	return nil
}

// operationalTuple is the saved operational destination of a pod:
// [purpose, owner], or [1, owner, interrupt] for an emergency unload that
// interrupts riders.
type operationalTuple struct {
	Purpose   uint8
	Owner     uint8
	Interrupt uint32
}

// MarshalJSONTo writes the tuple. It omits a zero interrupt set.
func (tuple *operationalTuple) MarshalJSONTo(encoder *jsontext.Encoder) error {
	if tuple.Interrupt == 0 {
		return json.MarshalEncode(encoder, [2]uint32{uint32(tuple.Purpose), uint32(tuple.Owner)})
	}
	return json.MarshalEncode(encoder, [3]uint32{uint32(tuple.Purpose), uint32(tuple.Owner), tuple.Interrupt})
}

// UnmarshalJSONFrom checks the length of the tuple, the purpose, and the
// owner. The owner is one known hold. Only an emergency unload has an
// interrupt set, and the set is not empty.
func (tuple *operationalTuple) UnmarshalJSONFrom(decoder *jsontext.Decoder) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var numbers []jsontext.Value
	if err := json.Unmarshal(value, &numbers); err != nil {
		return err
	}
	if len(numbers) != 2 && len(numbers) != 3 {
		return errors.New("saved operational tuple needs two or three numbers")
	}
	for _, number := range numbers {
		if number.Kind() != jsontext.KindNumber {
			return errors.New("saved operational tuple contains a nonnumber")
		}
	}
	var next operationalTuple
	if err := json.Unmarshal(numbers[0], &next.Purpose); err != nil {
		return err
	}
	if err := json.Unmarshal(numbers[1], &next.Owner); err != nil {
		return err
	}
	if len(numbers) == 3 {
		if err := json.Unmarshal(numbers[2], &next.Interrupt); err != nil {
			return err
		}
		if next.Purpose != incidentEmergencyUnload || next.Interrupt == 0 {
			return errors.New("saved interrupt set needs an emergency unload and a rider")
		}
	}
	switch {
	case next.Purpose < incidentEmergencyUnload || next.Purpose > incidentEmptyRecovery:
		return fmt.Errorf("unknown saved operational purpose %d", next.Purpose)
	case bits.OnesCount8(next.Owner) != 1 || next.Owner&^knownIncidentHolds != 0:
		return fmt.Errorf("saved operational owner %d is not one known hold", next.Owner)
	}
	*tuple = next
	return nil
}

// savedRequestWire is a saved order with its leg origin as an index into
// the saved project stations.
type savedRequestWire struct {
	packedSavedRequest
	LegFrom *int `json:"legFrom,omitzero"`
}

type savedTripFields sim.SavedTrip

// savedTripWire is a saved waiting trip with its excluded pod as an index
// into the saved pods.
type savedTripWire struct {
	savedTripFields
	ExcludedPod *int `json:"excludedPod,omitzero"`
}

// savedIndexes give the save index of each station of the saved project
// and of each saved pod.
type savedIndexes struct {
	stations map[string]int
	pods     map[string]int
}

func newSavedIndexes(file stateFile) savedIndexes {
	indexes := savedIndexes{stations: make(map[string]int, len(file.Project.Network.Stations)), pods: make(map[string]int, len(file.Simulation.Pods))}
	for index, station := range file.Project.Network.Stations {
		indexes.stations[station.ID] = index
	}
	for index, pod := range file.Simulation.Pods {
		indexes.pods[pod.ID] = index
	}
	return indexes
}

// encodeRequest writes a saved order with packed text and its leg origin
// as a station index. Index 0 is a present value, so the member is written
// for every leg origin.
func (indexes savedIndexes) encodeRequest(encoder *jsontext.Encoder, request sim.SavedRequest) error {
	var wire savedRequestWire
	if request.LegFrom != "" {
		index, ok := indexes.stations[request.LegFrom]
		if !ok {
			return errors.New("saved leg origin is not a station of the saved project")
		}
		wire.LegFrom = &index
		request.LegFrom = ""
	}
	if err := transformOrderText([]*string{&request.From, &request.To, &request.PodID, &request.DispatchReason, &request.ServiceID}, false); err != nil {
		return err
	}
	wire.packedSavedRequest = packedSavedRequest(request)
	return json.MarshalEncode(encoder, wire)
}

// encodeTrip writes a saved waiting trip with its excluded pod as a pod
// index. The encoder writes its order with encodeRequest.
func (indexes savedIndexes) encodeTrip(encoder *jsontext.Encoder, trip sim.SavedTrip) error {
	wire := savedTripWire{savedTripFields: savedTripFields(trip)}
	if trip.ExcludedPod != "" {
		index, ok := indexes.pods[trip.ExcludedPod]
		if !ok {
			return errors.New("saved excluded pod is not a saved pod")
		}
		wire.ExcludedPod = &index
		wire.savedTripFields.ExcludedPod = ""
	}
	return json.MarshalEncode(encoder, wire)
}

// noSavedReference marks an absent leg origin or exclusion in the tables
// of savedIncidentRefs.
const noSavedReference = -1

// decodeSavedRequest decodes a saved order with packed text. It returns
// the leg origin index, or noSavedReference.
func decodeSavedRequest(decoder *jsontext.Decoder, request *sim.SavedRequest) (int, error) {
	var wire savedRequestWire
	if err := json.UnmarshalDecode(decoder, &wire, json.RejectUnknownMembers(true)); err != nil {
		return 0, err
	}
	next := sim.SavedRequest(wire.packedSavedRequest)
	if err := transformOrderText([]*string{&next.From, &next.To, &next.PodID, &next.DispatchReason, &next.ServiceID}, true); err != nil {
		return 0, err
	}
	*request = next
	if wire.LegFrom == nil {
		return noSavedReference, nil
	}
	if *wire.LegFrom < 0 {
		return 0, errors.New("saved leg origin index is negative")
	}
	return *wire.LegFrom, nil
}

// savedIncidentRefs retains the unresolved stage 1 references of a saved
// state in saved order: the leg origin of each rider of each pod, and the
// leg origin and the excluded pod of each waiting trip. noSavedReference
// marks an absent reference.
type savedIncidentRefs struct {
	riders   [][]int
	waiting  []int
	excluded []int
}

// present reports whether refs has a reference.
func (refs *savedIncidentRefs) present() bool {
	some := func(indexes []int) bool {
		return slices.ContainsFunc(indexes, func(index int) bool { return index != noSavedReference })
	}
	return slices.ContainsFunc(refs.riders, some) || some(refs.waiting) || some(refs.excluded)
}

// decodeTrip decodes a saved waiting trip and records its references in
// refs.
func (refs *savedIncidentRefs) decodeTrip(decoder *jsontext.Decoder, trip *sim.SavedTrip) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	leg := noSavedReference
	decodeRequest := func(decoder *jsontext.Decoder, request *sim.SavedRequest) error {
		var err error
		leg, err = decodeSavedRequest(decoder, request)
		return err
	}
	var wire savedTripWire
	options := json.JoinOptions(json.RejectUnknownMembers(true), json.WithUnmarshalers(json.UnmarshalFromFunc(decodeRequest)))
	if err := json.Unmarshal(value, &wire, options); err != nil {
		return err
	}
	excluded := noSavedReference
	if wire.ExcludedPod != nil {
		if *wire.ExcludedPod < 0 {
			return errors.New("saved excluded pod index is negative")
		}
		excluded = *wire.ExcludedPod
	}
	*trip = sim.SavedTrip(wire.savedTripFields)
	refs.waiting = append(refs.waiting, leg)
	refs.excluded = append(refs.excluded, excluded)
	return nil
}

// checkSavedOperational checks the holds of a decoded saved pod and the
// owner and the interrupt set of its operational destination: W1, W5, and
// each interrupt bit names an active rider.
func checkSavedOperational(pod sim.SavedPod, tuple *operationalTuple) error {
	if pod.Withdrawn&^knownIncidentHolds != 0 {
		return fmt.Errorf("saved pod holds %d have an unknown bit", pod.Withdrawn)
	}
	if tuple == nil {
		return nil
	}
	if pod.Withdrawn&tuple.Owner == 0 {
		return errors.New("saved operational owner is not a hold of the pod")
	}
	for bit := range 32 {
		if tuple.Interrupt&(1<<bit) == 0 {
			continue
		}
		if bit >= len(pod.Riders) || pod.Riders[bit].Completed {
			return fmt.Errorf("saved interrupt bit %d names no active rider", bit)
		}
	}
	return nil
}

// resolveIncidentRefs binds the leg origins and the excluded pods after
// the caller accepts the saved project. A leg origin is a passenger
// station other than the destination. An excluded pod is a saved pod, the
// trip never boarded, and the pod is neither the pod nor the hold of the
// trip (X1). Each failure refuses the whole save.
func (file *stateFile) resolveIncidentRefs() error {
	refs := file.incidentRefs
	if refs == nil {
		return nil
	}
	state := &file.Simulation
	stations := file.Project.Network.Stations
	if len(refs.riders) != len(state.Pods) || len(refs.waiting) != len(state.Waiting) || len(refs.excluded) != len(state.Waiting) {
		return errors.New("saved incident references do not align")
	}
	legFrom := func(index int, request *sim.SavedRequest) error {
		if index == noSavedReference {
			return nil
		}
		if index >= len(stations) {
			return fmt.Errorf("order %d: saved leg origin index %d is out of range", request.ID, index)
		}
		station := stations[index]
		if station.ParkingOnly || station.ID == request.To {
			return fmt.Errorf("order %d: saved leg origin %d is a parking station or the destination", request.ID, index)
		}
		request.LegFrom = station.ID
		return nil
	}
	for position, legs := range refs.riders {
		pod := &state.Pods[position]
		if legs == nil {
			continue
		}
		if len(legs) != len(pod.Riders) {
			return errors.New("saved leg origins do not align with the riders")
		}
		for index, leg := range legs {
			if err := legFrom(leg, &pod.Riders[index]); err != nil {
				return err
			}
		}
	}
	for position := range state.Waiting {
		trip := &state.Waiting[position]
		if err := legFrom(refs.waiting[position], &trip.Request); err != nil {
			return err
		}
		index := refs.excluded[position]
		if index == noSavedReference {
			continue
		}
		if index >= len(state.Pods) {
			return fmt.Errorf("order %d: saved excluded pod index %d is out of range", trip.Request.ID, index)
		}
		excluded := state.Pods[index].ID
		if trip.Boarded || trip.Request.PodID == excluded || trip.DeferPodID == excluded {
			return fmt.Errorf("order %d: the saved exclusion of pod %s breaks X1", trip.Request.ID, excluded)
		}
		trip.ExcludedPod = excluded
	}
	file.incidentRefs = nil
	return nil
}
