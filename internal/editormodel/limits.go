package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"strings"
	"unicode"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// scanRequest checks container counts before typed decoding allocates their contents.
func scanRequest(data []byte) error {
	return scanRequestOptions(data, false)
}

func scanRequestOptions(data []byte, metadata bool) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowInvalidUTF8(metadata))
	var arrayLimits [65]int64
	for {
		var kind jsontext.Kind
		if decoder.PeekKind() == jsontext.KindString {
			value, err := decoder.ReadValue()
			if err != nil {
				return err
			}
			if len(value) > 1024 && (!metadata || !strings.HasPrefix(string(decoder.StackPointer()), "/metadata/")) {
				return errors.New("editor request string is too long")
			}
			kind = jsontext.KindString
		} else {
			token, err := decoder.ReadToken()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			kind = token.Kind()
		}
		if kind == jsontext.KindNull && strings.HasPrefix(string(decoder.StackPointer()), "/parkRide") {
			return errors.New("park-and-ride values cannot be null")
		}
		depth := decoder.StackDepth()
		if depth > 64 {
			return errors.New("editor request has too many nested values")
		}
		if kind == jsontext.KindBeginArray {
			arrayLimits[depth] = requestArrayLimit(decoder)
		}
		container, length := decoder.StackIndex(depth)
		if container == jsontext.KindBeginArray && length > arrayLimits[depth] {
			return errors.New("editor request array is too long")
		}
		if container == jsontext.KindBeginObject && (length+1)/2 > 256 {
			return errors.New("editor request object has too many members")
		}
	}
}

func requestArrayLimit(decoder *jsontext.Decoder) int64 {
	parts := strings.Split(string(decoder.StackPointer()), "/")
	if len(parts) > 1 && parts[1] == "patch" {
		parts[1] = "project"
	}
	for level := 1; level < len(parts); level++ {
		if kind, _ := decoder.StackIndex(level); kind == jsontext.KindBeginArray {
			parts[level] = "*"
		}
	}
	path := strings.Join(parts, "/")
	if strings.HasPrefix(path, "/project/") {
		// The server decoder matches member names as strings.EqualFold does.
		return projectArrayLimit(foldName(path))
	}
	switch path {
	case "/keys":
		return 32
	case "/edit/value/value":
		return 8
	case "/edit/value/value/*/BerthIDs":
		return project.MaxBerths
	case "/parkRide/destinations":
		return project.MaxStations - 1
	default:
		return 0
	}
}

// foldName gives name with each rune replaced by the smallest rune of its
// case fold set, as encoding/json/v2 folds a member name. Two names that
// strings.EqualFold matches give the same result. Unlike encoding/json/v2,
// foldName keeps dashes and underscores, because the server decoder uses the
// v1 options, which compare them exactly. ASCII letters become upper case.
func foldName(name string) string {
	var out strings.Builder
	for _, r := range name {
		for {
			next := unicode.SimpleFold(r)
			if next <= r {
				r = next
				break
			}
			r = next
		}
		out.WriteRune(r)
	}
	return out.String()
}

// projectArrayLimit gives the array limit of a folded project path.
func projectArrayLimit(path string) int64 {
	switch path {
	case "/PROJECT/NETWORK/NODES":
		return project.MaxNodes
	case "/PROJECT/NETWORK/LANES":
		return project.MaxLanes
	case "/PROJECT/NETWORK/STATIONS":
		return project.MaxStations
	case "/PROJECT/NETWORK/STATIONS/*/BANKS":
		return 8
	case "/PROJECT/NETWORK/STATIONS/*/BANKS/*/BERTHIDS", "/PROJECT/NETWORK/STATIONS/*/BERTHS":
		return project.MaxBerths
	case "/PROJECT/FLEET":
		return project.MaxPods
	case "/PROJECT/EXPRESSSERVICES":
		return project.MaxExpressServices
	case "/PROJECT/COUPLINGSITES":
		return sim.MaxCouplingSites
	case "/PROJECT/COUPLINGCORRIDORS":
		return sim.MaxCouplingCorridors
	case "/PROJECT/COUPLINGCORRIDORS/*/LANEIDS":
		return project.MaxLanes
	case "/PROJECT/NETWORK/LANES/*/VEHICLECLASSES", "/PROJECT/NETWORK/STATIONS/*/VEHICLECLASSES", "/PROJECT/NETWORK/STATIONS/*/BERTHS/*/VEHICLECLASSES":
		return 4
	case "/PROJECT/RAILARRIVALS", "/PROJECT/RAILDEPARTURES":
		return project.MaxRailArrivals
	case "/PROJECT/RAILARRIVALS/*/DESTINATIONS", "/PROJECT/RAILDEPARTURES/*/ORIGINS":
		return project.MaxRailDestinations
	case "/PROJECT/DEMANDPROFILES":
		return project.MaxProfiles
	case "/PROJECT/DEMANDPROFILES/*/BANDS", "/PROJECT/DEMANDPROFILES/*/FLOWS/*/WEIGHTS":
		return project.MaxBands
	case "/PROJECT/DEMANDPROFILES/*/FLOWS":
		return project.MaxFlows
	default:
		return 0
	}
}

type projectSizeCounter struct {
	size int
}

func (c *projectSizeCounter) Write(data []byte) (int, error) {
	if len(data) > project.MaxFileBytes-c.size {
		return 0, errors.New("the created profile makes the project too large")
	}
	c.size += len(data)
	return len(data), nil
}
