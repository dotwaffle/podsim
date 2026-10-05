package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/parkride"
)

func cliIdentity() parkride.Implementation {
	return parkride.Implementation{SourceRevision: strings.Repeat("a", 40), ExecutableSHA256: strings.Repeat("b", 64), GoVersion: "go1.27.1", GoExperiment: "jsonv2", GoOS: "linux", GoArch: "amd64"}
}
func testIdentify() (parkride.Implementation, error) { return cliIdentity(), nil }
func TestCheckpointCLIResumeAndControls(t *testing.T) {
	t.Parallel()
	args := commandInputs(t)
	checkpoint := filepath.Join(t.TempDir(), "pause.json")
	var first, log bytes.Buffer
	if err := commandWith(t.Context(), append(args, "-checkpoint-output", checkpoint, "-stop-at", "1"), &first, &log, testIdentify); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0o600 {
		t.Fatal("checkpoint permissions", stat.Mode())
	}
	var restored, ordinary bytes.Buffer
	if err := commandWith(t.Context(), []string{"-checkpoint-input", checkpoint, "-replay-timeout", "30s"}, &restored, &log, testIdentify); err != nil {
		t.Fatal(err)
	}
	if err := command(args, &ordinary); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Bytes(), ordinary.Bytes()) {
		t.Fatal("resumed report differs from ordinary report")
	}
	if !strings.Contains(log.String(), "Replay accepted") || !strings.Contains(log.String(), "Checkpoint published") {
		t.Fatal("missing observability signals")
	}
	for _, flags := range [][]string{
		{"-checkpoint-input", checkpoint}, {"-checkpoint-input", checkpoint, "-replay-timeout", "0s"},
		{"-checkpoint-input", checkpoint, "-replay-timeout", "1s", "-duration", "1s"},
		{"-checkpoint-input", checkpoint, "-replay-timeout", "1s", "-queue-limit", "0"},
		{"-checkpoint-input", checkpoint, "-replay-timeout", "1s", "-project", ""},
		{"-checkpoint-input", checkpoint, "-replay-timeout", "1s", "-plan", ""},
		{"-checkpoint-input", checkpoint, "-replay-timeout", "1s", "-stop-at", "0"},
		{"-checkpoint-input", checkpoint, "-replay-timeout", "1s", "-checkpoint-output", checkpoint},
	} {
		if _, err := parseOptions(flags, io.Discard); err == nil {
			t.Fatal("invalid resume flags accepted", flags)
		}
	}
	newPath := filepath.Join(t.TempDir(), "backwards.json")
	if err := commandWith(t.Context(), []string{"-checkpoint-input", checkpoint, "-replay-timeout", "1s", "-checkpoint-output", newPath, "-stop-at", "0"}, io.Discard, io.Discard, testIdentify); err == nil {
		t.Fatal("backwards stop accepted")
	}
	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("backwards stop published output")
	}
}
func TestCheckpointCLIIdentityAndWatchdog(t *testing.T) {
	t.Parallel()
	args := commandInputs(t)
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := commandWith(t.Context(), append(args, "-checkpoint-output", path, "-stop-at", "60"), io.Discard, io.Discard, testIdentify); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := commandWith(t.Context(), []string{"-checkpoint-input", path, "-replay-timeout", "1ns"}, &output, io.Discard, testIdentify); err == nil || output.Len() != 0 {
		t.Fatal("watchdog accepted replay or emitted report")
	}
	bad := func() (parkride.Implementation, error) { return parkride.Implementation{}, errors.New("dirty build") }
	if err := commandWith(t.Context(), append(args, "-checkpoint-output", filepath.Join(t.TempDir(), "bad.json")), &output, io.Discard, bad); err == nil {
		t.Fatal("dirty build accepted")
	}
	if output.Len() != 0 {
		t.Fatal("dirty build emitted report")
	}
}
func TestCheckpointBuildFacts(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "executable")
	if err := os.WriteFile(path, []byte("binary fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := []debug.BuildSetting{{Key: "vcs.revision", Value: strings.Repeat("a", 40)}, {Key: "vcs.modified", Value: "false"}, {Key: "GOEXPERIMENT", Value: "jsonv2"}}
	identity, err := implementationFromBuild(&debug.BuildInfo{GoVersion: "go1.27.1", Settings: settings}, func() (string, error) { return path, nil })
	if err != nil || identity.SourceRevision != strings.Repeat("a", 40) || len(identity.ExecutableSHA256) != 64 {
		t.Fatal("valid build facts rejected", err)
	}
	for _, modified := range []string{"true", ""} {
		local := append([]debug.BuildSetting(nil), settings...)
		local[1].Value = modified
		if _, err := implementationFromBuild(&debug.BuildInfo{Settings: local}, func() (string, error) { return path, nil }); err == nil {
			t.Fatal("unverified clean build accepted")
		}
	}
	if _, err := implementationFromBuild(&debug.BuildInfo{}, func() (string, error) { return path, nil }); err == nil {
		t.Fatal("unidentified build accepted")
	}
}

type faultFile struct {
	syncedFile
	fault string
}

func (f faultFile) Write(data []byte) (int, error) {
	if f.fault == "write" {
		return 0, errors.New("write injected")
	}
	if f.fault == "short" {
		return len(data) - 1, nil
	}
	return f.syncedFile.Write(data)
}
func (f faultFile) Chmod(mode os.FileMode) error {
	if f.fault == "chmod" {
		return errors.New("chmod injected")
	}
	return f.syncedFile.Chmod(mode)
}
func (f faultFile) Sync() error {
	if f.fault == "sync" {
		return errors.New("sync injected")
	}
	return f.syncedFile.Sync()
}
func (f faultFile) Close() error {
	err := f.syncedFile.Close()
	if f.fault == "close" {
		return errors.New("close injected")
	}
	return err
}
func TestCheckpointAtomicPublicationFailures(t *testing.T) {
	t.Parallel()
	for _, fault := range []string{"create", "chmod", "write", "short", "encode", "cancel", "sync", "close", "rename", "dirsync", "none"} {
		t.Run(fault, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "checkpoint.json")
			old := []byte("old complete checkpoint")
			if err := os.WriteFile(path, old, 0o600); err != nil {
				t.Fatal(err)
			}
			ops := localFileOps()
			create := ops.createTemp
			ops.createTemp = func(dir, pattern string) (syncedFile, error) {
				if fault == "create" {
					return nil, errors.New("create injected")
				}
				file, err := create(dir, pattern)
				return faultFile{file, fault}, err
			}
			if fault == "rename" {
				ops.rename = func(string, string) error { return errors.New("rename injected") }
			}
			if fault == "dirsync" {
				ops.syncDir = func(string) error { return errors.New("dirsync injected") }
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			err := publishCheckpoint(ctx, path, options{}, func(w io.Writer) error {
				if fault == "cancel" {
					cancel()
				}
				_, err := w.Write([]byte("new complete checkpoint"))
				if fault == "encode" {
					return errors.New("encode injected")
				}
				return err
			}, ops)
			if (err == nil) != (fault == "none") {
				t.Fatalf("fault %s error %v", fault, err)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if fault == "none" || fault == "dirsync" {
				if string(data) != "new complete checkpoint" {
					t.Fatal("post-rename target incorrect")
				}
			} else if !bytes.Equal(data, old) {
				t.Fatal("failure replaced old checkpoint")
			}
			if fault == "dirsync" && !strings.Contains(err.Error(), "durability is uncertain") {
				t.Fatal("directory sync ambiguity hidden")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatal("temporary or claim leak", entries)
			}
		})
	}
}
func TestCheckpointAliasesAndExclusiveClaim(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.json")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias.json")
	if err := os.Link(source, alias); err != nil {
		t.Fatal(err)
	}
	if err := publishCheckpoint(t.Context(), alias, options{plan: source}, func(w io.Writer) error { _, err := w.Write([]byte("replace")); return err }, localFileOps()); err == nil {
		t.Fatal("hard alias overwritten")
	}
	symbolic := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(source, symbolic); err != nil {
		t.Fatal(err)
	}
	if err := regularPath(symbolic, true); err == nil {
		t.Fatal("symlink checkpoint accepted")
	}
	output := filepath.Join(dir, "output.json")
	if err := os.WriteFile(output+".car-checkpoint-lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishCheckpoint(t.Context(), output, options{}, func(io.Writer) error { return nil }, localFileOps()); err == nil {
		t.Fatal("concurrent claim accepted")
	}
}
func TestFoundationRawPresence(t *testing.T) {
	t.Parallel()
	for _, member := range []string{"orderContract", "couplingContract", "incidentContract"} {
		for _, value := range []string{"null", "0", `""`, "1"} {
			if err := parkride.CheckFoundationProject([]byte(`{"version":1,"` + member + `":` + value + `}`)); err == nil {
				t.Fatal("contract marker presence accepted", member)
			}
		}
	}
	args := commandInputs(t)
	data, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	config["orderContract"] = nil
	data, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(args[1], data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := command(args, io.Discard); err == nil {
		t.Fatal("ordinary run accepted orderContract")
	}
}

func TestCheckpointRegularFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	projectPath, planPath := filepath.Join(dir, "project.json"), filepath.Join(dir, "plan.json")
	for _, path := range []string{projectPath, planPath} {
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(dir, "checkpoint.json")
	for _, tc := range []struct {
		name    string
		options options
	}{
		{"project directory", options{project: dir, plan: planPath, checkpointOutput: output}},
		{"plan directory", options{project: projectPath, plan: dir, checkpointOutput: output}},
		{"report directory", options{project: projectPath, plan: planPath, checkpointOutput: output, output: dir}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := checkOutputPaths(tc.options); err == nil {
				t.Fatal("nonregular checkpoint path accepted")
			}
		})
	}
}

