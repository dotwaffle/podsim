package session

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

type couplingScan struct {
	recognized, packed bool
	pods, groupCount   int
}

func couplingMember(name string) bool {
	switch name {
	case "couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors", "couplingGroups":
		return true
	}
	return false
}

// scanCouplingJSON checks the coupling members of a saved state, or of a
// topology when topology is true, before typed allocation. Native restore
// checks geometry. A saved state with a coupling member needs the coupling
// markers of the root, the project and the simulation.
func scanCouplingJSON(data []byte, topology bool) (scan couplingScan, err error) {
	d := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true))
	seen := make(map[string]bool, 8)
	rootClosed := false
	for {
		token, readErr := d.ReadToken()
		if errors.Is(readErr, io.EOF) {
			if !rootClosed {
				return scan, errors.New("incomplete coupling JSON")
			}
			if !topology && scan.recognized {
				for _, path := range []string{"/couplingContract", "/project/couplingContract", "/simulation/couplingContract"} {
					if !seen[path] {
						return scan, errors.New("saved coupling contract marker is missing")
					}
				}
			} else if topology && scan.recognized && !seen["/couplingContract"] {
				return scan, errors.New("topology coupling contract marker is missing")
			}
			if scan.groupCount > scan.pods/2 && !topology {
				return scan, errors.New("saved coupling groups exceed half the fleet")
			}
			return scan, nil
		}
		if readErr != nil {
			return scan, readErr
		}
		depth := d.StackDepth()
		if depth > 64 {
			return scan, errJSONTooDeep
		}
		kind, n := d.StackIndex(depth)
		if kind == jsontext.KindBeginObject && (n+1)/2 > 256 {
			return scan, errJSONObjectTooLong
		}
		if kind == jsontext.KindBeginArray && n > 65_536 {
			return scan, errJSONArrayTooLong
		}
		if depth == 0 && token.Kind() == jsontext.KindEndObject {
			if rootClosed {
				return scan, errors.New("multiple coupling JSON values")
			}
			rootClosed = true
		}
		path := string(d.StackPointer())
		parts := strings.Split(path, "/")
		if token.Kind() == jsontext.KindBeginObject && len(parts) == 4 && parts[1] == "simulation" && parts[2] == "pods" {
			scan.pods++
			if scan.pods > project.MaxPods {
				return scan, errJSONArrayTooLong
			}
		}
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || n%2 != 1 {
			continue
		}
		if path == "/orderContract" {
			value, e := d.ReadToken()
			if e != nil {
				return scan, e
			}
			scan.packed = value.Kind() == jsontext.KindString && value.String() == string(sim.ExpressOrderContract)
			continue
		}
		if !couplingMember(token.String()) {
			continue
		}
		valid := len(parts) == 2 || !topology && len(parts) == 3 && (parts[1] == "project" || parts[1] == "simulation")
		if !valid {
			continue
		}
		scan.recognized = true
		if seen[path] {
			return scan, errors.New("duplicate coupling member")
		}
		seen[path] = true
		switch parts[len(parts)-1] {
		case "couplingContract":
			value, e := d.ReadToken()
			if e != nil {
				return scan, e
			}
			if value.Kind() != jsontext.KindString || value.String() != string(sim.CompactPairV1CouplingContract) {
				return scan, sim.ErrUnknownCouplingContract
			}
		case "couplingEnabled":
			value, e := d.ReadToken()
			if e != nil {
				return scan, e
			}
			if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
				return scan, errors.New("coupling enabled must be Boolean")
			}
		case "couplingGroups":
			count, e := scanCouplingRecords(d, "group", project.MaxPods/2)
			if e != nil {
				return scan, e
			}
			scan.groupCount = count
		case "couplingSites", "couplingCorridors":
			record := "corridor"
			if parts[len(parts)-1] == "couplingSites" {
				record = "site"
			}
			if _, e := scanCouplingRecords(d, record, project.MaxStations); e != nil {
				return scan, e
			}
		}
	}
}

