package main

import (
	"math"
	"slices"

	"github.com/dotwaffle/podsim/internal/sim"
)

// screenMeasures holds the reported values of one arm that no gate reads.
// The arrival values stop at the end of the arrival window, and the total
// values include the clear, which has a different length in each arm.
type screenMeasures struct {
	// Lines holds the hub events of each line of the screen.
	Lines []screenLineMeasure `json:"lines"`
	// The blocked values count the time of Compact pods with a wait report
	// that names another pod. A pod that only slows behind a pod has no
	// wait report. The values have the resolution of the check interval.
	ArrivalBlockedByExpressSeconds float64 `json:"arrival_blocked_by_express_seconds"`
	ArrivalBlockedByCompactSeconds float64 `json:"arrival_blocked_by_compact_seconds"`
	TotalBlockedByExpressSeconds   float64 `json:"total_blocked_by_express_seconds"`
	TotalBlockedByCompactSeconds   float64 `json:"total_blocked_by_compact_seconds"`
	ArrivalEmptyMeters             float64 `json:"arrival_empty_meters"`
	TotalEmptyMeters               float64 `json:"total_empty_meters"`
	// QueuePeak is the largest sampled number of orders that wait for a
	// pod, and ArrivalQueue is the number at the end of the window.
	QueuePeak    int `json:"queue_peak"`
	ArrivalQueue int `json:"arrival_queue"`
	// The background values cover the background requests that boarded.
	BackgroundWait screenWaitMeasure `json:"background_wait"`
}

// screenLineMeasure holds the hub events of one line.
type screenLineMeasure struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Outbound is from From to To, and Inbound from To to From.
	Outbound screenHubMeasure `json:"outbound"`
	Inbound  screenHubMeasure `json:"inbound"`
	// HubPassengerWaitSeconds is the mean wait of the hub parties of both
	// directions that boarded whole, with each party weighted by its size.
	HubPassengerWaitSeconds float64 `json:"hub_passenger_wait_seconds"`
}

// screenHubMeasure holds the hub events of one direction. The arrival
// values count the parties that finished whole inside the window.
type screenHubMeasure struct {
	Offered           int `json:"offered_passengers"`
	ArrivalPassengers int `json:"arrival_passengers"`
	ArrivalParties    int `json:"arrival_parties"`
	TotalPassengers   int `json:"total_passengers"`
	TotalParties      int `json:"total_parties"`
	// Wait covers the parties that boarded whole.
	Wait screenWaitMeasure `json:"wait"`
}

// screenWaitMeasure summarizes waits in seconds. The percentiles are
// nearest-rank values.
type screenWaitMeasure struct {
	Count   int     `json:"count"`
	Mean    float64 `json:"mean_seconds"`
	Median  float64 `json:"median_seconds"`
	P95     float64 `json:"p95_seconds"`
	Longest float64 `json:"longest_seconds"`
}

// newScreenWaitMeasure summarizes ticks. It sorts ticks in place.
func newScreenWaitMeasure(ticks []int64) screenWaitMeasure {
	if len(ticks) == 0 {
		return screenWaitMeasure{}
	}
	slices.Sort(ticks)
	total := int64(0)
	for _, value := range ticks {
		total += value
	}
	rank := func(share float64) float64 {
		index := int(math.Ceil(share*float64(len(ticks)))) - 1
		return float64(ticks[max(index, 0)]) / sim.TicksPerSecond
	}
	return screenWaitMeasure{Count: len(ticks), Mean: float64(total) / float64(len(ticks)) / sim.TicksPerSecond,
		Median: rank(0.5), P95: rank(0.95), Longest: rank(1)}
}

type screenSample struct {
	arrivalTicks int64
	// ticks is the time that the sample stands for.
	ticks   int64
	classes map[string]sim.VehicleClass
}

// sample adds one observation of state. The samples come in tick order.
func (m *screenMeasures) sample(state sim.Snapshot, input screenSample) {
	seconds := float64(input.ticks) / sim.TicksPerSecond
	express, compact := 0.0, 0.0
	for _, vehicle := range state.Vehicles {
		if vehicle.Pod.WaitReason == sim.NoWait || input.classes[vehicle.Pod.ID] == sim.ExpressClass {
			continue
		}
		class, ok := input.classes[vehicle.Pod.BlockedBy]
		switch {
		case !ok:
		case class == sim.ExpressClass:
			express += seconds
		default:
			compact += seconds
		}
	}
	m.TotalBlockedByExpressSeconds += express
	m.TotalBlockedByCompactSeconds += compact
	m.TotalEmptyMeters = state.EmptyDistanceMeters
	m.QueuePeak = max(m.QueuePeak, len(state.Pending))
	if state.Tick <= input.arrivalTicks {
		m.ArrivalBlockedByExpressSeconds += express
		m.ArrivalBlockedByCompactSeconds += compact
		m.ArrivalEmptyMeters = state.EmptyDistanceMeters
		m.ArrivalQueue = len(state.Pending)
	}
}

