package sim

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"hash"
	"reflect"
	"strings"
	"testing"
)

type stationSwitchProfile struct {
	name      string
	contracts FleetContracts
	refusal   string
}

type stationSwitchSettings struct {
	name           string
	faults         bool
	emergencies    bool
	faultID        string
	emergencyID    string
	faultCount     int64
	emergencyCount int64
}

type stationSwitchPublication struct {
	calls int
	view  []byte
}

type stationSwitchFault struct {
	Generation, Serial uint64
	Debris             bool
	Start, End         int64
	Pod, Lane          int
	From, To           float64
}

type stationSwitchEmergency struct {
	Generation, Serial uint64
	Start              int64
	Pod, Order         int
}

type stationSwitchPod struct {
	ID             string
	Purpose, Owner uint8
	Interrupt      uint32
	RiderLegFrom   []string
}

type stationSwitchTrip struct {
	LegFrom, ExcludedPod string
}

// stationSwitchSavedExtras copies fields that the native save JSON omits.
func stationSwitchSavedExtras(t *testing.T, digest hash.Hash, saved SavedState) {
	t.Helper()
	var extra struct {
		Faults      []stationSwitchFault
		Emergencies []stationSwitchEmergency
		Pods        []stationSwitchPod
		Waiting     []stationSwitchTrip
	}
	if saved.Faults != nil {
		for _, r := range saved.Faults.Records {
			extra.Faults = append(extra.Faults, stationSwitchFault(r))
		}
	}
	if saved.Emergencies != nil {
		for _, r := range saved.Emergencies.Records {
			extra.Emergencies = append(extra.Emergencies, stationSwitchEmergency(r))
		}
	}
	for _, pod := range saved.Pods {
		item := stationSwitchPod{ID: pod.ID, Purpose: pod.Purpose, Owner: pod.Owner, Interrupt: pod.Interrupt}
		for _, rider := range pod.Riders {
			item.RiderLegFrom = append(item.RiderLegFrom, rider.LegFrom)
		}
		extra.Pods = append(extra.Pods, item)
	}
	for _, trip := range saved.Waiting {
		extra.Waiting = append(extra.Waiting, stationSwitchTrip{LegFrom: trip.Request.LegFrom, ExcludedPod: trip.ExcludedPod})
	}
	stationSwitchEncode(t, digest, extra)
}

func stationSwitchCheckOmittedFields(t *testing.T) {
	t.Helper()
	for _, test := range []struct {
		typeOf reflect.Type
		want   string
	}{
		{reflect.TypeFor[SavedFault](), "Generation Serial Debris Start End Pod Lane From To"},
		{reflect.TypeFor[SavedEmergency](), "Generation Serial Start Pod Order"},
		{reflect.TypeFor[SavedPod](), "Purpose Owner Interrupt"},
		{reflect.TypeFor[SavedRequest](), "LegFrom"},
		{reflect.TypeFor[SavedTrip](), "ExcludedPod"},
	} {
		var omitted []string
		for field := range test.typeOf.Fields() {
			if field.Tag.Get("json") == "-" {
				omitted = append(omitted, field.Name)
			}
		}
		if got := strings.Join(omitted, " "); got != test.want {
			t.Fatalf("%s omitted-field inventory=%q, want %q; review explicit save observation", test.typeOf.Name(), got, test.want)
		}
	}
}

