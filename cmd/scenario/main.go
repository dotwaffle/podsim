// Command scenario writes a generated server scenario as JSON.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// presets lists the preset names in help order.
var presets = []string{"small", "busy", "parking-constrained", "rail-hub", "rail-hub-incidents", "scale100", "london-central", "london-full", "london-full-express", "independent-banks", "compact-station"}

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
	parkingLanes                 int
	stationPods, parkingPods     int
	berths                       map[string]int
	berthPitch                   float64
	compactStations              []string
}

func run(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scenario", flag.ContinueOnError)
	flags.SetOutput(stdout)
	preset := flags.String("preset", "scale100", "Preset: "+strings.Join(presets, ", "))
	output := flags.String("output", "", "Output file; omit to write standard output")
	var options capacity
	flags.IntVar(&options.stationBerths, "station-berths", 0, "Berths at each passenger station; omit to keep the preset value")
	flags.IntVar(&options.parkingBerths, "parking-berths", 0, "Berths at each Parking station; omit to keep the preset value")
	flags.IntVar(&options.parkingLanes, "parking-lanes", 0, "Ring and London presets: queue lanes of a depot at each Parking station; -parking-berths then sets the slots of each lane; 0 gives London the ordinary facilities of "+strconv.Itoa(scenarios.LondonParkingBerths)+" berths")
	berths := flags.String("berths", "", "Berths at single stations, as comma-separated ID=N items")
	flags.IntVar(&options.stationPods, "station-pods", 0, "London presets only: initial pods at each passenger station")
	flags.IntVar(&options.parkingPods, "parking-pods", 0, "London presets only: initial pods at each Parking facility")
	flags.Float64Var(&options.berthPitch, "berth-pitch", 0, "Distance in meters between two berths of a station, at least 25")
	compactStations := flags.String("compact-stations", "", "London presets only: comma-separated IDs of at most 8 passenger stations with Compact bays, or none; each pod is then Compact")
	var incidents incidentFlags
	incidents.register(flags)
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
	if options.set["compact-stations"] {
		parsed, err := parseCompactStations(*compactStations)
		if err != nil {
			return err
		}
		options.compactStations = parsed
	}
	if err := incidents.parse(options.set); err != nil {
		return err
	}
	config, err := presetConfig(*preset, options)
	if err != nil {
		return err
	}
	incidents.write(&config, options.set)
	if validateErr := project.Validate(config); validateErr != nil {
		return fmt.Errorf("validate %s preset: %w", *preset, validateErr)
	}
	data, err := scenarioJSON(config, project.MaxFileBytes)
	if err != nil {
		return err
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
	if _, err := counter.Write(data); err != nil {
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

// scenarioJSON keeps indented output when it fits the project file limit.
// Larger projects use the same compact encoding as project validation.
func scenarioJSON(config project.Config, limit int) ([]byte, error) {
	data, err := jsonv2.Marshal(config, json.DefaultOptionsV1(), jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("encode scenario: %w", err)
	}
	if len(data)+1 > limit {
		data, err = jsonv2.Marshal(config, jsonv2.Deterministic(true))
		if err != nil {
			return nil, fmt.Errorf("encode compact scenario: %w", err)
		}
	}
	if len(data)+1 > limit {
		return nil, fmt.Errorf("scenario JSON has %d bytes, limit %d", len(data)+1, limit)
	}
	return append(data, '\n'), nil
}

// presetConfig returns the named preset with the capacity flags.
func presetConfig(name string, options capacity) (project.Config, error) {
	if !slices.Contains(presets, name) {
		return project.Config{}, fmt.Errorf("unknown preset %q", name)
	}
	fixtures := map[string]string{"independent-banks": "independent bank fixture", "compact-station": "compact station fixture", "london-full-express": "LondonFull Express preset"}
	if fixture, ok := fixtures[name]; ok {
		for _, key := range []string{"station-berths", "parking-berths", "parking-lanes", "station-pods", "parking-pods", "berths", "berth-pitch", "compact-stations"} {
			if options.set[key] {
				return project.Config{}, fmt.Errorf("-%s does not apply to the %s", key, fixture)
			}
		}
	}
	if name == "compact-station" {
		return scenarios.CompactStation()
	}
	if name == "london-full-express" {
		return scenarios.GenerateLondonFullExpress()
	}
	if name == "independent-banks" {
		config := project.Default()
		config.Name, config.Network = "Independent station banks", sim.BankExample()
		config.Fleet = []sim.Placement{{ID: "01", StationID: "origin", BerthID: "origin-1"}, {ID: "02", StationID: "hub", BerthID: "bank-a-1"}, {ID: "03", StationID: "parking", BerthID: "parking-1"}}
		return config, nil
	}
	if name == "london-central" || name == "london-full" {
		return londonPresetConfig(name, options)
	}
	for _, flag := range []string{"station-pods", "parking-pods", "compact-stations"} {
		if options.set[flag] {
			return project.Config{}, fmt.Errorf("-%s applies only to the London presets", flag)
		}
	}
	return scenarios.PresetWith(name, func(parameters *scenarios.Parameters) {
		setInt(options.set["station-berths"], &parameters.PassengerBerths, options.stationBerths)
		setInt(options.set["parking-berths"], &parameters.ParkingBerths, options.parkingBerths)
		setInt(options.set["parking-lanes"], &parameters.ParkingLanes, options.parkingLanes)
		if options.set["berths"] {
			parameters.StationBerths = options.berths
		}
		if options.set["berth-pitch"] {
			parameters.BerthPitch = options.berthPitch
		}
	})
}

// londonPresetConfig applies explicit flags over the selected preset defaults.
func londonPresetConfig(name string, options capacity) (project.Config, error) {
	london := scenarios.DefaultLondonCentralOptions()
	build := scenarios.LondonCentralWith
	if name == "london-full" {
		london, build = scenarios.DefaultLondonFullOptions(), scenarios.LondonFullWith
	}
	if options.set["station-berths"] {
		london.StationBerths, london.Berths = options.stationBerths, nil
	}
	setInt(options.set["parking-berths"], &london.ParkingBerths, options.parkingBerths)
	setInt(options.set["station-pods"], &london.StationPods, options.stationPods)
	setInt(options.set["parking-pods"], &london.ParkingPods, options.parkingPods)
	setInt(options.set["parking-lanes"], &london.ParkingLanes, options.parkingLanes)
	// The berth count of a depot is the slot count of each lane, so the
	// ordinary facilities get their own default.
	if options.set["parking-lanes"] && options.parkingLanes == 0 && !options.set["parking-berths"] {
		london.ParkingBerths = scenarios.LondonParkingBerths
	}
	if options.set["compact-stations"] {
		london.CompactStations = options.compactStations
	}
	if options.set["berths"] {
		if london.Berths == nil {
			london.Berths = make(map[string]int)
		}
		maps.Copy(london.Berths, options.berths)
	}
	if options.set["berth-pitch"] {
		london.BerthPitch = options.berthPitch
	}
	// The preset gives each LondonFull station a berth count, so only the
	// -berths flag tells an explicit count of a Compact station.
	for _, id := range london.CompactStations {
		if _, ok := options.berths[id]; ok {
			return project.Config{}, fmt.Errorf("-berths names Compact station %q, which has the 4 bays of the compact station; use -compact-stations none or a list without it", id)
		}
	}
	return build(london)
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

// parseCompactStations parses a -compact-stations value of comma-separated
// station IDs. The value none gives no IDs.
func parseCompactStations(text string) ([]string, error) {
	if text == "none" {
		return nil, nil
	}
	var ids []string
	for item := range strings.SplitSeq(text, ",") {
		id := strings.TrimSpace(item)
		if id == "" {
			return nil, fmt.Errorf("-compact-stations item %q has no station ID", item)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// incidentFlags holds the incident flags. Each flag that is set writes its
// project member over the preset value, and a flag that is not set writes
// nothing. duration is the parsed value of -fault-duration.
type incidentFlags struct {
	faultRate, debrisShare, debrisMeters, emergencyRate float64
	evacuationSeconds                                   int
	faultDuration                                       string
	duration                                            *project.FaultDuration
}

// register adds the incident flags to flags.
func (f *incidentFlags) register(flags *flag.FlagSet) {
	flags.Float64Var(&f.faultRate, "fault-rate", 0, "Scenario faults in a simulated hour, from 0 to 60; omit to keep the preset value")
	flags.Float64Var(&f.debrisShare, "debris-share", 0, "Share of scenario faults that are debris, from 0 to 1; needs -fault-rate")
	flags.Float64Var(&f.debrisMeters, "debris-meters", 0, "Length of scenario debris, from 0.5 to 50 meters; needs -fault-rate")
	flags.StringVar(&f.faultDuration, "fault-duration", "", "Duration of scenario faults in seconds, as fixed:S, uniform:MIN:MAX, or exponential:MIN:MAX:MEAN; needs -fault-rate")
	flags.IntVar(&f.evacuationSeconds, "evacuation-seconds", 0, "Seconds from a pod fault to the evacuation, from 0 to 3,600; omit to keep the preset value")
	flags.Float64Var(&f.emergencyRate, "emergency-rate", 0, "Scenario emergencies in a simulated hour, from 0 to 60; omit to keep the preset value")
}

// parse applies the refusals of the incident flags, in their fixed order.
// project.Validate checks the values later.
func (f *incidentFlags) parse(set map[string]bool) error {
	if set["fault-duration"] {
		duration, err := parseFaultDuration(f.faultDuration)
		if err != nil {
			return err
		}
		f.duration = duration
	}
	if !set["fault-rate"] && (set["debris-share"] || set["debris-meters"] || set["fault-duration"]) {
		return errors.New("-debris-share, -debris-meters, and -fault-duration need -fault-rate")
	}
	return nil
}

// write sets the project member of each flag in set.
func (f *incidentFlags) write(config *project.Config, set map[string]bool) {
	if set["fault-rate"] {
		faultConfig(config).PerHour = new(f.faultRate)
	}
	if set["debris-share"] {
		faultConfig(config).DebrisShare = new(f.debrisShare)
	}
	if set["debris-meters"] {
		faultConfig(config).DebrisMeters = new(f.debrisMeters)
	}
	if set["fault-duration"] {
		faultConfig(config).Duration = f.duration
	}
	if set["evacuation-seconds"] {
		faultConfig(config).EvacuationSeconds = new(f.evacuationSeconds)
	}
	if set["emergency-rate"] {
		config.Emergencies = &project.EmergencyConfig{PerHour: new(f.emergencyRate)}
	}
}

// faultConfig returns the faults member of config, and adds it when it is
// absent.
func faultConfig(config *project.Config) *project.FaultConfig {
	if config.Faults == nil {
		config.Faults = &project.FaultConfig{}
	}
	return config.Faults
}

// parseFaultDuration parses a -fault-duration value.
func parseFaultDuration(text string) (*project.FaultDuration, error) {
	errForm := errors.New("-fault-duration must be fixed:S, uniform:MIN:MAX, or exponential:MIN:MAX:MEAN")
	kind, rest, _ := strings.Cut(text, ":")
	var values []*int
	for item := range strings.SplitSeq(rest, ":") {
		value, err := strconv.Atoi(item)
		if err != nil {
			return nil, errForm
		}
		values = append(values, new(value))
	}
	switch {
	case kind == "fixed" && len(values) == 1:
		return &project.FaultDuration{Kind: kind, Seconds: values[0]}, nil
	case kind == "uniform" && len(values) == 2:
		return &project.FaultDuration{Kind: kind, MinSeconds: values[0], MaxSeconds: values[1]}, nil
	case kind == "exponential" && len(values) == 3:
		return &project.FaultDuration{Kind: kind, MinSeconds: values[0], MaxSeconds: values[1], MeanSeconds: values[2]}, nil
	}
	return nil, errForm
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
	network, err := jsonv2.Marshal(config.Network, json.DefaultOptionsV1())
	if err != nil {
		return fmt.Errorf("encode network: %w", err)
	}
	soft := 0
	switch preset {
	case "london-central":
		soft, err = scenarios.LondonCentralSoftConflicts(config.Network)
	case "london-full", "london-full-express":
		soft, err = scenarios.LondonFullSoftConflicts(config.Network)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stderr, "preset=%s passenger_berths=%d parking_berths=%d pods=%d nodes=%d/%d lanes=%d/%d bytes=%d network_sha256=%x soft_conflicts=%d\n",
		preset, passenger, parking, len(config.Fleet), len(config.Network.Nodes), project.MaxNodes,
		len(config.Network.Lanes), project.MaxLanes, size, sha256.Sum256(network), soft)
	return err
}
