package editormodel

import (
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingKeys are the project members of the physical coupling contract.
var couplingKeys = []string{"couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors"}

// Presence includes null and empty values, as native project decoding counts them.
func hasCouplingMetadata(draft any) bool {
	return slices.ContainsFunc(couplingKeys, func(key string) bool { return hasFold(draft, key) })
}

// couplingContractError checks the coupling members that native decoding
// rejects before geometry. Native validation checks the site and corridor records.
func couplingContractError(draft any) string {
	if number(member(draft, "version")) != project.CouplingVersion {
		if hasCouplingMetadata(draft) {
			return "Coupling fields require project version 5."
		}
		return ""
	}
	if text(member(draft, "couplingContract")) != string(sim.CompactPairV1CouplingContract) {
		return "Project version 5 requires couplingContract compact-pair-v1."
	}
	if _, valid := member(draft, "couplingEnabled").(bool); has(draft, "couplingEnabled") && !valid {
		return "The train setting must be true or false."
	}
	for _, key := range []string{"couplingSites", "couplingCorridors"} {
		if _, valid := member(draft, key).([]any); has(draft, key) && !valid {
			return "Coupling sites and corridors must be arrays."
		}
	}
	return ""
}
