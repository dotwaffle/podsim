package sim

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
)

// samplerSaveRates are the rates of the save fixtures. Each fault setting
// differs from its default, so a restore that loses one changes the events.
func samplerSaveRates() IncidentRates {
	return IncidentRates{
		Faults: FaultRate{PerHour: 60, DebrisShare: 0.5, DebrisMeters: 3,
			Duration: FaultDurationRule{Kind: FaultDurationUniform, MinSeconds: 60, MaxSeconds: 900}},
		EmergenciesPerHour: 30,
		FaultSeed:          [2]uint64{5, 6},
		EmergencySeed:      [2]uint64{7, 8},
	}
}

// samplerRestoreInput returns the restore input of s with both switches on.
func samplerRestoreInput(s *Simulation, state SavedState, rates IncidentRates) RestoreStateInput {
	input := faultRestoreInput(s, state)
	input.EmergenciesOn, input.Rates = true, rates
	return input
}

// liveSamplerFleet returns the five idle pods of traceFleet with both samplers
// live, at tick 100.
func liveSamplerFleet(t *testing.T) *Simulation {
	t.Helper()
	s := traceFleet(t)
	if err := s.StartIncidentSampling(samplerSaveRates()); err != nil {
		t.Fatal(err)
	}
	advance(s, 100)
	return s
}

// validSamplerRandom returns a valid state of a sampler stream.
func validSamplerRandom(t *testing.T) []byte {
	t.Helper()
	random, err := rand.NewPCG(1, 2).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	return random
}

