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
	case "couplingContract":
		return 1
	case "couplingEnabled":
		return 2
	case "couplingSites":
		return 4
	case "couplingCorridors":
		return 8
	default:
		return 0
	}
}

func scanCouplingMember(decoder *jsontext.Decoder, name string) error {
	switch name {
	case "couplingContract":
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindString || value.String() != string(sim.CompactPairV1CouplingContract) {
			return sim.ErrUnknownCouplingContract
		}
		return nil
	case "couplingEnabled":
		value, err := decoder.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
			return errors.New("coupling enabled must be Boolean")
		}
		return nil
	case "couplingSites":
		return scanCouplingRecords(decoder, true, sim.MaxCouplingSites)
	case "couplingCorridors":
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
		field := name.String()
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
		case "laneId":
			return 2, false, false
		case "startMeters":
			return 4, true, false
		case "endMeters":
			return 8, true, false
		case "frontStagingMeters":
			return 16, true, false
		case "rearStagingMeters":
			return 32, true, false
		}
	} else {
		switch name {
		case "assemblySiteId":
			return 2, false, false
		case "splitSiteId":
			return 4, false, false
		case "laneIds":
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
	if value.Kind() != jsontext.KindString || value.String() == "" || !utf8.ValidString(value.String()) || len(value.String()) > MaxIDLength {
		return fmt.Errorf("coupling ID must contain 1 to %d UTF-8 bytes", MaxIDLength)
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
		parts := strings.Split(string(decoder.StackPointer()), "/")
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
		case "/railArrivals":
			arrivals = count
		case "/railDepartures":
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
	case "/network/lanes", "/couplingCorridors/*/laneIds":
		return MaxLanes
	case "/network/stations":
		return MaxStations
	case "/fleet":
		return MaxPods
	case "/network/stations/*/berths", "/network/stations/*/banks/*/berthIDs":
		return MaxBerths
	case "/network/stations/*/banks":
		return sim.MaxStationBanks
	case "/network/lanes/*/vehicleClasses", "/network/stations/*/vehicleClasses", "/network/stations/*/berths/*/vehicleClasses":
		return 4
	case "/expressServices":
		return MaxExpressServices
	case "/couplingSites":
		return sim.MaxCouplingSites
	case "/couplingCorridors":
		return sim.MaxCouplingCorridors
	case "/demandProfiles":
		return MaxProfiles
	case "/demandProfiles/*/bands", "/demandProfiles/*/flows/*/weights":
		return MaxBands
	case "/demandProfiles/*/flows":
		return MaxFlows
	case "/railArrivals", "/railDepartures":
		return MaxRailArrivals
	case "/railArrivals/*/destinations", "/railDepartures/*/origins":
		return MaxRailDestinations
	default:
		return 0
	}
}
