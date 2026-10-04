package sim

import "math"

// The command and published speed differ at the existing ordinary endpoint snap.
type ordinaryMoveResult struct {
	commandedSpeed float64
	distance       float64
	speed          float64
}

// This kernel preserves the ordinary controller's float operations and order.
// Its caller derives the limit from actual grants and the frozen predecessor cap.
func ordinaryMoveStep(blocks *blockList, lane int, distance, speed, limit float64) ordinaryMoveResult {
	available := math.Max(0, limit-distance)
	dt := 1.0 / TicksPerSecond
	safe := math.Sqrt(acceleration*acceleration*dt*dt+2*acceleration*available) - acceleration*dt
	command := math.Min(speed+acceleration*dt, math.Min(blocks.route[lane].SpeedLimit, math.Max(0, safe)))
	command = blocks.speedBeforeLane(lane, distance, command)
	travel := math.Min(available, command*dt)
	next := ordinaryMoveResult{commandedSpeed: command, distance: distance + travel, speed: command}
	if limit-next.distance < 1e-5 {
		next.distance = limit
		next.speed = 0
	}
	return next
}
