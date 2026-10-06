package sim

import (
	"errors"
	"math"
	"testing"
)

// TestFaultRequestRefusals checks the error of each refused fault request
// and that a refusal changes nothing: the state equals a clone from before
// the call, and the monitor does not run. In the fixture, pod 02 cruises
// at the start of the lane "return", with its body in cell 0 and its
// grants to the end of cell 1.
func TestFaultRequestRefusals(t *testing.T) {
	t.Parallel()
	seconds := func(value int64) *int64 { return &value }
	at := func(value float64) *float64 { return &value }
	free := FaultRequest{LaneID: "s3-link", FromMeters: at(70), ToMeters: at(80)}
	tests := []struct {
		name    string
		request FaultRequest
		want    error
		// prepare changes the fixture before the request.
		prepare func(s *Simulation)
	}{
		{"faults off", free, errFaultsOff, func(s *Simulation) { s.faultsOn = false }},
		{"faults off before the shape", FaultRequest{}, errFaultsOff, func(s *Simulation) { s.faultsOn = false }},
		{"zero duration", FaultRequest{PodID: "01", DurationSeconds: seconds(0)}, errFaultDuration, nil},
		{"negative duration", FaultRequest{PodID: "01", DurationSeconds: seconds(-1)}, errFaultDuration, nil},
		{"duration above the limit", FaultRequest{PodID: "01", DurationSeconds: seconds(maxFaultSeconds + 1)}, errFaultDuration, nil},
		{"duration before the shape", FaultRequest{DurationSeconds: seconds(0)}, errFaultDuration, nil},
		{"no target", FaultRequest{}, errFaultTarget, nil},
		{"pod and lane", FaultRequest{PodID: "01", LaneID: "s3-link", FromMeters: at(70), ToMeters: at(80)}, errFaultTarget, nil},
		{"pod and lane without a segment", FaultRequest{PodID: "01", LaneID: "s3-link"}, errFaultTarget, nil},
		{"pod with a start", FaultRequest{PodID: "01", FromMeters: at(0)}, errFaultTarget, nil},
		{"pod with an end", FaultRequest{PodID: "01", ToMeters: at(1)}, errFaultTarget, nil},
		{"unknown pod", FaultRequest{PodID: "99"}, errUnknownPod, nil},
		{"faulted pod", FaultRequest{PodID: "01"}, errPodFaulted, func(s *Simulation) {
			if _, err := s.startPodFault(s.findVehicle("01"), 0); err != nil {
				t.Fatal(err)
			}
		}},
		{"incident limit", FaultRequest{PodID: "01"}, errIncidentLimit, func(s *Simulation) { s.incidentSerial = math.MaxUint64 }},
		{"unknown lane", FaultRequest{LaneID: "nowhere", FromMeters: at(0), ToMeters: at(1)}, errUnknownLane, nil},
		{"lane without a start", FaultRequest{LaneID: "s3-link", ToMeters: at(40)}, errDebrisSegment, nil},
		{"lane without an end", FaultRequest{LaneID: "s3-link", FromMeters: at(70)}, errDebrisSegment, nil},
		{"empty segment", FaultRequest{LaneID: "s3-link", FromMeters: at(70), ToMeters: at(70)}, errDebrisSegment, nil},
		{"debris on a pod body", FaultRequest{LaneID: "return", FromMeters: at(0), ToMeters: at(1)}, errDebrisOverlap, nil},
		{"debris on a grant", FaultRequest{LaneID: "return", FromMeters: at(45), ToMeters: at(46)}, errDebrisClaim, nil},
		{"debris limit", free, errDebrisLimit, func(s *Simulation) {
			if _, err := fillDebris(s); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := debrisFleet(t)
			returnTraveler(t, s)
			if test.prepare != nil {
				test.prepare(s)
			}
			before := s.Clone()
			observed := 0
			s.monitor = func(*Simulation) { observed++ }
			id, err := s.Fault(test.request)
			if !errors.Is(err, test.want) || id != "" {
				t.Fatalf("Fault = %q, %v, want %v", id, err, test.want)
			}
			if observed != 0 {
				t.Fatalf("the monitor ran %d times after a refusal", observed)
			}
			s.monitor = nil
			if !sameState(before, s) {
				t.Fatal("a refused request changed the state")
			}
		})
	}
}

