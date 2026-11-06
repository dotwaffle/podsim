package session

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestDepotExpressPhysicalSaveRestore(t *testing.T) {
	t.Parallel()
	config, err := scenarios.PresetWith("small", func(p *scenarios.Parameters) {
		p.Pods, p.InitialParkingPods = 2, 1
		p.ParkingLanes, p.ParkingBerths = 2, 2
	})
	if err != nil {
		t.Fatal(err)
	}
	classes, err := sim.NewClassSet("legacy", "compact", "group", "express")
	if err != nil {
		t.Fatal(err)
	}
	config.OrderContract, config.Redistribution, config.Demand.Enabled = sim.ExpressOrderContract, true, false
	for i := range config.Network.Nodes {
		config.Network.Nodes[i].Position.X *= 4
		config.Network.Nodes[i].Position.Y *= 4
	}
	for i := range config.Network.Lanes {
		lane := &config.Network.Lanes[i]
		if !strings.HasPrefix(lane.ID, "s05-") || lane.StationRole == sim.StationThroughRole {
			lane.VehicleClasses = classes
		}
		if lane.Control != nil {
			lane.Control = &sim.Point{X: lane.Control.X * 4, Y: lane.Control.Y * 4}
		}
	}
	for i := range config.Network.Stations {
		station := &config.Network.Stations[i]
		if !station.ParkingOnly {
			station.VehicleClasses = classes
			for j := range station.Berths {
				station.Berths[j].VehicleClasses = classes
			}
		}
	}
	for i := range config.Fleet {
		if config.Fleet[i].StationID == "parking" {
			config.Fleet[i].Class = sim.CompactClass
		} else {
			config.Fleet[i].Class = sim.ExpressClass
		}
	}
	shared, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	client := newTestClient(shared, "mixed-depot")
	client.mustApply(t, Command{Action: "trip", Origin: "station-01", Destination: "station-02", PartySize: 20, OrderContract: sim.ExpressOrderContract})
	moving := false
	for range 120 * sim.TicksPerSecond {
		shared.advance()
		for _, v := range shared.simulation.Snapshot().Vehicles {
			if v.Pod.Class == sim.ExpressClass && v.Pod.Activity == sim.Traveling {
				moving = true
			}
		}
		if moving {
			break
		}
	}
	if !moving {
		t.Fatal("Express pod did not start its trip")
	}
	file := sessionStateFile(t, shared)
	file.RestoreAttempts = 0
	data := encodeTestState(t, file)
	decoded, err := decodeCheckedState(data)
	if err != nil {
		t.Fatal(err)
	}
	if resolveErr := decoded.resolveBoardings(); resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if !reflect.DeepEqual(decoded.Project.Network.Depots, config.Network.Depots) || !reflect.DeepEqual(decoded.Simulation.Pods, file.Simulation.Pods) {
		t.Fatal("save changed the depot or the mixed fleet")
	}
	loaded, err := shared.loadState(loadInput{data: data, project: &config, steps: realRestoreSteps()})
	if err != nil || loaded.result.Tier != sim.RestorePhysical || len(loaded.result.Demoted) != 0 {
		t.Fatalf("physical restore: %v %+v", err, loaded.result)
	}
	if got := loaded.simulation.ExportState(); !reflect.DeepEqual(got.Pods, file.Simulation.Pods) {
		t.Fatal("physical restore changed mixed fleet positions")
	}
	shared.installRestored(loaded)
	if loaded.simulation.OrderContract() != sim.ExpressOrderContract || loaded.simulation.Positioning() != sim.PositioningGuarded {
		t.Fatal("restore lost the contract or redistribution")
	}
	for range 10 * sim.TicksPerSecond {
		loaded.simulation.Step()
		if _, err := loaded.simulation.SafetyObservation().Check(); err != nil {
			t.Fatal(err)
		}
	}
}
