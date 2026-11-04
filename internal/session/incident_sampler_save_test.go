package session

import (
	"bytes"
	"encoding/base64"
	"errors"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// samplerRandom returns a valid saved state of a sampler stream.
func samplerRandom(t *testing.T, seed uint64) []byte {
	t.Helper()
	random, err := rand.NewPCG(seed, seed+1).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return random
}

// faultCountersForTest and emergencyCountersForTest have a different value in
// each counter, so a swap of two counters fails.
func faultCountersForTest() sim.FaultCounters {
	return sim.FaultCounters{Started: 1, Cleared: 2, Evacuations: 3, Reroutes: 4, FaultWaitTicks: 5, Sampled: 6, Skipped: 7, ClearedTicks: 8, DelayedRiderTicks: 9}
}

func emergencyCountersForTest() sim.EmergencyCounters {
	return sim.EmergencyCounters{Started: 11, Ended: 12, EmergencyTicks: 13, Sampled: 14, Skipped: 15, EndedTicks: 16, MaxTicks: 17}
}

// TestSamplerSaveMembers checks the saved members of section 4.2 of the
// incident stage 7 contract: the bytes of each group, the round trip with a
// different value in each counter, and a group with only the sampler members,
// which is not empty.
func TestSamplerSaveMembers(t *testing.T) {
	t.Parallel()
	file := faultMemberFile(t)
	random, other := samplerRandom(t, 1), samplerRandom(t, 3)
	file.Simulation.Faults.Counters, file.Simulation.Faults.Random, file.Simulation.Faults.NextTick = faultCountersForTest(), random, new(int64(1234))
	file.Simulation.Emergencies = &sim.SavedEmergencies{Counters: emergencyCountersForTest(), Random: other, NextTick: new(int64(5678))}
	raw := decompressTestJSON(t, encodeTestState(t, file))
	encoded := base64.StdEncoding.EncodeToString(random)
	if len(encoded) != 28 {
		t.Fatalf("the random member has %d characters", len(encoded))
	}
	for _, member := range []string{
		`"counters":{"started":1,"cleared":2,"evacuations":3,"reroutes":4,"faultWaitTicks":5,"sampled":6,"skipped":7,"clearedTicks":8,"delayedRiderTicks":9},"random":"` + encoded + `","nextTick":1234}`,
		`"emergencies":{"counters":{"started":11,"ended":12,"emergencyTicks":13,"sampled":14,"skipped":15,"endedTicks":16,"maxTicks":17},"random":"` + base64.StdEncoding.EncodeToString(other) + `","nextTick":5678}`,
	} {
		if !bytes.Contains(raw, []byte(member)) {
			t.Errorf("the save has no %s", member)
		}
	}
	decoded, err := decodeIncidentSave(compressTestJSON(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded.Simulation.Faults, file.Simulation.Faults) || !reflect.DeepEqual(decoded.Simulation.Emergencies, file.Simulation.Emergencies) {
		t.Fatalf("decoded\n%+v\n%+v\nwant\n%+v\n%+v", decoded.Simulation.Faults, decoded.Simulation.Emergencies, file.Simulation.Faults, file.Simulation.Emergencies)
	}
	file.Simulation.Faults = &sim.SavedFaults{Random: random, NextTick: new(int64(0))}
	file.Simulation.Emergencies = &sim.SavedEmergencies{Random: other, NextTick: new(int64(sim.MaxCounter))}
	raw = decompressTestJSON(t, encodeTestState(t, file))
	for _, member := range []string{`"faults":{"random":"` + encoded + `","nextTick":0}`, `"nextTick":9007199254740991}`} {
		if !bytes.Contains(raw, []byte(member)) {
			t.Errorf("the save has no %s", member)
		}
	}
	// A group with one sampler member is not empty for the encoder. The
	// restore refuses it.
	file.Simulation.Faults, file.Simulation.Emergencies = &sim.SavedFaults{NextTick: new(int64(0))}, &sim.SavedEmergencies{Random: []byte{}}
	if _, err := new(stateEncoder).encode(file); err != nil {
		t.Fatalf("encode one sampler member: %v", err)
	}
	file.Simulation.Faults, file.Simulation.Emergencies = &sim.SavedFaults{}, nil
	if _, err := new(stateEncoder).encode(file); err == nil || err.Error() != "saved faults member is empty" {
		t.Fatalf("encode an empty faults member: %v", err)
	}
	file.Simulation.Faults, file.Simulation.Emergencies = nil, &sim.SavedEmergencies{}
	if _, err := new(stateEncoder).encode(file); err == nil || err.Error() != "saved emergencies member is empty" {
		t.Fatalf("encode an empty emergencies member: %v", err)
	}
}

// TestSamplerSaveValues checks the raw sampler members of a save (sections 4.2
// and 4.3 of the incident stage 7 contract). Presence does not depend on
// value, a null is refused below the scanned member, and a value out of the
// integer scan or of its type makes the save invalid.
func TestSamplerSaveValues(t *testing.T) {
	t.Parallel()
	file := newTestStateFile(t)
	raw := string(decompressTestJSON(t, encodeTestState(t, file)))
	place := `"simulation":{`
	edit := func(group, value string) []byte {
		return compressTestJSON(t, []byte(strings.Replace(raw, place, place+`"`+group+`":`+value+",", 1)))
	}
	presence := func(t *testing.T, group, value string) (random []byte, next *int64) {
		t.Helper()
		decoded, err := decodeIncidentSave(edit(group, value))
		if err != nil {
			t.Fatal(err)
		}
		if group == "faults" {
			return decoded.Simulation.Faults.Random, decoded.Simulation.Faults.NextTick
		}
		return decoded.Simulation.Emergencies.Random, decoded.Simulation.Emergencies.NextTick
	}
	for _, group := range []string{"faults", "emergencies"} {
		t.Run(group, func(t *testing.T) {
			t.Parallel()
			if random, next := presence(t, group, `{"nextTick":0}`); random != nil || next == nil || *next != 0 {
				t.Errorf("nextTick 0 gives %v and %v", random, next)
			}
			if random, next := presence(t, group, `{"random":""}`); random == nil || len(random) != 0 || next != nil {
				t.Errorf("an empty random gives %v and %v", random, next)
			}
			for name, value := range map[string]string{
				"random null":          `{"random":null}`,
				"nextTick null":        `{"nextTick":null}`,
				"random not base64":    `{"random":"!"}`,
				"random a number":      `{"random":1}`,
				"nextTick a fraction":  `{"nextTick":1.5}`,
				"nextTick a string":    `{"nextTick":"1"}`,
				"nextTick above range": `{"nextTick":9007199254740992}`,
				"unknown member":       `{"seed":1}`,
			} {
				if _, err := decodeIncidentSave(edit(group, value)); stateReason(err) != reasonInvalidState {
					t.Errorf("%s: %v", name, err)
				}
			}
			for name, want := range map[string]string{
				`{"random":null}`:   "saved member /simulation/" + group + "/random is null",
				`{"nextTick":null}`: "saved member /simulation/" + group + "/nextTick is null",
			} {
				if _, err := decodeIncidentSave(edit(group, name)); err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("%s: %v, want %q", name, err, want)
				}
			}
		})
	}
}

// TestSamplerSaveRejections checks the sampler refusals at a rate of 0
// through the session, in both tiers (section 4.3 of the incident stage 7
// contract). A session cannot have a rate until the validators accept one.
func TestSamplerSaveRejections(t *testing.T) {
	t.Parallel()
	check := func(t *testing.T, file stateFile, want string) {
		t.Helper()
		for _, logical := range []bool{false, true} {
			file.RestoreAttempts = 0
			if logical {
				file.RestoreAttempts = restoreLoopAttempts - 1
			}
			_, err := newTestSession(t).loadState(loadInput{data: encodeTestState(t, file), steps: realRestoreSteps()})
			if stateReason(err) != reasonInvalidState || !strings.Contains(err.Error(), want) {
				t.Fatalf("logical %t: %v, want %q", logical, err, want)
			}
		}
	}
	random := samplerRandom(t, 1)
	t.Run("faults", func(t *testing.T) {
		t.Parallel()
		base, _ := faultSessionData(t)
		for name, test := range map[string]struct {
			edit func(*sim.SavedFaults)
			want string
		}{
			"nextTick 0 alone":   {func(faults *sim.SavedFaults) { faults.NextTick = new(int64(0)) }, "invalid saved fault: random and nextTick need each other"},
			"random alone":       {func(faults *sim.SavedFaults) { faults.Random = random }, "invalid saved fault: random and nextTick need each other"},
			"both at rate 0":     {func(faults *sim.SavedFaults) { faults.Random, faults.NextTick = random, new(int64(0)) }, "invalid saved fault: the sampler members need a rate above 0"},
			"a negative sampled": {func(faults *sim.SavedFaults) { faults.Counters.Sampled = -1 }, "invalid saved fault: a fault counter is negative"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				file := base
				faults := *file.Simulation.Faults
				faults.Records = slices.Clone(faults.Records)
				test.edit(&faults)
				file.Simulation.Faults = &faults
				check(t, file, test.want)
			})
		}
	})
	t.Run("emergencies", func(t *testing.T) {
		t.Parallel()
		base, _ := emergencySessionData(t)
		for name, test := range map[string]struct {
			edit func(*sim.SavedEmergencies)
			want string
		}{
			"nextTick 0 alone":   {func(group *sim.SavedEmergencies) { group.NextTick = new(int64(0)) }, "invalid saved emergency: random and nextTick need each other"},
			"both at rate 0":     {func(group *sim.SavedEmergencies) { group.Random, group.NextTick = random, new(int64(0)) }, "invalid saved emergency: the sampler members need a rate above 0"},
			"a negative maximum": {func(group *sim.SavedEmergencies) { group.Counters.MaxTicks = -1 }, "invalid saved emergency: an emergency counter is negative"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				file := base
				group := *file.Simulation.Emergencies
				group.Records = slices.Clone(group.Records)
				test.edit(&group)
				file.Simulation.Emergencies = &group
				check(t, file, test.want)
			})
		}
	})
}