// TestFaultRequestStarts checks that a pod request and a lane request
// start their faults with the requested duration, return the fault ID,
// and run the monitor once. ClearFault ends each fault and runs the
// monitor once, and a second clear of the same ID is refused.
func TestFaultRequestStarts(t *testing.T) {
	t.Parallel()
	s := debrisFleet(t)
	s.SetIncidentGeneration(3)
	observed := 0
	s.monitor = func(s *Simulation) {
		observed++
		if err := s.CheckContract(); err != nil {
			t.Fatal(err)
		}
	}
	shortest, longest := int64(1), int64(maxFaultSeconds)
	from, to := 70.0, 80.0
	pod, err := s.Fault(FaultRequest{PodID: "01", DurationSeconds: &shortest})
	if err != nil {
		t.Fatal(err)
	}
	debris, err := s.Fault(FaultRequest{LaneID: "s3-link", FromMeters: &from, ToMeters: &to, DurationSeconds: &longest})
	if err != nil {
		t.Fatal(err)
	}
	untimed, err := s.Fault(FaultRequest{PodID: "02"})
	if err != nil {
		t.Fatal(err)
	}
	if pod != "i3.1" || debris != "i3.2" || untimed != "i3.3" || observed != 3 {
		t.Fatalf("fault IDs %s, %s and %s after %d monitor runs", pod, debris, untimed, observed)
	}
	want := []faultRecord{
		{generation: 3, serial: 1, kind: podFault, end: TicksPerSecond, pod: s.vehicleIndexes["01"]},
		{generation: 3, serial: 2, kind: debrisFault, end: maxFaultSeconds * TicksPerSecond, lane: s.graph.lanes["s3-link"], from: 70, to: 80},
		{generation: 3, serial: 3, kind: podFault, pod: s.vehicleIndexes["02"]},
	}
	for index, record := range s.faults {
		if record != want[index] {
			t.Fatalf("record %d is %+v, want %+v", index, record, want[index])
		}
	}
	for _, id := range []string{pod, debris, untimed} {
		if err := s.ClearFault(id); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.faults) != 0 || observed != 6 {
		t.Fatalf("%d records after the clears and %d monitor runs", len(s.faults), observed)
	}
	if err := s.ClearFault(pod); !errors.Is(err, errUnknownFault) || observed != 6 {
		t.Fatalf("second clear: %v after %d monitor runs", err, observed)
	}
	s.faultsOn = false
	if err := s.ClearFault(pod); !errors.Is(err, errFaultsOff) {
		t.Fatalf("clear with faults off: %v", err)
	}
}

// TestDemoEndsFaults checks that faults on the demo routes do not refuse
// the traffic demo, and that the demo ends them. Pod 01 starts the first
// demo journey, and the debris blocks bypass-in, the trigger lane of the
// demo.
func TestDemoEndsFaults(t *testing.T) {
	t.Parallel()
	s := newTraffic(t)
	s.incidentContract = IncidentV1Contract
	if err := s.SetFaults(true, FaultSettings{EvacuationSeconds: 300}); err != nil {
		t.Fatal(err)
	}
	from, to := 80.0, 82.0
	for _, request := range []FaultRequest{{PodID: "01"}, {LaneID: "bypass-in", FromMeters: &from, ToMeters: &to}} {
		if _, err := s.Fault(request); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.StartDemo(); err != nil {
		t.Fatalf("StartDemo with faults: %v", err)
	}
	if len(s.faults) != 0 || s.faultsOn || !s.DemoRunning() {
		t.Fatalf("after the demo starts: %d faults, faults on %t, demo %t", len(s.faults), s.faultsOn, s.DemoRunning())
	}
}
