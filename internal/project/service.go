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

// projectFieldScan carries the state of one scanProjectFields pass.
// couplingMembers holds the couplingMemberBit of each coupling member that
// the scan has read.
type projectFieldScan struct {
	decoder         *jsontext.Decoder
	fields          projectFields
	couplingMembers uint8
}

func scanProjectFields(data []byte) (projectFields, error) {
	scan := projectFieldScan{decoder: jsontext.NewDecoder(bytes.NewReader(data))}
	for {
		token, err := scan.decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return scan.fields, nil
		}
		if err != nil {
			return projectFields{}, err
		}
		if err := scan.token(token); err != nil {
			return projectFields{}, err
		}
	}
}

// token checks the bound of the express registry at each of its
// elements. When token is an object member name, it then checks the
// member at the path.
func (scan *projectFieldScan) token(token jsontext.Token) error {
	path := strings.Split(string(scan.decoder.StackPointer()), "/")
	kind, length := scan.decoder.StackIndex(scan.decoder.StackDepth())
	if kind == jsontext.KindBeginArray && len(path) == 3 && path[1] == "expressServices" && length > MaxExpressServices {
		return fmt.Errorf("express registry has more than %d services", MaxExpressServices)
	}
	if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
		return nil
	}
	if len(path) == 2 {
		return scan.topMember(path[1])
	}
	return scan.nestedMember(path)
}

// topMember checks the value of the top-level member name before the
// decoder reads it.
func (scan *projectFieldScan) topMember(name string) error {
	decoder := scan.decoder
	if bit := couplingMemberBit(name); bit != 0 {
		return scan.couplingMember(name, bit)
	}
	switch name {
	case "incidentContract":
		// An explicit null or empty marker is presence. The typed
		// check cannot see it.
		return scanContractMarker(decoder, string(sim.IncidentV1Contract), sim.ErrUnknownIncidentContract)
	case "faultContract":
		// An explicit null or empty marker is presence, as for the
		// incident marker.
		return scanContractMarker(decoder, string(FaultV1Contract), errUnknownFaultContract)
	case "faults", "emergencies":
		value, err := decoder.ReadValue()
		if err != nil {
			return err
		}
		return scanSettings(name, value)
	case "emergencyContract":
		// An explicit null or empty marker is presence, as for the
		// incident marker.
		return scanContractMarker(decoder, string(EmergencyV1Contract), errUnknownEmergencyContract)
	case "orderContract":
		scan.fields.service = true
		return scanContractMarker(decoder, string(sim.ExpressOrderContract), errors.New("order contract must be express-v1"))
	case "onboardPickups":
		scan.fields.service = true
		return scanOnboardPickups(decoder)
	case "stationQueueSpacing":
		scan.fields.service = true
		return scanStationQueueSpacing(decoder)
	case "expressServices":
		scan.fields.service = true
		if decoder.PeekKind() != jsontext.KindBeginArray {
			return errors.New("express registry must be an array")
		}
	}
	return nil
}

// couplingMember checks a coupling member. Each coupling member can occur
// once.
func (scan *projectFieldScan) couplingMember(name string, bit uint8) error {
	scan.fields.coupling = true
	if scan.couplingMembers&bit != 0 {
		return errors.New("duplicate coupling member")
	}
	scan.couplingMembers |= bit
	return scanCouplingMember(scan.decoder, name)
}

// nestedMember checks the vehicle class members below the top level.
func (scan *projectFieldScan) nestedMember(path []string) error {
	switch {
	case serviceClassListPath(path):
		scan.fields.service = true
		var classes sim.ClassSet
		return classes.UnmarshalJSONFrom(scan.decoder)
	case len(path) == 4 && path[1] == "fleet" && path[3] == "class":
		scan.fields.service = true
		value, err := scan.decoder.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindString || value.String() == "" {
			return errors.New("explicit fleet class needs nonempty text")
		}
		if _, known := sim.LookupVehicleClass(sim.VehicleClass(value.String())); !known {
			return sim.ErrUnknownVehicleClass
		}
	}
	return nil
}

// scanContractMarker reads the value of a contract marker. It returns
// refusal unless the value is the text want.
func scanContractMarker(decoder *jsontext.Decoder, want string, refusal error) error {
	value, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if value.Kind() != jsontext.KindString || value.String() != want {
		return refusal
	}
	return nil
}

func scanOnboardPickups(decoder *jsontext.Decoder) error {
	value, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
		return errors.New("onboard pickups must be Boolean")
	}
	return nil
}

func scanStationQueueSpacing(decoder *jsontext.Decoder) error {
	value, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if value.Kind() != jsontext.KindString || !ValidStationQueueSpacing(sim.StationQueueSpacing(value.String())) {
		return errors.New("station queue spacing must be ordinary or compact-v1")
	}
	return nil
}

func serviceClassListPath(path []string) bool {
	if len(path) == 5 && path[1] == "network" && (path[2] == "stations" || path[2] == "lanes") {
		return path[4] == "vehicleClasses"
	}
	return len(path) == 7 && path[1] == "network" && path[2] == "stations" &&
		path[4] == "berths" && path[6] == "vehicleClasses"
}

func validateOnboardPickups(config Config) error {
	if config.OnboardPickups && (EffectiveSharedRidePartyLimit(config) <= 1 || EffectiveSharedRideMode(config) != sim.SharedRideDropOffs) {
		return errors.New("onboard pickups require shared ride party limit above one and drop-offs mode")
	}
	return nil
}
