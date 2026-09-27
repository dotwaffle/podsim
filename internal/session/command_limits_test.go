package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/sim"
)

// TestCommandLimitsCoverEachArray checks that commandJSONLimits has a limit
// for each array of a command. An array without a limit gets the limit 0,
// so a new array field needs a limit.
func TestCommandLimitsCoverEachArray(t *testing.T) {
	t.Parallel()
	var paths []string
	var walk func(typ reflect.Type, path string)
	walk = func(typ reflect.Type, path string) {
		switch typ.Kind() {
		case reflect.Pointer:
			walk(typ.Elem(), path)
		case reflect.Slice:
			paths = append(paths, path)
			walk(typ.Elem(), path+"/*")
		case reflect.Struct:
			for field := range typ.Fields() {
				name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
				if name == "" {
					name = field.Name
				}
				walk(field.Type, path+"/"+name)
			}
		default:
		}
	}
	walk(reflect.TypeFor[Command](), "")
	if len(paths) != len(commandJSONLimits.arrays) {
		t.Errorf("a command has %d arrays, and the limits have %d: %v", len(paths), len(commandJSONLimits.arrays), paths)
	}
	for _, path := range paths {
		if _, ok := commandJSONLimits.arrays[path]; !ok {
			t.Errorf("no limit for the array at %s", path)
		}
	}
}