// TestSamplerFrameMembers checks the frame members of section 4.2 of the
// incident stage 7 contract. The counters of each group come back from a full
// frame, a delta, and an HTTP state, with a different value in each counter.
// random and nextTick are unknown members of a frame, and the strict decoder
// refuses them.
func TestSamplerFrameMembers(t *testing.T) {
	t.Parallel()
	topology, base, next := faultStreamFrames(t)
	next.State.Simulation.Faults.Counters = faultCountersForTest()
	next.State.Simulation.Emergencies.Counters = emergencyCountersForTest()
	simulation := next.State.Simulation
	for _, kind := range []string{"full", "delta"} {
		raw := faultEnvelopeJSON(t, base, next, kind)
		applied, err := applyFaultJSON(topology, base, raw)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got := applied.State.Simulation; !reflect.DeepEqual(got.Faults, simulation.Faults) || !reflect.DeepEqual(got.Emergencies, simulation.Emergencies) {
			t.Fatalf("%s gives %+v and %+v", kind, got.Faults, got.Emergencies)
		}
		for _, counters := range []string{
			`"counters":{"started":1,"cleared":2,"evacuations":3,"reroutes":4,"faultWaitTicks":5,"sampled":6,"skipped":7,"clearedTicks":8,"delayedRiderTicks":9}`,
			`"counters":{"started":11,"ended":12,"emergencyTicks":13,"sampled":14,"skipped":15,"endedTicks":16,"maxTicks":17}`,
		} {
			if !bytes.Contains(raw, []byte(counters)) {
				t.Errorf("%s has no %s: %s", kind, counters, raw)
			}
		}
	}
	data, err := EncodeStateJSON(topology, next)
	if err != nil {
		t.Fatal(err)
	}
	state, err := DecodeStateJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := stateFrame(state).Simulation; !reflect.DeepEqual(got.Faults, simulation.Faults) || !reflect.DeepEqual(got.Emergencies, simulation.Emergencies) {
		t.Fatalf("the HTTP state gives %+v and %+v", got.Faults, got.Emergencies)
	}
	for _, member := range []string{`"random":"AAAA",`, `"nextTick":1,`} {
		for _, group := range []string{`"faults":{`, `"emergencies":{`} {
			for _, kind := range []string{"full", "delta"} {
				raw := faultEnvelopeJSON(t, base, next, kind)
				edited := bytes.Replace(raw, []byte(group), []byte(group+member), 1)
				if bytes.Equal(edited, raw) {
					t.Fatalf("%s has no %s", kind, group)
				}
				if _, err := applyFaultJSON(topology, base, edited); err == nil {
					t.Errorf("%s: %s in %s is accepted", kind, member, group)
				}
			}
			edited := bytes.Replace(data, []byte(group), []byte(group+member), 1)
			if bytes.Equal(edited, data) {
				t.Fatalf("the HTTP state has no %s", group)
			}
			if _, err := DecodeStateJSON(edited); err == nil {
				t.Errorf("HTTP: %s in %s is accepted", member, group)
			}
		}
	}
}