func stationSwitchEncode(t *testing.T, digest hash.Hash, value any) {
	t.Helper()
	data, err := json.Marshal(value, json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := digest.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
}

func stationSwitchObservation(t *testing.T, s *Simulation) []byte {
	t.Helper()
	digest := sha256.New()
	stationSwitchObserve(t, digest, s)
	return digest.Sum(nil)
}

func stationSwitchObserve(t *testing.T, digest hash.Hash, s *Simulation) {
	t.Helper()
	stationBaselineObserve(t, digest, s)
	stationSwitchSavedExtras(t, digest, s.ExportState())
	stationSwitchEncode(t, digest, struct {
		Generation          uint64
		Faults, Emergencies bool
		EvacuationSeconds   int64
	}{s.incidentGeneration, s.faultsOn, s.emergenciesOn, s.faultSettings.evacuationSeconds})
}

// stationSwitchCommand checks refusal text separately from error identity.
func stationSwitchCommand(t *testing.T, digest hash.Hash, s *Simulation, publication *stationSwitchPublication, name, wantID, wantText string, wantError error, command func() (string, error)) {
	t.Helper()
	before := stationSwitchObservation(t, s)
	calls, published := publication.calls, bytes.Clone(publication.view)
	id, err := command()
	text := ""
	if err != nil {
		text = err.Error()
	}
	if id != wantID || text != wantText || (err == nil) != (wantText == "") {
		t.Fatalf("%s result: id=%q error=%q, want id=%q error=%q", name, id, text, wantID, wantText)
	}
	if wantError != nil && !errors.Is(err, wantError) {
		t.Fatalf("%s error identity: %v, want %v", name, err, wantError)
	}
	if wantText != "" {
		if !bytes.Equal(before, stationSwitchObservation(t, s)) {
			t.Fatalf("%s refusal changed native save, snapshot, owners, or release frontiers", name)
		}
		if publication.calls != calls || !bytes.Equal(published, publication.view) {
			t.Fatalf("%s refusal published a snapshot", name)
		}
	} else if publication.calls != calls+1 {
		t.Fatalf("%s success publications=%d, want 1", name, publication.calls-calls)
	}
	stationSwitchEncode(t, digest, struct{ Command, ID, Error string }{name, id, text})
	stationSwitchObserve(t, digest, s)
}

func stationSwitchPublished(t *testing.T, s *Simulation, publication *stationSwitchPublication) {
	t.Helper()
	data, err := json.Marshal(s.Snapshot(), json.Deterministic(true))
	if err != nil {
		t.Fatal(err)
	}
	publication.calls++
	publication.view = data
}

func stationSwitchSteps(t *testing.T, digest hash.Hash, s *Simulation, count int) {
	t.Helper()
	for step := range count {
		s.Step()
		if err := s.CheckContract(); err != nil {
			t.Fatalf("native tick %d: %v", s.tick, err)
		}
		if step+1 == count/2 || step+1 == count {
			stationSwitchEncode(t, digest, struct{ Checkpoint int64 }{s.tick})
			stationSwitchObserve(t, digest, s)
		}
	}
}

func stationSwitchUnknownCommands(t *testing.T, digest hash.Hash, s *Simulation, publication *stationSwitchPublication, settings stationSwitchSettings) {
	t.Helper()
	faultText, faultError := "faults are not enabled", errFaultsOff
	clearText, clearError := "faults are not enabled", errFaultsOff
	if settings.faults {
		faultText, faultError = "unknown pod", errUnknownPod
		clearText, clearError = "unknown fault", errUnknownFault
	}
	stationSwitchCommand(t, digest, s, publication, "clear-unknown-before-onset", "", clearText, clearError, func() (string, error) {
		return "", s.ClearFault("i7.999")
	})
	stationSwitchCommand(t, digest, s, publication, "fault-unknown-pod", "", faultText, faultError, func() (string, error) {
		return s.Fault(FaultRequest{PodID: "unknown"})
	})
	emergencyText, emergencyError := "emergencies are not enabled", errEmergenciesOff
	if settings.emergencies {
		emergencyText, emergencyError = "unknown pod", errUnknownPod
	}
	stationSwitchCommand(t, digest, s, publication, "emergency-unknown-pod", "", emergencyText, emergencyError, func() (string, error) {
		return s.Emergency("unknown", 0)
	})
}

func stationSwitchOnset(t *testing.T, digest hash.Hash, s *Simulation, publication *stationSwitchPublication, settings stationSwitchSettings) {
	t.Helper()
	stationSwitchUnknownCommands(t, digest, s, publication, settings)
	faultText, faultError := "faults are not enabled", errFaultsOff
	if settings.faults {
		faultText, faultError = "", nil
	}
	stationSwitchCommand(t, digest, s, publication, "fault-moving-pod", settings.faultID, faultText, faultError, func() (string, error) {
		return s.Fault(FaultRequest{PodID: "01"})
	})
	if int64(len(s.faults)) != settings.faultCount || s.vehicles[0].faulted != settings.faults || s.faultCounters.started != settings.faultCount {
		t.Fatal("fault onset did not create exactly the expected moving-pod fault")
	}
	if settings.faults {
		stationSwitchCommand(t, digest, s, publication, "duplicate-fault", "", "pod already has a fault", errPodFaulted, func() (string, error) {
			return s.Fault(FaultRequest{PodID: "01"})
		})
		stationSwitchCommand(t, digest, s, publication, "disable-active-fault", "", "1 faults are active", nil, func() (string, error) {
			return "", s.SetFaults(false, FaultSettings{EvacuationSeconds: 300})
		})
	}
	emergencyText, emergencyError := "emergencies are not enabled", errEmergenciesOff
	if settings.emergencies {
		emergencyText, emergencyError = "", nil
	}
	stationSwitchCommand(t, digest, s, publication, "emergency-after-fault-command", settings.emergencyID, emergencyText, emergencyError, func() (string, error) {
		return s.Emergency("01", 0)
	})
	if int64(len(s.emergencies)) != settings.emergencyCount || s.emergencyCounters.started != settings.emergencyCount {
		t.Fatal("emergency onset did not create exactly the expected passenger record")
	}
	if settings.emergencies {
		if settings.faults && (!s.vehicles[0].faulted || s.vehicles[0].op.purpose != opService) {
			t.Fatal("faulted emergency did not retain the ordinary deferred purpose")
		}
		stationSwitchCommand(t, digest, s, publication, "duplicate-emergency", "", "pod already has an emergency", errPodEmergency, func() (string, error) {
			return s.Emergency("01", 0)
		})
		stationSwitchCommand(t, digest, s, publication, "disable-active-emergency", "", "1 emergencies are active", nil, func() (string, error) {
			return "", s.SetEmergencies(false)
		})
	}
	if s.incidentSerial != uint64(settings.faultCount+settings.emergencyCount) {
		t.Fatalf("incident serial=%d, want %d", s.incidentSerial, settings.faultCount+settings.emergencyCount)
	}
}

func stationSwitchClear(t *testing.T, digest hash.Hash, s *Simulation, publication *stationSwitchPublication, settings stationSwitchSettings) {
	t.Helper()
	id, text, identity := "i7.999", "faults are not enabled", errFaultsOff
	if settings.faults {
		if s.vehicles[0].Pod.Speed <= 0 || !s.vehicles[0].faulted || len(s.faults) != 1 {
			t.Fatal("early clear did not reach an active fault before actual rest")
		}
		stationSwitchCommand(t, digest, s, publication, "early-clear", "", "", nil, func() (string, error) {
			return "", s.ClearFault(settings.faultID)
		})
		if len(s.faults) != 0 || s.vehicles[0].faulted || s.faultCounters.cleared != 1 {
			t.Fatal("early clear did not end the active fault")
		}
		id, text, identity = settings.faultID, "unknown fault", errUnknownFault
	}
	stationSwitchCommand(t, digest, s, publication, "clear-removed-or-unknown", "", text, identity, func() (string, error) {
		return "", s.ClearFault(id)
	})
}

func stationSwitchRun(t *testing.T, digest hash.Hash, s *Simulation, settings stationSwitchSettings) {
	t.Helper()
	s.SetIncidentGeneration(7)
	if err := s.SetFaults(settings.faults, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEmergencies(settings.emergencies); err != nil {
		t.Fatal(err)
	}
	publication := &stationSwitchPublication{}
	s.monitor = func(current *Simulation) { stationSwitchPublished(t, current, publication) }
	stationSwitchObserve(t, digest, s)
	stationSwitchCommand(t, digest, s, publication, "ordinary-journey", "", "", nil, func() (string, error) {
		return "", s.RequestJourney("01", "market")
	})
	stationSwitchSteps(t, digest, s, 600)
	if s.tick != 600 || s.vehicles[0].Pod.Activity != Traveling || s.vehicles[0].Pod.Speed <= 0 || !s.vehicles[0].carriesPassengers() {
		t.Fatal("switch fixture did not reach positive-speed ordinary passenger travel at tick 600")
	}
	owned := 0
	for _, owner := range s.owners {
		if owner.isPod("01") {
			owned++
		}
	}
	if owned == 0 || s.vehicles[0].reservedThrough < 0 {
		t.Fatal("moving switch fixture lacks native owners or an admission frontier")
	}
	stationSwitchOnset(t, digest, s, publication, settings)
	stationSwitchSteps(t, digest, s, 60)
	stationSwitchClear(t, digest, s, publication, settings)
	stationSwitchSteps(t, digest, s, 300)
	if s.tick != 960 || s.faultCounters.started != settings.faultCount || s.faultCounters.cleared != settings.faultCount || s.emergencyCounters.started != settings.emergencyCount {
		t.Fatal("bounded continuation changed expected command counts or native tick")
	}
	stationSwitchEncode(t, digest, struct{ Publications int }{publication.calls})
}

func TestStationFeatureOffSwitchBaseline(t *testing.T) {
	stationSwitchCheckOmittedFields(t)
	// Native markers describe formats. Native operation switches gate commands.
	profiles := []stationSwitchProfile{
		{name: "none"},
		{name: "incident", contracts: FleetContracts{IncidentContract: IncidentV1Contract}},
		{name: "fault-without-incident", contracts: FleetContracts{FaultContract: FaultV1Contract}, refusal: "the fault contract requires the incident contract"},
		{name: "incident-fault", contracts: FleetContracts{IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract}},
		{name: "emergency-without-incident", contracts: FleetContracts{EmergencyContract: EmergencyV1Contract}, refusal: "the emergency contract requires the incident contract"},
		{name: "incident-emergency", contracts: FleetContracts{IncidentContract: IncidentV1Contract, EmergencyContract: EmergencyV1Contract}},
		{name: "fault-emergency-without-incident", contracts: FleetContracts{FaultContract: FaultV1Contract, EmergencyContract: EmergencyV1Contract}, refusal: "the fault contract requires the incident contract"},
		{name: "all", contracts: FleetContracts{IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, EmergencyContract: EmergencyV1Contract}},
	}
	settings := []stationSwitchSettings{
		{name: "both-off"},
		{name: "fault-on", faults: true, faultID: "i7.1", faultCount: 1},
		{name: "emergency-on", emergencies: true, emergencyID: "i7.1", emergencyCount: 1},
		{name: "both-on", faults: true, emergencies: true, faultID: "i7.1", emergencyID: "i7.2", faultCount: 1, emergencyCount: 1},
	}
	digest := sha256.New()
	valid, refused := 0, 0
	seen := make(map[struct {
		Contracts           FleetContracts
		Faults, Emergencies bool
	}]bool)
	for _, profile := range profiles {
		for _, switches := range settings {
			name := profile.name + "/" + switches.name
			key := struct {
				Contracts           FleetContracts
				Faults, Emergencies bool
			}{profile.contracts, switches.faults, switches.emergencies}
			if seen[key] {
				t.Fatalf("duplicate marker/switch input: %s", name)
			}
			seen[key] = true
			t.Run(name, func(t *testing.T) {
				row := sha256.New()
				stationSwitchEncode(t, row, struct {
					Name                string
					Contracts           FleetContracts
					Faults, Emergencies bool
					EvacuationSeconds   int
				}{name, profile.contracts, switches.faults, switches.emergencies, 300})
				s, err := NewFleetWithContracts(Example(), []Placement{{ID: "01", StationID: "harbor"}}, profile.contracts)
				if profile.refusal != "" {
					if err == nil || err.Error() != profile.refusal || s != nil {
						t.Fatalf("constructor refusal: simulation=%v error=%v, want nil and %q", s != nil, err, profile.refusal)
					}
					refused++
					stationSwitchEncode(t, row, struct{ ConstructorError string }{err.Error()})
				} else {
					if err != nil {
						t.Fatal(err)
					}
					view := s.Snapshot()
					if view.IncidentContract != profile.contracts.IncidentContract || view.FaultContract != profile.contracts.FaultContract || view.EmergencyContract != profile.contracts.EmergencyContract {
						t.Fatal("constructor changed independent native markers")
					}
					valid++
					stationSwitchRun(t, row, s, switches)
				}
				got := hex.EncodeToString(row.Sum(nil))
				t.Logf("switch row %s SHA-256 %s", name, got)
				stationSwitchEncode(t, digest, struct{ Name, Digest string }{name, got})
			})
		}
	}
	if len(profiles) != 8 || len(settings) != 4 || len(seen) != 32 || valid != 20 || refused != 12 {
		t.Fatalf("switch matrix coverage: profiles=%d settings=%d valid=%d refused=%d, want 8/4/20/12", len(profiles), len(settings), valid, refused)
	}
	const want = "ba5d0d301ea1d92d0b5cd7d94ac32bc8691f0e459d7f651a7735bd4fdc799b22"
	if got := hex.EncodeToString(digest.Sum(nil)); got != want {
		t.Fatalf("native switch boundary digest = %s, want %s", got, want)
	}
}
