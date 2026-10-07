package session

import (
	"bytes"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// setRootMember returns raw with the value of its root member name
// replaced by value.
func setRootMember(t *testing.T, raw []byte, name, value string) []byte {
	t.Helper()
	members := splitObject(t, raw)
	index := slices.IndexFunc(members, func(member rootMember) bool { return member.name == name })
	if index < 0 {
		t.Fatalf("no root member %q", name)
	}
	members[index].value = []byte(value)
	return joinObject(t, members)
}

// TestSavedPackedRoundTrip saves waiting trips, riders and boarding tuples
// of a plain file. The file packs its order text, and the decode gives the
// saved values again.
func TestSavedPackedRoundTrip(t *testing.T) {
	t.Parallel()
	plain := boardingTestFile(t)
	plain.Simulation.Waiting = newTestStateFile(t).Simulation.Waiting
	for name, file := range map[string]stateFile{"plain": plain} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := encodeTestState(t, file)
			raw := decompressTestJSON(t, data)
			from := file.Simulation.Waiting[0].Request.From
			rider := file.Simulation.Pods[0].Riders[1].From
			for _, text := range []string{from, rider} {
				if bytes.Contains(raw, []byte(`"from":"`+text+`"`)) || !bytes.Contains(raw, []byte(`"from":"`+packedTestText(t, text)+`"`)) {
					t.Fatalf("order text %q is not packed", text)
				}
			}
			if !bytes.Contains(raw, []byte(`"boardings":[[1,0],[0,60]]`)) {
				t.Fatal("the save has no boarding tuples")
			}
			decoded, err := decodeStateFile(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := decoded.resolveBoardings(); err != nil {
				t.Fatal(err)
			}
			// The boarding tuples replace journeyOrigin, so compare the
			// orders and the records.
			pod, saved := decoded.Simulation.Pods[0], file.Simulation.Pods[0]
			if !reflect.DeepEqual(decoded.Simulation.Waiting, file.Simulation.Waiting) || !reflect.DeepEqual(pod.Riders, saved.Riders) ||
				!slices.Equal(pod.Boardings, saved.Boardings) {
				t.Fatal("the decoded orders or boarding records differ from the saved values")
			}
			if !bytes.Equal(raw, decompressTestJSON(t, encodeTestState(t, decoded))) {
				t.Fatal("the reencoded save differs")
			}
		})
	}
}

func packedTestText(t *testing.T, text string) string {
	t.Helper()
	packed, err := packOrderText(text, 64)
	if err != nil {
		t.Fatal(err)
	}
	return packed
}

// TestSavedMarkersSelectSections saves a project of each kind, and checks
// the markers, the packed order text and the restore. It then changes the
// markers of the save. A textEncoding member is refused in each kind.
func TestSavedMarkersSelectSections(t *testing.T) {
	t.Parallel()
	for _, kind := range []struct {
		name    string
		express bool
	}{
		{"plain", false},
		{"Express", true},
	} {
		t.Run(kind.name, func(t *testing.T) {
			t.Parallel()
			config := project.Default()
			command := Command{Action: "trip", Origin: "harbor", Destination: "market", PartySize: 1, SharingConsent: sim.PrivateConsent, Service: sim.OnDemandService}
			if kind.express {
				config = expressConsumerProject(t)
				command.OrderContract, command.Service, command.ServiceID, command.SharingConsent = sim.ExpressOrderContract, sim.ExpressServiceChoice, "harbor-market", sim.SharedConsent
			}
			store := &fakeStore{}
			s, err := NewFromStore(t.Context(), StoreInput{Store: store, Project: &config})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.Close)
			newTestClient(s, "markers").mustApply(t, command)
			if saveErr := s.SaveState(t.Context(), SavePeriodic); saveErr != nil {
				t.Fatal(saveErr)
			}
			file := store.lastWrite(t)
			if file.Version != stateVersion || file.OrderContract != config.OrderContract || file.Simulation.OrderContract != config.OrderContract {
				t.Fatalf("version %d or marker %q differs from the project", file.Version, file.OrderContract)
			}
			data := store.writeList()[len(store.writeList())-1]
			raw := decompressTestJSON(t, data)
			if !bytes.Contains(raw, []byte(`"from":"`+packedTestText(t, "harbor")+`"`)) || bytes.Contains(raw, []byte(`"textEncoding"`)) {
				t.Fatal("the save does not pack its order text, or has a textEncoding member")
			}
			restored, err := NewFromStore(t.Context(), StoreInput{Store: &fakeStore{data: data}})
			if err != nil || restored.restore.Tier != "physical" {
				t.Fatalf("restore: %v", err)
			}
			t.Cleanup(restored.Close)
			for name, changed := range savedMarkerEdits(t, raw, kind.express) {
				_, err := decodeStateFile(compressTestJSON(t, changed))
				switch {
				case err == nil:
					t.Errorf("%s: accepted", name)
				case strings.HasPrefix(name, "textEncoding") && !strings.Contains(err.Error(), "text encoding marker"):
					// A later check also refuses these files, so check
					// that the marker scan refuses them first.
					t.Errorf("%s: %v", name, err)
				}
			}
		})
	}
}

