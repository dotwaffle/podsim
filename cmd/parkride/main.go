// Command parkride runs finite car itineraries around native pod journeys.
package main

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/dotwaffle/podsim/internal/parkride"
	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	commandErr := commandWith(ctx, os.Args[1:], os.Stdout, os.Stderr, currentImplementation)
	stop()
	if commandErr != nil {
		fmt.Fprintln(os.Stderr, commandErr)
		os.Exit(1)
	}
}

type options struct {
	project, plan, output             string
	duration                          time.Duration
	queueLimit                        int
	checkpointInput, checkpointOutput string
	replayTimeout                     time.Duration
	stopAt                            int64
	stopGiven                         bool
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
	flags.StringVar(&opts.checkpointInput, "checkpoint-input", "", "version-1 checkpoint to replay")
	flags.StringVar(&opts.checkpointOutput, "checkpoint-output", "", "atomic local version-1 checkpoint output")
	flags.Int64Var(&opts.stopAt, "stop-at", 0, "absolute checkpoint stop tick (requires checkpoint output)")
	flags.DurationVar(&opts.replayTimeout, "replay-timeout", 0, "positive replay watchdog duration (required for resume)")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	given := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { given[f.Name] = true })
	opts.stopGiven = given["stop-at"]
	if len(flags.Args()) != 0 {
		return options{}, errors.New("parkride accepts no positional arguments")
	}
	if opts.checkpointInput != "" {
		for _, name := range []string{"project", "plan", "duration", "queue-limit"} {
			if given[name] {
				return options{}, errors.New("resume controls come from the checkpoint origin")
			}
		}
		if opts.replayTimeout <= 0 {
			return options{}, errors.New("resume requires a positive -replay-timeout")
		}
	} else {
		if given["replay-timeout"] {
			return options{}, errors.New("-replay-timeout requires -checkpoint-input")
		}
		if opts.project == "" || opts.plan == "" {
			return options{}, errors.New("provide -project, -plan, -duration, and -queue-limit")
		}
		if opts.duration <= 0 || opts.duration > 24*time.Hour || durationTicks(opts.duration) < 1 {
			return options{}, errors.New("duration must be at least one simulation tick and at most 24h")
		}
		if opts.queueLimit < 1 || opts.queueLimit > parkride.MaxQueueLimit {
			return options{}, errors.New("queue-limit must be 1 to 1000000")
		}
	}
	if opts.stopGiven && (opts.checkpointOutput == "" || opts.stopAt < 0 || opts.stopAt > parkride.MaxHorizonTicks) {
		return options{}, errors.New("-stop-at requires checkpoint output and a bounded nonnegative tick")
	}
	if err := checkOutputPaths(opts); err != nil {
		return options{}, err
	}
	return opts, nil
}

func samePath(a, b string) bool {
	first, firstErr := resolvedPath(a)
	second, secondErr := resolvedPath(b)
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
	return commandWith(context.Background(), args, stdout, io.Discard, currentImplementation)
}

func commandWith(ctx context.Context, args []string, stdout, stderr io.Writer, identify func() (parkride.Implementation, error)) (commandErr error) {
	opts, err := parseOptions(args, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	checkpointing := opts.checkpointInput != "" || opts.checkpointOutput != ""
	if checkpointing {
		defer func() {
			if commandErr != nil {
				logger.ErrorContext(ctx, "Car continuation failed", slog.Any("error", commandErr))
			}
		}()
	}
	var identity *parkride.Implementation
	if checkpointing {
		facts, err := identify()
		if err != nil {
			return err
		}
		identity = &facts
	}
	var run *parkride.Run
	var runErr error
	if opts.checkpointInput != "" {
		file, err := os.Open(opts.checkpointInput) // #nosec G304 G703 -- The operator selects this local checkpoint input.
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		replayCtx, cancel := context.WithTimeout(ctx, opts.replayTimeout)
		started := time.Now()
		run, runErr = parkride.DecodeCheckpoint(replayCtx, file, parkride.ResumeInput{Implementation: *identity, Progress: func(progress parkride.ResumeProgress) {
			logger.InfoContext(replayCtx, "Replay candidate", slog.String("path", opts.checkpointInput), slog.String("checkpoint", progress.CheckpointID), slog.Int64("tick", progress.Tick), slog.Int64("targetTick", progress.TargetTick), slog.Duration("elapsed", time.Since(started)))
		}})
		cancel()
		if runErr != nil {
			return runErr
		}
		if err := file.Close(); err != nil {
			return err
		}
		logger.InfoContext(ctx, "Replay accepted", slog.String("path", opts.checkpointInput), slog.Int64("tick", run.Report().EndpointTick), slog.Duration("elapsed", time.Since(started)))
	} else {
		input, err := freshRunInput(opts)
		if err != nil {
			return err
		}
		input.Continuation = identity
		run, runErr = parkride.NewRunContext(ctx, input)
		if run == nil {
			return runErr
		}
	}
	report := run.Report()
	if opts.stopGiven && (opts.stopAt < report.EndpointTick || opts.stopAt > report.HorizonTicks) {
		return errors.New("-stop-at lies outside the checkpoint origin interval")
	}
	for runErr == nil && !run.Done() && (!opts.stopGiven || run.Report().EndpointTick < opts.stopAt) {
		runErr = run.StepContext(ctx)
	}
	if runErr == nil && opts.checkpointOutput != "" {
		runErr = publishCheckpoint(ctx, opts.checkpointOutput, opts, func(w io.Writer) error { return run.EncodeCheckpoint(ctx, w) }, localFileOps())
		if runErr == nil {
			logger.InfoContext(ctx, "Checkpoint published", slog.String("path", opts.checkpointOutput), slog.Int64("tick", run.Report().EndpointTick))
		}
	}
	if err := writeReport(opts.output, stdout, run.Report()); err != nil {
		return err
	}
	return runErr
}

func freshRunInput(opts options) (parkride.RunInput, error) {
	data, err := readInput(opts.project)
	if err != nil {
		return parkride.RunInput{}, fmt.Errorf("read project: %w", err)
	}
	if foundationErr := parkride.CheckFoundationProject(data); foundationErr != nil {
		return parkride.RunInput{}, foundationErr
	}
	var config project.Config
	if err = json.Unmarshal(data, &config, json.RejectUnknownMembers(true)); err != nil {
		return parkride.RunInput{}, fmt.Errorf("decode project: %w", err)
	}
	if err = project.Validate(config); err != nil {
		return parkride.RunInput{}, fmt.Errorf("validate project: %w", err)
	}
	data, err = readInput(opts.plan)
	if err != nil {
		return parkride.RunInput{}, fmt.Errorf("read plan: %w", err)
	}
	plan, err := parkride.DecodePlan(data, config.Network)
	if err != nil {
		return parkride.RunInput{}, fmt.Errorf("decode plan: %w", err)
	}
	return parkride.RunInput{Project: config, Plan: plan, HorizonTicks: durationTicks(opts.duration), QueueLimit: opts.queueLimit, Build: buildProvenance()}, nil
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
