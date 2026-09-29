package scenarios

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

//go:embed data/london-full-tube.json
var londonFullSourceJSON []byte

var (
	londonFullOnce   sync.Once
	londonFullPreset project.Config
)

// LondonFull returns an owned copy of the 269-site LondonFull preset.
// Its demand contains 2024 network journeys filtered to Tube endpoints.
func LondonFull() project.Config {
	londonFullOnce.Do(func() {
		source, err := decodeLondonFullSource()
		if err != nil {
			panic(err)
		}
		capacity, err := DefaultLondonFullOptions().resolve(source)
		if err != nil {
			panic(err)
		}
		londonFullPreset, err = londonFullConfig(source, capacity, true)
		if err != nil {
			panic(err)
		}
	})
	return project.Clone(londonFullPreset)
}

// DefaultLondonFullOptions returns owned capacity settings for LondonFull.
// It gives each passenger site two berths and one pod, then allocates 100
// extra berths by demand share. Bank keeps two berths for layout clearance.
// Berths overrides take precedence over StationBerths, as in LondonOptions.
func DefaultLondonFullOptions() LondonOptions {
	londonFullDemandOnce.Do(loadLondonFullDemand)
	options := DefaultLondonOptions()
	options.Berths = londonFullBerths(londonFullProfile)
	return options
}

// LondonFullWith builds LondonFull with the given capacity settings.
// It checks project limits and rejects hard layout conflicts.
func LondonFullWith(options LondonOptions) (project.Config, error) {
	source, err := decodeLondonFullSource()
	if err != nil {
		return project.Config{}, err
	}
	capacity, err := options.resolve(source)
	if err != nil {
		return project.Config{}, err
	}
	defaults, err := DefaultLondonFullOptions().resolve(source)
	if err != nil {
		return project.Config{}, err
	}
	if capacity.pitch == defaults.pitch && slices.Equal(capacity.berths, defaults.berths) && slices.Equal(capacity.pods, defaults.pods) {
		return LondonFull(), nil
	}
	return londonFullConfig(source, capacity, false)
}

func decodeLondonFullSource() (londonSource, error) {
	var source londonSource
	if err := json.Unmarshal(londonFullSourceJSON, &source); err != nil {
		return londonSource{}, fmt.Errorf("decode LondonFull source: %w", err)
	}
	return source, nil
}

func londonFullConfig(source londonSource, capacity londonCapacity, defaultCapacity bool) (project.Config, error) {
	// These full-only headings clear the Bank and Mansion House guideways.
	headings := map[string]float64{"940GZZLUBNK": 234 * math.Pi / 180, "940GZZLUMSH": 56 * math.Pi / 180}
	network, err := londonNetworkWithHeadings(source, capacity, headings)
	if err != nil {
		return project.Config{}, err
	}
	if err := layoutError(network, auditLondonLayout(network, newLondonAuditInput(source))); err != nil {
		return project.Config{}, err
	}
	name := "Full London Underground-derived PRT"
	if !defaultCapacity {
		name += customCapacitySuffix
	}
	config := project.Config{
		Version: 1, Name: name, Network: network,
		Fleet:          londonFleet(network, capacity.pods),
		Demand:         project.DemandConfig{PerMinute: 20, Pattern: "profile", Seed: 20260929, Profile: londonFullDemandProfileID, Band: "am-peak"},
		DemandProfiles: []project.DemandProfile{londonFullDemandProfile()},
		PlatoonLimit:   sim.MaxPlatoonLimit,
		Geo:            &project.Geo{Latitude: londonReferenceLatitude, Longitude: londonReferenceLongitude, Projection: project.GeoProjection, Radius: project.GeoRadius},
	}
	if err := project.Validate(config); err != nil {
		return project.Config{}, fmt.Errorf("validate LondonFull scenario: %w", err)
	}
	return config, nil
}

// londonFullBerths uses the largest boarding-plus-alighting share of each
// station across the six observed bands. Equal remainders use station ID order.
func londonFullBerths(profile project.DemandProfile) map[string]int {
	const extraBudget = 100
	totals := make([]float64, len(profile.Bands))
	traffic := make(map[string][]float64)
	for _, flow := range profile.Flows {
		for _, id := range []string{flow.From, flow.To} {
			if traffic[id] == nil {
				traffic[id] = make([]float64, len(totals))
			}
			for band, weight := range flow.Weights {
				traffic[id][band] += weight
			}
		}
		for band, weight := range flow.Weights {
			totals[band] += weight
		}
	}
	type allocation struct {
		id           string
		score, quota float64
		extra        int
	}
	items := make([]allocation, 0, len(traffic))
	scoreTotal := 0.0
	for id, weights := range traffic {
		score := 0.0
		for band, weight := range weights {
			score = max(score, weight/totals[band])
		}
		items = append(items, allocation{id: id, score: score})
	}
	slices.SortFunc(items, func(a, b allocation) int { return cmp.Compare(a.id, b.id) })
	for _, item := range items {
		scoreTotal += item.score
	}
	allocated := 0
	for index := range items {
		item := &items[index]
		item.quota = extraBudget * item.score / scoreTotal
		item.extra = int(item.quota)
		allocated += item.extra
	}
	// First allocate the ordinary largest remainders. Then move Bank's
	// extras to the largest remaining deficits without changing the budget.
	assign := func(skipBank bool) {
		best := -1
		for index, item := range items {
			if skipBank && item.id == "940GZZLUBNK" {
				continue
			}
			if best < 0 || item.quota-float64(item.extra) > items[best].quota-float64(items[best].extra) {
				best = index
			}
		}
		items[best].extra++
	}
	for ; allocated < extraBudget; allocated++ {
		assign(false)
	}
	for index := range items {
		if items[index].id == "940GZZLUBNK" {
			move := items[index].extra
			items[index].extra = 0
			for range move {
				assign(true)
			}
			break
		}
	}
	berths := make(map[string]int, len(items))
	for _, item := range items {
		berths[item.id] = londonStationBerths + item.extra
	}
	return berths
}