// savedMarkerEdits returns the changes of raw, a valid save, that the
// decoder must refuse.
func savedMarkerEdits(t *testing.T, raw []byte, express bool) map[string][]byte {
	t.Helper()
	edits := map[string][]byte{}
	for _, path := range []string{"", "project", "simulation"} {
		edits["textEncoding in /"+path] = addMember(t, raw, path, "textEncoding", `"order-text-base64-v1"`)
	}
	if !express {
		for _, path := range []string{"project", "simulation"} {
			edits["only the "+path+" Express marker"] = addMember(t, raw, path, "orderContract", `"express-v1"`)
		}
	}
	return edits
}

// addMember adds the member name with value to the object at path, a root
// member name, or to the root when path is empty.
func addMember(t *testing.T, raw []byte, path, name, value string) []byte {
	t.Helper()
	members := splitObject(t, raw)
	if path == "" {
		return joinObject(t, append(members, rootMember{name, []byte(value)}))
	}
	for i, member := range members {
		if member.name == path {
			members[i].value = joinObject(t, append(splitObject(t, member.value), rootMember{name, []byte(value)}))
			return joinObject(t, members)
		}
	}
	t.Fatalf("no root member %q", path)
	return nil
}

// TestSavedVersionRefusals refuses each other version of a plain file.
// Each earlier and later version moves aside.
func TestSavedVersionRefusals(t *testing.T) {
	t.Parallel()
	bases := map[string][]byte{
		"plain": decompressTestJSON(t, encodeTestState(t, newTestStateFile(t))),
	}
	for name, raw := range bases {
		for _, version := range []int{1, 2, 3, 4, 5, 6, 7, 8, 10} {
			t.Run(name+"/"+strconv.Itoa(version), func(t *testing.T) {
				t.Parallel()
				data := compressTestJSON(t, setRootMember(t, raw, "version", strconv.Itoa(version)))
				store := &fakeStore{data: data}
				s, err := NewFromStore(t.Context(), StoreInput{Store: store})
				assertMovedAside(t, s, err, store, reasonUnsupportedVersion)
			})
		}
	}
}

