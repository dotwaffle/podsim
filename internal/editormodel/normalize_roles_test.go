package editormodel

import (
	"encoding/json/v2"
	"maps"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// normalizedConfig returns config after the editor normalizes it.
func normalizedConfig(t *testing.T, config project.Config) project.Config {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var draft map[string]any
	if err = json.Unmarshal(data, &draft); err != nil {
		t.Fatal(err)
	}
	change, err := normalizeProject(draft)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(draft, change.Patch)
	if data, err = json.Marshal(draft); err != nil {
		t.Fatal(err)
	}
	var out project.Config
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestNormalizationKeepsMotionAndPhases runs a generated project with and
// without the roles that the editor gives its lanes. The editor gives a role
// to each lane of a maneuver group, and the simulation gives such a lane no
// role, so the pods must move and report phases the same in both runs.
func TestNormalizationKeepsMotionAndPhases(t *testing.T) {
	t.Parallel()
	compact, err := scenarios.CompactStation()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name   string
		config project.Config
	}{
		{"compact-station", compact},
		{"london-central", scenarios.LondonCentral()},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			checkNormalizedRun(t, row.config)
		})
	}
}

func checkNormalizedRun(t *testing.T, base project.Config) {
	t.Helper()
	normal := normalizedConfig(t, base)
	var grouped int
	for _, group := range base.Network.ManeuverGroups {
		for _, id := range group.LaneIDs {
			for index, lane := range base.Network.Lanes {
				if lane.ID == id && lane.StationRole == "" && normal.Network.Lanes[index].StationRole != "" {
					grouped++
				}
			}
		}
	}
	if grouped == 0 {
		t.Fatal("the fixture has no maneuver group lane that gets a role")
	}
	start := func(config project.Config) *sim.Simulation {
		s, err := sim.NewFleetWithOrderContract(config.Network, config.Fleet, config.OrderContract)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	a, b := start(base), start(normal)
	stations := project.PassengerStations(base.Network)
	phases := make(map[sim.StationPhase]bool)
	for tick := range 12000 {
		if tick%120 == 0 {
			from, to := stations[(tick/120)%len(stations)].ID, stations[(tick/120+1)%len(stations)].ID
			if errA, errB := a.RequestTrip(from, to), b.RequestTrip(from, to); (errA == nil) != (errB == nil) {
				t.Fatalf("tick %d trip %s to %s: %v and %v", tick, from, to, errA, errB)
			}
		}
		a.Step()
		b.Step()
		podsA, podsB := a.Snapshot().Vehicles, b.Snapshot().Vehicles
		for i := range podsA {
			podA, podB := podsA[i].Pod, podsB[i].Pod
			if podA.Position != podB.Position || podA.StationPhase != podB.StationPhase {
				t.Fatalf("tick %d pod %s on lane %s: %v %q and %v %q", tick, podA.ID, podA.LaneID, podA.Position, podA.StationPhase, podB.Position, podB.StationPhase)
			}
			phases[podA.StationPhase] = true
		}
	}
	if !phases[sim.AccessingBerth] || !phases[sim.DepartingBerth] {
		t.Fatalf("the run reached the phases %v, want the berth phases", phases)
	}
}
