package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestLoadProjectRefusesRemovedMembers pins the refusal of a native project
// file whose network has a removed member, with the values true, false,
// and null, and with both members in each order. The unknown member comes
// before the version check, so a file with version 2 gets the same text.
func TestLoadProjectRefusesRemovedMembers(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := filepath.Join(directory, "project.json")
	if err := saveProject(path, project.Default()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- The test writes this file.
	if err != nil {
		t.Fatal(err)
	}
	type test struct{ name, inserted, first string }
	var tests []test
	for _, member := range []string{"lateralMerges", "shapeClearance"} {
		for _, value := range []string{"true", "false", "null"} {
			tests = append(tests, test{member + " " + value, `"` + member + `":` + value + `,`, member})
		}
	}
	tests = append(tests,
		test{"both", `"lateralMerges":true,"shapeClearance":true,`, "lateralMerges"},
		test{"both in the other order", `"shapeClearance":null,"lateralMerges":false,`, "shapeClearance"},
		test{"version 2", `"shapeClearance":true,`, "shapeClearance"})
	const network, version = `"network":{`, `"version":1`
	if bytes.Count(data, []byte(network)) != 1 || bytes.Count(data, []byte(version)) != 1 {
		t.Fatal("the project file does not have one network and one version")
	}
	for index, test := range tests {
		changed := bytes.Replace(data, []byte(network), []byte(network+test.inserted), 1)
		if test.name == "version 2" {
			changed = bytes.Replace(changed, []byte(version), []byte(`"version":2`), 1)
		}
		file := filepath.Join(directory, test.name+".json")
		if err := os.WriteFile(file, changed, 0o600); err != nil {
			t.Fatal(err)
		}
		want := "read project " + file + `: json: unknown field "` + test.first + `"`
		if _, err := loadProject(file); err == nil || err.Error() != want {
			t.Errorf("case %d, %s: error %v, want %s", index, test.name, err, want)
		}
	}
	if _, err := loadProject(path); err != nil {
		t.Fatalf("control: %v", err)
	}
	// The control of the version case: without the member, the version
	// check refuses the file.
	file := filepath.Join(directory, "version 2 only.json")
	if err := os.WriteFile(file, bytes.Replace(data, []byte(version), []byte(`"version":2`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadProject(file); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("version control: %v", err)
	}
}
