package project

import (
	"errors"

	"github.com/dotwaffle/podsim/internal/sim"
)

// EffectiveStationQueueSpacing returns ordinary spacing when the setting is omitted.
func EffectiveStationQueueSpacing(config Config) sim.StationQueueSpacing {
	if config.StationQueueSpacing == "" {
		return sim.StationQueueOrdinary
	}
	return config.StationQueueSpacing
}

func validStationQueueSpacing(mode sim.StationQueueSpacing) bool {
	return mode == sim.StationQueueOrdinary || mode == sim.StationQueueCompactV1
}

func validateStationQueueSpacing(config Config) error {
	mode := EffectiveStationQueueSpacing(config)
	if !validStationQueueSpacing(mode) {
		return errors.New("station queue spacing must be ordinary or compact-v1")
	}
	if mode == sim.StationQueueCompactV1 && (!config.StationBuffers || config.PlatoonLimit < sim.MinPlatoonLimit || config.PlatoonLimit > sim.MaxPlatoonLimit) {
		return errors.New("compact station queues require station buffers and a platoon limit from 2 to 4")
	}
	return nil
}
