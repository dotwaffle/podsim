package project

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestDecodeCanonicalJSONCheckpointComponent(t *testing.T) {
	config := Default()
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	padded := append([]byte{'{'}, bytes.Repeat([]byte{' '}, MaxFileBytes)...)
	padded = append(padded, raw[1:]...)
	var ordinary Config
	if err = json.Unmarshal(padded, &ordinary); err == nil {
		t.Fatal("ordinary project raw cap changed")
	}
	got, err := DecodeCanonicalJSON(padded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, config) {
		t.Fatal("checkpoint component changed project")
	}
	if _, err := DecodeCanonicalJSON(make([]byte, 8*MaxFileBytes+1)); err == nil {
		t.Fatal("checkpoint raw cap bypassed")
	}
}

func TestDecodeCanonicalJSONUsesTypedChecks(t *testing.T) {
	for _, raw := range []string{`{"version":"1"}`, `{"version":1,"unknown":true}`, `{"version":1,"network":{"stations":[{"banks":null}]}}`, `{"version":1}`, `null`} {
		if got, err := DecodeCanonicalJSON([]byte(raw)); err == nil || !reflect.DeepEqual(got, Config{}) {
			t.Fatal("invalid component accepted or returned partial config", raw, err)
		}
	}
}
