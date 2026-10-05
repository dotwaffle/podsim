package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

type boardingBerth struct {
	station        string
	stationClasses sim.ClassSet
	classes        sim.ClassSet
	parkingOnly    bool
}

func finiteNonnegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func validateVehicleBoardings(v VehicleFrame) error { return validateVehicleBoardingsContract(v, "") }

func validateVehicleBoardingsContract(v VehicleFrame, contract sim.OrderContract) error {
	if !finiteNonnegative(v.RiddenMeters) {
		return errors.New("invalid passenger chain distance")
	}
	if len(v.Boardings) == 0 {
		if v.Boardings != nil {
			return errors.New("full boarding records cannot be empty")
		}
		if v.RiddenMeters != 0 {
			return errors.New("passenger chain distance needs boarding records")
		}
		return nil
	}
	if len(v.Boardings) > sim.MaxStoredRidersForOrderContract(v.Pod.Class, contract) || len(v.Boardings) != len(v.Riders) {
		return errors.New("invalid boarding record alignment")
	}
	for i, record := range v.Boardings {
		if record.BerthID == "" || len(record.BerthID) > 64 || !finiteNonnegative(record.MetersAtBoarding) || record.MetersAtBoarding > v.RiddenMeters {
			return errors.New("invalid boarding record")
		}
		rider := v.Riders[i]
		if !validStreamOrderContract(rider, contract) {
			return errors.New("boarding record needs a known service order")
		}
		if !rider.Completed && rider.SharingConsent != sim.SharedConsent {
			return errors.New("active recorded rider needs shared consent")
		}
	}
	return nil
}

func (a *StreamAssembler) vehicleBoardings(v VehicleFrame) error {
	var err error
	if a.topology.OrderContract == "" {
		err = validateVehicleBoardings(v)
	} else {
		err = validateVehicleBoardingsContract(v, a.topology.OrderContract)
	}
	if err != nil {
		return err
	}
	for i, record := range v.Boardings {
		berth, ok := a.boardingBerths[record.BerthID]
		if !ok || berth.station != v.Riders[i].From || berth.parkingOnly || !berth.stationClasses.Allows(string(v.Pod.Class)) || !berth.classes.Allows(string(v.Pod.Class)) {
			return errors.New("boarding berth does not match rider origin or vehicle class")
		}
	}
	return nil
}

func ownStreamBoardings(f StreamFrame) StreamFrame {
	f.State.Simulation.CouplingGroups = cloneCouplingGroups(f.State.Simulation.CouplingGroups)
	if f.State.Simulation.OrderContract == sim.ExpressOrderContract {
		f.State.Simulation.Pending = slices.Clone(f.State.Simulation.Pending)
	}
	f.State.Simulation.Vehicles = slices.Clone(f.State.Simulation.Vehicles)
	for i := range f.State.Simulation.Vehicles {
		v := &f.State.Simulation.Vehicles[i]
		if f.State.Simulation.OrderContract == sim.ExpressOrderContract {
			v.Stops = slices.Clone(v.Stops)
		}
		v.Riders = slices.Clone(v.Riders)
		v.Boardings = slices.Clone(v.Boardings)
	}
	return f
}

// scanStreamBoardingMembers checks presence before typed decoding loses nulls.
func scanStreamBoardingMembers(data []byte, version int) error {
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
		name := token.String()
		if name != "boardings" && name != "riddenMeters" {
			continue
		}
		path := strings.Split(string(decoder.StackPointer()), "/")
		full := len(path) == 7 && (path[1] == "full" || path[1] == "frame") && path[2] == "state" && path[3] == "simulation" && path[4] == "vehicles" && streamArrayIndex(path[5])
		delta := len(path) == 5 && path[1] == "delta" && path[2] == "vehicles" && streamArrayIndex(path[3])
		metadata := len(path) == 7 && path[1] == "delta" && path[2] == "vehicles" && streamArrayIndex(path[3]) && path[4] == "metadata" && path[5] == "value"
		records := name == "boardings" && (full || delta)
		distance := name == "riddenMeters" && (full || metadata)
		if !records && !distance {
			continue
		}
		raw, err := decoder.ReadValue()
		if err != nil {
			return err
		}
		if distance {
			var meters float64
			if bytes.Equal(raw, []byte("null")) || decodeStreamJSON(raw, &meters) != nil || !finiteNonnegative(meters) {
				return errors.New("invalid passenger chain distance")
			}
			continue
		}
		if delta {
			members, memberErr := boardingMembers(raw)
			if memberErr != nil || len(members) != 1 || members["value"] == nil {
				return errors.New("boarding replacement needs exactly one value")
			}
			raw = members["value"]
		}
		if version == ExpressStreamVersion {
			err = scanBoardingRecordsLimit(raw, delta, 20)
		} else {
			err = scanBoardingRecords(raw, delta)
		}
		if err != nil {
			return err
		}
	}
}

func streamArrayIndex(value string) bool {
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func boardingMembers(raw []byte) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || raw[0] != '{' {
		return nil, errors.New("boarding member needs an object")
	}
	var members map[string]json.RawMessage
	if err := decodeStreamJSON(raw, &members); err != nil {
		return nil, err
	}
	return members, nil
}

func scanBoardingRecords(raw []byte, allowEmpty bool) error {
	return scanBoardingRecordsLimit(raw, allowEmpty, 8)
}

func scanBoardingRecordsLimit(raw []byte, allowEmpty bool, limit int) error {
	if len(raw) == 0 || raw[0] != '[' {
		return errors.New("boarding records need an array")
	}
	var records []json.RawMessage
	if err := decodeStreamJSON(raw, &records); err != nil {
		return err
	}
	if len(records) > limit || len(records) == 0 && !allowEmpty {
		return errors.New("boarding records need 1 to 8 entries")
	}
	for _, record := range records {
		members, err := boardingMembers(record)
		if err != nil || len(members) != 2 || members["berthID"] == nil || members["metersAtBoarding"] == nil {
			return errors.New("boarding record needs exactly its berth and baseline")
		}
		var id string
		var meters float64
		if decodeStreamJSON(members["berthID"], &id) != nil || id == "" || len(id) > 64 || bytes.Equal(members["metersAtBoarding"], []byte("null")) || decodeStreamJSON(members["metersAtBoarding"], &meters) != nil || !finiteNonnegative(meters) {
			return errors.New("invalid boarding record members")
		}
	}
	return nil
}
