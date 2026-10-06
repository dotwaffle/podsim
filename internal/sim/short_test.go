package sim

import "testing"

// skipLong skips a test that takes a second or more without the race
// detector when the tests run with -short. The race tasks and the plain
// test run do not use -short, so they run it.
func skipLong(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("a long test runs without -short")
	}
}