// postCommand sends body to the command endpoint of handler.
func postCommand(t *testing.T, handler http.Handler, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/command", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// pauseWithLanes returns the body of a pause command whose project has lanes
// empty lanes under the member name lanesName.
func pauseWithLanes(t *testing.T, s *Session, client, lanesName string, lanes int) []byte {
	t.Helper()
	head, err := json.Marshal(Command{Client: client, Sequence: 1, Epoch: s.State().Epoch, Action: "pause", Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	body := slices.Clone(head[:len(head)-1])
	body = append(body, `,"project":{"network":{"`+lanesName+`":[`...)
	if lanes > 0 {
		body = append(body, "{}"...)
		body = append(body, strings.Repeat(",{}", lanes-1)...)
	}
	return append(body, "]}}}"...)
}

// pauseWithOrigin returns the body of a pause command with an origin of
// length bytes.
func pauseWithOrigin(t *testing.T, s *Session, client string, length int) []byte {
	t.Helper()
	body, err := json.Marshal(Command{Client: client, Sequence: 1, Epoch: s.State().Epoch, Action: "pause", Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	return append(body[:len(body)-1], `,"origin":"`+strings.Repeat("o", length)+`"}`...)
}

// TestCommandShapeLimits checks that the server rejects a command with an
// array over its limit before it decodes the command, also for an action
// that does not use the project. It measures the memory that the request
// allocates. It does not run in parallel, because it measures the heap.
func TestCommandShapeLimits(t *testing.T) {
	s := newTestSession(t)
	handler := s.Handler(t.TempDir())
	fill := (maxCommandBytes - len(pauseWithLanes(t, s, "fill", "Lanes", 0))) / 3
	tests := []struct {
		name  string
		body  []byte
		code  int
		reply string
	}{
		{"1,000,000 lanes", pauseWithLanes(t, s, "million", "Lanes", 1_000_000), http.StatusBadRequest, "invalid command JSON\n"},
		{"lanes up to the body limit", pauseWithLanes(t, s, "fill", "Lanes", fill), http.StatusBadRequest, "invalid command JSON\n"},
		// The scan goes on past the invalid string to the lanes.
		{"invalid UTF-8 before the lanes", bytes.Replace(pauseWithLanes(t, s, "utf8", "Lanes", fill), []byte(`"utf8"`), []byte("\"\xff\""), 1), http.StatusBadRequest, "invalid command JSON\n"},
		{"one lane over the limit", pauseWithLanes(t, s, "over", "Lanes", project.MaxLanes+1), http.StatusBadRequest, "invalid command JSON\n"},
		{"a member name in another case", pauseWithLanes(t, s, "case", "lANES", project.MaxLanes+1), http.StatusBadRequest, "invalid command JSON\n"},
		{"lanes at the limit", pauseWithLanes(t, s, "limit", "Lanes", project.MaxLanes), http.StatusOK, ""},
		{"an array at another path", []byte(`{"client":"other","sequence":1,"action":["pause"]}`), http.StatusBadRequest, "invalid command JSON\n"},
		{"a second value that is not JSON", append(pauseWithLanes(t, s, "second", "Lanes", 1), " x"...), http.StatusBadRequest, "send one command only\n"},
		// The limits count the quotes of a string.
		{"a string over the limit", pauseWithOrigin(t, s, "long", commandJSONLimits.stringBytes-1), http.StatusBadRequest, "invalid command JSON\n"},
		{"a string at the limit", pauseWithOrigin(t, s, "string", commandJSONLimits.stringBytes-2), http.StatusOK, ""},
		{"a string of escapes over the limit", bytes.Replace(pauseWithOrigin(t, s, "escapes", 0), []byte(`"origin":""`), []byte(`"origin":"`+strings.Repeat(`\u0078`, 200)+`"`), 1), http.StatusBadRequest, "invalid command JSON\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			recorder := postCommand(t, handler, tc.body)
			runtime.GC()
			runtime.ReadMemStats(&after)
			if recorder.Code != tc.code || (tc.reply != "" && recorder.Body.String() != tc.reply) {
				t.Fatalf("status %d, reply %q, want %d and %q", recorder.Code, recorder.Body.String(), tc.code, tc.reply)
			}
			// The body is at most maxCommandBytes. Reading it, and the
			// buffer of the scan, allocate a few times that. The decoded
			// lanes of the largest body need more than 70 MB.
			allocated := after.TotalAlloc - before.TotalAlloc
			t.Logf("%d bytes, allocated %d bytes", len(tc.body), allocated)
			if allocated >= 8*maxCommandBytes {
				t.Fatalf("the request allocated %d bytes", allocated)
			}
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.receipts) != 2 {
		t.Fatalf("the session has %d receipts, want 2 for the commands at the limits", len(s.receipts))
	}
}

// TestCommandInvalidUTF8 checks that a command with invalid UTF-8 gets the
// same reply as before the shape limits.
func TestCommandInvalidUTF8(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	body := fmt.Appendf(nil, `{"client":"c\xff","sequence":1,"epoch":%q,"action":"pause","paused":true}`, s.State().Epoch)
	recorder := postCommand(t, s.Handler(t.TempDir()), body)
	if recorder.Code != http.StatusBadRequest || recorder.Body.String() != "invalid command JSON\n" {
		t.Fatalf("status %d, reply %q", recorder.Code, recorder.Body.String())
	}
}

// TestReceiptsKeepNoProject sends a large project command from each of 16
// clients. The session is not paused, so it rejects each command. Its
// receipts must not keep the projects.
func TestReceiptsKeepNoProject(t *testing.T) {
	s := newTestSession(t)
	handler := s.Handler(t.TempDir())
	config := scenarios.London()
	bodies := make([][]byte, 16)
	for index := range bodies {
		body, err := json.Marshal(Command{
			Client: fmt.Sprintf("client-%d", index), Sequence: 1, Epoch: s.State().Epoch,
			Action: "project", Project: &config, ProjectRevision: s.State().ProjectRevision,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(body) > maxCommandBytes {
			t.Fatalf("the body has %d bytes, more than %d", len(body), maxCommandBytes)
		}
		bodies[index] = body
	}
	// Two collections also free the buffers that sync.Pool keeps.
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	for _, body := range bodies {
		if recorder := postCommand(t, handler, body); recorder.Code != http.StatusConflict {
			t.Fatalf("status %d, reply %q, want a rejected project command", recorder.Code, recorder.Body.String())
		}
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	// Receipts that keep the commands would retain about 28 MB here, a
	// quarter of the 112.8 MB measured with 64 clients. A receipt keeps
	// the sequence, the digest and the reply.
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("16 rejected project commands of %d bytes retain %d bytes", len(bodies[0]), retained)
	if retained >= 1<<20 {
		t.Fatalf("16 receipts retain %d bytes", retained)
	}
	// The session holds the receipts, so it must stay alive to the
	// measurement.
	runtime.KeepAlive(s)
	runtime.KeepAlive(bodies)
	runtime.KeepAlive(config)
}

// TestRetryMatchesTheCommandContent checks the retry rules. A retry with the
// same sequence and equal content gets the stored reply. A retry with other
// content gets SequenceConflict, also when only the project of an action
// that does not use it is different.
func TestRetryMatchesTheCommandContent(t *testing.T) {
	t.Parallel()
	s := newTestSession(t)
	config := project.Default()
	command := commandFor(s, "pause")
	command.Paused, command.Project = true, &config
	first := s.Apply(command)
	if first.Error != "" {
		t.Fatal(first.Error)
	}
	same := command
	same.Project = new(project.Clone(config))
	if reply := s.Apply(same); !reflect.DeepEqual(reply, first) {
		t.Fatalf("retry = %+v, want %+v", reply, first)
	}
	other := project.Clone(config)
	other.Name += " changed"
	changed := command
	changed.Project = &other
	if reply := s.Apply(changed); reply.ErrorCode != SequenceConflict {
		t.Fatalf("retry with another project = %+v, want %s", reply, SequenceConflict)
	}
	none := command
	none.Project = nil
	if reply := s.Apply(none); reply.ErrorCode != SequenceConflict {
		t.Fatalf("retry without the project = %+v, want %s", reply, SequenceConflict)
	}
}

// TestDigestMatchesDeepEqual checks that two commands have matching digests
// exactly when reflect.DeepEqual reports them equal.
func TestDigestMatchesDeepEqual(t *testing.T) {
	t.Parallel()
	base := func() Command {
		config := project.Default()
		return Command{Client: "c", Sequence: 1, Action: "project", Project: &config}
	}
	tests := []struct {
		name string
		// change changes the two commands.
		change func(first, second *Command)
	}{
		{"same content", func(_, _ *Command) {}},
		{"other client", func(_, second *Command) { second.Client = "d" }},
		{"text moved between fields", func(_, second *Command) { second.Client, second.Action = "cp", "roject" }},
		{"no project", func(_, second *Command) { second.Project = nil }},
		{"negative zero", func(first, second *Command) {
			first.Project.Network.Nodes[0].Position.X = 0
			second.Project.Network.Nodes[0].Position.X = math.Copysign(0, -1)
		}},
		{"NaN in one", func(_, second *Command) { second.Project.Network.Lanes[0].SpeedLimit = math.NaN() }},
		{"NaN in both", func(first, second *Command) {
			first.Project.Network.Lanes[0].SpeedLimit = math.NaN()
			second.Project.Network.Lanes[0].SpeedLimit = math.NaN()
		}},
		{"empty in place of nil", func(first, second *Command) {
			first.Project.DemandProfiles = nil
			second.Project.DemandProfiles = []project.DemandProfile{}
		}},
		{"curve", func(_, second *Command) { second.Project.Network.Lanes[0].Control = &sim.Point{} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, second := base(), base()
			tc.change(&first, &second)
			want := reflect.DeepEqual(first, second)
			if got := digestCommand(first).matches(digestCommand(second)); got != want {
				t.Fatalf("digests match = %v, reflect.DeepEqual = %v", got, want)
			}
		})
	}
}

// TestReceiptsKeepShortErrors applies a project command with a demand
// profile ID of 3 MiB from each of 16 clients to a paused session. The
// body limits reject such a command before it gets to the session, but the
// session must not depend on them. Project validation rejects each
// command, and the receipts must not keep the ID.
func TestReceiptsKeepShortErrors(t *testing.T) {
	s := newTestSession(t)
	pause := commandFor(s, "pause")
	pause.Paused = true
	if reply := s.Apply(pause); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	config := project.Default()
	config.DemandProfiles = []project.DemandProfile{{ID: strings.Repeat("p", 3<<20), Name: "Long"}}
	commands := make([]Command, 16)
	for index := range commands {
		commands[index] = Command{
			Client: fmt.Sprintf("client-%d", index), Sequence: 1, Epoch: s.State().Epoch,
			Action: "project", Project: &config, ProjectRevision: s.State().ProjectRevision,
		}
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	for _, command := range commands {
		reply := s.Apply(command)
		if reply.ErrorCode != CommandRejected || len(reply.Error) > maxErrorBytes {
			t.Fatalf("reply %s with an error of %d bytes, want %s", reply.ErrorCode, len(reply.Error), CommandRejected)
		}
	}
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&after)
	// Errors that quote the ID would retain about 48 MiB.
	retained := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("16 rejected project commands with an ID of %d bytes retain %d bytes", len(config.DemandProfiles[0].ID), retained)
	if retained >= 1<<20 {
		t.Fatalf("16 receipts retain %d bytes", retained)
	}
	runtime.KeepAlive(s)
	runtime.KeepAlive(commands)
	runtime.KeepAlive(config)
}

// TestRejectCutsLongErrors checks that a reply keeps at most maxErrorBytes
// bytes of an error. No error of a command quotes a long value now, but a
// future error can.
func TestRejectCutsLongErrors(t *testing.T) {
	t.Parallel()
	var reply Reply
	reply.reject(CommandRejected, strings.Repeat("x", 1<<20))
	if len(reply.Error) != maxErrorBytes || !strings.HasSuffix(reply.Error, "...") {
		t.Fatalf("the error has %d bytes and ends with %q", len(reply.Error), reply.Error[len(reply.Error)-3:])
	}
	for _, message := range []string{"", "short", strings.Repeat("x", maxErrorBytes), strings.Repeat("é", maxErrorBytes), strings.Repeat("\x80", 2*maxErrorBytes)} {
		got := truncateError(message)
		switch {
		case len(message) <= maxErrorBytes:
			if got != message {
				t.Errorf("truncateError changed a message of %d bytes", len(message))
			}
		case len(got) > maxErrorBytes || len(got) < maxErrorBytes-utf8.UTFMax:
			t.Errorf("truncateError kept %d bytes of %d", len(got), len(message))
		case utf8.ValidString(message) && !utf8.ValidString(got):
			t.Errorf("truncateError cut a rune")
		}
	}
}

// TestProjectWithManyLanesAtOneNode sends a project in which all added lanes
// join the same two nodes. Without the lane limit of a node, the fleet
// would compare each pair of these lanes at each node, and keep an entry for
// each pair. Validation rejects the project first, so the request allocates
// little memory. It does not run in parallel, because it measures the heap.
func TestProjectWithManyLanesAtOneNode(t *testing.T) {
	s := newTestSession(t)
	handler := s.Handler(t.TempDir())
	pause := commandFor(s, "pause")
	pause.Paused = true
	if reply := s.Apply(pause); reply.Error != "" {
		t.Fatal(reply.Error)
	}
	config := project.Default()
	config.Network.Nodes = append(config.Network.Nodes,
		sim.Node{ID: "a", Position: sim.Point{X: 20_000, Y: 20_000}}, sim.Node{ID: "b", Position: sim.Point{X: 20_030, Y: 20_000}})
	for index := len(config.Network.Lanes); index < project.MaxLanes; index++ {
		config.Network.Lanes = append(config.Network.Lanes, sim.Lane{ID: fmt.Sprintf("parallel-%d", index), From: "a", To: "b", SpeedLimit: 12})
	}
	body, err := json.Marshal(Command{
		Client: "lanes", Sequence: 1, Epoch: s.State().Epoch, Action: "project", Project: &config, ProjectRevision: s.State().ProjectRevision,
	})
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	recorder := postCommand(t, handler, body)
	runtime.ReadMemStats(&after)
	var reply Reply
	if err := json.Unmarshal(recorder.Body.Bytes(), &reply); err != nil {
		t.Fatalf("status %d, reply %q: %v", recorder.Code, recorder.Body.String(), err)
	}
	if recorder.Code != http.StatusConflict || reply.ErrorCode != CommandRejected || !strings.Contains(reply.Error, `node "a" has`) {
		t.Fatalf("status %d, reply %+v, want a rejection for node a", recorder.Code, reply)
	}
	// The fleet would allocate more than 1 GiB for the pairs of lanes.
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("a project of %d bytes with %d lanes at one node allocated %d bytes", len(body), len(config.Network.Lanes), allocated)
	if allocated >= 64<<20 {
		t.Fatalf("the request allocated %d bytes", allocated)
	}
}
