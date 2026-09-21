package sim

// WaitStats measures request-to-boarding wait since reset, including elapsed pending waits.
type WaitStats struct {
	AverageSeconds float64
	MaxSeconds     float64
}

func (s *Simulation) waitStats() WaitStats {
	total, longest := s.totalWaitTicks, s.maxWaitTicks
	for _, trip := range s.waiting {
		elapsed := s.tick - trip.request.RequestedTick
		total += elapsed
		longest = max(longest, elapsed)
	}
	count := s.boarded + len(s.waiting)
	if count == 0 {
		return WaitStats{}
	}
	return WaitStats{AverageSeconds: float64(total) / float64(count) / TicksPerSecond, MaxSeconds: float64(longest) / TicksPerSecond}
}
