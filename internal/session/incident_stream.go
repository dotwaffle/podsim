package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// streamIncidentPaths are the paths of the stage 1 members of a stream
// envelope and an HTTP state, with "*" for each array index (section
// 11.5). The incident group key is a member of its own.
var streamIncidentPaths = func() map[string]bool {
	paths := map[string]bool{
		"/delta/groups/incident":                       true,
		"/delta/groups/incident/interrupted":           true,
		"/delta/groups/incident/interruptedPassengers": true,
		"/delta/groups/pending/*/legFrom":              true,
		"/delta/vehicles/*/riders/value/*/legFrom":     true,
		"/delta/vehicles/*/metadata/value/withdrawn":   true,
		"/delta/vehicles/*/metadata/value/operational": true,
	}
	for _, frame := range []string{"/full/state/simulation", "/frame/state/simulation"} {
		for _, member := range []string{"/interrupted", "/interruptedPassengers", "/vehicles/*/withdrawn", "/vehicles/*/operational", "/vehicles/*/riders/*/legFrom", "/pending/*/legFrom"} {
			paths[frame+member] = true
		}
	}
	return paths
}()

// The paths of streamIncidentPaths in the raw incident and pending
// groups, relative to the group. ApplyStream scans these groups again,
// because an envelope that a caller builds has no decoder scan.
var (
	incidentGroupPaths = groupIncidentPaths("incident")
	pendingGroupPaths  = groupIncidentPaths("pending")
)

// groupIncidentPaths returns the paths of streamIncidentPaths in delta
// group name, relative to the group.
func groupIncidentPaths(name string) map[string]bool {
	prefix := "/delta/groups/" + name + "/"
	paths := map[string]bool{}
	for path := range streamIncidentPaths {
		if member, ok := strings.CutPrefix(path, prefix); ok {
			paths["/"+member] = true
		}
	}
	return paths
}

// scanIncidentMembers refuses null and an empty text for each stage 1 member
// or the incident group. No encoder writes them, and the typed decode reads
// them as no member, so only this scan sees them.
func scanIncidentMembers(data []byte) error {
	return scanIncidentPaths(data, streamIncidentPaths)
}

// scanIncidentPaths is scanIncidentMembers for the members at paths.
func scanIncidentPaths(data []byte, paths map[string]bool) error {
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
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 || !paths[arrayPath(decoder)] {
			continue
		}
		kind = decoder.PeekKind()
		if kind == jsontext.KindNull {
			return fmt.Errorf("stream incident member %s is null", decoder.StackPointer())
		}
		if kind != jsontext.KindString {
			continue
		}
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if value.String() == "" {
			return fmt.Errorf("stream incident member %s is empty", decoder.StackPointer())
		}
	}
}

// checkIncidentFrame checks the typed stage 1 members of a frame. It refuses
// a negative counter, an unknown hold bit, an unknown purpose, and a purpose
// without a hold (section 11.6).
func checkIncidentFrame(frame SimulationFrame) error {
	if frame.Interrupted < 0 || frame.InterruptedPassengers < 0 {
		return errors.New("negative interrupted counter")
	}
	for _, vehicle := range frame.Vehicles {
		if err := validateVehicleIncident(vehicle); err != nil {
			return fmt.Errorf("vehicle %q: %w", vehicle.Pod.ID, err)
		}
	}
	return nil
}

// validateVehicleIncident checks the service holds and the operational
// purpose of a vehicle. A purpose always has an owner hold (W5).
func validateVehicleIncident(vehicle VehicleFrame) error {
	if vehicle.Withdrawn&^knownIncidentHolds != 0 {
		return errors.New("unknown hold bit")
	}
	switch vehicle.Operational {
	case "", sim.OperationalEmergencyUnload, sim.OperationalRefuge, sim.OperationalEmptyRecovery:
	default:
		return errors.New("unknown operational purpose")
	}
	if vehicle.Operational != "" && vehicle.Withdrawn == 0 {
		return errors.New("operational purpose without a hold")
	}
	return nil
}
