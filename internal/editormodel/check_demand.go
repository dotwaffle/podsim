package editormodel

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// checkProfiles adds the errors of the demand profiles: first the errors of
// the profile list, and then the errors of each profile in order. The order
// of the errors is the order of the check report.
func checkProfiles(value any, passenger map[string]bool, errors *checkList) {
	profiles := checkProfileList(value, errors)
	ids := make(map[string]bool)
	for _, profile := range profiles {
		checkProfile(profile, ids, passenger, errors)
	}
}

// checkProfileList checks that the demand profiles are an array with at
// most project.MaxProfiles items. It returns the profiles.
func checkProfileList(value any, errors *checkList) []any {
	profiles := items(member(value, "demandProfiles"))
	if member(value, "demandProfiles") != nil && profiles == nil {
		errors.add("Demand profiles must be an array.", nil)
	}
	if len(profiles) > project.MaxProfiles {
		errors.add("The project has too many demand profiles.", nil)
	}
	return profiles
}

// checkProfile checks the ID of a profile. A profile with a valid and new
// ID adds its ID to ids, and then gets checks of its name, its band and
// flow counts, its bands, its flows, and its band totals.
func checkProfile(profile any, ids, passenger map[string]bool, errors *checkList) {
	id := member(profile, "id")
	if object(profile) == nil || !validID(id) || ids[text(id)] {
		errors.add("A demand profile has an invalid or duplicate ID.", nil)
		return
	}
	ids[text(id)] = true
	prefix := "Demand profile " + text(id)
	name := member(profile, "name")
	if strings.TrimSpace(text(name)) == "" || len(text(name)) > project.MaxNameLength {
		errors.add(prefix+" has an invalid name.", nil)
	}
	bands, flows := items(member(profile, "bands")), items(member(profile, "flows"))
	checkProfileCounts(len(bands), len(flows), prefix, errors)
	checkProfileBands(bands, prefix, errors)
	totals := checkProfileFlows(flows, len(bands), passenger, prefix, errors)
	if slices.ContainsFunc(totals, func(total float64) bool { return math.IsNaN(total) || math.IsInf(total, 0) || total <= 0 }) {
		errors.add(prefix+" has an empty band.", nil)
	}
}

// checkProfileCounts checks the number of bands, and then the number of
// flows, of a profile.
func checkProfileCounts(bands, flows int, prefix string, errors *checkList) {
	if bands < 1 || bands > project.MaxBands {
		errors.add(fmt.Sprintf("%s must contain 1 to %d bands.", prefix, project.MaxBands), nil)
	}
	if flows < 1 || flows > project.MaxFlows {
		errors.add(fmt.Sprintf("%s must contain 1 to %d flows.", prefix, project.MaxFlows), nil)
	}
}

// checkProfileBands checks each band of a profile in order.
func checkProfileBands(bands []any, prefix string, errors *checkList) {
	bandIDs := make(map[string]bool)
	for _, band := range bands {
		if invalidBand(band, bandIDs) {
			errors.add(prefix+" has an invalid band.", nil)
		} else {
			bandIDs[text(member(band, "id"))] = true
		}
	}
}

// invalidBand reports whether a band has an invalid or duplicate ID, no
// name, or a start or duration outside one day.
func invalidBand(band any, bandIDs map[string]bool) bool {
	bandID, start, duration := member(band, "id"), member(band, "startMinute"), member(band, "durationMinutes")
	return !validID(bandID) || bandIDs[text(bandID)] || strings.TrimSpace(text(member(band, "name"))) == "" ||
		!integer(start) || number(start) < 0 || number(start) >= 1440 || !integer(duration) || number(duration) < 1 || number(duration) > 1440
}

// checkProfileFlows checks each flow of a profile in order, and then its
// weights. It returns the total weight of each band.
func checkProfileFlows(flows []any, bandCount int, passenger map[string]bool, prefix string, errors *checkList) []float64 {
	totals, pairs := make([]float64, bandCount), make(map[[2]string]bool)
	for _, flow := range flows {
		weights, pair := items(member(flow, "weights")), [2]string{text(member(flow, "from")), text(member(flow, "to"))}
		if invalidFlow(flow, pair, weights, bandCount, passenger, pairs) {
			errors.add(prefix+" has an invalid flow.", nil)
			continue
		}
		pairs[pair] = true
		addFlowWeights(weights, totals, prefix, errors)
	}
	return totals
}

// invalidFlow reports whether a flow is not an object, does not join two
// different passenger stations, repeats an earlier pair, or does not have
// one weight for each band.
func invalidFlow(flow any, pair [2]string, weights []any, bandCount int, passenger map[string]bool, pairs map[[2]string]bool) bool {
	from, to := pair[0], pair[1]
	return object(flow) == nil || !passenger[from] || !passenger[to] || from == to || pairs[pair] || weights == nil || len(weights) != bandCount
}

// addFlowWeights checks each weight of a flow, and adds each valid weight
// to the total of its band.
func addFlowWeights(weights []any, totals []float64, prefix string, errors *checkList) {
	for index, weight := range weights {
		if !finite(weight) || number(weight) < 0 || number(weight) > sim.MaxCounter {
			errors.add(prefix+" has an invalid weight.", nil)
		} else {
			totals[index] += number(weight)
		}
	}
}

func checkDemandRate(value any, errors *checkList) {
	demand := member(value, "demand")
	if has(demand, "enabled") {
		if _, ok := member(demand, "enabled").(bool); !ok {
			errors.add("The passenger demand enabled setting must be true or false.", nil)
		}
	}
	rate := member(demand, "perMinute")
	if !integer(rate) || number(rate) < 1 || number(rate) > 120 {
		errors.add("Passenger demand must be 1 to 120 trips per minute.", nil)
	}
}

