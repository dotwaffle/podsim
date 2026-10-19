package editormodel

import (
	"cmp"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// MaxObservationRequestBytes is the HTTP state limit of the server plus the
// request envelope.
const MaxObservationRequestBytes = 128<<20 + 64<<10

const (
	observationMaxItems = 65536
	observationMaxText  = 4096 // UTF-16 code units, as in the browser
	orderIDBytes        = 64
	orderTextBytes      = 1024
	maxSafeInteger      = 1<<53 - 1
	idAlphabet          = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789.+-"
	base64Alphabet      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	observationScope    = "Manual live server snapshot, not streaming. May now be stale. Counts are observations, not causal bottleneck findings. Runtime throughput, p99 delay, and future capacity are NOT MEASURED."
	observationLink     = "maneuverStationID, else berth owner, else station-owned lane, else reported stationID; association does not prove physical presence in a station footprint."
)

type observationIdentity struct {
	Epoch           string `json:"epoch"`
	ServerStart     string `json:"serverStart"`
	ProjectRevision int64  `json:"projectRevision"`
}

type observationOccupancy struct {
	Occupied      int `json:"occupied"`
	ReservedEmpty int `json:"reservedEmpty"`
	Free          int `json:"free"`
	Capacity      int `json:"capacity"`
}

type observationReason struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type observationPod struct {
	ID         string  `json:"id"`
	Activity   *string `json:"activity"`
	Phase      *string `json:"phase"`
	WaitReason *string `json:"waitReason"`
	BlockedBy  *string `json:"blockedBy"`
}

// observationStation holds the facts of one station. A nil count or list
// is unavailable.
type observationStation struct {
	ID                    string                `json:"id"`
	Name                  string                `json:"name"`
	Occupancy             *observationOccupancy `json:"occupancy"`
	AssociatedPods        *int                  `json:"associatedPods"`
	IdlePods              *int                  `json:"idlePods"`
	BoardingPods          *int                  `json:"boardingPods"`
	DepartingPods         *int                  `json:"departingPods"`
	BlockedPods           *int                  `json:"blockedPods"`
	WaitReasons           []observationReason   `json:"waitReasons"`
	Pods                  []observationPod      `json:"pods"`
	PendingOriginRequests *int                  `json:"pendingOriginRequests"`
	DispatchReasons       []observationReason   `json:"dispatchReasons"`
	Lines                 []string              `json:"lines"`
}

type observationReport struct {
	Status      string               `json:"status"`
	Reason      string               `json:"reason,omitempty"`
	Identity    *observationIdentity `json:"identity,omitzero"`
	Revision    *int64               `json:"revision,omitzero"`
	Tick        *int64               `json:"tick,omitzero"`
	Stations    []observationStation `json:"stations"`
	Scope       string               `json:"scope,omitempty"`
	Association string               `json:"association,omitempty"`
}

type pendingRequest struct {
	id                                                  float64
	from, to, podID, dispatchReason, serviceID, legFrom string
}

var orderFields = map[string]bool{"sharingConsent": true, "service": true, "serviceID": true, "id": true, "from": true, "legFrom": true, "to": true, "partySize": true, "podID": true, "completed": true, "requestedTick": true, "boardedTick": true, "dispatchReason": true}

// requiredOrderFields have neither omitempty nor omitzero in sim.Request.
var requiredOrderFields = []string{"sharingConsent", "service", "id", "from", "to", "partySize", "podID", "completed", "requestedTick", "dispatchReason"}

func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func compareUTF16(a, b string) int {
	return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
}

func reportText(value any) (string, bool) {
	s, ok := value.(string)
	return s, ok && utf16Length(s) <= observationMaxText
}

func reportID(value any) (string, bool) {
	s, ok := reportText(value)
	return s, ok && s != ""
}

// reportCount accepts a safe integer of the browser that is not negative.
func reportCount(value any) (int64, bool) {
	x, ok := value.(float64)
	if !ok || x < 0 || x > maxSafeInteger || x != math.Trunc(x) {
		return 0, false
	}
	return int64(x), true
}

// unpackOrderText decodes canonical base64 of UTF-8 order text. A leading
// byte order mark stays in the text.
func unpackOrderText(packed any, freeText bool) (string, error) {
	limit := orderIDBytes
	if freeText {
		limit = orderTextBytes
	}
	invalid := errors.New("invalid canonical packed order text")
	value, ok := packed.(string)
	body := strings.TrimRight(value, "=")
	// The decoder skips line breaks, and Strict refuses nonzero padding bits.
	if !ok || len(value) > 4*((limit+2)/3) || len(value)%4 != 0 || strings.Trim(body, base64Alphabet) != "" {
		return "", invalid
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) > limit {
		return "", invalid
	}
	if !utf8.Valid(decoded) {
		return "", errors.New("invalid packed order UTF-8")
	}
	text := string(decoded)
	if freeText {
		for _, r := range text {
			if r <= 0x1f || r >= 0x7f && r <= 0x9f {
				return "", errors.New("order text has a control character")
			}
		}
		return text, nil
	}
	if !idCharacters(text) {
		return "", errors.New("invalid order ID characters")
	}
	return text, nil
}

func unpackPendingRequest(value any) (pendingRequest, error) {
	request, ok := value.(map[string]any)
	for key := range request {
		ok = ok && orderFields[key]
	}
	if !ok {
		return pendingRequest{}, errors.New("unknown or invalid pending order fields")
	}
	missing := slices.ContainsFunc(requiredOrderFields, func(key string) bool { _, present := request[key]; return !present })
	id, idOK := reportCount(request["id"])
	party, partyOK := reportCount(request["partySize"])
	_, tickOK := reportCount(request["requestedTick"])
	consent, service := request["sharingConsent"], request["service"]
	boarded, hasBoarded := request["boardedTick"]
	_, boardedOK := reportCount(boarded)
	if missing || !idOK || id == 0 || !partyOK || party == 0 ||
		consent != "private" && consent != "shared" || service != "on-demand" && service != "express" ||
		request["completed"] != false || !tickOK || hasBoarded && !boardedOK {
		return pendingRequest{}, errors.New("missing or invalid pending order data")
	}
	next := pendingRequest{id: float64(id)}
	fields := []*string{&next.from, &next.to, &next.podID, &next.dispatchReason, &next.serviceID, &next.legFrom}
	for i, key := range []string{"from", "to", "podID", "dispatchReason", "serviceID", "legFrom"} {
		if packed, present := request[key]; present {
			text, err := unpackOrderText(packed, key == "dispatchReason")
			if err != nil {
				return pendingRequest{}, err
			}
			*fields[i] = text
		}
	}
	if next.from == "" || next.to == "" {
		return pendingRequest{}, errors.New("pending order origin or destination is missing")
	}
	return next, nil
}

func reportIdentity(value any) (observationIdentity, bool) {
	epoch, epochOK := reportID(member(value, "epoch"))
	start, startOK := reportID(member(value, "serverStart"))
	revision, revisionOK := reportCount(member(value, "projectRevision"))
	if !epochOK || !startOK || !revisionOK || !idCharacters(epoch) || !idCharacters(start) {
		return observationIdentity{}, false
	}
	return observationIdentity{Epoch: epoch, ServerStart: start, ProjectRevision: revision}, true
}

func idCharacters(s string) bool { return strings.Trim(s, idAlphabet) == "" }

func sameIdentity(a, b any) bool {
	x, xOK := reportIdentity(a)
	y, yOK := reportIdentity(b)
	return xOK && yOK && x == y
}

func unavailableObservations(reason string) observationReport {
	return observationReport{Status: "unavailable", Reason: reason, Stations: []observationStation{}}
}

// decodeStationObservations counts only the facts that one HTTP state
// reply supplies. A missing or malformed source makes its counts
// unavailable, not zero. Only a JSON syntax error is an error.
func decodeStationObservations(raw jsontext.Value) (observationReport, error) {
	var reply any
	if err := json.Unmarshal(raw, &reply); err != nil {
		return observationReport{}, fmt.Errorf("decode live state: %w", err)
	}
	state, topology := object(member(member(reply, "frame"), "state")), object(member(reply, "topology"))
	if !sameIdentity(state, topology) {
		return unavailableObservations("State and topology identity are missing or do not match."), nil
	}
	sim := object(state["simulation"])
	revision, revisionOK := reportCount(state["revision"])
	tick, tickOK := reportCount(sim["tick"])
	if !revisionOK || !tickOK {
		return unavailableObservations("Snapshot revision or simulated tick is unavailable."), nil
	}
	network := object(topology["network"])
	stations, stationList, stationsOK := indexItems(network["stations"])
	lanes, laneList, lanesOK := indexItems(network["lanes"])
	if !stationsOK || !lanesOK || slices.ContainsFunc(laneList, func(lane map[string]any) bool {
		value, present := lane["stationID"]
		owner, ok := reportText(value)
		return present && (!ok || owner != "" && stations[owner] == nil)
	}) {
		return unavailableObservations("Live topology station or lane definitions are unavailable."), nil
	}
	berthOwners := map[string]string{}
	for _, station := range stationList {
		berths, ok := reportList(station["berths"])
		if !ok {
			return unavailableObservations("Live topology berth definitions are unavailable."), nil
		}
		for _, berth := range berths {
			id, ok := reportID(member(berth, "id"))
			if _, repeated := berthOwners[id]; !ok || repeated {
				return unavailableObservations("Live topology has invalid or duplicate berth IDs."), nil
			}
			berthOwners[id] = text(station["id"])
		}
	}
	pods, podIndex, associated := observedPods(sim, stations, lanes, berthOwners)
	podsByStation := map[string][]map[string]any{}
	for _, pod := range pods {
		owner := cmp.Or(text(pod["maneuverStationID"]), berthOwners[text(pod["berthID"])], text(member(lanes[text(pod["laneID"])], "stationID")), text(pod["stationID"]))
		podsByStation[owner] = append(podsByStation[owner], pod)
	}
	pending := observedPending(sim, stations, podIndex)
	dispatchReasons := map[string][]string{}
	for _, request := range pending {
		owner := cmp.Or(request.legFrom, request.from)
		dispatchReasons[owner] = append(dispatchReasons[owner], request.dispatchReason)
	}
	berths, _, berthsOK := indexItems(sim["berths"])
	identity, _ := reportIdentity(state)
	report := observationReport{Status: "snapshot", Identity: &identity, Revision: &revision, Tick: &tick, Stations: []observationStation{}, Scope: observationScope, Association: observationLink}
	for _, station := range stationList {
		id := text(station["id"])
		name, ok := reportText(station["name"])
		if !ok {
			name = id
		}
		entry := observationStation{ID: id, Name: name}
		if berthsOK {
			entry.Occupancy = occupancy(items(station["berths"]), berths)
		}
		if associated {
			entry.podFacts(podsByStation[id])
		}
		if pending != nil {
			entry.PendingOriginRequests, entry.DispatchReasons = new(len(dispatchReasons[id])), frequencies(dispatchReasons[id])
		}
		entry.Lines = stationLines(entry)
		report.Stations = append(report.Stations, entry)
	}
	return report, nil
}

func reportList(value any) ([]any, bool) {
	list, ok := value.([]any)
	return list, ok && len(list) <= observationMaxItems
}

// observedPods gives the pods of the vehicles and their index, and whether
// every pod has a valid association.
func observedPods(sim map[string]any, stations, lanes map[string]map[string]any, berthOwners map[string]string) ([]map[string]any, map[string]map[string]any, bool) {
	vehicles, listed := reportList(sim["vehicles"])
	values := make([]any, len(vehicles))
	for i, vehicle := range vehicles {
		values[i] = member(vehicle, "pod")
	}
	index, pods, indexed := indexItems(values)
	if !listed || !indexed {
		return nil, nil, false
	}
	known := func(set map[string]map[string]any, id string) bool { return id == "" || set[id] != nil }
	for _, pod := range pods {
		station, stationOK := reportText(pod["stationID"])
		berth, berthOK := reportText(pod["berthID"])
		lane, laneOK := reportText(pod["laneID"])
		maneuverValue, hasManeuver := pod["maneuverStationID"]
		maneuver, maneuverOK := reportText(maneuverValue)
		_, owned := berthOwners[berth]
		if !stationOK || !berthOK || !laneOK || hasManeuver && !maneuverOK || !known(stations, maneuver) || berth != "" && !owned || !known(lanes, lane) || !known(stations, station) {
			return nil, index, false
		}
	}
	return pods, index, true
}

// observedPending gives the unpacked pending orders, or nil when they are
// unavailable. The HTTP codec writes an empty queue as null.
func observedPending(sim map[string]any, stations, pods map[string]map[string]any) []pendingRequest {
	value, present := sim["pending"]
	list, ok := reportList(value)
	if !present || value != nil && !ok {
		return nil
	}
	decoded := make([]pendingRequest, 0, len(list))
	ids := map[float64]bool{}
	for _, value := range list {
		request, err := unpackPendingRequest(value)
		if err != nil || ids[request.id] || stations[request.from] == nil || stations[request.to] == nil ||
			request.legFrom != "" && stations[request.legFrom] == nil || request.podID != "" && pods[request.podID] == nil {
			return nil
		}
		ids[request.id] = true
		decoded = append(decoded, request)
	}
	return decoded
}

// occupancy counts each berth once: occupied, else reserved, else free.
func occupancy(stationBerths []any, berths map[string]map[string]any) *observationOccupancy {
	result := observationOccupancy{Capacity: len(stationBerths)}
	for _, value := range stationBerths {
		berth := berths[text(member(value, "id"))]
		occupant, occupantOK := reportText(berth["occupant"])
		reserved, reservedOK := reportText(berth["reservedBy"])
		switch {
		case !occupantOK || !reservedOK:
			return nil
		case occupant != "":
			result.Occupied++
		case reserved != "":
			result.ReservedEmpty++
		default:
			result.Free++
		}
	}
	return &result
}

// podFacts adds the counts of the associated pods. A count is unavailable
// when a pod does not report its source field.
func (s *observationStation) podFacts(pods []map[string]any) {
	s.AssociatedPods = new(len(pods))
	countWhere := func(key string, match func(string) bool) *int {
		n := 0
		for _, pod := range pods {
			value, ok := reportText(pod[key])
			if !ok {
				return nil
			}
			if match(value) {
				n++
			}
		}
		return &n
	}
	s.IdlePods = countWhere("activity", func(v string) bool { return v == "Ready" })
	s.BoardingPods = countWhere("activity", func(v string) bool { return v == "Boarding" })
	s.DepartingPods = countWhere("stationPhase", func(v string) bool { return v == "Departing berth" || v == "Exiting station" })
	s.BlockedPods = countWhere("waitReason", func(v string) bool { return v != "" })
	if s.BlockedPods != nil {
		reasons := []string{}
		for _, pod := range pods {
			if reason := text(pod["waitReason"]); reason != "" {
				reasons = append(reasons, reason)
			}
		}
		s.WaitReasons = frequencies(reasons)
	}
	optional := func(value any) *string {
		if s, ok := reportText(value); ok {
			return &s
		}
		return nil
	}
	s.Pods = make([]observationPod, 0, len(pods))
	for _, pod := range pods {
		phase, hasPhase := pod["stationPhase"]
		entry := observationPod{ID: text(pod["id"]), Activity: optional(pod["activity"]), Phase: new("Not reported"), WaitReason: optional(pod["waitReason"]), BlockedBy: optional(pod["blockedBy"])}
		if hasPhase {
			entry.Phase = optional(phase)
		}
		s.Pods = append(s.Pods, entry)
	}
}

// frequencies counts each reason, in the UTF-16 order of the browser.
func frequencies(reasons []string) []observationReason {
	counts := map[string]int{}
	for _, reason := range reasons {
		counts[reason]++
	}
	result := make([]observationReason, 0, len(counts))
	for reason, count := range counts {
		result = append(result, observationReason{Reason: reason, Count: count})
	}
	slices.SortFunc(result, func(a, b observationReason) int { return compareUTF16(a.Reason, b.Reason) })
	return result
}

// stationLines gives the panel text of a station, with at most 100 reason
// groups and 100 pods.
func stationLines(station observationStation) []string {
	number := func(value *int) string {
		if value == nil {
			return "Unavailable"
		}
		return strconv.Itoa(*value)
	}
	show := func(s *string, empty string) string {
		if s == nil {
			return "Unavailable"
		}
		return cmp.Or(*s, empty)
	}
	lines := []string{"Berth occupancy: Unavailable."}
	if o := station.Occupancy; o != nil {
		lines[0] = fmt.Sprintf("Berths: %d occupied, %d reserved-empty, %d free; capacity %d.", o.Occupied, o.ReservedEmpty, o.Free, o.Capacity)
	}
	lines = append(lines,
		fmt.Sprintf("Associated pods: %s. Ready (idle): %s. Boarding: %s. Departing berth or exiting station: %s. Nonempty reported wait reason: %s. Categories can overlap.",
			number(station.AssociatedPods), number(station.IdlePods), number(station.BoardingPods), number(station.DepartingPods), number(station.BlockedPods)),
		fmt.Sprintf("Pending origin requests (not started boarding): %s. These are requests, not passenger counts.", number(station.PendingOriginRequests)),
		reasonLine("Pod wait reasons", station.WaitReasons), reasonLine("Pending dispatch reasons", station.DispatchReasons))
	if station.Pods == nil {
		return append(lines, "Pod details: Unavailable.")
	}
	for _, pod := range station.Pods[:min(len(station.Pods), 100)] {
		lines = append(lines, fmt.Sprintf("Pod %s: activity %s; phase %s; wait %s; blockedBy %s.", pod.ID, show(pod.Activity, ""), show(pod.Phase, ""), show(pod.WaitReason, "none reported"), show(pod.BlockedBy, "none reported")))
	}
	if len(station.Pods) > 100 {
		lines = append(lines, fmt.Sprintf("Only the first 100 of %d associated pods are listed; counts include all associated pods.", len(station.Pods)))
	}
	return lines
}

func reasonLine(label string, reasons []observationReason) string {
	if reasons == nil {
		return label + ": Unavailable."
	}
	parts := make([]string, 0, min(len(reasons), 100))
	for _, entry := range reasons[:min(len(reasons), 100)] {
		parts = append(parts, fmt.Sprintf("%s: %d", cmp.Or(entry.Reason, "No reason reported"), entry.Count))
	}
	line := label + ": " + cmp.Or(strings.Join(parts, "; "), "none reported") + "."
	if len(reasons) > 100 {
		line += " Only the first 100 reason groups are shown."
	}
	return line
}

// observationScopeText tells how a station observation relates to the
// draft: identity is the report identity, base the draft base identity, and
// unchanged whether the draft equals its live baseline.
func observationScopeText(raw jsontext.Value) (string, error) {
	var scope struct {
		Identity  any  `json:"identity"`
		Base      any  `json:"base"`
		Unchanged bool `json:"unchanged"`
	}
	if raw.Kind() != '{' {
		return "", errors.New("observation scope needs an object")
	}
	if err := json.Unmarshal(raw, &scope, json.RejectUnknownMembers(true)); err != nil {
		return "", fmt.Errorf("decode observation scope: %w", err)
	}
	switch {
	case !sameIdentity(scope.Identity, scope.Base):
		return "Live snapshot identity differs from the draft base or is unavailable. No live data is mapped onto the draft.", nil
	case scope.Unchanged:
		return "Snapshot matches the draft base identity. Observations still describe only the server snapshot.", nil
	default:
		return "Unsaved draft differs from its live baseline. Observations describe only the server snapshot, not this draft.", nil
	}
}
