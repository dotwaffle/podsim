package view

import (
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestPlatoonLinks checks the lines between the pods of platoons. The order
// of the vehicles is not the order of the platoon, and a pod whose pod
// ahead is not in the snapshot gets no line.
func TestPlatoonLinks(t *testing.T) {
	t.Parallel()
	pod := func(id, platoon string, index int) sim.Vehicle {
		return sim.Vehicle{Pod: sim.Pod{ID: id}, PlatoonID: platoon, PlatoonIndex: index}
	}
	tests := []struct {
		name     string
		vehicles []sim.Vehicle
		want     []platoonLink
	}{
		{name: "no platoon", vehicles: []sim.Vehicle{pod("01", "", 0), pod("02", "", 0)}},
		{
			name:     "a platoon of two",
			vehicles: []sim.Vehicle{pod("01", "01", 1), pod("02", "01", 2)},
			want:     []platoonLink{{follower: 1, ahead: 0}},
		},
		{
			name:     "a platoon of three out of order and a pod that is not coupled",
			vehicles: []sim.Vehicle{pod("01", "03", 3), pod("02", "", 0), pod("03", "03", 1), pod("04", "03", 2)},
			want:     []platoonLink{{follower: 0, ahead: 3}, {follower: 3, ahead: 2}},
		},
		{
			name:     "two platoons",
			vehicles: []sim.Vehicle{pod("01", "01", 1), pod("02", "03", 2), pod("03", "03", 1), pod("04", "01", 2)},
			want:     []platoonLink{{follower: 1, ahead: 2}, {follower: 3, ahead: 0}},
		},
		{
			name:     "a pod ahead that is not in the snapshot",
			vehicles: []sim.Vehicle{pod("01", "09", 3), pod("02", "09", 4)},
			want:     []platoonLink{{follower: 1, ahead: 0}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := platoonLinks(test.vehicles); !slices.Equal(got, test.want) {
				t.Fatalf("links = %+v, want %+v", got, test.want)
			}
		})
	}
}
