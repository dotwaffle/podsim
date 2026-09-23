package project_test

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// TestValidateAcceptsLondon checks that the largest preset passes Validate,
// including the limit on its encoded size. The test is in an external
// package, because the scenarios package imports this package.
func TestValidateAcceptsLondon(t *testing.T) {
	t.Parallel()
	if err := project.Validate(scenarios.London()); err != nil {
		t.Fatal(err)
	}
}
