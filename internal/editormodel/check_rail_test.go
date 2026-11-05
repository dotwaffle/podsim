package editormodel

import (
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestRailReleaseCheckMatchesProject compares the release limit of the
// editor with the limit of project.Validate, across a ladder of plans that
// crosses the limit. The first offers of each departure share a tick with
// the arrival, and the count of these offers depends on the tick rate, so a
// different rate in either check gives a different answer on the ladder.
func TestRailReleaseCheckMatchesProject(t *testing.T) {
	t.Parallel()
	accepted, refused := 0, 0
	for arrivals := 100; arrivals <= project.MaxRailRelease; arrivals++ {
		config := project.Default()
		config.RailArrivals = []project.RailArrival{{
			ID: "arrival", Station: "harbor", AtSeconds: 10, Passengers: arrivals,
			Destinations: []project.RailDestination{{Station: "market", Weight: 1}},
		}}
		for _, id := range []string{"a", "b", "c", "d", "e"} {
			config.RailDepartures = append(config.RailDepartures, project.RailDeparture{
				ID: id, Station: "harbor", AtSeconds: 600, WalkingSeconds: 30,
				RequestFromSeconds: 10, RequestUntilSeconds: 11, Passengers: project.MaxRailRelease,
				Origins: []project.RailOrigin{{Station: "market", Weight: 1}},
			})
		}
		projectRefuses := false
		if err := project.Validate(config); err != nil {
			if !strings.Contains(err.Error(), "exceed") {
				t.Fatalf("arrivals %d: %v", arrivals, err)
			}
			projectRefuses = true
		}
		editorRefuses := false
		for _, item := range railCheckErrors(t, config) {
			if !strings.Contains(item.Text, "at one release tick") {
				t.Fatalf("arrivals %d: %s", arrivals, item.Text)
			}
			editorRefuses = true
		}
		if editorRefuses != projectRefuses {
			t.Fatalf("arrivals %d: the editor refuses: %t, the project refuses: %t", arrivals, editorRefuses, projectRefuses)
		}
		if projectRefuses {
			refused++
		} else {
			accepted++
		}
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("the ladder does not cross the limit: %d accepted, %d refused", accepted, refused)
	}
}

// railCheckErrors runs the rail check of the editor on the rail plans of
// config, as the JSON values that the editor holds.
func railCheckErrors(t *testing.T, config project.Config) []check {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"railArrivals": config.RailArrivals, "railDepartures": config.RailDepartures})
	if err != nil {
		t.Fatal(err)
	}
	var draft any
	if err := json.Unmarshal(encoded, &draft); err != nil {
		t.Fatal(err)
	}
	passenger := make(map[string]bool)
	for _, station := range project.PassengerStations(config.Network) {
		passenger[station.ID] = true
	}
	var errors checkList
	checkRailPlans(draft, passenger, &errors)
	return errors.items
}
