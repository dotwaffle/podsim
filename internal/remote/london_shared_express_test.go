package remote

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonSharedExpressRemoteLoads is the remote load test of row SX6:
// as TestLondonExpressRemoteLoads does for D, it runs the Go client against
// a live session of the shared-track preset over HTTP and WebSocket. The
// client must read the topology of the preset, and its stream must show
// Express pods with loads of 12, 13, and 20 riders aboard.
func TestLondonSharedExpressRemoteLoads(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the shared-track Express remote run needs a full-speed simulation")
	}
	runSharedExpressLoads(t, scenarios.LondonFullShared.Generate, map[string]string{"express-parking": "940GZZLUBNK"}, [][2]string{{"940GZZLUWLO", "940GZZLUBNK"}})
}

// runSharedExpressLoads runs TestLondonSharedExpressRemoteLoads on a
// preset, with loads of 12, 13, and 20 from the first hub to the second hub
// of each line. parkings gives a hub of the line of each Express parking.
func runSharedExpressLoads(t *testing.T, generate func() (project.Config, error), parkings map[string]string, lines [][2]string) {
	t.Helper()
	config, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	config.Demand.Enabled = true
	services := map[string]string{}
	for _, service := range config.ExpressServices {
		services[service.From] = service.ID
	}
	shared, err := session.NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	server := httptest.NewServer(shared.HandlerFS(nil))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	go shared.Run(ctx)
	client := New(ctx, server.URL)
	wait := func(name string, limit time.Duration, done func(session.State) bool) {
		t.Helper()
		for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if state, ok, _ := client.View(); ok && done(state) {
				return
			}
		}
		t.Fatalf("%s: not seen in %s", name, limit)
	}
	wait("Express stream", 10*time.Second, func(state session.State) bool {
		return state.Simulation.OrderContract == sim.ExpressOrderContract
	})
	state, _, _ := client.View()
	// The preset has no banks, and has an Express parking beside the first
	// hub of each line.
	for parkingID, hub := range parkings {
		station, ok := state.Network.Station(hub)
		parking, okParking := state.Network.Station(parkingID)
		if !ok || len(station.Banks) != 0 || !okParking || len(parking.Berths) != 16 {
			t.Fatalf("the client topology is not the preset: hub %s, parking %s", hub, parkingID)
		}
	}
	if len(state.Network.Lanes) != len(config.Network.Lanes) {
		t.Fatal("the client topology is not the preset")
	}
	submit := func(command session.Command) {
		t.Helper()
		if err := client.Submit(command); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-client.Results():
			if result.Err != nil || result.Reply.Error != "" {
				t.Fatalf("%s: %v %s", command.Action, result.Err, result.Reply.Error)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: no result", command.Action)
		}
	}
	submit(session.Command{Action: "speed", Speed: 60})
	for _, line := range lines {
		for _, size := range []int{12, 13, 20} {
			submit(session.Command{Action: "trip", Origin: line[0], Destination: line[1], PartySize: size,
				SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: services[line[0]], OrderContract: sim.ExpressOrderContract})
		}
	}
	// seen holds the party sizes aboard by the origin of the order, so each
	// line has its own loads.
	type load struct {
		from string
		size int
	}
	seen := map[load]bool{}
	wait("loads of 12, 13, and 20 on each line", 2*time.Minute, func(state session.State) bool {
		for _, v := range state.Simulation.Vehicles {
			if v.Pod.Class != sim.ExpressClass {
				continue
			}
			for _, r := range v.Riders {
				if !r.Completed && r.BoardedTick > 0 {
					seen[load{r.From, r.PartySize}] = true
				}
			}
		}
		for _, line := range lines {
			for _, size := range []int{12, 13, 20} {
				if !seen[load{line[0], size}] {
					return false
				}
			}
		}
		return true
	})
}

// TestLondonSharedExpressRemoteRecovery runs the sequence recovery, the
// topology replacement of a new epoch, and the atomic rejection of the Go
// client on frames of the shared-track preset (row SX6).
func TestLondonSharedExpressRemoteRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("the shared-track Express frames are large")
	}
	config, err := scenarios.GenerateLondonFullVariant(scenarios.LondonFullShared)
	if err != nil {
		t.Fatal(err)
	}
	// The recovery replaces the topology of the preset with the topology of
	// london-full:dedicated, which has banks and dedicated Express lanes.
	next, err := scenarios.GenerateLondonFullVariant(scenarios.LondonFullDedicated)
	if err != nil {
		t.Fatal(err)
	}
	checkExpressRemoteRecoveryTo(t, config, &next)
	checkExpressInvalidStateHasNoACK(t, config, "940GZZLUWLO", "940GZZLUBNK")
}
