package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

const serviceStateVersion = 6

func serviceStateLimits() jsonLimits {
	limits := stateJSONLimits
	limits.arrays = maps.Clone(limits.arrays)
	// Recognize larger shapes without admitting unsupported operating states.
	limits.arrays["/simulation/waiting"] = 6200
	limits.arrays["/simulation/pods/*/riders"] = sim.MaxExpressParties
	limits.arrays["/project/expressServices"] = project.MaxExpressServices
	for _, path := range []string{
		"/project/network/Lanes/*/VehicleClasses",
		"/project/network/Stations/*/VehicleClasses",
		"/project/network/Stations/*/Berths/*/VehicleClasses",
	} {
		limits.arrays[path] = 4
	}
	return limits
}

func scanStateOrderFields(data []byte, version int) error {
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
		path := strings.Split(string(decoder.StackPointer()), "/")
		if !savedOrderFieldPath(path) {
			continue
		}
		if version < serviceStateVersion {
			return errors.New("legacy saved state contains version 6 order fields")
		}
		name := path[len(path)-1]
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if name == "legacyCohort" || name == "legacyPartySize" {
			if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
				return errors.New("saved legacy marker must be Boolean")
			}
			continue
		}
		if value.Kind() != jsontext.KindString || value.String() == "" {
			return fmt.Errorf("saved order field %s needs nonempty text", name)
		}
		switch name {
		case "class":
			if _, known := sim.LookupVehicleClass(sim.VehicleClass(value.String())); !known {
				return sim.ErrUnknownVehicleClass
			}
		case "sharingConsent":
			if consent := sim.SharingConsent(value.String()); consent != sim.PrivateConsent && consent != sim.SharedConsent && consent != sim.LegacyUnknownConsent {
				return errors.New("invalid saved sharing consent")
			}
		case "service":
			if service := sim.ServiceChoice(value.String()); service != sim.OnDemandService && service != sim.ExpressServiceChoice {
				return errors.New("invalid saved service choice")
			}
		case "serviceID":
			if len(value.String()) > 64 {
				return errors.New("saved service ID is too long")
			}
		}
	}
}

func savedOrderFieldPath(path []string) bool {
	if len(path) == 5 && path[1] == "simulation" && path[2] == "pods" {
		return path[4] == "class" || path[4] == "legacyCohort"
	}
	request := len(path) == 7 && path[1] == "simulation" && path[2] == "pods" && path[4] == "riders" ||
		len(path) == 6 && path[1] == "simulation" && path[2] == "waiting" && path[4] == "request"
	if !request {
		return false
	}
	switch path[len(path)-1] {
	case "sharingConsent", "service", "serviceID", "legacyPartySize":
		return true
	}
	return false
}
