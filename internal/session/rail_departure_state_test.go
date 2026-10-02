package session

import (
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestRailDeparturePlanStateRoundTrip(t *testing.T) {
	t.Parallel()
	older := withoutMember(reflect.TypeFor[stateFile](), reflect.TypeFor[project.Config](), "railDepartures")
	for _, version := range []int{stateVersion, bufferStateVersion, bufferPlatoonStateVersion} {
		for _, withPlan := range []bool{false, true} {
			file := legacyTestState(newTestStateFile(t), version)
			if withPlan {
				file.Project.RailDepartures = []project.RailDeparture{{ID: "train", Station: "harbor", AtSeconds: 600,
					WalkingSeconds: 15, RequestFromSeconds: 30, RequestUntilSeconds: 120, Passengers: 120,
					Origins: []project.RailOrigin{{Station: "market", Weight: 1}}}}
			}
			encoded := encodeTestState(t, file)
			restored, err := decodeCheckedState(encoded)
			if err != nil || !reflect.DeepEqual(restored, file) {
				t.Fatalf("version %d with departure plan %t: %v", version, withPlan, err)
			}
			err = json.Unmarshal(decompressTestJSON(t, encoded), reflect.New(older).Interface(), strictStateOptions)
			if withPlan && !errors.Is(err, json.ErrUnknownName) || !withPlan && err != nil {
				t.Fatalf("older reader, version %d with departure plan %t: %v", version, withPlan, err)
			}
		}
	}
}

func TestRailDepartureDecodeBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func(*stateFile)
	}{
		{"events", func(f *stateFile) {
			f.Project.RailDepartures = make([]project.RailDeparture, project.MaxRailArrivals+1)
		}},
		{"origins", func(f *stateFile) {
			f.Project.RailDepartures = []project.RailDeparture{{Origins: make([]project.RailOrigin, project.MaxRailDestinations+1)}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			file := newTestStateFile(t)
			tc.edit(&file)
			if _, err := decodeStateFile(encodeTestState(t, file)); !errors.Is(err, errJSONArrayTooLong) {
				t.Fatalf("departure array bound: %v", err)
			}
			raw, err := json.Marshal(Command{Project: &file.Project})
			if err != nil {
				t.Fatal(err)
			}
			if err := prescanJSON(raw, commandJSONLimits); !errors.Is(err, errJSONArrayTooLong) {
				t.Fatalf("command departure array bound: %v", err)
			}
		})
	}
}
