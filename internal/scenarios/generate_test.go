package scenarios

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// generateChildEnv makes TestGenerateLeavesNoCacheChild run. The test needs a
// process in which no other test has filled the caches of this package.
const generateChildEnv = "PODSIM_GENERATE_CACHE_CHILD"

// generateRetainedBytes is the largest growth of the live heap after a
// generation. The two presets hold more than 100 MB of objects, so a cache
// of either preset, or of its demand, is far larger than this limit.
const generateRetainedBytes = 4 << 20

func canonicalProject(t *testing.T, config project.Config) []byte {
	t.Helper()
	data, err := json.Marshal(config, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestGeneratedPresetsMatchCachedPresets checks that the path without a cache
// and the path with a cache give the same project.
func TestGeneratedPresetsMatchCachedPresets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		generate func() (project.Config, error)
		cached   func() project.Config
	}{
		{"london-central", GenerateLondonCentral, LondonCentral},
		{"london-full", GenerateLondonFull, LondonFull},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			generated, err := test.generate()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(canonicalProject(t, generated), canonicalProject(t, test.cached())) {
				t.Fatal("the generated project differs from the cached project")
			}
		})
	}
}

// TestGenerateLeavesNoCache runs the check in a new process, because other
// tests of this package fill the caches.
func TestGenerateLeavesNoCache(t *testing.T) {
	t.Parallel()
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestGenerateLeavesNoCacheChild$", "-test.v") // #nosec G204 -- The test runs its own binary.
	command.Env = append(os.Environ(), generateChildEnv+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("the child failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("--- PASS: TestGenerateLeavesNoCacheChild")) {
		t.Fatalf("the child did not run:\n%s", output)
	}
}

// generatedName calls generate and keeps only the project name.
func generatedName(t *testing.T, generate func() (project.Config, error)) string {
	t.Helper()
	config, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	return config.Name
}

func TestGenerateLeavesNoCacheChild(t *testing.T) {
	if os.Getenv(generateChildEnv) == "" {
		t.Skip("TestGenerateLeavesNoCache runs this test in a new process")
	}
	for _, generate := range []func() (project.Config, error){GenerateLondonCentral, GenerateLondonFull} {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		name := generatedName(t, generate)
		runtime.GC()
		runtime.ReadMemStats(&after)
		t.Logf("%s: live heap %d before, %d after", name, before.HeapAlloc, after.HeapAlloc)
		if after.HeapAlloc > before.HeapAlloc+generateRetainedBytes {
			t.Errorf("%s: the live heap grew from %d to %d bytes", name, before.HeapAlloc, after.HeapAlloc)
		}
	}
	if londonPreset.Name != "" || londonFullPreset.Name != "" {
		t.Error("a generation filled a preset cache")
	}
	if len(londonDemand) != 0 || len(londonProfile.Flows) != 0 || len(londonFullDemand) != 0 || len(londonFullProfile.Flows) != 0 {
		t.Error("a generation filled a demand cache")
	}
}