// TestSavedFaultSamplerRefusals pins the rows of the saved-state table of
// checkSavedFaults (section 4.3 of the incident stage 7 contract), their
// order and their text, with inputs that have more than one error, and the
// explicit-zero table. An edit that gives no error names no text.
func TestSavedFaultSamplerRefusals(t *testing.T) {
	t.Parallel()
	const (
		needs   = "invalid saved fault: a rate above 0 needs the sampler members"
		each    = "invalid saved fault: random and nextTick need each other"
		noRate  = "invalid saved fault: the sampler members need a rate above 0"
		badPCG  = "invalid saved fault: the random source is not valid"
		records = "invalid saved fault: fault i1.1 has a serial above the saved serial 0"
		off     = "saved faults need faults on"
		counter = "invalid saved fault: a fault counter is negative"
	)
	s := liveSamplerFleet(t)
	random := validSamplerRandom(t)
	tick := s.tick
	badRecord := []SavedFault{{Generation: 1, Serial: 1, Start: 0, End: -1, Debris: true}}
	outside := func(next int64) string {
		return fmt.Sprintf("invalid saved fault: nextTick %d is outside %d to %d", next, tick+1, MaxCounter)
	}
	faults := func(input *RestoreStateInput) *SavedFaults { return input.State.Faults }
	type edit = func(*RestoreStateInput)
	tests := []struct {
		name string
		edit edit
		want string
	}{
		{"control", func(*RestoreStateInput) {}, ""},
		{"row 1: no member and a rate", func(i *RestoreStateInput) { i.State.Faults = nil }, needs},
		{"row 2: no member and rate 0", func(i *RestoreStateInput) { i.State.Faults, i.Rates.Faults.PerHour = nil, 0 }, ""},
		{"row 2: no member and faults off", func(i *RestoreStateInput) { i.State.Faults, i.FaultsOn = nil, false }, ""},
		{"row 2: no member and a demo member", func(i *RestoreStateInput) { i.State.Faults, i.State.Demo = nil, &SavedDemo{} }, ""},
		{"row 2: no member and more pods than the fleet", func(i *RestoreStateInput) { i.State.Faults, i.Fleet = nil, i.Fleet[:1] }, ""},
		{"row 3 before row 4", func(i *RestoreStateInput) { i.FaultsOn, faults(i).Counters.Started = false, -1 }, off},
		{"row 4: started", func(i *RestoreStateInput) { faults(i).Counters.Started, faults(i).NextTick = -1, nil }, counter},
		{"row 4: sampled", func(i *RestoreStateInput) { faults(i).Counters.Sampled, faults(i).NextTick = -1, nil }, counter},
		{"row 4: skipped", func(i *RestoreStateInput) { faults(i).Counters.Skipped, faults(i).NextTick = -1, nil }, counter},
		{"row 4: clearedTicks", func(i *RestoreStateInput) { faults(i).Counters.ClearedTicks, faults(i).NextTick = -1, nil }, counter},
		{"row 4: delayedRiderTicks", func(i *RestoreStateInput) { faults(i).Counters.DelayedRiderTicks, faults(i).NextTick = -1, nil }, counter},
		{"row 5: random only, before row 6", func(i *RestoreStateInput) { faults(i).NextTick, i.Rates.Faults.PerHour = nil, 0 }, each},
		{"row 5: nextTick only, with a rate", func(i *RestoreStateInput) { faults(i).Random = nil }, each},
		{"row 5: rate 0, a counter, and only nextTick 0", func(i *RestoreStateInput) {
			*faults(i) = SavedFaults{Counters: FaultCounters{Started: 1}, NextTick: new(int64(0))}
			i.Rates.Faults.PerHour = 0
		}, each},
		{"row 6: rate 0, a valid random, and nextTick 0", func(i *RestoreStateInput) {
			*faults(i) = SavedFaults{Random: random, NextTick: new(int64(0))}
			i.Rates.Faults.PerHour = 0
		}, noRate},
		{"row 6 before rows 8 and 9", func(i *RestoreStateInput) {
			*faults(i) = SavedFaults{Random: []byte{}, NextTick: new(int64(-1))}
			i.Rates.Faults.PerHour = 0
		}, noRate},
		{"row 7: a counter and no sampler member, before row 10", func(i *RestoreStateInput) {
			*faults(i) = SavedFaults{Counters: FaultCounters{Started: 1}, Records: badRecord}
		}, needs},
		{"row 7 does not apply to a demo member", func(i *RestoreStateInput) {
			*faults(i) = SavedFaults{Counters: FaultCounters{Started: 1}}
			i.State.Demo = &SavedDemo{}
		}, ""},
		{"row 8: an empty random, before row 9", func(i *RestoreStateInput) {
			faults(i).Random, faults(i).NextTick = []byte{}, new(int64(-1))
		}, badPCG},
		{"row 8: rate above 0, an empty random, and a valid nextTick", func(i *RestoreStateInput) { faults(i).Random = []byte{} }, badPCG},
		{"row 9: rate above 0, a valid random, and nextTick 0", func(i *RestoreStateInput) { faults(i).NextTick = new(int64(0)) }, outside(0)},
		{"row 9: the saved tick, before row 10", func(i *RestoreStateInput) {
			faults(i).NextTick, faults(i).Records = new(tick), badRecord
		}, outside(tick)},
		{"row 9: above the terminal value", func(i *RestoreStateInput) { faults(i).NextTick = new(int64(MaxCounter + 1)) }, outside(MaxCounter + 1)},
		{"row 9: the saved tick + 1 is valid", func(i *RestoreStateInput) { faults(i).NextTick = new(tick + 1) }, ""},
		{"row 9: the terminal value is valid", func(i *RestoreStateInput) { faults(i).NextTick = new(int64(MaxCounter)) }, ""},
		{"row 10", func(i *RestoreStateInput) { faults(i).Records = badRecord }, records},
		{"the fault rows run before the emergency rows", func(i *RestoreStateInput) { i.State.Faults, i.State.Emergencies = nil, nil }, needs},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := s.ExportState()
			input := samplerRestoreInput(s, state, samplerSaveRates())
			test.edit(&input)
			if err := checkSavedFaults(input); errorText(err) != test.want {
				t.Fatalf("the check gives %q, want %q", errorText(err), test.want)
			}
			if test.want == "" {
				return
			}
			for _, logical := range []bool{false, true} {
				input.LogicalOnly = logical
				reached := func() (*Simulation, error) { t.Fatal("the restore reached a tier"); return nil, nil }
				if _, _, err := restoreState(input, reached); errorText(err) != test.want {
					t.Fatalf("logical %t: the restore gives %q, want %q", logical, errorText(err), test.want)
				}
			}
		})
	}
}

