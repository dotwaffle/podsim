package remote

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLondonExpressRemoteLoads runs the Go client against a live session of
// the London Express preset over HTTP and WebSocket. The client must read the
// topology of the preset, and its stream must show Express pods with loads of
// 12, 13, and 20 riders aboard.
func TestLondonExpressRemoteLoads(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the London Express remote run needs a full-speed simulation")
	}
	const waterloo, bank = "940GZZLUWLO", "940GZZLUBNK"
	config := scenarios.LondonFullExpress()
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
	station, ok := state.Network.Station(bank)
	if _, parking := state.Network.Station("express-parking"); !ok || len(station.Banks) != 2 || !parking {
		t.Fatal("the client topology is not the London Express preset")
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
	for _, size := range []int{12, 13, 20} {
		submit(session.Command{Action: "trip", Origin: waterloo, Destination: bank, PartySize: size,
			SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: services[waterloo], OrderContract: sim.ExpressOrderContract})
	}
	seen := map[int]bool{}
	wait("loads of 12, 13, and 20", 2*time.Minute, func(state session.State) bool {
		for _, v := range state.Simulation.Vehicles {
			if v.Pod.Class != sim.ExpressClass {
				continue
			}
			for _, r := range v.Riders {
				if !r.Completed && r.BoardedTick > 0 {
					seen[r.PartySize] = true
				}
			}
		}
		return seen[12] && seen[13] && seen[20]
	})
}

// TestLondonExpressRemoteRecovery runs the sequence recovery and the atomic
// rejection of the Go client on frames of the London Express preset.
func TestLondonExpressRemoteRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("the London Express frames are large")
	}
	checkExpressRemoteRecovery(t, scenarios.LondonFullExpress())
	checkExpressInvalidStateHasNoACK(t, scenarios.LondonFullExpress(), "940GZZLUWLO", "940GZZLUBNK")
}
