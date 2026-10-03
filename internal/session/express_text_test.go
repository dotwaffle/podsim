package session

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestExpressOrderText(t *testing.T) {
	for _, text := range []string{"", "plain", "\x00\"\\\n", "é中", strings.Repeat("x", 64)} {
		packed, err := packOrderText(text, 64)
		if err != nil {
			t.Fatal(err)
		}
		got, err := unpackOrderText([]byte(`"`+packed+`"`), 64)
		if err != nil || got != text {
			t.Fatalf("round trip %q: %q %v", text, got, err)
		}
	}
	for _, raw := range []string{`null`, `"eA"`, `"eA==\n"`, `"\u0065A=="`, `"eB=="`, `"_w=="`, `"/w=="`, `"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 65))) + `"`} {
		if _, err := unpackOrderText([]byte(raw), 64); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if _, err := packOrderText(string([]byte{255}), 64); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func FuzzExpressOrderText(f *testing.F) {
	for _, s := range []string{`"eA=="`, `"\u0065A=="`, `null`, `"/w=="`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		text, err := unpackOrderText([]byte(raw), 64)
		if err != nil {
			return
		}
		packed, err := packOrderText(text, 64)
		if err != nil || raw != `"`+packed+`"` {
			t.Fatalf("noncanonical accepted %q", raw)
		}
	})
}

func FuzzExpressPublicDecode(f *testing.F) {
	for _, raw := range []string{
		`{"orderContract":"express-v1","textEncoding":"order-text-base64-v1","kind":"full","stream":"fuzz","sequence":"1"}`,
		`{"orderContract":null,"textEncoding":"order-text-base64-v1"}`,
		`{"orderContract":"express-v1","textEncoding":"order-text-base64-v1","delta":{"groups":{"pending":[{"From":"eA==","To":"eQ=="}]}}}`,
	} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 {
			t.Skip()
		}
		envelope, err := DecodeStreamJSONVersion([]byte(raw), 4)
		if err != nil {
			return
		}
		encoded, err := EncodeStreamJSON(envelope)
		if err != nil {
			t.Fatal("accepted data cannot encode", err)
		}
		if _, err = DecodeStreamJSONVersion(encoded, 4); err != nil {
			t.Fatal("accepted data cannot decode again", err)
		}
	})
}
