package remote

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
	"strings"
)

// New field markers require qualified media before the legacy decoder runs.
func rejectUnqualifiedStateMarkers(raw []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(raw), jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if token.Kind() == jsontext.KindString && kind == jsontext.KindBeginObject && length%2 == 1 && (strings.EqualFold(token.String(), "orderContract") || strings.EqualFold(token.String(), "textEncoding")) {
			return errors.New("express state markers require the qualified media type")
		}
	}
}
