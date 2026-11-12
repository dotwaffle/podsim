package session

import (
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestRailPlanStateRoundTrip(t *testing.T) {
	t.Parallel()
	older := withoutMember(reflect.TypeFor[stateFile](), reflect.TypeFor[project.Config](), "railArrivals")
	for _, withPlan := range []bool{false, true} {
		file := newTestStateFile(t)
		if withPlan {
			file.Project.RailArrivals = []project.RailArrival{{ID: "train", Station: "harbor", AtSeconds: 60, WalkingSeconds: 15, Passengers: 120,
				Destinations: []project.RailDestination{{Station: "market", Weight: 1}}}}
		}
		encoded := encodeTestState(t, file)
		restored, err := decodeCheckedState(encoded)
		if err == nil {
			err = restored.resolveBoardings()
		}
		if err != nil || !reflect.DeepEqual(restored, file) {
			t.Fatalf("plan %t: %v", withPlan, err)
		}
		// The older reader has the plain JSON form of the pods, so the
		// file that it reads has no boarding tuples.
		plain := decompressTestJSON(t, encodeTestState(t, withoutRiders(file)))
		err = json.Unmarshal(plain, reflect.New(older).Interface(), strictStateOptions)
		if withPlan && !errors.Is(err, json.ErrUnknownName) || !withPlan && err != nil {
			t.Fatalf("older reader with plan %t: %v", withPlan, err)
		}
	}
}