// screenFinish gives the parts of a screen that finish needs.
type screenFinish struct {
	arrivalTicks int64
	lines        []screenLine
}

// screenLineTotals collects the hub waits of one line.
type screenLineTotals struct {
	outbound, inbound []int64
	weighted, boarded int64
}

// finish sets the values that come from the outcomes of events.
func (m *screenMeasures) finish(outcomes []screenOutcome, events []screenEvent, input screenFinish) {
	var background []int64
	totals := make([]screenLineTotals, len(input.lines))
	m.Lines = make([]screenLineMeasure, len(input.lines))
	for index, line := range input.lines {
		m.Lines[index].From, m.Lines[index].To = line.from, line.to
	}
	for index, event := range events {
		outcome := outcomes[index]
		if !event.Hub {
			if outcome.WaitTicks >= 0 {
				background = append(background, outcome.WaitTicks)
			}
			continue
		}
		total := &totals[event.Line]
		hub, waits := &m.Lines[event.Line].Inbound, &total.inbound
		if event.Outbound {
			hub, waits = &m.Lines[event.Line].Outbound, &total.outbound
		}
		hub.Offered += event.Size
		if outcome.WaitTicks >= 0 {
			*waits = append(*waits, outcome.WaitTicks)
			total.weighted += outcome.WaitTicks * int64(event.Size)
			total.boarded += int64(event.Size)
		}
		hub.finished(event, outcome, input.arrivalTicks)
	}
	for index, total := range totals {
		line := &m.Lines[index]
		if total.boarded > 0 {
			line.HubPassengerWaitSeconds = float64(total.weighted) / float64(total.boarded) / sim.TicksPerSecond
		}
		line.Outbound.Wait, line.Inbound.Wait = newScreenWaitMeasure(total.outbound), newScreenWaitMeasure(total.inbound)
	}
	m.BackgroundWait = newScreenWaitMeasure(background)
}

// finished counts a party that finished whole.
func (h *screenHubMeasure) finished(event screenEvent, outcome screenOutcome, arrivalTicks int64) {
	if outcome.JourneyTicks < 0 {
		return
	}
	h.TotalParties++
	h.TotalPassengers += event.Size
	if event.Tick+outcome.JourneyTicks <= arrivalTicks {
		h.ArrivalParties++
		h.ArrivalPassengers += event.Size
	}
}

// screenMatched compares the waits of the background requests that boarded
// in both arms of a pair. A positive change is a longer wait in the
// candidate. Longer and Shorter count the changes past the wait limit of
// the gate, with the sides exchanged for Shorter.
type screenMatched struct {
	Count      int     `json:"count"`
	MeanChange float64 `json:"mean_change_seconds"`
	// MedianChange, P95Change, and LargestChange are nearest-rank values.
	MedianChange  float64 `json:"median_change_seconds"`
	P95Change     float64 `json:"p95_change_seconds"`
	LargestChange float64 `json:"largest_change_seconds"`
	Longer        int     `json:"longer"`
	Shorter       int     `json:"shorter"`
}

func newScreenMatched(candidate, reference screenArm, events []screenEvent) screenMatched {
	var changes []int64
	matched := screenMatched{}
	for index, event := range events {
		c, r := candidate.Outcomes[index].WaitTicks, reference.Outcomes[index].WaitTicks
		if event.Hub || c < 0 || r < 0 {
			continue
		}
		changes = append(changes, c-r)
		if screenWaitOver(c, r) {
			matched.Longer++
		}
		if screenWaitOver(r, c) {
			matched.Shorter++
		}
	}
	summary := newScreenWaitMeasure(changes)
	matched.Count, matched.MeanChange, matched.MedianChange = summary.Count, summary.Mean, summary.Median
	matched.P95Change, matched.LargestChange = summary.P95, summary.Longest
	return matched
}

// screenWaitOver reports whether wait is over the wait limit of reference.
func screenWaitOver(wait, reference int64) bool {
	return float64(wait-reference) > max(screenWaitFloor, screenShare*float64(reference))
}
