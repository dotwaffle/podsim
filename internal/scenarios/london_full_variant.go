package scenarios

import (
	"fmt"

	"github.com/dotwaffle/podsim/internal/project"
)

// LondonFullVariant names the Express variant of the LondonFull preset. Its
// value is the value of the -express flag of cmd/scenario.
type LondonFullVariant string

// The variants of the LondonFull preset.
const (
	// LondonFullNone is LondonFull without Express.
	LondonFullNone LondonFullVariant = "none"
	// LondonFullDedicated is the LondonFull Express preset, with dedicated
	// Express lanes between Waterloo and Bank.
	LondonFullDedicated LondonFullVariant = "dedicated"
	// LondonFullShared is the LondonFull Shared Express preset, with Express
	// on shared track between Waterloo and Bank.
	LondonFullShared LondonFullVariant = "shared"
	// LondonFullWharf is the LondonFull Shared Express Wharf preset, which
	// adds a shared-track pair between London Bridge and Canary Wharf.
	LondonFullWharf LondonFullVariant = "wharf"
	// LondonFullHubs is the LondonFull Shared Express Wharf Hubs preset,
	// which joins the two Express networks of the wharf variant.
	LondonFullHubs LondonFullVariant = "hubs"
)

// LondonFullVariants returns the variants in help order.
func LondonFullVariants() []LondonFullVariant {
	return []LondonFullVariant{LondonFullNone, LondonFullDedicated, LondonFullShared, LondonFullWharf, LondonFullHubs}
}

// Key returns the name of the variant in test, soak, and golden keys:
// london-full for LondonFullNone, and london-full:<variant> for the others.
func (v LondonFullVariant) Key() string {
	if v == LondonFullNone {
		return "london-full"
	}
	return "london-full:" + string(v)
}

// GenerateLondonFullVariant builds the variant of the LondonFull preset
// without a cache, so the caller owns all the memory of the result.
func GenerateLondonFullVariant(v LondonFullVariant) (project.Config, error) {
	switch v {
	case LondonFullNone:
		return GenerateLondonFull()
	case LondonFullDedicated:
		return generateLondonFullExpress()
	case LondonFullShared:
		return generateLondonFullSharedExpress()
	case LondonFullWharf:
		return generateLondonFullSharedExpressWharf()
	case LondonFullHubs:
		return generateLondonFullSharedExpressWharfHubs()
	}
	return project.Config{}, fmt.Errorf("unknown LondonFull variant %q", v)
}

// Generate returns GenerateLondonFullVariant(v). Its method value fits the
// callers that take a generator function.
func (v LondonFullVariant) Generate() (project.Config, error) {
	return GenerateLondonFullVariant(v)
}
