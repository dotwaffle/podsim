package project

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/sim"
)

func couplingMemberBit(name string) uint8 {
	switch name {
	case "couplingcontract":
		return 1
	case "couplingenabled":
		return 2
	case "couplingsites":
		return 4
	case "couplingcorridors":
		return 8
	default:
		return 0
	}
}

func scanCouplingMember(decoder *jsontext.Decoder, name string) error {
	switch name {
	case "couplingcontract":
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindString || value.String() != string(sim.CompactPairV1CouplingContract) {
			return sim.ErrUnknownCouplingContract
		}
		return nil
	case "couplingenabled":
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
			return errors.New("coupling enabled must be Boolean")
		}
		return nil
	case "couplingsites":
		return scanCouplingRecords(decoder, true, sim.MaxCouplingSites)
	case "couplingcorridors":
		return scanCouplingRecords(decoder, false, sim.MaxCouplingCorridors)
	default:
		return errors.New("unknown coupling member")
	}
}

func scanCouplingRecords(decoder *jsontext.Decoder, sites bool, limit int) error {
	start, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if start.Kind() != jsontext.KindBeginArray {
		return errors.New("coupling registry must be an array")
	}
	for count := 0; decoder.PeekKind() != jsontext.KindEndArray; count++ {
		if count >= limit {
			return fmt.Errorf("coupling registry exceeds %d records", limit)
		}
		if recordErr := scanCouplingRecord(decoder, sites); recordErr != nil {
			return recordErr
		}
	}
	_, err = decoder.ReadToken()
	return err
}

func scanCouplingRecord(decoder *jsontext.Decoder, site bool) error {
	start, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if start.Kind() != jsontext.KindBeginObject {
		return errors.New("coupling record must be an object")
	}
	var seen uint8
	for decoder.PeekKind() != jsontext.KindEndObject {
		name, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if name.Kind() != jsontext.KindString {
			return errors.New("coupling record needs a member name")
		}
		field := strings.ToLower(name.String())
		bit, number, path := couplingRecordField(field, site)
		if bit == 0 {
			return fmt.Errorf("unknown coupling record member %s", quoteID(name.String()))
		}
		if seen&bit != 0 {
			return errors.New("duplicate coupling record member")
		}
		seen |= bit
		switch {
		case path:
			err = scanCouplingLaneIDs(decoder)
		case number:
			err = scanCouplingNumber(decoder)
		default:
			err = scanCouplingID(decoder)
		}
		if err != nil {
			return err
		}
	}
	if _, err := decoder.ReadToken(); err != nil {
		return err
	}
	required := uint8(15)
	if site {
		required = 63
	}
	if seen != required {
		return errors.New("coupling record has missing members")
	}
	return nil
}

func couplingRecordField(name string, site bool) (bit uint8, number, path bool) {
	if name == "id" {
		return 1, false, false
	}
	if site {
		switch name {
		case "laneid":
			return 2, false, false
		case "startmeters":
			return 4, true, false
		case "endmeters":
			return 8, true, false
		case "frontstagingmeters":
			return 16, true, false
		case "rearstagingmeters":
			return 32, true, false
		}
	} else {
		switch name {
		case "assemblysiteid":
			return 2, false, false
		case "splitsiteid":
			return 4, false, false
		case "laneids":
			return 8, false, true
		}
	}
	return 0, false, false
}

func scanCouplingID(decoder *jsontext.Decoder) error {
	value, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if value.Kind() != jsontext.KindString || value.String() == "" || !utf8.ValidString(value.String()) || len(value.String()) > maxIDLength {
		return fmt.Errorf("coupling ID must contain 1 to %d UTF-8 bytes", maxIDLength)
	}
	return nil
}

func scanCouplingNumber(decoder *jsontext.Decoder) error {
	value, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if value.Kind() != jsontext.KindNumber {
		return errors.New("coupling position must be a finite number")
	}
	number, err := value.Float()
	if err != nil {
		return err
	}
	if math.IsInf(number, 0) || math.IsNaN(number) {
		return errors.New("coupling position must be a finite number")
	}
	return nil
}

func scanCouplingLaneIDs(decoder *jsontext.Decoder) error {
	start, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	if start.Kind() != jsontext.KindBeginArray {
		return errors.New("coupling lane IDs must be an array")
	}
	count := 0
	for decoder.PeekKind() != jsontext.KindEndArray {
		if count >= MaxLanes {
			return fmt.Errorf("coupling path exceeds %d lanes", MaxLanes)
		}
		if err := scanCouplingID(decoder); err != nil {
			return err
		}
		count++
	}
	if _, err := decoder.ReadToken(); err != nil {
		return err
	}
	if count == 0 {
		return errors.New("coupling path must contain lanes")
	}
	return nil
}

// scanCouplingProjectBounds checks the existing collection bounds of a
// project with coupling fields before typed allocation. Other projects
// retain their existing two scan passes.
func scanCouplingProjectBounds(data []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	var arrivals, departures int64
	for {
		_, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		kind, count := decoder.StackIndex(decoder.StackDepth())
		if kind != jsontext.KindBeginArray {
			continue
		}
		parts := strings.Split(strings.ToLower(string(decoder.StackPointer())), "/")
		if len(parts) < 2 {
			continue
		}
		// An array's current pointer ends with its completed element index.
		parts = parts[:len(parts)-1]
		for index, part := range parts {
			if _, err := strconv.ParseUint(part, 10, 64); err == nil {
				parts[index] = "*"
			}
		}
		path := strings.Join(parts, "/")
		limit := couplingProjectArrayLimit(path)
		if limit > 0 && count > int64(limit) {
			return fmt.Errorf("project array %s exceeds %d elements", path, limit)
		}
		switch path {
		case "/railarrivals":
			arrivals = count
		case "/raildepartures":
			departures = count
		}
		if arrivals+departures > MaxRailArrivals {
			return fmt.Errorf("rail plans exceed %d combined events", MaxRailArrivals)
		}
	}
}

func couplingProjectArrayLimit(path string) int {
	switch path {
	case "/network/nodes":
		return MaxNodes
	case "/network/lanes", "/couplingcorridors/*/laneids":
		return MaxLanes
	case "/network/stations":
		return MaxStations
	case "/fleet":
		return MaxPods
	case "/network/stations/*/berths", "/network/stations/*/banks/*/berthids":
		return MaxBerths
	case "/network/stations/*/banks":
		return sim.MaxStationBanks
	case "/network/lanes/*/vehicleclasses", "/network/stations/*/vehicleclasses", "/network/stations/*/berths/*/vehicleclasses":
		return 4
	case "/expressservices":
		return MaxExpressServices
	case "/couplingsites":
		return sim.MaxCouplingSites
	case "/couplingcorridors":
		return sim.MaxCouplingCorridors
	case "/demandprofiles":
		return MaxProfiles
	case "/demandprofiles/*/bands", "/demandprofiles/*/flows/*/weights":
		return MaxBands
	case "/demandprofiles/*/flows":
		return MaxFlows
	case "/railarrivals", "/raildepartures":
		return MaxRailArrivals
	case "/railarrivals/*/destinations", "/raildepartures/*/origins":
		return MaxRailDestinations
	default:
		return 0
	}
}
