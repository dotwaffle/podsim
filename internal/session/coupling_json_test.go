package session

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestScanCouplingJSONRefusalOrder pins which refusal comes first when the
// JSON has two faults, and the refusal of each single fault.
func TestScanCouplingJSONRefusalOrder(t *testing.T) {
	t.Parallel()
	const group = `{"id":"g","members":["a","b"],"formationTick":0,"corridorID":"c","assemblySiteID":"s","splitSiteID":"s","phase":"p","dwellTicks":0,"progress":{"leg":0,"drainFirstMember":0}}`
	const markers = `"couplingContract":"compact-pair-v1","project":{"couplingContract":"compact-pair-v1"}`
	deep := `{"x":` + strings.Repeat("[", 64) + strings.Repeat("]", 64) + `}`
	cases := []struct {
		name     string
		data     string
		topology bool
		want     error
	}{
		{"empty", ``, false, errors.New("incomplete coupling JSON")},
		{"array root", `[]`, true, errors.New("incomplete coupling JSON")},
		{"second root", `{} {}`, false, errors.New("multiple coupling JSON values")},
		{"too deep", deep, false, errJSONTooDeep},
		{"saved markers", `{"couplingEnabled":true}`, false, errors.New("saved coupling contract marker is missing")},
		{"markers before half fleet", `{` + markers + `,"couplingGroups":[` + group + `]}`, false, errors.New("saved coupling contract marker is missing")},
		{"half fleet", `{` + markers + `,"simulation":{"couplingContract":"compact-pair-v1","pods":[{}]},"couplingGroups":[` + group + `]}`, false, errors.New("saved coupling groups exceed half the fleet")},
		{"topology marker", `{"couplingEnabled":true}`, true, errors.New("topology coupling contract marker is missing")},
		{"topology skips half fleet", `{"couplingContract":"compact-pair-v1","couplingGroups":[` + group + `]}`, true, nil},
		{"topology skips nested members", `{"project":{"couplingEnabled":1}}`, true, nil},
		{"saved checks nested members", `{"project":{"couplingEnabled":1}}`, false, errors.New("coupling enabled must be Boolean")},
		{"duplicate before value", `{"couplingEnabled":true,"couplingEnabled":1}`, true, errors.New("duplicate coupling member")},
		{"contract value", `{"couplingContract":"other"}`, true, sim.ErrUnknownCouplingContract},
		{"registry shape", `{"couplingSites":{}}`, true, errors.New("coupling registry must be an array")},
		{"pods", `{"simulation":{"pods":[` + strings.Repeat(`{},`, 300) + `{}]}}`, false, errJSONArrayTooLong},
	}
	sentinels := []error{errJSONTooDeep, errJSONArrayTooLong, sim.ErrUnknownCouplingContract}
	for _, c := range cases {
		_, err := scanCouplingJSON([]byte(c.data), c.topology)
		if c.want == nil {
			if err != nil {
				t.Errorf("%s: got %v, want nil", c.name, err)
			}
			continue
		}
		if err == nil || err.Error() != c.want.Error() {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
		if slices.Contains(sentinels, c.want) && !errors.Is(err, c.want) {
			t.Errorf("%s: %v is not the sentinel %v", c.name, err, c.want)
		}
	}
	// The scan keeps its count when the pod limit refuses the input.
	if scan, _ := scanCouplingJSON([]byte(cases[len(cases)-1].data), false); scan.pods != 301 {
		t.Errorf("pods after the refusal = %d, want 301", scan.pods)
	}
	scan, err := scanCouplingJSON([]byte(`{"orderContract":"express-v1","simulation":{"pods":[{},{}]}}`), true)
	if err != nil || !scan.packed || scan.pods != 2 || scan.recognized {
		t.Errorf("got %+v, %v", scan, err)
	}
}
