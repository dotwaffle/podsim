package sim

import (
	"fmt"
	"maps"
	"reflect"
	"testing"
)

func TestCouplingNativeOrdinaryResourceGuards(t *testing.T) {
	t.Parallel()
	for _, occupied := range []bool{false, true} {
		for _, operation := range []struct {
			name string
			run  func(*Simulation)
		}{
			{"admission", (*Simulation).admit},
			{"release", (*Simulation).releaseCleared},
		} {
			t.Run(fmt.Sprintf("occupied=%t/%s", occupied, operation.name), func(t *testing.T) {
				t.Parallel()
				input := nativeCouplingSavedFixture(t, occupied, couplingConnected, 1)
				s, _, err := RestoreState(input)
				if err != nil {
					t.Fatal(err)
				}
				// Ordinary admission must preserve the committed controller's wait.
				for i := range s.vehicles {
					s.vehicles[i].Pod.WaitReason = TrackOccupied
					s.vehicles[i].Pod.BlockedBy = "committed-controller"
				}
				before := s.Clone()
				operation.run(s)
				if !maps.Equal(before.owners, s.owners) {
					t.Fatal("ordinary controller changed committed resource owners")
				}
				if !reflect.DeepEqual(before.vehicles, s.vehicles) {
					t.Fatal("ordinary controller changed a committed member or its retained claims")
				}
				if !reflect.DeepEqual(before.ExportState(), s.ExportState()) {
					t.Fatal("ordinary controller changed exported committed state")
				}
			})
		}
	}
}
