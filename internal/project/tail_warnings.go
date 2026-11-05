package project

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

// MaxTailWarnings is the number of lanes that TailWarnings names.
const MaxTailWarnings = 5

// TailWarning is the text of one warning. LaneID is empty for the line that
// counts the lanes after MaxTailWarnings.
type TailWarning struct {
	LaneID, Text string
}

// TailWarnings returns the warnings for the lanes of a validated network
// whose large-class tail exceeds their length. They do not affect validation.
func TailWarnings(network sim.Network) []TailWarning {
	return OverrunWarnings(network.LaneTailOverruns())
}

// OverrunWarnings returns the warnings for the given lanes, in their order.
func OverrunWarnings(overruns []sim.LaneTailOverrun) []TailWarning {
	var warnings []TailWarning
	for index, overrun := range overruns {
		if index == MaxTailWarnings {
			break
		}
		warnings = append(warnings, TailWarning{
			LaneID: overrun.LaneID,
			Text:   fmt.Sprintf("Lane %s has a large-class tail of %.1f m on a length of %.1f m, which can stop the network.", overrun.LaneID, overrun.Tail, overrun.Length),
		})
	}
	switch rest := len(overruns) - MaxTailWarnings; {
	case rest == 1:
		warnings = append(warnings, TailWarning{Text: "1 more lane has a large-class tail longer than its length."})
	case rest > 1:
		warnings = append(warnings, TailWarning{Text: fmt.Sprintf("%d more lanes have a large-class tail longer than their length.", rest)})
	}
	return warnings
}
