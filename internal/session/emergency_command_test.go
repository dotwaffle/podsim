package session

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// emergencySessionProject returns the example project with the incident
// marker, the emergency marker, and emergencies.
func emergencySessionProject(emergencies project.EmergencyConfig) project.Config {
	config := project.Default()
	config.IncidentContract = sim.IncidentV1Contract
	config.EmergencyContract = project.EmergencyV1Contract
	config.Emergencies = &emergencies
	config.SharedRidePartyLimit = 4
	return config
}

// boardOrders submits n orders from harbor to market, ends the pause, and
// steps the session until pod 01 carries each order. It then pauses the
// session again, and returns the order IDs.
func (c *testClient) boardOrders(t *testing.T, n int) []int {
	t.Helper()
	c.session.mu.Lock()
	orders := make([]int, n)
	for index := range orders {
		id, err := c.session.simulation.SubmitTripOptions(sim.TripOptions{From: "harbor", To: "market", SharingConsent: sim.SharedConsent})
		if err != nil {
			c.session.mu.Unlock()
			t.Fatal(err)
		}
		orders[index] = id
	}
	c.session.mu.Unlock()
	c.mustApply(t, Command{Action: "pause", Paused: false})
	c.session.mu.Lock()
	for activeRiders(savedPod(c.session.simulation.ExportState(), "01")) < n {
		if c.session.simulation.Tick() > 600*sim.TicksPerSecond {
			c.session.mu.Unlock()
			t.Fatal("pod 01 does not board the orders")
		}
		if err := c.session.step(); err != nil {
			c.session.mu.Unlock()
			t.Fatal(err)
		}
	}
	c.session.mu.Unlock()
	c.mustApply(t, Command{Action: "pause", Paused: true})
	return orders
}

// emergencyHeld reports whether pod 01 of the session has the emergency
// hold.
func (c *testClient) emergencyHeld() bool {
	c.session.mu.Lock()
	defer c.session.mu.Unlock()
	return savedPod(c.session.simulation.ExportState(), "01").Withdrawn&2 != 0
}

// TestEmergencyCommand checks the emergency command in a paused session.
// The reply has the emergency ID, an exact retry gets the stored reply and
// starts no second emergency, and the pod gets the emergency hold. The
// party is the order of the command, so a second command for the pod is
// refused.
func TestEmergencyCommand(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, emergencySessionProject(project.EmergencyConfig{}))
	orders := client.boardOrders(t, 2)
	command := client.next(Command{Action: "emergency", PodID: "01", OrderID: orders[1]})
	reply := client.session.Apply(command)
	if reply.Error != "" || reply.EmergencyID != "i1.1" || reply.FaultID != "" {
		t.Fatalf("emergency reply %+v, want emergency i1.1", reply)
	}
	if retry := client.session.Apply(command); !reflect.DeepEqual(retry, reply) {
		t.Fatalf("retry reply %+v, want %+v", retry, reply)
	}
	if !client.emergencyHeld() {
		t.Fatal("pod 01 has no emergency hold")
	}
	client.mustReject(t, Command{Action: "emergency", PodID: "01"}, "pod already has an emergency")
	client.mustReject(t, Command{Action: "emergency", PodID: "01", OrderID: orders[0]}, "pod already has an emergency")
}

