package main

import (
	"encoding/csv"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"slices"
	"strconv"
	"text/tabwriter"

	"github.com/dotwaffle/podsim/internal/sim"
)

type result struct {
	Energy                         *energyReport      `json:"energy,omitempty"`
	SharingConsent                 sim.SharingConsent `json:"sharing_consent"`
	Pattern                        string             `json:"pattern"`
	DemandProfile                  string             `json:"demand_profile,omitempty"`
	DemandBand                     string             `json:"demand_band,omitempty"`
	DailyStartMinute               *int               `json:"daily_start_minute,omitempty"`
	RequestEverySeconds            float64            `json:"request_every_seconds"`
	OfferedPerMinute               float64            `json:"offered_per_minute"`
	BurstSize                      int                `json:"burst_size"`
	Seed                           int64              `json:"seed"`
	Policy                         string             `json:"policy"`
	SharedRidePartyLimit           int                `json:"shared_ride_party_limit"`
	SharingMode                    string             `json:"sharing_mode"`
	SharingJoin                    string             `json:"sharing_join,omitempty"`
	OnboardPickups                 string             `json:"onboard_pickups,omitempty"`
	SharedParties                  int                `json:"shared_parties"`
	FullPodRefusals                int                `json:"full_pod_refusals"`
	FullDepartures                 int                `json:"full_departures"`
	DepartureBacklog               int                `json:"departure_backlog"`
	DeparturesDemandOverFour       int                `json:"departures_demand_over_four"`
	DeparturesOverFourAboard       int                `json:"departures_over_four_aboard"`
	JoinEligibleAssigned           int                `json:"join_eligible_assigned"`
	JoinEligibleExistingStop       int                `json:"join_eligible_existing_stop"`
	JoinEligibleAddedStopOnly      int                `json:"join_eligible_added_stop_only"`
	ReassignedParties              int                `json:"reassigned_parties"`
	RoutingPolicy                  string             `json:"routing_policy"`
	WaitRule                       string             `json:"wait_rule,omitempty"`
	PlatoonPolicy                  string             `json:"platoon_policy,omitempty"`
	PickupReassignment             string             `json:"pickup_reassignment,omitempty"`
	PickupReassignmentStats        *pickupPolicyStats `json:"pickup_reassignment_stats,omitempty"`
	FocusStation                   string             `json:"focus_station"`
	WindowStartSeconds             float64            `json:"window_start_seconds"`
	WindowEndSeconds               float64            `json:"window_end_seconds"`
	ActualEndSeconds               float64            `json:"actual_end_seconds"`
	ArrivalWindowSeconds           float64            `json:"arrival_window_seconds"`
	ArrivalEndSeconds              float64            `json:"arrival_end_seconds"`
	ScheduleID                     string             `json:"schedule_id"`
	Scheduled                      int                `json:"scheduled"`
	Served                         int                `json:"served"`
	Remaining                      int                `json:"remaining"`
	Skipped                        int                `json:"skipped"`
	RailConnections                *connectionReport  `json:"rail_connections,omitempty"`
	RailSkippedOffers              []int              `json:"rail_skipped_offers,omitempty"`
	CompletedAtArrivalEnd          int                `json:"completed_at_arrival_end"`
	BacklogAtArrivalEnd            int                `json:"backlog_at_arrival_end"`
	ArrivalThroughputPerMinute     float64            `json:"arrival_throughput_per_minute"`
	CompletedAtArrivalMidpoint     int                `json:"completed_at_arrival_midpoint"`
	BacklogAtArrivalMidpoint       int                `json:"backlog_at_arrival_midpoint"`
	LateArrivalThroughputPerMinute float64            `json:"late_arrival_throughput_per_minute"`
	LateBacklogChange              int                `json:"late_backlog_change"`
	Drained                        bool               `json:"drained"`
	DrainSeconds                   float64            `json:"drain_seconds"`
	PeakPending                    int                `json:"peak_pending"`
	PeakOutstanding                int                `json:"peak_outstanding"`
	PeakActiveVehicles             int                `json:"peak_active_vehicles"`
	PeakPassengerVehicles          int                `json:"peak_passenger_vehicles"`
	PeakStoppedVehicles            int                `json:"peak_stopped_vehicles"`
	PeakFocusApproaching           int                `json:"peak_focus_approaching"`
	PeakFocusEntranceStopped       int                `json:"peak_focus_entrance_stopped"`
	PeakFocusExitStopped           int                `json:"peak_focus_exit_stopped"`
	PeakFocusOccupiedBerths        int                `json:"peak_focus_occupied_berths"`
	PeakFocusReservedEmptyBerths   int                `json:"peak_focus_reserved_empty_berths"`
	StoppedPodSeconds              float64            `json:"stopped_pod_seconds"`
	JunctionWaitSeconds            float64            `json:"junction_wait_seconds"`
	TrackWaitSeconds               float64            `json:"track_wait_seconds"`
	PeakNodeThroughputPerMinute    int                `json:"peak_node_throughput_per_minute"`
	PeakNode                       string             `json:"peak_node"`
	QueueCleared                   bool               `json:"queue_cleared"`
	QueueClearSeconds              float64            `json:"queue_clear_seconds"`
	WaitAverageSeconds             float64            `json:"wait_average_seconds"`
	WaitMaximumSeconds             float64            `json:"wait_maximum_seconds"`
	WaitP95Seconds                 float64            `json:"wait_p95_seconds"`
	JourneyAverageSeconds          float64            `json:"journey_average_seconds"`
	JourneyP95Seconds              float64            `json:"journey_p95_seconds"`
	JourneyMaximumSeconds          float64            `json:"journey_maximum_seconds"`
	PassengerDistanceMeters        float64            `json:"passenger_distance_meters"`
	EmptyDistanceMeters            float64            `json:"empty_distance_meters"`
	LoadedDistancePercent          float64            `json:"loaded_distance_percent"`
	Occupancy                      float64            `json:"occupancy"`
	RiderDistanceMeters            float64            `json:"rider_distance_meters"`
	DirectDistanceMeters           float64            `json:"direct_distance_meters"`
	DetourRatioMean                float64            `json:"detour_ratio_mean"`
	DetourRatioMax                 float64            `json:"detour_ratio_max"`
	IntermediateStops              int                `json:"intermediate_stops"`
	PositioningMoveCount           int                `json:"positioning_moves"`
	PlatoonTimePercent             float64            `json:"platoon_time_percent"`
}

