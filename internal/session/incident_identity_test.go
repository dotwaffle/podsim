package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"maps"
	"math"
	"runtime"
	"slices"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/scenarios"
)

// incidentIdentityTicks is the length of each run of the rate-0 identity,
// and incidentIdentityFrame the ticks between two frames that it encodes.
const (
	incidentIdentityTicks = 6000
	incidentIdentityFrame = 600
)

// TestIncidentRateZeroIdentity checks section 5.1.5 of the incident stage 7
// contract. Each of the four forms of a rate of 0 gives the digest that the
// commit before the stage 7 sampler gave, on Busy and on the rail hub, with
// no command and with a script of fault, debris, emergency, and clear
// commands. The digest covers the full and the delta frames, and the saved
// state without its version and project members: the version can move, and
// the project member keeps its own bytes.
//
// On each CPU type, the four forms give one digest and write no sampler
// member. A run is deterministic on one architecture only (section 5.1.4), so
// the pinned digests hold on amd64.
func TestIncidentRateZeroIdentity(t *testing.T) {
	t.Parallel()
	negativeZero := math.Copysign(0, -1)
	forms := []struct {
		name        string
		faults      *project.FaultConfig
		emergencies *project.EmergencyConfig
	}{
		{"absent", nil, nil},
		{"empty", &project.FaultConfig{}, &project.EmergencyConfig{}},
		{"zero", &project.FaultConfig{PerHour: new(0.0)}, &project.EmergencyConfig{PerHour: new(0.0)}},
		{"negative zero", &project.FaultConfig{PerHour: &negativeZero}, &project.EmergencyConfig{PerHour: &negativeZero}},
	}
	for _, test := range []struct {
		name   string
		preset func() project.Config
		script bool
		want   string
	}{
		{"busy", scenarios.Busy, false, "69ec6d63a90e9029b1c00b48c9aa78488f7c744c058eb5eb67b8b50f5140884f"},
		{"busy commands", scenarios.Busy, true, "a3a436c543779b58eb465a812a6b2230804937e29516e81f988f196271830b7f"},
		{"rail hub", scenarios.RailHub, false, "2175bf81addb819968e90c5d9e9f2898b5aa5c356b7bd72b14597003512d1d7c"},
		{"rail hub commands", scenarios.RailHub, true, "cbe67983b4be33ef21302d18e1984b5ae80968566979157537ba822bcf2759ab"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			first := ""
			for _, form := range forms {
				config := test.preset()
				config.Demand.Enabled = true
				config.Faults, config.Emergencies = form.faults, form.emergencies
				got := incidentIdentityDigest(t, config, test.script)
				if first == "" {
					first = got
				}
				if got != first {
					t.Errorf("%s: digest %s, want the digest %s of %s", form.name, got, first, forms[0].name)
				}
				if identityDigestGolden() && got != test.want {
					t.Errorf("%s: digest %s, want %s", form.name, got, test.want)
				}
			}
		})
	}
}

// identityDigestGolden reports whether the pinned digests hold on this CPU
// type (section 5.1.4 of the incident stage 7 contract).
func identityDigestGolden() bool {
	return runtime.GOARCH == "amd64"
}

// identityForbiddenMembers are the member names of stage 7 that a run at rate
// 0 never writes in a save or a frame (section 5.1.5 of the incident stage 7
// contract).
var identityForbiddenMembers = []string{"random", "nextTick", "sampled", "skipped", "clearedTicks", "delayedRiderTicks", "endedTicks", "maxTicks"}

// checkIdentityGroup fails the test when the JSON of a faults or an
// emergencies group has a member of stage 7.
func checkIdentityGroup(t *testing.T, where string, group []byte) {
	t.Helper()
	for _, name := range identityForbiddenMembers {
		if bytes.Contains(group, []byte(`"`+name+`"`)) {
			t.Fatalf("%s at rate 0 has the member %s: %s", where, name, group)
		}
	}
}

// checkIdentityFrame checks both incident groups of a frame.
func checkIdentityFrame(t *testing.T, frame StreamFrame) {
	t.Helper()
	simulation := frame.State.Simulation
	for name, group := range map[string]any{"frame faults": simulation.Faults, "frame emergencies": simulation.Emergencies} {
		data, err := json.Marshal(group)
		if err != nil {
			t.Fatal(err)
		}
		checkIdentityGroup(t, name, data)
	}
}

