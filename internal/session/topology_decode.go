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

var topologyJSONLimits = jsonLimits{
	depth: 64, elements: 0, members: 256, foldNames: true,
	arrays: map[string]int64{
		"/couplingSites":                              sim.MaxCouplingSites,
		"/couplingCorridors":                          sim.MaxCouplingCorridors,
		"/couplingCorridors/*/laneIds":                project.MaxLanes,
		"/expressServices":                            project.MaxExpressServices,
		"/network/Lanes/*/VehicleClasses":             4,
		"/network/Stations/*/VehicleClasses":          4,
		"/network/Stations/*/Berths/*/VehicleClasses": 4,
		"/network/Nodes":                              project.MaxNodes,
		"/network/Lanes":                              project.MaxLanes,
		"/network/Stations":                           project.MaxStations,
		"/network/Stations/*/Berths":                  project.MaxBerths,
		"/network/Stations/*/Banks":                   sim.MaxStationBanks,
		"/network/Stations/*/Banks/*/BerthIDs":        project.MaxBerths,
	},
}

// UnmarshalJSON rejects unbounded or invalid topology members before allocation.
func (topology *TopologySnapshot) UnmarshalJSON(data []byte) error {
	if len(data) > project.MaxFileBytes+4096 {
		return errors.New("topology JSON is too large")
	}
	if err := prescanJSON(data, topologyJSONLimits); err != nil {
		return err
	}
	if err := scanTopologyBanks(data); err != nil {
		return err
	}
	version := FoundationStreamVersion
	var header struct {
		ProjectVersion int `json:"projectVersion"`
	}
	if err := jsonv2.Unmarshal(data, &header, json.DefaultOptionsV1()); err != nil {
		return err
	}
	if header.ProjectVersion == project.CouplingVersion {
		decoded, err := decodeCouplingTopology(data)
		if err != nil {
			return err
		}
		*topology = decoded
		return nil
	}
	if header.ProjectVersion == project.ExpressVersion {
		version = ExpressStreamVersion
	}
	if err := scanContractMarkers(data, version == ExpressStreamVersion, false); err != nil {
		return err
	}
	if err := scanStreamServiceMembers(data, version); err != nil {
		return err
	}
	type plainTopology TopologySnapshot
	var decoded plainTopology
	if err := jsonv2.Unmarshal(data, &decoded, json.DefaultOptionsV1(), jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if err := decoded.Network.ValidateStationBanks(); err != nil {
		return err
	}
	if version == ExpressStreamVersion {
		if err := validateStreamTopology(TopologySnapshot(decoded), version); err != nil {
			return err
		}
	}
	*topology = TopologySnapshot(decoded)
	return nil
}

func scanTopologyBanks(data []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	var coupling bool
	var version int64
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			if coupling && version != project.CouplingVersion {
				return errors.New("legacy topology contains coupling fields")
			}
			return nil
		}
		if err != nil {
			return err
		}
		tokenKind := token.Kind()
		path := strings.Split(strings.ToLower(string(decoder.StackPointer())), "/")
		kind, length := decoder.StackIndex(decoder.StackDepth())
		member := tokenKind == jsontext.KindString && kind == jsontext.KindBeginObject && length%2 == 1
		if len(path) == 2 && member {
			coupling = coupling || couplingMember(token.String())
			if path[1] == "projectversion" {
				value, err := decoder.ReadToken()
				if err != nil {
					return err
				}
				if value.Kind() == jsontext.KindNumber {
					version, _ = value.Int()
				}
			}
		}
		if len(path) != 5 || path[1] != "network" || path[2] != "stations" || path[4] != "banks" {
			continue
		}
		if kind == jsontext.KindBeginObject && length%2 == 1 && decoder.PeekKind() != jsontext.KindBeginArray {
			return errors.New("station Banks must be a nonempty array")
		}
		if tokenKind == jsontext.KindBeginArray && decoder.PeekKind() == jsontext.KindEndArray {
			return errors.New("station Banks must be a nonempty array")
		}
	}
}
