package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
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
	switch strings.Join(parts, "/") {
	case "/keys":
		return 32
	case "/project/network/Nodes":
		return project.MaxNodes
	case "/project/network/Lanes":
		return project.MaxLanes
	case "/project/network/Stations":
		return project.MaxStations
	case "/project/network/Stations/*/Banks", "/edit/value/value":
		return 8
	case "/project/network/Stations/*/Banks/*/BerthIDs", "/edit/value/value/*/BerthIDs":
		return project.MaxBerths
	case "/project/network/Stations/*/Berths":
		return project.MaxBerths
	case "/project/fleet":
		return project.MaxPods
	case "/project/railArrivals", "/project/railDepartures":
		return project.MaxRailArrivals
	case "/project/railArrivals/*/destinations", "/project/railDepartures/*/origins":
		return project.MaxRailDestinations
	case "/project/demandProfiles":
		return project.MaxProfiles
	case "/project/demandProfiles/*/bands", "/project/demandProfiles/*/flows/*/weights":
		return project.MaxBands
	case "/project/demandProfiles/*/flows":
		return project.MaxFlows
	case "/parkRide/destinations":
		return project.MaxStations - 1
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
