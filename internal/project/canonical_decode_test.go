package project

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"testing"
)

func TestDecodeCanonicalJSONCheckpointComponent(t *testing.T) {
	config := Default()
	raw, err := jsonv2.Marshal(config, json.DefaultOptionsV1())
	if err != nil {
		t.Fatal(err)
	}
	padded := append([]byte{'{'}, bytes.Repeat([]byte{' '}, MaxFileBytes)...)
	padded = append(padded, raw[1:]...)
	var ordinary Config
	if err = jsonv2.Unmarshal(padded, &ordinary, json.DefaultOptionsV1()); err == nil {
		t.Fatal("ordinary project raw cap changed")
	}
	got, err := DecodeCanonicalJSON(padded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, config) {
		t.Fatal("checkpoint component changed project")
	}
	// The raw cap of a checkpoint component is 80 MiB.
	atCap := append([]byte{'{'}, bytes.Repeat([]byte{' '}, 80<<20-len(raw))...)
	atCap = append(atCap, raw[1:]...)
	if _, err := DecodeCanonicalJSON(atCap); err != nil {
		t.Fatal("checkpoint component at the raw cap refused", err)
	}
	if _, err := DecodeCanonicalJSON(append([]byte{' '}, atCap...)); !errors.Is(err, errTooLarge) {
		t.Fatal("checkpoint raw cap bypassed", err)
	}
}

func TestDecodeCanonicalJSONUsesTypedChecks(t *testing.T) {
	for _, raw := range []string{`{"version":"1"}`, `{"version":1,"unknown":true}`, `{"version":1,"network":{"stations":[{"banks":null}]}}`, `{"version":1}`, `null`} {
		if got, err := DecodeCanonicalJSON([]byte(raw)); err == nil || !reflect.DeepEqual(got, Config{}) {
			t.Fatal("invalid component accepted or returned partial config", raw, err)
		}
	}
}
