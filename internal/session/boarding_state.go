package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

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

func (source boardingSource) encodePod(encoder *jsontext.Encoder, pod sim.SavedPod) error {
	return source.encodePodContract(encoder, pod, "")
}

func (source boardingSource) encodeExpressPod(encoder *jsontext.Encoder, pod sim.SavedPod) error {
	return source.encodePodContract(encoder, pod, sim.ExpressOrderContract)
}

func (source boardingSource) encodePodContract(encoder *jsontext.Encoder, pod sim.SavedPod, contract sim.OrderContract) error {
	if len(pod.Boardings) == 0 {
		return json.MarshalEncode(encoder, savedPodFields(pod))
	}
	if len(pod.Boardings) != len(pod.Riders) || len(pod.Boardings) > sim.MaxStoredRidersForOrderContract(pod.Class, contract) || pod.LegacyCohort {
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

func decodeBoardingPod(decoder *jsontext.Decoder, pod *sim.SavedPod) ([]boardingTuple, error) {
	return decodeBoardingPodContract(decoder, pod, "")
}

func decodeExpressBoardingPod(decoder *jsontext.Decoder, pod *sim.SavedPod) ([]boardingTuple, error) {
	return decodeBoardingPodContract(decoder, pod, sim.ExpressOrderContract)
}

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
	options := json.JoinOptions(json.RejectUnknownMembers(true), json.WithUnmarshalers(json.JoinUnmarshalers(
		json.UnmarshalFromFunc(decodeV6Platoon), json.UnmarshalFromFunc(decodeCompactQueue))))
	if contract == sim.ExpressOrderContract {
		options = json.JoinOptions(json.RejectUnknownMembers(true), json.WithUnmarshalers(json.JoinUnmarshalers(json.UnmarshalFromFunc(decodeV6Platoon), json.UnmarshalFromFunc(decodeCompactQueue), json.UnmarshalFromFunc(decodePackedSavedRequest))))
	}
	if err := json.Unmarshal(value, &wire, options); err != nil {
		return nil, err
	}
	if present && (len(wire.Boardings) < 1 || len(wire.Boardings) > sim.MaxStoredRidersForOrderContract(wire.Class, contract) || len(wire.Boardings) != len(wire.Riders) || wire.LegacyCohort) {
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
		if len(tuples) != len(pod.Riders) || len(tuples) > sim.MaxStoredRidersForOrderContract(pod.Class, file.OrderContract) || pod.LegacyCohort {
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

func scanStateBoardingFields(data []byte, version int) error {
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
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		path := strings.Split(string(decoder.StackPointer()), "/")
		if len(path) == 5 && path[1] == "simulation" && path[2] == "pods" && path[4] == "boardings" && version < serviceStateVersion {
			return errors.New("legacy saved state contains boardings")
		}
	}
}
