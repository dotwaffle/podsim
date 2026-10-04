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

// MaxExpressServices bounds the directed service registry of a project.
const MaxExpressServices = 300

// validateVersion accepts CurrentVersion and a known order contract.
// The earlier project versions 2 to 5 get a clear refusal, because they
// do not migrate.
func validateVersion(config Config) error {
	if config.Version >= 2 && config.Version <= 5 {
		return fmt.Errorf("project version %d is not supported: use version %d with feature markers", config.Version, CurrentVersion)
	}
	if config.Version != CurrentVersion {
		return fmt.Errorf("project version must be %d", CurrentVersion)
	}
	return sim.ValidateOrderContract(config.OrderContract)
}

// scanProjectService checks new field presence and shape before typed allocation.
func scanProjectService(data []byte) (bool, error) {
	fields, err := scanProjectFields(data)
	return fields.service, err
}

type projectFields struct {
	service  bool
	coupling bool
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
