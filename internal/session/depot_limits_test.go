package session

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestDepotArrayLimits checks the bound of each depot array in the save,
// topology and command tables: the bound passes the scan, and one more
// element fails it.
func TestDepotArrayLimits(t *testing.T) {
	t.Parallel()
	list := func(count int) string { return "[" + strings.TrimSuffix(strings.Repeat("0,", count), ",") + "]" }
	tables := []struct {
		name   string
		limits jsonLimits
		prefix string
	}{
		{name: "save", limits: savedLimits(contractMarkers{}), prefix: `{"project":%s}`},
		{name: "Express save", limits: savedLimits(contractMarkers{order: sim.ExpressOrderContract}), prefix: `{"project":%s}`},
		{name: "topology", limits: topologyJSONLimits, prefix: "%s"},
		{name: "command", limits: commandJSONLimits, prefix: `{"project":%s}`},
	}
	arrays := []struct {
		document string
		limit    int
	}{
		{document: `{"network":{"depots":%s}}`, limit: project.MaxStations},
		{document: `{"network":{"depots":[{"lanes":%s}]}}`, limit: sim.MaxDepotLanes},
		{document: `{"network":{"depots":[{"lanes":[{"berthIDs":%s}]}]}}`, limit: project.MaxBerths},
	}
	for _, table := range tables {
		for _, array := range arrays {
			for _, count := range []int{array.limit, array.limit + 1} {
				document := fmt.Sprintf(table.prefix, fmt.Sprintf(array.document, list(count)))
				err := prescanJSON([]byte(document), table.limits)
				if (err == nil) != (count == array.limit) || err != nil && !errors.Is(err, errJSONArrayTooLong) {
					t.Errorf("%s %s with %d elements: %v", table.name, array.document, count, err)
				}
			}
		}
	}
}