func scanCouplingRecords(d *jsontext.Decoder, record string, limit int) (int, error) {
	start, err := d.ReadToken()
	if err != nil {
		return 0, err
	}
	if start.Kind() != jsontext.KindBeginArray {
		return 0, errors.New("coupling registry must be an array")
	}
	count := 0
	for d.PeekKind() != jsontext.KindEndArray {
		if count == limit {
			return count, errJSONArrayTooLong
		}
		if recordErr := scanCouplingRecord(d, record); recordErr != nil {
			return count, recordErr
		}
		count++
	}
	_, err = d.ReadToken()
	return count, err
}

func scanCouplingRecord(d *jsontext.Decoder, record string) error {
	fields := couplingRecordFields(record)
	start, err := d.ReadToken()
	if err != nil {
		return err
	}
	if start.Kind() != jsontext.KindBeginObject {
		return errors.New("coupling record must be an object")
	}
	var seen uint16
	for d.PeekKind() != jsontext.KindEndObject {
		name, err := d.ReadToken()
		if err != nil {
			return err
		}
		field := name.String()
		index := -1
		for i, candidate := range fields {
			if candidate == field {
				index = i
				break
			}
		}
		if name.Kind() != jsontext.KindString || index < 0 {
			return errors.New("unknown coupling record member")
		}
		bit := uint16(1) << index
		if seen&bit != 0 {
			return errors.New("duplicate coupling record member")
		}
		seen |= bit
		if err := scanCouplingValue(d, field); err != nil {
			return err
		}
	}
	if _, err := d.ReadToken(); err != nil {
		return err
	}
	if seen != (uint16(1)<<len(fields))-1 {
		return errors.New("coupling record has missing members")
	}
	return nil
}

func couplingRecordFields(record string) []string {
	switch record {
	case "group":
		return []string{"id", "members", "formationTick", "corridorID", "assemblySiteID", "splitSiteID", "phase", "dwellTicks", "progress"}
	case "site":
		return []string{"id", "laneId", "startMeters", "endMeters", "frontStagingMeters", "rearStagingMeters"}
	case "corridor":
		return []string{"id", "assemblySiteId", "splitSiteId", "laneIds"}
	default:
		return []string{"leg", "drainFirstMember"}
	}
}

func scanCouplingValue(d *jsontext.Decoder, field string) error {
	switch field {
	case "progress":
		return scanCouplingRecord(d, "progress")
	case "members", "laneIds":
		limit := project.MaxLanes
		if field == "members" {
			limit = 2
		}
		start, err := d.ReadToken()
		if err != nil {
			return err
		}
		if start.Kind() != jsontext.KindBeginArray {
			return errors.New("coupling IDs must be an array")
		}
		count := 0
		for d.PeekKind() != jsontext.KindEndArray {
			if count == limit {
				return errJSONArrayTooLong
			}
			if err := scanCouplingID(d); err != nil {
				return err
			}
			count++
		}
		if _, err := d.ReadToken(); err != nil {
			return err
		}
		if field == "members" && count != 2 || field == "laneIds" && count == 0 {
			return errors.New("coupling ID array has an invalid length")
		}
		return nil
	case "formationTick", "dwellTicks", "leg", "drainFirstMember":
		token, err := d.ReadToken()
		if err != nil {
			return err
		}
		if token.Kind() != jsontext.KindNumber {
			return errors.New("coupling progress must be an integer")
		}
		if field == "formationTick" {
			_, err = token.Int()
		} else {
			_, err = strconv.Atoi(token.String())
		}
		return err
	case "startMeters", "endMeters", "frontStagingMeters", "rearStagingMeters":
		token, err := d.ReadToken()
		if err != nil {
			return err
		}
		if token.Kind() != jsontext.KindNumber {
			return errors.New("coupling position must be a finite number")
		}
		_, err = strconv.ParseFloat(token.String(), 64)
		return err
	default:
		return scanCouplingID(d)
	}
}

func scanCouplingID(d *jsontext.Decoder) error {
	value, err := d.ReadValue()
	if err != nil {
		return err
	}
	if value.Kind() != jsontext.KindString || len(value) > 6*64+2 {
		return errors.New("coupling ID exceeds its bounded string shape")
	}
	var id string
	if err := json.Unmarshal(value, &id); err != nil {
		return err
	}
	if id == "" || len(id) > 64 {
		return fmt.Errorf("coupling ID must contain 1 to %d UTF-8 bytes", 64)
	}
	return nil
}