type writeReportInput struct {
	sharingConsentColumn bool
	output               io.Writer
	format               string
	results              []result
	// waitRuleColumn adds the wait rule to table and CSV output. JSON output
	// has the wait_rule field only when a result has a wait rule.
	waitRuleColumn bool
	// platoonColumn adds the platoon policy to table and CSV output. JSON
	// output has the platoon_policy field only when a result has a platoon
	// policy.
	platoonColumn bool
	// sharingJoinColumn adds the join policy to table and CSV output. JSON
	// output has the sharing_join field only when a result has a join
	// policy.
	sharingJoinColumn    bool
	onboardPickupsColumn bool
	// seatColumns adds the seat screen columns to CSV output. JSON output
	// always has them.
	seatColumns              bool
	pickupReassignmentColumn bool
}

func writeReport(input writeReportInput) error {
	input.results = slices.Clone(input.results)
	for i := range input.results {
		consent, err := comparisonConsent(input.results[i].SharingConsent)
		if err != nil {
			return err
		}
		input.results[i].SharingConsent = consent
	}
	switch input.format {
	case "json":
		version := 12
		if slices.ContainsFunc(input.results, func(outcome result) bool {
			return outcome.PickupReassignment != ""
		}) {
			version = 13
		}
		if slices.ContainsFunc(input.results, func(outcome result) bool { return outcome.OnboardPickups != "" }) {
			version = 14
		}
		if slices.ContainsFunc(input.results, func(outcome result) bool { return outcome.Energy != nil }) {
			version = 15
		}
		data, err := jsonv2.Marshal(report{SchemaVersion: version, Results: input.results}, json.DefaultOptionsV1(), jsontext.WithIndent("  "))
		if err == nil {
			_, err = input.output.Write(append(data, '\n'))
		}
		if err != nil {
			return fmt.Errorf("write JSON report: %w", err)
		}
		return nil
	case "csv":
		return writeCSV(input)
	default:
		return writeTable(input)
	}
}

