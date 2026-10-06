package project

import (
	"errors"
	"fmt"

	"github.com/dotwaffle/podsim/internal/sim"
)

// EffectiveStationQueueSpacing returns ordinary spacing when the setting is omitted.
func EffectiveStationQueueSpacing(config Config) sim.StationQueueSpacing {
	if config.StationQueueSpacing == "" {
		return sim.StationQueueOrdinary
	}
	return config.StationQueueSpacing
}

// ValidStationQueueSpacing reports whether mode is a known queue spacing.
// The empty mode is not valid. EffectiveStationQueueSpacing replaces it
// before a check.
func ValidStationQueueSpacing(mode sim.StationQueueSpacing) bool {
	return mode == sim.StationQueueOrdinary || mode == sim.StationQueueCompactV1
}

// CompactStationQueuesAllowed reports whether a project with these
// settings can use compact station queues. Compact queues need station
// buffers and platoons.
func CompactStationQueuesAllowed(stationBuffers bool, platoonLimit int) bool {
	return stationBuffers && platoonLimit >= sim.MinPlatoonLimit && platoonLimit <= sim.MaxPlatoonLimit
}

func validateStationQueueSpacing(config Config) error {
	mode := EffectiveStationQueueSpacing(config)
	if !ValidStationQueueSpacing(mode) {
		return errors.New("station queue spacing must be ordinary or compact-v1")
	}
	if mode == sim.StationQueueCompactV1 && !CompactStationQueuesAllowed(bool(config.StationBuffers), config.PlatoonLimit) {
		return fmt.Errorf("compact station queues require station buffers and a platoon limit from %d to %d", sim.MinPlatoonLimit, sim.MaxPlatoonLimit)
	}
	return nil
}
