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
			for _, banks := range []string{`null`, `[]`, `[{}]`, `[{"id":"a","entry":"in","exit":"out","berthIDs":["b"]}]`} {
				for _, version := range []int{1, 2, 3} {
					raw := fmt.Sprintf(`{"version":%d,"network":{"stations":[{"banks":%s}]}}`, version, banks)
					var got Config
					if err := decoder.decode([]byte(raw), &got); err == nil {
						t.Fatalf("accepted %s", raw)
					}
				}
			}
			for _, raw := range []string{
				`{"version":2,"network":{"stations":[]}}`,
				`{"version":1,"network":{"stations":[{"banks":null}]}}`,
				`{"version":1,"network":{"stations":[{"banks":[]}]}}`,
				`{"version":1,"network":{"stations":[{"banks":"invalid"}]}}`,
				`{"version":1,"extra":1}`,
				`{"version":1,"network":{"stations":[{"extra":1}]}}`,
				`{"version":1,"network":{"stations":[{"banks":[{"extra":1}]}]}}`,
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
		{"200 berth IDs", `[{"berthIDs":[` + strings.Repeat(`"b",`, MaxBerths-1) + `"b"]}]`, false},
		{"201 berth IDs", `[{"berthIDs":[` + strings.Repeat(`"b",`, MaxBerths) + `"b"]}]`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw := fmt.Sprintf(`{"version":1,"network":{"stations":[{"banks":%s}]}}`, test.value)
			err := scanProjectBanks([]byte(raw))
			if (err != nil) != test.bad {
				t.Fatalf("scan error %v", err)
			}
			if test.bad {
				var config Config
				if err := jsonv2.Unmarshal([]byte(raw), &config, json.DefaultOptionsV1()); err == nil {
					t.Fatal("decoder accepted excessive array")
				}
			}
			// Another case does not name the bank member. The scan leaves
			// it to the decoder, which refuses it as an unknown member.
			upper := fmt.Sprintf(`{"version":1,"network":{"stations":[{"Banks":%s}]}}`, test.value)
			if err := scanProjectBanks([]byte(upper)); err != nil {
				t.Fatalf("scan bounded another case: %v", err)
			}
			var config Config
			if err := jsonv2.Unmarshal([]byte(upper), &config, json.DefaultOptionsV1()); err == nil {
				t.Fatal("decoder accepted Banks")
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
			raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
			if err != nil {
				t.Fatal(err)
			}
			// The encoder omits an empty Banks array, so the decoded
			// project has no banks and is valid.
			if test.name == "empty banks" {
				return
			}
			var decoded Config
			if err := jsonv2.Unmarshal(raw, &decoded, json.DefaultOptionsV1()); err == nil {
				t.Fatal("decoder accepted invalid bank metadata")
			}
		})
	}
}

func TestBankNetworkCloneOwnsNestedStorage(t *testing.T) {
	t.Parallel()
	config := bankMetadataConfig()
	before, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	cloned := Clone(config)
	cloned.Network.Stations[0].Banks[0].ID = "changed"
	cloned.Network.Stations[0].Banks[0].BerthIDs[0] = "changed"
	after, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
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
	before, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"network":{"stations":[{"id":"changed","banks":[{"id":"changed","berthIDs":["changed"]}]}]},"version":2}`)
	if decodeErr := jsonv2.Unmarshal(raw, &config, json.DefaultOptionsV1()); decodeErr == nil {
		t.Fatal("accepted bank metadata in project version 2")
	}
	after, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed decode changed project storage")
	}
	if err := jsonv2.Unmarshal([]byte(`{"demand":{"perMinute":6}}`), &config, json.DefaultOptionsV1()); err != nil {
		t.Fatal(err)
	}
	if config.Version != CurrentVersion || config.Demand.PerMinute != 6 || config.Network.Stations[0].Banks[0].ID != "a" {
		t.Fatal("partial update lost project metadata")
	}
}
