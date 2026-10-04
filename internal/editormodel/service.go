package editormodel

import (
	"encoding/json/v2"
	"fmt"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// Metadata presence includes null so normalization cannot grant a new version.
func hasServiceMetadata(draft any) bool {
	if hasFold(draft, "orderContract") || hasFold(draft, "expressServices") || hasFold(draft, "stationQueueSpacing") || hasFold(draft, "onboardPickups") {
		return true
	}
	for _, pod := range items(member(draft, "fleet")) {
		if hasFold(pod, "Class") {
			return true
		}
	}
	network := member(draft, "network")
	for _, key := range []string{"Lanes", "Stations"} {
		for _, item := range items(member(network, key)) {
			if hasFold(item, "VehicleClasses") {
				return true
			}
			for _, berth := range items(member(item, "Berths")) {
				if hasFold(berth, "VehicleClasses") {
					return true
				}
			}
		}
	}
	return false
}

func hasFold(value any, key string) bool {
	for candidate := range object(value) {
		if strings.EqualFold(candidate, key) {
			return true
		}
	}
	return false
}

func draftClassSet(value any) (sim.ClassSet, bool) {
	if !has(value, "VehicleClasses") {
		return 0, true
	}
	raw, err := json.Marshal(member(value, "VehicleClasses"))
	var classes sim.ClassSet
	if err != nil || json.Unmarshal(raw, &classes) != nil {
		return 0, false
	}
	return classes, true
}

func checkServiceMetadata(draft any, errors *checkList) {
	if problem := draftContractError(draft); problem != "" {
		errors.add(problem, nil)
	}
	checkCouplingGeometry(draft, errors)
	if !hasServiceMetadata(draft) {
		return
	}
	network := member(draft, "network")
	for _, key := range []string{"Lanes", "Stations"} {
		for _, item := range items(member(network, key)) {
			checkDraftClassSet(item, errors)
			for _, berth := range items(member(item, "Berths")) {
				checkDraftClassSet(berth, errors)
			}
		}
	}
	for _, pod := range items(member(draft, "fleet")) {
		checkPodClass(pod, network, draftOrderContract(draft), errors)
	}
	if has(draft, "expressServices") {
		checkExpressRegistry(draft, errors)
	}
}

func checkDraftClassSet(value any, errors *checkList) {
	if _, valid := draftClassSet(value); !valid {
		errors.add("VehicleClasses must contain 1 to 4 distinct known classes.", nil)
	}
}

func checkPodClass(pod, network any, contract sim.OrderContract, errors *checkList) {
	class := sim.VehicleClass(text(member(pod, "Class")))
	if has(pod, "Class") {
		if class == "" {
			errors.add(fmt.Sprintf("Pod %s has an invalid vehicle class.", label(member(pod, "ID"))), target("station", member(pod, "StationID")))
			return
		}
		if _, valid := sim.LookupVehicleClassWithOrderContract(class, contract); !valid {
			errors.add(fmt.Sprintf("Pod %s has an invalid vehicle class.", label(member(pod, "ID"))), target("station", member(pod, "StationID")))
			return
		}
	}
	if sim.ValidateVehicleClassProfileWithOrderContract(class, contract) != nil {
		errors.add(fmt.Sprintf("Pod %s has no approved physical profile.", label(member(pod, "ID"))), target("station", member(pod, "StationID")))
		return
	}
	for _, station := range items(member(network, "Stations")) {
		if !sameOptionalMember(station, "ID", pod, "StationID") {
			continue
		}
		for _, berth := range items(member(station, "Berths")) {
			if !sameOptionalMember(berth, "ID", pod, "BerthID") {
				continue
			}
			stationClasses, stationValid := draftClassSet(station)
			berthClasses, berthValid := draftClassSet(berth)
			if stationValid && berthValid && (!stationClasses.Allows(string(class)) || !berthClasses.Allows(string(class))) {
				errors.add(fmt.Sprintf("Pod %s has an incompatible station or berth.", label(member(pod, "ID"))), target("station", member(pod, "StationID")))
			}
		}
	}
}

func checkExpressRegistry(draft any, errors *checkList) {
	services, valid := member(draft, "expressServices").([]any)
	if !valid || len(services) > project.MaxExpressServices {
		errors.add("Express services must be an array of at most 300 records.", nil)
		return
	}
	if len(services) == 0 {
		return
	}
	raw, err := json.Marshal(map[string]any{"network": member(draft, "network"), "expressServices": services})
	type branches project.Config
	var decoded branches
	if err != nil || json.Unmarshal(raw, &decoded, json.RejectUnknownMembers(true)) != nil {
		errors.add("Express services or their network have invalid fields.", nil)
		return
	}
	if err := sim.ValidateExpressServicesWithOrderContract(decoded.Network, decoded.ExpressServices, draftOrderContract(draft)); err != nil {
		errors.add("Express services: "+err.Error()+".", nil)
	}
}

func draftOrderContract(draft any) sim.OrderContract {
	version := number(member(draft, "version"))
	if (version == 4 || version == project.CouplingVersion) && text(member(draft, "orderContract")) == string(sim.ExpressOrderContract) {
		return sim.ExpressOrderContract
	}
	return ""
}

func draftContractError(draft any) string {
	if problem := couplingContractError(draft); problem != "" {
		return problem
	}
	// Version 5 accepts service metadata. Its order contract stays optional.
	if number(member(draft, "version")) == project.CouplingVersion {
		if hasFold(draft, "orderContract") && draftOrderContract(draft) != sim.ExpressOrderContract {
			return "Project version 5 accepts only orderContract express-v1."
		}
		return ""
	}
	if number(member(draft, "version")) == 4 {
		if draftOrderContract(draft) != sim.ExpressOrderContract {
			return "Project version 4 requires orderContract express-v1."
		}
		return ""
	}
	if hasFold(draft, "orderContract") {
		return "The order contract requires project version 4."
	}
	if hasServiceMetadata(draft) && number(member(draft, "version")) != 3 {
		return "Vehicle and service fields require project version 3."
	}
	return ""
}
