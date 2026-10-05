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
	if version < FoundationStreamVersion || version > StreamVersion || len(data) > MaxStreamJSON {
		return StreamEnvelope{}, errors.New("unsupported stream version or size")
	}
	markers := streamVersionMarkers(version, "")
	// Bound the document before the token scans and the typed decode read
	// it. Hello 5 selects the coupling marker, and the document carries its
	// order marker.
	if markers.coupling != "" {
		packed, err := scanCouplingOrderContract(data)
		if err != nil {
			return StreamEnvelope{}, err
		}
		if packed {
			markers.order = sim.ExpressOrderContract
		}
	} else if err := prescanJSON(data, streamLimits(markers)); err != nil {
		return StreamEnvelope{}, err
	}
	return decodeStreamEnvelope(data, markers)
}

// streamVersionMarkers returns the markers that a supported hello version
// selects. Hello 5 selects the coupling marker with either order marker, so
// order gives the order marker of its document or topology.
func streamVersionMarkers(version int, order sim.OrderContract) contractMarkers {
	switch version {
	case CouplingStreamVersion:
		return contractMarkers{order: order, coupling: sim.CompactPairV1CouplingContract}
	case ExpressStreamVersion:
		return contractMarkers{order: sim.ExpressOrderContract}
	default:
		return contractMarkers{}
	}
}

// decodeStreamEnvelope decodes an envelope under the rules of markers. The
// caller bounds data with the limits of markers first.
func decodeStreamEnvelope(data []byte, markers contractMarkers) (StreamEnvelope, error) {
	packed := markers.order == sim.ExpressOrderContract
	if markers.coupling != "" {
		if err := scanCouplingStreamJSON(data); err != nil {
			return StreamEnvelope{}, err
		}
	}
	if err := scanContractMarkers(data, packed, packed); err != nil {
		return StreamEnvelope{}, err
	}
	if packed {
		if err := scanPackedOrders(data); err != nil {
			return StreamEnvelope{}, err
		}
	}
	if err := scanStreamBoardingMembers(data, markers); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanStreamServiceMembers(data, markers); err != nil {
		return StreamEnvelope{}, err
	}
	var envelope StreamEnvelope
	var err error
	if packed {
		err = jsonv2.Unmarshal(data, &envelope, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true), packedDecodeOptions())
	} else {
		err = decodeStreamJSON(data, &envelope)
	}
	return envelope, err
}

// unpackedStreamLimits bound hello 3 documents. The assembler accepts at
// most maxSavedTrips pending orders, and at most sim.MaxSharedRideParties
// riders and boarding records for each vehicle.
func unpackedStreamLimits() jsonLimits {
	return streamLimits(contractMarkers{})
}

// PrescanStateFrameJSON bounds the document of the plain HTTP state
// endpoint before a client reads it. It reads the tokens only and makes no
// values.
func PrescanStateFrameJSON(data []byte) error {
	return prescanJSON(data, stateFrameLimits())
}

// stateFrameLimits bound the state frame that the plain HTTP state endpoint
// sends for family 3. The frame is the full frame of that stream family, so
// it gets its limits without the "/full/state" prefix. Plain JSON decoding
// replaces invalid UTF-8, so the scan accepts it.
func stateFrameLimits() jsonLimits {
	stream := unpackedStreamLimits()
	limits := stream
	limits.allowInvalidUTF8 = true
	limits.arrays = map[string]int64{}
	for path, bound := range stream.arrays {
		if rest, found := strings.CutPrefix(path, "/full/state/"); found {
			limits.arrays["/"+rest] = bound
		}
	}
	// The frame has the complete route of each vehicle, not a route window.
	// The simulation bounds only saved routes, so a live route has the
	// general element limit.
	limits.arrays["/simulation/vehicles/*/routeLaneIDs"] = limits.elements
	return limits
}

// scanStreamServiceMembers checks the service members of a stream
// document. Coupling members need the coupling marker. The Express marker
// packs the order text, so scanPackedOrders checks the length of its
// service IDs.
func scanStreamServiceMembers(data []byte, markers contractMarkers) error {
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
		if couplingMember(token.String()) || token.String() == "couplingID" {
			if markers.coupling == "" {
				return errors.New("current stream family contains coupling fields")
			}
			continue
		}
		name := token.String()
		switch name {
		case "projectVersion", "vehicleClasses", "class", "sharingConsent", "service", "serviceID":
		default:
			continue
		}
		if name == "vehicleClasses" {
			// ClassSet checks the array's shape and values during typed decoding.
			continue
		}
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		switch name {
		case "projectVersion":
			if value.Kind() != jsontext.KindNumber {
				return errors.New("stream project version must be an integer")
			}
			projectVersion, numberErr := value.Int()
			if numberErr != nil || projectVersion != project.CurrentVersion {
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
			case "sharingConsent":
				consent := sim.SharingConsent(value.String())
				if consent != sim.PrivateConsent && consent != sim.SharedConsent {
					return errors.New("invalid stream sharing consent")
				}
			case "service":
				if service := sim.ServiceChoice(value.String()); service != sim.OnDemandService && service != sim.ExpressServiceChoice {
					return errors.New("invalid stream service")
				}
			case "serviceID":
				if len(value.String()) > 64 && markers.order != sim.ExpressOrderContract {
					return errors.New("stream service ID is too long")
				}
			}
		}
	}
}

