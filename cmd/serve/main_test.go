package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestProjectFileRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "scenario.json")
	want := project.Default()
	want.Name = "Saved scenario"
	want.Demand = project.DemandConfig{Enabled: true, PerMinute: 20, Pattern: "destination", Seed: 4, Destination: "garden"}
	if err := saveProject(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved project changed\n got: %#v\nwant: %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestLoadProjectRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{"malformed", "{"},
		{"unknown field", `{"version":1,"unknown":true}`},
		{"trailing", `{}` + `{}`},
		{"oversize", strings.Repeat(" ", (2<<20)+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "scenario.json")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadProject(path); err == nil {
				t.Fatal("accepted invalid project file")
			}
		})
	}
}
