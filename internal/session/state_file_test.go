package session

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

var update = flag.Bool("update", false, "write the golden files in testdata again")

const (
	// goldenStatePath holds the JSON form of newTestStateFile, indented.
	goldenStatePath = "testdata/state_v1.json"
	// stateMembersPath lists the members of the state file.
	stateMembersPath = "testdata/state_v1_members.txt"
	// stateMembersHeader starts the member list.
	stateMembersHeader = `# Members of the session state file, version 1.
# A change here needs a version bump: change stateVersion in state_file.go.
# Each line is a member path and its JSON kind. "[]" is an array element.
# TestStateFileMembers compares this list with the Go types.
`
	// testStateEpoch is a fixed epoch, so that the golden file does not
	// change with each run.
	testStateEpoch = "N4ZDMYDG2PQ6QJ5MIH3DTWMF7U"
	// testBuildID is a build ID of the correct form.
	testBuildID = "b9a50fb31ed44c66"
)

// newTestStateFile returns the state file of the example project with
// demand, an active journey, a save point and generated orders. The clock
// does not run, so the pods do not move and no value depends on the
// floating-point behavior of a machine.
func newTestStateFile(t *testing.T) stateFile {
	t.Helper()
	config := project.Default()
	config.Demand.Enabled = true
	shared, err := NewWithProject(config, WithBuildID(testBuildID))
	if err != nil {
		t.Fatal(err)
	}
	client := newTestClient(shared, "state")
	client.mustApply(t, Command{Action: "trip", Origin: "harbor", Destination: "market"})
	client.mustApply(t, Command{Action: "checkpoint"})
	client.mustApply(t, Command{Action: "speed", Speed: 2})
	// Two orders at 2 orders each minute, and part of a third.
	for range 4000 {
		shared.demand.step(shared.simulation)
	}
	file := sessionStateFile(t, shared)
	if len(file.Simulation.Waiting) == 0 || file.Demand.State.Generated == 0 ||
		!slices.ContainsFunc(file.Simulation.Pods, func(pod sim.SavedPod) bool { return len(pod.Route) > 0 }) {
		t.Fatalf("the test state has no queued order, generated order or pod route: %+v", file.Simulation)
	}
	return file
}

// sessionStateFile copies the state of shared into a state file, as a save
// does. The file has a fixed epoch and time.
func sessionStateFile(t *testing.T, shared *Session) stateFile {
	t.Helper()
	random, err := shared.demand.pcg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return stateFile{
		Format: stateFormat, Version: stateVersion, Final: true,
		SavedAt: time.Date(2026, time.September, 23, 9, 0, 0, 123456789, time.UTC),
		Build:   shared.build, Epoch: testStateEpoch,
		Revision: shared.revision, ProjectRevision: shared.projectRevision, Generation: shared.generation,
		LastCheckpoint: shared.lastCheckpoint, Speed: shared.speed, RestoreAttempts: 1,
		Sequences:  shared.commandSequences(),
		Demand:     savedDemand{State: shared.demand.state, Random: random, Budget: shared.demand.budget},
		Simulation: shared.simulation.ExportState(),
		Project:    shared.project,
	}
}

// testSequences returns count client sequences with increasing client IDs.
func testSequences(count int) []savedSequence {
	sequences := make([]savedSequence, count)
	for index := range sequences {
		sequences[index] = savedSequence{Client: fmt.Sprintf("client%05d", index), Sequence: uint64(index + 1)}
	}
	return sequences
}

