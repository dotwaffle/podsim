package parkride

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

type objectRule struct {
	fields   map[string]byte
	required []string
}

func rule(fields, required string) objectRule {
	r := objectRule{fields: make(map[string]byte), required: strings.Fields(required)}
	for field := range strings.FieldsSeq(fields) {
		parts := strings.Split(field, ":")
		r.fields[parts[0]] = parts[1][0]
	}
	return r
}

// These names freeze the native encoding independently of future sim structs.
var checkpointRules = map[string]objectRule{
	"":                                    rule("format:s version:n checkpointID:s payload:o", "format version checkpointID payload"),
	"/payload":                            rule("runID:s origin:o tick:n phase:s nativeEncoding:s native:o nativeHash:s ledger:o ledgerHash:s observationHash:s traceHash:s", "runID origin tick phase nativeEncoding native nativeHash ledger ledgerHash observationHash traceHash"),
	"/payload/origin":                     rule("project:o plan:o projectHash:s planHash:s horizonTicks:n queueLimit:n reportBuild:s implementation:o", "project plan projectHash planHash horizonTicks queueLimit reportBuild implementation"),
	"/payload/origin/implementation":      rule("sourceRevision:s executableSHA256:s goVersion:s goExperiment:s goOS:s goArch:s", "sourceRevision executableSHA256 goVersion goExperiment goOS goArch"),
	"/payload/origin/plan":                rule("lots:a itineraries:a", "lots itineraries"),
	"/payload/origin/plan/lots/*":         rule("id:s hub:s capacity:n", "id hub capacity"),
	"/payload/origin/plan/itineraries/*":  rule("id:s carID:s lot:s destination:s carSeats:n partySize:n sharingConsent:s departureSeconds:n outwardSeconds:n returnNotBeforeSeconds:n activitySeconds:n retrievalSeconds:n homeboundSeconds:n outwardRefusal:s returnRefusal:s", "id carID lot destination carSeats partySize sharingConsent departureSeconds outwardSeconds returnNotBeforeSeconds activitySeconds retrievalSeconds homeboundSeconds outwardRefusal returnRefusal"),
	"/payload/ledger":                     rule("lastTick:n records:a lots:a", "lastTick records lots"),
	"/payload/ledger/records/*":           rule("stage:s outcome:s held:b carArrivalTick:n outward:o return:o returnEligibleTick:n carReleaseTick:n homeArrivalTick:n doorToDoorTicks:n", "stage outcome held carArrivalTick outward return returnEligibleTick carReleaseTick homeArrivalTick doorToDoorTicks"),
	"/payload/ledger/records/*/outward":   rule("requestID:n offeredTick:n boardedTick:n alightedTick:n reason:s", "requestID offeredTick boardedTick alightedTick reason"),
	"/payload/ledger/records/*/return":    rule("requestID:n offeredTick:n boardedTick:n alightedTick:n reason:s", "requestID offeredTick boardedTick alightedTick reason"),
	"/payload/ledger/lots/*":              rule("occupancy:n peak:n", "occupancy peak"),
	"/payload/native":                     rule("tick:n paused:b completed:n requestID:n boarded:n totalWaitTicks:n maxWaitTicks:n nextRedistributionTick:n passengerDistanceMeters:n emptyDistanceMeters:n rebalanceMoves:n sharedParties:n sharedRidePartyLimit:n sharedRideMode:s sharedRideMaxStops:n sharedRideJoin:s journeys:n totalJourneyTicks:n maxJourneyTicks:n riderDistanceMeters:n directDistanceMeters:n maxDetourRatio:n demo:o demoError:s pods:a waiting:a", "tick completed requestID boarded totalWaitTicks maxWaitTicks nextRedistributionTick passengerDistanceMeters emptyDistanceMeters rebalanceMoves sharedParties sharedRidePartyLimit pods"),
	"/payload/native/demo":                rule("secondSent:b followupsSent:b", ""),
	"/payload/native/pods/*":              rule("boardings:a class:s id:s activity:s stationID:s berthID:s occupied:b relocatingTo:s rebalancing:b rebalanceAfter:n phaseTicks:n origin:s destination:s destinationStation:s riders:a stops:a riddenMeters:n journeyOrigin:s claimsDestination:b released:b stationBuffered:b route:a routeIndex:n laneID:s laneDistance:n distance:n waiting:b waitSince:n platoon:o compactQueue:o", "id activity"),
	"/payload/native/pods/*/riders/*":     rule("sharingConsent:s service:s serviceID:s id:n from:s to:s partySize:n podID:s completed:b requestedTick:n boardedTick:n dispatchReason:s", "sharingConsent service id from to partySize requestedTick"),
	"/payload/native/waiting/*":           rule("request:o route:a boarded:b deferUntil:n deferCheck:n deferPodID:s", "request"),
	"/payload/native/waiting/*/request":   rule("sharingConsent:s service:s serviceID:s id:n from:s to:s partySize:n podID:s completed:b requestedTick:n boardedTick:n dispatchReason:s", "sharingConsent service id from to partySize requestedTick"),
	"/payload/native/pods/*/boardings/*":  rule("BerthID:s MetersAtBoarding:n", "BerthID MetersAtBoarding"),
	"/payload/native/pods/*/platoon":      rule("kind:s terminalCell:n leader:s lane:n leaderLane:n lanes:n turn:n draining:b", "leader lane leaderLane lanes turn"),
	"/payload/native/pods/*/compactQueue": rule("kind:s phase:s lane:s members:a start:n frontier:n stopCells:a speeds:a targets:a landingSpeeds:a", "kind phase lane members start frontier stopCells speeds targets landingSpeeds"),
}

