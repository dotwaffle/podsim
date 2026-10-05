package sim

import (
	"errors"
	"fmt"
)

// IncidentTestOperation is one stage 1 operation of IncidentForTest.
type IncidentTestOperation struct {
	// Kind is "withdraw", "restore", "destination", "unload", "resume", or
	// "evacuate".
	Kind string
	// Hold is the hold of withdraw and restore, and the owner of
	// destination and unload: 1 for a fault, 2 for an emergency.
	Hold uint8
	// Purpose, Station, and Berth are the target of destination: 1 for an
	// emergency unload, 2 for a refuge, 3 for an empty recovery.
	Purpose        uint8
	Station, Berth string
	// Interrupt marks the riders of destination and unload, by index in
	// Riders.
	Interrupt uint32
}

// IncidentForTest runs one stage 1 operation on pod podID, and then the
// monitor once: withdrawService, restoreService,
// setOperationalDestination, startOperationalUnload, resumeFromRefuge, or
// evacuate. Each operation checks its preconditions, and a refusal changes
// nothing.
//
// It is a test entry (incident contract, section 13). The session tests
// use it to save each stage 1 state. No production code calls it. It
// refuses a simulation without the incident marker and a call during a
// dispatch pass.
func (s *Simulation) IncidentForTest(podID string, operation IncidentTestOperation) error {
	if s.incidentContract != IncidentV1Contract {
		return errors.New("an incident operation needs the incident contract")
	}
	if s.pass != nil && s.pass.active {
		return errors.New("an incident operation during a dispatch pass")
	}
	v := s.findVehicle(podID)
	if v == nil {
		return fmt.Errorf("pod %s does not exist", podID)
	}
	hold := serviceHold(operation.Hold)
	var err error
	switch operation.Kind {
	case "withdraw":
		err = s.withdrawService(v, hold)
	case "restore":
		err = s.restoreService(v, hold)
	case "destination":
		err = s.setOperationalDestination(v, operationalTarget{
			purpose: opPurpose(operation.Purpose), owner: hold, interrupt: operation.Interrupt,
			station: operation.Station, berth: operation.Berth,
		})
	case "unload":
		err = s.startOperationalUnload(v, hold, operation.Interrupt)
	case "resume":
		err = s.resumeFromRefuge(v)
	case "evacuate":
		err = s.evacuate(v)
	default:
		return fmt.Errorf("unknown incident operation %q", operation.Kind)
	}
	if err != nil {
		return err
	}
	s.observe()
	return nil
}
