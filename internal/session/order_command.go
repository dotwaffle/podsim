package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"

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
	// Member names match exactly. The other encoding/json rules stay.
	decoder := jsontext.NewDecoder(bytes.NewReader(data), json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true))
	if err := jsonv2.UnmarshalDecode(decoder, &next); err != nil {
		return err
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
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
	if err := jsonv2.Unmarshal(data, &marker, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false)); err != nil {
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
		name := token.String()
		raw, err := decoder.ReadValue()
		if err != nil {
			return false, err
		}
		switch name {
		case "orderContract", "partySize", "sharingConsent", "service", "serviceID":
			if seen[name] {
				return false, fmt.Errorf("duplicate order field %s", name)
			}
			seen[name] = true
			if name == "orderContract" {
				var contract sim.OrderContract
				if string(raw) == "null" || unmarshalOrderScalar(raw, &contract) != nil || contract != sim.ExpressOrderContract {
					return false, errors.New("invalid trip order contract")
				}
				continue
			}
			if name == "partySize" && marker.OrderContract == sim.ExpressOrderContract {
				var size int
				if unmarshalOrderScalar(raw, &size) != nil || size < 1 || size > 20 {
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
	if name == "partySize" {
		var size int
		if err := unmarshalOrderScalar(raw, &size); err != nil || size < 1 || size > sim.MaxNewPartySize {
			return fmt.Errorf("party size must be 1 to %d", sim.MaxNewPartySize)
		}
		return nil
	}
	var value string
	if err := unmarshalOrderScalar(raw, &value); err != nil || value == "" {
		return fmt.Errorf("order field %s needs nonempty text", name)
	}
	return nil
}

// unmarshalOrderScalar decodes one order field value with the same
// acceptance as the order command decoder.
func unmarshalOrderScalar(raw jsontext.Value, target any) error {
	return jsonv2.Unmarshal(raw, target, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false))
}