// TestSamplerFrameCounterChecks checks each new counter in the frame checks
// (section 4.3 of the incident stage 7 contract): a negative value, and a
// nonzero value with the operations off.
func TestSamplerFrameCounterChecks(t *testing.T) {
	t.Parallel()
	faults := map[string]sim.FaultCounters{
		"sampled":           {Sampled: 1},
		"skipped":           {Skipped: 1},
		"clearedTicks":      {ClearedTicks: 1},
		"delayedRiderTicks": {DelayedRiderTicks: 1},
	}
	negativeFaults := map[string]sim.FaultCounters{
		"sampled":           {Sampled: -1},
		"skipped":           {Skipped: -1},
		"clearedTicks":      {ClearedTicks: -1},
		"delayedRiderTicks": {DelayedRiderTicks: -1},
	}
	for name, counters := range negativeFaults {
		frame := SimulationFrame{Faults: sim.FaultsView{Counters: counters}}
		if err := checkFaultFrame(frame); err == nil || err.Error() != "negative fault counter" {
			t.Errorf("negative %s: %v", name, err)
		}
	}
	for name, counters := range faults {
		frame := SimulationFrame{OperationsOff: true, Faults: sim.FaultsView{Counters: counters}}
		if err := checkFaultFrame(frame); !errors.Is(err, errFaultStreamOff) {
			t.Errorf("%s with the operations off: %v", name, err)
		}
	}
	emergencies := map[string]sim.EmergencyCounters{
		"sampled":    {Sampled: 1},
		"skipped":    {Skipped: 1},
		"endedTicks": {EndedTicks: 1},
		"maxTicks":   {MaxTicks: 1},
	}
	negativeEmergencies := map[string]sim.EmergencyCounters{
		"sampled":    {Sampled: -1},
		"skipped":    {Skipped: -1},
		"endedTicks": {EndedTicks: -1},
		"maxTicks":   {MaxTicks: -1},
	}
	for name, counters := range negativeEmergencies {
		frame := SimulationFrame{Emergencies: sim.EmergenciesView{Counters: counters}}
		if err := checkEmergencyFrame(frame); err == nil || err.Error() != "negative emergency counter" {
			t.Errorf("negative %s: %v", name, err)
		}
	}
	for name, counters := range emergencies {
		frame := SimulationFrame{OperationsOff: true, Emergencies: sim.EmergenciesView{Counters: counters}}
		if err := checkEmergencyFrame(frame); !errors.Is(err, errEmergencyStreamOff) {
			t.Errorf("%s with the operations off: %v", name, err)
		}
	}
}
