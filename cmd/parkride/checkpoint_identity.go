package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/dotwaffle/podsim/internal/parkride"
)

func currentImplementation() (parkride.Implementation, error) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return parkride.Implementation{}, errors.New("continuation needs identified build information")
	}
	return implementationFromBuild(info, os.Executable)
}
func implementationFromBuild(info *debug.BuildInfo, executable func() (string, error)) (parkride.Implementation, error) {
	settings := make(map[string]string)
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["vcs.modified"] != "false" || settings["vcs.revision"] == "" || settings["GOEXPERIMENT"] != "jsonv2" {
		return parkride.Implementation{}, errors.New("continuation needs a clean identified jsonv2 build")
	}
	path, err := executable()
	if err != nil {
		return parkride.Implementation{}, fmt.Errorf("identify executable: %w", err)
	}
	file, err := os.Open(path) // #nosec G304 G703 -- The runtime selects this executable for identity hashing.
	if err != nil {
		return parkride.Implementation{}, err
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return parkride.Implementation{}, err
	}
	if !stat.Mode().IsRegular() {
		return parkride.Implementation{}, errors.New("continuation executable must be a regular file")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return parkride.Implementation{}, fmt.Errorf("hash executable: %w", err)
	}
	if err := file.Close(); err != nil {
		return parkride.Implementation{}, err
	}
	return parkride.Implementation{SourceRevision: settings["vcs.revision"], ExecutableSHA256: hex.EncodeToString(digest.Sum(nil)), GoVersion: info.GoVersion, GoExperiment: settings["GOEXPERIMENT"], GoOS: runtime.GOOS, GoArch: runtime.GOARCH}, nil
}
