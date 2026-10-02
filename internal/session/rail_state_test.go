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
	for _, version := range []int{stateVersion, bufferStateVersion, bufferPlatoonStateVersion} {
		for _, withPlan := range []bool{false, true} {
			file := legacyTestState(newTestStateFile(t), version)
			if withPlan {
				file.Project.RailArrivals = []project.RailArrival{{ID: "train", Station: "harbor", AtSeconds: 60, WalkingSeconds: 15, Passengers: 120,
					Destinations: []project.RailDestination{{Station: "market", Weight: 1}}}}
			}
			encoded := encodeTestState(t, file)
			restored, err := decodeCheckedState(encoded)
			if err != nil || !reflect.DeepEqual(restored, file) {
				t.Fatalf("version %d with plan %t: %v", version, withPlan, err)
			}
			err = json.Unmarshal(decompressTestJSON(t, encoded), reflect.New(older).Interface(), strictStateOptions)
			if withPlan && !errors.Is(err, json.ErrUnknownName) || !withPlan && err != nil {
				t.Fatalf("older reader, version %d with plan %t: %v", version, withPlan, err)
			}
		}
	}
}
