package main

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

func TestBrowserFilesUsesSelectedDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("podsim"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := browserFiles(directory)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := fs.ReadFile(files, "index.html")
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "podsim" {
		t.Fatalf("index contents = %q", contents)
	}
}

func TestBrowserFilesRejectsMissingBuild(t *testing.T) {
	t.Parallel()
	if _, err := browserFiles(t.TempDir()); err == nil {
		t.Fatal("accepted a browser directory without index.html")
	}
}

func TestPprofHandlerIsScoped(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		path string
		want int
	}{{path: "/debug/pprof/", want: http.StatusOK}, {path: "/", want: http.StatusNotFound}} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, test.path, http.NoBody)
		response := httptest.NewRecorder()
		pprofHandler().ServeHTTP(response, request)
		if response.Code != test.want {
			t.Fatalf("%s status = %d", test.path, response.Code)
		}
	}
}

func TestRunServersStopsAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	server := &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler(), ReadHeaderTimeout: time.Second}
	if err := runServers(ctx, []namedServer{{name: "test", server: server}}); err != nil {
		t.Fatal(err)
	}
}

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

func TestLondonProjectFileRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "london.json")
	want := scenarios.London()
	if err := saveProject(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("saved London project changed")
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
		{"oversize", strings.Repeat(" ", project.MaxFileBytes+1)},
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