// TestSavedEmergencySamplerRefusals pins the rows of the saved-state table of
// checkSavedEmergencies (section 4.3 of the incident stage 7 contract), with
// inputs that have more than one error.
func TestSavedEmergencySamplerRefusals(t *testing.T) {
	t.Parallel()
	const (
		needs   = "invalid saved emergency: a rate above 0 needs the sampler members"
		each    = "invalid saved emergency: random and nextTick need each other"
		noRate  = "invalid saved emergency: the sampler members need a rate above 0"
		badPCG  = "invalid saved emergency: the random source is not valid"
		counter = "invalid saved emergency: an emergency counter is negative"
		pod     = "invalid saved emergency: E2: pod 01 has the emergency hold and no emergency record"
	)
	s := liveSamplerFleet(t)
	random := validSamplerRandom(t)
	tick := s.tick
	outside := func(next int64) string {
		return fmt.Sprintf("invalid saved emergency: nextTick %d is outside %d to %d", next, tick+1, MaxCounter)
	}
	group := func(input *RestoreStateInput) *SavedEmergencies { return input.State.Emergencies }
	holdFirstPod := func(input *RestoreStateInput) {
		input.State.Pods = slices.Clone(input.State.Pods)
		input.State.Pods[0].Withdrawn = uint8(emergencyHold)
	}
	type edit = func(*RestoreStateInput)
	tests := []struct {
		name string
		edit edit
		want string
	}{
		{"control", func(*RestoreStateInput) {}, ""},
		{"row 1: started, before row 2", func(i *RestoreStateInput) { group(i).Counters.Started, group(i).NextTick = -1, nil }, counter},
		{"row 1: sampled", func(i *RestoreStateInput) { group(i).Counters.Sampled, group(i).NextTick = -1, nil }, counter},
		{"row 1: skipped", func(i *RestoreStateInput) { group(i).Counters.Skipped, group(i).NextTick = -1, nil }, counter},
		{"row 1: endedTicks", func(i *RestoreStateInput) { group(i).Counters.EndedTicks, group(i).NextTick = -1, nil }, counter},
		{"row 1: maxTicks", func(i *RestoreStateInput) { group(i).Counters.MaxTicks, group(i).NextTick = -1, nil }, counter},
		{"row 2: random only, before row 3", func(i *RestoreStateInput) { group(i).NextTick, i.Rates.EmergenciesPerHour = nil, 0 }, each},
		{"row 2: rate 0, a counter, and only nextTick 0", func(i *RestoreStateInput) {
			*group(i) = SavedEmergencies{Counters: EmergencyCounters{Started: 1}, NextTick: new(int64(0))}
			i.Rates.EmergenciesPerHour = 0
		}, each},
		{"row 3: rate 0, a valid random, and nextTick 0", func(i *RestoreStateInput) {
			*group(i) = SavedEmergencies{Random: random, NextTick: new(int64(0))}
			i.Rates.EmergenciesPerHour = 0
		}, noRate},
		{"row 3 before rows 5 and 6", func(i *RestoreStateInput) {
			*group(i) = SavedEmergencies{Random: []byte{}, NextTick: new(int64(-1))}
			i.Rates.EmergenciesPerHour = 0
		}, noRate},
		{"row 4: no member and a rate", func(i *RestoreStateInput) { i.State.Emergencies = nil }, needs},
		{"row 4: a counter and no sampler member, before row 7", func(i *RestoreStateInput) {
			*group(i) = SavedEmergencies{Counters: EmergencyCounters{Started: 1}}
			holdFirstPod(i)
		}, needs},
		{"row 4 does not apply at rate 0", func(i *RestoreStateInput) { i.State.Emergencies, i.Rates.EmergenciesPerHour = nil, 0 }, ""},
		{"row 4 does not apply to a demo member", func(i *RestoreStateInput) { i.State.Emergencies, i.State.Demo = nil, &SavedDemo{} }, ""},
		{"row 4 does not apply to more pods than the fleet", func(i *RestoreStateInput) { i.State.Emergencies, i.Fleet = nil, i.Fleet[:1] }, ""},
		{"row 5: an empty random, before row 6", func(i *RestoreStateInput) {
			group(i).Random, group(i).NextTick = []byte{}, new(int64(-1))
		}, badPCG},
		{"row 6: nextTick 0", func(i *RestoreStateInput) { group(i).NextTick = new(int64(0)) }, outside(0)},
		{"row 6: the saved tick, before row 7", func(i *RestoreStateInput) {
			group(i).NextTick = new(tick)
			holdFirstPod(i)
		}, outside(tick)},
		{"row 6: above the terminal value", func(i *RestoreStateInput) { group(i).NextTick = new(int64(MaxCounter + 1)) }, outside(MaxCounter + 1)},
		{"row 6: the saved tick + 1 is valid", func(i *RestoreStateInput) { group(i).NextTick = new(tick + 1) }, ""},
		{"row 6: the terminal value is valid", func(i *RestoreStateInput) { group(i).NextTick = new(int64(MaxCounter)) }, ""},
		{"row 7", holdFirstPod, pod},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := s.ExportState()
			input := samplerRestoreInput(s, state, samplerSaveRates())
			test.edit(&input)
			if err := checkSavedEmergencies(input); errorText(err) != test.want {
				t.Fatalf("the check gives %q, want %q", errorText(err), test.want)
			}
			if test.want == "" {
				return
			}
			for _, logical := range []bool{false, true} {
				input.LogicalOnly = logical
				reached := func() (*Simulation, error) { t.Fatal("the restore reached a tier"); return nil, nil }
				if _, _, err := restoreState(input, reached); errorText(err) != test.want {
					t.Fatalf("logical %t: the restore gives %q, want %q", logical, errorText(err), test.want)
				}
			}
		})
	}
	t.Run("emergencies off", func(t *testing.T) {
		t.Parallel()
		state := s.ExportState()
		input := samplerRestoreInput(s, state, samplerSaveRates())
		input.EmergenciesOn = false
		if err := checkSavedEmergencies(input); errorText(err) != "saved emergencies need emergencies on" {
			t.Fatalf("the check gives %q", errorText(err))
		}
		input.State.Emergencies = nil
		if err := checkSavedEmergencies(input); err != nil {
			t.Fatalf("no member with emergencies off: %v", err)
		}
	})
}

