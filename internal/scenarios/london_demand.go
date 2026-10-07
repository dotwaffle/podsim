package scenarios

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	"github.com/dotwaffle/podsim/internal/project"
)

const (
	londonDemandSourceURL = "https://crowding.data.tfl.gov.uk/NUMBAT/NUMBAT%202019/NBT19_OD_data/NBT19MTT2b_od__LU_tb_wf.csv"
	londonDemandSourceDay = "2019 midweek"
	londonDemandProfileID = "tfl-numbat-2019-midweek"
)

//go:embed data/london-od-2019.csv
var londonDemandCSV string

// demandWeightDigits is the number of significant digits of each weight
// of a generated demand profile. The source matrices give up to eight
// digits. Four digits keep each weight within 0.05 percent of the source
// value, and shorten the LondonFull flows from 3.8 MB to 2.7 MB of JSON.
const demandWeightDigits = 4

// roundDemandWeight rounds a source weight to demandWeightDigits
// significant digits. The decimal text of the result has at most that
// many digits, so the project JSON writes it in a short form. A positive
// weight stays positive.
func roundDemandWeight(weight float64) float64 {
	rounded, err := strconv.ParseFloat(strconv.FormatFloat(weight, 'g', demandWeightDigits, 64), 64)
	if err != nil {
		panic(fmt.Sprintf("round demand weight %g: %v", weight, err))
	}
	return rounded
}

// LondonODFlow is one normalized origin-destination probability.
type LondonODFlow struct {
	From  string
	To    string
	Share float64
}

// LondonDemandBand describes one source time band and its modeled OD demand.
type LondonDemandBand struct {
	Name             string
	StartMinute      int
	DurationMinutes  int
	ObservedJourneys float64
	Flows            []LondonODFlow
}

var (
	londonDemandOnce sync.Once
	londonDemand     []LondonDemandBand
	londonProfile    project.DemandProfile
)

// LondonCentralDemand returns the normalized 2019 midweek OD demand for the preset.
func LondonCentralDemand() []LondonDemandBand {
	londonDemandOnce.Do(loadLondonDemand)
	result := make([]LondonDemandBand, len(londonDemand))
	copy(result, londonDemand)
	for index := range result {
		result[index].Flows = append([]LondonODFlow(nil), londonDemand[index].Flows...)
	}
	return result
}

func londonDemandProfile() project.DemandProfile {
	londonDemandOnce.Do(loadLondonDemand)
	result := londonProfile
	result.Bands = append([]project.DemandBand(nil), londonProfile.Bands...)
	result.Flows = append([]project.DemandFlow(nil), londonProfile.Flows...)
	for index := range result.Flows {
		result.Flows[index].Weights = append([]float64(nil), londonProfile.Flows[index].Weights...)
	}
	return result
}

func loadLondonDemand() {
	londonDemand, londonProfile = mustLondonDemand()
}

func mustLondonDemand() ([]LondonDemandBand, project.DemandProfile) {
	bands := []LondonDemandBand{
		{Name: "Early", StartMinute: 3 * 60, DurationMinutes: 2 * 60},
		{Name: "Morning", StartMinute: 5 * 60, DurationMinutes: 2 * 60},
		{Name: "AM peak", StartMinute: 7 * 60, DurationMinutes: 3 * 60},
		{Name: "Interpeak", StartMinute: 10 * 60, DurationMinutes: 6 * 60},
		{Name: "PM peak", StartMinute: 16 * 60, DurationMinutes: 3 * 60},
		{Name: "Evening", StartMinute: 19 * 60, DurationMinutes: 3 * 60},
		{Name: "Late", StartMinute: 22 * 60, DurationMinutes: 150},
		{Name: "Night", StartMinute: 30, DurationMinutes: 150},
	}
	bandIDs := []string{"early", "morning", "am-peak", "interpeak", "pm-peak", "evening", "late", "night"}
	profile := project.DemandProfile{ID: londonDemandProfileID, Name: "London Underground 2019 midweek"}
	for index, band := range bands {
		profile.Bands = append(profile.Bands, project.DemandBand{
			ID: bandIDs[index], Name: band.Name, StartMinute: band.StartMinute, DurationMinutes: band.DurationMinutes,
		})
	}
	validStations := make(map[string]bool)
	var source londonSource
	if err := decodeLondonSource(&source); err != nil {
		panic(err)
	}
	for _, station := range source.Stations {
		validStations[station.ID] = true
	}
	reader := csv.NewReader(strings.NewReader(londonDemandCSV))
	header, err := reader.Read()
	if err != nil {
		panic(fmt.Errorf("read London demand header: %w", err))
	}
	if strings.Join(header, ",") != "from,to,early,morning,am_peak,inter_peak,pm_peak,evening,late,night" {
		panic(fmt.Sprintf("unexpected London demand header %q", header))
	}
	for row := 2; ; row++ {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			panic(fmt.Errorf("read London demand row %d: %w", row, readErr))
		}
		if len(record) != 10 || !validStations[record[0]] || !validStations[record[1]] || record[0] == record[1] {
			panic(fmt.Sprintf("invalid London demand row %d", row))
		}
		weights := make([]float64, len(bands))
		for index := range bands {
			weight, parseErr := strconv.ParseFloat(record[index+2], 64)
			if parseErr != nil || weight < 0 {
				panic(fmt.Sprintf("invalid London demand weight at row %d, band %d", row, index+1))
			}
			if weight == 0 {
				continue
			}
			weight = roundDemandWeight(weight)
			weights[index] = weight
			bands[index].ObservedJourneys += weight
			bands[index].Flows = append(bands[index].Flows, LondonODFlow{From: record[0], To: record[1], Share: weight})
		}
		profile.Flows = append(profile.Flows, project.DemandFlow{From: record[0], To: record[1], Weights: weights})
	}
	for index := range bands {
		if bands[index].ObservedJourneys <= 0 {
			panic(fmt.Sprintf("London demand band %q is empty", bands[index].Name))
		}
		for flowIndex := range bands[index].Flows {
			bands[index].Flows[flowIndex].Share /= bands[index].ObservedJourneys
		}
	}
	return bands, profile
}
