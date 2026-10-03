package parkride

import (
	"cmp"
	"container/heap"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// Leg records one pod offer and its actual unloading receipt.
type Leg struct {
	RequestID    int    `json:"requestID"`
	OfferedTick  int64  `json:"offeredTick"`
	AlightedTick int64  `json:"alightedTick"`
	Reason       string `json:"reason,omitempty"`
}

// Record retains a car identity through refusal, completion, or censoring.
type Record struct {
	Itinerary          Itinerary `json:"itinerary"`
	Stage              string    `json:"stage"`
	Outcome            string    `json:"outcome"`
	Held               bool      `json:"held"`
	CarArrivalTick     int64     `json:"carArrivalTick"`
	Outward            Leg       `json:"outward"`
	Return             Leg       `json:"return"`
	ReturnEligibleTick int64     `json:"returnEligibleTick"`
	CarReleaseTick     int64     `json:"carReleaseTick"`
	HomeArrivalTick    int64     `json:"homeArrivalTick"`
	DoorToDoorTicks    *int64    `json:"doorToDoorTicks,omitempty"`
}

// LotState reports car usage without native pod berth ownership.
type LotState struct {
	Lot       Lot   `json:"lot"`
	Occupancy int64 `json:"occupancy"`
	Peak      int64 `json:"peak"`
}

type event struct {
	tick  int64
	index int
	kind  string
}
type events []event

func (e *events) Len() int { return len(*e) }
func (e *events) Less(i, j int) bool {
	return (*e)[i].tick < (*e)[j].tick || (*e)[i].tick == (*e)[j].tick && ((*e)[i].index < (*e)[j].index || (*e)[i].index == (*e)[j].index && (*e)[i].kind < (*e)[j].kind)
}
func (e *events) Swap(i, j int) { (*e)[i], (*e)[j] = (*e)[j], (*e)[i] }

// Push adds one scheduled car or return event.
func (e *events) Push(value any) {
	entry, ok := value.(event)
	if !ok {
		panic("invalid car event")
	}
	*e = append(*e, entry)
}

// Pop removes the event at the heap tail.
func (e *events) Pop() any { last := len(*e) - 1; entry := (*e)[last]; *e = (*e)[:last]; return entry }

type binding struct {
	index     int
	returning bool
}
type podService interface {
	PendingCount() int
	SubmitTripOptions(sim.TripOptions) (int, error)
}

type ledger struct {
	records    []Record
	lots       []LotState
	lotIndexes map[string]int
	times      []times
	bindings   map[int]binding
	carEvents  events
	returns    events
	arrivals   []int
	cursor     int
	lastTick   int64
	terminal   int
	queueLimit int
	horizon    int64
}

func newLedger(plan Plan, queueLimit int, horizon int64) *ledger {
	l := &ledger{records: make([]Record, len(plan.Itineraries)), lots: make([]LotState, len(plan.Lots)),
		lotIndexes: make(map[string]int), times: make([]times, len(plan.Itineraries)), bindings: make(map[int]binding),
		arrivals: make([]int, len(plan.Itineraries)), lastTick: -1, queueLimit: queueLimit, horizon: horizon}
	for index, lot := range plan.Lots {
		l.lots[index].Lot = lot
		l.lotIndexes[lot.ID] = index
	}
	for index, itinerary := range plan.Itineraries {
		l.records[index] = Record{Itinerary: itinerary, Stage: "planned", CarArrivalTick: -1, CarReleaseTick: -1, HomeArrivalTick: -1, ReturnEligibleTick: -1,
			Outward: Leg{OfferedTick: -1, AlightedTick: -1}, Return: Leg{OfferedTick: -1, AlightedTick: -1}}
		l.times[index], _ = compileTimes(itinerary) // The normalized plan already proved these sums.
		l.arrivals[index] = index
		l.carEvents = append(l.carEvents, event{l.times[index].departure, index, "departure"})
	}
	heap.Init(&l.carEvents)
	slices.SortFunc(l.arrivals, func(a, b int) int {
		if order := cmp.Compare(l.times[a].arrival, l.times[b].arrival); order != 0 {
			return order
		}
		return cmp.Compare(a, b)
	})
	return l
}

func (l *ledger) clone() *ledger {
	clone := *l
	clone.records, clone.lots, clone.times = slices.Clone(l.records), slices.Clone(l.lots), slices.Clone(l.times)
	for index := range clone.records {
		if value := clone.records[index].DoorToDoorTicks; value != nil {
			ticks := *value
			clone.records[index].DoorToDoorTicks = &ticks
		}
	}
	clone.lotIndexes, clone.bindings = maps.Clone(l.lotIndexes), maps.Clone(l.bindings)
	clone.carEvents, clone.returns, clone.arrivals = slices.Clone(l.carEvents), slices.Clone(l.returns), slices.Clone(l.arrivals)
	return &clone
}

func (l *ledger) advance(tick int64, completions []sim.StepCompletion, pods podService) error {
	if tick < l.lastTick || tick > l.lastTick+1 || tick < 0 || tick > l.horizon {
		return errors.New("car ledger tick is outside its consecutive run bound")
	}
	offers := tick < l.horizon
	l.lastTick = tick
	for _, completion := range completions {
		if err := l.complete(tick, completion); err != nil {
			return err
		}
	}
	if err := l.finishCars(tick); err != nil {
		return err
	}
	if offers {
		// Drain all due returns in itinerary ID order, including returns made late.
		var due []int
		for len(l.returns) > 0 && l.returns[0].tick <= tick {
			value, ok := heap.Pop(&l.returns).(event)
			if !ok {
				return errors.New("invalid return event")
			}
			due = append(due, value.index)
		}
		slices.Sort(due)
		for _, index := range due {
			if err := l.offer(tick, index, true, pods); err != nil {
				return err
			}
		}
	}
	for l.cursor < len(l.arrivals) && l.times[l.arrivals[l.cursor]].arrival <= tick {
		index := l.arrivals[l.cursor]
		l.cursor++
		record := &l.records[index]
		record.CarArrivalTick = l.times[index].arrival
		lot := &l.lots[l.lotIndexes[record.Itinerary.Lot]]
		if lot.Occupancy >= lot.Lot.Capacity {
			l.end(index, "full-lot")
			continue
		}
		lot.Occupancy++
		lot.Peak = max(lot.Peak, lot.Occupancy)
		record.Held, record.Stage = true, "at-hub"
		if offers {
			if err := l.offer(tick, index, false, pods); err != nil {
				return err
			}
			if err := l.finishCars(tick); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *ledger) complete(tick int64, completion sim.StepCompletion) error {
	receipt, exists := l.bindings[completion.RequestID]
	if !exists {
		return nil
	} // Duplicate receipts and unrelated native orders change nothing.
	record := &l.records[receipt.index]
	leg := &record.Outward
	if receipt.returning {
		leg = &record.Return
	}
	if completion.AlightedTick < leg.OfferedTick || completion.AlightedTick > tick {
		return errors.New("native completion has an invalid unloading tick")
	}
	if leg.AlightedTick >= 0 {
		return nil
	}
	leg.AlightedTick = completion.AlightedTick
	delete(l.bindings, completion.RequestID)
	if receipt.returning {
		return l.startRetrieval(receipt.index, completion.AlightedTick)
	}
	ready, err := addTicks(completion.AlightedTick, l.times[receipt.index].activity)
	if err != nil {
		return err
	}
	record.ReturnEligibleTick = max(ready, l.times[receipt.index].notBefore)
	record.Stage = "activity"
	heap.Push(&l.returns, event{record.ReturnEligibleTick, receipt.index, "return"})
	return nil
}

func (l *ledger) offer(tick int64, index int, returning bool, pods podService) error {
	record := &l.records[index]
	leg := &record.Outward
	lot := l.lots[l.lotIndexes[record.Itinerary.Lot]].Lot
	options := sim.TripOptions{From: lot.Hub, To: record.Itinerary.Destination, PartySize: record.Itinerary.PartySize, SharingConsent: record.Itinerary.SharingConsent}
	if returning {
		leg = &record.Return
		options.From, options.To = options.To, options.From
	}
	if leg.OfferedTick >= 0 {
		return errors.New("pod leg was already offered")
	}
	leg.OfferedTick = tick
	if pods.PendingCount() >= l.queueLimit {
		leg.Reason = "queue-limit"
		if returning {
			l.end(index, "stranded")
			return nil
		}
		return l.startRetrieval(index, tick)
	}
	id, err := pods.SubmitTripOptions(options)
	if err != nil {
		leg.Reason = "submission-error"
		l.end(index, "error")
		return fmt.Errorf("itinerary %q native submission: %w", record.Itinerary.ID, err)
	}
	if _, exists := l.bindings[id]; id <= 0 || exists {
		leg.Reason = "invalid-receipt"
		l.end(index, "error")
		return errors.New("native submission returned an invalid request ID")
	}
	leg.RequestID = id
	l.bindings[id] = binding{index, returning}
	record.Stage = "outward-pod"
	if returning {
		record.Stage = "return-pod"
	}
	return nil
}

func (l *ledger) startRetrieval(index int, tick int64) error {
	release, err := addTicks(tick, l.times[index].retrieval)
	if err != nil {
		return err
	}
	l.records[index].Stage = "retrieval"
	heap.Push(&l.carEvents, event{release, index, "release"})
	return nil
}

func (l *ledger) finishCars(tick int64) error {
	for len(l.carEvents) > 0 && l.carEvents[0].tick <= tick {
		value, ok := heap.Pop(&l.carEvents).(event)
		if !ok {
			return errors.New("invalid car event")
		}
		record := &l.records[value.index]
		switch value.kind {
		case "departure":
			record.Stage = "car-out"
		case "release":
			if !record.Held {
				return errors.New("car release has no held slot")
			}
			lot := &l.lots[l.lotIndexes[record.Itinerary.Lot]]
			if lot.Occupancy < 1 {
				return errors.New("car lot occupancy cannot become negative")
			}
			home, err := addTicks(value.tick, l.times[value.index].homebound)
			if err != nil {
				return err
			}
			lot.Occupancy--
			record.Held, record.CarReleaseTick, record.Stage = false, value.tick, "car-home"
			heap.Push(&l.carEvents, event{home, value.index, "home"})
		case "home":
			record.HomeArrivalTick = value.tick
			if record.Outward.Reason != "" {
				l.end(value.index, "recovered-refusal")
			} else {
				duration := value.tick - l.times[value.index].departure
				record.DoorToDoorTicks = &duration
				l.end(value.index, "completed")
			}
		default:
			return errors.New("unknown car event")
		}
	}
	return nil
}

func (l *ledger) end(index int, outcome string) {
	l.records[index].Stage, l.records[index].Outcome = "terminal", outcome
	l.terminal++
}

func (l *ledger) reports() ([]Record, []LotState) {
	snapshot := l.clone()
	for index := range snapshot.records {
		if snapshot.records[index].Outcome == "" {
			snapshot.records[index].Outcome = "censored"
		}
	}
	return snapshot.records, snapshot.lots
}
