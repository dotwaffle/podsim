package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
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
	if version == CouplingStreamVersion {
		return decodeCouplingStreamJSON(data)
	}
	// Bound the document before the token scans and the typed decode read it.
	limits := unpackedStreamLimits()
	if version == ExpressStreamVersion {
		limits = expressStreamLimits()
	}
	if err := prescanJSON(data, limits); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanContractMarkers(data, version == ExpressStreamVersion, version == ExpressStreamVersion); err != nil {
		return StreamEnvelope{}, err
	}
	if version == ExpressStreamVersion {
		if err := scanPackedOrders(data); err != nil {
			return StreamEnvelope{}, err
		}
	}
	if err := scanStreamBoardingMembers(data, version); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanStreamServiceMembers(data, version); err != nil {
		return StreamEnvelope{}, err
	}
	var envelope StreamEnvelope
	var err error
	if version == ExpressStreamVersion {
		err = jsonv2.Unmarshal(data, &envelope, json.DefaultOptionsV1(), jsonv2.RejectUnknownMembers(true), packedDecodeOptions())
	} else {
		err = decodeStreamJSON(data, &envelope)
	}
	return envelope, err
}

// unpackedStreamLimits bound stream families 1 to 3 and unpacked coupling
// documents. They narrow the Express limits to the order bounds of the
// unpacked contract. The assembler accepts at most maxSavedTrips pending
// orders, and at most sim.MaxSharedRideParties riders and boarding records
// for each vehicle. Earlier encoders had the same or smaller caps.
func unpackedStreamLimits() jsonLimits {
	limits := expressStreamLimits()
	for _, prefix := range []string{"/full", "/frame"} {
		limits.arrays[prefix+"/state/simulation/Pending"] = maxSavedTrips
		limits.arrays[prefix+"/state/simulation/Vehicles/*/Riders"] = sim.MaxSharedRideParties
		limits.arrays[prefix+"/state/simulation/Vehicles/*/Boardings"] = sim.MaxSharedRideParties
	}
	limits.arrays["/delta/groups/pending"] = maxSavedTrips
	limits.arrays["/delta/vehicles/*/riders/value"] = sim.MaxSharedRideParties
	limits.arrays["/delta/vehicles/*/boardings/value"] = sim.MaxSharedRideParties
	return limits
}

// PrescanStateFrameJSON bounds the document of the plain HTTP state
// endpoint before a client reads it. It reads the tokens only and makes no
// values.
func PrescanStateFrameJSON(data []byte) error {
	return prescanJSON(data, stateFrameLimits())
}

// stateFrameLimits bound the state frame that the plain HTTP state endpoint
// sends for families 1 to 3. The frame is the full frame of those stream
// families, so it gets their limits without the "/full/state" prefix.
// Servers before the topology endpoint sent the network in the state, so
// the network gets the topology limits. Plain JSON decoding replaces
// invalid UTF-8, so the scan accepts it.
func stateFrameLimits() jsonLimits {
	stream := unpackedStreamLimits()
	limits := stream
	limits.allowInvalidUTF8 = true
	limits.arrays = map[string]int64{}
	for path, bound := range stream.arrays {
		for _, prefix := range []string{"/full/state/", "/topology/"} {
			if rest, found := strings.CutPrefix(path, prefix); found {
				limits.arrays["/"+rest] = bound
			}
		}
	}
	// The frame has the complete route of each vehicle, not a route window.
	// The simulation bounds only saved routes, so a live route has the
	// general element limit.
	limits.arrays["/simulation/Vehicles/*/RouteLaneIDs"] = limits.elements
	return limits
}

func scanStreamServiceMembers(data []byte, version int) error {
	return scanStreamServiceMembersContract(data, version, false)
}

func scanStreamServiceMembersContract(data []byte, version int, coupling bool) error {
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
		if couplingMember(token.String()) || strings.EqualFold(token.String(), "couplingID") {
			if !coupling {
				return errors.New("current stream family contains coupling fields")
			}
			continue
		}
		name := strings.ToLower(token.String())
		switch name {
		case "projectversion", "vehicleclasses", "class", "sharingconsent", "service", "serviceid", "legacycohort", "legacypartysize":
		default:
			continue
		}
		if version < FoundationStreamVersion {
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
			maximum := maxStreamProjectVersion(version)
			if coupling {
				maximum = project.CouplingVersion
			}
			if numberErr != nil || projectVersion < 1 || projectVersion > int64(maximum) {
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
				if len(value.String()) > 64 && version != ExpressStreamVersion {
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
	if version == CouplingStreamVersion {
		if topology.ProjectVersion != project.CouplingVersion || topology.CouplingContract != sim.CompactPairV1CouplingContract {
			return errors.New("coupling topology needs project 5 and contract")
		}
		if err := sim.ValidateCouplingGeometry(sim.CouplingGeometryInput{Contract: topology.CouplingContract,
			Network: topology.Network, Sites: topology.CouplingSites, Corridors: topology.CouplingCorridors}); err != nil {
			return err
		}
		if topology.OrderContract == "" && len(topology.ExpressServices) != 0 {
			return errors.New("unmarked coupling topology contains Express services")
		}
		return sim.ValidateExpressServicesWithOrderContract(topology.Network, topology.ExpressServices, topology.OrderContract)
	}
	if hasCouplingTopology(topology) {
		return errors.New("coupling topology requires a qualified stream family")
	}
	if version == ExpressStreamVersion {
		if topology.ProjectVersion != project.ExpressVersion || topology.OrderContract != sim.ExpressOrderContract {
			return errors.New("express topology needs project 4 and contract")
		}
		return sim.ValidateExpressServicesWithOrderContract(topology.Network, topology.ExpressServices, topology.OrderContract)
	}
	if topology.OrderContract != "" || len(topology.ExpressServices) != 0 {
		return errors.New("legacy topology contains Express metadata")
	}
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
	if version < FoundationStreamVersion {
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
	if a.topology.OrderContract == sim.ExpressOrderContract {
		return a.expressOrders(frame)
	}
	if a.version < FoundationStreamVersion {
		return nil
	}
	for _, request := range frame.State.Simulation.Pending {
		if !validStreamOrder(request) || request.SharingConsent == sim.LegacyUnknownConsent {
			return errors.New("invalid pending service order")
		}
	}
	for _, vehicle := range frame.State.Simulation.Vehicles {
		profile, known := sim.LookupVehicleClassWithOrderContract(vehicle.Pod.Class, a.topology.OrderContract)
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
	if a.version < FoundationStreamVersion {
		return nil
	}
	next := make(map[string]sim.VehicleClass, len(a.classes))
	maps.Copy(next, a.classes)
	for _, vehicle := range frame.State.Simulation.Vehicles {
		profile, _ := sim.LookupVehicleClassWithOrderContract(vehicle.Pod.Class, a.topology.OrderContract)
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
