package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestRunWritesRawProjectConfig(t *testing.T) {
	t.Parallel()
	var first bytes.Buffer
	if err := run([]string{"-preset", "scale100"}, &first); err != nil {
		t.Fatal(err)
	}
	var config project.Config
	if err := json.Unmarshal(first.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if err := project.Validate(config); err != nil {
		t.Fatal(err)
	}
	if len(config.Network.Stations) != 20 || len(config.Fleet) != 100 {
		t.Fatalf("got %d stations and %d pods", len(config.Network.Stations), len(config.Fleet))
	}
	var second bytes.Buffer
	if err := run([]string{"-preset", "scale100"}, &second); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("successive runs produced different JSON")
	}
}

func TestRunRejectsUnknownPreset(t *testing.T) {
	t.Parallel()
	if err := run([]string{"-preset", "missing"}, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted an unknown preset")
	}
	if err := run([]string{"extra"}, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted a positional argument")
	}
}
