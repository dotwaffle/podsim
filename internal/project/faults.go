package project

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
)

// FaultContract selects the fault operations of the incident suspension
// contract. It requires the incident marker.
type FaultContract string

// FaultV1Contract permits the fault commands and the faults settings.
const FaultV1Contract FaultContract = "fault-v1"

// These limits bound the faults settings. The simulation has the same
// limits for the evacuation delay and for a fault duration.
const (
	maxFaultEvacuationSeconds = 3_600
	defaultEvacuationSeconds  = 300
	maxFaultSeconds           = 86_400
	minDebrisMeters           = 0.5
	maxDebrisMeters           = 50
)

// FaultConfig holds the faults settings of a project with the fault
// marker. Each member is optional. A nil member has its default:
// EvacuationSeconds 300, PerHour 0, DebrisShare 0, DebrisMeters 2, and no
// Duration.
type FaultConfig struct {
	// EvacuationSeconds is the time from fault onset to evacuation, from 0
	// to 3,600 seconds.
	EvacuationSeconds *int `json:"evacuationSeconds,omitzero"`
	// PerHour is the scenario fault rate. It must be 0, because the
	// scenario sampler is not available yet.
	PerHour *float64 `json:"perHour,omitzero"`
	// DebrisShare is the share of scenario faults that are debris, from 0
	// to 1.
	DebrisShare *float64 `json:"debrisShare,omitzero"`
	// DebrisMeters is the length of scenario debris, from 0.5 to 50
	// meters.
	DebrisMeters *float64 `json:"debrisMeters,omitzero"`
	// Duration is the duration distribution of scenario faults.
	Duration *FaultDuration `json:"duration,omitzero"`
}

// FaultDuration is a duration distribution of scenario faults. Kind is
// "fixed", "uniform", or "exponential". A fixed duration has Seconds. A
// uniform duration has MinSeconds and MaxSeconds. An exponential duration
// has MinSeconds, MaxSeconds, and MeanSeconds. Each value is from 1 to
// 86,400 seconds, MinSeconds is at most MaxSeconds, and MeanSeconds is
// from MinSeconds to MaxSeconds. Every other member is absent.
type FaultDuration struct {
	Kind        string `json:"kind"`
	Seconds     *int   `json:"seconds,omitzero"`
	MinSeconds  *int   `json:"minSeconds,omitzero"`
	MaxSeconds  *int   `json:"maxSeconds,omitzero"`
	MeanSeconds *int   `json:"meanSeconds,omitzero"`
}

// errUnknownFaultContract means that the fault marker is not
// FaultV1Contract.
var errUnknownFaultContract = errors.New("fault contract must be fault-v1")

// widestFaults has the longest canonical encoding of the faults settings
// that Validate accepts. Validate measures a project with the fault marker
// with it, as it measures each project with widestDemand. JSON writes a
// number from 1e-6 to 1e21 in decimal form, so the widest debris share is
// a number just above 1e-6 with 17 significant digits, which takes 24
// bytes. The widest debris length takes 18 bytes. The exponential kind
// has the longest name and the most members.
var widestFaults = FaultConfig{
	EvacuationSeconds: new(maxFaultEvacuationSeconds),
	PerHour:           new(0.0),
	DebrisShare:       new(1.0000000000000002e-06),
	DebrisMeters:      new(0.5000000000000001),
	Duration: &FaultDuration{Kind: "exponential", MinSeconds: new(maxFaultSeconds),
		MaxSeconds: new(maxFaultSeconds), MeanSeconds: new(maxFaultSeconds)},
}

// validateFaultContract checks the fault marker and the faults settings.
// The marker needs the incident marker and the settings. The settings
// need the marker.
func validateFaultContract(config Config) error {
	switch {
	case config.FaultContract == "" && config.Faults != nil:
		return errors.New("faults require faultContract fault-v1")
	case config.FaultContract == "":
		return nil
	case config.FaultContract != FaultV1Contract:
		return errUnknownFaultContract
	case config.IncidentContract == "":
		return errors.New("faultContract requires incidentContract incident-v1")
	case config.Faults == nil:
		return errors.New("faultContract requires faults")
	}
	return validateFaults(*config.Faults)
}

