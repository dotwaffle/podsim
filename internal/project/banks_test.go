package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func bankMetadataConfig() Config {
	config := Default()
	station := &config.Network.Stations[0]
	bank := sim.StationBank{ID: "a", Entry: station.Entry, Exit: station.Exit}
	for _, berth := range station.Berths {
		bank.BerthIDs = append(bank.BerthIDs, berth.ID)
	}
	station.Banks = []sim.StationBank{bank}
	return config
}

func TestBankProjectJSONVersions(t *testing.T) {
	t.Parallel()
	for _, decoder := range []struct {
		name   string
		decode func([]byte, any) error
	}{
		{"legacy", json.Unmarshal},
		{"v2", func(data []byte, value any) error { return jsonv2.Unmarshal(data, value) }},
	} {
		t.Run(decoder.name, func(t *testing.T) {
			t.Parallel()
			for _, config := range []Config{Default(), bankMetadataConfig()} {
				data, err := jsonv2.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				var got Config
				if err := decoder.decode(data, &got); err != nil || !reflect.DeepEqual(config, got) {
					t.Fatalf("version %d round trip: %v", config.Version, err)
				}
			}
			for _, banks := range []string{`null`, `[]`, `[{}]`, `[{"ID":"a","Entry":"in","Exit":"out","BerthIDs":["b"]}]`} {
				for _, version := range []int{1, 2, 3} {
					raw := fmt.Sprintf(`{"version":%d,"network":{"Stations":[{"Banks":%s}]}}`, version, banks)
					var got Config
					if err := decoder.decode([]byte(raw), &got); err == nil {
						t.Fatalf("accepted %s", raw)
					}
				}
			}
			for _, raw := range []string{
				`{"version":2,"network":{"Stations":[]}}`,
				`{"version":1,"network":{"Stations":[{"Banks":null}]}}`,
				`{"version":1,"network":{"Stations":[{"Banks":[]}]}}`,
				`{"version":1,"network":{"Stations":[{"Banks":"invalid"}]}}`,
				`{"version":1,"extra":1}`,
				`{"version":1,"network":{"Stations":[{"extra":1}]}}`,
				`{"version":1,"network":{"Stations":[{"Banks":[{"extra":1}]}]}}`,
			} {
				var got Config
				if err := decoder.decode([]byte(raw), &got); err == nil {
					t.Fatalf("accepted %s", raw)
				}
			}
		})
	}
}

func TestBankProjectPrescanBounds(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value string
		bad   bool
	}{
		{"eight banks", `[` + strings.Repeat(`{},`, sim.MaxStationBanks-1) + `{}]`, false},
		{"nine banks", `[` + strings.Repeat(`{},`, sim.MaxStationBanks) + `{}]`, true},
		{"200 berth IDs", `[{"BerthIDs":[` + strings.Repeat(`"b",`, MaxBerths-1) + `"b"]}]`, false},
		{"201 berth IDs", `[{"BerthIDs":[` + strings.Repeat(`"b",`, MaxBerths) + `"b"]}]`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, member := range []string{"Banks", "banks"} {
				raw := fmt.Sprintf(`{"version":1,"network":{"Stations":[{%q:%s}]}}`, member, test.value)
				err := scanProjectBanks([]byte(raw))
				if (err != nil) != test.bad {
					t.Fatalf("scan error %v", err)
				}
				if test.bad {
					var config Config
					if err := json.Unmarshal([]byte(raw), &config); err == nil {
						t.Fatal("decoder accepted excessive array")
					}
				}
			}
		})
	}
}

