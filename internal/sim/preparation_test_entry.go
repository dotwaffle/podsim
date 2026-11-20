package sim

import "sync/atomic"

// This file holds the counts that pin the reuse of the network
// preparation. A test that reads a count must not run in parallel with
// tests that prepare a network or infer lane roles.

// preparations counts the runs of prepareNetworkTails. inferences counts
// the runs of inferStationLaneRoles, which only tests call.
var preparations, inferences atomic.Int64

// PreparationsForTest returns the number of network preparations in the
// process.
func PreparationsForTest() int64 { return preparations.Load() }