// errorText returns the text of err, or "" for nil.
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestSavedSamplerPolicy checks that CheckIncidentPolicy keeps refusing a demo
// save with a sampler member, by the same demo rule as the presence checks
// (section 4.2 of the incident stage 7 contract).
func TestSavedSamplerPolicy(t *testing.T) {
	t.Parallel()
	const refused = "saved demo fleet has fault or emergency state"
	s := liveSamplerFleet(t)
	for name, test := range map[string]struct {
		edit func(*RestoreStateInput)
		want string
	}{
		"ordinary save":              {func(*RestoreStateInput) {}, ""},
		"demo member and samplers":   {func(i *RestoreStateInput) { i.State.Demo = &SavedDemo{} }, refused},
		"more pods and samplers":     {func(i *RestoreStateInput) { i.Fleet = i.Fleet[:1] }, refused},
		"demo fault group only":      {func(i *RestoreStateInput) { i.State.Demo, i.State.Emergencies = &SavedDemo{}, nil }, refused},
		"demo emergency group only":  {func(i *RestoreStateInput) { i.State.Demo, i.State.Faults = &SavedDemo{}, nil }, refused},
		"demo member and no sampler": {func(i *RestoreStateInput) { i.State.Demo, i.State.Faults, i.State.Emergencies = &SavedDemo{}, nil, nil }, ""},
	} {
		input := samplerRestoreInput(s, s.ExportState(), samplerSaveRates())
		test.edit(&input)
		if err := CheckIncidentPolicy(input); errorText(err) != test.want {
			t.Errorf("%s: the check gives %q, want %q", name, errorText(err), test.want)
		}
	}
}

