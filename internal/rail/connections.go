// Package rail tracks pod transfers to scheduled train departures.
package rail

import (
	"container/heap"
	"fmt"
	"maps"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// Counts reports outcomes for all issued outbound offers since reset.
type Counts struct {
	Made       int `json:"made"`
	Missed     int `json:"missed"`
	Unserved   int `json:"unserved"`
	Unresolved int `json:"unresolved"`
}

// Connection retains one outbound offer and its transfer outcome.
// AlightedTick is -1 until actual unloading completes. RequestID is zero
// for a rejected offer. Restore failures retain their positive request ID.
type Connection struct {
	Event         string `json:"event"`
	Passenger     int    `json:"passenger"`
	RequestedTick int64  `json:"requestedTick"`
	From          string `json:"from"`
	To            string `json:"to"`
	RequestID     int    `json:"requestID"`
	AlightedTick  int64  `json:"alightedTick"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason,omitempty"`
}

type offerKey struct {
	event     string
	passenger int
}

type deadline struct {
	tick  int64
	index int
}

type deadlines []deadline

func (d deadlines) Len() int { return len(d) }
func (d deadlines) Less(i, j int) bool {
	return d[i].tick < d[j].tick || d[i].tick == d[j].tick && d[i].index < d[j].index
}
func (d deadlines) Swap(i, j int)   { d[i], d[j] = d[j], d[i] }
func (d *deadlines) Push(value any) { *d = append(*d, value.(deadline)) }
func (d *deadlines) Pop() any {
	last := len(*d) - 1
	value := (*d)[last]
	*d = (*d)[:last]
	return value
}

// Connections owns issued offers. Clones share only immutable departure data.
// Call its methods only while no other goroutine uses it.
type Connections struct {
	departures map[string]project.RailDeparture
	records    []Connection
	issued     map[offerKey]int
	requests   map[int]int
	deadlines  deadlines
	counts     Counts
}

// NewConnections starts an empty ledger for validated project departures.
func NewConnections(departures []project.RailDeparture) *Connections {
	capacity := 0
	for _, departure := range departures {
		capacity += departure.Passengers
	}
	c := &Connections{departures: make(map[string]project.RailDeparture, len(departures)),
		issued: make(map[offerKey]int), requests: make(map[int]int),
		records: make([]Connection, 0, capacity), deadlines: make(deadlines, 0, capacity)}
	for _, departure := range departures {
		departure.Origins = slices.Clone(departure.Origins)
		c.departures[departure.ID] = departure
	}
	return c
}

// Clone returns an independent ledger with identical future behavior.
func (c *Connections) Clone() *Connections {
	if c == nil {
		return nil
	}
	clone := *c
	clone.records = slices.Clone(c.records)
	clone.issued, clone.requests = maps.Clone(c.issued), maps.Clone(c.requests)
	clone.deadlines = slices.Clone(c.deadlines)
	return &clone
}

// Issued reports whether this departure passenger has already been offered.
func (c *Connections) Issued(event string, passenger int) bool {
	_, ok := c.issued[offerKey{event, passenger}]
	return ok
}

// Counts returns maintained outcome counts without exposing mutable storage.
func (c *Connections) Counts() Counts { return c.counts }

// Records returns owned connections in issue order.
func (c *Connections) Records() []Connection { return slices.Clone(c.records) }

// Add records an issued departure offer after its submission attempt.
// Rejected offers use ID zero and reason queue-limit or request-error.
// Invalid or duplicate input changes nothing.
func (c *Connections) Add(offer project.RailServiceOffer, requestID int, reason string) error {
	departure, ok := c.departures[offer.Event]
	if offer.Kind != "departure" || !ok || !validOffer(departure, offer.RailOffer) ||
		offer.DepartureTick != int64(departure.AtSeconds)*sim.TicksPerSecond ||
		offer.WalkingTicks != int64(departure.WalkingSeconds)*sim.TicksPerSecond {
		return fmt.Errorf("invalid departure offer %q passenger %d", offer.Event, offer.Passenger)
	}
	if len(c.records) >= project.MaxRailPassengers || c.Issued(offer.Event, offer.Passenger) {
		return fmt.Errorf("departure offer %q passenger %d exceeds the ledger bound or was already issued", offer.Event, offer.Passenger)
	}
	if requestID < 0 || requestID == 0 && reason != "queue-limit" && reason != "request-error" || requestID > 0 && reason != "" {
		return fmt.Errorf("invalid submission receipt for departure %q", offer.Event)
	}
	if _, exists := c.requests[requestID]; requestID > 0 && exists {
		return fmt.Errorf("duplicate connection request ID %d", requestID)
	}
	record := Connection{Event: offer.Event, Passenger: offer.Passenger, RequestedTick: offer.Tick,
		From: offer.From, To: offer.To, RequestID: requestID, AlightedTick: -1, Outcome: "pending", Reason: reason}
	if requestID == 0 {
		record.Outcome = "unserved"
	}
	c.append(record, departure)
	return nil
}

func validOffer(departure project.RailDeparture, offer project.RailOffer) bool {
	return offer.Passenger >= 1 && offer.Passenger <= departure.Passengers &&
		offer.Tick == project.DepartureOfferTick(departure, offer.Passenger) && offer.To == departure.Station &&
		slices.ContainsFunc(departure.Origins, func(origin project.RailOrigin) bool { return origin.Station == offer.From })
}

func (c *Connections) append(record Connection, departure project.RailDeparture) {
	index := len(c.records)
	c.records = append(c.records, record)
	c.issued[offerKey{record.Event, record.Passenger}] = index
	if record.RequestID > 0 {
		c.requests[record.RequestID] = index
	}
	switch record.Outcome {
	case "pending":
		c.counts.Unresolved++
		heap.Push(&c.deadlines, deadline{tick: int64(departure.AtSeconds) * sim.TicksPerSecond, index: index})
	case "made":
		c.counts.Made++
	case "missed":
		c.counts.Missed++
	case "unserved":
		c.counts.Unserved++
	}
}

// Advance consumes actual completions before scoring departures at this tick.
// Repeated calls consume no offer or completion twice. Missed journeys continue.
func (c *Connections) Advance(tick int64, completions []sim.StepCompletion) {
	for _, completion := range completions {
		index, ok := c.requests[completion.RequestID]
		if !ok {
			continue
		}
		record := &c.records[index]
		if record.AlightedTick == -1 && (record.Outcome == "pending" || record.Outcome == "missed") {
			record.AlightedTick = completion.AlightedTick
		}
	}
	for len(c.deadlines) > 0 && c.deadlines[0].tick <= tick {
		entry := heap.Pop(&c.deadlines).(deadline)
		record := &c.records[entry.index]
		if record.Outcome != "pending" {
			continue
		}
		departure := c.departures[record.Event]
		ready := record.AlightedTick + int64(departure.WalkingSeconds)*sim.TicksPerSecond
		c.counts.Unresolved--
		if record.AlightedTick >= 0 && ready <= entry.tick {
			record.Outcome = "made"
			c.counts.Made++
		} else {
			record.Outcome = "missed"
			c.counts.Missed++
		}
	}
}

// RestoreConnections validates owned saved records against the saved project
// and active requests. It never regenerates past origins from the current seed.
func RestoreConnections(departures []project.RailDeparture, records []Connection, state sim.SavedState, counts Counts) (*Connections, error) {
	if len(records) > project.MaxRailPassengers || len(records) > 0 && len(departures) == 0 {
		return nil, fmt.Errorf("connection ledger exceeds its bound or has no departure plan")
	}
	c := NewConnections(departures)
	bindings, err := savedBindings(state)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		departure, ok := c.departures[record.Event]
		if !ok || c.Issued(record.Event, record.Passenger) ||
			!validOffer(departure, project.RailOffer{Tick: record.RequestedTick, Passenger: record.Passenger, From: record.From, To: record.To}) {
			return nil, fmt.Errorf("invalid or duplicate saved connection %q passenger %d", record.Event, record.Passenger)
		}
		if err := validateRecord(record, departure, state.Tick, state.RequestID, bindings); err != nil {
			return nil, err
		}
		if _, duplicate := c.requests[record.RequestID]; record.RequestID > 0 && duplicate {
			return nil, fmt.Errorf("duplicate saved connection request ID %d", record.RequestID)
		}
		c.append(record, departure)
	}
	if c.counts != counts {
		return nil, fmt.Errorf("saved connection counts do not match the ledger")
	}
	return c, nil
}

