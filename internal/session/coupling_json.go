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

// couplingScanner holds the state of scanCouplingJSON between tokens.
// The decoder is not a field, so that seen does not escape with it.
type couplingScanner struct {
	topology bool
	// seen holds the path of each coupling member that the scan checked.
	seen       map[string]bool
	rootClosed bool
	scan       couplingScan
}

// scanCouplingJSON checks the coupling members of a saved state, or of a
// topology when topology is true, before typed allocation. Native restore
// checks geometry. A saved state with a coupling member needs the coupling
// markers of the root, the project and the simulation.
func scanCouplingJSON(data []byte, topology bool) (couplingScan, error) {
	d := jsontext.NewDecoder(bytes.NewReader(data), jsontext.AllowDuplicateNames(true))
	s := couplingScanner{topology: topology, seen: make(map[string]bool, 8)}
	for {
		token, err := d.ReadToken()
		if errors.Is(err, io.EOF) {
			return s.scan, s.finish()
		}
		if err != nil {
			return s.scan, err
		}
		container, n, err := s.checkToken(d, token)
		if err != nil {
			return s.scan, err
		}
		path := string(d.StackPointer())
		parts := strings.Split(path, "/")
		if err := s.countPod(token, parts); err != nil {
			return s.scan, err
		}
		// Only the name of an object member can name a checked member.
		if token.Kind() != jsontext.KindString || container != jsontext.KindBeginObject || n%2 != 1 {
			continue
		}
		if err := s.member(d, token, path, parts); err != nil {
			return s.scan, err
		}
	}
}

// finish checks the contract markers and the group count at the end of
// the input.
func (s *couplingScanner) finish() error {
	if !s.rootClosed {
		return errors.New("incomplete coupling JSON")
	}
	if s.scan.recognized && !s.markersPresent() {
		if s.topology {
			return errors.New("topology coupling contract marker is missing")
		}
		return errors.New("saved coupling contract marker is missing")
	}
	if s.scan.groupCount > s.scan.pods/2 && !s.topology {
		return errors.New("saved coupling groups exceed half the fleet")
	}
	return nil
}

// markersPresent reports whether the input has each coupling contract
// marker that a topology or a saved state needs.
func (s *couplingScanner) markersPresent() bool {
	if s.topology {
		return s.seen["/couplingContract"]
	}
	for _, path := range []string{"/couplingContract", "/project/couplingContract", "/simulation/couplingContract"} {
		if !s.seen[path] {
			return false
		}
	}
	return true
}

// checkToken checks the depth and the length of the container of the
// token that d read last, and refuses a second root object. It returns
// the kind and the length of the container.
func (s *couplingScanner) checkToken(d *jsontext.Decoder, token jsontext.Token) (jsontext.Kind, int64, error) {
	depth := d.StackDepth()
	if depth > 64 {
		return 0, 0, errJSONTooDeep
	}
	container, n := d.StackIndex(depth)
	if container == jsontext.KindBeginObject && (n+1)/2 > 256 {
		return 0, 0, errJSONObjectTooLong
	}
	if container == jsontext.KindBeginArray && n > 65_536 {
		return 0, 0, errJSONArrayTooLong
	}
	if depth == 0 && token.Kind() == jsontext.KindEndObject {
		if s.rootClosed {
			return 0, 0, errors.New("multiple coupling JSON values")
		}
		s.rootClosed = true
	}
	return container, n, nil
}

// countPod counts the start of each object in simulation.pods.
func (s *couplingScanner) countPod(token jsontext.Token, parts []string) error {
	if token.Kind() != jsontext.KindBeginObject || len(parts) != 4 || parts[1] != "simulation" || parts[2] != "pods" {
		return nil
	}
	s.scan.pods++
	if s.scan.pods > project.MaxPods {
		return errJSONArrayTooLong
	}
	return nil
}

// member reads the value of the member name at path. It records the
// order marker and checks a coupling member at a permitted path. It
// ignores other members.
func (s *couplingScanner) member(d *jsontext.Decoder, name jsontext.Token, path string, parts []string) error {
	if path == "/orderContract" {
		value, err := d.ReadToken()
		if err != nil {
			return err
		}
		s.scan.packed = value.Kind() == jsontext.KindString && value.String() == string(sim.ExpressOrderContract)
		return nil
	}
	if !couplingMember(name.String()) {
		return nil
	}
	valid := len(parts) == 2 || !s.topology && len(parts) == 3 && (parts[1] == "project" || parts[1] == "simulation")
	if !valid {
		return nil
	}
	s.scan.recognized = true
	if s.seen[path] {
		return errors.New("duplicate coupling member")
	}
	s.seen[path] = true
	return s.memberValue(d, parts[len(parts)-1])
}

// memberValue reads and checks the value of the coupling member name.
func (s *couplingScanner) memberValue(d *jsontext.Decoder, name string) error {
	switch name {
	case "couplingContract":
		value, err := d.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindString || value.String() != string(sim.CompactPairV1CouplingContract) {
			return sim.ErrUnknownCouplingContract
		}
	case "couplingEnabled":
		value, err := d.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindTrue && value.Kind() != jsontext.KindFalse {
			return errors.New("coupling enabled must be Boolean")
		}
	case "couplingGroups":
		count, err := scanCouplingRecords(d, "group", project.MaxPods/2)
		if err != nil {
			return err
		}
		s.scan.groupCount = count
	case "couplingSites":
		_, err := scanCouplingRecords(d, "site", project.MaxStations)
		return err
	case "couplingCorridors":
		_, err := scanCouplingRecords(d, "corridor", project.MaxStations)
		return err
	}
	return nil
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
