package session

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
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
	if tuple.Index < 0 || !finiteCompactNumber(tuple.Meters) || tuple.Meters < 0 {
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
		if !finiteCompactNumber(pod.Distance) || pod.Distance < 0 {
			return 0, errors.New("invalid saved passenger travel distance")
		}
		meters += pod.Distance
	}
	if !finiteCompactNumber(pod.RiddenMeters) || pod.RiddenMeters < 0 || !finiteCompactNumber(meters) {
		return 0, errors.New("invalid saved passenger cumulative distance")
	}
	return meters, nil
}

func validBoardingConsent(rider sim.SavedRequest) bool {
	return rider.SharingConsent == sim.SharedConsent || rider.Completed && rider.SharingConsent == sim.PrivateConsent
}

type savedPodFields sim.SavedPod

type boardingWirePod struct {
	savedPodFields
	Boardings []boardingTuple `json:"boardings,omitempty"`
}

func (source boardingSource) encodePodContract(encoder *jsontext.Encoder, pod sim.SavedPod, contract sim.OrderContract) error {
	if len(pod.Boardings) == 0 {
		return json.MarshalEncode(encoder, savedPodFields(pod))
	}
	if len(pod.Boardings) != len(pod.Riders) || len(pod.Boardings) > sim.MaxStoredRidersForOrderContract(pod.Class, contract) {
		return errors.New("invalid native boarding record alignment")
	}
	meters, err := savedPassengerMeters(pod)
	if err != nil {
		return err
	}
	wire := boardingWirePod{savedPodFields: savedPodFields(pod), Boardings: make([]boardingTuple, len(pod.Boardings))}
	wire.JourneyOrigin = ""
	for index, record := range pod.Boardings {
		if !finiteCompactNumber(record.MetersAtBoarding) || record.MetersAtBoarding < 0 || record.MetersAtBoarding > meters || !validBoardingConsent(pod.Riders[index]) {
			return errors.New("invalid native boarding baseline or consent")
		}
		berths := source[pod.Riders[index].From]
		berthIndex := slices.IndexFunc(berths, func(berth sim.Berth) bool { return berth.ID == record.BerthID })
		if berthIndex < 0 {
			return errors.New("boarding berth is outside the rider source station")
		}
		wire.Boardings[index] = boardingTuple{Index: berthIndex, Meters: record.MetersAtBoarding}
	}
	return json.MarshalEncode(encoder, wire)
}

// decodeBoardingPodContract decodes a saved pod with packed riders. The
// order marker contract bounds the boarding records.
func decodeBoardingPodContract(decoder *jsontext.Decoder, pod *sim.SavedPod, contract sim.OrderContract) ([]boardingTuple, error) {
	value, err := decoder.ReadValue()
	if err != nil {
		return nil, err
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(value, &members); err != nil {
		return nil, err
	}
	field, present := members["boardings"]
	if present {
		if field.Kind() != jsontext.KindBeginArray {
			return nil, errors.New("saved boardings must be an array")
		}
		if _, contradictory := members["journeyOrigin"]; contradictory {
			return nil, errors.New("saved boardings contradict journeyOrigin")
		}
	}
	var wire boardingWirePod
	// The decode of value does not inherit the options of the state
	// decoder, so it registers the packed rider decoder itself.
	options := json.JoinOptions(json.RejectUnknownMembers(true), json.WithUnmarshalers(json.JoinUnmarshalers(
		json.UnmarshalFromFunc(decodePlatoon), json.UnmarshalFromFunc(decodeCompactQueue), json.UnmarshalFromFunc(decodePackedSavedRequest))))
	if err := json.Unmarshal(value, &wire, options); err != nil {
		return nil, err
	}
	if present && (len(wire.Boardings) < 1 || len(wire.Boardings) > sim.MaxStoredRidersForOrderContract(wire.Class, contract) || len(wire.Boardings) != len(wire.Riders)) {
		return nil, errors.New("invalid saved boarding tuple alignment")
	}
	*pod = sim.SavedPod(wire.savedPodFields)
	pod.Boardings = nil
	return wire.Boardings, nil
}

// resolveBoardings binds records only after the caller accepts the saved project.
func (file *stateFile) resolveBoardings() error {
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
			berths := source[pod.Riders[index].From]
			if tuple.Index < 0 || tuple.Index >= len(berths) || !finiteCompactNumber(tuple.Meters) || tuple.Meters < 0 || tuple.Meters > meters || !validBoardingConsent(pod.Riders[index]) {
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
