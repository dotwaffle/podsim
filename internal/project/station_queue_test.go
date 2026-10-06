package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

func TestStationQueueProjectVersionAndShape(t *testing.T) {
	t.Parallel()
	for _, codec := range []struct {
		name   string
		decode func([]byte, any) error
	}{
		{"json", json.Unmarshal},
		{"jsonv2", func(data []byte, value any) error { return jsonv2.Unmarshal(data, value) }},
	} {
		t.Run(codec.name, func(t *testing.T) {
			t.Parallel()
			for _, version := range []int{1, 2, 3} {
				for _, raw := range []string{`"ordinary"`, `"compact-v1"`, `null`, `""`, `"other"`, `false`, `[]`, `{}`} {
					config := Default()
					before := Clone(config)
					err := codec.decode(fmt.Appendf(nil, `{"version":%d,"stationQueueSpacing":%s}`, version, raw), &config)
					accepted := version == CurrentVersion && (raw == `"ordinary"` || raw == `"compact-v1"`)
					if (err == nil) != accepted {
						t.Fatalf("version %d spacing %s: %v", version, raw, err)
					}
					if err != nil && !reflect.DeepEqual(before, config) {
						t.Fatal("rejected queue policy changed the destination")
					}
				}
			}
		})
	}
}

func TestStationQueueProjectLimitsAndConfiguration(t *testing.T) {
	t.Parallel()
	for _, buffers := range []bool{false, true} {
		for _, limit := range []int{0, 1, 2, 3, 4, 5} {
			config := Default()
			config.StationQueueSpacing = sim.StationQueueCompactV1
			config.StationBuffers, config.PlatoonLimit = PolicyFlag(buffers), limit
			valid := buffers && limit >= sim.MinPlatoonLimit && limit <= sim.MaxPlatoonLimit
			if err := Validate(config); (err == nil) != valid {
				t.Fatalf("buffers %v, limit %d: %v", buffers, limit, err)
			}
			if !valid {
				continue
			}
			s, err := sim.NewFleet(config.Network, config.Fleet)
			if err != nil {
				t.Fatal(err)
			}
			if err := ConfigurePlatoons(s, config); err != nil {
				t.Fatal(err)
			}
			if err := ConfigureExperiments(s, config); err != nil {
				t.Fatal(err)
			}
			if s.StationQueueSpacing() != sim.StationQueueCompactV1 || !s.NeedsCompactQueueState() {
				t.Fatal("validated compact policy did not reach the controller")
			}
			if _, err := s.SubmitTrip("harbor", "garden"); err != nil {
				t.Fatal(err)
			}
			original := make(map[string]sim.Lane, len(config.Network.Lanes))
			for _, lane := range config.Network.Lanes {
				original[lane.ID] = lane
			}
			lanes := 0
			for _, vehicle := range s.Snapshot().Vehicles {
				for _, lane := range vehicle.Route {
					authored, ok := original[lane.ID]
					if !ok || !reflect.DeepEqual(lane, authored) {
						t.Fatal("queue policy changed authored lane speeds or geometry")
					}
					lanes++
				}
			}
			if lanes == 0 {
				t.Fatal("lane preservation check exercised no passenger route")
			}
			config.StationQueueSpacing, config.StationBuffers, config.PlatoonLimit = sim.StationQueueOrdinary, false, 0
			if err := ConfigurePlatoons(s, config); err != nil {
				t.Fatal(err)
			}
			if err := ConfigureExperiments(s, config); err != nil {
				t.Fatal(err)
			}
			if s.StationQueueSpacing() != sim.StationQueueOrdinary || s.NeedsCompactQueueState() {
				t.Fatal("unused compact policy survived disablement")
			}
		}
	}
}

func TestStationQueueOmissionAndRejectedConfiguration(t *testing.T) {
	t.Parallel()
	config := Default()
	data, err := jsonv2.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("stationQueueSpacing")) || EffectiveStationQueueSpacing(config) != sim.StationQueueOrdinary {
		t.Fatal("default queue policy changed legacy project bytes")
	}
	s, err := sim.NewFleet(config.Network, config.Fleet)
	if err != nil {
		t.Fatal(err)
	}
	before := s.ExportState()
	config.StationBuffers, config.StationQueueSpacing = true, sim.StationQueueCompactV1
	if err := ConfigureExperiments(s, config); err == nil || !reflect.DeepEqual(before, s.ExportState()) || s.NeedsBufferState() {
		t.Fatal("invalid zero-limit configuration mutated the controller", err)
	}
}

func TestStationQueueExpressSpacing(t *testing.T) {
	t.Parallel()
	for _, mode := range []sim.StationQueueSpacing{sim.StationQueueOrdinary, sim.StationQueueCompactV1} {
		config := expressProject(t)
		config.StationQueueSpacing, config.StationBuffers, config.PlatoonLimit = mode, true, 2
		if err := Validate(config); err != nil {
			t.Fatalf("Express refused %s queue spacing: %v", mode, err)
		}
	}
}
