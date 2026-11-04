package project

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// TestGeneratedOrderConsentValidate checks the accepted values, the refusal
// text, and the place of the check after the other shared ride checks.
func TestGeneratedOrderConsentValidate(t *testing.T) {
	t.Parallel()
	const refused = `generated order consent must be "private" or "shared"`
	bad := func(change func(*Config)) func(*Config) {
		return func(config *Config) {
			config.GeneratedOrderConsent = "pooled"
			if change != nil {
				change(config)
			}
		}
	}
	for _, test := range []struct {
		name   string
		change func(*Config)
		want   string
	}{
		{"absent", func(*Config) {}, ""},
		{"private", func(config *Config) { config.GeneratedOrderConsent = sim.PrivateConsent }, ""},
		{"shared", func(config *Config) { config.GeneratedOrderConsent = sim.SharedConsent }, ""},
		{"shared with a limit of one", func(config *Config) {
			config.GeneratedOrderConsent, config.SharedRidePartyLimit = sim.SharedConsent, 1
		}, ""},
		{"another value", bad(nil), refused},
		{"another case", func(config *Config) { config.GeneratedOrderConsent = "Shared" }, refused},
		{"after the party limit", bad(func(config *Config) { config.SharedRidePartyLimit = sim.MaxSharedRideParties + 1 }), "shared ride party limit must be 1 to 8"},
		{"after the mode", bad(func(config *Config) { config.SharedRideMode = "other" }), `shared ride mode must be "destination" or "drop-offs"`},
		{"after the stop limit", bad(func(config *Config) { config.SharedRideMaxStops = sim.MaxSharedRideStops + 1 }), "shared ride stop limit must be 1 to 7"},
		{"after the join policy", bad(func(config *Config) { config.SharedRideJoin = "other" }), `shared ride join policy must be "unassigned" or "reassign-existing"`},
		{"before the platoon limit", bad(func(config *Config) { config.PlatoonLimit = sim.MaxPlatoonLimit + 1 }), refused},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Default()
			test.change(&config)
			err := Validate(config)
			switch {
			case test.want == "" && err != nil:
				t.Fatal(err)
			case test.want != "" && (err == nil || err.Error() != test.want):
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

// TestGeneratedOrderConsentRoundTrip checks that an absent member stays
// absent, and that a set member stays the same.
func TestGeneratedOrderConsentRoundTrip(t *testing.T) {
	t.Parallel()
	member := []byte(`"generatedOrderConsent"`)
	if data := canonicalJSON(t, Default()); bytes.Contains(data, member) {
		t.Fatal("a project without the member writes it")
	}
	for _, consent := range []sim.SharingConsent{sim.PrivateConsent, sim.SharedConsent} {
		config := Default()
		config.GeneratedOrderConsent = consent
		data := canonicalJSON(t, config)
		if !bytes.Contains(data, []byte(`"generatedOrderConsent":"`+string(consent)+`"`)) {
			t.Fatalf("consent %q is not in the project", consent)
		}
		var decoded Config
		if err := jsonv2.Unmarshal(data, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.GeneratedOrderConsent != consent || !bytes.Equal(canonicalJSON(t, decoded), data) {
			t.Fatalf("consent %q did not round trip", consent)
		}
		if Clone(config).GeneratedOrderConsent != consent {
			t.Fatalf("Clone lost consent %q", consent)
		}
	}
	var decoded Config
	if err := jsonv2.Unmarshal(canonicalJSON(t, Default()), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GeneratedOrderConsent != "" {
		t.Fatalf("an absent member decoded as %q", decoded.GeneratedOrderConsent)
	}
}
