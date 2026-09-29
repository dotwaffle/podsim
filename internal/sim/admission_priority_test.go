package sim

import "testing"

func TestAdmissionPriority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                        string
		activity                    Activity
		occupied, completed, pickup bool
		want                        int
	}{
		{name: "passengers", activity: Traveling, occupied: true, want: 0},
		{name: "boarding", activity: Boarding, want: 0},
		{name: "continuing", activity: Continuing, occupied: true, want: 0},
		{name: "passengers and pickup", activity: Traveling, occupied: true, pickup: true, want: 0},
		{name: "pickup", activity: Traveling, pickup: true, want: 1},
		{name: "departing pickup", activity: DepartingEmpty, pickup: true, want: 1},
		{name: "repositioning", activity: DepartingEmpty, want: 2},
		{name: "completed riders", activity: Traveling, completed: true, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := vehicle{
				Pod:    Pod{Activity: tc.activity, Occupied: tc.occupied},
				Riders: []Request{{Completed: tc.completed, PartySize: 1}},
			}
			if got := admissionPriority(&v, tc.pickup); got != tc.want {
				t.Fatalf("priority = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestAdmissionContention(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                         string
		passengers, pickup, existing bool
		wait                         int64
		want                         string
	}{
		{name: "passengers beat older empty", passengers: true, wait: admissionAgeTicks - 1, want: "02"},
		{name: "pickup beats older empty", pickup: true, wait: admissionAgeTicks - 1, want: "02"},
		{name: "same class uses age", wait: 1, want: "01"},
		{name: "same age uses ID", want: "01"},
		{name: "age threshold overrides passengers", passengers: true, wait: admissionAgeTicks, want: "01"},
		{name: "reserved empty stays owner", passengers: true, existing: true, want: "01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			junction := resource{kind: junctionResource, id: "merge"}
			s := &Simulation{tick: admissionAgeTicks, owners: make(map[resource]string)}
			for _, id := range []string{"02", "01"} {
				v := vehicle{Pod: Pod{ID: id, Activity: Traveling}, reservedThrough: -1}
				v.blocks = blockListOf([]block{{lane: Lane{ID: id, SpeedLimit: 14}, end: 30, resources: []resource{junction}}})
				v.waitSince = s.tick
				if id == "01" {
					v.waitSince -= tc.wait
				} else if tc.passengers {
					v.Pod.Occupied = true
					v.Riders = []Request{{PartySize: 1}}
				}
				s.vehicles = append(s.vehicles, v)
			}
			if tc.pickup {
				s.waiting = []waitingTrip{{request: Request{PodID: "02"}}}
			}
			if tc.existing {
				s.owners[junction] = "01"
				s.vehicles[1].reservedThrough = 0
			}
			s.admit()
			if got := s.owners[junction]; got != tc.want {
				t.Fatalf("junction owner = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAdmissionAgedRequestsUseAgeBeforeClass(t *testing.T) {
	t.Parallel()
	older := intent{since: 0, priority: 2, id: "02"}
	newer := intent{since: 1, priority: 0, id: "01"}
	if compareAdmission(older, newer, admissionAgeTicks+1) >= 0 || compareAdmission(newer, older, admissionAgeTicks+1) <= 0 {
		t.Fatal("aged requests did not preserve oldest-first order")
	}
}
