package editormodel

import (
	"fmt"
	"math"
)

func checkRailPlans(value any, passenger map[string]bool, errors *checkList) {
	arrivals, departures := member(value, "railArrivals"), member(value, "railDepartures")
	checkRailArrivals(arrivals, passenger, errors)
	if departures == nil {
		return
	}
	if items(departures) == nil {
		errors.add("Rail departures must be an array.", nil)
		return
	}
	if len(items(arrivals))+len(items(departures)) > 256 {
		errors.add("Rail plans must contain at most 256 combined events.", nil)
	}
	total, outbound := 0.0, 0.0
	releases := make(map[float64]float64)
	for _, arrival := range items(arrivals) {
		if count := member(arrival, "passengers"); integer(count) {
			total += number(count)
			if at, walking := member(arrival, "atSeconds"), member(arrival, "walkingSeconds"); finite(at) && finite(walking) {
				releases[max(1, (number(at)+number(walking))*60)] += number(count)
			}
		}
	}
	ids := make(map[string]bool)
	for index, departure := range items(departures) {
		prefix := fmt.Sprintf("Rail departure %d", index+1)
		if object(departure) == nil {
			errors.add(prefix+" must be an object.", nil)
			continue
		}
		checkRailIdentity(departure, prefix, passenger, ids, errors)
		at, walking := member(departure, "atSeconds"), member(departure, "walkingSeconds")
		from, until := member(departure, "requestFromSeconds"), member(departure, "requestUntilSeconds")
		timeValid := integer(at) && number(at) >= 1 && number(at) <= 86400 && integer(walking) && number(walking) >= 0 && number(walking) <= 3600 && integer(from) && number(from) >= 0 && integer(until) && number(until) >= number(from) && number(until)+number(walking) < number(at)
		if !timeValid {
			errors.add(prefix+" has an invalid request window or transfer time.", nil)
		}
		count := member(departure, "passengers")
		if !integer(count) || number(count) < 1 || number(count) > 200 {
			errors.add(prefix+" must offer 1 to 200 passengers.", nil)
		} else {
			outbound += number(count)
			total += number(count)
			if timeValid {
				for i := 0.0; i < number(count); i++ {
					tick := number(from) * 60
					if number(count) > 1 {
						tick += math.Floor(i * (number(until) - number(from)) * 60 / (number(count) - 1))
					}
					releases[max(1, tick)]++
				}
			}
		}
		checkRailEndpoints(departure, prefix, "origins", "origin", passenger, errors)
	}
	if outbound > 3000 {
		errors.add("Rail departures must offer at most 3000 passengers.", nil)
	}
	if total > 10000 {
		errors.add("Rail plans must offer at most 10000 combined passengers.", nil)
	}
	if crowdedRelease(releases) {
		errors.add("Rail plans must offer at most 200 passengers at one release tick.", nil)
	}
}

func checkRailArrivals(value any, passenger map[string]bool, errors *checkList) {
	if value == nil {
		return
	}
	if items(value) == nil {
		errors.add("Rail arrivals must be an array.", nil)
		return
	}
	if len(items(value)) > 256 {
		errors.add("The project must contain at most 256 rail arrivals.", nil)
	}
	ids, releases, total := make(map[string]bool), make(map[float64]float64), 0.0
	for index, arrival := range items(value) {
		prefix := fmt.Sprintf("Rail arrival %d", index+1)
		if object(arrival) == nil {
			errors.add(prefix+" must be an object.", nil)
			continue
		}
		checkRailIdentity(arrival, prefix, passenger, ids, errors)
		at, walking := member(arrival, "atSeconds"), member(arrival, "walkingSeconds")
		timeValid := integer(at) && number(at) >= 0 && number(at) <= 86400 && integer(walking) && number(walking) >= 0 && number(walking) <= 3600 && number(at)+number(walking) <= 86400
		if !timeValid {
			errors.add(prefix+" has an invalid arrival time or walking delay.", nil)
		}
		count := member(arrival, "passengers")
		if !integer(count) || number(count) < 1 || number(count) > 200 {
			errors.add(prefix+" must offer 1 to 200 passengers.", nil)
		} else {
			total += number(count)
			if timeValid {
				releases[max(1, (number(at)+number(walking))*60)] += number(count)
			}
		}
		checkRailEndpoints(arrival, prefix, "destinations", "destination", passenger, errors)
	}
	if total > 10000 {
		errors.add("Rail arrivals must offer at most 10000 passengers.", nil)
	}
	if crowdedRelease(releases) {
		errors.add("Rail arrivals must offer at most 200 passengers at one release tick.", nil)
	}
}

func checkRailIdentity(event any, prefix string, passenger, ids map[string]bool, errors *checkList) {
	id := member(event, "id")
	if value, ok := id.(string); !ok || value == "" || len(value) > 64 || ids[value] {
		errors.add(prefix+" has an invalid or duplicate ID.", nil)
	}
	ids[text(id)] = true
	if !passenger[text(member(event, "station"))] {
		errors.add(prefix+" needs a passenger hub.", nil)
	}
}

func checkRailEndpoints(event any, prefix, key, kind string, passenger map[string]bool, errors *checkList) {
	endpoints := items(member(event, key))
	if len(endpoints) < 1 || len(endpoints) > 16 {
		errors.add(prefix+" needs 1 to 16 "+key+".", nil)
		return
	}
	ids := make(map[string]bool)
	for _, endpoint := range endpoints {
		if object(endpoint) == nil {
			errors.add(prefix+" has an invalid "+kind+".", nil)
			continue
		}
		station := text(member(endpoint, "station"))
		if !passenger[station] || station == text(member(event, "station")) || ids[station] {
			errors.add(prefix+" has an invalid or duplicate "+kind+".", nil)
		}
		ids[station] = true
		weight := member(endpoint, "weight")
		if !integer(weight) || number(weight) < 1 || number(weight) > 1000000 {
			errors.add(prefix+" needs "+kind+" weights from 1 to 1000000.", nil)
		}
	}
}

func crowdedRelease(releases map[float64]float64) bool {
	for _, count := range releases {
		if count > 200 {
			return true
		}
	}
	return false
}
