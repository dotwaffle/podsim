package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"hash"
	"testing"
)

func stationBaselineRefusal(t *testing.T, digest hash.Hash, s *Simulation, name string, want error, command func() error) {
	t.Helper()
	before := sha256.New()
	stationBaselineObserve(t, before, s)
	err := command()
	if !errors.Is(err, want) {
		t.Fatalf("%s: error=%v, want %v", name, err, want)
	}
	after := sha256.New()
	stationBaselineObserve(t, after, s)
	if hex.EncodeToString(before.Sum(nil)) != hex.EncodeToString(after.Sum(nil)) {
		t.Fatalf("%s changed baseline state on refusal", name)
	}
	data, encodeErr := json.Marshal(struct{ Command, Error string }{name, err.Error()})
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if _, writeErr := digest.Write(data); writeErr != nil {
		t.Fatal(writeErr)
	}
	if _, writeErr := digest.Write([]byte{'\n'}); writeErr != nil {
		t.Fatal(writeErr)
	}
	stationBaselineObserve(t, digest, s)
}

type stationIncidentBaselineCase struct {
	name          string
	duration      int64
	evacuation    int
	clearTick     int
	emergency     bool
	wantEvacuated bool
	want          string
}

func stationIncidentBaselineCommand(t *testing.T, s *Simulation, digest hash.Hash, test stationIncidentBaselineCase, tick int, faultID string) string {
	t.Helper()
	if tick == 600 {
		if s.vehicles[0].Pod.Activity != Traveling || s.vehicles[0].Pod.Speed <= 0 {
			t.Fatal("fault fixture did not reach moving ordinary travel")
		}
		request := FaultRequest{PodID: "01"}
		if test.duration != 0 {
			request.DurationSeconds = &test.duration
		}
		var err error
		faultID, err = s.Fault(request)
		if err != nil {
			t.Fatal(err)
		}
		stationBaselineObserve(t, digest, s)
		stationBaselineRefusal(t, digest, s, "duplicate-fault", errPodFaulted, func() error {
			_, err := s.Fault(FaultRequest{PodID: "01"})
			return err
		})
		if test.emergency {
			if _, err := s.Emergency("01", 0); err != nil {
				t.Fatal(err)
			}
			stationBaselineObserve(t, digest, s)
		}
	}
	if test.clearTick != 0 && tick == test.clearTick {
		if !test.wantEvacuated && s.vehicles[0].Pod.Speed <= 0 {
			t.Fatal("moving-clear fixture reached rest before its clear")
		}
		if err := s.ClearFault(faultID); err != nil {
			t.Fatal(err)
		}
		stationBaselineObserve(t, digest, s)
		stationBaselineRefusal(t, digest, s, "clear-removed-fault", errUnknownFault, func() error { return s.ClearFault(faultID) })
	}
	return faultID
}

func TestStationIntegrationIncidentBaseline(t *testing.T) {
	for _, test := range []stationIncidentBaselineCase{
		{name: "moving-clear", evacuation: 300, clearTick: 660, want: "2e493f88cae65d438febb1a3c2a9afa952e3ae3436f40e6549a36a8db7a65970"},
		{name: "duration-expiry", duration: 1, evacuation: 300, want: "f1f702df241618dd4e1ad6cc3c40e9170e6fc222db372190ead9dc6038a1136a"},
		{name: "emergency-while-faulted", evacuation: 300, clearTick: 660, emergency: true, want: "181bda4495724c9a56e188921a4dcb7a6fdc99011851b9f1c7e908869d013207"},
		{name: "evacuated-recovery", evacuation: 0, clearTick: 1200, wantEvacuated: true, want: "b146278c2fafa71495d6467417a8cf8cd40828a8d4ce440c4db066590f59bb9c"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, err := NewFleetWithContracts(Example(), []Placement{{ID: "01", StationID: "harbor"}}, FleetContracts{
				IncidentContract: IncidentV1Contract, FaultContract: FaultV1Contract, EmergencyContract: EmergencyV1Contract,
			})
			if err != nil {
				t.Fatal(err)
			}
			s.SetIncidentGeneration(7)
			if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: test.evacuation}); err != nil {
				t.Fatal(err)
			}
			if err := s.SetEmergencies(true); err != nil {
				t.Fatal(err)
			}
			digest := sha256.New()
			stationBaselineObserve(t, digest, s)
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			stationBaselineObserve(t, digest, s)
			var faultID string
			for tick := range 18000 {
				faultID = stationIncidentBaselineCommand(t, s, digest, test, tick, faultID)
				s.Step()
				if err := s.CheckContract(); err != nil {
					t.Fatalf("tick %d: %v", s.tick, err)
				}
				stationBaselineObserve(t, digest, s)
			}
			if len(s.faults) != 0 || len(s.emergencies) != 0 || s.faultCounters.started != 1 || s.faultCounters.cleared != 1 {
				t.Fatalf("incident lifecycle incomplete: faults=%d emergencies=%d counters=%+v", len(s.faults), len(s.emergencies), s.faultCounters)
			}
			if (s.faultCounters.evacuations != 0) != test.wantEvacuated {
				t.Fatalf("evacuations=%d, want evacuated=%t", s.faultCounters.evacuations, test.wantEvacuated)
			}
			if s.vehicles[0].withdrawn != 0 || s.vehicles[0].op.purpose != opService || s.vehicles[0].carriesPassengers() {
				t.Fatal("incident recovery retained a hold, purpose, or active rider")
			}
			if test.wantEvacuated && s.vehicles[0].Pod.StationID != "market" {
				t.Fatal("empty recovery did not retain its original destination")
			}
			got := hex.EncodeToString(digest.Sum(nil))
			if got != test.want {
				t.Fatalf("legacy incident digest = %s, want %s", got, test.want)
			}
		})
	}
}