// TestEmergencyCommandErrors checks the message of each command error of
// the incident emergency contract (section 10.4) that a session of the
// example project can reach. Each rejection is command_rejected and
// changes nothing. The simulation tests reach the limits.
func TestEmergencyCommandErrors(t *testing.T) {
	t.Parallel()
	incident := project.Default()
	incident.IncidentContract = sim.IncidentV1Contract
	for _, config := range []project.Config{project.Default(), incident} {
		unmarked := newFaultClient(t, config)
		unmarked.mustReject(t, Command{Action: "emergency", PodID: "01"}, "emergencies are not enabled")
		unmarked.mustReject(t, Command{Action: "emergency"}, "emergencies are not enabled")
	}
	client := newFaultClient(t, emergencySessionProject(project.EmergencyConfig{}))
	orders := client.boardOrders(t, 1)
	for _, test := range []struct {
		command Command
		message string
	}{
		{Command{Action: "emergency"}, "unknown pod"},
		{Command{Action: "emergency", OrderID: orders[0]}, "unknown pod"},
		{Command{Action: "emergency", PodID: "99"}, "unknown pod"},
		{Command{Action: "emergency", PodID: "02"}, "pod carries no passenger"},
		{Command{Action: "emergency", PodID: "02", OrderID: orders[0]}, "pod carries no passenger"},
		{Command{Action: "emergency", PodID: "01", OrderID: orders[0] + 1}, "order is not aboard the pod"},
		{Command{Action: "emergency", PodID: "01", OrderID: -1}, "order is not aboard the pod"},
	} {
		client.mustReject(t, test.command, test.message)
	}
	// The default party is the first active rider.
	if reply := client.mustApply(t, Command{Action: "emergency", PodID: "01"}); reply.EmergencyID != "i1.1" {
		t.Fatalf("emergency reply %+v", reply)
	}
	client.mustReject(t, Command{Action: "emergency", PodID: "01", OrderID: orders[0]}, "pod already has an emergency")
}

// TestEmergencyCommandWithCouplingFault checks that a retained coupling
// fault rejects the emergency action, as it rejects other actions.
func TestEmergencyCommandWithCouplingFault(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, emergencySessionProject(project.EmergencyConfig{}))
	client.boardOrders(t, 1)
	setViewFault(t, client.session)
	reply := client.session.Apply(client.next(Command{Action: "emergency", PodID: "01"}))
	if reply.ErrorCode != CommandRejected || !strings.Contains(reply.Error, "test observation fault") || reply.EmergencyID != "" {
		t.Fatalf("emergency with a coupling fault: %+v", reply)
	}
	if client.emergencyHeld() {
		t.Fatal("the rejected command started an emergency")
	}
}

// TestEmergencySessionEvents checks the emergencies across the session
// events of section 10.7 of the incident emergency contract. A reset, a
// rewind, and the apply of a changed project end every record and keep
// emergencies on. The apply of the same project keeps the record. The
// apply of a project without the marker turns emergencies off. A demo
// ends every record and turns emergencies off until a reset. The empty
// pod 02 tells whether emergencies are on.
func TestEmergencySessionEvents(t *testing.T) {
	t.Parallel()
	config := emergencySessionProject(project.EmergencyConfig{})
	changed := emergencySessionProject(project.EmergencyConfig{PerHour: new(0.0)})
	unmarked := project.Default()
	unmarked.IncidentContract = sim.IncidentV1Contract
	tests := []struct {
		name string
		// event runs the event after the emergency command.
		event func(t *testing.T, client *testClient)
		// kept tells whether the record stays, and on whether emergencies
		// stay on.
		kept, on bool
	}{
		{"reset", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "reset"})
		}, false, true},
		{"same project", func(t *testing.T, client *testClient) {
			t.Helper()
			same := project.Clone(config)
			client.mustApply(t, Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &same})
		}, true, true},
		{"changed emergencies", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &changed})
		}, false, true},
		{"unmarked project", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "project", ProjectRevision: client.session.Project().Revision, Project: &unmarked})
		}, false, false},
		{"demo", func(t *testing.T, client *testClient) {
			t.Helper()
			client.mustApply(t, Command{Action: "demo"})
			client.mustApply(t, Command{Action: "pause", Paused: true})
		}, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newFaultClient(t, config)
			client.boardOrders(t, 1)
			client.mustApply(t, Command{Action: "emergency", PodID: "01"})
			test.event(t, client)
			if held := client.emergencyHeld(); held != test.kept {
				t.Fatalf("emergency hold %t, want %t", held, test.kept)
			}
			if test.kept {
				client.mustReject(t, Command{Action: "emergency", PodID: "01"}, "pod already has an emergency")
			}
			message := "emergencies are not enabled"
			if test.on {
				message = "pod carries no passenger"
			}
			client.mustReject(t, Command{Action: "emergency", PodID: "02"}, message)
			if test.name == "demo" {
				client.mustApply(t, Command{Action: "reset"})
				client.mustReject(t, Command{Action: "emergency", PodID: "02"}, "pod carries no passenger")
			}
		})
	}
	t.Run("rewind", func(t *testing.T) {
		t.Parallel()
		client := newFaultClient(t, config)
		client.boardOrders(t, 1)
		checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
		id := client.mustApply(t, Command{Action: "emergency", PodID: "01"}).EmergencyID
		client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
		if client.emergencyHeld() {
			t.Fatal("the rewound session keeps the emergency hold")
		}
		if again := client.mustApply(t, Command{Action: "emergency", PodID: "01"}).EmergencyID; again == "" || again == id {
			t.Fatalf("the rewound session gave the emergency ID %q after %s", again, id)
		}
	})
}

