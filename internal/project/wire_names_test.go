package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/wirename"
)

// TestProjectMemberNamesAreLowerCamel keeps each member name of the project
// format in lowerCamel case.
func TestProjectMemberNamesAreLowerCamel(t *testing.T) {
	t.Parallel()
	members, _ := wirename.Walk(Config{})
	for _, member := range members {
		if !wirename.LowerCamel(member.Name) {
			t.Errorf("%s.%s has JSON name %q, want lowerCamel", member.Owner, member.Field, member.Name)
		}
	}
}

// TestProjectScannerLiteralsMatchTags finds member names and JSON Pointers
// in the project scanners that match a member name only without case.
func TestProjectScannerLiteralsMatchTags(t *testing.T) {
	t.Parallel()
	members, _ := wirename.Walk(Config{})
	paths := wirename.Paths(members)
	for _, path := range []string{
		"/network/nodes", "/network/lanes", "/network/stations", "/network/stations/*/berths",
		"/network/stations/*/banks", "/network/stations/*/banks/*/berthIDs",
		"/network/lanes/*/vehicleClasses", "/network/stations/*/vehicleClasses",
		"/network/stations/*/berths/*/vehicleClasses",
	} {
		if !paths[path] {
			t.Errorf("project array path %q has no member", path)
		}
	}
	found, err := wirename.Mismatch([]string{"banks.go", "service.go", "config.go", "station_queue.go"}, wirename.Names(members))
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range found {
		t.Errorf("%s: %q matches a member name only without case", literal.Position, literal.Value)
	}
}

// TestProjectRefusesCaseVariantMembers checks that a project member whose
// case differs from the declared name is unknown, with the options of any
// caller.
func TestProjectRefusesCaseVariantMembers(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ from, to string }{
		{`"name":`, `"Name":`},
		{`"nodes":`, `"Nodes":`},
		{`"speedLimit":`, `"SpeedLimit":`},
	} {
		if !bytes.Contains(raw, []byte(test.from)) {
			t.Fatal("project has no", test.from)
		}
		changed := bytes.Replace(raw, []byte(test.from), []byte(test.to), 1)
		var legacy, v1, v2 Config
		for name, err := range map[string]error{
			"legacy": json.Unmarshal(changed, &legacy),
			"v1":     jsonv2.Unmarshal(changed, &v1, json.DefaultOptionsV1()),
			"v2":     jsonv2.Unmarshal(changed, &v2, jsonv2.MatchCaseInsensitiveNames(true)),
		} {
			if err == nil || !strings.Contains(err.Error(), "unknown") {
				t.Errorf("%s decoder: %s gave %v", name, test.to, err)
			}
		}
	}
}
