package session

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestCapturedProjectEncodingCache(t *testing.T) {
	t.Parallel()
	file := newTestStateFile(t)
	var encoder stateEncoder
	check := func() {
		t.Helper()
		cached, err := encoder.encodeCaptured(file)
		if err != nil {
			t.Fatal(err)
		}
		// Each encoder owns a different buffer, so this also checks that
		// reusing the project leaves the complete gzip encoding unchanged.
		fresh, err := new(stateEncoder).encode(file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(cached, fresh) {
			t.Fatal("cached project changed saved bytes")
		}
	}
	check()
	check()
	file.Revision++
	file.Simulation.Tick++
	check()
	file.Project.Name += " edited"
	file.ProjectRevision++
	check()
	file.Project.Demand.PerMinute++
	file.ProjectRevision++
	check()
	file.Epoch += "new"
	file.Project.Name += " new epoch"
	check()
	// Direct encoding does not promise an immutable captured project. It
	// must neither reuse nor leave cached bytes under the capture's key.
	mutable := file
	mutable.Project.Name += " direct edit"
	direct, err := encoder.encode(mutable)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := new(stateEncoder).encode(mutable)
	if err != nil || !bytes.Equal(direct, fresh) {
		t.Fatalf("direct encoding reused an unrelated capture: %v", err)
	}
	check()
}

func TestCapturedProjectEncodingFailureInvalidatesCache(t *testing.T) {
	t.Parallel()
	file := newTestStateFile(t)
	var encoder stateEncoder
	if _, err := encoder.encodeCaptured(file); err != nil {
		t.Fatal(err)
	}
	invalid := file
	invalid.Project = project.Clone(file.Project)
	invalid.ProjectRevision++
	invalid.Project.Network.Lanes = nil
	// This still encodes: the saver checks wire shape and size, not project
	// semantics. A later failed encoding must not retain this cache key.
	if _, err := encoder.encodeCaptured(invalid); err != nil {
		t.Fatal(err)
	}
	invalid.ProjectRevision++
	invalid.Project.Network.Nodes[0].Position.X = math.NaN()
	if _, err := encoder.encodeCaptured(invalid); err == nil {
		t.Fatal("accepted nonfinite project coordinate")
	}
	if encoder.projectCached {
		t.Fatal("failed encoding retained a reusable project")
	}
	recovered, err := encoder.encodeCaptured(file)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := new(stateEncoder).encode(file)
	if err != nil || !bytes.Equal(recovered, fresh) {
		t.Fatalf("failed encoding poisoned recovery: %v", err)
	}
}

func TestSaveStateProjectCacheInvalidation(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	shared := startFromStore(t, StoreInput{Store: store})
	client := newTestClient(shared, "cache")
	client.mustApply(t, Command{Action: "pause", Paused: true})
	checkpoint := client.mustApply(t, Command{Action: "checkpoint"}).Checkpoint
	check := func() {
		t.Helper()
		if err := shared.SaveState(t.Context(), SavePeriodic); err != nil {
			t.Fatal(err)
		}
		got := store.lastWrite(t)
		want := shared.Project()
		if !shared.persist.encoder.projectCached || shared.persist.encoder.projectKey != (projectEncodingKey{epoch: shared.epoch, revision: want.Revision}) {
			t.Fatal("session saver did not retain its immutable project encoding")
		}
		if got.ProjectRevision != want.Revision || !reflect.DeepEqual(got.Project, want.Project) {
			t.Fatal("saved capture reused a stale project")
		}
	}
	check()
	changeTestDemand(t, client)
	check()
	config := shared.Project().Project
	config.Name += " edited"
	client.mustApply(t, Command{Action: "project", ProjectRevision: shared.Project().Revision, Project: &config})
	check()
	client.mustApply(t, Command{Action: "rewind", Checkpoint: checkpoint})
	check()
}
