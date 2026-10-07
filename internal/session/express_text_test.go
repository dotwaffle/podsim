package session

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestExpressOrderText(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		text string
		free bool
	}{
		{"", false}, {"plain", false}, {"a.B+0-", false}, {strings.Repeat("x", 64), false},
		{"", true}, {"\"\\<&>", true}, {"é中\u2028", true}, {strings.Repeat("x", 1024), true},
	} {
		limit := orderIDBytes
		if test.free {
			limit = orderTextBytes
		}
		packed, err := packOrderText(test.text, limit)
		if err != nil {
			t.Fatal(err)
		}
		got, err := unpackOrderText([]byte(`"`+packed+`"`), test.free)
		if err != nil || got != test.text {
			t.Fatalf("round trip %q: %q %v", test.text, got, err)
		}
	}
	for _, raw := range []string{`null`, `"eA"`, `"eA==\n"`, `"\u0065A=="`, `"eB=="`, `"_w=="`, `"/w=="`, `"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 65))) + `"`} {
		if _, err := unpackOrderText([]byte(raw), false); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	// The character check comes after the canonical check.
	for _, test := range []struct {
		text string
		free bool
		want string
	}{
		{"a_b", false, "order ID has a character other than A-Z, a-z, 0-9, '.', '+' or '-'"},
		{"é", false, "order ID has a character other than A-Z, a-z, 0-9, '.', '+' or '-'"},
		{"a\x00", true, "order text has a control character"},
		{"a\u0085", true, "order text has a control character"},
	} {
		raw := `"` + base64.StdEncoding.EncodeToString([]byte(test.text)) + `"`
		if _, err := unpackOrderText([]byte(raw), test.free); err == nil || err.Error() != test.want {
			t.Fatalf("%q: got %v, want %q", test.text, err, test.want)
		}
		if _, err := unpackOrderText([]byte(raw[:len(raw)-2]+`"`), test.free); err == nil || err.Error() == test.want {
			t.Fatalf("%q: the character check came before the canonical check: %v", test.text, err)
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
		text, err := unpackOrderText([]byte(raw), false)
		if err != nil {
			return
		}
		packed, err := packOrderText(text, orderIDBytes)
		if err != nil || raw != `"`+packed+`"` {
			t.Fatalf("noncanonical accepted %q", raw)
		}
	})
}

func FuzzExpressPublicDecode(f *testing.F) {
	for _, raw := range []string{
		`{"orderContract":"express-v1","textEncoding":"order-text-base64-v1","kind":"full","stream":"fuzz","sequence":"1"}`,
		`{"orderContract":null,"textEncoding":"order-text-base64-v1"}`,
		`{"orderContract":"express-v1","textEncoding":"order-text-base64-v1","delta":{"groups":{"pending":[{"from":"eA==","to":"eQ=="}]}}}`,
	} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 8192 {
			t.Skip()
		}
		envelope, err := DecodeStreamJSON([]byte(raw))
		if err != nil {
			return
		}
		encoded, err := EncodeStreamJSON(envelope)
		if err != nil {
			t.Fatal("accepted data cannot encode", err)
		}
		if _, err = DecodeStreamJSON(encoded); err != nil {
			t.Fatal("accepted data cannot decode again", err)
		}
	})
}
