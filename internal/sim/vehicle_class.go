package sim

import (
	"errors"
	"fmt"
)

// VehicleClass selects immutable capacity and physical profile metadata.
type VehicleClass string

// Vehicle classes separate the legacy model from authored passenger capacity.
const (
	LegacyClass  VehicleClass = "legacy"
	CompactClass VehicleClass = "compact"
	GroupClass   VehicleClass = "group"
	ExpressClass VehicleClass = "express"
)

// VehicleClassSpec describes capacity independently of physical approval.
// An unsupported profile has no approved body length.
type VehicleClassSpec struct {
	Class             VehicleClass
	Seats             int
	MaxNewPartySize   int
	BodyLengthMeters  float64
	PhysicalSupported bool
}

var (
	// ErrUnknownVehicleClass means the class has no registry entry.
	ErrUnknownVehicleClass = errors.New("unknown vehicle class")
	// ErrUnsupportedVehicleProfile means physical operation is not approved.
	ErrUnsupportedVehicleProfile = errors.New("vehicle physical profile is not supported")
)

// LookupVehicleClass returns an independent registry value. An omitted class
// selects legacy. Legacy seats preserve singleton sharing, not group admission.
func LookupVehicleClass(class VehicleClass) (VehicleClassSpec, bool) {
	switch class {
	case "", LegacyClass:
		return VehicleClassSpec{LegacyClass, 8, 1, 4, true}, true
	case CompactClass:
		return VehicleClassSpec{CompactClass, 4, 4, 4, true}, true
	case GroupClass:
		return VehicleClassSpec{GroupClass, 8, 8, 0, false}, true
	case ExpressClass:
		return VehicleClassSpec{ExpressClass, 20, 8, 0, false}, true
	default:
		return VehicleClassSpec{}, false
	}
}

// ValidateVehicleClassProfile checks physical approval for placement, startup,
// and restore. Logical capacity checks do not replace this check.
func ValidateVehicleClassProfile(class VehicleClass) error {
	profile, ok := LookupVehicleClass(class)
	if !ok {
		return ErrUnknownVehicleClass
	}
	if !profile.PhysicalSupported {
		return fmt.Errorf("class %s: %w", profile.Class, ErrUnsupportedVehicleProfile)
	}
	return nil
}
