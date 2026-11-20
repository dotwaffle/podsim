package main

import (
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestLoadedProjectPreparesOneTime pins that the load of a project file and
// the start of its session prepare the network one time, without a state
// store and with a store that has no saved state: the session takes the
// checks of the load. It does not run in parallel, because the count is
// for the process.
func TestLoadedProjectPreparesOneTime(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "project.json")
	if err := saveProject(path, project.Default()); err != nil {
		t.Fatal(err)
	}
	for _, store := range []bool{false, true} {
		input := openInput{logger: slog.New(slog.DiscardHandler), options: []session.Option{session.WithLogger(slog.New(slog.DiscardHandler))}}
		if store {
			input.stateURL = fileURL(t.TempDir())
		}
		before := sim.PreparationsForTest()
		loaded, err := loadProject(path)
		if err != nil {
			t.Fatal(err)
		}
		input.project = &loaded
		shared, closeStore, err := openSession(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		if got := sim.PreparationsForTest() - before; got != 1 {
			t.Errorf("store %v: the load and the start prepare the network %d times, want 1", store, got)
		}
		shared.Close()
		if err := closeStore(); err != nil {
			t.Fatal(err)
		}
	}
}

// BenchmarkLoadStartProject measures the load of the london-full-express
// project file and the start of its session, without a state store and
// with a store that has no saved state.
func BenchmarkLoadStartProject(b *testing.B) {
	directory := b.TempDir()
	path := filepath.Join(directory, "project.json")
	if err := saveProject(path, scenarios.LondonFullExpress()); err != nil {
		b.Fatal(err)
	}
	for _, store := range []bool{false, true} {
		name := map[bool]string{false: "no-store", true: "new-store"}[store]
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				input := openInput{logger: slog.New(slog.DiscardHandler), options: []session.Option{session.WithLogger(slog.New(slog.DiscardHandler))}}
				if store {
					input.stateURL = fileURL(b.TempDir())
				}
				loaded, err := loadProject(path)
				if err != nil {
					b.Fatal(err)
				}
				input.project = &loaded
				shared, closeStore, err := openSession(b.Context(), input)
				if err != nil {
					b.Fatal(err)
				}
				shared.Close()
				if err := closeStore(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
