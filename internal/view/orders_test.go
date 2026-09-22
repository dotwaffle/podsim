package view

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestOutstandingOrderCountIncludesSharedParties(t *testing.T) {
	t.Parallel()
	state := sim.Snapshot{
		Pending: []sim.Request{{ID: 3}},
		Vehicles: []sim.Vehicle{{
			Pod: sim.Pod{ID: "01"}, Request: &sim.Request{ID: 1}, Parties: 3,
		}},
	}
	if got := outstandingOrderCount(state); got != 4 {
		t.Fatalf("outstanding order count = %d", got)
	}
	rows := outstandingOrders(state)
	if len(rows) != 2 || rows[0].parties != 3 || !rows[0].active {
		t.Fatalf("outstanding rows = %+v", rows)
	}
}
