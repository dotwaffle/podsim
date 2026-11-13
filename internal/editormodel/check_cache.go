package editormodel

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"maps"
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
		raw := e.branches["demandProfiles"].raw
		passenger := make(map[string]bool, len(e.checks.network.passenger))
		for _, station := range e.checks.network.passenger {
			passenger[text(member(station, "id"))] = true
		}
		if !e.profileVerdict.covers(raw, passenger) {
			var profiles any
			if len(raw) != 0 {
				if err := json.Unmarshal(raw, &profiles); err != nil {
					return checkReport{}, fmt.Errorf("decode demand profile checks: %w", err)
				}
			}
			var errors checkList
			checkProfiles(map[string]any{"demandProfiles": profiles}, passenger, &errors)
			e.profileVerdict = &profileVerdict{raw: raw, passenger: passenger, errors: errors.items, selections: profileSelections(profiles)}
		}
		e.checks.profiles, e.checks.profilesReady = e.profileVerdict.errors, true
		e.profiles = e.profileVerdict.selections
	}
	draft["demandProfiles"] = e.profiles
	return preparedDraftChecks(draft, e.checks), nil
}

// profileVerdict is the result of checkProfiles for the raw bytes of the
// demand profiles and the set of the passenger station IDs. The check reads
// only these two, so the result holds for each network with the same set. A
// node move then does not decode the profiles again.
type profileVerdict struct {
	raw        jsontext.Value
	passenger  map[string]bool
	errors     []check
	selections []any
}

// covers reports whether the verdict is for these profiles and this set of
// passenger station IDs. A nil verdict covers nothing.
func (v *profileVerdict) covers(raw jsontext.Value, passenger map[string]bool) bool {
	return v != nil && bytes.Equal(v.raw, raw) && maps.Equal(v.passenger, passenger)
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
