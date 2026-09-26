package sim

import "testing"

func TestSetPositioning(t *testing.T) {
	t.Parallel()
	s := newExample(t)
	advance(s, 100)
	if s.positioning != PositioningOff || s.nextRedistributionTick != 0 {
		t.Fatalf("new simulation has mode %d and next check %d", s.positioning, s.nextRedistributionTick)
	}
	for _, mode := range []Positioning{PositioningOff - 1, PositioningRedistribution + 1} {
		if err := s.SetPositioning(mode); err == nil || s.positioning != PositioningOff || s.nextRedistributionTick != 0 {
			t.Fatalf("SetPositioning(%d) = %v, mode %d, next check %d", mode, err, s.positioning, s.nextRedistributionTick)
		}
	}
	if err := s.SetPositioning(PositioningOff); err != nil || s.nextRedistributionTick != 0 {
		t.Fatalf("SetPositioning(off) = %v, next check %d", err, s.nextRedistributionTick)
	}
	if err := s.SetPositioning(PositioningRedistribution); err != nil || s.positioning != PositioningRedistribution || s.nextRedistributionTick != s.tick {
		t.Fatalf("SetPositioning(redistribution) = %v, mode %d, next check %d at tick %d", err, s.positioning, s.nextRedistributionTick, s.tick)
	}
	s.nextRedistributionTick = s.tick + 50
	if err := s.SetPositioning(PositioningRedistribution); err != nil || s.nextRedistributionTick != s.tick+50 {
		t.Fatalf("SetPositioning moved a later check to %d at tick %d: %v", s.nextRedistributionTick, s.tick, err)
	}
	s.SetRedistribution(false)
	if s.positioning != PositioningOff {
		t.Fatalf("SetRedistribution(false) gives mode %d", s.positioning)
	}
	s.SetRedistribution(true)
	if s.positioning != PositioningRedistribution {
		t.Fatalf("SetRedistribution(true) gives mode %d", s.positioning)
	}
	s.Reset()
	if s.positioning != PositioningOff || s.nextRedistributionTick != 0 {
		t.Fatalf("Reset gives mode %d and next check %d", s.positioning, s.nextRedistributionTick)
	}
}
