package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
	"github.com/dotwaffle/podsim/internal/sim"
	"github.com/dotwaffle/podsim/internal/statestore"
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

// BenchmarkLoadStartProject measures the load of the london-full:dedicated
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

// TestRestoredProjectPreparesOneTime pins that the load of a project file
// and the start of its session prepare the network one time when the store
// has a saved state of the same project: the restore takes the checks of
// the load. A saved traffic demo with other demand settings restores and
// is then rejected, and the empty session takes the same checks. It does
// not run in parallel, because the count is for the process.
func TestRestoredProjectPreparesOneTime(t *testing.T) {
	config := project.Default()
	changed := config
	changed.Demand.PerMinute++
	for _, test := range []struct {
		name        string
		demo        bool
		file        project.Config
		wantRestore session.RestoreInfo
	}{
		{name: "restored", file: config, wantRestore: session.RestoreInfo{Tier: "physical"}},
		{
			name: "rejected after the restore", demo: true, file: changed,
			wantRestore: session.RestoreInfo{Tier: "empty", Reason: "project_changed"},
		},
	} {
		saved := savedState(t, config, test.demo)
		path := filepath.Join(t.TempDir(), "project.json")
		if err := saveProject(path, test.file); err != nil {
			t.Fatal(err)
		}
		before := sim.PreparationsForTest()
		shared, closeStore := loadStartSaved(t, path, saved)
		if got := sim.PreparationsForTest() - before; got != 1 {
			t.Errorf("%s: the load and the start prepare the network %d times, want 1", test.name, got)
		}
		if got := shared.State().Restore; got != test.wantRestore {
			t.Errorf("%s: restore %+v, want %+v", test.name, got, test.wantRestore)
		}
		shared.Close()
		if err := closeStore(); err != nil {
			t.Fatal(err)
		}
	}
}

// savedState returns the final saved state of a new session of config.
// With demo, the traffic demo runs in the saved state.
func savedState(tb testing.TB, config project.Config, demo bool) []byte {
	tb.Helper()
	directory := tb.TempDir()
	checked, err := session.CheckProject(config)
	if err != nil {
		tb.Fatal(err)
	}
	discard := slog.New(slog.DiscardHandler)
	shared, closeStore, err := openSession(tb.Context(), openInput{
		project: &checked, stateURL: fileURL(directory), logger: discard, options: []session.Option{session.WithLogger(discard)},
	})
	if err != nil {
		tb.Fatal(err)
	}
	if demo {
		reply := shared.Apply(session.Command{Client: "demo", Sequence: 1, Epoch: shared.State().Epoch, Action: "demo"})
		if reply.Error != "" || !shared.State().Simulation.Demo {
			tb.Fatalf("the traffic demo does not run: %s", reply.Error)
		}
	}
	shared.Close()
	if err = shared.SaveState(tb.Context(), session.SaveFinal); err != nil {
		tb.Fatal(err)
	}
	if err = closeStore(); err != nil {
		tb.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, statestore.StateKey))
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// loadStartSaved loads the project file at path and starts its session
// from a new store that holds saved. Each start then reads the same saved
// state, with no earlier restore of it.
func loadStartSaved(tb testing.TB, path string, saved []byte) (*session.Session, func() error) {
	tb.Helper()
	directory := tb.TempDir()
	if err := os.WriteFile(filepath.Join(directory, statestore.StateKey), saved, 0o600); err != nil {
		tb.Fatal(err)
	}
	loaded, err := loadProject(path)
	if err != nil {
		tb.Fatal(err)
	}
	discard := slog.New(slog.DiscardHandler)
	shared, closeStore, err := openSession(tb.Context(), openInput{
		project: &loaded, stateURL: fileURL(directory), logger: discard, options: []session.Option{session.WithLogger(discard)},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return shared, closeStore
}

// BenchmarkLoadRestoreProject measures the load of the london-full:dedicated
// project file and the start of its session from a store with a saved
// state of the same project, which restores.
func BenchmarkLoadRestoreProject(b *testing.B) {
	config := scenarios.LondonFullExpress()
	path := filepath.Join(b.TempDir(), "project.json")
	if err := saveProject(path, config); err != nil {
		b.Fatal(err)
	}
	saved := savedState(b, config, false)
	b.ReportAllocs()
	for b.Loop() {
		shared, closeStore := loadStartSaved(b, path, saved)
		if got := shared.State().Restore; got != (session.RestoreInfo{Tier: "physical"}) {
			b.Fatalf("restore %+v, want physical", got)
		}
		shared.Close()
		if err := closeStore(); err != nil {
			b.Fatal(err)
		}
	}
}
