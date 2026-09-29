package sim

import "testing"

func TestPendingCountFollowsDispatch(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, station string
		pending       [2]int
	}{
		{name: "local boarding", station: "harbor", pending: [2]int{0, 1}},
		{name: "assigned pickup", station: "parking", pending: [2]int{1, 2}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, err := New(Example(), test.station)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.PendingCount(); got != 0 {
				t.Fatalf("initial pending count = %d, want 0", got)
			}
			for i, want := range test.pending {
				if err := s.RequestTrip("harbor", "garden"); err != nil {
					t.Fatal(err)
				}
				if got := s.PendingCount(); got != want {
					t.Fatalf("after arrival %d: pending count = %d, want %d", i+1, got, want)
				}
			}
			state := s.Snapshot()
			for range 900 {
				if got := s.PendingCount(); got != len(state.Pending) {
					t.Fatalf("tick %d: pending count = %d, snapshot has %d", state.Tick, got, len(state.Pending))
				}
				if state.Completed == 2 {
					break
				}
				advance(s, TicksPerSecond)
				state = s.Snapshot()
			}
			if state.Completed != 2 || s.PendingCount() != 0 {
				t.Fatalf("requests did not drain: completed %d, pending %d", state.Completed, s.PendingCount())
			}
		})
	}
}
