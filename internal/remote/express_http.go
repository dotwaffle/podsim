package remote

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"io"
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
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		switch token.String() {
		case "orderContract", "textEncoding":
			return errors.New("express state markers require the qualified media type")
		case "couplingContract", "couplingEnabled", "couplingGroups", "couplingSites", "couplingCorridors", "couplingID":
			return errors.New("coupling state markers require the qualified media type")
		}
	}
}
