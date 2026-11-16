package scenarios

import (
	jsonv2 "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// sharedTrackLayoutEnv names a directory of the projects that the
// shared-track study of internal/sim writes (see
// internal/sim/shared_track_study_test.go). The test is part of that study,
// for docs/design/shared-track-preset.md, section 3.5.
const sharedTrackLayoutEnv = "PODSIM_SHARED_TRACK_LAYOUT"

// TestSharedTrackStudyLayout runs the layout audit of the LondonFull
// generator on each project of the directory, and logs each conflict.
func TestSharedTrackStudyLayout(t *testing.T) {
	dir := os.Getenv(sharedTrackLayoutEnv)
	if dir == "" {
		t.Skip("set " + sharedTrackLayoutEnv + " to the output directory of the shared-track study")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(paths)
	source, err := decodeLondonFullSource()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var project struct {
			Network sim.Network `json:"network"`
		}
		if err := jsonv2.Unmarshal(data, &project); err != nil {
			t.Fatal(err)
		}
		conflicts := auditLondonLayout(project.Network, newLondonAuditInput(source))
		for _, conflict := range conflicts {
			t.Logf("%s: soft %v: %s", filepath.Base(path), conflict.soft, conflict.describe(project.Network))
		}
		t.Logf("%s: %d conflicts, %d soft, first hard conflict: %v", filepath.Base(path), len(conflicts), countSoft(conflicts), layoutError(project.Network, conflicts))
	}
}
