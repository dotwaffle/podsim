package session

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
	"github.com/dotwaffle/podsim/internal/wirename"
)

// wireRoots are the values whose JSON forms make the save, topology, HTTP
// state, command and stream formats. The delta groups are separate roots,
// because a delta holds them as raw values.
func wireRoots() []any {
	return []any{
		stateFile{}, TopologySnapshot{}, StateFrame{}, State{}, ProjectState{},
		Command{}, Reply{}, StreamEnvelope{}, StreamHello{}, StateEnvelope{},
		couplingReplacement{},
	}
}

// deltaGroups maps each stream delta group to the type of its value.
var deltaGroups = map[string]any{
	"controls": controlsGroup{}, "global": globalGroup{}, "statistics": streamStatistics{},
	"demand": DemandState{}, "restore": RestoreInfo{}, "checkpoints": []Checkpoint{},
	"pending": []sim.Request{}, "coupling": couplingReplacement{},
}

// wirePaths returns the member paths of the wire roots and of the delta
// groups under /delta/groups.
func wirePaths(t *testing.T) (map[string]bool, []wirename.Member) {
	t.Helper()
	members, _ := wirename.Walk(wireRoots()...)
	paths := wirename.Paths(members)
	for name, value := range deltaGroups {
		group, _ := wirename.Walk(value)
		paths["/delta/groups/"+name] = true
		for _, member := range group {
			paths["/delta/groups/"+name+member.Path] = true
		}
		members = append(members, group...)
	}
	return paths, members
}

// TestWireMemberNamesAreLowerCamel keeps each member name of the formats in
// lowerCamel case, so that exact-case decoding has one spelling to match.
func TestWireMemberNamesAreLowerCamel(t *testing.T) {
	t.Parallel()
	_, members := wirePaths(t)
	for _, member := range members {
		if !wirename.LowerCamel(member.Name) {
			t.Errorf("%s.%s has JSON name %q, want lowerCamel", member.Owner, member.Field, member.Name)
		}
	}
}

// TestScannerLimitPathsMatchTags checks each path of the bounded prescans
// against the struct tags. A path that does not name a member with its
// exact case does not bound anything.
func TestScannerLimitPathsMatchTags(t *testing.T) {
	t.Parallel()
	paths, _ := wirePaths(t)
	sets := map[string]jsonLimits{
		"state":           stateJSONLimits,
		"command":         commandJSONLimits,
		"topology":        topologyJSONLimits,
		"service save":    serviceStateLimits(),
		"express save":    expressSavedLimits(),
		"coupling save":   couplingSavedLimits(false),
		"packed save":     couplingSavedLimits(true),
		"express stream":  streamLimits(contractMarkers{order: sim.ExpressOrderContract}),
		"unpacked stream": streamLimits(contractMarkers{}),
		"compact save":    compactStateLimits(serviceStateLimits()),
		"boarding save":   boardingStateLimits(serviceStateLimits()),
	}
	for name, limits := range sets {
		for path := range limits.arrays {
			// The walk lists members, not the elements of nested arrays.
			for strings.HasSuffix(path, "/*") {
				path = strings.TrimSuffix(path, "/*")
			}
			if !paths[path] {
				t.Errorf("%s limit path %q names no member", name, path)
			}
		}
	}
}

// TestScannerLiteralsMatchTags finds member names and JSON Pointers in the
// scanner sources that match a member name only without case.
func TestScannerLiteralsMatchTags(t *testing.T) {
	t.Parallel()
	_, members := wirePaths(t)
	files := []string{
		"boarding_state.go", "compact_state.go", "coupling_json.go",
		"coupling_state.go", "coupling_stream.go", "coupling_stream_json.go", "express_text.go",
		"express_wire.go", "http.go", "order_command.go", "order_state.go", "protocol.go",
		"state_file.go", "stream_boardings.go", "stream_codec.go", "stream_frame.go",
		"state_http.go", "stream_service.go", "topology_decode.go", "../remote/client.go", "../remote/stream.go",
	}
	found, err := wirename.Mismatch(files, wirename.Names(members))
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range found {
		t.Errorf("%s: %q matches a member name only without case", literal.Position, literal.Value)
	}
}

// TestWireNamesDetectDrift shows that the literal check finds a member name
// with a different case.
func TestWireNamesDetectDrift(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "drift.go")
	if err := os.WriteFile(source, []byte("package drift\n\nvar paths = []string{\"/project/network/Nodes\", \"berthids\", \"/project/network/nodes\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, members := wirePaths(t)
	found, err := wirename.Mismatch([]string{source}, wirename.Names(members))
	if err != nil {
		t.Fatal(err)
	}
	values := make([]string, len(found))
	for index, literal := range found {
		values[index] = literal.Value
	}
	if want := []string{"/project/network/Nodes", "berthids"}; !slices.Equal(values, want) {
		t.Fatalf("drift literals = %v, want %v", values, want)
	}
	if paths, _ := wirePaths(t); paths["/project/network/Nodes"] || !paths["/project/network/nodes"] {
		t.Fatal("member paths do not use the exact tag case")
	}
}
