// Package editormodel implements project operations shared by native tests and the editor worker.
package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/project"
)

// MaxRequestBytes bounds one private editor-worker request.
const MaxRequestBytes = project.MaxFileBytes + 256<<10

type request struct {
	Op       string         `json:"op"`
	Project  jsontext.Value `json:"project"`
	ParkRide jsontext.Value `json:"parkRide,omitempty"`
	View     jsontext.Value `json:"view,omitempty"`
}

type response struct {
	Valid   bool                   `json:"valid,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Profile *project.DemandProfile `json:"profile,omitempty"`
	Demand  *project.DemandConfig  `json:"demand,omitempty"`
	View    *mapView               `json:"view,omitempty"`
}

// Call handles one bounded JSON request without retaining caller data.
// It returns JSON errors instead of throwing across the browser boundary.
func Call(input string) string {
	result, err := execute(input)
	if err != nil {
		result = response{Error: err.Error()}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return `{"error":"Cannot encode the editor model result."}`
	}
	return string(encoded)
}

func execute(input string) (response, error) {
	if len(input) > MaxRequestBytes {
		return response{}, errors.New("editor model request is too large")
	}
	if err := scanRequest([]byte(input)); err != nil {
		return response{}, fmt.Errorf("check editor request: %w", err)
	}
	var command request
	if err := json.Unmarshal([]byte(input), &command, json.RejectUnknownMembers(true)); err != nil {
		return response{}, fmt.Errorf("decode editor request: %w", err)
	}
	if len(command.Project) > project.MaxFileBytes {
		return response{}, errors.New("editor project is too large")
	}
	if command.Project.Kind() != '{' {
		return response{}, errors.New("editor request needs a project object")
	}
	var config project.Config
	if err := json.Unmarshal(command.Project, &config, json.RejectUnknownMembers(true)); err != nil {
		return response{}, fmt.Errorf("decode editor project: %w", err)
	}
	if command.Op != "place-view" && len(command.View) != 0 || command.Op != "park-ride" && len(command.ParkRide) != 0 {
		return response{}, errors.New("editor operation has unrelated parameters")
	}
	switch command.Op {
	case "validate":
		if len(command.ParkRide) != 0 {
			return response{}, errors.New("validation does not accept a park-and-ride plan")
		}
		if err := project.Validate(config); err != nil {
			return response{}, err
		}
		return response{Valid: true}, nil
	case "park-ride":
		if len(command.ParkRide) == 0 {
			return response{}, errors.New("park-and-ride creation needs a plan")
		}
		plan, err := decodeParkRide(command.ParkRide)
		if err != nil {
			return response{}, err
		}
		profile, demand, err := makeParkRide(config, plan)
		if err != nil {
			return response{}, err
		}
		return response{Profile: &profile, Demand: &demand}, nil
	case "place-view":
		view, err := placeView(config.Geo, command.View)
		if err != nil {
			return response{}, err
		}
		return response{View: &view}, nil
	default:
		return response{}, errors.New("unknown editor model operation")
	}
}
