package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// MaxTopologyJSON is the largest topology document that a client decodes.
// The topology holds the network of a project file and a few identity
// members.
const MaxTopologyJSON = project.MaxFileBytes + 4096

var topologyJSONLimits = jsonLimits{
	depth: 64, elements: 0, members: 256,
	arrays: map[string]int64{
		"/couplingSites":                              sim.MaxCouplingSites,
		"/couplingCorridors":                          sim.MaxCouplingCorridors,
		"/couplingCorridors/*/laneIds":                project.MaxLanes,
		"/expressServices":                            project.MaxExpressServices,
		"/network/lanes/*/vehicleClasses":             4,
		"/network/stations/*/vehicleClasses":          4,
		"/network/stations/*/berths/*/vehicleClasses": 4,
		"/network/nodes":                              project.MaxNodes,
		"/network/lanes":                              project.MaxLanes,
		"/network/stations":                           project.MaxStations,
		"/network/stations/*/berths":                  project.MaxBerths,
		"/network/stations/*/banks":                   sim.MaxStationBanks,
		"/network/stations/*/banks/*/berthIDs":        project.MaxBerths,
	},
}

// UnmarshalJSON rejects unbounded or invalid topology members before allocation.
func (topology *TopologySnapshot) UnmarshalJSON(data []byte) error {
	if len(data) > MaxTopologyJSON {
		return errors.New("topology JSON is too large")
	}
	if err := prescanJSON(data, topologyJSONLimits); err != nil {
		return err
	}
	markers, err := scanTopologyBanks(data)
	if err != nil {
		return err
	}
	// The contract markers select the family. A coupling member selects
	// the coupling decoder, which requires the coupling marker.
	if markers.coupling {
		decoded, err := decodeCouplingTopology(data)
		if err != nil {
			return err
		}
		if err := checkTopologyProjectVersion(decoded); err != nil {
			return err
		}
		*topology = decoded
		return nil
	}
	var contract contractMarkers
	if markers.express {
		contract.order = sim.ExpressOrderContract
	}
	if err := scanContractMarkers(data, markers.express, false); err != nil {
		return err
	}
	if err := scanStreamServiceMembers(data, contract); err != nil {
		return err
	}
	type plainTopology TopologySnapshot
	var decoded plainTopology
	if err := jsonv2.Unmarshal(data, &decoded, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if err := checkTopologyProjectVersion(TopologySnapshot(decoded)); err != nil {
		return err
	}
	if err := decoded.Network.ValidateStationBanks(); err != nil {
		return err
	}
	if markers.express {
		if err := validateStreamTopology(TopologySnapshot(decoded), contract); err != nil {
			return err
		}
	}
	*topology = TopologySnapshot(decoded)
	return nil
}

type topologyMarkers struct {
	coupling, express bool
}

// scanTopologyBanks bounds bank arrays and records the root contract members.
func scanTopologyBanks(data []byte) (topologyMarkers, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	var markers topologyMarkers
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return markers, nil
		}
		if err != nil {
			return topologyMarkers{}, err
		}
		tokenKind := token.Kind()
		path := strings.Split(string(decoder.StackPointer()), "/")
		kind, length := decoder.StackIndex(decoder.StackDepth())
		member := tokenKind == jsontext.KindString && kind == jsontext.KindBeginObject && length%2 == 1
		if len(path) == 2 && member {
			markers.coupling = markers.coupling || couplingMember(token.String())
			markers.express = markers.express || path[1] == "orderContract"
		}
		if len(path) != 5 || path[1] != "network" || path[2] != "stations" || path[4] != "banks" {
			continue
		}
		if kind == jsontext.KindBeginObject && length%2 == 1 && decoder.PeekKind() != jsontext.KindBeginArray {
			return topologyMarkers{}, errors.New("station banks must be a nonempty array")
		}
		if tokenKind == jsontext.KindBeginArray && decoder.PeekKind() == jsontext.KindEndArray {
			return topologyMarkers{}, errors.New("station banks must be a nonempty array")
		}
	}
}