// TestSavedSamplerPresence checks the presence rule of section 4.2 of the
// incident stage 7 contract: both sampler members are present exactly when the
// sampler is live, and a live sampler with no record and no counter has the
// group.
func TestSavedSamplerPresence(t *testing.T) {
	t.Parallel()
	s := samplerFleet(t)
	if state := s.ExportState(); state.Faults != nil || state.Emergencies != nil {
		t.Fatalf("a rate of 0 has the members %+v and %+v", state.Faults, state.Emergencies)
	}
	if err := s.StartIncidentSampling(incidentRatesForTest()); err != nil {
		t.Fatal(err)
	}
	state := s.ExportState()
	faultRandom, err := s.faultSampler.stream.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	emergencyRandom, err := s.emergencySampler.stream.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	wantFaults := &SavedFaults{Random: faultRandom, NextTick: new(s.faultSampler.nextTick)}
	wantEmergencies := &SavedEmergencies{Random: emergencyRandom, NextTick: new(s.emergencySampler.nextTick)}
	if !reflect.DeepEqual(state.Faults, wantFaults) || !reflect.DeepEqual(state.Emergencies, wantEmergencies) {
		t.Fatalf("the members are %+v and %+v, want %+v and %+v", state.Faults, state.Emergencies, wantFaults, wantEmergencies)
	}
	if len(faultRandom) != 20 {
		t.Fatalf("the random state has %d bytes", len(faultRandom))
	}
	// Each member is a copy.
	state.Faults.Random[0]++
	*state.Faults.NextTick = 0
	if again := s.ExportState(); !reflect.DeepEqual(again.Faults, wantFaults) {
		t.Fatal("the saved members share storage with the sampler")
	}
	// Only the sampler counters leave a group with a stopped sampler, with no
	// sampler member.
	s.faultCounters.sampled, s.emergencyCounters.skipped, s.emergencyCounters.sampled = 2, 1, 1
	if err := s.SetFaults(false, FaultSettings{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmergencies(false); err != nil {
		t.Fatal(err)
	}
	state = s.ExportState()
	if got := state.Faults; got == nil || got.Random != nil || got.NextTick != nil || got.Counters.Sampled != 2 {
		t.Fatalf("the stopped fault sampler saves %+v", got)
	}
	if got := state.Emergencies; got == nil || got.Random != nil || got.NextTick != nil || got.Counters.Skipped != 1 {
		t.Fatalf("the stopped emergency sampler saves %+v", got)
	}
}

// TestIncidentCounterForms checks each counter of the exported and native
// forms with a different value, so a swap of two counters fails.
func TestIncidentCounterForms(t *testing.T) {
	t.Parallel()
	native := faultCounters{started: 1, cleared: 2, evacuations: 3, reroutes: 4, faultWaitTicks: 5, sampled: 6, skipped: 7, clearedTicks: 8, delayedRiderTicks: 9}
	exported := FaultCounters{Started: 1, Cleared: 2, Evacuations: 3, Reroutes: 4, FaultWaitTicks: 5, Sampled: 6, Skipped: 7, ClearedTicks: 8, DelayedRiderTicks: 9}
	if got := native.exported(); got != exported {
		t.Errorf("exported fault counters %+v, want %+v", got, exported)
	}
	if got := exported.counters(); got != native {
		t.Errorf("native fault counters %+v, want %+v", got, native)
	}
	nativeEmergency := emergencyCounters{started: 1, ended: 2, emergencyTicks: 3, sampled: 4, skipped: 5, endedTicks: 6, maxTicks: 7}
	exportedEmergency := EmergencyCounters{Started: 1, Ended: 2, EmergencyTicks: 3, Sampled: 4, Skipped: 5, EndedTicks: 6, MaxTicks: 7}
	if got := nativeEmergency.exported(); got != exportedEmergency {
		t.Errorf("exported emergency counters %+v, want %+v", got, exportedEmergency)
	}
	if got := exportedEmergency.counters(); got != nativeEmergency {
		t.Errorf("native emergency counters %+v, want %+v", got, nativeEmergency)
	}
}

// samplerEventTrace steps s until each sampler has n more events, and returns
// each event: its tick, its draws, its outcome, and its next tick.
func samplerEventTrace(s *Simulation, n int) []string {
	var trace []string
	for faults, emergencies := 0, 0; faults < n || emergencies < n; {
		fault, emergency := s.faultSampler, s.emergencySampler
		started, begun := s.faultCounters.started, s.emergencyCounters.started
		s.Step()
		if fault.due(s.tick) {
			faults++
			outcome := "skip"
			if s.faultCounters.started > started {
				outcome = fmt.Sprintf("%+v", s.faults[len(s.faults)-1])
			}
			trace = append(trace, fmt.Sprintf("fault tick=%d draws=%v %s next=%d", s.tick, pcgDraws(fault.stream, 5), outcome, s.faultSampler.nextTick))
		}
		if emergency.due(s.tick) {
			emergencies++
			outcome := "skip"
			if s.emergencyCounters.started > begun {
				outcome = fmt.Sprintf("%+v", s.emergencies[len(s.emergencies)-1])
			}
			trace = append(trace, fmt.Sprintf("emergency tick=%d draws=%v %s next=%d", s.tick, pcgDraws(emergency.stream, 3), outcome, s.emergencySampler.nextTick))
		}
	}
	return trace
}

// TestSamplerRestoreTrace saves a run in which each pod stays at its berth,
// with no motion and no queue, so the physical tier keeps its state, and it
// checks that the next 20 events of each sampler are those of the run that did
// not stop (section 5.1.8 of the incident stage 7 contract). The events show
// each restored setting, which draws and due ticks alone cannot show.
func TestSamplerRestoreTrace(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		// events is the number of events before the save, so the save can have
		// records and counters.
		events int
	}{{"before the first event", 0}, {"with records", 3}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := liveSamplerFleet(t)
			if test.events > 0 {
				samplerEventTrace(s, test.events)
			}
			saved := s.ExportState()
			if test.events > 0 && (saved.Faults == nil || saved.Faults.Counters.Sampled == 0 || len(saved.Faults.Records) == 0) {
				t.Fatalf("the save has the faults %+v", saved.Faults)
			}
			restored, result, err := RestoreState(samplerRestoreInput(s, saved, samplerSaveRates()))
			if err != nil {
				t.Fatal(err)
			}
			if !cleanRestore(result) {
				t.Fatalf("the restore has the result %+v", result)
			}
			if !reflect.DeepEqual(restored.faultRate, s.faultRate) || restored.faultSampler != s.faultSampler || restored.emergencySampler != s.emergencySampler {
				t.Fatalf("the restored samplers are %+v and %+v, want %+v and %+v", restored.faultSampler, restored.emergencySampler, s.faultSampler, s.emergencySampler)
			}
			want := samplerEventTrace(s, 20)
			got := samplerEventTrace(restored, 20)
			if !slices.Equal(got, want) {
				t.Fatalf("the restored events differ:\n%q\nwant\n%q", got, want)
			}
		})
	}
}

