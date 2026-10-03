// Command projectcheck validates a local project without advancing simulation.
package main

import (
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/dotwaffle/podsim/internal/project"
)

func main() {
	if err := command(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func projectPath(args []string, help io.Writer) (string, error) {
	flags := flag.NewFlagSet("projectcheck", flag.ContinueOnError)
	flags.SetOutput(help)
	path := flags.String("project", "", "local project JSON to validate (required)")
	if err := flags.Parse(args); err != nil {
		return "", err
	}
	if *path == "" || flags.NArg() != 0 {
		return "", errors.New("provide -project without positional arguments")
	}
	return *path, nil
}

func readProject(path string) ([]byte, error) {
	file, err := os.Open(path) // #nosec G304 G703 -- The operator selects the local project file for reading.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, project.MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > project.MaxFileBytes {
		return nil, errors.New("project exceeds the 10 MiB input bound")
	}
	return data, nil
}

func command(args []string, stdout io.Writer) error {
	path, err := projectPath(args, stdout)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := readProject(path)
	if err != nil {
		return fmt.Errorf("read project: %w", err)
	}
	var config project.Config
	if err := json.Unmarshal(data, &config, json.RejectUnknownMembers(true)); err != nil {
		return fmt.Errorf("decode project: %w", err)
	}
	if err := project.Validate(config); err != nil {
		return fmt.Errorf("validate project: %w", err)
	}
	if _, err := fmt.Fprintf(stdout, "Valid project %q: version %d, %d stations, %d lanes, %d pods.\nStatic project checks only. Simulation not advanced.\n",
		config.Name, config.Version, len(config.Network.Stations), len(config.Network.Lanes), len(config.Fleet)); err != nil {
		return fmt.Errorf("write project summary: %w", err)
	}
	return nil
}
