// Command parkride runs finite car itineraries around native pod journeys.
package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/dotwaffle/podsim/internal/parkride"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func main() {
	if err := command(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	project, plan, output string
	duration              time.Duration
	queueLimit            int
}

func parseOptions(args []string, help io.Writer) (options, error) {
	var opts options
	flags := flag.NewFlagSet("parkride", flag.ContinueOnError)
	flags.SetOutput(help)
	flags.StringVar(&opts.project, "project", "", "validated project JSON (required)")
	flags.StringVar(&opts.plan, "plan", "", "finite itinerary JSON (required)")
	flags.DurationVar(&opts.duration, "duration", 0, "observation horizon, at most 24h (required)")
	flags.IntVar(&opts.queueLimit, "queue-limit", 0, "pending pod admission limit, 1 to 1000000 (required)")
	flags.StringVar(&opts.output, "output", "", "JSON report path (default stdout)")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if len(flags.Args()) != 0 || opts.project == "" || opts.plan == "" {
		return options{}, errors.New("provide -project, -plan, -duration, and -queue-limit without positional arguments")
	}
	if opts.duration <= 0 || opts.duration > 24*time.Hour || durationTicks(opts.duration) < 1 {
		return options{}, errors.New("duration must be at least one simulation tick and at most 24h")
	}
	if opts.queueLimit < 1 || opts.queueLimit > parkride.MaxQueueLimit {
		return options{}, errors.New("queue-limit must be 1 to 1000000")
	}
	if opts.output != "" {
		for _, source := range []string{opts.project, opts.plan} {
			if samePath(source, opts.output) {
				return options{}, errors.New("output must not replace a project or plan input")
			}
		}
	}
	return opts, nil
}

func samePath(a, b string) bool {
	first, firstErr := filepath.Abs(a)
	second, secondErr := filepath.Abs(b)
	if firstErr == nil && secondErr == nil && first == second {
		return true
	}
	firstInfo, firstErr := os.Stat(a)
	secondInfo, secondErr := os.Stat(b)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}

func readInput(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 -- The operator selects the offline input.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, project.MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > project.MaxFileBytes {
		return nil, errors.New("input exceeds the 10 MiB file bound")
	}
	return data, nil
}

func command(args []string, stdout io.Writer) error {
	opts, err := parseOptions(args, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := readInput(opts.project)
	if err != nil {
		return fmt.Errorf("read project: %w", err)
	}
	var config project.Config
	if decodeErr := json.Unmarshal(data, &config, json.RejectUnknownMembers(true)); decodeErr != nil {
		return fmt.Errorf("decode project: %w", decodeErr)
	}
	if validationErr := project.Validate(config); validationErr != nil {
		return fmt.Errorf("validate project: %w", validationErr)
	}
	data, err = readInput(opts.plan)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}
	plan, err := parkride.DecodePlan(data, config.Network)
	if err != nil {
		return fmt.Errorf("decode plan: %w", err)
	}
	run, runErr := parkride.NewRun(parkride.RunInput{Project: config, Plan: plan,
		HorizonTicks: durationTicks(opts.duration), QueueLimit: opts.queueLimit, Build: buildProvenance()})
	if run == nil {
		return runErr
	}
	for runErr == nil && !run.Done() {
		runErr = run.Step()
	}
	if err := writeReport(opts.output, stdout, run.Report()); err != nil {
		return err
	}
	return runErr
}

func durationTicks(duration time.Duration) int64 {
	return int64(duration * sim.TicksPerSecond / time.Second)
}

func writeReport(path string, stdout io.Writer, report parkride.Report) error {
	writer := stdout
	if path != "" {
		file, err := os.Create(path) // #nosec G304 -- The operator selects the report path.
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		writer = file
		if err := encodeReport(writer, report); err != nil {
			return err
		}
		return file.Close()
	}
	return encodeReport(writer, report)
}

func encodeReport(writer io.Writer, report parkride.Report) error {
	if err := json.MarshalWrite(writer, report, jsontext.WithIndent("  ")); err != nil {
		return err
	}
	_, err := fmt.Fprintln(writer)
	return err
}

func buildProvenance() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unavailable"
	}
	parts := []string{info.GoVersion, info.Main.Path, info.Main.Version}
	for _, setting := range info.Settings {
		if strings.HasPrefix(setting.Key, "vcs") {
			parts = append(parts, setting.Key+"="+setting.Value)
		}
	}
	return strings.Join(parts, " ")
}
