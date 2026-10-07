package scenarios

import (
	"encoding/csv"
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestLondonFullDemandRoundsWeights checks that each profile weight is the
// source weight rounded to demandWeightDigits significant digits.
func TestLondonFullDemandRoundsWeights(t *testing.T) {
	t.Parallel()
	bands := LondonFullDemand()
	profile := londonFullDemandProfile()
	if len(bands) != 6 || len(profile.Flows) != 76567 {
		t.Fatalf("bands=%d flows=%d", len(bands), len(profile.Flows))
	}
	starts := []int{300, 420, 600, 960, 1140, 1320}
	durations := []int{120, 180, 360, 180, 180, 150}
	expected := []string{"morning", "am-peak", "interpeak", "pm-peak", "evening", "late"}
	rows, err := csv.NewReader(strings.NewReader(londonFullDemandCSV)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(profile.Flows)+1 {
		t.Fatal("demand rows truncated")
	}
	origins := map[string]bool{}
	destinations := map[string]bool{}
	for index, flow := range profile.Flows {
		row := rows[index+1]
		if flow.From != row[0] || flow.To != row[1] || flow.From == flow.To {
			t.Fatalf("pair changed at %d", index)
		}
		origins[flow.From] = true
		destinations[flow.To] = true
		for band := range bands {
			weight, err := strconv.ParseFloat(row[band+3], 64)
			if err != nil {
				t.Fatal(err)
			}
			if flow.Weights[band] != roundDemandWeight(weight) {
				t.Fatalf("weight changed at %d band %d", index, band)
			}
		}
	}
	if len(origins) != 309 || len(destinations) != 309 {
		t.Fatalf("demand coverage origins=%d destinations=%d", len(origins), len(destinations))
	}
	for index, band := range bands {
		if profile.Bands[index].ID != expected[index] || band.StartMinute != starts[index] || band.DurationMinutes != durations[index] {
			t.Fatal("incorrect full band metadata")
		}
		share := 0.0
		for _, flow := range band.Flows {
			if flow.Share <= 0 || math.IsNaN(flow.Share) || math.IsInf(flow.Share, 0) {
				t.Fatal("invalid normalized share")
			}
			share += flow.Share
		}
		if math.Abs(share-1) > 1e-10 {
			t.Fatalf("band %s normalized sum=%g", band.Name, share)
		}
		total := 0.0
		for _, flow := range profile.Flows {
			total += flow.Weights[index]
		}
		if total != band.ObservedJourneys {
			t.Fatalf("observed total changed for %s", band.Name)
		}
	}
	shareBefore := bands[0].Flows[0].Share
	weightBefore := profile.Flows[0].Weights[0]
	bands[0].Flows[0].Share = 99
	bands[0].Name = "changed"
	profile.Bands[0].Name = "changed"
	profile.Flows[0].Weights[0] = 99
	if LondonFullDemand()[0].Flows[0].Share != shareBefore || londonFullDemandProfile().Flows[0].Weights[0] != weightBefore {
		t.Fatal("demand exposes cached slices")
	}
	if LondonFullDemand()[0].Name == "changed" || londonFullDemandProfile().Bands[0].Name == "changed" {
		t.Fatal("demand metadata is shared")
	}
}

func TestLondonFullDemandRejectsInvalidRows(t *testing.T) {
	t.Parallel()
	const header = "from,to,early,morning,am_peak,inter_peak,pm_peak,evening,late,night\n"
	const valid = "a,b,0,1,2,3,4,5,6,0\n"
	source := londonSource{Stations: []londonSourceStation{{ID: "a"}, {ID: "b"}}}
	if _, _, err := readLondonFullDemand(header+valid, source); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, data string }{
		{"header", "bad\n" + valid}, {"short", header + "a,b,1\n"}, {"same site", header + strings.Replace(valid, "a,b", "a,a", 1)},
		{"unknown site", header + strings.Replace(valid, "a,b", "a,c", 1)}, {"duplicate", header + valid + valid},
		{"negative", header + "a,b,0,-1,2,3,4,5,6,0\n"}, {"nonfinite", header + "a,b,0,NaN,2,3,4,5,6,0\n"},
		{"infinite", header + "a,b,0,Inf,2,3,4,5,6,0\n"}, {"nonnumeric", header + "a,b,0,x,2,3,4,5,6,0\n"},
		{"empty Early", header + "a,b,1,1,2,3,4,5,6,0\n"}, {"empty Night", header + "a,b,0,1,2,3,4,5,6,1\n"},
		{"empty pair", header + "a,b,0,0,0,0,0,0,0,0\n"}, {"empty observed band", header + "a,b,0,0,2,3,4,5,6,0\n"},
		{"overflow total", header + "a,b,0,1e308,1,1,1,1,1,0\nb,a,0,1e308,1,1,1,1,1,0\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := readLondonFullDemand(test.data, source); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
}
