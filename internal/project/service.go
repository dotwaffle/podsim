package project

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// ServiceVersion identifies projects with authored vehicle and service metadata.
const ServiceVersion = 3

// ExpressVersion identifies explicit Express operating projects.
const ExpressVersion = 4

// MaxExpressServices bounds the directed service registry of a project.
const MaxExpressServices = 300

func validateServiceVersion(config Config) error {
	if config.Version == ExpressVersion {
		if config.OrderContract != sim.ExpressOrderContract {
			return errors.New("project version 4 requires express-v1")
		}
		return nil
	}
	if config.Version == CouplingVersion {
		return sim.ValidateOrderContract(config.OrderContract)
	}
	if config.OrderContract != "" {
		return errors.New("order contract requires project version 4")
	}
	if config.Version == ServiceVersion {
		return nil
	}
	present := config.ExpressServices != nil || config.StationQueueSpacing != "" || config.OnboardPickups
	for _, placement := range config.Fleet {
		present = present || placement.Class != ""
	}
	for _, lane := range config.Network.Lanes {
		present = present || lane.VehicleClasses != 0
	}
	for _, station := range config.Network.Stations {
		present = present || station.VehicleClasses != 0
		for _, berth := range station.Berths {
			present = present || berth.VehicleClasses != 0
		}
	}
	if present {
		return errors.New("vehicle and service fields require project version 3")
	}
	return nil
}

// scanProjectService checks new field presence and shape before typed allocation.
func scanProjectService(data []byte) (bool, error) {
	fields, err := scanProjectFields(data)
	return fields.service, err
}

type projectFields struct {
	service     bool
	coupling    bool
	versionFive bool
}

func scanProjectFields(data []byte) (projectFields, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	fields := projectFields{}
	var couplingMembers uint8
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return fields, nil
		}
		if err != nil {
			return projectFields{}, err
		}
		path := strings.Split(strings.ToLower(string(decoder.StackPointer())), "/")
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if kind == jsontext.KindBeginArray && len(path) == 3 && path[1] == "expressservices" && length > MaxExpressServices {
			return projectFields{}, fmt.Errorf("express registry has more than %d services", MaxExpressServices)
		}
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		switch {
		case len(path) == 2 && path[1] == "version":
			value, err := decoder.ReadToken()
			if err != nil {
				return projectFields{}, err
			}
			if value.Kind() == jsontext.KindNumber {
				version, err := value.Int()
				fields.versionFive = fields.versionFive || err == nil && version == CouplingVersion
			}
		case len(path) == 2 && couplingMemberBit(path[1]) != 0:
			fields.coupling = true
			bit := couplingMemberBit(path[1])
			if couplingMembers&bit != 0 {
				return projectFields{}, errors.New("duplicate coupling member")
			}
			couplingMembers |= bit
			if err := scanCouplingMember(decoder, path[1]); err != nil {
				return projectFields{}, err
			}

		case len(path) == 2 && path[1] == "ordercontract":
			fields.service = true
			value, err := decoder.ReadToken()
			if err != nil {
				return projectFields{}, err
			}
			if value.Kind() != jsontext.KindString || value.String() != string(sim.ExpressOrderContract) {
				return projectFields{}, errors.New("order contract must be express-v1")
			}
		case len(path) == 2 && path[1] == "onboardpickups":
			fields.service = true
			value, err := decoder.ReadToken()
			if err != nil {
				return projectFields{}, err
			}
			if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
				return projectFields{}, errors.New("onboard pickups must be Boolean")
			}
		case len(path) == 2 && path[1] == "stationqueuespacing":
			fields.service = true
			value, err := decoder.ReadToken()
			if err != nil {
				return projectFields{}, err
			}
			if value.Kind() != jsontext.KindString || !validStationQueueSpacing(sim.StationQueueSpacing(value.String())) {
				return projectFields{}, errors.New("station queue spacing must be ordinary or compact-v1")
			}
		case serviceClassListPath(path):
			fields.service = true
			var classes sim.ClassSet
			if err := classes.UnmarshalJSONFrom(decoder); err != nil {
				return projectFields{}, err
			}
		case len(path) == 4 && path[1] == "fleet" && path[3] == "class":
			fields.service = true
			value, err := decoder.ReadToken()
			if err != nil {
				return projectFields{}, err
			}
			if value.Kind() != jsontext.KindString || value.String() == "" {
				return projectFields{}, errors.New("explicit fleet class needs nonempty text")
			}
			if _, known := sim.LookupVehicleClass(sim.VehicleClass(value.String())); !known {
				return projectFields{}, sim.ErrUnknownVehicleClass
			}
		case len(path) == 2 && path[1] == "expressservices":
			fields.service = true
			if decoder.PeekKind() != jsontext.KindBeginArray {
				return projectFields{}, errors.New("express registry must be an array")
			}
		}
	}
}

func serviceClassListPath(path []string) bool {
	if len(path) == 5 && path[1] == "network" && (path[2] == "stations" || path[2] == "lanes") {
		return path[4] == "vehicleclasses"
	}
	return len(path) == 7 && path[1] == "network" && path[2] == "stations" &&
		path[4] == "berths" && path[6] == "vehicleclasses"
}

func validateOnboardPickups(config Config) error {
	if config.OnboardPickups && (EffectiveSharedRidePartyLimit(config) <= 1 || EffectiveSharedRideMode(config) != sim.SharedRideDropOffs) {
		return errors.New("onboard pickups require shared ride party limit above one and drop-offs mode")
	}
	return nil
}