// NewStreamAssemblerVersion verifies topology against a negotiated hello.
// Hello 5 takes the order marker of the topology.
func NewStreamAssemblerVersion(topology TopologySnapshot, version int) (*StreamAssembler, error) {
	if version < FoundationStreamVersion || version > StreamVersion {
		return nil, errors.New("unsupported stream version")
	}
	if err := validateStreamTopology(topology, streamVersionMarkers(version, topology.OrderContract)); err != nil {
		return nil, err
	}
	return NewStreamAssembler(topology)
}

// checkTopologyProjectVersion refuses a topology of any project version
// other than the current one, including a missing version. Every decoded
// topology and every assembler checks it, with or without contract markers.
func checkTopologyProjectVersion(topology TopologySnapshot) error {
	if topology.ProjectVersion != project.CurrentVersion {
		return errors.New("stream topology has an unsupported project version")
	}
	return nil
}

// validateStreamTopology checks that the contract markers of topology are
// markers: the coupling marker with either order marker, the Express marker
// alone, or neither marker.
func validateStreamTopology(topology TopologySnapshot, markers contractMarkers) error {
	if err := checkTopologyProjectVersion(topology); err != nil {
		return err
	}
	if markers.coupling != "" {
		if topology.CouplingContract != markers.coupling {
			return errors.New("coupling topology needs the coupling contract")
		}
		if err := sim.ValidateCouplingGeometry(sim.CouplingGeometryInput{Contract: topology.CouplingContract,
			Network: topology.Network, Sites: topology.CouplingSites, Corridors: topology.CouplingCorridors}); err != nil {
			return err
		}
		if topology.OrderContract != markers.order {
			return errors.New("coupling topology has another order contract")
		}
		if topology.OrderContract == "" && len(topology.ExpressServices) != 0 {
			return errors.New("unmarked coupling topology contains Express services")
		}
		return sim.ValidateExpressServicesWithOrderContract(topology.Network, topology.ExpressServices, topology.OrderContract)
	}
	if hasCouplingTopology(topology) {
		return errors.New("coupling topology requires a qualified stream family")
	}
	if markers.order == sim.ExpressOrderContract {
		if topology.OrderContract != sim.ExpressOrderContract {
			return errors.New("express topology needs the Express contract")
		}
		return sim.ValidateExpressServicesWithOrderContract(topology.Network, topology.ExpressServices, topology.OrderContract)
	}
	if topology.OrderContract != "" || len(topology.ExpressServices) != 0 {
		return errors.New("legacy topology contains Express metadata")
	}
	return nil
}

func (a *StreamAssembler) serviceOrders(frame StreamFrame) error {
	if a.topology.OrderContract == sim.ExpressOrderContract {
		return a.expressOrders(frame)
	}
	for _, request := range frame.State.Simulation.Pending {
		if !validStreamOrder(request) {
			return errors.New("invalid pending service order")
		}
	}
	for _, vehicle := range frame.State.Simulation.Vehicles {
		profile, known := sim.LookupVehicleClassWithOrderContract(vehicle.Pod.Class, a.topology.OrderContract)
		if !known || sim.ValidateVehicleClassProfile(vehicle.Pod.Class) != nil {
			return errors.New("unsupported stream vehicle profile")
		}
		if previous, recorded := a.classes[vehicle.Pod.ID]; recorded && previous != profile.Class {
			return errors.New("stream vehicle class changed within project")
		}
		active, seats := 0, 0
		for _, rider := range vehicle.Riders {
			if !validStreamOrder(rider) {
				return errors.New("invalid rider service order")
			}
			if rider.Completed {
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
	if request.PartySize < 1 || request.PartySize > sim.MaxNewPartySize {
		return false
	}
	if request.SharingConsent != sim.PrivateConsent && request.SharingConsent != sim.SharedConsent {
		return false
	}
	// Current operating profiles have no express service certificate.
	return request.Service == sim.OnDemandService && request.ServiceID == ""
}
