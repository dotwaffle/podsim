package sim

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"reflect"
	"testing"
)

func TestClassSetDefaultsAndIsolation(t *testing.T) {
	t.Parallel()
	var omitted ClassSet
	for _, class := range []string{"", "legacy", "compact"} {
		if !omitted.Allows(class) {
			t.Fatalf("default rejects %q", class)
		}
	}
	for _, class := range []string{"group", "express", "unknown"} {
		if omitted.Allows(class) {
			t.Fatalf("default admits %q", class)
		}
	}
	explicit, err := NewClassSet("express", "group")
	if err != nil {
		t.Fatal(err)
	}
	if !explicit.Allows("express") || !explicit.Allows("group") || explicit.Allows("legacy") || explicit.Allows("compact") {
		t.Fatal("explicit list did not constrain admission")
	}
	type berthKey struct {
		id      string
		classes ClassSet
	}
	key := berthKey{"a", explicit}
	cache := map[berthKey]int{key: 1}
	copied := key
	copied.classes, _ = NewClassSet("legacy")
	if cache[key] != 1 || reflect.DeepEqual(key, copied) {
		t.Fatal("copy changed comparable cache key")
	}
	invalid := ClassSet(16)
	if invalid.Allows("legacy") {
		t.Fatal("unknown bits admit legacy")
	}
	if _, err := jsonv2.Marshal(invalid); err == nil {
		t.Fatal("unknown bits serialize")
	}
}

func TestClassSetStrictJSON(t *testing.T) {
	t.Parallel()
	for _, decoder := range []struct {
		name   string
		decode func([]byte, any) error
	}{
		{"legacy", json.Unmarshal},
		{"v2", func(raw []byte, out any) error { return jsonv2.Unmarshal(raw, out) }},
	} {
		t.Run(decoder.name, func(t *testing.T) {
			t.Parallel()
			for _, raw := range []string{`null`, `[]`, `"legacy"`, `{}`, `[null]`, `[1]`, `[{}]`, `[[]]`, `["unknown"]`, `["legacy","legacy"]`, `["legacy","compact","group","express","legacy"]`} {
				set, _ := NewClassSet("express")
				before := set
				if err := decoder.decode([]byte(raw), &set); err == nil {
					t.Fatalf("accepted %s", raw)
				}
				if set != before {
					t.Fatalf("rejected %s changed set", raw)
				}
			}
			for _, raw := range []string{`["legacy"] true`, `["legacy"] x`} {
				var set ClassSet
				if err := decoder.decode([]byte(raw), &set); err == nil {
					t.Fatalf("accepted trailing data %s", raw)
				}
			}
			for _, raw := range []string{`["legacy"]`, `["express","legacy"]`, `["legacy","compact","group","express"]`} {
				var set ClassSet
				if err := decoder.decode([]byte(raw), &set); err != nil {
					t.Fatal(err)
				}
				encoded, err := jsonv2.Marshal(set)
				if err != nil {
					t.Fatal(err)
				}
				var got ClassSet
				if err := decoder.decode(encoded, &got); err != nil || set != got {
					t.Fatalf("round trip %s: %v", raw, err)
				}
			}
		})
	}
}

func TestClassSetRawDecoderAtomicity(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`["legacy"] x`, `["legacy"] null`, `["legacy","legacy"]`} {
		set, _ := NewClassSet("express")
		before := set
		if err := set.UnmarshalJSON([]byte(raw)); err == nil || set != before {
			t.Fatalf("raw decoder accepted or changed on %s", raw)
		}
	}
}

func TestClassSetCanonicalAndOmittedMembers(t *testing.T) {
	t.Parallel()
	set, _ := NewClassSet("express", "compact")
	raw, err := jsonv2.Marshal(struct {
		Classes ClassSet `json:"vehicleClasses,omitzero"`
	}{set})
	if err != nil || string(raw) != `{"vehicleClasses":["compact","express"]}` {
		t.Fatalf("canonical %s: %v", raw, err)
	}
	raw, err = jsonv2.Marshal(struct {
		Classes ClassSet `json:"vehicleClasses,omitzero"`
	}{})
	if err != nil || string(raw) != `{}` {
		t.Fatalf("omitted %s: %v", raw, err)
	}
	defaultJSON, err := jsonv2.Marshal(ClassSet(0))
	if err != nil || string(defaultJSON) != `["legacy","compact"]` {
		t.Fatalf("default %s: %v", defaultJSON, err)
	}
}
