package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// UnmarshalJSON checks explicit order fields before replacing a command.
// Omitted fields keep their zero values for exact-command retry comparison.
func (command *Command) UnmarshalJSON(data []byte) error {
	if len(data) > MaxInflatedCommandBytes {
		return errors.New("command JSON is too large")
	}
	present, err := scanOrderFields(data)
	if err != nil {
		return err
	}
	type plainCommand Command
	next := plainCommand(*command)
	if next.Project != nil {
		next.Project = new(project.Clone(*next.Project))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&next); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("send one command only")
	}
	if (present || next.OrderContract != "") && next.Action != "trip" {
		return errors.New("order fields require a trip command")
	}
	if present {
		// Endpoint validation stays at admission. It supplies the usual
		// command reply and does not change legacy HTTP rejection behavior.
		options := sim.TripOptions{From: "origin", To: "destination", PartySize: next.PartySize,
			SharingConsent: next.SharingConsent, Service: next.Service, ServiceID: next.ServiceID}
		if _, err := sim.NormalizeTripOptionsWithOrderContract(options, next.OrderContract); err != nil {
			return err
		}
	}
	*command = Command(next)
	return nil
}

func scanOrderFields(data []byte) (bool, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	token, err := decoder.ReadToken()
	if err != nil {
		return false, err
	}
	if token.Kind() != jsontext.KindBeginObject {
		// The typed decoder retains the existing non-object behavior.
		return false, nil
	}
	seen := make(map[string]bool, 5)
	var marker struct {
		OrderContract sim.OrderContract `json:"orderContract"`
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		return false, err
	}
	if err := sim.ValidateOrderContract(marker.OrderContract); err != nil {
		return false, err
	}
	for decoder.PeekKind() != jsontext.KindEndObject {
		token, err := decoder.ReadToken()
		if err != nil {
			return false, err
		}
		name := strings.ToLower(token.String())
		raw, err := decoder.ReadValue()
		if err != nil {
			return false, err
		}
		switch name {
		case "ordercontract", "partysize", "sharingconsent", "service", "serviceid":
			if seen[name] {
				return false, fmt.Errorf("duplicate order field %s", name)
			}
			seen[name] = true
			if name == "ordercontract" {
				var contract sim.OrderContract
				if string(raw) == "null" || json.Unmarshal(raw, &contract) != nil || contract != sim.ExpressOrderContract {
					return false, errors.New("invalid trip order contract")
				}
				continue
			}
			if name == "partysize" && marker.OrderContract == sim.ExpressOrderContract {
				var size int
				if json.Unmarshal(raw, &size) != nil || size < 1 || size > 20 {
					return false, errors.New("party size must be 1 to 20")
				}
				continue
			}
			if err := validateExplicitOrderField(name, raw); err != nil {
				return false, err
			}
		}
	}
	return len(seen) != 0, nil
}

func validateExplicitOrderField(name string, raw jsontext.Value) error {
	if raw.Kind() == jsontext.KindNull {
		return fmt.Errorf("order field %s cannot be null", name)
	}
	if name == "partysize" {
		var size int
		if err := json.Unmarshal(raw, &size); err != nil || size < 1 || size > sim.MaxNewPartySize {
			return fmt.Errorf("party size must be 1 to %d", sim.MaxNewPartySize)
		}
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || value == "" {
		return fmt.Errorf("order field %s needs nonempty text", name)
	}
	return nil
}
