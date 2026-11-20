package sim

import "testing"

// TestPreparationTakesValidationRoutes pins that a preparation gives the
// station lane roles from the berth routes of its validation and does not
// run the role inference again. It does not run in parallel, because other
// tests run the inference.
func TestPreparationTakesValidationRoutes(t *testing.T) {
	network := withoutRoles(Example())
	before := inferences.Load()
	inferred := network.clone()
	inferStationLaneRoles(&inferred)
	if runs := inferences.Load() - before; runs != 1 {
		t.Fatalf("one inference counts %d runs", runs)
	}
	before = inferences.Load()
	if _, err := PrepareNetwork(network); err != nil {
		t.Fatal(err)
	}
	if runs := inferences.Load() - before; runs != 0 {
		t.Fatalf("the preparation runs the role inference: %d runs, want 0", runs)
	}
}
