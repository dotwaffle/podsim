package scenarios

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func londonDemandSchedule(seed uint64, band LondonDemandBand, count int) []scheduledRequest {
	const interval = 5 * sim.TicksPerSecond
	rng := rand.New(rand.NewPCG(seed, ^seed))
	schedule := make([]scheduledRequest, count)
	for index := range schedule {
		target := rng.Float64()
		selected := band.Flows[len(band.Flows)-1]
		cumulative := 0.0
		for _, flow := range band.Flows {
			cumulative += flow.Share
			if target < cumulative {
				selected = flow
				break
			}
		}
		schedule[index] = scheduledRequest{
			tick: int64((index + 1) * interval), origin: selected.From, destination: selected.To,
		}
	}
	return schedule
}

func TestLondonDemandBandsAreNormalized(t *testing.T) {
	t.Parallel()
	bands := LondonDemand()
	want := []struct {
		name            string
		start, duration int
	}{
		{name: "Early", start: 180, duration: 120},
		{name: "Morning", start: 300, duration: 120},
		{name: "AM peak", start: 420, duration: 180},
		{name: "Interpeak", start: 600, duration: 360},
		{name: "PM peak", start: 960, duration: 180},
		{name: "Evening", start: 1140, duration: 180},
		{name: "Late", start: 1320, duration: 150},
		{name: "Night", start: 30, duration: 150},
	}
	if len(bands) != len(want) {
		t.Fatalf("got %d demand bands", len(bands))
	}
	config := London()
	if config.Demand.Pattern != "profile" || config.Demand.Profile != londonDemandProfileID || config.Demand.Band != "am-peak" || len(config.DemandProfiles) != 1 {
		t.Fatalf("London project demand = %+v, profiles=%d", config.Demand, len(config.DemandProfiles))
	}
	profile := config.DemandProfiles[0]
	if len(profile.Bands) != len(want) || len(profile.Flows) != 8474 {
		t.Fatalf("London profile has %d bands and %d flows", len(profile.Bands), len(profile.Flows))
	}
	valid := make(map[string]bool)
	for _, station := range project.PassengerStations(config.Network) {
		valid[station.ID] = true
	}
	origins := make(map[string]bool)
	for index, band := range bands {
		if band.Name != want[index].name || band.StartMinute != want[index].start || band.DurationMinutes != want[index].duration {
			t.Fatalf("band %d = %+v", index+1, band)
		}
		share := 0.0
		for _, flow := range band.Flows {
			if !valid[flow.From] || !valid[flow.To] || flow.From == flow.To || flow.Share <= 0 {
				t.Fatalf("invalid flow in %q: %+v", band.Name, flow)
			}
			origins[flow.From] = true
			share += flow.Share
		}
		if math.Abs(share-1) > 1e-9 {
			t.Fatalf("band %q shares sum to %.12f", band.Name, share)
		}
		profileTotal := 0.0
		for _, flow := range profile.Flows {
			profileTotal += flow.Weights[index]
		}
		if math.Abs(profileTotal-band.ObservedJourneys) > 1e-6 {
			t.Fatalf("band %q profile total %.3f, want %.3f", band.Name, profileTotal, band.ObservedJourneys)
		}
	}
	if len(origins) != 94 {
		t.Fatalf("got demand for %d origins, want 94", len(origins))
	}
}

func TestLondonDemandPreservesStationScale(t *testing.T) {
	t.Parallel()
	band := LondonDemand()[2]
	outbound := make(map[string]float64)
	for _, flow := range band.Flows {
		outbound[flow.From] += flow.Share
	}
	const (
		euston = "940GZZLUEUS"
		goodge = "940GZZLUGDG"
	)
	if outbound[euston] < 20*outbound[goodge] {
		t.Fatalf("AM peak Euston share %.6f does not dominate Goodge Street %.6f", outbound[euston], outbound[goodge])
	}
}

func TestLondonDemandScheduleIsRepeatable(t *testing.T) {
	t.Parallel()
	band := LondonDemand()[2]
	first := londonDemandSchedule(20260922, band, 20)
	second := londonDemandSchedule(20260922, band, 20)
	other := londonDemandSchedule(20260923, band, 20)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical London demand seeds produced different schedules")
	}
	if reflect.DeepEqual(first, other) {
		t.Fatal("different London demand seeds produced the same schedule")
	}
}

func TestLondonAMPeakSampleCompletes(t *testing.T) {
	t.Parallel()
	config := London()
	const requestCount = 40
	schedule := londonDemandSchedule(20260922, LondonDemand()[2], requestCount)
	result := runQualification(t, qualificationInput{config: config, schedule: schedule, checkSafety: true})
	if result.state.Completed != requestCount || len(result.state.Pending) != 0 {
		t.Fatalf("London sample did not finish: completed=%d remaining=%d", result.state.Completed, result.state.Submitted-result.state.Completed)
	}
	for _, vehicle := range result.state.Vehicles {
		if vehicle.Pod.StationID != "" && vehicle.Pod.BerthID == "" {
			t.Fatalf("stationary pod has no berth: %+v", vehicle.Pod)
		}
	}
	t.Logf("preset=london source=%s source_url=%s band=am_peak seed=%d interval_ticks=%d requests=%d schedule_sha256=%s completed=%d wait_average_seconds=%.3f wait_max_seconds=%.3f ticks=%d",
		londonDemandSourceDay, londonDemandSourceURL, uint64(20260922), 5*sim.TicksPerSecond,
		requestCount, result.fingerprint, result.state.Completed, result.state.Wait.AverageSeconds,
		result.state.Wait.MaxSeconds, result.state.Tick)
}
