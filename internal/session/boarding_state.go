package session

import (
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// boardingTuple uses the berth order of the rider's saved source station.
type boardingTuple struct {
	Index  int
	Meters float64
}

// MarshalJSONTo writes a two-number boarding tuple.
func (tuple *boardingTuple) MarshalJSONTo(encoder *jsontext.Encoder) error {
	return json.MarshalEncode(encoder, [2]float64{float64(tuple.Index), tuple.Meters})
}

// UnmarshalJSONFrom checks the tuple shape and its numeric values.
func (tuple *boardingTuple) UnmarshalJSONFrom(decoder *jsontext.Decoder) error {
	value, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	var numbers []jsontext.Value
	if err := json.Unmarshal(value, &numbers); err != nil {
		return err
	}
	if len(numbers) != 2 {
		return errors.New("saved boarding tuple needs two numbers")
	}
	for _, number := range numbers {
		if number.Kind() != jsontext.KindNumber {
			return errors.New("saved boarding tuple contains a nonnumber")
		}
	}
	if err := json.Unmarshal(numbers[0], &tuple.Index); err != nil {
		return err
	}
	if err := json.Unmarshal(numbers[1], &tuple.Meters); err != nil {
		return err
	}
	if tuple.Index < 0 || !finiteNumber(tuple.Meters) || tuple.Meters < 0 {
		return errors.New("invalid saved boarding tuple numbers")
	}
	return nil
}

type boardingSource map[string][]sim.Berth

func bindBoardingSource(config project.Config) boardingSource {
	source := make(boardingSource, len(config.Network.Stations))
	for _, station := range config.Network.Stations {
		source[station.ID] = slices.Clone(station.Berths)
	}
	return source
}

// savedPassengerMeters excludes empty movement from completed rider history.
func savedPassengerMeters(pod sim.SavedPod) (float64, error) {
	meters := pod.RiddenMeters
	active := slices.ContainsFunc(pod.Riders, func(rider sim.SavedRequest) bool { return !rider.Completed })
	if pod.Activity == "traveling" && active {
		if !finiteNumber(pod.Distance) || pod.Distance < 0 {
			return 0, errors.New("invalid saved passenger travel distance")
		}
		meters += pod.Distance
	}
	if !finiteNumber(pod.RiddenMeters) || pod.RiddenMeters < 0 || !finiteNumber(meters) {
		return 0, errors.New("invalid saved passenger cumulative distance")
	}
	return meters, nil
}

func validBoardingConsent(rider sim.SavedRequest) bool {
	return rider.SharingConsent == sim.SharedConsent || rider.Completed && rider.SharingConsent == sim.PrivateConsent
}

type savedPodFields sim.SavedPod

// boardingWirePod is a saved pod with its boarding records as tuples and
// its operational destination as one tuple (incident contract, section
// 11.5).
type boardingWirePod struct {
	savedPodFields
	Boardings   []boardingTuple   `json:"boardings,omitempty"`
	Operational *operationalTuple `json:"operational,omitzero"`
}

// encodePodContract writes a saved pod. The encoder writes its riders with
// the options of encoder.
func (source boardingSource) encodePodContract(encoder *jsontext.Encoder, pod sim.SavedPod, contract sim.OrderContract) error {
	wire := boardingWirePod{savedPodFields: savedPodFields(pod)}
	wire.Purpose, wire.Owner, wire.Interrupt = 0, 0, 0
	if pod.Purpose != 0 || pod.Owner != 0 || pod.Interrupt != 0 {
		wire.Operational = &operationalTuple{Purpose: pod.Purpose, Owner: pod.Owner, Interrupt: pod.Interrupt}
	}
	if len(pod.Boardings) == 0 {
		return json.MarshalEncode(encoder, wire)
	}
	if len(pod.Boardings) != len(pod.Riders) || len(pod.Boardings) > sim.MaxStoredRidersForOrderContract(pod.Class, contract) {
		return errors.New("invalid native boarding record alignment")
	}
	meters, err := savedPassengerMeters(pod)
	if err != nil {
		return err
	}
	wire.Boardings = make([]boardingTuple, len(pod.Boardings))
	wire.JourneyOrigin = ""
	for index, record := range pod.Boardings {
		if !finiteNumber(record.MetersAtBoarding) || record.MetersAtBoarding < 0 || record.MetersAtBoarding > meters || !validBoardingConsent(pod.Riders[index]) {
			return errors.New("invalid native boarding baseline or consent")
		}
		berths := source[cmp.Or(pod.Riders[index].LegFrom, pod.Riders[index].From)]
		berthIndex := slices.IndexFunc(berths, func(berth sim.Berth) bool { return berth.ID == record.BerthID })
		if berthIndex < 0 {
			return errors.New("boarding berth is outside the rider source station")
		}
		wire.Boardings[index] = boardingTuple{Index: berthIndex, Meters: record.MetersAtBoarding}
	}
	return json.MarshalEncode(encoder, wire)
}

// savedPodRefs are the unresolved references of a decoded saved pod: its
// boarding tuples, and the leg origin index of each rider or
// noSavedReference.
type savedPodRefs struct {
	tuples []boardingTuple
	legs   []int
}

// decodeBoardingPodContract decodes a saved pod with packed riders. The
// order marker contract bounds the boarding records. It checks the holds
// and the operational destination of the pod.
func decodeBoardingPodContract(decoder *jsontext.Decoder, pod *sim.SavedPod, contract sim.OrderContract) (savedPodRefs, error) {
	value, err := decoder.ReadValue()
	if err != nil {
		return savedPodRefs{}, err
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(value, &members); err != nil {
		return savedPodRefs{}, err
	}
	field, present := members["boardings"]
	if present {
		if field.Kind() != jsontext.KindBeginArray {
			return savedPodRefs{}, errors.New("saved boardings must be an array")
		}
		if _, contradictory := members["journeyOrigin"]; contradictory {
			return savedPodRefs{}, errors.New("saved boardings contradict journeyOrigin")
		}
	}
	var refs savedPodRefs
	decodeRider := func(decoder *jsontext.Decoder, rider *sim.SavedRequest) error {
		leg, err := decodeSavedRequest(decoder, rider)
		refs.legs = append(refs.legs, leg)
		return err
	}
	var wire boardingWirePod
	// The decode of value does not inherit the options of the state
	// decoder, so it registers the packed rider decoder itself.
	options := json.JoinOptions(json.RejectUnknownMembers(true), json.WithUnmarshalers(json.JoinUnmarshalers(
		json.UnmarshalFromFunc(decodePlatoon), json.UnmarshalFromFunc(decodeRider))))
	if err := json.Unmarshal(value, &wire, options); err != nil {
		return savedPodRefs{}, err
	}
	if present && (len(wire.Boardings) < 1 || len(wire.Boardings) > sim.MaxStoredRidersForOrderContract(wire.Class, contract) || len(wire.Boardings) != len(wire.Riders)) {
		return savedPodRefs{}, errors.New("invalid saved boarding tuple alignment")
	}
	*pod = sim.SavedPod(wire.savedPodFields)
	pod.Boardings = nil
	if err := checkSavedOperational(*pod, wire.Operational); err != nil {
		return savedPodRefs{}, err
	}
	if wire.Operational != nil {
		pod.Purpose, pod.Owner, pod.Interrupt = wire.Operational.Purpose, wire.Operational.Owner, wire.Operational.Interrupt
	}
	if !slices.ContainsFunc(refs.legs, func(leg int) bool { return leg != noSavedReference }) {
		refs.legs = nil
	}
	refs.tuples = wire.Boardings
	return refs, nil
}

// resolveBoardings binds the saved references only after the caller
// accepts the saved project. It binds the leg origins and the excluded
// pods first, because a boarding tuple resolves against the leg origin of
// its rider.
func (file *stateFile) resolveBoardings() error {
	if err := file.resolveIncidentRefs(); err != nil {
		return err
	}
	if len(file.boardingTuples) == 0 {
		return nil
	}
	if len(file.boardingTuples) != len(file.Simulation.Pods) {
		return errors.New("saved boarding pod positions do not align")
	}
	source := bindBoardingSource(file.Project)
	for position, tuples := range file.boardingTuples {
		if len(tuples) == 0 {
			continue
		}
		pod := &file.Simulation.Pods[position]
		if len(tuples) != len(pod.Riders) || len(tuples) > sim.MaxStoredRidersForOrderContract(pod.Class, file.OrderContract) {
			return errors.New("invalid saved boarding record alignment")
		}
		meters, err := savedPassengerMeters(*pod)
		if err != nil {
			return fmt.Errorf("saved pod %d: %w", position, err)
		}
		records := make([]sim.RiderBoarding, len(tuples))
		for index, tuple := range tuples {
			berths := source[cmp.Or(pod.Riders[index].LegFrom, pod.Riders[index].From)]
			if tuple.Index < 0 || tuple.Index >= len(berths) || !finiteNumber(tuple.Meters) || tuple.Meters < 0 || tuple.Meters > meters || !validBoardingConsent(pod.Riders[index]) {
				return errors.New("invalid saved source boarding reference or baseline")
			}
			records[index] = sim.RiderBoarding{BerthID: berths[tuple.Index].ID, MetersAtBoarding: tuple.Meters}
		}
		pod.Boardings = records
	}
	file.boardingTuples = nil
	return nil
}

func boardingStateLimits(limits jsonLimits) jsonLimits {
	limits.arrays = maps.Clone(limits.arrays)
	limits.arrays["/simulation/pods/*/boardings"] = sim.MaxSharedRideParties
	limits.arrays["/simulation/pods/*/boardings/*"] = 2
	return limits
}

// finiteNumber reports whether value is neither NaN nor an infinity.
func finiteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
