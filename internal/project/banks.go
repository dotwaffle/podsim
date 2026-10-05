package project

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/sim"
)

// UnmarshalJSON checks bank field presence before decoding a project.
func (config *Config) UnmarshalJSON(data []byte) error {
	return config.decodeJSON(data, json.DefaultOptionsV1())
}

// UnmarshalJSONFrom keeps the caller's JSON options when decoding a project.
func (config *Config) UnmarshalJSONFrom(decoder *jsontext.Decoder) error {
	data, err := decoder.ReadValue()
	if err != nil {
		return err
	}
	return config.decodeJSON(data, decoder.Options())
}

func (config *Config) decodeJSON(data []byte, options jsonv2.Options) error {
	return config.decodeJSONLimit(data, options, MaxFileBytes)
}

// DecodeCanonicalJSON validates a project from a bounded checkpoint component.
// Raw JSON can use 80 MiB. Its canonical project must still fit MaxFileBytes.
// The checkpoint caller must bound its enclosing input before this call.
func DecodeCanonicalJSON(raw []byte) (Config, error) {
	var config Config
	if err := config.decodeJSONLimit(raw, json.DefaultOptionsV1(), 8*MaxFileBytes); err != nil {
		return Config{}, err
	}
	if err := Validate(config); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config *Config) decodeJSONLimit(data []byte, options jsonv2.Options, rawLimit int) error {
	if len(data) > rawLimit {
		return errTooLarge
	}
	if err := scanProjectBanks(data); err != nil {
		return err
	}
	fields, err := scanProjectFields(data)
	if err != nil {
		return err
	}
	if fields.coupling || HasCouplingContract(*config) {
		if err := scanCouplingProjectBounds(data); err != nil {
			return err
		}
	}
	type plainConfig Config
	decoded := plainConfig(Clone(*config))
	if err := jsonv2.Unmarshal(data, &decoded, options, jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	// Partial project updates can omit the version. Validate checks complete projects.
	if decoded.Version != 0 {
		if err := validateVersion(Config(decoded)); err != nil {
			return err
		}
	}
	// An explicit false or empty coupling member is presence. The typed
	// check below cannot see it.
	if fields.coupling && !HasCouplingContract(Config(decoded)) {
		return errors.New("coupling fields require couplingContract compact-pair-v1")
	}
	if err := validateCouplingContract(Config(decoded)); err != nil {
		return err
	}
	if err := sim.ValidateIncidentContract(decoded.IncidentContract); err != nil {
		return err
	}
	// The scan above refuses a null faults value, which the typed decode
	// reads as no faults. The marker rules then hold for the decoded
	// project.
	if err := validateFaultContract(Config(decoded)); err != nil {
		return err
	}
	if err := validateCouplingGeometry(Config(decoded)); err != nil {
		return err
	}
	for _, station := range decoded.Network.Stations {
		if err := validateBankNames(station); err != nil {
			return err
		}
	}
	*config = Config(decoded)
	return nil
}

// scanProjectBanks bounds bank arrays without allocating their elements.
func scanProjectBanks(data []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		tokenKind := token.Kind()
		path := strings.Split(string(decoder.StackPointer()), "/")
		if len(path) < 5 || path[1] != "network" || path[2] != "stations" || path[4] != "banks" {
			continue
		}
		if len(path) == 5 {
			kind, length := decoder.StackIndex(decoder.StackDepth())
			if kind == jsontext.KindBeginObject && length%2 == 1 {
				if decoder.PeekKind() != jsontext.KindBeginArray {
					return errors.New("station banks must be a nonempty array")
				}
			}
			if tokenKind == jsontext.KindBeginArray && decoder.PeekKind() == jsontext.KindEndArray {
				return errors.New("station banks must be a nonempty array")
			}
		}
		depth := decoder.StackDepth()
		kind, length := decoder.StackIndex(depth)
		if kind == jsontext.KindBeginArray {
			limit := int64(0)
			switch {
			case len(path) == 6:
				limit = sim.MaxStationBanks
			case len(path) == 8 && path[6] == "berthIDs":
				limit = MaxBerths
			}
			if limit > 0 && length > limit {
				return fmt.Errorf("bank array has more than %d elements", limit)
			}
		}
	}
}

func validateBankNames(station sim.Station) error {
	if station.Banks == nil {
		return nil
	}
	if len(station.Banks) < 1 || len(station.Banks) > sim.MaxStationBanks {
		return fmt.Errorf("station %s must contain 1 to %d banks", quoteID(station.ID), sim.MaxStationBanks)
	}
	if station.Entry != station.Banks[0].Entry || station.Exit != station.Banks[0].Exit {
		return fmt.Errorf("station %s gates must match its first bank", quoteID(station.ID))
	}
	berths := make(map[string]bool, len(station.Berths))
	for _, berth := range station.Berths {
		berths[berth.ID] = true
	}
	banks := make(map[string]bool, len(station.Banks))
	assigned := make(map[string]bool, len(station.Berths))
	validID := func(id string) bool { return id != "" && len(id) <= maxIDLength }
	for _, bank := range station.Banks {
		if !validID(bank.ID) || !validID(bank.Entry) || !validID(bank.Exit) || banks[bank.ID] {
			return fmt.Errorf("station %s bank IDs must be unique and contain 1 to %d characters", quoteID(station.ID), maxIDLength)
		}
		banks[bank.ID] = true
		if len(bank.BerthIDs) < 1 || len(bank.BerthIDs) > MaxBerths {
			return fmt.Errorf("bank %s must contain 1 to %d berth IDs", quoteID(bank.ID), MaxBerths)
		}
		for _, id := range bank.BerthIDs {
			if !validID(id) || !berths[id] || assigned[id] {
				return fmt.Errorf("bank %s has an invalid or duplicate berth ID %s", quoteID(bank.ID), quoteID(id))
			}
			assigned[id] = true
		}
	}
	if len(assigned) != len(berths) {
		return fmt.Errorf("station %s banks must assign every berth", quoteID(station.ID))
	}
	return nil
}
