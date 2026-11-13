package project

import (
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// islandConfig is the default project with three lanes that give a plane
// reach refusal.
func islandConfig(t *testing.T) Config {
	t.Helper()
	config := Default()
	classes, err := sim.NewClassSet("group")
	if err != nil {
		t.Fatal(err)
	}
	config.Network.Nodes, config.Network.Lanes = slices.Clone(config.Network.Nodes), slices.Clone(config.Network.Lanes)
	config.Network.Nodes = append(config.Network.Nodes, []sim.Node{
		{ID: "island-mid", Position: sim.Point{X: 5000, Y: 100}}, {ID: "island-join", Position: sim.Point{X: 5000}}, {ID: "island-east", Position: sim.Point{X: 5400}},
		{ID: "island-cross-east", Position: sim.Point{X: 5140, Y: 24}}, {ID: "island-cross-west", Position: sim.Point{X: 4860, Y: 24}},
	}...)
	for _, row := range [][4]string{{"island-near", "island-mid", "island-join", "A"}, {"island-main", "island-join", "island-east", "Q"}, {"island-cross", "island-cross-east", "island-cross-west", "Q"}} {
		config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: row[0], From: row[1], To: row[2], SpeedLimit: 14, VehicleClasses: classes, SeparationGroup: row[3]})
	}
	return config
}

func validationText(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// TestValidateWithMatchesValidate pins that ValidateWith with the checks of
// the network of a project gives the refusal of Validate, for valid
// projects, for each refusal that the network checks give, and for a plane
// reach refusal with and without an earlier or a later refusal.
func TestValidateWithMatchesValidate(t *testing.T) {
	t.Parallel()
	shortLane := func(config *Config) {
		config.Network.Nodes = append(slices.Clone(config.Network.Nodes), sim.Node{ID: "short-a", Position: sim.Point{X: 9000}}, sim.Node{ID: "short-b", Position: sim.Point{X: 9003}})
		config.Network.Lanes = append(slices.Clone(config.Network.Lanes), sim.Lane{ID: "short", From: "short-a", To: "short-b", SpeedLimit: 14})
	}
	edits := map[string]func(*Config){
		"none":            func(*Config) {},
		"short lane":      shortLane,
		"unknown station": func(config *Config) { config.Fleet = slices.Clone(config.Fleet); config.Fleet[0].StationID = "nowhere" },
		"express registry": func(config *Config) {
			config.ExpressServices = []sim.ExpressService{{ID: "service"}}
		},
		"express contract": func(config *Config) { config.OrderContract = sim.ExpressOrderContract },
		"unknown contract": func(config *Config) { config.OrderContract = "unknown" },
		"all parking": func(config *Config) {
			config.Network.Stations = slices.Clone(config.Network.Stations)
			for index := range config.Network.Stations {
				config.Network.Stations[index].ParkingOnly = true
			}
		},
		"long name": func(config *Config) { config.Name = strings.Repeat("n", MaxNameLength+1) },
	}
	refusals := make(map[string]bool)
	for baseName, base := range map[string]Config{"default": Default(), "island": islandConfig(t)} {
		for name, edit := range edits {
			config := base
			edit(&config)
			want := validationText(Validate(config))
			refusals[want] = true
			for index, clearance := range []sim.LaneClearance{{}, sim.NewLaneClearance(config.Network)} {
				check := sim.NewNetworkCheck(config.Network, clearance)
				for range 2 {
					if got := validationText(ValidateWith(config, Checked{Network: check})); got != want {
						t.Errorf("%s, %s, clearance %d: refusal = %s, want %s", baseName, name, index, got, want)
					}
				}
			}
		}
	}
	if len(refusals) < 8 || !refusals["<nil>"] {
		t.Fatalf("the cases give only the refusals %v", refusals)
	}
}

// TestValidateWithReadsNetworkCheck pins that ValidateWith reads the checks
// that it gets and does not run them again: the checks of another network
// give the refusal of that network.
func TestValidateWithReadsNetworkCheck(t *testing.T) {
	t.Parallel()
	other := Default().Network
	other.Nodes = append(slices.Clone(other.Nodes), sim.Node{ID: "short-a", Position: sim.Point{X: 9000}}, sim.Node{ID: "short-b", Position: sim.Point{X: 9003}})
	other.Lanes = append(slices.Clone(other.Lanes), sim.Lane{ID: "short", From: "short-a", To: "short-b", SpeedLimit: 14})
	check := sim.NewNetworkCheck(other, sim.LaneClearance{})
	err := ValidateWith(Default(), Checked{Network: check})
	if err == nil || !strings.HasPrefix(err.Error(), `invalid project scenario: lane "short"`) {
		t.Fatalf("refusal = %v, want the refusal of the short lane of the checks", err)
	}
	name := runtime.FuncForPC(reflect.ValueOf(projectChecks[scenarioCheck]).Pointer()).Name()
	if !strings.HasSuffix(name, ".validateScenario") {
		t.Fatalf("projectChecks[scenarioCheck] is %s", name)
	}
}