// writeTable writes one aligned row for each result. When
// input.waitRuleColumn is set, a WAIT RULE column follows POLICY. When
// input.platoonColumn is set, a PLATOON column follows them. When
// input.sharingJoinColumn is set, a JOIN column follows them.
func writeTable(input writeReportInput) error {
	output, results := input.output, input.results
	if len(results) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(output, "window: %.2fs to %.2fs / arrivals end: %.2fs / focus: %s\n", results[0].WindowStartSeconds, results[0].WindowEndSeconds, results[0].ArrivalEndSeconds, results[0].FocusStation); err != nil {
		return fmt.Errorf("write table window: %w", err)
	}
	for _, outcome := range results {
		if outcome.DailyStartMinute != nil {
			if _, err := fmt.Fprintf(output, "daily profile: %s / start minute: %d / seed: %d / policy: %s\n", outcome.DemandProfile, *outcome.DailyStartMinute, outcome.Seed, outcome.Policy); err != nil {
				return fmt.Errorf("write daily clock: %w", err)
			}
		}
	}
	w := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	policyHeader := "POLICY"
	if input.sharingConsentColumn {
		policyHeader += "\tSHARING CONSENT"
	}
	if input.waitRuleColumn {
		policyHeader += "\tWAIT RULE"
	}
	if input.platoonColumn {
		policyHeader += "\tPLATOON"
	}
	if input.sharingJoinColumn {
		policyHeader += "\tJOIN"
	}
	if input.onboardPickupsColumn {
		policyHeader += "\tONBOARD PICKUPS"
	}
	if input.pickupReassignmentColumn {
		policyHeader += "\tREASSIGN"
	}
	if _, err := fmt.Fprintln(w, "PATTERN\tBAND\tLOAD (S)\tOFFERED/M\tARRIVAL/M\tLATE/M\tBACKLOG\tLATE DELTA\tDRAIN (S)\tSEED\t"+policyHeader+"\tWAIT AVG\tWAIT MAX\tWAIT P95\tJOURNEY AVG\tJOURNEY P95\tSERVED\tLEFT\tSKIPPED\tPEAK OUT\tPEAK ACTIVE\tPEAK PAX\tPEAK STOPPED\tHUB IN\tHUB OUT\tHUB OCC\tHUB RSV\tPASSENGER (M)\tEMPTY (M)\tLOADED %\tMOVES"); err != nil {
		return fmt.Errorf("write table header: %w", err)
	}
	for _, outcome := range results {
		policy := outcome.Policy
		if input.sharingConsentColumn {
			policy += "\t" + string(outcome.SharingConsent)
		}
		if input.waitRuleColumn {
			policy += "\t" + outcome.WaitRule
		}
		if input.platoonColumn {
			policy += "\t" + outcome.PlatoonPolicy
		}
		if input.sharingJoinColumn {
			policy += "\t" + outcome.SharingJoin
		}
		if input.onboardPickupsColumn {
			policy += "\t" + outcome.OnboardPickups
		}
		if input.pickupReassignmentColumn {
			policy += "\t" + outcome.PickupReassignment
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%d\t%d\t%s\t%d\t%s\t%.2f\t%.2f\t%.2f\t%.2f\t%.2f\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%.1f\t%.1f\t%.2f\t%d\n",
			outcome.Pattern, outcome.DemandBand, outcome.RequestEverySeconds, outcome.OfferedPerMinute,
			outcome.ArrivalThroughputPerMinute, outcome.LateArrivalThroughputPerMinute,
			outcome.BacklogAtArrivalEnd, outcome.LateBacklogChange, drainText(outcome), outcome.Seed, policy,
			outcome.WaitAverageSeconds, outcome.WaitMaximumSeconds, outcome.WaitP95Seconds,
			outcome.JourneyAverageSeconds, outcome.JourneyP95Seconds, outcome.Served, outcome.Remaining, outcome.Skipped,
			outcome.PeakOutstanding, outcome.PeakActiveVehicles, outcome.PeakPassengerVehicles, outcome.PeakStoppedVehicles,
			outcome.PeakFocusEntranceStopped, outcome.PeakFocusExitStopped,
			outcome.PeakFocusOccupiedBerths, outcome.PeakFocusReservedEmptyBerths,
			outcome.PassengerDistanceMeters, outcome.EmptyDistanceMeters, outcome.LoadedDistancePercent,
			outcome.PositioningMoveCount); err != nil {
			return fmt.Errorf("write table row: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("flush table report: %w", err)
	}
	return writeTableEnergy(output, results)
}

// writeCSV writes a header and one row for each result. When
// input.waitRuleColumn is set, a wait_rule column follows routing_policy.
// When input.platoonColumn is set, a platoon_policy column follows them.
// When input.sharingJoinColumn is set, a sharing_join column follows
// sharing_mode. When input.seatColumns is set, the seat screen columns
// follow shared_parties. Without these options, the columns are the same
// as before them.
func writeCSV(input writeReportInput) error {
	w := csv.NewWriter(input.output)
	energyColumn := slices.ContainsFunc(input.results, func(r result) bool { return r.Energy != nil })
	dailyColumn := slices.ContainsFunc(input.results, func(r result) bool { return r.DailyStartMinute != nil })
	header := []string{
		"pattern", "demand_profile", "demand_band", "request_every_seconds", "offered_per_minute", "burst_size", "seed", "policy", "routing_policy",
	}
	if input.sharingConsentColumn {
		header = append(header, "sharing_consent")
	}
	if input.waitRuleColumn {
		header = append(header, "wait_rule")
	}
	if input.platoonColumn {
		header = append(header, "platoon_policy")
	}
	if input.pickupReassignmentColumn {
		header = append(header, "pickup_reassignment")
	}
	header = append(header, "shared_ride_party_limit", "sharing_mode")
	if input.sharingJoinColumn {
		header = append(header, "sharing_join")
	}
	if input.onboardPickupsColumn {
		header = append(header, "onboard_pickups")
	}
	header = append(header, "shared_parties")
	if input.seatColumns {
		header = append(header, "full_pod_refusals", "full_departures", "departure_backlog", "departures_demand_over_four", "departures_over_four_aboard",
			"join_eligible_assigned", "join_eligible_existing_stop", "join_eligible_added_stop_only", "reassigned_parties")
	}
	header = append(header,
		"focus_station", "window_start_seconds", "window_end_seconds", "actual_end_seconds", "arrival_window_seconds", "arrival_end_seconds", "schedule_id",
		"scheduled", "served", "remaining", "skipped", "completed_at_arrival_end", "backlog_at_arrival_end", "arrival_throughput_per_minute",
		"completed_at_arrival_midpoint", "backlog_at_arrival_midpoint", "late_arrival_throughput_per_minute", "late_backlog_change", "drained", "drain_seconds",
		"peak_pending", "peak_outstanding", "peak_active_vehicles", "peak_passenger_vehicles", "peak_stopped_vehicles", "peak_focus_approaching", "peak_focus_entrance_stopped", "peak_focus_exit_stopped",
		"peak_focus_occupied_berths", "peak_focus_reserved_empty_berths", "stopped_pod_seconds", "junction_wait_seconds", "track_wait_seconds",
		"peak_node_throughput_per_minute", "peak_node",
		"queue_cleared", "queue_clear_seconds",
		"wait_average_seconds", "wait_maximum_seconds", "wait_p95_seconds",
		"journey_average_seconds", "journey_p95_seconds", "journey_maximum_seconds", "passenger_distance_meters", "empty_distance_meters", "loaded_distance_percent", "occupancy",
		"rider_distance_meters", "direct_distance_meters", "detour_ratio_mean", "detour_ratio_max", "intermediate_stops", "positioning_moves",
		"platoon_time_percent",
	)
	if dailyColumn {
		header = append(header, "daily_start_minute")
	}
	if energyColumn {
		header = append(header, energyCSVHeader...)
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, outcome := range input.results {
		row := []string{
			outcome.Pattern, outcome.DemandProfile, outcome.DemandBand, floatText(outcome.RequestEverySeconds), floatText(outcome.OfferedPerMinute), strconv.Itoa(outcome.BurstSize), strconv.FormatInt(outcome.Seed, 10), outcome.Policy, outcome.RoutingPolicy,
		}
		if input.sharingConsentColumn {
			row = append(row, string(outcome.SharingConsent))
		}
		if input.waitRuleColumn {
			row = append(row, outcome.WaitRule)
		}
		if input.platoonColumn {
			row = append(row, outcome.PlatoonPolicy)
		}
		if input.pickupReassignmentColumn {
			row = append(row, outcome.PickupReassignment)
		}
		row = append(row, strconv.Itoa(outcome.SharedRidePartyLimit), outcome.SharingMode)
		if input.sharingJoinColumn {
			row = append(row, outcome.SharingJoin)
		}
		if input.onboardPickupsColumn {
			row = append(row, outcome.OnboardPickups)
		}
		row = append(row, strconv.Itoa(outcome.SharedParties))
		if input.seatColumns {
			row = append(row, strconv.Itoa(outcome.FullPodRefusals), strconv.Itoa(outcome.FullDepartures), strconv.Itoa(outcome.DepartureBacklog),
				strconv.Itoa(outcome.DeparturesDemandOverFour), strconv.Itoa(outcome.DeparturesOverFourAboard),
				strconv.Itoa(outcome.JoinEligibleAssigned), strconv.Itoa(outcome.JoinEligibleExistingStop), strconv.Itoa(outcome.JoinEligibleAddedStopOnly),
				strconv.Itoa(outcome.ReassignedParties))
		}
		row = append(row,
			outcome.FocusStation,
			floatText(outcome.WindowStartSeconds), floatText(outcome.WindowEndSeconds), floatText(outcome.ActualEndSeconds), floatText(outcome.ArrivalWindowSeconds), floatText(outcome.ArrivalEndSeconds), outcome.ScheduleID,
			strconv.Itoa(outcome.Scheduled), strconv.Itoa(outcome.Served), strconv.Itoa(outcome.Remaining), strconv.Itoa(outcome.Skipped),
			strconv.Itoa(outcome.CompletedAtArrivalEnd), strconv.Itoa(outcome.BacklogAtArrivalEnd), floatText(outcome.ArrivalThroughputPerMinute),
			strconv.Itoa(outcome.CompletedAtArrivalMidpoint), strconv.Itoa(outcome.BacklogAtArrivalMidpoint), floatText(outcome.LateArrivalThroughputPerMinute), strconv.Itoa(outcome.LateBacklogChange),
			strconv.FormatBool(outcome.Drained), floatText(outcome.DrainSeconds),
			strconv.Itoa(outcome.PeakPending), strconv.Itoa(outcome.PeakOutstanding), strconv.Itoa(outcome.PeakActiveVehicles), strconv.Itoa(outcome.PeakPassengerVehicles), strconv.Itoa(outcome.PeakStoppedVehicles),
			strconv.Itoa(outcome.PeakFocusApproaching), strconv.Itoa(outcome.PeakFocusEntranceStopped), strconv.Itoa(outcome.PeakFocusExitStopped),
			strconv.Itoa(outcome.PeakFocusOccupiedBerths), strconv.Itoa(outcome.PeakFocusReservedEmptyBerths),
			floatText(outcome.StoppedPodSeconds), floatText(outcome.JunctionWaitSeconds), floatText(outcome.TrackWaitSeconds),
			strconv.Itoa(outcome.PeakNodeThroughputPerMinute), outcome.PeakNode,
			strconv.FormatBool(outcome.QueueCleared), floatText(outcome.QueueClearSeconds),
			floatText(outcome.WaitAverageSeconds), floatText(outcome.WaitMaximumSeconds), floatText(outcome.WaitP95Seconds),
			floatText(outcome.JourneyAverageSeconds), floatText(outcome.JourneyP95Seconds), floatText(outcome.JourneyMaximumSeconds),
			floatText(outcome.PassengerDistanceMeters),
			floatText(outcome.EmptyDistanceMeters), floatText(outcome.LoadedDistancePercent), floatText(outcome.Occupancy),
			floatText(outcome.RiderDistanceMeters), floatText(outcome.DirectDistanceMeters),
			floatText(outcome.DetourRatioMean), floatText(outcome.DetourRatioMax), strconv.Itoa(outcome.IntermediateStops),
			strconv.Itoa(outcome.PositioningMoveCount), floatText(outcome.PlatoonTimePercent),
		)
		if dailyColumn {
			value := ""
			if outcome.DailyStartMinute != nil {
				value = strconv.Itoa(*outcome.DailyStartMinute)
			}
			row = append(row, value)
		}
		if energyColumn {
			values, err := energyCSVRow(outcome.Energy)
			if err != nil {
				return err
			}
			row = append(row, values...)
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush CSV report: %w", err)
	}
	return nil
}

func floatText(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func drainText(outcome result) string {
	if !outcome.Drained {
		return "-"
	}
	return fmt.Sprintf("%.1f", outcome.DrainSeconds)
}
