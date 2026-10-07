package scenarios

import (
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/dotwaffle/podsim/internal/project"
)

const londonFullDemandProfileID = "tfl-numbat-2024-twt"

//go:embed data/london-full-od-2024.csv
var londonFullDemandCSV string

var (
	londonFullDemandOnce sync.Once
	londonFullDemand     []LondonDemandBand
	londonFullProfile    project.DemandProfile
)

// LondonFullDemand returns owned normalized demand for the six observed bands.
// The 2024 source has no Early or Night rows. Journeys can use other modes
// between their Tube endpoints.
func LondonFullDemand() []LondonDemandBand {
	londonFullDemandOnce.Do(loadLondonFullDemand)
	result := append([]LondonDemandBand(nil), londonFullDemand...)
	for index := range result {
		result[index].Flows = append([]LondonODFlow(nil), result[index].Flows...)
	}
	return result
}

func londonFullDemandProfile() project.DemandProfile {
	londonFullDemandOnce.Do(loadLondonFullDemand)
	result := londonFullProfile
	result.Bands = append([]project.DemandBand(nil), result.Bands...)
	result.Flows = append([]project.DemandFlow(nil), result.Flows...)
	for index := range result.Flows {
		result.Flows[index].Weights = append([]float64(nil), result.Flows[index].Weights...)
	}
	return result
}

func loadLondonFullDemand() {
	source, err := decodeLondonFullSource()
	if err != nil {
		panic(err)
	}
	londonFullDemand, londonFullProfile, err = readLondonFullDemand(londonFullDemandCSV, source)
	if err != nil {
		panic(err)
	}
	if len(source.Stations) != 269 || len(londonFullProfile.Flows) != 60996 {
		panic("LondonFull source must contain 269 sites and 60996 OD pairs")
	}
}

func readLondonFullDemand(data string, source londonSource) ([]LondonDemandBand, project.DemandProfile, error) {
	profile := project.DemandProfile{ID: londonFullDemandProfileID, Name: "LondonFull 2024 Tube-endpoint journeys", Bands: []project.DemandBand{
		{ID: "morning", Name: "Morning", StartMinute: 300, DurationMinutes: 120},
		{ID: "am-peak", Name: "AM peak", StartMinute: 420, DurationMinutes: 180},
		{ID: "interpeak", Name: "Interpeak", StartMinute: 600, DurationMinutes: 360},
		{ID: "pm-peak", Name: "PM peak", StartMinute: 960, DurationMinutes: 180},
		{ID: "evening", Name: "Evening", StartMinute: 1140, DurationMinutes: 180},
		{ID: "late", Name: "Late", StartMinute: 1320, DurationMinutes: 150},
	}}
	valid := make(map[string]bool, len(source.Stations))
	for _, station := range source.Stations {
		valid[station.ID] = true
	}
	reader := csv.NewReader(strings.NewReader(data))
	header, err := reader.Read()
	if err != nil || strings.Join(header, ",") != "from,to,early,morning,am_peak,inter_peak,pm_peak,evening,late,night" {
		return nil, project.DemandProfile{}, fmt.Errorf("invalid LondonFull demand header: %q", header)
	}
	seen := make(map[[2]string]bool)
	for row := 2; ; row++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, project.DemandProfile{}, fmt.Errorf("read LondonFull demand row %d: %w", row, err)
		}
		key := [2]string{record[0], record[1]}
		if !valid[key[0]] || !valid[key[1]] || key[0] == key[1] || seen[key] {
			return nil, project.DemandProfile{}, fmt.Errorf("invalid or duplicate LondonFull demand pair at row %d", row)
		}
		seen[key] = true
		weights, err := londonFullWeights(record[2:])
		if err != nil {
			return nil, project.DemandProfile{}, fmt.Errorf("LondonFull demand row %d: %w", row, err)
		}
		profile.Flows = append(profile.Flows, project.DemandFlow{From: key[0], To: key[1], Weights: weights})
	}
	bands := make([]LondonDemandBand, len(profile.Bands))
	for index, band := range profile.Bands {
		bands[index] = LondonDemandBand{Name: band.Name, StartMinute: band.StartMinute, DurationMinutes: band.DurationMinutes}
		for _, flow := range profile.Flows {
			weight := flow.Weights[index]
			if weight == 0 {
				continue
			}
			bands[index].ObservedJourneys += weight
			bands[index].Flows = append(bands[index].Flows, LondonODFlow{From: flow.From, To: flow.To, Share: weight})
		}
		if bands[index].ObservedJourneys <= 0 || math.IsInf(bands[index].ObservedJourneys, 0) {
			return nil, project.DemandProfile{}, fmt.Errorf("LondonFull demand band %s has no finite positive total", band.ID)
		}
		for flow := range bands[index].Flows {
			bands[index].Flows[flow].Share /= bands[index].ObservedJourneys
		}
	}
	return bands, profile, nil
}

// londonFullWeights reads the eight source weights of a pair, and returns
// the six observed weights, each rounded by roundDemandWeight.
func londonFullWeights(fields []string) ([]float64, error) {
	if len(fields) != 8 {
		return nil, errors.New("expected eight source weights")
	}
	var values [8]float64
	positive := false
	for band := range values {
		weight, err := strconv.ParseFloat(fields[band], 64)
		if err != nil || weight < 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
			return nil, fmt.Errorf("invalid weight in source band %d", band+1)
		}
		values[band] = roundDemandWeight(weight)
		positive = positive || weight > 0
	}
	if values[0] != 0 || values[7] != 0 {
		return nil, errors.New("empty source bands must have zero weight")
	}
	if !positive {
		return nil, errors.New("OD pair has no positive weight")
	}
	return append([]float64(nil), values[1:7]...), nil
}