// TestSavedInvalidMovedAside checks that startup moves each damaged or
// invalid file aside as invalid_state and starts a new session. Only a
// file that is too large stays, and startup then fails.
func TestSavedInvalidMovedAside(t *testing.T) {
	t.Parallel()
	padding := `"padding":[` + strings.Repeat(`null,`, 65_536) + `null],`
	for name, file := range map[string]stateFile{"plain": newTestStateFile(t)} {
		encoded := encodeTestState(t, file)
		raw := decompressTestJSON(t, encoded)
		checksum := bytes.Clone(encoded)
		checksum[len(checksum)-8] ^= 0xff
		speed := file
		speed.Speed = 3
		cases := map[string][]byte{
			"gzip header cut":  encoded[:5],
			"gzip trailer cut": encoded[:len(encoded)-4],
			"gzip checksum":    checksum,
			"syntax error":     compressTestJSON(t, insertAfter(t, raw, `"version":9,`, `,`)),
			"array limit":      compressTestJSON(t, insertAfter(t, raw, `{`, padding)),
			"format type":      compressTestJSON(t, setRootMember(t, raw, "format", "1")),
			"version type":     compressTestJSON(t, setRootMember(t, raw, "version", `"9"`)),
			"decode error":     compressTestJSON(t, setRootMember(t, raw, "speed", `"fast"`)),
			"validation error": encodeTestState(t, speed),
			"missing version": compressTestJSON(t, joinObject(t, slices.DeleteFunc(splitObject(t, raw),
				func(member rootMember) bool { return member.name == "version" }))),
		}
		for _, version := range []string{"null", "0", "-1"} {
			cases["version "+version] = compressTestJSON(t, setRootMember(t, raw, "version", version))
		}
		cases["restore panic"] = encoded
		for kind, damaged := range cases {
			t.Run(name+"/"+kind, func(t *testing.T) {
				t.Parallel()
				steps := realRestoreSteps()
				if kind == "restore panic" {
					steps.restoreSimulation = func(sim.RestoreStateInput) (*sim.Simulation, sim.RestoreResult, error) {
						panic("restore test")
					}
				}
				store := &fakeStore{data: damaged}
				s, err := newFromStore(t.Context(), StoreInput{Store: store}, steps)
				assertMovedAside(t, s, err, store, reasonInvalidState)
			})
		}
	}
	t.Run("too large", func(t *testing.T) {
		t.Parallel()
		store := &fakeStore{data: compressTestJSON(t, make([]byte, MaxStateBytes+1))}
		before := bytes.Clone(store.data)
		s, err := NewFromStore(t.Context(), StoreInput{Store: store})
		assertPreserved(t, s, err, store, before)
		if stateReason(err) != reasonTooLarge {
			t.Fatal("oversized state is not too_large", err)
		}
	})
}

// TestSavedRemovedMembersMovedAside checks a version 9 file with a member
// of a removed feature: physical coupling, compact station queues, or
// station buffers.
// Before the first release a removed member keeps the version, so the
// file is not of another version.
// The strict decode refuses the member as unknown, and startup moves the
// file aside as invalid_state and starts a new session.
func TestSavedRemovedMembersMovedAside(t *testing.T) {
	t.Parallel()
	raw := decompressTestJSON(t, encodeTestState(t, newTestStateFile(t)))
	for name, damaged := range map[string][]byte{
		"couplingContract":                     addMember(t, raw, "", "couplingContract", `"compact-pair-v1"`),
		"simulation/couplingGroups":            addMember(t, raw, "simulation", "couplingGroups", `[]`),
		"project/couplingContract":             addMember(t, raw, "project", "couplingContract", `"compact-pair-v1"`),
		"simulation/couplingContract":          addMember(t, raw, "simulation", "couplingContract", `"compact-pair-v1"`),
		"simulation/pods/compactQueue":         insertAfter(t, raw, `"pods":[{`, `"compactQueue":{},`),
		"project/stationQueueSpacing":          addMember(t, raw, "project", "stationQueueSpacing", `"ordinary"`),
		"simulation/pods/stationBuffered":      insertAfter(t, raw, `"pods":[{`, `"stationBuffered":true,`),
		"simulation/pods/platoon/kind":         insertAfter(t, raw, `"pods":[{`, `"platoon":{"kind":"buffer","leader":"01"},`),
		"simulation/pods/platoon/terminalCell": insertAfter(t, raw, `"pods":[{`, `"platoon":{"terminalCell":0,"leader":"01"},`),
		"project/stationBuffers":               addMember(t, raw, "project", "stationBuffers", `false`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			member := name[strings.LastIndex(name, "/")+1:]
			data := compressTestJSON(t, damaged)
			if _, err := decodeStateFile(data); err == nil || !strings.Contains(err.Error(), `unknown object member name "`+member+`"`) {
				t.Fatalf("decode: %v, want the unknown member %s", err, member)
			}
			store := &fakeStore{data: data}
			s, err := NewFromStore(t.Context(), StoreInput{Store: store})
			assertMovedAside(t, s, err, store, reasonInvalidState)
		})
	}
}

// insertAfter adds text after the first match of after in raw.
func insertAfter(t *testing.T, raw []byte, after, text string) []byte {
	t.Helper()
	if !bytes.Contains(raw, []byte(after)) {
		t.Fatalf("no %s", after)
	}
	return bytes.Replace(raw, []byte(after), []byte(after+text), 1)
}