type requestBinding struct {
	request sim.SavedRequest
	active  bool
}

func savedBindings(state sim.SavedState) (map[int]requestBinding, error) {
	bindings := make(map[int]requestBinding)
	add := func(request sim.SavedRequest, active bool) error {
		if _, exists := bindings[request.ID]; exists {
			return fmt.Errorf("duplicate connection binding %d", request.ID)
		}
		bindings[request.ID] = requestBinding{request: request, active: active}
		return nil
	}
	// Keep malformed queued entries until explicit restore drop reconciliation.
	for _, trip := range state.Waiting {
		if err := add(trip.Request, true); err != nil {
			return nil, err
		}
	}
	for _, pod := range state.Pods {
		for _, request := range pod.Riders {
			if err := add(request, !request.Completed); err != nil {
				return nil, err
			}
		}
	}
	return bindings, nil
}

func validateRecord(record Connection, departure project.RailDeparture, tick int64, submitted int, bindings map[int]requestBinding) error {
	if record.RequestedTick > tick || record.RequestID < 0 || record.RequestID > submitted ||
		record.AlightedTick < -1 || record.AlightedTick >= 0 && (record.AlightedTick < record.RequestedTick || record.AlightedTick > tick) {
		return fmt.Errorf("invalid times or request ID for connection %q", record.Event)
	}
	departureTick := int64(departure.AtSeconds) * sim.TicksPerSecond
	ready := record.AlightedTick + int64(departure.WalkingSeconds)*sim.TicksPerSecond
	valid := false
	switch record.Outcome {
	case "pending":
		valid = record.RequestID > 0 && record.Reason == "" && tick < departureTick
	case "made":
		valid = record.RequestID > 0 && record.Reason == "" && tick >= departureTick && record.AlightedTick >= 0 && ready <= departureTick
	case "missed":
		valid = record.RequestID > 0 && record.Reason == "" && tick >= departureTick && (record.AlightedTick == -1 || ready > departureTick)
	case "unserved":
		valid = record.AlightedTick == -1 && (record.RequestID == 0 && (record.Reason == "queue-limit" || record.Reason == "request-error") ||
			record.RequestID > 0 && (record.Reason == "restore-drop" || record.Reason == "restore-degraded"))
	}
	if !valid {
		return fmt.Errorf("invalid outcome for connection %q", record.Event)
	}
	binding, bound := bindings[record.RequestID]
	if record.RequestID > 0 && bound {
		request := binding.request
		if request.From != record.From || request.To != record.To || request.RequestedTick != record.RequestedTick ||
			binding.active && (record.AlightedTick >= 0 || record.Outcome == "unserved") {
			return fmt.Errorf("connection %q does not match its saved request", record.Event)
		}
	}
	if record.Outcome == "pending" && record.AlightedTick == -1 && (!bound || !binding.active) {
		return fmt.Errorf("pending connection %q has no active request", record.Event)
	}

	return nil
}

// ReconcileRestore preserves terminal outcomes and actual alighting times.
// Untimed pending connections become unserved only with an explicit receipt.
// Other missing or changed active requests are errors.
func (c *Connections) ReconcileRestore(state sim.SavedState, result sim.RestoreResult) error {
	bindings, err := savedBindings(state)
	if err != nil {
		return err
	}
	changes := make(map[int]string)
	for index, record := range c.records {
		if record.Outcome != "pending" || record.AlightedTick >= 0 {
			continue
		}
		switch {
		case slices.Contains(result.Dropped, record.RequestID):
			changes[index] = "restore-drop"
		case slices.Contains(result.LogicalCompleted, record.RequestID):
			changes[index] = "restore-degraded"
		default:
			if err := validateRecord(record, c.departures[record.Event], state.Tick, state.RequestID, bindings); err != nil {
				return err
			}
		}
	}
	for index, reason := range changes {
		c.records[index].Outcome, c.records[index].Reason = "unserved", reason
		c.counts.Unresolved--
		c.counts.Unserved++
	}
	return nil
}