func TestBankMetadataValidation(t *testing.T) {
	t.Parallel()
	base := bankMetadataConfig()
	if err := validateNames(base); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"version 2", func(c *Config) { c.Version = 2 }},
		{"empty banks", func(c *Config) { c.Network.Stations[0].Banks = []sim.StationBank{} }},
		{"nine banks", func(c *Config) { c.Network.Stations[0].Banks = make([]sim.StationBank, 9) }},
		{"long bank ID", func(c *Config) { c.Network.Stations[0].Banks[0].ID = strings.Repeat("x", 65) }},
		{"empty bank ID", func(c *Config) { c.Network.Stations[0].Banks[0].ID = "" }},
		{"long entry", func(c *Config) {
			c.Network.Stations[0].Banks[0].Entry = strings.Repeat("x", 65)
			c.Network.Stations[0].Entry = c.Network.Stations[0].Banks[0].Entry
		}},
		{"long exit", func(c *Config) {
			c.Network.Stations[0].Banks[0].Exit = strings.Repeat("x", 65)
			c.Network.Stations[0].Exit = c.Network.Stations[0].Banks[0].Exit
		}},
		{"duplicate bank ID", func(c *Config) {
			c.Network.Stations[0].Banks = append(c.Network.Stations[0].Banks, c.Network.Stations[0].Banks[0])
		}},
		{"empty members", func(c *Config) { c.Network.Stations[0].Banks[0].BerthIDs = nil }},
		{"201 members", func(c *Config) { c.Network.Stations[0].Banks[0].BerthIDs = make([]string, 201) }},
		{"unknown berth", func(c *Config) { c.Network.Stations[0].Banks[0].BerthIDs[0] = "unknown" }},
		{"long berth ID", func(c *Config) { c.Network.Stations[0].Banks[0].BerthIDs[0] = strings.Repeat("x", 65) }},
		{"duplicate membership", func(c *Config) {
			c.Network.Stations[0].Banks[0].BerthIDs = append(c.Network.Stations[0].Banks[0].BerthIDs, c.Network.Stations[0].Banks[0].BerthIDs[0])
		}},
		{"missing membership", func(c *Config) {
			c.Network.Stations[0].Banks[0].BerthIDs = slices.Delete(c.Network.Stations[0].Banks[0].BerthIDs, 0, 1)
		}},
		{"wrong alias", func(c *Config) { c.Network.Stations[0].Entry = "wrong" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Clone(base)
			test.change(&config)
			if err := Validate(config); err == nil {
				t.Fatal("accepted invalid bank metadata")
			}
			raw, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			// The encoder omits an empty Banks array, so the decoded
			// project has no banks and is valid.
			if test.name == "empty banks" {
				return
			}
			var decoded Config
			if err := json.Unmarshal(raw, &decoded); err == nil {
				t.Fatal("decoder accepted invalid bank metadata")
			}
		})
	}
}

func TestBankNetworkCloneOwnsNestedStorage(t *testing.T) {
	t.Parallel()
	config := bankMetadataConfig()
	before, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	cloned := Clone(config)
	cloned.Network.Stations[0].Banks[0].ID = "changed"
	cloned.Network.Stations[0].Banks[0].BerthIDs[0] = "changed"
	after, err := json.Marshal(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("clone aliases bank storage")
	}
	config.Network.Stations[0].Banks = []sim.StationBank{}
	if Clone(config).Network.Stations[0].Banks == nil {
		t.Fatal("clone lost explicit empty Banks")
	}
	config.Network.Stations[0].Banks = nil
	if Clone(config).Network.Stations[0].Banks != nil {
		t.Fatal("clone added Banks")
	}
}

func TestBankMetadataAcceptsNameAndCountLimits(t *testing.T) {
	t.Parallel()
	station := sim.Station{ID: "station"}
	for index := range sim.MaxStationBanks {
		station.Banks = append(station.Banks, sim.StationBank{
			ID: fmt.Sprintf("%064d", index), Entry: fmt.Sprintf("entry-%058d", index), Exit: fmt.Sprintf("exit-%059d", index),
		})
	}
	station.Entry, station.Exit = station.Banks[0].Entry, station.Banks[0].Exit
	for index := range MaxBerths {
		id := fmt.Sprintf("b%063d", index)
		station.Berths = append(station.Berths, sim.Berth{ID: id})
		bank := index % len(station.Banks)
		station.Banks[bank].BerthIDs = append(station.Banks[bank].BerthIDs, id)
	}
	if err := validateBankNames(station); err != nil {
		t.Fatal(err)
	}
}

func TestBankProjectDecodeFailureKeepsStorage(t *testing.T) {
	t.Parallel()
	config := bankMetadataConfig()
	before, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"network":{"Stations":[{"ID":"changed","Banks":[{"ID":"changed","BerthIDs":["changed"]}]}]},"version":2}`)
	if decodeErr := json.Unmarshal(raw, &config); decodeErr == nil {
		t.Fatal("accepted bank metadata in project version 2")
	}
	after, err := json.Marshal(config)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed decode changed project storage")
	}
	if err := json.Unmarshal([]byte(`{"demand":{"perMinute":6}}`), &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentVersion || config.Demand.PerMinute != 6 || config.Network.Stations[0].Banks[0].ID != "a" {
		t.Fatal("partial update lost project metadata")
	}
}