// TestSamplerLogicalRestore checks the logical tier with a live sampler: the
// stream, the next tick, and the counters stay, and the records end. The later
// events keep their ticks and their draws (section 5.1.4 of the incident stage
// 7 contract).
func TestSamplerLogicalRestore(t *testing.T) {
	t.Parallel()
	s := sharedTraceFleet(t)
	if err := s.StartIncidentSampling(samplerSaveRates()); err != nil {
		t.Fatal(err)
	}
	for len(s.faults) == 0 || len(s.emergencies) == 0 {
		submitSharedTrips(t, s)
		s.Step()
		if s.tick > 200_000 {
			t.Fatal("the run has no fault and emergency at one time")
		}
	}
	state := s.ExportState()
	input := samplerRestoreInput(s, state, samplerSaveRates())
	input.LogicalOnly = true
	restored, result, err := RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Tier != RestoreLogical || result.DroppedFaults != len(state.Faults.Records) || result.DroppedEmergencies != len(state.Emergencies.Records) {
		t.Fatalf("the result is %+v", result)
	}
	if len(restored.faults) != 0 || len(restored.emergencies) != 0 {
		t.Fatalf("the tier kept %d faults and %d emergencies", len(restored.faults), len(restored.emergencies))
	}
	if restored.faultCounters != s.faultCounters || restored.emergencyCounters != s.emergencyCounters {
		t.Fatalf("the counters are %+v and %+v, want %+v and %+v", restored.faultCounters, restored.emergencyCounters, s.faultCounters, s.emergencyCounters)
	}
	if restored.faultSampler != s.faultSampler || restored.emergencySampler != s.emergencySampler || restored.faultRate != s.faultRate {
		t.Fatal("the tier changed a sampler")
	}
	if err := restored.CheckContract(); err != nil {
		t.Fatal(err)
	}
	// The saved stream gives the same draws at the same ticks, whatever the
	// fleet does after the tier.
	for range 5 {
		faultDraws := pcgDraws(s.faultSampler.stream, 5)
		restoredDraws := pcgDraws(restored.faultSampler.stream, 5)
		if !slices.Equal(faultDraws, restoredDraws) {
			t.Fatalf("the draws are %v, want %v", restoredDraws, faultDraws)
		}
		next := s.faultSampler.nextTick
		if restored.faultSampler.nextTick != next {
			t.Fatalf("the next tick is %d, want %d", restored.faultSampler.nextTick, next)
		}
		for s.tick < next {
			s.Step()
		}
		for restored.tick < next {
			restored.Step()
		}
	}
}

