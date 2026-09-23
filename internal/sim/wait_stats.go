package sim

// WaitStats measures request-to-boarding wait since reset, including elapsed pending waits.
type WaitStats struct {
	AverageSeconds float64 `json:"AverageSeconds"`
	MaxSeconds     float64 `json:"MaxSeconds"`
}

func (s *Simulation) waitStats() WaitStats {
	total, longest, pending := s.totalWaitTicks, s.maxWaitTicks, 0
	for _, trip := range s.waiting {
		// The totals already hold the wait of a requeued trip.
		if trip.parties > 0 {
			continue
		}
		elapsed := s.tick - trip.request.RequestedTick
		total += elapsed
		longest = max(longest, elapsed)
		pending++
	}
	count := s.boarded + pending
	if count == 0 {
		return WaitStats{}
	}
	return WaitStats{AverageSeconds: float64(total) / float64(count) / TicksPerSecond, MaxSeconds: float64(longest) / TicksPerSecond}
}
