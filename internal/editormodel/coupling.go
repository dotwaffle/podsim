package editormodel

import (
	"encoding/json/v2"
	"slices"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

// couplingKeys are the project members of the physical coupling contract.
var couplingKeys = []string{"couplingContract", "couplingEnabled", "couplingSites", "couplingCorridors"}

// Presence includes null and empty values, as native project decoding counts them.
func hasCouplingMetadata(draft any) bool {
	return slices.ContainsFunc(couplingKeys, func(key string) bool { return has(draft, key) })
}

// couplingMarked reports whether the draft has the coupling marker. Only a
// marked draft has the train option, coupling sites, and corridors.
func couplingMarked(draft any) bool {
	return text(member(draft, "couplingContract")) == string(sim.CompactPairV1CouplingContract)
}

// couplingContractError checks the coupling members that native decoding
// rejects before geometry. Native validation checks the site and corridor records.
func couplingContractError(draft any) string {
	if !hasCouplingMetadata(draft) {
		return ""
	}
	if !couplingMarked(draft) {
		return "Coupling fields require couplingContract compact-pair-v1."
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

// checkCouplingGeometry reports the native geometry verdict for the sites and
// corridors of a draft with the coupling marker. The text is the native
// error, so the worker shows it once when native validation reports the
// same error.
func checkCouplingGeometry(draft any, report *checkList) {
	if !couplingMarked(draft) || couplingContractError(draft) != "" {
		return
	}
	// Native validation accepts empty registries without reading the network.
	if len(items(member(draft, "couplingSites"))) == 0 && len(items(member(draft, "couplingCorridors"))) == 0 {
		return
	}
	raw, err := json.Marshal(map[string]any{"network": member(draft, "network"), "couplingSites": member(draft, "couplingSites"), "couplingCorridors": member(draft, "couplingCorridors")})
	type branches project.Config
	var decoded branches
	if err != nil || json.Unmarshal(raw, &decoded, json.RejectUnknownMembers(true)) != nil {
		report.add("Coupling sites, corridors, or their network have invalid fields.", nil)
		return
	}
	input := sim.CouplingGeometryInput{Contract: sim.CompactPairV1CouplingContract, Network: decoded.Network, Sites: decoded.CouplingSites, Corridors: decoded.CouplingCorridors}
	if err := sim.ValidateCouplingGeometry(input); err != nil {
		report.add(err.Error(), nil)
	}
}
