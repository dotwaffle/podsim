package ride

import (
	"cmp"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/session"
)

// PassengerPhase is a step of the passenger flow of the page.
type PassengerPhase string

// The phases of the passenger flow. Arrived and Stopped are the last
// phases. The page leaves them with End.
const (
	// NoTrip means that the page follows no order.
	NoTrip PassengerPhase = ""
	// Requested means that the page sent the trip and waits for the reply,
	// or for the first state with the order.
	Requested PassengerPhase = "requested"
	// Waiting means that the party waits at Station. PodID is the pod that
	// will pick it up, or empty.
	Waiting PassengerPhase = "waiting"
	// Riding means that the party is aboard PodID.
	Riding PassengerPhase = "riding"
	// Arrived means that the party left PodID at Station, its destination.
	Arrived PassengerPhase = "arrived"
	// Stopped means that the page cannot follow the order. Message tells why.
	Stopped PassengerPhase = "stopped"
)

// The messages of a stopped passenger flow.
const (
	sessionChangedMessage = "The session changed, so the page cannot follow the order."
	orderGoneMessage      = "The order is not in the session. A reset, a rewind, a project apply, or an incident can remove it."
)

// Passenger is the state of the passenger flow: one order that the page sent.
// Station fields hold station IDs.
type Passenger struct {
	Phase       PassengerPhase `json:"phase"`
	OrderID     int            `json:"orderID,omitzero"`
	Origin      string         `json:"origin,omitempty"`
	Destination string         `json:"destination,omitempty"`
	// Station is the station of the platform camera: the station where the
	// party waits, or its destination after it arrives.
	Station string `json:"station,omitempty"`
	PodID   string `json:"podID,omitempty"`
	Message string `json:"message,omitempty"`
	// session is the session of the order. revision is the state revision of
	// the reply that accepted the order. A state before that revision can
	// lack the order.
	session  remote.SessionID
	revision uint64
}

// Request starts the flow for a trip from origin to destination that the
// page sent in the session of state.
func Request(state session.State, origin, destination string) Passenger {
	return Passenger{Phase: Requested, Origin: origin, Destination: destination, Station: origin, session: remote.SessionOf(state)}
}

// Reply records the result of the trip command. An error stops the flow.
func (p Passenger) Reply(reply session.Reply, err error) Passenger {
	if p.Phase != Requested || p.OrderID != 0 {
		return p
	}
	switch {
	case err != nil:
		return p.stop(err.Error())
	case reply.Error != "":
		return p.stop(reply.Error)
	}
	p.OrderID, p.revision = reply.OrderID, reply.Revision
	return p
}

// Step returns the flow after state. It finds the order in the pending
// orders first, then aboard a pod, and then in the completed riders, because
// a party that changes pods has a completed record on its first pod.
func (p Passenger) Step(state session.State) Passenger {
	switch p.Phase {
	case Requested, Waiting, Riding:
	default:
		return p
	}
	if remote.SessionOf(state) != p.session {
		return p.stop(sessionChangedMessage)
	}
	if p.OrderID == 0 {
		return p
	}
	simulation := state.Simulation
	for _, request := range simulation.Pending {
		if request.ID == p.OrderID {
			return p.at(Waiting, cmp.Or(request.LegFrom, request.From), request.PodID)
		}
	}
	// arrival is the destination and the pod of a completed record.
	var arrival struct{ station, podID string }
	for i := range simulation.Vehicles {
		vehicle := &simulation.Vehicles[i]
		for j := range vehicle.Riders {
			rider := &vehicle.Riders[j]
			switch {
			case rider.ID != p.OrderID:
			case !rider.Completed && rider.BoardedTick > 0:
				return p.at(Riding, "", vehicle.Pod.ID)
			case !rider.Completed:
				// A shared ride can assign the party before the pod comes.
				return p.at(Waiting, cmp.Or(rider.LegFrom, rider.From), vehicle.Pod.ID)
			default:
				arrival.station, arrival.podID = rider.To, vehicle.Pod.ID
			}
		}
	}
	if arrival.station != "" {
		return p.at(Arrived, arrival.station, arrival.podID)
	}
	if state.Revision < p.revision {
		return p
	}
	return p.stop(orderGoneMessage)
}

func (p Passenger) at(phase PassengerPhase, station, podID string) Passenger {
	p.Phase, p.PodID = phase, podID
	if station != "" {
		p.Station = station
	}
	return p
}

func (p Passenger) stop(message string) Passenger {
	p.Phase, p.Message = Stopped, message
	return p
}
