package remote

import (
	"testing"

	"github.com/dotwaffle/podsim/internal/scenarios"
)

// TestLondonWharfExpressRemoteLoads is TestLondonSharedExpressRemoteLoads
// on both lines of london-full-shared-express-wharf (row HX6).
func TestLondonWharfExpressRemoteLoads(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the wharf Express remote run needs a full-speed simulation")
	}
	runSharedExpressLoads(t, scenarios.GenerateLondonFullSharedExpressWharf,
		map[string]string{"express-parking": "940GZZLUBNK", "express-parking-lnb": "940GZZLUCYF"},
		[][2]string{{"940GZZLUWLO", "940GZZLUBNK"}, {"940GZZLULNB", "940GZZLUCYF"}})
}

// TestLondonWharfExpressRemoteRecovery is
// TestLondonSharedExpressRemoteRecovery on london-full-shared-express-wharf
// (row HX6), with the atomic rejection on each line.
func TestLondonWharfExpressRemoteRecovery(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("the wharf Express frames are large")
	}
	config, err := scenarios.GenerateLondonFullSharedExpressWharf()
	if err != nil {
		t.Fatal(err)
	}
	next, err := scenarios.GenerateLondonFullExpress()
	if err != nil {
		t.Fatal(err)
	}
	checkExpressRemoteRecoveryTo(t, config, &next)
	checkExpressInvalidStateHasNoACK(t, config, "940GZZLUWLO", "940GZZLUBNK")
	checkExpressInvalidStateHasNoACK(t, config, "940GZZLULNB", "940GZZLUCYF")
}
