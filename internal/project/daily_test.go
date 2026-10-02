package project

import (
	"encoding/json/v2"
	"math"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func dailyTestConfig() Config {
	config := Default()
	config.Demand = DemandConfig{Enabled: true, Pattern: "profile-daily", Profile: "park", PerMinute: 12, Seed: 42}
	config.DemandProfiles = []DemandProfile{{
		ID: "park", Name: "Park and ride",
		Bands: []DemandBand{
			{ID: "morning", Name: "Morning", StartMinute: 0, DurationMinutes: 1},
			{ID: "evening", Name: "Evening", StartMinute: 2, DurationMinutes: 1, PerMinute: new(6)},
		},
		Flows: []DemandFlow{{From: "harbor", To: "garden", Weights: []float64{3, 0}}, {From: "garden", To: "harbor", Weights: []float64{0, 1}}},
	}}
	return config
}

func TestDailyProfileClockAndWeights(t *testing.T) {
	t.Parallel()
	config := dailyTestConfig()
	daily, err := NewDailyProfile(config.Demand, config.DemandProfiles)
	if err != nil {
		t.Fatal(err)
	}
	minute := int64(60 * sim.TicksPerSecond)
	for _, test := range []struct {
		tick       int64
		band, rate int
	}{
		{0, 0, 12}, {1, 0, 12}, {minute, 0, 12}, {minute + 1, -1, 0},
		{2 * minute, -1, 0}, {2*minute + 1, 1, 6}, {3 * minute, 1, 6}, {3*minute + 1, -1, 0},
		{minutesPerDay * minute, -1, 0}, {minutesPerDay*minute + 1, 0, 12}, {math.MinInt64, 0, 12},
	} {
		if band := daily.Band(test.tick); band != test.band || daily.Rate(band) != test.rate {
			t.Errorf("tick %d: band %d rate %d, want %d/%d", test.tick, band, daily.Rate(band), test.band, test.rate)
		}
	}
	if got := daily.Weights(0); !reflect.DeepEqual(got, map[string]float64{"harbor": 3}) {
		t.Fatal(got)
	}
	weights := daily.Weights(0)
	weights["harbor"] = 99
	config.DemandProfiles[0].Flows[0].Weights[0] = 99
	*config.DemandProfiles[0].Bands[1].PerMinute = 100
	if daily.Weights(0)["harbor"] != 3 || daily.Rate(1) != 6 {
		t.Fatal("compiled profile shares mutable input or returned storage")
	}
	for _, draw := range []float64{0, .5, math.Nextafter(1, 0)} {
		from, to, ok := daily.Pair(1, draw)
		if !ok || from != "garden" || to != "harbor" {
			t.Fatal(from, to, ok)
		}
	}
	if _, _, ok := daily.Pair(-1, .5); ok {
		t.Fatal("gap returned a pair")
	}
}

func TestDailyProfileWrapAndValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		change func(*Config)
		valid  bool
	}{
		{"baseline", func(*Config) {}, true},
		{"midnight wrap", func(c *Config) {
			c.DemandProfiles[0].Bands[0].StartMinute = 1439
			c.DemandProfiles[0].Bands[0].DurationMinutes = 2
		}, true},
		{"overlap", func(c *Config) { c.DemandProfiles[0].Bands[1].StartMinute = 0 }, false},
		{"wrap overlap", func(c *Config) {
			c.DemandProfiles[0].Bands[0].StartMinute = 1439
			c.DemandProfiles[0].Bands[0].DurationMinutes = 4
		}, false},
		{"zero rate", func(c *Config) { c.DemandProfiles[0].Bands[0].PerMinute = new(0) }, true},
		{"rate too high", func(c *Config) { c.DemandProfiles[0].Bands[0].PerMinute = new(121) }, false},
		{"negative rate", func(c *Config) { c.DemandProfiles[0].Bands[0].PerMinute = new(-1) }, false},
		{"start negative", func(c *Config) { c.Demand.DailyStartMinute = -1 }, false},
		{"start next day", func(c *Config) { c.Demand.DailyStartMinute = 1440 }, false},
		{"legacy rejects daily clock", func(c *Config) {
			c.Demand.Pattern = "profile"
			c.Demand.Band = "morning"
			c.Demand.DailyStartMinute = 1
		}, false},
		{"legacy retains overlap", func(c *Config) {
			c.Demand.Pattern = "profile"
			c.Demand.Band = "morning"
			c.DemandProfiles[0].Bands[1].StartMinute = 0
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := dailyTestConfig()
			test.change(&config)
			if err := Validate(config); (err == nil) != test.valid {
				t.Fatalf("Validate: %v, valid %t", err, test.valid)
			}
		})
	}
	config := dailyTestConfig()
	config.DemandProfiles[0].Bands = config.DemandProfiles[0].Bands[:1]
	config.DemandProfiles[0].Bands[0].StartMinute = 1439
	config.DemandProfiles[0].Bands[0].DurationMinutes = 1440
	for i := range config.DemandProfiles[0].Flows {
		config.DemandProfiles[0].Flows[i].Weights = config.DemandProfiles[0].Flows[i].Weights[:1]
	}
	daily, err := NewDailyProfile(config.Demand, config.DemandProfiles)
	if err != nil {
		t.Fatal(err)
	}
	for minute := range minutesPerDay {
		if daily.Band(int64(minute)*60*sim.TicksPerSecond+1) != 0 {
			t.Fatal("full-day wrap has a gap")
		}
	}
}

func TestDailyOptionalRateAndClone(t *testing.T) {
	t.Parallel()
	config := dailyTestConfig()
	config.DemandProfiles[0].Bands[1].PerMinute = new(0)
	cloned := Clone(config)
	*cloned.DemandProfiles[0].Bands[1].PerMinute = 7
	if *config.DemandProfiles[0].Bands[1].PerMinute != 0 {
		t.Fatal("clone shares optional rate")
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Config
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.DemandProfiles[0].Bands[0].PerMinute != nil || roundtrip.DemandProfiles[0].Bands[1].PerMinute == nil || *roundtrip.DemandProfiles[0].Bands[1].PerMinute != 0 {
		t.Fatal("optional rate presence changed")
	}
}

func TestDailyZeroClockKeepsLegacyEncoding(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(Default().Demand)
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]any
	if err := json.Unmarshal(data, &members); err != nil {
		t.Fatal(err)
	}
	if _, present := members["dailyStartMinute"]; present {
		t.Fatal("zero daily clock changed legacy demand encoding")
	}
}