// TestSamplerCountersAtRestore checks that a restore refuses a save with more
// skipped events than sampled events. No row of the sampler tables has this
// rule. F13 and E9 refuse it after each tier, so the save moves aside.
func TestSamplerCountersAtRestore(t *testing.T) {
	t.Parallel()
	s := liveSamplerFleet(t)
	for name, edit := range map[string]func(*SavedState){
		"F13": func(state *SavedState) { state.Faults.Counters.Skipped = 1 },
		"E9":  func(state *SavedState) { state.Emergencies.Counters.Skipped = 1 },
	} {
		state := s.ExportState()
		edit(&state)
		if _, _, err := RestoreState(samplerRestoreInput(s, state, samplerSaveRates())); err == nil {
			t.Errorf("%s: the restore accepts a skipped count above the sampled count", name)
		}
	}
}

// TestPreparedRestoreRates checks that a prepared restore gets the rates: a
// save with sampler members needs a rate, and the sampler continues.
func TestPreparedRestoreRates(t *testing.T) {
	t.Parallel()
	s := liveSamplerFleet(t)
	prepared, err := PrepareNetwork(s.network)
	if err != nil {
		t.Fatal(err)
	}
	input := PreparedRestoreInput{Fleet: s.initial, State: s.ExportState(), FaultsOn: true,
		Faults: FaultSettings{EvacuationSeconds: 300}, EmergenciesOn: true, Rates: samplerSaveRates()}
	restored, _, err := prepared.RestoreState(input)
	if err != nil {
		t.Fatal(err)
	}
	if restored.faultSampler != s.faultSampler || restored.emergencySampler != s.emergencySampler {
		t.Fatal("the prepared restore changed a sampler")
	}
}
