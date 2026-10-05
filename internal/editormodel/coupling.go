package editormodel

import (
	jsonv1 "encoding/json"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
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

// convertToTrains proposes the explicit one-way conversion of a project
// without coupling members. It adds the coupling marker. Trains stay off,
// and the project has no sites or corridors. The server decoder must
// accept the project before and after the change.
func convertToTrains(draft any) (projectChange, error) {
	if hasCouplingMetadata(draft) {
		return projectChange{}, errors.New("only a project without coupling fields can convert to trains")
	}
	patch := map[string]any{
		"couplingContract":  string(sim.CompactPairV1CouplingContract),
		"couplingEnabled":   false,
		"couplingSites":     []any{},
		"couplingCorridors": []any{},
	}
	if err := serverValidation(draft); err != nil {
		return projectChange{}, fmt.Errorf("fix the project before it converts to trains: %w", err)
	}
	converted := maps.Clone(object(draft))
	maps.Copy(converted, patch)
	if err := serverValidation(converted); err != nil {
		return projectChange{}, fmt.Errorf("the converted project is not valid: %w", err)
	}
	return projectChange{Patch: patch}, nil
}

// serverValidation decodes the draft as the server reads a project file or
// a project command, with encoding/json, and validates it.
func serverValidation(draft any) error {
	raw, err := json.Marshal(draft)
	if err != nil {
		return fmt.Errorf("encode project: %w", err)
	}
	var config project.Config
	if err := jsonv1.Unmarshal(raw, &config); err != nil {
		return err
	}
	return project.Validate(config)
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
