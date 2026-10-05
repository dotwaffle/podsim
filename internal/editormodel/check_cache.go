package editormodel

import (
	"encoding/json/v2"
	"fmt"
)

func (e *engine) draftChecks() (checkReport, error) {
	if err := e.ensureNetworkValue(); err != nil {
		return checkReport{}, err
	}
	draft := make(map[string]any, len(e.branches))
	for key, branch := range e.branches {
		draft[key] = branch.value
	}
	network := draft["network"]
	if object(network) == nil || items(member(network, "nodes")) == nil || items(member(network, "lanes")) == nil || items(member(network, "stations")) == nil {
		return draftChecks(draft), nil
	}
	if e.checks == nil {
		var errors checkList
		e.checks = &preparedChecks{network: checkNetwork(network, &errors), networkErrors: errors.items}
	}
	if !e.checks.profilesReady {
		var profiles any
		if raw := e.branches["demandProfiles"].raw; len(raw) != 0 {
			if err := json.Unmarshal(raw, &profiles); err != nil {
				return checkReport{}, fmt.Errorf("decode demand profile checks: %w", err)
			}
		}
		passenger := make(map[string]bool, len(e.checks.network.passenger))
		for _, station := range e.checks.network.passenger {
			passenger[text(member(station, "id"))] = true
		}
		var errors checkList
		checkProfiles(map[string]any{"demandProfiles": profiles}, passenger, &errors)
		e.checks.profiles, e.checks.profilesReady = errors.items, true
		e.profiles = profileSelections(profiles)
	}
	draft["demandProfiles"] = e.profiles
	return preparedDraftChecks(draft, e.checks), nil
}

// profileSelections keeps only the IDs needed to check demand selection.
// Flow maps and weights are temporary during profile checks, then released.
func profileSelections(value any) []any {
	profiles := items(value)
	if profiles == nil {
		return nil
	}
	out := make([]any, 0, len(profiles))
	for _, profile := range profiles {
		if object(profile) == nil {
			out = append(out, nil)
			continue
		}
		var bands []any
		for _, band := range items(member(profile, "bands")) {
			if object(band) == nil {
				bands = append(bands, nil)
			} else {
				bands = append(bands, map[string]any{"id": member(band, "id")})
			}
		}
		out = append(out, map[string]any{"id": member(profile, "id"), "bands": bands})
	}
	return out
}
