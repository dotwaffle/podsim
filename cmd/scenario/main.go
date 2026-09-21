// Command scenario writes a generated server scenario as JSON.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("scenario", flag.ContinueOnError)
	flags.SetOutput(stdout)
	preset := flags.String("preset", "scale100", "Preset: small, busy, parking-constrained, rail-hub, or scale100")
	output := flags.String("output", "", "Output file; omit to write standard output")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("scenario does not accept positional arguments")
	}
	config, err := presetConfig(*preset)
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
	encoder := json.NewEncoder(destination)
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
	return nil
}

func presetConfig(name string) (project.Config, error) {
	switch name {
	case "small":
		return scenarios.Small(), nil
	case "busy":
		return scenarios.Busy(), nil
	case "parking-constrained":
		return scenarios.ParkingConstrained(), nil
	case "rail-hub":
		return scenarios.RailHub(), nil
	case "scale100":
		return scenarios.Scale100(), nil
	default:
		return project.Config{}, fmt.Errorf("unknown preset %q", name)
	}
}
