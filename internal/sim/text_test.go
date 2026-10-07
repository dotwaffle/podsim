package sim

import (
	"strings"
	"testing"
)

func TestIDCharacters(t *testing.T) {
	t.Parallel()
	for c := range 256 {
		if want := strings.IndexByte(IDCharacters, byte(c)) >= 0; ValidIDText(string([]byte{byte(c)})) != want {
			t.Fatalf("byte %#x: ValidIDText is %v", c, !want)
		}
	}
	if !ValidIDText("") || ValidIDText("a_b") || ValidIDText("a b") || ValidIDText("é") {
		t.Fatal("ValidIDText accepts a character other than IDCharacters")
	}
}

func TestValidText(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]bool{
		"": true, "Harbor & Market": true, "é中 ": true, `"\<>`: true,
		"a\x00": false, "a\tb": false, "a\x7f": false, "a\u0085": false, "a\u009f": false, "\xff": false,
	} {
		if ValidText(text) != want {
			t.Fatalf("ValidText(%q) is %v", text, !want)
		}
	}
}