func TestCheckpointSymlinkedParentAliases(t *testing.T) {
	t.Parallel()
	args := commandInputs(t)
	realDir := t.TempDir()
	link := filepath.Join(t.TempDir(), "parent-link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "new.json")
	alias := filepath.Join(link, "new.json")
	if err := commandWith(t.Context(), append(args, "-checkpoint-output", target, "-stop-at", "1", "-output", alias), io.Discard, io.Discard, testIdentify); err == nil || !strings.Contains(err.Error(), "outputs must differ") {
		t.Fatal("absent parent alias accepted", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("alias rejection created output", err)
	}
	for _, exists := range []bool{false, true} {
		if exists {
			if err := os.WriteFile(target, []byte("old checkpoint"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		called := false
		err := publishCheckpoint(t.Context(), target, options{output: alias}, func(io.Writer) error { called = true; return nil }, localFileOps())
		if err == nil || called {
			t.Fatal("aliased publication reached encoder", err)
		}
		if exists {
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "old checkpoint" {
				t.Fatal("alias changed old checkpoint", err)
			}
		}
	}
	missingParent := filepath.Join(realDir, "missing", "new.json")
	if err := checkOutputPaths(options{checkpointOutput: missingParent}); err == nil {
		t.Fatal("unresolved parent accepted")
	}
}