// checkIdentitySave checks both incident groups of a saved simulation. A run
// with no command writes neither group, because it has no record and no
// counter.
func checkIdentitySave(t *testing.T, simulation jsontext.Value, script bool) {
	t.Helper()
	var members map[string]jsontext.Value
	if err := json.Unmarshal(simulation, &members); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"faults", "emergencies"} {
		group, found := members[name]
		if found && !script {
			t.Fatalf("a run with no command has the saved member %s: %s", name, group)
		}
		checkIdentityGroup(t, "saved "+name, group)
	}
}

// incidentIdentityDigest runs a session of config and returns the digest of
// its frames and its final saved state.
func incidentIdentityDigest(t *testing.T, config project.Config, script bool) string {
	t.Helper()
	s, err := NewWithProject(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	s.simulation.SetPaused(false)
	client := newTestClient(s, "identity")
	digest := sha256.New()
	base := sessionFrame(t, s)
	for tick := 1; tick <= incidentIdentityTicks; tick++ {
		s.advance()
		if script {
			runIdentityScript(t, client, config, tick)
		}
		if tick%incidentIdentityFrame != 0 {
			continue
		}
		frame := sessionFrame(t, s)
		checkIdentityFrame(t, frame)
		for _, kind := range []string{"full", "delta"} {
			digest.Write(faultEnvelopeJSON(t, identityFrame(base), identityFrame(frame), kind))
		}
		base = frame
	}
	if state := s.simulation.ExportState(); state.Tick != incidentIdentityTicks || state.RequestID == 0 {
		t.Fatalf("the run did not advance with demand: tick %d, orders %d", state.Tick, state.RequestID)
	}
	saved, err := decompress(bytes.NewReader(encodeTestState(t, identityFile(t, s))))
	if err != nil {
		t.Fatal(err)
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(saved, &members); err != nil {
		t.Fatal(err)
	}
	checkIdentitySave(t, members["simulation"], script)
	delete(members, "version")
	delete(members, "project")
	for _, name := range slices.Sorted(maps.Keys(members)) {
		digest.Write([]byte(name))
		digest.Write(members[name])
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// identityFrame returns frame with the session identity fixed.
func identityFrame(frame StreamFrame) StreamFrame {
	frame = ownStreamBoardings(frame)
	frame.State.ServerStart, frame.State.Epoch, frame.State.Build = "identity-start", "identity-epoch", "identity-build"
	return frame
}

// runIdentityScript applies the commands of the tick: debris and a clear of
// it, an emergency, a pod fault and a clear of it, and a pod fault that its
// duration ends. Each start takes the first target that the session accepts.
func runIdentityScript(t *testing.T, client *testClient, config project.Config, tick int) {
	t.Helper()
	pods := func(command Command) []Command {
		commands := make([]Command, 0, len(config.Fleet))
		for _, placement := range config.Fleet {
			command.PodID = placement.ID
			commands = append(commands, command)
		}
		return commands
	}
	switch tick {
	case 600:
		var commands []Command
		for _, lane := range config.Network.Lanes {
			commands = append(commands, Command{Action: "fault", LaneID: lane.ID, FromMeters: new(1.0), ToMeters: new(3.0)})
		}
		identityStart(t, client, commands)
	case 1200:
		identityStart(t, client, pods(Command{Action: "emergency"}))
	case 1800:
		identityStart(t, client, pods(Command{Action: "fault"}))
	case 2400, 3000:
		client.mustApply(t, Command{Action: "clearFault", FaultID: identityActiveFault(client)})
	case 3300:
		identityStart(t, client, pods(Command{Action: "fault", DurationSeconds: new(int64(10))}))
	}
}

// identityStart applies the commands in order until the session accepts one.
func identityStart(t *testing.T, client *testClient, commands []Command) {
	t.Helper()
	for _, command := range commands {
		if reply := client.session.Apply(client.next(command)); reply.Error == "" {
			return
		}
	}
	t.Fatalf("the session refused each %s command", commands[0].Action)
}

// identityActiveFault returns the ID of the oldest active fault.
func identityActiveFault(client *testClient) string {
	return client.session.State().Simulation.Faults.Active[0].ID
}
