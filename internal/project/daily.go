package project

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"sort"

	"github.com/dotwaffle/podsim/internal/sim"
)

const minutesPerDay = 24 * 60

type dailyFlow struct {
	from, to   string
	cumulative float64
}

type dailyBand struct {
	startMinute int
	rate        int
	total       float64
	flows       []dailyFlow
	weights     map[string]float64
}

// DailyProfile owns immutable band samplers and a repeating simulation clock.
// Its methods do not change the profile and can run concurrently.
type DailyProfile struct {
	startMinute int
	minutes     [minutesPerDay]int
	bands       []dailyBand
}

func validateDailyBands(profile DemandProfile) error {
	var used [minutesPerDay]bool
	for _, band := range profile.Bands {
		if band.StartMinute < 0 || band.StartMinute >= minutesPerDay || band.DurationMinutes < 1 || band.DurationMinutes > minutesPerDay {
			return errors.New("daily demand bands need a start minute from 0 to 1439 and a duration from 1 to 1440")
		}
		for offset := range band.DurationMinutes {
			minute := (band.StartMinute + offset) % minutesPerDay
			if used[minute] {
				return fmt.Errorf("daily demand bands overlap at minute %d", minute)
			}
			used[minute] = true
		}
	}
	return nil
}

// NewDailyProfile compiles owned samplers for validated daily demand settings.
// The profile must have nonoverlapping bands and finite positive weight totals.
func NewDailyProfile(config DemandConfig, profiles []DemandProfile) (*DailyProfile, error) {
	if config.Pattern != "profile-daily" || config.DailyStartMinute < 0 || config.DailyStartMinute >= minutesPerDay || config.PerMinute < 1 || config.PerMinute > 120 {
		return nil, errors.New("invalid daily demand settings")
	}
	profile, ok := demandProfile(profiles, config.Profile)
	if !ok {
		return nil, fmt.Errorf("unknown demand profile %s", quoteID(config.Profile))
	}
	if err := validateDailyBands(profile); err != nil {
		return nil, err
	}
	daily := &DailyProfile{startMinute: config.DailyStartMinute, bands: make([]dailyBand, len(profile.Bands))}
	for minute := range daily.minutes {
		daily.minutes[minute] = -1
	}
	for index, band := range profile.Bands {
		rate := config.PerMinute
		if band.PerMinute != nil {
			rate = *band.PerMinute
		}
		if rate < 0 || rate > 120 {
			return nil, errors.New("daily band rate must be 0 to 120")
		}
		sampler := dailyBand{startMinute: band.StartMinute, rate: rate, weights: make(map[string]float64)}
		for _, flow := range profile.Flows {
			if len(flow.Weights) != len(profile.Bands) {
				return nil, errors.New("daily flow weights must match the bands")
			}
			weight := flow.Weights[index]
			if weight < 0 || math.IsNaN(weight) || math.IsInf(weight, 0) {
				return nil, errors.New("daily weights must be finite and nonnegative")
			}
			if weight == 0 {
				continue
			}
			sampler.total += weight
			sampler.weights[flow.From] += weight
			sampler.flows = append(sampler.flows, dailyFlow{from: flow.From, to: flow.To, cumulative: sampler.total})
		}
		if sampler.total <= 0 || math.IsInf(sampler.total, 0) {
			return nil, errors.New("daily bands need finite positive weight totals")
		}
		daily.bands[index] = sampler
		for offset := range band.DurationMinutes {
			daily.minutes[(band.StartMinute+offset)%minutesPerDay] = index
		}
	}
	return daily, nil
}

// Band returns the active band index, or -1 during a gap. Tick one starts
// the first minute. Tick zero selects that same initial band.
func (d *DailyProfile) Band(tick int64) int {
	minute := ((max(1, tick) - 1) / (60 * sim.TicksPerSecond)) % minutesPerDay
	return d.minutes[(int(minute)+d.startMinute)%minutesPerDay]
}

// BandOccurrence returns the active band and the day when that occurrence started.
// A band that crosses midnight keeps its occurrence until its next start.
func (d *DailyProfile) BandOccurrence(tick int64) (band int, day int64) {
	band = d.Band(tick)
	if band < 0 {
		return band, -1
	}
	minute := (max(1, tick)-1)/(60*sim.TicksPerSecond) + int64(d.startMinute)
	delta := minute - int64(d.bands[band].startMinute)
	day = delta / minutesPerDay
	if delta < 0 && delta%minutesPerDay != 0 {
		day--
	}
	return band, day
}

// Rate returns the band's configured rate, or zero during a gap.
func (d *DailyProfile) Rate(band int) int {
	if band < 0 || band >= len(d.bands) {
		return 0
	}
	return d.bands[band].rate
}

// Weights returns owned pickup weights, or nil during a gap.
func (d *DailyProfile) Weights(band int) map[string]float64 {
	if d.Rate(band) == 0 {
		return nil
	}
	return maps.Clone(d.bands[band].weights)
}

// Pair selects a band's flow using a draw in [0, 1). A gap returns no pair.
func (d *DailyProfile) Pair(band int, draw float64) (from, to string, ok bool) {
	if d.Rate(band) == 0 || draw < 0 || draw >= 1 || math.IsNaN(draw) {
		return "", "", false
	}
	sampler := &d.bands[band]
	index := sort.Search(len(sampler.flows), func(index int) bool {
		return sampler.flows[index].cumulative > draw*sampler.total
	})
	if index == len(sampler.flows) {
		index--
	}
	flow := sampler.flows[index]
	return flow.from, flow.to, true
}