func checkDemand(value any, passenger map[string]bool, errors *checkList) {
	demand := member(value, "demand")
	pattern := text(member(demand, "pattern"))
	if !slices.Contains([]string{"balanced", "destination", "market", "profile", "profile-daily", "rail-arrivals", "rail-services"}, pattern) {
		errors.add("The passenger demand pattern is invalid.", nil)
	}
	if has(demand, "destination") {
		if destination, ok := member(demand, "destination").(string); !ok || len(destination) > project.MaxIDLength {
			errors.add("The passenger demand destination is invalid.", nil)
		}
	}
	if pattern == "destination" && !passenger[text(member(demand, "destination"))] {
		errors.add("Select a passenger destination.", nil)
	}
	if pattern == "rail-arrivals" && len(items(member(value, "railArrivals"))) == 0 {
		errors.add("Rail-arrivals demand needs a nonempty arrival plan.", nil)
	}
	if pattern == "rail-services" && len(items(member(value, "railArrivals")))+len(items(member(value, "railDepartures"))) == 0 {
		errors.add("Rail-services demand needs a nonempty rail plan.", nil)
	}
	if pattern == "profile" || pattern == "profile-daily" {
		profiles := items(member(value, "demandProfiles"))
		var chosen any
		for _, profile := range profiles {
			if object(profile) != nil && text(member(profile, "id")) == text(member(demand, "profile")) {
				chosen = profile
				break
			}
		}
		switch {
		case len(profiles) == 0:
			errors.add("The project has no demand profiles. Select another pattern.", nil)
		case chosen == nil:
			errors.add("Select a demand profile.", nil)
		case pattern == "profile" && !slices.ContainsFunc(items(member(chosen, "bands")), func(band any) bool {
			return object(band) != nil && text(member(band, "id")) == text(member(demand, "band"))
		}):
			errors.add("Select a demand time band.", nil)
		}
	}
	seed := member(demand, "seed")
	if !integer(seed) || number(seed) < 0 || number(seed) > 9007199254740991 {
		errors.add("The demand seed must be a nonnegative whole number.", nil)
	}
}

func checkSettings(value any, errors *checkList) {
	for _, setting := range []struct {
		key, message string
		max          float64
	}{
		{"sharedRidePartyLimit", fmt.Sprintf("The shared ride party limit must be 1 to %d.", sim.MaxSharedRideParties), sim.MaxSharedRideParties},
	} {
		v := member(value, setting.key)
		if has(value, setting.key) && (!integer(v) || number(v) < 0 || number(v) > setting.max) {
			errors.add(setting.message, nil)
		}
	}
	for _, setting := range []struct {
		key, message string
		accepted     []string
	}{
		{"sharedRideMode", "The shared ride mode must be destination or drop-offs.", []string{"", "destination", "drop-offs"}},
		{"sharedRideJoin", "The shared ride join policy must be unassigned or reassign-existing.", []string{"", "unassigned", "reassign-existing"}},
	} {
		v, ok := member(value, setting.key).(string)
		if has(value, setting.key) && (!ok || !slices.Contains(setting.accepted, v)) {
			errors.add(setting.message, nil)
		}
	}
	stops := member(value, "sharedRideMaxStops")
	if has(value, "sharedRideMaxStops") && (!integer(stops) || number(stops) < 0 || number(stops) > sim.MaxSharedRideStops) {
		errors.add(fmt.Sprintf("The shared ride stop limit must be 1 to %d.", sim.MaxSharedRideStops), nil)
	}
	if has(value, "platoonLimit") && !draftPlatoonLimit(member(value, "platoonLimit")) {
		errors.add(fmt.Sprintf("The platoon limit must be %d to %d, or 0 for no platoons.", sim.MinPlatoonLimit, sim.MaxPlatoonLimit), nil)
	}
	for _, setting := range []struct{ key, message string }{
		{"pickupReassignment", "The pickup reassignment setting must be true or false."},
	} {
		if has(value, setting.key) {
			if _, ok := member(value, setting.key).(bool); !ok {
				errors.add(setting.message, nil)
			}
		}
	}
	if problem := onboardSettingError(value); problem != "" {
		errors.add(problem, nil)
	}
}

func checkGeo(value any, errors *checkList) {
	geo := member(value, "geo")
	if geo != nil {
		if problem := draftGeoError(geo); problem != "" {
			errors.add(problem, nil)
		}
	}
	background := member(value, "map")
	if background != nil && (object(background) == nil || text(member(background, "provider")) != "osm" || !finite(member(background, "opacity")) || number(member(background, "opacity")) < 0 || number(member(background, "opacity")) > 1 || geo == nil || draftGeoError(geo) != "") {
		errors.add("The map needs provider osm, opacity from 0 to 1, and a geographic reference.", nil)
	}
}

func draftGeoError(geo any) string {
	switch {
	case object(geo) == nil:
		return "The geo reference must be an object."
	case !(math.Abs(number(member(geo, "latitude"))) <= project.MaxGeoLatitude):
		return fmt.Sprintf("The geo latitude must be from %d to %d degrees.", -project.MaxGeoLatitude, project.MaxGeoLatitude)
	case !(math.Abs(number(member(geo, "longitude"))) <= 180):
		return "The geo longitude must be from -180 to 180 degrees."
	case text(member(geo, "projection")) != project.GeoProjection:
		return `The geo projection must be "equirectangular".`
	case number(member(geo, "radius")) != project.GeoRadius:
		return fmt.Sprintf("The geo radius must be %d meters.", project.GeoRadius)
	}
	return ""
}
