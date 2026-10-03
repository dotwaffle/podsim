package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// DecodeStreamJSONVersion applies the connection's negotiated field contract.
func DecodeStreamJSONVersion(data []byte, version int) (StreamEnvelope, error) {
	if version < 1 || version > StreamVersion || len(data) > MaxStreamJSON {
		return StreamEnvelope{}, errors.New("unsupported stream version or size")
	}
	if err := scanStreamBoardingMembers(data, version); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanStreamServiceMembers(data, version); err != nil {
		return StreamEnvelope{}, err
	}
	var envelope StreamEnvelope
	err := decodeStreamJSON(data, &envelope)
	return envelope, err
}

func scanStreamServiceMembers(data []byte, version int) error {
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
		name := strings.ToLower(token.String())
		switch name {
		case "projectversion", "vehicleclasses", "class", "sharingconsent", "service", "serviceid", "legacycohort", "legacypartysize":
		default:
			continue
		}
		if version < StreamVersion {
			return errors.New("legacy stream contains version 3 service fields")
		}
		if name == "vehicleclasses" {
			// ClassSet checks the array's shape and values during typed decoding.
			continue
		}
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		switch name {
		case "legacycohort", "legacypartysize":
			if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
				return errors.New("stream legacy marker must be Boolean")
			}
		case "projectversion":
			if value.Kind() != jsontext.KindNumber {
				return errors.New("stream project version must be an integer")
			}
			projectVersion, numberErr := value.Int()
			if numberErr != nil || projectVersion < 1 || projectVersion > project.ServiceVersion {
				return errors.New("invalid stream project version")
			}
		default:
			if value.Kind() != jsontext.KindString || value.String() == "" {
				return fmt.Errorf("stream %s needs nonempty text", name)
			}
			switch name {
			case "class":
				if _, ok := sim.LookupVehicleClass(sim.VehicleClass(value.String())); !ok {
					return sim.ErrUnknownVehicleClass
				}
			case "sharingconsent":
				consent := sim.SharingConsent(value.String())
				if consent != sim.PrivateConsent && consent != sim.SharedConsent && consent != sim.LegacyUnknownConsent {
					return errors.New("invalid stream sharing consent")
				}
			case "service":
				if service := sim.ServiceChoice(value.String()); service != sim.OnDemandService && service != sim.ExpressServiceChoice {
					return errors.New("invalid stream service")
				}
			case "serviceid":
				if len(value.String()) > 64 {
					return errors.New("stream service ID is too long")
				}
			}
		}
	}
}

// NewStreamAssemblerVersion verifies topology against a negotiated hello.
func NewStreamAssemblerVersion(topology TopologySnapshot, version int) (*StreamAssembler, error) {
	if version < 1 || version > StreamVersion {
		return nil, errors.New("unsupported stream version")
	}
	if err := validateStreamTopology(topology, version); err != nil {
		return nil, err
	}
	assembler, err := NewStreamAssembler(topology)
	if err != nil {
		return nil, err
	}
	assembler.version = version
	return assembler, nil
}

func validateStreamTopology(topology TopologySnapshot, version int) error {
	classes := false
	banks := false
	for _, lane := range topology.Network.Lanes {
		classes = classes || lane.VehicleClasses != 0
	}
	for _, station := range topology.Network.Stations {
		banks = banks || station.Banks != nil
		if version == 1 && station.Banks != nil {
			return errors.New("version 1 stream cannot contain Banks")
		}
		classes = classes || station.VehicleClasses != 0
		for _, berth := range station.Berths {
			classes = classes || berth.VehicleClasses != 0
		}
	}
	if version < StreamVersion {
		if topology.ProjectVersion != 0 || classes {
			return errors.New("legacy stream topology contains service metadata")
		}
		return nil
	}
	if topology.ProjectVersion < 1 || topology.ProjectVersion > project.ServiceVersion ||
		classes && topology.ProjectVersion < project.ServiceVersion || banks && topology.ProjectVersion < project.BankVersion ||
		topology.ProjectVersion == project.BankVersion && !banks {
		return errors.New("stream topology has incompatible project version")
	}
	return nil
}

func (a *StreamAssembler) serviceOrders(frame StreamFrame) error {
	if a.version < StreamVersion {
		return nil
	}
	for _, request := range frame.State.Simulation.Pending {
		if !validStreamOrder(request) || request.SharingConsent == sim.LegacyUnknownConsent {
			return errors.New("invalid pending service order")
		}
	}
	for _, vehicle := range frame.State.Simulation.Vehicles {
		profile, known := sim.LookupVehicleClass(vehicle.Pod.Class)
		if !known || sim.ValidateVehicleClassProfile(vehicle.Pod.Class) != nil {
			return errors.New("unsupported stream vehicle profile")
		}
		if profile.Class != sim.LegacyClass && a.topology.ProjectVersion < project.ServiceVersion {
			return errors.New("stream vehicle class needs project version 3")
		}
		if previous, recorded := a.classes[vehicle.Pod.ID]; recorded && previous != profile.Class {
			return errors.New("stream vehicle class changed within project")
		}
		if vehicle.LegacyCohort && (profile.Class != sim.LegacyClass || len(vehicle.Riders) == 0) {
			return errors.New("invalid closed legacy cohort")
		}
		active, seats := 0, 0
		for _, rider := range vehicle.Riders {
			if !validStreamOrder(rider) || (rider.SharingConsent == sim.LegacyUnknownConsent) != vehicle.LegacyCohort {
				return errors.New("invalid rider service order")
			}
			if rider.Completed || vehicle.LegacyCohort {
				continue
			}
			active++
			seats += rider.PartySize
			if rider.PartySize > profile.MaxNewPartySize || seats > profile.Seats || active > 1 && rider.SharingConsent != sim.SharedConsent {
				return errors.New("stream riders exceed class capacity or consent")
			}
		}
		if active > 1 {
			for _, rider := range vehicle.Riders {
				if !rider.Completed && rider.SharingConsent != sim.SharedConsent {
					return errors.New("stream pooled riders need explicit consent")
				}
			}
		}
	}
	return nil
}

func (a *StreamAssembler) rememberClasses(frame StreamFrame) error {
	if a.version < StreamVersion {
		return nil
	}
	next := make(map[string]sim.VehicleClass, len(a.classes))
	maps.Copy(next, a.classes)
	for _, vehicle := range frame.State.Simulation.Vehicles {
		profile, _ := sim.LookupVehicleClass(vehicle.Pod.Class)
		next[vehicle.Pod.ID] = profile.Class
	}
	if len(next) > project.MaxPods {
		return errors.New("stream project exceeds fleet identity limit")
	}
	a.classes = next
	return nil
}

func validStreamOrder(request sim.Request) bool {
	if request.PartySize < 1 || request.LegacyPartySize != (request.PartySize > sim.MaxNewPartySize) {
		return false
	}
	if request.SharingConsent != sim.PrivateConsent && request.SharingConsent != sim.SharedConsent && request.SharingConsent != sim.LegacyUnknownConsent {
		return false
	}
	// Current operating profiles have no express service certificate.
	return request.Service == sim.OnDemandService && request.ServiceID == "" && (!request.LegacyPartySize || request.SharingConsent != sim.SharedConsent)
}
