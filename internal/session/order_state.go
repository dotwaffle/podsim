package session

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

const serviceStateVersion = 6

// serviceStateLimits bound a version 6 state file, which has no markers.
func serviceStateLimits() jsonLimits {
	return savedLimits(contractMarkers{})
}

func scanStateOrderFields(data []byte, version int) error {
	return scanStateOrderFieldsContract(data, version, version == expressStateVersion)
}

func scanStateOrderFieldsContract(data []byte, version int, packed bool) error {
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
		if version < couplingStateVersion && couplingMember(token.String()) {
			return errors.New("legacy saved state contains coupling fields")
		}
		path := strings.Split(string(decoder.StackPointer()), "/")
		if !savedOrderFieldPath(path) {
			continue
		}
		name := path[len(path)-1]
		value, err := decoder.ReadToken()
		if err != nil {
			return err
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
			if consent := sim.SharingConsent(value.String()); consent != sim.PrivateConsent && consent != sim.SharedConsent {
				return errors.New("invalid saved sharing consent")
			}
		case "service":
			if service := sim.ServiceChoice(value.String()); service != sim.OnDemandService && service != sim.ExpressServiceChoice {
				return errors.New("invalid saved service choice")
			}
		case "serviceID":
			if len(value.String()) > 64 && !packed {
				return errors.New("saved service ID is too long")
			}
		}
	}
}

func savedOrderFieldPath(path []string) bool {
	if len(path) == 5 && path[1] == "simulation" && path[2] == "pods" {
		return path[4] == "class"
	}
	request := len(path) == 7 && path[1] == "simulation" && path[2] == "pods" && path[4] == "riders" ||
		len(path) == 6 && path[1] == "simulation" && path[2] == "waiting" && path[4] == "request"
	if !request {
		return false
	}
	switch path[len(path)-1] {
	case "sharingConsent", "service", "serviceID":
		return true
	}
	return false
}