// validateFaults checks the range of each faults member that is present.
func validateFaults(faults FaultConfig) error {
	if value := faults.EvacuationSeconds; value != nil && (*value < 0 || *value > maxFaultEvacuationSeconds) {
		return fmt.Errorf("faults evacuationSeconds must be 0 to %d", maxFaultEvacuationSeconds)
	}
	// A rate above 0 needs the scenario sampler. NaN is not 0.
	if value := faults.PerHour; value != nil && *value != 0 {
		return errors.New("faults perHour must be 0")
	}
	if value := faults.DebrisShare; value != nil && !(*value >= 0 && *value <= 1) {
		return errors.New("faults debrisShare must be 0 to 1")
	}
	if value := faults.DebrisMeters; value != nil && !(*value >= minDebrisMeters && *value <= maxDebrisMeters) {
		return fmt.Errorf("faults debrisMeters must be %g to %d", minDebrisMeters, maxDebrisMeters)
	}
	if faults.Duration != nil {
		return validateFaultDuration(*faults.Duration)
	}
	return nil
}

// validateFaultDuration checks that the members of duration match its
// kind, and that each value is in range.
func validateFaultDuration(duration FaultDuration) error {
	var need, forbid []*int
	switch duration.Kind {
	case "fixed":
		need, forbid = []*int{duration.Seconds}, []*int{duration.MinSeconds, duration.MaxSeconds, duration.MeanSeconds}
	case "uniform":
		need, forbid = []*int{duration.MinSeconds, duration.MaxSeconds}, []*int{duration.Seconds, duration.MeanSeconds}
	case "exponential":
		need, forbid = []*int{duration.MinSeconds, duration.MaxSeconds, duration.MeanSeconds}, []*int{duration.Seconds}
	default:
		return errors.New(`faults duration kind must be "fixed", "uniform", or "exponential"`)
	}
	for _, value := range forbid {
		if value != nil {
			return fmt.Errorf("faults duration kind %s has a member of another kind", duration.Kind)
		}
	}
	for _, value := range need {
		if value == nil || *value < 1 || *value > maxFaultSeconds {
			return fmt.Errorf("faults duration kind %s needs each of its values from 1 to %d seconds", duration.Kind, maxFaultSeconds)
		}
	}
	if duration.Kind == "fixed" {
		return nil
	}
	low, high := *duration.MinSeconds, *duration.MaxSeconds
	if low > high {
		return errors.New("faults duration minSeconds must be at most maxSeconds")
	}
	if mean := duration.MeanSeconds; mean != nil && (*mean < low || *mean > high) {
		return errors.New("faults duration meanSeconds must be from minSeconds to maxSeconds")
	}
	return nil
}

// scanFaults checks the raw faults value of a project. The value must be
// an object, and no member at any depth can be null. The typed decode
// reads null as an absent member, so it cannot see these errors.
func scanFaults(value jsontext.Value) error {
	if value.Kind() != '{' {
		return errors.New("faults must be an object")
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(value))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if token.Kind() == 'n' {
			return errors.New("faults members must not be null")
		}
	}
}

// cloneFaults returns a copy of faults that shares no memory with it.
func cloneFaults(faults *FaultConfig) *FaultConfig {
	if faults == nil {
		return nil
	}
	clone := FaultConfig{
		EvacuationSeconds: clonePointer(faults.EvacuationSeconds), PerHour: clonePointer(faults.PerHour),
		DebrisShare: clonePointer(faults.DebrisShare), DebrisMeters: clonePointer(faults.DebrisMeters),
	}
	if duration := faults.Duration; duration != nil {
		clone.Duration = &FaultDuration{Kind: duration.Kind, Seconds: clonePointer(duration.Seconds),
			MinSeconds: clonePointer(duration.MinSeconds), MaxSeconds: clonePointer(duration.MaxSeconds),
			MeanSeconds: clonePointer(duration.MeanSeconds)}
	}
	return &clone
}

// clonePointer returns a new pointer to the value of pointer, or nil.
func clonePointer[T any](pointer *T) *T {
	if pointer == nil {
		return nil
	}
	return new(*pointer)
}

// evacuationSeconds returns the evacuation delay of a project with the
// fault marker, with the default for an absent member.
func evacuationSeconds(faults *FaultConfig) int {
	if faults == nil || faults.EvacuationSeconds == nil {
		return defaultEvacuationSeconds
	}
	return *faults.EvacuationSeconds
}
