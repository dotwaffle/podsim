package main

import (
	"github.com/dotwaffle/podsim/internal/scenarios"
	"github.com/dotwaffle/podsim/internal/session"
)

// presetGenerators gives the presets that the editor menu can load. The
// other presets of cmd/scenario are test tools. The generators do not use the
// caches of the scenarios package, so that the server does not keep the
// memory of a preset after the reply.
func presetGenerators() map[string]session.PresetGenerator {
	return map[string]session.PresetGenerator{
		"london-central": scenarios.GenerateLondonCentral,
		"london-full":    scenarios.GenerateLondonFull,
	}
}
