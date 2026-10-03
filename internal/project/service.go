package project

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// ServiceVersion identifies projects with authored vehicle and service metadata.
const ServiceVersion = 3

// MaxExpressServices bounds the directed service registry of a project.
const MaxExpressServices = 300

func validateServiceVersion(config Config) error {
	if config.Version == ServiceVersion {
		return nil
	}
	present := config.ExpressServices != nil || config.StationQueueSpacing != ""
	for _, placement := range config.Fleet {
		present = present || placement.Class != ""
	}
	for _, lane := range config.Network.Lanes {
		present = present || lane.VehicleClasses != 0
	}
	for _, station := range config.Network.Stations {
		present = present || station.VehicleClasses != 0
		for _, berth := range station.Berths {
			present = present || berth.VehicleClasses != 0
		}
	}
	if present {
		return errors.New("vehicle and service fields require project version 3")
	}
	return nil
}

// scanProjectService checks new field presence and shape before typed allocation.
func scanProjectService(data []byte) (bool, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	present := false
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return present, nil
		}
		if err != nil {
			return false, err
		}
		path := strings.Split(strings.ToLower(string(decoder.StackPointer())), "/")
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if kind == jsontext.KindBeginArray && len(path) == 3 && path[1] == "expressservices" && length > MaxExpressServices {
			return false, fmt.Errorf("express registry has more than %d services", MaxExpressServices)
		}
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		switch {
		case len(path) == 2 && path[1] == "stationqueuespacing":
			present = true
			value, err := decoder.ReadToken()
			if err != nil {
				return false, err
			}
			if value.Kind() != jsontext.KindString || !validStationQueueSpacing(sim.StationQueueSpacing(value.String())) {
				return false, errors.New("station queue spacing must be ordinary or compact-v1")
			}
		case serviceClassListPath(path):
			present = true
			var classes sim.ClassSet
			if err := classes.UnmarshalJSONFrom(decoder); err != nil {
				return false, err
			}
		case len(path) == 4 && path[1] == "fleet" && path[3] == "class":
			present = true
			value, err := decoder.ReadToken()
			if err != nil {
				return false, err
			}
			if value.Kind() != jsontext.KindString || value.String() == "" {
				return false, errors.New("explicit fleet class needs nonempty text")
			}
			if _, known := sim.LookupVehicleClass(sim.VehicleClass(value.String())); !known {
				return false, sim.ErrUnknownVehicleClass
			}
		case len(path) == 2 && path[1] == "expressservices":
			present = true
			if decoder.PeekKind() != jsontext.KindBeginArray {
				return false, errors.New("express registry must be an array")
			}
		}
	}
}

func serviceClassListPath(path []string) bool {
	if len(path) == 5 && path[1] == "network" && (path[2] == "stations" || path[2] == "lanes") {
		return path[4] == "vehicleclasses"
	}
	return len(path) == 7 && path[1] == "network" && path[2] == "stations" &&
		path[4] == "berths" && path[6] == "vehicleclasses"
}
