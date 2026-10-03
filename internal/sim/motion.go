package sim

import "slices"

// MotionSample records actual path travel and speed change during one tick.
// Speeds use meters per second. DistanceMeters uses the simulation path length.
type MotionSample struct {
	ID             string
	Class          VehicleClass
	DistanceMeters float64
	StartSpeed     float64
	EndSpeed       float64
}

// MotionFrame holds movement samples from the latest successful tick.
// An empty frame still represents elapsed simulation time.
type MotionFrame struct {
	Tick    int64
	Samples []MotionSample
}

type motionRecorder struct {
	frame   MotionFrame
	pending []MotionSample
}

// SetMotionRecording enables an independent measurement window at the current tick.
// Repeated enable preserves the window. Disable drops all recording storage.
func (s *Simulation) SetMotionRecording(enabled bool) {
	if !enabled {
		s.motion = nil
	} else if s.motion == nil {
		s.motion = &motionRecorder{frame: MotionFrame{Tick: s.tick}}
	}
}

// MotionFrame returns an owned copy of the latest frame, or false when disabled.
// Enable and reset provide an empty baseline frame at the current tick.
func (s *Simulation) MotionFrame() (MotionFrame, bool) {
	if s.motion == nil {
		return MotionFrame{}, false
	}
	frame := s.motion.frame
	frame.Samples = slices.Clone(frame.Samples)
	return frame, true
}

func (s *Simulation) beginMotionFrame() {
	if s.motion != nil {
		s.motion.pending = s.motion.pending[:0]
	}
}

func (s *Simulation) recordMotion(sample MotionSample) {
	if s.motion == nil || sample.DistanceMeters == 0 && sample.StartSpeed == sample.EndSpeed {
		return
	}
	profile, _ := LookupVehicleClass(sample.Class)
	sample.Class = profile.Class
	s.motion.pending = append(s.motion.pending, sample)
}

func (s *Simulation) publishMotionFrame() {
	if s.motion != nil {
		s.motion.frame, s.motion.pending = MotionFrame{Tick: s.tick, Samples: s.motion.pending}, s.motion.frame.Samples
	}
}

func (s *Simulation) cloneMotion(c *Simulation) {
	if s.motion != nil {
		c.motion = &motionRecorder{frame: s.motion.frame, pending: slices.Clone(s.motion.pending)}
		c.motion.frame.Samples = slices.Clone(s.motion.frame.Samples)
	}
}