// TestEmergencyRestoreKeepsEmergenciesOn checks that a session restored
// from a save of a project with the emergency marker has emergencies on,
// in the physical and in the logical tier, and that a restored traffic
// demo has emergencies off, as the demo command leaves them.
func TestEmergencyRestoreKeepsEmergenciesOn(t *testing.T) {
	t.Parallel()
	save := func(t *testing.T, demo bool) storedRun {
		t.Helper()
		config := emergencySessionProject(project.EmergencyConfig{})
		store := &fakeStore{}
		s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
		if err != nil {
			t.Fatal(err)
		}
		client := newTestClient(s, "emergencies")
		client.mustApply(t, Command{Action: "pause", Paused: true})
		if demo {
			client.mustApply(t, Command{Action: "demo"})
			client.mustApply(t, Command{Action: "pause", Paused: true})
		}
		if err = s.SaveState(t.Context(), SavePeriodic); err != nil {
			t.Fatal(err)
		}
		s.Close()
		writes := store.writeList()
		return storedRun{data: writes[len(writes)-1]}
	}
	plain, demo := save(t, false), save(t, true)
	tests := []struct {
		name string
		data []byte
		tier string
		on   bool
	}{
		{"physical tier", plain.data, "physical", true},
		{"logical tier", plain.edited(t, logicalOnly), "logical", true},
		{"running demo", demo.data, "physical", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: test.data}})
			if err != nil || restored.restore.Tier != test.tier {
				t.Fatalf("restore %v, %+v", err, restored.restore)
			}
			t.Cleanup(restored.Close)
			next := newTestClient(restored, "emergencies-restored")
			next.mustApply(t, Command{Action: "pause", Paused: true})
			message := "emergencies are not enabled"
			if test.on {
				message = "pod carries no passenger"
			}
			next.mustReject(t, Command{Action: "emergency", PodID: "02"}, message)
		})
	}
}

// TestEmergencyCommandOverHTTP checks the member names of the emergency
// command and its reply. An order ID that is not an integer is invalid
// command JSON, and an order that is not aboard is command_rejected.
func TestEmergencyCommandOverHTTP(t *testing.T) {
	t.Parallel()
	client := newFaultClient(t, emergencySessionProject(project.EmergencyConfig{}))
	orders := client.boardOrders(t, 2)
	handler := client.session.Handler(t.TempDir())
	post := func(members string) (int, Reply, string) {
		t.Helper()
		client.sequence++
		body := fmt.Sprintf(`{"client":%q,"sequence":%d,"epoch":%q,%s}`, client.name, client.sequence, client.epoch, members)
		recorder := postCommand(t, handler, []byte(body))
		var reply Reply
		_ = json.Unmarshal(recorder.Body.Bytes(), &reply)
		return recorder.Code, reply, recorder.Body.String()
	}
	if code, _, _ := post(`"action":"emergency","podID":"01","orderID":1.5`); code != http.StatusBadRequest {
		t.Fatalf("order 1.5: %d", code)
	}
	if code, reply, _ := post(fmt.Sprintf(`"action":"emergency","podID":"01","orderID":%d`, orders[1]+1)); code != http.StatusConflict || reply.Error != "order is not aboard the pod" {
		t.Fatalf("order not aboard: %d %+v", code, reply)
	}
	code, reply, body := post(fmt.Sprintf(`"action":"emergency","podID":"01","orderID":%d`, orders[1]))
	if code != http.StatusOK || reply.EmergencyID != "i1.1" || !strings.Contains(body, `"emergencyID":"i1.1"`) {
		t.Fatalf("emergency: %d %s", code, body)
	}
}
