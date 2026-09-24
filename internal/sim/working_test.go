package sim

import "testing"

func TestWorkingVehicles(t *testing.T) {
	t.Parallel()
	open := &Request{ID: 1, From: "a", To: "b", PodID: "p1"}
	done := &Request{ID: 2, From: "a", To: "b", PodID: "p1", Completed: true}
	pod := func(id string) Pod { return Pod{ID: id} }
	for _, test := range []struct {
		name  string
		state Snapshot
		want  int
	}{
		{name: "empty fleet", want: 0},
		{name: "idle pod without a trip", state: Snapshot{Vehicles: []Vehicle{{Pod: pod("p1")}}}, want: 0},
		{name: "idle pod after a trip", state: Snapshot{Vehicles: []Vehicle{{Pod: pod("p1"), Request: done}}}, want: 0},
		{name: "pod with a trip", state: Snapshot{Vehicles: []Vehicle{{Pod: pod("p1"), Request: open}}}, want: 1},
		{name: "empty move", state: Snapshot{Vehicles: []Vehicle{{Pod: pod("p1"), Request: done, RelocatingTo: "b"}}}, want: 0},
		{
			name: "pod on its way to a pickup",
			state: Snapshot{
				Vehicles: []Vehicle{{Pod: pod("p1"), Request: done, RelocatingTo: "a"}, {Pod: pod("p2"), RelocatingTo: "b"}},
				Pending:  []Request{{ID: 3, From: "a", To: "b", PodID: "p1"}, {ID: 4, From: "b", To: "a"}},
			},
			want: 1,
		},
		{
			name: "pod with a trip and a later pickup is one pod",
			state: Snapshot{
				Vehicles: []Vehicle{{Pod: pod("p1"), Request: open}},
				Pending:  []Request{{ID: 3, From: "b", To: "a", PodID: "p1"}},
			},
			want: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.state.WorkingVehicles(); got != test.want {
				t.Fatalf("WorkingVehicles() = %d, want %d", got, test.want)
			}
		})
	}
}
