package session

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestOrderCommandExplicitFields(t *testing.T) {
	for _, decode := range []struct {
		name string
		run  func([]byte, any) error
	}{
		{"legacy", json.Unmarshal},
		{"v2", func(raw []byte, target any) error { return jsonv2.Unmarshal(raw, target) }},
	} {
		t.Run(decode.name, func(t *testing.T) {
			for _, test := range []struct {
				name, fields string
				want         sim.TripOptions
			}{
				{"omitted", "", sim.TripOptions{}},
				{"private group", `,"partySize":4,"sharingConsent":"private"`, sim.TripOptions{PartySize: 4, SharingConsent: sim.PrivateConsent}},
				{"shared", `,"sharingConsent":"shared","service":"on-demand"`, sim.TripOptions{SharingConsent: sim.SharedConsent, Service: sim.OnDemandService}},
				{"express", `,"partySize":1,"sharingConsent":"shared","service":"express","serviceID":"hub-pair"`, sim.TripOptions{PartySize: 1, SharingConsent: sim.SharedConsent, Service: sim.ExpressServiceChoice, ServiceID: "hub-pair"}},
			} {
				t.Run(test.name, func(t *testing.T) {
					var got Command
					raw := `{"action":"trip","origin":"harbor","destination":"market"` + test.fields + `}`
					if err := decode.run([]byte(raw), &got); err != nil {
						t.Fatal(err)
					}
					if got.PartySize != test.want.PartySize || got.SharingConsent != test.want.SharingConsent ||
						got.Service != test.want.Service || got.ServiceID != test.want.ServiceID {
						t.Fatalf("effective command fields changed: %+v", got)
					}
				})
			}
		})
	}
}

func TestOrderCommandRejectsUnchanged(t *testing.T) {
	tests := []string{
		`"partySize":null`, `"partySize":0`, `"partySize":9`, `"partySize":1.5`, `"partySize":"1"`,
		`"sharingConsent":null`, `"sharingConsent":""`, `"sharingConsent":"legacy-unknown"`, `"sharingConsent":true`,
		`"service":null`, `"service":""`, `"service":"unknown"`, `"service":[]`,
		`"serviceID":null`, `"serviceID":""`, `"serviceID":"pair"`,
		`"service":"express"`, `"service":"express","serviceID":"pair"`,
		`"sharingConsent":"private","service":"express","serviceID":"pair"`,
		`"sharingConsent":"shared","service":"express","serviceID":"` + strings.Repeat("x", 65) + `"`,
		`"partySize":1,"partySize":2`, `"service":"on-demand","SERVICE":"express"`,
		`"sharingConsent":"private","unknown":true`,
	}
	for _, fields := range tests {
		t.Run(fields, func(t *testing.T) {
			config := project.Default()
			got := Command{Action: "trip", Origin: "old", Project: &config}
			want := got
			want.Project = new(project.Clone(config))
			raw := `{"action":"trip","origin":"new",` + fields + `}`
			if err := jsonv2.Unmarshal([]byte(raw), &got, json.DefaultOptionsV1()); err == nil {
				t.Fatal("invalid explicit fields accepted")
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal("rejection changed command or retained project")
			}
		})
	}
	for _, action := range []string{"pause", "demand", "project", ""} {
		var got Command
		if err := jsonv2.Unmarshal([]byte(`{"action":"`+action+`","partySize":1}`), &got, json.DefaultOptionsV1()); err == nil {
			t.Fatalf("%q ignored an order field", action)
		}
	}
}

func TestOrderCommandDecodeFailureKeepsProject(t *testing.T) {
	t.Parallel()
	config := project.Default()
	got := Command{Project: &config}
	want := Command{Project: new(project.Clone(config))}
	if err := jsonv2.Unmarshal([]byte(`{"project":{"name":"Changed"},"unknown":true}`), &got, json.DefaultOptionsV1()); err == nil {
		t.Fatal("unknown command field accepted")
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("failed decode changed a nested project")
	}
}

func TestOrderCommandDigestPreservesChoice(t *testing.T) {
	t.Parallel()
	base := Command{Action: "trip", Origin: "harbor", Destination: "market"}
	for _, change := range []func(*Command){
		func(c *Command) { c.PartySize = 1 },
		func(c *Command) { c.SharingConsent = sim.PrivateConsent },
		func(c *Command) { c.Service = sim.OnDemandService },
		func(c *Command) { c.ServiceID = "pair" },
	} {
		next := base
		change(&next)
		if digestCommand(base).matches(digestCommand(next)) {
			t.Fatalf("retry digest ignored changed choice: %+v", next)
		}
	}
}

func TestOrderCommandEncodingKeepsOmittedOptions(t *testing.T) {
	t.Parallel()
	for _, encode := range []struct {
		name string
		run  func(any) ([]byte, error)
	}{
		{"legacy", json.Marshal},
		{"v2", func(value any) ([]byte, error) { return jsonv2.Marshal(value) }},
	} {
		t.Run(encode.name, func(t *testing.T) {
			t.Parallel()
			for _, command := range []Command{
				{Action: "pause", Paused: true},
				{Action: "trip", Origin: "harbor", Destination: "market"},
				{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 4, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService},
			} {
				raw, err := encode.run(command)
				if err != nil {
					t.Fatal(err)
				}
				var got Command
				if err := jsonv2.Unmarshal(raw, &got, json.DefaultOptionsV1()); err != nil {
					t.Fatalf("decode %s: %v", raw, err)
				}
				if !reflect.DeepEqual(command, got) {
					t.Fatalf("changed order options: got %+v, want %+v", got, command)
				}
				if command.PartySize == 0 && strings.Contains(string(raw), `"partySize"`) {
					t.Fatalf("omitted party size encoded: %s", raw)
				}
			}
		})
	}
}

// A command member whose case differs from the declared name is unknown,
// so both decoders refuse the command.
func TestCommandRefusesCaseVariantMembers(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"client":"c","sequence":1,"action":"pause","Paused":true}`,
		`{"Client":"c","sequence":1,"action":"pause","paused":true}`,
		`{"client":"c","sequence":1,"action":"trip","origin":"a","destination":"b","PartySize":2}`,
	} {
		var legacy, v2 Command
		if err := jsonv2.Unmarshal([]byte(raw), &legacy, json.DefaultOptionsV1()); err == nil {
			t.Errorf("legacy decoder accepted %s", raw)
		}
		if err := jsonv2.Unmarshal([]byte(raw), &v2); err == nil {
			t.Errorf("v2 decoder accepted %s", raw)
		}
	}
	var exact Command
	if err := jsonv2.Unmarshal([]byte(`{"client":"c","sequence":1,"action":"pause","paused":true}`), &exact, json.DefaultOptionsV1()); err != nil || !exact.Paused {
		t.Fatal("exact command changed", err)
	}
}
