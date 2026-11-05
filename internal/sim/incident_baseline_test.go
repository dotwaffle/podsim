package sim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"hash"
	"testing"
)

func baselineRefusal(t *testing.T, digest hash.Hash, s *Simulation, name string, want error, command func() error) {
	t.Helper()
	before := sha256.New()
	baselineObserve(t, before, s)
	err := command()
	if !errors.Is(err, want) {
		t.Fatalf("%s: error=%v, want %v", name, err, want)
	}
	after := sha256.New()
	baselineObserve(t, after, s)
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
	baselineObserve(t, digest, s)
}

type incidentBaselineCase struct {
	name          string
	duration      int64
	evacuation    int
	clearTick     int
	emergency     bool
	wantEvacuated bool
	want          string
}

func incidentBaselineCommand(t *testing.T, s *Simulation, digest hash.Hash, test incidentBaselineCase, tick int, faultID string) string {
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
		baselineObserve(t, digest, s)
		baselineRefusal(t, digest, s, "duplicate-fault", errPodFaulted, func() error {
			_, err := s.Fault(FaultRequest{PodID: "01"})
			return err
		})
		if test.emergency {
			if _, err := s.Emergency("01", 0); err != nil {
				t.Fatal(err)
			}
			baselineObserve(t, digest, s)
		}
	}
	if test.clearTick != 0 && tick == test.clearTick {
		if !test.wantEvacuated && s.vehicles[0].Pod.Speed <= 0 {
			t.Fatal("moving-clear fixture reached rest before its clear")
		}
		if err := s.ClearFault(faultID); err != nil {
			t.Fatal(err)
		}
		baselineObserve(t, digest, s)
		baselineRefusal(t, digest, s, "clear-removed-fault", errUnknownFault, func() error { return s.ClearFault(faultID) })
	}
	return faultID
}

func TestIncidentBaseline(t *testing.T) {
	for _, test := range []incidentBaselineCase{
		{name: "moving-clear", evacuation: 300, clearTick: 660, want: "b5c467389cb05c3b2e22cc4bbbc8d3223960e1095b2c4d37366d65858448565e"},
		{name: "duration-expiry", duration: 1, evacuation: 300, want: "09d54d5710931f83dd38d05b320590681897891b083cd52d0d6b44e354249f03"},
		{name: "emergency-while-faulted", evacuation: 300, clearTick: 660, emergency: true, want: "551837e3993f65d7593a312e86f1284876fb808857ac2fe0a5e9f0d2454a505d"},
		{name: "evacuated-recovery", evacuation: 0, clearTick: 1200, wantEvacuated: true, want: "cbea0bdffdf5b33d4541f2920d1076d28956ef31293c27848c42a48f2516d277"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, err := NewFleet(Example(), []Placement{{ID: "01", StationID: "harbor"}})
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
			baselineObserve(t, digest, s)
			if err := s.RequestJourney("01", "market"); err != nil {
				t.Fatal(err)
			}
			baselineObserve(t, digest, s)
			var faultID string
			for tick := range 18000 {
				faultID = incidentBaselineCommand(t, s, digest, test, tick, faultID)
				s.Step()
				if err := s.CheckContract(); err != nil {
					t.Fatalf("tick %d: %v", s.tick, err)
				}
				baselineObserve(t, digest, s)
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
				t.Fatalf("incident baseline digest = %s, want %s", got, test.want)
			}
		})
	}
}
