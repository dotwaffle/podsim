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

// BankVersion identifies projects with explicit station banks.
const BankVersion = 2

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
	if len(data) > MaxFileBytes {
		return errTooLarge
	}
	present, err := scanProjectBanks(data)
	if err != nil {
		return err
	}
	type plainConfig Config
	decoded := plainConfig(Clone(*config))
	if err := jsonv2.Unmarshal(data, &decoded, options, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	if present && decoded.Version != BankVersion {
		return errors.New("station Banks requires project version 2")
	}
	// Partial project updates can omit the version. Validate checks complete projects.
	if decoded.Version != 0 {
		if err := validateBankVersion(Config(decoded)); err != nil {
			return err
		}
	}
	for _, station := range decoded.Network.Stations {
		if err := validateBankNames(station); err != nil {
			return err
		}
	}
	*config = Config(decoded)
	return nil
}

func validateBankVersion(config Config) error {
	if config.Version != currentVersion && config.Version != BankVersion {
		return errors.New("project version must be 1 or 2")
	}
	banked := false
	for _, station := range config.Network.Stations {
		banked = banked || station.Banks != nil
	}
	if config.Version == currentVersion && banked {
		return errors.New("project version 1 cannot contain Banks")
	}
	if config.Version == BankVersion && !banked {
		return errors.New("project version 2 requires a banked station")
	}
	return nil
}

// scanProjectBanks bounds bank arrays without allocating their elements.
func scanProjectBanks(data []byte) (bool, error) {
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
		tokenKind := token.Kind()
		path := strings.Split(strings.ToLower(string(decoder.StackPointer())), "/")
		if len(path) < 5 || path[1] != "network" || path[2] != "stations" || path[4] != "banks" {
			continue
		}
		if len(path) == 5 {
			kind, length := decoder.StackIndex(decoder.StackDepth())
			if kind == jsontext.KindBeginObject && length%2 == 1 {
				present = true
				if decoder.PeekKind() != jsontext.KindBeginArray {
					return false, errors.New("station Banks must be a nonempty array")
				}
			}
			if tokenKind == jsontext.KindBeginArray && decoder.PeekKind() == jsontext.KindEndArray {
				return false, errors.New("station Banks must be a nonempty array")
			}
		}
		depth := decoder.StackDepth()
		kind, length := decoder.StackIndex(depth)
		if kind == jsontext.KindBeginArray {
			limit := int64(0)
			switch {
			case len(path) == 6:
				limit = sim.MaxStationBanks
			case len(path) == 8 && path[6] == "berthids":
				limit = MaxBerths
			}
			if limit > 0 && length > limit {
				return false, fmt.Errorf("bank array has more than %d elements", limit)
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
