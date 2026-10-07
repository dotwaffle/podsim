package sim

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IDCharacters are the characters of an ID. No JSON encoder escapes them,
// so each one is one byte in each saved, stream and HTTP document. The
// editor gives a client ID with "." when the browser has no UUID source,
// and a berth ID with "+" when its number has an exponent.
const IDCharacters = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789.+-"

// ValidIDText reports whether each byte of id is one of IDCharacters. It
// does not check the length, so it reports true for an empty ID.
func ValidIDText(id string) bool {
	for i := range len(id) {
		if !idCharacter(id[i]) {
			return false
		}
	}
	return true
}

func idCharacter(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '+' || c == '-'
}

// ValidText reports whether text is valid UTF-8 without a control
// character. A control character is a C0 character, DEL or a C1
// character.
func ValidText(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsFunc(text, unicode.IsControl)
}
