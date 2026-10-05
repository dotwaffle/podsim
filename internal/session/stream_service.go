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

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// DecodeStreamJSON validates one inflated envelope. The root contract
// markers of the envelope select its rules. The client checks that they
// are the markers of the hello, and ApplyStream checks that they are the
// markers of the frame.
func DecodeStreamJSON(data []byte) (StreamEnvelope, error) {
	var envelope StreamEnvelope
	err := decodeMarkedJSON(data, false, &envelope)
	return envelope, err
}

// decodeMarkedJSON decodes a stream envelope, or an HTTP state when
// httpState is true, into target. The root contract markers of the
// document select the rules of the scans. Each scan reads the tokens only,
// so it bounds the document before the typed decode makes values.
func decodeMarkedJSON(data []byte, httpState bool, target any) error {
	if len(data) > MaxStreamJSON {
		return errors.New("state JSON too large")
	}
	markers, err := scanRootMarkers(data)
	if err != nil {
		return err
	}
	// Without the coupling marker, scanStreamServiceMembers refuses each
	// coupling member.
	if markers.coupling != "" {
		if err := scanCouplingPublicJSON(data, httpState); err != nil {
			return err
		}
	}
	if err := scanContractMarkers(data, markers.order == sim.ExpressOrderContract, textRefused); err != nil {
		return err
	}
	if err := scanPackedOrders(data); err != nil {
		return err
	}
	if err := scanStreamBoardingMembers(data, markers); err != nil {
		return err
	}
	if err := scanStreamServiceMembers(data, markers); err != nil {
		return err
	}
	return decodePackedStreamJSON(data, target)
}

// scanRootMarkers bounds data with the limits of its root contract markers
// and returns the markers. The Express limits contain the other limits, so
// the first scan bounds the read of the markers without refusing a
// document that the exact scan accepts. The other scans check the values
// and the duplicates of the markers.
func scanRootMarkers(data []byte) (contractMarkers, error) {
	if err := prescanJSON(data, streamLimits(contractMarkers{order: sim.ExpressOrderContract})); err != nil {
		return contractMarkers{}, err
	}
	var header struct {
		OrderContract    sim.OrderContract    `json:"orderContract"`
		CouplingContract sim.CouplingContract `json:"couplingContract"`
	}
	if err := jsonv2.Unmarshal(data, &header, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false)); err != nil {
		return contractMarkers{}, err
	}
	markers := contractMarkers{order: header.OrderContract, coupling: header.CouplingContract}
	if markers.order != sim.ExpressOrderContract {
		if err := prescanJSON(data, streamLimits(markers)); err != nil {
			return markers, err
		}
	}
	return markers, nil
}

// scanStreamServiceMembers checks the service members of a stream
// document. Coupling members need the coupling marker. The order text is
// packed, so scanPackedOrders checks the length of the service IDs.
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
			}
		}
	}
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