type checkpointScanner struct {
	check          func() error
	decoder        *jsontext.Decoder
	counts         map[string]int64
	compactMembers map[string]bool
}

func scanCheckpoint(ctx context.Context, data []byte) error {
	s := checkpointScanner{check: ctx.Err, decoder: jsontext.NewDecoder(bytes.NewReader(data)), counts: make(map[string]int64), compactMembers: make(map[string]bool)}
	if err := s.value("", 0); err != nil {
		return err
	}
	if _, err := s.decoder.ReadToken(); !errors.Is(err, io.EOF) {
		return errors.New("checkpoint has trailing JSON")
	}
	n, l := s.counts["/payload/origin/plan/itineraries"], s.counts["/payload/origin/plan/lots"]
	if !fitsStorage(l, n) {
		return errors.New("checkpoint origin exceeds retained ledger bound")
	}
	if s.counts["/payload/ledger/records"] != n || s.counts["/payload/ledger/lots"] != l {
		return errors.New("checkpoint ledger count mismatch")
	}
	if s.counts["/payload/native/waiting"]+s.counts["/payload/native/pods/*/riders"] > 2*n {
		return errors.New("native retained requests exceed finite offer bound")
	}
	nodes, lanes := s.counts["/payload/origin/project/network/Nodes"], s.counts["/payload/origin/project/network/Lanes"]
	if s.counts["/payload/native/pods/*/route"] > nodes+lanes || s.counts["/payload/native/waiting/*/route"] > nodes {
		return errors.New("native route exceeds origin network bound")
	}
	if s.counts["/payload/origin/project/railArrivals"]+s.counts["/payload/origin/project/railDepartures"] > project.MaxRailArrivals {
		return errors.New("origin rail plans exceed combined event bound")
	}
	return nil
}
func (s *checkpointScanner) value(path string, depth int) error {
	if err := s.check(); err != nil {
		return err
	}
	if depth > 64 {
		return errors.New("checkpoint JSON exceeds depth bound")
	}
	token, err := s.decoder.ReadToken()
	if err != nil {
		return err
	}
	if path == "/payload/origin/project/version" {
		if token.Kind() != jsontext.KindNumber {
			return errors.New("car continuation requires a numeric foundation project version")
		}
		version, numberErr := token.Int()
		if numberErr != nil || version != project.CurrentVersion {
			return errFoundationProject
		}
	}
	if expected := nativeElementKind(path); expected != 0 && !matchesKind(expected, token.Kind()) {
		return fmt.Errorf("invalid native checkpoint element type at %q", path)
	}
	if path == "/payload/native/pods/*/compactQueue/members/*" {
		id := token.String()
		if s.compactMembers[id] || len(s.compactMembers) >= project.MaxPods {
			return errors.New("native compact members exceed disjoint fleet bound")
		}
		s.compactMembers[id] = true
	}
	switch token.Kind() {
	case jsontext.KindBeginObject:
		err = s.object(path, depth)
	case jsontext.KindBeginArray:
		err = s.array(path, depth)
	case jsontext.KindNull:
		if !strings.HasPrefix(path, "/payload/origin/project/") {
			err = fmt.Errorf("nonnull checkpoint value required at %q", path)
		}
	case jsontext.KindString:
		if len(token.String()) > 1024 {
			err = fmt.Errorf("checkpoint text exceeds bound at %q", path)
		}
	default:
	}
	if err != nil {
		return err
	}
	return nil
}
func (s *checkpointScanner) object(path string, depth int) error {
	r, known := checkpointRules[path]
	if strings.HasPrefix(path, "/payload/native") && !known {
		return fmt.Errorf("unknown native checkpoint object at %q", path)
	}
	seen := make(map[string]bool)
	for s.decoder.PeekKind() != jsontext.KindEndObject {
		token, err := s.decoder.ReadToken()
		if err != nil {
			return err
		}
		if token.Kind() != jsontext.KindString {
			return errors.New("checkpoint object name required")
		}
		key := token.String()
		if len(key) > 256 || len(seen) >= 256 {
			return errors.New("checkpoint object exceeds member bound")
		}
		if seen[key] {
			return fmt.Errorf("duplicate checkpoint member %q", key)
		}
		seen[key] = true
		if key == "orderContract" {
			return errors.New("car continuation does not support orderContract")
		}
		if key == "couplingContract" {
			return errors.New("car continuation does not support couplingContract")
		}
		if known {
			kind, ok := r.fields[key]
			if !ok {
				return fmt.Errorf("unknown checkpoint member %q at %q", key, path)
			}
			if !matchesKind(kind, s.decoder.PeekKind()) {
				return fmt.Errorf("invalid checkpoint member type %q at %q", key, path)
			}
		}
		if err := s.value(path+"/"+key, depth+1); err != nil {
			return err
		}
	}
	if _, err := s.decoder.ReadToken(); err != nil {
		return err
	}
	for _, key := range r.required {
		if !seen[key] {
			return fmt.Errorf("checkpoint needs %s at %q", key, path)
		}
	}
	return nil
}
func matchesKind(expected byte, kind jsontext.Kind) bool {
	switch expected {
	case 's':
		return kind == jsontext.KindString
	case 'n':
		return kind == jsontext.KindNumber
	case 'b':
		return kind == jsontext.KindTrue || kind == jsontext.KindFalse
	case 'o':
		return kind == jsontext.KindBeginObject
	case 'a':
		return kind == jsontext.KindBeginArray
	default:
		return false
	}
}
func (s *checkpointScanner) array(path string, depth int) error {
	limit := arrayLimit(path)
	var count int64
	for s.decoder.PeekKind() != jsontext.KindEndArray {
		count++
		if count > limit {
			return fmt.Errorf("checkpoint array exceeds bound at %q", path)
		}
		if err := s.value(path+"/*", depth+1); err != nil {
			return err
		}
	}
	if _, err := s.decoder.ReadToken(); err != nil {
		return err
	}
	switch path {
	case "/payload/origin/plan/itineraries", "/payload/origin/plan/lots", "/payload/ledger/records", "/payload/ledger/lots", "/payload/native/waiting", "/payload/native/pods/*/riders", "/payload/origin/project/network/Nodes", "/payload/origin/project/network/Lanes", "/payload/origin/project/railArrivals", "/payload/origin/project/railDepartures":
		s.counts[path] += count
	case "/payload/native/pods/*/route", "/payload/native/waiting/*/route":
		s.counts[path] = max(s.counts[path], count)
	}
	return nil
}
func arrayLimit(path string) int64 {
	switch path {
	case "/payload/origin/plan/lots", "/payload/ledger/lots":
		return storageLimit / lotBytes
	case "/payload/origin/plan/itineraries", "/payload/ledger/records":
		return storageLimit / itineraryBytes
	case "/payload/native/pods", "/payload/origin/project/fleet", "/payload/origin/project/network/Stations", "/payload/origin/project/expressServices":
		return project.MaxPods
	case "/payload/native/waiting":
		return 2 * (storageLimit / itineraryBytes)
	case "/payload/native/pods/*/riders", "/payload/native/pods/*/stops", "/payload/native/pods/*/boardings":
		return 8
	case "/payload/native/pods/*/route":
		return project.MaxLanes + project.MaxNodes
	case "/payload/native/waiting/*/route", "/payload/origin/project/network/Nodes":
		return project.MaxNodes
	case "/payload/origin/project/network/Lanes":
		return project.MaxLanes
	case "/payload/origin/project/network/Stations/*/Berths", "/payload/origin/project/network/Stations/*/Banks/*/BerthIDs":
		return project.MaxBerths
	case "/payload/origin/project/network/Stations/*/Banks":
		return sim.MaxStationBanks
	case "/payload/origin/project/network/Lanes/*/VehicleClasses", "/payload/origin/project/network/Stations/*/VehicleClasses", "/payload/origin/project/network/Stations/*/Berths/*/VehicleClasses":
		return 4
	case "/payload/origin/project/railArrivals", "/payload/origin/project/railDepartures":
		return project.MaxRailArrivals
	case "/payload/origin/project/railArrivals/*/destinations", "/payload/origin/project/railDepartures/*/origins":
		return project.MaxRailDestinations
	case "/payload/origin/project/demandProfiles":
		return project.MaxProfiles
	case "/payload/origin/project/demandProfiles/*/bands", "/payload/origin/project/demandProfiles/*/flows/*/weights":
		return project.MaxBands
	case "/payload/origin/project/demandProfiles/*/flows":
		return project.MaxFlows
	case "/payload/native/pods/*/compactQueue/members", "/payload/native/pods/*/compactQueue/stopCells", "/payload/native/pods/*/compactQueue/speeds", "/payload/native/pods/*/compactQueue/targets", "/payload/native/pods/*/compactQueue/landingSpeeds":
		return 4
	default:
		return 65_536
	}
}

// Native array element types are part of the frozen checkpoint encoding.
func nativeElementKind(path string) byte {
	switch path {
	case "/payload/native/pods/*", "/payload/native/waiting/*", "/payload/native/pods/*/riders/*", "/payload/native/pods/*/boardings/*":
		return 'o'
	case "/payload/native/pods/*/stops/*", "/payload/native/pods/*/compactQueue/members/*":
		return 's'
	case "/payload/native/pods/*/route/*", "/payload/native/waiting/*/route/*", "/payload/native/pods/*/compactQueue/stopCells/*", "/payload/native/pods/*/compactQueue/speeds/*", "/payload/native/pods/*/compactQueue/targets/*", "/payload/native/pods/*/compactQueue/landingSpeeds/*":
		return 'n'
	default:
		return 0
	}
}
