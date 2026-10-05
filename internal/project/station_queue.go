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
	// Express without the coupling marker refuses a queue spacing selection.
	// The rule came from saved state 7 and stream hello 4, which did not
	// carry the selection. Saved state 9 and hello 6 replaced them, but the
	// rule stays until a contract change removes it.
	if config.StationQueueSpacing != "" && config.OrderContract == sim.ExpressOrderContract && !HasCouplingContract(config) {
		return errors.New("station queue spacing with express-v1 requires couplingContract compact-pair-v1")
	}
	if mode == sim.StationQueueCompactV1 && (!config.StationBuffers || config.PlatoonLimit < sim.MinPlatoonLimit || config.PlatoonLimit > sim.MaxPlatoonLimit) {
		return errors.New("compact station queues require station buffers and a platoon limit from 2 to 4")
	}
	return nil
}
