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

// scanStateOrderFields checks the order option members of a saved state
// before the typed decode. scanPackedOrders checks the size of the order
// text.
func scanStateOrderFields(data []byte) error {
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
