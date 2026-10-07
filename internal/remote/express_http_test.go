package remote

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// The client reads only the state media type. The media types of earlier
// servers fail, whatever the body.
func TestStateHTTPMediaFailClosed(t *testing.T) {
	valid := validStateBody(t)
	exchangeState := func(t *testing.T, media, body string) (session.State, error) {
		t.Helper()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Accept") != session.StateMediaType {
				t.Error("missing negotiated Accept")
			}
			if media != "" {
				w.Header().Set("Content-Type", media)
			}
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(server.Close)
		client := &Client{url: server.URL, http: server.Client()}
		state := session.State{Epoch: "accepted", Simulation: sim.Snapshot{Pending: []sim.Request{{ID: 7, PartySize: 1}}}}
		err := client.exchange(t.Context(), http.MethodGet, "/api/state", nil, &state)
		return state, err
	}
	if _, err := exchangeState(t, session.StateMediaType, valid); err != nil {
		t.Fatalf("refused a valid state of the state media type: %v", err)
	}
	for _, media := range []string{"", "application/json", "application/vnd.podsim.wrong+json", "application/vnd.podsim.express-v1+json", "application/vnd.podsim.compact-pair-v1+json"} {
		for _, body := range []string{
			valid,
			`{"orderContract":"express-v1","textEncoding":"order-text-base64-v1","topology":{},"frame":{}}`,
			`{"epoch":"new","simulation":{"orderContract":"express-v1"}}`,
			`{"epoch":"new","simulation":{"orderContract":null}}`,
			`{"epoch":"new","textEncoding":""}`,
		} {
			name := media + body
			if body == valid {
				name = media + "valid state"
			}
			t.Run(name, func(t *testing.T) {
				state, err := exchangeState(t, media, body)
				if err == nil {
					t.Fatal("accepted a state of another media type")
				}
				before := session.State{Epoch: "accepted", Simulation: sim.Snapshot{Pending: []sim.Request{{ID: 7, PartySize: 1}}}}
				if !reflect.DeepEqual(before, state) {
					t.Fatal("invalid HTTP response mutated accepted state")
				}
			})
		}
	}
}

// validStateBody returns the HTTP state of an Express project, in the
// state media type.
func validStateBody(t *testing.T) string {
	t.Helper()
	shared, err := session.NewWithProject(remoteExpressProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody)
	request.Header.Set("Accept", session.StateMediaType)
	response := httptest.NewRecorder()
	shared.HandlerFS(nil).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("state status %d", response.Code)
	}
	return response.Body.String()
}

func remoteExpressProject(t *testing.T) project.Config {
	t.Helper()
	raw, err := os.ReadFile("../project/testdata/group_public.json")
	if err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err = jsonv2.Unmarshal(raw, &config, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	config.OrderContract = sim.ExpressOrderContract
	classes, err := sim.NewClassSet("group", "express")
	if err != nil {
		t.Fatal(err)
	}
	for i := range config.Network.Lanes {
		config.Network.Lanes[i].VehicleClasses = classes
	}
	for i := range config.Network.Stations {
		station := &config.Network.Stations[i]
		station.VehicleClasses = classes
		for j := range station.Berths {
			station.Berths[j].VehicleClasses = classes
		}
	}
	config.Fleet[0].Class = sim.ExpressClass
	return config
}

func TestExpressRemoteHTTPAndTripMarker(t *testing.T) {
	shared, err := session.NewWithProject(remoteExpressProject(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	server := httptest.NewServer(shared.Handler("missing"))
	t.Cleanup(server.Close)
	client := &Client{url: server.URL, http: server.Client(), commands: make(chan session.Command, 1), connected: true}
	var state session.State
	if err = client.exchange(t.Context(), http.MethodGet, "/api/state", nil, &state); err != nil {
		t.Fatal(err)
	}
	if state.Simulation.OrderContract != sim.ExpressOrderContract {
		t.Fatal("HTTP lost contract")
	}
	client.state = state
	if err = client.Submit(session.Command{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 20}); err != nil {
		t.Fatal(err)
	}
	command := <-client.commands
	if command.OrderContract != sim.ExpressOrderContract || command.Epoch != state.Epoch {
		t.Fatal("trip lost explicit contract or source epoch")
	}
}
