package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/parkride"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func TestDurationAndRequiredOptions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text  string
		ticks int64
		valid bool
	}{
		{"1s", 60, true}, {"1m", 3600, true}, {"24h", parkride.MaxHorizonTicks, true}, {"17ms", 1, true}, {"16.666666ms", 0, false}, {"16.666667ms", 1, true}, {"1.5s", 90, true}, {"24h1ns", 0, false}, {"-1s", 0, false}, {"0s", 0, false},
	} {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			opts, err := parseOptions([]string{"-project", "project.json", "-plan", "plan.json", "-duration", tc.text, "-queue-limit", "200"}, io.Discard)
			if (err == nil) != tc.valid {
				t.Fatalf("duration %s: %v", tc.text, err)
			}
			if err == nil && durationTicks(opts.duration) != tc.ticks {
				t.Fatal("tick conversion drift")
			}
		})
	}
	for _, args := range [][]string{{}, {"-project", "p", "-plan", "p", "-duration", "1s"}, {"-project", "p", "-plan", "p", "-queue-limit", "200"}, {"-project", "p", "-plan", "p", "-duration", "1s", "-queue-limit", "-1"}, {"-project", "p", "-plan", "p", "-duration", "1s", "-queue-limit", "1000001"}, {"-project", "p", "-plan", "p", "-duration", "1s", "-queue-limit", "200", "extra"}, {"-project", "p", "-plan", "p", "-duration", "1s", "-queue-limit", "200", "-output", "p"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatal("invalid CLI accepted", args)
		}
	}
	if durationTicks(time.Second) != sim.TicksPerSecond {
		t.Fatal("ordinary second tick scale")
	}
}

func TestHelpDoesNotReadInputs(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	if err := command([]string{"-h", "-project", "missing"}, &output); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"-project", "-plan", "-duration", "-queue-limit", "-output"} {
		if !strings.Contains(output.String(), flag) {
			t.Fatal("help omitted", flag)
		}
	}
}

func commandInputs(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	projectPath := filepath.Join(dir, "project.json")
	planPath := filepath.Join(dir, "plan.json")
	config := project.Default()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(projectPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile("testdata/plan.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(planPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"-project", projectPath, "-plan", planPath, "-duration", "20m", "-queue-limit", "200"}
}

func TestCommandNativeJSONReport(t *testing.T) {
	t.Parallel()
	args := commandInputs(t)
	var output bytes.Buffer
	if err := command(args, &output); err != nil {
		t.Fatal(err)
	}
	var report parkride.Report
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Error != "" || report.Itineraries[0].Outcome != "completed" || report.Pods.Submitted != 2 || report.Pods.Completed != 2 || report.Lots[0].Occupancy != 0 || len(report.ProjectHash) != 64 || len(report.PlanHash) != 64 || report.Build == "" {
		t.Fatalf("report %+v", report)
	}
	reportPath := filepath.Join(t.TempDir(), "result.json")
	if err := command(append(args, "-output", reportPath), io.Discard); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), data) {
		t.Fatal("file report differs from stdout")
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("write fault") }

func TestCommandInputAndOutputGuards(t *testing.T) {
	t.Parallel()
	args := commandInputs(t)
	if err := command(args, failedWriter{}); err == nil {
		t.Fatal("report write failure swallowed")
	}
	for _, tc := range []struct {
		name, data string
		source     int
	}{
		{"unknown-plan", `{"lots":[],"itineraries":[],"extra":1}`, 3},
		{"null-consent", `{"lots":[],"itineraries":[{"sharingConsent":null}]}`, 3},
		{"invalid-project", `{"version":1}`, 1},
		{"unknown-project", `{"extra":1}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			local := commandInputs(t)
			if err := os.WriteFile(local[tc.source], []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			var report bytes.Buffer
			if err := command(local, &report); err == nil || report.Len() != 0 {
				t.Fatal("invalid input emitted report")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", project.MaxFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readInput(path); err == nil {
		t.Fatal("oversized input accepted")
	}
	link := filepath.Join(t.TempDir(), "same-source.json")
	if err := os.Link(args[1], link); err != nil {
		t.Fatal(err)
	}
	if _, err := parseOptions(append(args, "-output", link), io.Discard); err == nil {
		t.Fatal("output overwrites input through hard link")
	}
}