// encodeTestState encodes file with a new encoder.
func encodeTestState(t *testing.T, file stateFile) []byte {
	t.Helper()
	var encoder stateEncoder
	data, err := encoder.encode(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// compressTestJSON compresses data with gzip.
func compressTestJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// decompressTestJSON returns the JSON form of a state file.
func decompressTestJSON(t *testing.T, data []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// stateReason returns the reason code of a decode error, or "" when err is
// not a *stateError.
func stateReason(err error) string {
	if stateErr, ok := errors.AsType[*stateError](err); ok {
		return stateErr.reason
	}
	return ""
}

// decodeCheckedState decodes a state file and checks its session members, as
// a restore does before it restores the simulation.
func decodeCheckedState(data []byte) (stateFile, error) {
	file, err := decodeStateFile(data)
	if err != nil {
		return stateFile{}, err
	}
	if err := file.validate(); err != nil {
		return stateFile{}, invalidState(err)
	}
	return file, nil
}

func TestStateFileRoundTrip(t *testing.T) {
	t.Parallel()
	// A new session has revision 0 until its first change.
	fresh, err := NewWithProject(project.Default(), WithBuildID(testBuildID))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		file stateFile
	}{
		{"example with demand", newTestStateFile(t)},
		{"new session", sessionStateFile(t, fresh)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeStateFile(encodeTestState(t, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.file) {
				t.Fatalf("decoded state file differs:\ngot  %+v\nwant %+v", got, tc.file)
			}
		})
	}
}

// TestStateFileGolden fixes the JSON form of the state file. The golden file
// is indented, and the test removes the indentation before it compares. Run
// the test with -update to write the file again.
func TestStateFileGolden(t *testing.T) {
	t.Parallel()
	var encoder stateEncoder
	if *update {
		data, err := encoder.encode(newTestStateFile(t))
		if err != nil {
			t.Fatal(err)
		}
		indented := jsontext.Value(decompressTestJSON(t, data))
		if err := indented.Indent(jsontext.WithIndent("  ")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenStatePath, append(indented, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(goldenStatePath)
	if err != nil {
		t.Fatal(err)
	}
	want := jsontext.Value(golden)
	if err = want.Compact(); err != nil {
		t.Fatal(err)
	}
	file, err := decodeStateFile(compressTestJSON(t, want))
	if err != nil {
		t.Fatal(err)
	}
	if file.Demand.State.Generated == 0 || len(file.Simulation.Waiting) == 0 {
		t.Fatalf("the golden state has no generated or queued order: %+v", file.Demand.State)
	}
	data, err := encoder.encode(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := decompressTestJSON(t, data); !bytes.Equal(got, want) {
		t.Fatalf("the golden state encodes differently:\n%s", got)
	}
}

func TestDecodeStateFileRejects(t *testing.T) {
	t.Parallel()
	valid := newTestStateFile(t)
	compressed := encodeTestState(t, valid)
	raw := decompressTestJSON(t, compressed)
	edit := func(change func(*stateFile)) []byte {
		file := valid
		change(&file)
		return encodeTestState(t, file)
	}
	// replace changes the first match of old in the JSON form to text.
	replace := func(old, text string) []byte {
		if !bytes.Contains(raw, []byte(old)) {
			t.Fatalf("the state has no %s", old)
		}
		return compressTestJSON(t, bytes.Replace(raw, []byte(old), []byte(text), 1))
	}
	// insert adds text after the first match of after in the JSON form.
	insert := func(after, text string) []byte { return replace(after, after+text) }
	badChecksum := slices.Clone(compressed)
	badChecksum[len(badChecksum)-8] ^= 0xff
	nested := strings.Repeat("[", 64) + strings.Repeat("]", 64)
	zeros := func(count int) string { return "[" + strings.Repeat("0,", count-1) + "0]" }
	tests := []struct {
		name   string
		data   []byte
		reason string
		// err is nil or an error that the result must wrap.
		err error
	}{
		{"decompressed too large", compressTestJSON(t, make([]byte, MaxStateBytes+1)), reasonTooLarge, ErrStateTooLarge},
		{"compressed too large", make([]byte, MaxStateBytes+1), reasonTooLarge, ErrStateTooLarge},
		{"cut at half", compressed[:len(compressed)/2], reasonInvalidState, io.ErrUnexpectedEOF},
		{"bad checksum", badChecksum, reasonInvalidState, gzip.ErrChecksum},
		{"not gzip", raw, reasonInvalidState, gzip.ErrHeader},
		{"empty", nil, reasonInvalidState, io.EOF},
		{"version 2", edit(func(file *stateFile) { file.Version = 2 }), reasonUnsupportedVersion, nil},
		{"format x", edit(func(file *stateFile) { file.Format = "x" }), reasonUnsupportedVersion, nil},
		{"unknown member at the top", insert(`{`, `"extra":1,`), reasonInvalidState, json.ErrUnknownName},
		{"unknown member in the project", insert(`"project":{`, `"extra":1,`), reasonInvalidState, json.ErrUnknownName},
		{"unknown member in the simulation", insert(`"simulation":{`, `"extra":1,`), reasonInvalidState, json.ErrUnknownName},
		{"duplicate member", insert(`{`, `"speed":1,`), reasonInvalidState, jsontext.ErrDuplicateName},
		{"trailing data", compressTestJSON(t, append(slices.Clone(raw), "{}"...)), reasonInvalidState, nil},
		{"depth 65", insert(`{`, `"extra":`+nested+`,`), reasonInvalidState, errJSONTooDeep},
		{"array of 32,769 elements", insert(`{`, `"extra":`+zeros(32_769)+`,`), reasonInvalidState, errJSONArrayTooLong},
		{"201 pods", edit(func(file *stateFile) {
			file.Simulation.Pods = make([]sim.SavedPod, 201)
		}), reasonInvalidState, errJSONArrayTooLong},
		{"project too large", replace(`"name":"Podsim example"`, `"name":"`+strings.Repeat("x", project.MaxFileBytes)+`"`),
			reasonInvalidState, errProjectTooLarge},
		{"demand error of 1 KiB+1", edit(func(file *stateFile) {
			file.Demand.State.Error = strings.Repeat("x", 1<<10+1)
		}), reasonInvalidState, nil},
		{"build xyz", edit(func(file *stateFile) { file.Build = "xyz" }), reasonInvalidState, nil},
		{"build in uppercase", edit(func(file *stateFile) { file.Build = strings.ToUpper(testBuildID) }), reasonInvalidState, nil},
		{"no epoch", edit(func(file *stateFile) { file.Epoch = "" }), reasonInvalidState, nil},
		{"epoch of 101 bytes", edit(func(file *stateFile) { file.Epoch = strings.Repeat("E", 101) }), reasonInvalidState, nil},
		{"project revision 0", edit(func(file *stateFile) { file.ProjectRevision = 0 }), reasonInvalidState, nil},
		{"generation 0", edit(func(file *stateFile) { file.Generation = 0 }), reasonInvalidState, nil},
		{"largest revision", edit(func(file *stateFile) { file.Revision = math.MaxUint64 }), reasonInvalidState, nil},
		{"largest project revision", edit(func(file *stateFile) { file.ProjectRevision = math.MaxUint64 }), reasonInvalidState, nil},
		{"largest generation", edit(func(file *stateFile) { file.Generation = math.MaxUint64 }), reasonInvalidState, nil},
		{"negative restore attempts", edit(func(file *stateFile) { file.RestoreAttempts = -1 }), reasonInvalidState, nil},
		{"speed 3", edit(func(file *stateFile) { file.Speed = 3 }), reasonInvalidState, nil},
		{"demand rate 0", edit(func(file *stateFile) { file.Demand.State.Config.PerMinute = 0 }), reasonInvalidState, nil},
		{"unknown demand destination", edit(func(file *stateFile) {
			file.Demand.State.Config.Pattern, file.Demand.State.Config.Destination = "destination", "nowhere"
		}), reasonInvalidState, nil},
		{"generated -1", edit(func(file *stateFile) { file.Demand.State.Generated = -1 }), reasonInvalidState, nil},
		{"skipped -1", edit(func(file *stateFile) { file.Demand.State.Skipped = -1 }), reasonInvalidState, nil},
		{"random source", edit(func(file *stateFile) { file.Demand.Random = []byte("pcg:") }), reasonInvalidState, nil},
		{"budget -1", edit(func(file *stateFile) { file.Demand.Budget = -1 }), reasonInvalidState, nil},
		{"budget 3600", edit(func(file *stateFile) { file.Demand.Budget = 3600 }), reasonInvalidState, nil},
		{"1,025 client sequences", edit(func(file *stateFile) {
			file.Sequences = testSequences(clientLimit + 1)
		}), reasonInvalidState, errJSONArrayTooLong},
		{"client ID of 0 bytes", edit(func(file *stateFile) {
			file.Sequences = []savedSequence{{Client: "", Sequence: 1}}
		}), reasonInvalidState, nil},
		{"client ID of 101 bytes", edit(func(file *stateFile) {
			file.Sequences = []savedSequence{{Client: strings.Repeat("c", maxClientBytes+1), Sequence: 1}}
		}), reasonInvalidState, nil},
		{"client ID not UTF-8", replace(`"client":"state"`, "\"client\":\"\xff\""), reasonInvalidState, nil},
		{"client sequence 0", edit(func(file *stateFile) {
			file.Sequences = []savedSequence{{Client: "c", Sequence: 0}}
		}), reasonInvalidState, nil},
		{"duplicate client", edit(func(file *stateFile) {
			file.Sequences = []savedSequence{{Client: "c", Sequence: 1}, {Client: "c", Sequence: 2}}
		}), reasonInvalidState, nil},
		{"clients out of order", edit(func(file *stateFile) {
			file.Sequences = []savedSequence{{Client: "d", Sequence: 1}, {Client: "c", Sequence: 2}}
		}), reasonInvalidState, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeCheckedState(tc.data)
			if reason := stateReason(err); reason != tc.reason {
				t.Fatalf("reason %q, want %q: %v", reason, tc.reason, err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("error %v does not wrap %v", err, tc.err)
			}
		})
	}
}

func TestDecodeStateFileAcceptsLimits(t *testing.T) {
	t.Parallel()
	valid := newTestStateFile(t)
	tests := []struct {
		name   string
		change func(*stateFile)
	}{
		{"epoch of 100 bytes", func(file *stateFile) { file.Epoch = strings.Repeat("E", 100) }},
		{"no build", func(file *stateFile) { file.Build = "" }},
		{"speed 8", func(file *stateFile) { file.Speed = 8 }},
		{"demand error of 1 KiB", func(file *stateFile) { file.Demand.State.Error = strings.Repeat("x", 1<<10) }},
		{"budget 0", func(file *stateFile) { file.Demand.Budget = 0 }},
		{"budget 3599", func(file *stateFile) { file.Demand.Budget = 3599 }},
		{"revision 0", func(file *stateFile) { file.Revision = 0 }},
		{"200 pods", func(file *stateFile) { file.Simulation.Pods = make([]sim.SavedPod, 200) }},
		{"1,024 client sequences", func(file *stateFile) { file.Sequences = testSequences(clientLimit) }},
		{"client ID of 100 bytes", func(file *stateFile) {
			file.Sequences = []savedSequence{{Client: strings.Repeat("c", maxClientBytes), Sequence: math.MaxUint64}}
		}},
		{"no client sequences", func(file *stateFile) { file.Sequences = nil }},
		{"destination demand", func(file *stateFile) {
			file.Demand.State.Config.Pattern, file.Demand.State.Config.Destination = "destination", "market"
		}},
		{"profile demand", func(file *stateFile) {
			file.Project.DemandProfiles = []project.DemandProfile{{
				ID: "p", Name: "P", Bands: []project.DemandBand{{ID: "b", Name: "B", DurationMinutes: 60}},
				Flows: []project.DemandFlow{{From: "harbor", To: "market", Weights: []float64{1}}},
			}}
			file.Demand.State.Config.Pattern, file.Demand.State.Config.Profile, file.Demand.State.Config.Band = "profile", "p", "b"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := valid
			tc.change(&want)
			got, err := decodeCheckedState(encodeTestState(t, want))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded state file differs:\ngot  %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestPrescanJSON(t *testing.T) {
	t.Parallel()
	limits := jsonLimits{depth: 3, elements: 4, members: 2}
	tests := []struct {
		name string
		data string
		// err is nil when the data is within the limits.
		err error
	}{
		{"within the limits", `{"a":[[1,2,3,4]],"b":{"c":[]}}`, nil},
		{"depth 4", `[[[[]]]]`, errJSONTooDeep},
		{"depth 4 in an object", `{"a":{"b":{"c":{}}}}`, errJSONTooDeep},
		{"array of 5 numbers", `[1,2,3,4,5]`, errJSONArrayTooLong},
		{"array of 5 arrays", `[[],[],[],[],[]]`, errJSONArrayTooLong},
		{"array of 5 objects", `[{},{},{},{},{}]`, errJSONArrayTooLong},
		{"inner array of 5", `{"a":[[1,2,3,4,5]]}`, errJSONArrayTooLong},
		{"object of 3 members", `{"a":1,"b":2,"c":3}`, errJSONObjectTooLong},
		{"object of 3 members with an object", `{"a":1,"b":2,"c":{}}`, errJSONObjectTooLong},
		{"duplicate names", `{"a":1,"a":2}`, nil},
		{"two values", `{} {}`, nil},
		{"cut short", `{"a":`, io.ErrUnexpectedEOF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := prescanJSON([]byte(tc.data), limits); !errors.Is(err, tc.err) {
				t.Fatalf("error %v, want %v", err, tc.err)
			}
		})
	}
}

func TestPrescanJSONPaths(t *testing.T) {
	t.Parallel()
	limits := jsonLimits{depth: 8, elements: 4, members: 4, arrays: map[string]int64{"/a": 2, "/b/*/c": 1, "/d/e": 3}}
	tests := []struct {
		name string
		data string
		// err is nil when the data is within the limits.
		err error
	}{
		{"path at its limit", `{"a":[1,2]}`, nil},
		{"path past its limit", `{"a":[1,2,3]}`, errJSONArrayTooLong},
		{"escaped name past its limit", `{"\u0061":[1,2,3]}`, errJSONArrayTooLong},
		{"second duplicate past its limit", `{"a":[1],"a":[1,2,3]}`, errJSONArrayTooLong},
		{"nested path past its limit", `{"d":{"e":[1,2,3,4]}}`, errJSONArrayTooLong},
		{"each index at its limit", `{"b":[{"c":[1]},{"c":[2]}]}`, nil},
		{"second index past its limit", `{"b":[{},{"c":[1,2]}]}`, errJSONArrayTooLong},
		{"other path at the element limit", `{"x":{"a":[1,2,3,4]}}`, nil},
		{"other path past the element limit", `{"x":[1,2,3,4,5]}`, errJSONArrayTooLong},
		{"member name that looks like an index", `{"b":{"0":{"c":[1,2]}}}`, nil},
		{"member name with a slash", `{"b/*/c":[1,2]}`, nil},
		{"array in an array", `{"a":[[1,2,3,4]]}`, nil},
		{"array at the top", `[{"a":[1,2,3]}]`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := prescanJSON([]byte(tc.data), limits); !errors.Is(err, tc.err) {
				t.Fatalf("error %v, want %v", err, tc.err)
			}
		})
	}
}

// TestStateJSONLimits checks that each array of a state file has its limit.
func TestStateJSONLimits(t *testing.T) {
	t.Parallel()
	limits := maps.Clone(stateJSONLimits.arrays)
	// The path of an array that a state file does not have.
	limits["/simulation/extra"] = stateJSONLimits.elements
	for path, limit := range limits {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			for count, want := range map[int64]error{limit: nil, limit + 1: errJSONArrayTooLong} {
				data := "[" + strings.Repeat("0,", int(count-1)) + "0]"
				tokens := strings.Split(path, "/")
				for _, token := range slices.Backward(tokens[1:]) {
					if token == "*" {
						data = "[" + data + "]"
					} else {
						data = `{"` + token + `":` + data + "}"
					}
				}
				if err := prescanJSON([]byte(data), stateJSONLimits); !errors.Is(err, want) {
					t.Fatalf("%d elements: error %v, want %v", count, err, want)
				}
			}
		})
	}
}

// bombInput describes a compressed JSON text of at most size bytes. The
// text has head, then as many items as fit, then tail. item appends the
// text of the item with an index to a slice. level is the gzip level.
type bombInput struct {
	head  string
	item  func([]byte, int) []byte
	tail  string
	size  int
	level int
}

// compressBomb returns the compressed text that input describes.
func compressBomb(t *testing.T, input bombInput) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buffer, input.level)
	if err != nil {
		t.Fatal(err)
	}
	chunk, next := []byte(input.head), []byte(nil)
	for index, written := 0, 0; ; index++ {
		next = input.item(next[:0], index)
		if written+len(chunk)+len(next)+len(input.tail) > input.size {
			break
		}
		if chunk = append(chunk, next...); len(chunk) >= 64<<10 {
			if _, err := zw.Write(chunk); err != nil {
				t.Fatal(err)
			}
			written += len(chunk)
			chunk = chunk[:0]
		}
	}
	if _, err := zw.Write(append(chunk, input.tail...)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestDecodeStateFileBombs decodes files that expand to very many values.
// It measures the heap, so it does not run in parallel, and it sets the
// default target of the garbage collector. The files with many members are
// not compressed, because compression takes too long with the race
// detector.
func TestDecodeStateFileBombs(t *testing.T) {
	defer debug.SetGCPercent(debug.SetGCPercent(100))
	const head = `{"format":"podsim-session","version":1`
	emptyPods := strings.Repeat(",{}", 1<<10)
	morePods := func(chunk []byte, _ int) []byte { return append(chunk, emptyPods...) }
	member := func(chunk []byte, index int) []byte {
		chunk = append(chunk, `,"m`...)
		chunk = strconv.AppendInt(chunk, int64(index), 10)
		return append(chunk, `":0`...)
	}
	pods := bombInput{
		head: head + `,"simulation":{"pods":[{}`, item: morePods, tail: "]}}",
		size: MaxStateBytes, level: gzip.BestSpeed,
	}
	past := pods
	past.size = 2 * MaxStateBytes
	// Stored blocks add 5 bytes to each 64 KiB, so the file stays below the
	// size limit.
	members := bombInput{head: head, item: member, tail: "}", size: MaxStateBytes - 4<<10, level: gzip.NoCompression}
	// repeated returns count copies of item, with commas between them.
	repeated := func(item string, count int) string { return strings.Repeat(item+",", count-1) + item }
	// arrayBomb returns a project-sized array of copies of item. Each item
	// holds an array past its limit. Without the path limits, such a file
	// decodes to hundreds of megabytes.
	arrayBomb := func(path, item, tail string) bombInput {
		return bombInput{
			head: head + path + item, item: func(chunk []byte, _ int) []byte { return append(append(chunk, ','), item...) },
			tail: tail, size: project.MaxFileBytes, level: gzip.BestSpeed,
		}
	}
	tests := []struct {
		name   string
		input  bombInput
		reason string
		err    error
	}{
		{"pods up to the size limit", pods, reasonInvalidState, errJSONArrayTooLong},
		{"pods to twice the size limit", past, reasonTooLarge, ErrStateTooLarge},
		{"members up to the size limit", members, reasonInvalidState, errJSONObjectTooLong},
		{"stations with many berths", arrayBomb(
			`,"project":{"network":{"Stations":[`, `{"Berths":[`+repeated("{}", 32_768)+"]}", "]}}}",
		), reasonInvalidState, errJSONArrayTooLong},
		{"profiles with many flows", arrayBomb(
			`,"project":{"demandProfiles":[`, `{"flows":[`+repeated("{}", 32_768)+"]}", "]}}",
		), reasonInvalidState, errJSONArrayTooLong},
		{"pods with long routes", arrayBomb(
			`,"simulation":{"pods":[`, `{"route":[`+repeated("0", 32_768)+"]}", "]}}",
		), reasonInvalidState, errJSONArrayTooLong},
		{"trips with long routes", arrayBomb(
			`,"simulation":{"waiting":[`, `{"route":[`+repeated("0", 32_768)+"]}", "]}}",
		), reasonInvalidState, errJSONArrayTooLong},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := compressBomb(t, tc.input)
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := decodeStateFile(data)
			runtime.ReadMemStats(&after)
			if reason := stateReason(err); reason != tc.reason {
				t.Fatalf("reason %q, want %q: %v", reason, tc.reason, err)
			}
			if !errors.Is(err, tc.err) {
				t.Fatalf("error %v does not wrap %v", err, tc.err)
			}
			growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
			t.Logf("%d compressed bytes, heap growth %d bytes", len(data), growth)
			if growth >= 64<<20 {
				t.Fatalf("the heap grew by %d bytes", growth)
			}
		})
	}
}

// TestStateFileWorstCaseSize finds the size of the largest state file that
// the encoder writes. The project has the most nodes and lanes that
// project.Validate allows, and its JSON form has project.MaxFileBytes, the
// largest project member that the encoder writes. There are maxSavedPods
// pods, each with a route of the largest saved length. There are
// maxSavedTrips queued trips, each with a route of the largest saved length.
// There are clientLimit client sequences, and each client ID has the
// longest JSON form. Each other value has its largest length. The size must
// be accepted, so that a save fails only when its project member is too
// large.
func TestStateFileWorstCaseSize(t *testing.T) {
	t.Parallel()
	const nodes, lanes = project.MaxNodes, project.MaxLanes
	id := func(prefix string, index int) string {
		return prefix + strings.Repeat("0", 64-len(prefix)-len(strconv.Itoa(index))) + strconv.Itoa(index)
	}
	config := project.Default()
	config.Network.Nodes = make([]sim.Node, nodes)
	for index := range config.Network.Nodes {
		config.Network.Nodes[index] = sim.Node{ID: id("n", index)}
	}
	config.Network.Lanes = make([]sim.Lane, lanes)
	for index := range config.Network.Lanes {
		config.Network.Lanes[index] = sim.Lane{ID: id("l", index), From: id("n", index%nodes), To: id("n", (index+1)%nodes)}
	}
	// The project member can hold project.MaxFileBytes, whatever it
	// contains, so a long name fills the rest.
	config.Name = strings.Repeat("n", project.MaxFileBytes-jsonSize(t, config)+len(config.Name))
	if size := jsonSize(t, config); size != project.MaxFileBytes {
		t.Fatalf("project has %d bytes, want %d", size, project.MaxFileBytes)
	}

	const widest = math.MinInt64
	text := strings.Repeat("x", 1<<10)
	route := func(length int) []int {
		indexes := make([]int, length)
		for index := range indexes {
			indexes[index] = lanes - 1
		}
		return indexes
	}
	request := sim.SavedRequest{
		ID: widest, From: id("f", 0), To: id("t", 0), PartySize: widest, PodID: id("p", 0),
		Completed: true, RequestedTick: widest, DispatchReason: text,
	}
	pod := sim.SavedPod{
		ID: id("p", 0), Activity: "departing", StationID: id("s", 0), BerthID: id("b", 0),
		Occupied: true, Request: &request, Parties: widest, RelocatingTo: id("r", 0),
		Rebalancing: true, RebalanceAfter: widest, PhaseTicks: widest, Origin: id("o", 0),
		Destination: id("d", 0), DestinationStation: id("e", 0), ClaimsDestination: true,
		Route: route(lanes + nodes), RouteIndex: widest, LaneID: id("l", 0),
		LaneDistance: -math.MaxFloat64, Distance: -math.MaxFloat64, Waiting: true, WaitSince: widest,
	}
	trip := sim.SavedTrip{
		Request: request, Route: route(nodes), Parties: widest,
		DeferUntil: widest, DeferCheck: widest, DeferPodID: id("p", 0),
	}
	// A client ID of control characters has the longest JSON form, 6 bytes
	// for each byte. The last 3 bytes make the IDs increase.
	sequences := make([]savedSequence, clientLimit)
	for index := range sequences {
		suffix := string([]byte{byte(0x10 + index/256), byte(0x10 + index/16%16), byte(0x10 + index%16)})
		sequences[index] = savedSequence{
			Client: strings.Repeat("\x01", maxClientBytes-len(suffix)) + suffix, Sequence: math.MaxUint64,
		}
	}
	demand := config.Demand
	demand.Enabled, demand.PerMinute, demand.Seed = true, 120, math.MaxUint64
	demand.Destination, demand.Profile, demand.Band = id("d", 0), id("p", 0), id("b", 0)
	random, err := newDemand(demandInput{config: demand, network: config.Network}).pcg.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	file := stateFile{
		Format: stateFormat, Version: stateVersion, Final: true,
		SavedAt: time.Date(2026, time.September, 23, 9, 0, 0, 123456789, time.FixedZone("", -12*60*60)),
		Build:   testBuildID, Epoch: strings.Repeat("E", maxEpochBytes),
		Revision: math.MaxUint64 - 1, ProjectRevision: math.MaxUint64 - 1, Generation: math.MaxUint64 - 1,
		LastCheckpoint: math.MaxUint64, Speed: 8, RestoreAttempts: math.MaxInt, Sequences: sequences,
		Demand: savedDemand{
			State:  DemandState{Config: demand, Generated: math.MaxInt, Skipped: math.MaxInt, Error: text},
			Random: random, Budget: demandBudgetLimit - 1,
		},
		Simulation: sim.SavedState{
			Tick: widest, Paused: true, Completed: widest, RequestID: widest, Boarded: widest,
			TotalWaitTicks: widest, MaxWaitTicks: widest, NextRedistributionTick: widest,
			PassengerDistanceMeters: -math.MaxFloat64, EmptyDistanceMeters: -math.MaxFloat64,
			RebalanceMoves: widest, SharedParties: widest, SharedRidePartyLimit: widest,
			Demo: &sim.SavedDemo{SecondSent: true, FollowupsSent: true}, DemoError: text,
			Pods: []sim.SavedPod{pod}, Waiting: []sim.SavedTrip{trip},
		},
		Project: config,
	}
	// A file with all pods and trips takes too long to encode with the race
	// detector. The file has one of each. A comma separates the elements of
	// an array, so each other pod or trip adds its size and 1.
	size := jsonSize(t, file) + (maxSavedPods-1)*(jsonSize(t, pod)+1) + (maxSavedTrips-1)*(jsonSize(t, trip)+1)
	t.Logf("worst case: %d JSON bytes, limit %d", size, MaxStateBytes)
	if size > MaxStateBytes {
		t.Fatalf("the largest state has %d JSON bytes, more than %d", size, MaxStateBytes)
	}
	if _, err := decodeCheckedState(encodeTestState(t, file)); err != nil {
		t.Fatal(err)
	}
}

// jsonSize returns the size of the state file encoding of value.
func jsonSize(t *testing.T, value any) int {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	return len(data)
}

func TestEncodeStateFileTooLarge(t *testing.T) {
	t.Parallel()
	file := newTestStateFile(t)
	// A project file can hold 1e20, and the project member holds its 21
	// digits. The project is not valid, but the encoder does not check it.
	const weights = 200_000
	var profile project.DemandProfile
	text := `{"id":"p","name":"P","flows":[{"from":"harbor","to":"market","weights":[` +
		strings.Repeat("1e20,", weights-1) + `1e20]}]}`
	if err := json.Unmarshal([]byte(text), &profile); err != nil {
		t.Fatal(err)
	}
	if len(text) > project.MaxFileBytes || weights*len("100000000000000000000,") <= project.MaxFileBytes {
		t.Fatalf("the profile has %d bytes, and it does not grow past %d bytes", len(text), project.MaxFileBytes)
	}
	tests := []struct {
		name   string
		change func(*stateFile)
		// err is nil or a second error that the result must wrap.
		err error
	}{
		{"demo error", func(file *stateFile) { file.Simulation.DemoError = strings.Repeat("x", MaxStateBytes) }, nil},
		{"project in long form", func(file *stateFile) {
			file.Project.DemandProfiles = []project.DemandProfile{profile}
		}, errProjectTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var encoder stateEncoder
			large := file
			tc.change(&large)
			_, err := encoder.encode(large)
			if !errors.Is(err, ErrStateTooLarge) || tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatalf("error %v, want %v", err, cmp.Or(tc.err, ErrStateTooLarge))
			}
			// The encoder works again after the failure.
			data, err := encoder.encode(file)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeStateFile(data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestStateFileMembers makes sure that each change to the members of the
// state file is seen. Such a change needs a new stateVersion. Run the test
// with -update to write the member list again.
func TestStateFileMembers(t *testing.T) {
	t.Parallel()
	got := stateMembers(t, "", reflect.TypeFor[stateFile](), nil)
	if *update {
		if err := os.WriteFile(stateMembersPath, []byte(stateMembersHeader+strings.Join(got, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(stateMembersPath)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for line := range strings.Lines(string(data)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			want = append(want, line)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the state file members changed. Change stateVersion, then run the test with -update.\ngot:\n%s", strings.Join(got, "\n"))
	}
}

// stateMembers returns a line for each JSON member below path, in field
// order. Each line has the member path and the JSON kind. parents holds the
// struct types that contain typ.
func stateMembers(t *testing.T, path string, typ reflect.Type, parents []reflect.Type) []string {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ {
	case reflect.TypeFor[time.Time]():
		return []string{path + " time"}
	case reflect.TypeFor[[]byte]():
		return []string{path + " base64"}
	}
	switch typ.Kind() {
	case reflect.Bool:
		return []string{path + " boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return []string{path + " number"}
	case reflect.String:
		return []string{path + " string"}
	case reflect.Slice, reflect.Array:
		return append([]string{path + " array"}, stateMembers(t, path+"[]", typ.Elem(), parents)...)
	case reflect.Struct:
		if slices.Contains(parents, typ) {
			t.Fatalf("%s: %v contains itself", path, typ)
		}
		var lines []string
		if path != "" {
			lines = append(lines, path+" object")
			path += "."
		}
		parents = append(parents, typ)
		for field := range typ.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if !field.IsExported() || name == "-" {
				continue
			}
			if field.Anonymous {
				t.Fatalf("%s%s: the member list does not support embedded fields", path, field.Name)
			}
			lines = append(lines, stateMembers(t, path+cmp.Or(name, field.Name), field.Type, parents)...)
		}
		return lines
	default:
		t.Fatalf("%s: the member list does not support %v", path, typ)
		return nil
	}
}
