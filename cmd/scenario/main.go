// Command scenario writes a generated server scenario as JSON.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// presets lists the preset names in help order.
var presets = []string{"small", "busy", "parking-constrained", "rail-hub", "scale100", "london"}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// capacity holds the station capacity flags. set holds the names of the
// flags on the command line. A flag that is not set keeps the preset value.
type capacity struct {
	set                          map[string]bool
	stationBerths, parkingBerths int
	stationPods, parkingPods     int
	berths                       map[string]int
	berthPitch                   float64
}

func run(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scenario", flag.ContinueOnError)
	flags.SetOutput(stdout)
	preset := flags.String("preset", "scale100", "Preset: small, busy, parking-constrained, rail-hub, scale100, or london")
	output := flags.String("output", "", "Output file; omit to write standard output")
	var options capacity
	flags.IntVar(&options.stationBerths, "station-berths", 0, "Berths at each passenger station; omit to keep the preset value")
	flags.IntVar(&options.parkingBerths, "parking-berths", 0, "Berths at each Parking station; omit to keep the preset value")
	berths := flags.String("berths", "", "Berths at single stations, as comma-separated ID=N items")
	flags.IntVar(&options.stationPods, "station-pods", 0, "London only: initial pods at each passenger station")
	flags.IntVar(&options.parkingPods, "parking-pods", 0, "London only: initial pods at each Parking facility")
	flags.Float64Var(&options.berthPitch, "berth-pitch", 0, "Distance in meters between two berths of a station, at least 25")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("scenario does not accept positional arguments")
	}
	options.set = make(map[string]bool)
	flags.Visit(func(flag *flag.Flag) { options.set[flag.Name] = true })
	if options.set["berths"] {
		parsed, err := parseBerths(*berths)
		if err != nil {
			return err
		}
		options.berths = parsed
	}
	config, err := presetConfig(*preset, options)
	if err != nil {
		return err
	}
	if validateErr := project.Validate(config); validateErr != nil {
		return fmt.Errorf("validate %s preset: %w", *preset, validateErr)
	}
	destination := stdout
	var file *os.File
	if *output != "" {
		file, err = os.Create(*output) // #nosec G304 -- The operator selects this local output file.
		if err != nil {
			return fmt.Errorf("create output: %w", err)
		}
		defer func() {
			if file != nil {
				_ = file.Close()
			}
		}()
		destination = file
	}
	counter := &countingWriter{writer: destination}
	encoder := json.NewEncoder(counter)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(config); err != nil {
		return fmt.Errorf("write scenario: %w", err)
	}
	if file != nil {
		if err := file.Close(); err != nil {
			return fmt.Errorf("close output: %w", err)
		}
		file = nil
	}
	return writeSummary(stderr, *preset, config, counter.count)
}

// presetConfig returns the named preset with the capacity flags.
func presetConfig(name string, options capacity) (project.Config, error) {
	if !slices.Contains(presets, name) {
		return project.Config{}, fmt.Errorf("unknown preset %q", name)
	}
	if name == "london" {
		london := scenarios.DefaultLondonOptions()
		setInt(options.set["station-berths"], &london.StationBerths, options.stationBerths)
		setInt(options.set["parking-berths"], &london.ParkingBerths, options.parkingBerths)
		setInt(options.set["station-pods"], &london.StationPods, options.stationPods)
		setInt(options.set["parking-pods"], &london.ParkingPods, options.parkingPods)
		if options.set["berths"] {
			london.Berths = options.berths
		}
		if options.set["berth-pitch"] {
			london.BerthPitch = options.berthPitch
		}
		return scenarios.LondonWith(london)
	}
	for _, flag := range []string{"station-pods", "parking-pods"} {
		if options.set[flag] {
			return project.Config{}, fmt.Errorf("-%s applies only to the london preset", flag)
		}
	}
	return scenarios.PresetWith(name, func(parameters *scenarios.Parameters) {
		setInt(options.set["station-berths"], &parameters.PassengerBerths, options.stationBerths)
		setInt(options.set["parking-berths"], &parameters.ParkingBerths, options.parkingBerths)
		if options.set["berths"] {
			parameters.StationBerths = options.berths
		}
		if options.set["berth-pitch"] {
			parameters.BerthPitch = options.berthPitch
		}
	})
}

// setInt sets target to value when set is true.
func setInt(set bool, target *int, value int) {
	if set {
		*target = value
	}
}

// parseBerths parses a -berths value of comma-separated ID=N items.
func parseBerths(text string) (map[string]int, error) {
	berths := make(map[string]int)
	for item := range strings.SplitSeq(text, ",") {
		id, count, ok := strings.Cut(item, "=")
		id = strings.TrimSpace(id)
		if !ok || id == "" {
			return nil, fmt.Errorf("-berths item %q is not ID=N", item)
		}
		value, err := strconv.Atoi(strings.TrimSpace(count))
		if err != nil {
			return nil, fmt.Errorf("-berths item %q has no whole berth count", item)
		}
		if _, ok := berths[id]; ok {
			return nil, fmt.Errorf("-berths names station %q more than once", id)
		}
		berths[id] = value
	}
	return berths, nil
}

// countingWriter counts the bytes that it writes.
type countingWriter struct {
	writer io.Writer
	count  int
}

func (w *countingWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	w.count += written
	return written, err
}

// writeSummary writes one line about the generated project: its preset,
// berths, pods, nodes and lanes with their limits, output bytes, network
// SHA-256, and soft layout conflicts.
func writeSummary(stderr io.Writer, preset string, config project.Config, size int) error {
	passenger, parking := 0, 0
	for _, station := range config.Network.Stations {
		if station.ParkingOnly {
			parking += len(station.Berths)
		} else {
			passenger += len(station.Berths)
		}
	}
	network, err := json.Marshal(config.Network)
	if err != nil {
		return fmt.Errorf("encode network: %w", err)
	}
	soft := 0
	if preset == "london" {
		if soft, err = scenarios.LondonSoftConflicts(config.Network); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(stderr, "preset=%s passenger_berths=%d parking_berths=%d pods=%d nodes=%d/%d lanes=%d/%d bytes=%d network_sha256=%x soft_conflicts=%d\n",
		preset, passenger, parking, len(config.Fleet), len(config.Network.Nodes), project.MaxNodes,
		len(config.Network.Lanes), project.MaxLanes, size, sha256.Sum256(network), soft)
	return err
}
