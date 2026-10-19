package sim

import "testing"

// TestPassAvailableAfterResets checks the availableAfter memo of a dispatch
// pass in the busy hold scenario. In a pass, the memo keeps each estimate
// until a reset. After a change to the pods and a reset, each estimate is
// equal to a new estimate. After the end of the pass, each estimate is a
// new estimate, also with no reset.
func TestPassAvailableAfterResets(t *testing.T) {
	t.Parallel()
	skipLong(t)
	changed, kept := 0, 0
	holdScenario{}.run(t, func(s *Simulation) {
		c := s.Clone()
		pass := new(dispatchPass)
		c.pass, pass.active = pass, true
		pass.begin(c.waiting)
		before := make([]finishMemo, len(c.vehicles))
		for i := range c.vehicles {
			before[i].node, before[i].remaining, before[i].ok = c.passAvailableAfter(i)
		}
		// A tick changes the pods. With no reset, the pass keeps the
		// estimates.
		c.pass = nil
		c.Step()
		c.pass = pass
		for i := range c.vehicles {
			node, remaining, ok := c.passAvailableAfter(i)
			if node != before[i].node || remaining != before[i].remaining || ok != before[i].ok {
				t.Fatalf("tick %d, pod %s: the pass did not keep the estimate", c.tick, c.vehicles[i].Pod.ID)
			}
		}
		pass.reset()
		for i := range c.vehicles {
			node, remaining, ok := c.passAvailableAfter(i)
			wantNode, wantRemaining, wantOK := c.availableAfter(&c.vehicles[i])
			if node != wantNode || remaining != wantRemaining || ok != wantOK {
				t.Fatalf("tick %d, pod %s: estimate %q %v %v after a reset, want %q %v %v",
					c.tick, c.vehicles[i].Pod.ID, node, remaining, ok, wantNode, wantRemaining, wantOK)
			}
			if ok && before[i].ok && remaining != before[i].remaining {
				changed++
			}
		}
		// After the end of the pass, the memo is not read.
		pass.end()
		c.pass = nil
		c.Step()
		c.pass = pass
		for i := range c.vehicles {
			node, remaining, ok := c.passAvailableAfter(i)
			wantNode, wantRemaining, wantOK := c.availableAfter(&c.vehicles[i])
			if node != wantNode || remaining != wantRemaining || ok != wantOK {
				t.Fatalf("tick %d, pod %s: estimate %q %v %v after the pass, want %q %v %v",
					c.tick, c.vehicles[i].Pod.ID, node, remaining, ok, wantNode, wantRemaining, wantOK)
			}
			if ok {
				kept++
			}
		}
	})
	if changed == 0 || kept == 0 {
		t.Fatalf("%d estimates changed in a tick and %d estimates were made after a pass, want both", changed, kept)
	}
}
