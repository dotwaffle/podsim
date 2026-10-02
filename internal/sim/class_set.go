package sim

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
)

// ClassSet stores a bounded vehicle-class allowlist by value. Its zero
// value admits legacy and compact pods, as an omitted project member does.
// Value storage keeps berths comparable and network copies independent.
//
//nolint:recvcheck // JSON encoding reads a value. Decoding must replace a pointer.
type ClassSet uint8

const allClassBits ClassSet = 0b1111

var classSetNames = [...]string{"legacy", "compact", "group", "express"}

// NewClassSet accepts one through four distinct known class IDs.
func NewClassSet(classes ...string) (ClassSet, error) {
	if len(classes) < 1 || len(classes) > len(classSetNames) {
		return 0, errors.New("a vehicle-class allowlist needs 1 to 4 entries")
	}
	var result ClassSet
	for _, class := range classes {
		bit := classBit(class)
		if bit == 0 {
			return 0, fmt.Errorf("unknown vehicle class %q", class)
		}
		if result&bit != 0 {
			return 0, fmt.Errorf("duplicate vehicle class %q", class)
		}
		result |= bit
	}
	return result, nil
}

func classBit(class string) ClassSet {
	for index, name := range classSetNames {
		if class == name {
			return 1 << index
		}
	}
	return 0
}

// Allows reports whether class is admitted. An omitted pod class means legacy.
// An invalid native bit set admits no class.
func (set ClassSet) Allows(class string) bool {
	if set&^allClassBits != 0 {
		return false
	}
	if class == "" {
		class = "legacy"
	}
	if set == 0 {
		set = 0b0011
	}
	return set&classBit(class) != 0
}

// MarshalJSON emits the effective allowlist in stable class order.
// Struct fields can use omitzero to preserve omitted legacy members.
func (set ClassSet) MarshalJSON() ([]byte, error) {
	if set&^allClassBits != 0 {
		return nil, errors.New("invalid vehicle-class allowlist bits")
	}
	var names []string
	for _, name := range classSetNames {
		if set.Allows(name) {
			names = append(names, name)
		}
	}
	return json.Marshal(names)
}

// MarshalJSONTo preserves the same allowlist shape with JSON v2.
func (set ClassSet) MarshalJSONTo(encoder *jsontext.Encoder) error {
	raw, err := set.MarshalJSON()
	if err != nil {
		return err
	}
	return encoder.WriteValue(raw)
}

// UnmarshalJSON decodes a strict bounded list without allocating its elements.
// A rejected list leaves the previous set unchanged.
func (set *ClassSet) UnmarshalJSON(raw []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	next, err := decodeClassSet(decoder)
	if err != nil {
		return err
	}
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("vehicle-class allowlist has trailing data")
	}
	*set = next
	return nil
}

// UnmarshalJSONFrom checks list shape before decoding any member collection.
func (set *ClassSet) UnmarshalJSONFrom(decoder *jsontext.Decoder) error {
	next, err := decodeClassSet(decoder)
	if err != nil {
		return err
	}
	*set = next
	return nil
}

func decodeClassSet(decoder *jsontext.Decoder) (ClassSet, error) {
	token, err := decoder.ReadToken()
	if err != nil {
		return 0, err
	}
	if token.Kind() != jsontext.KindBeginArray {
		return 0, errors.New("vehicle-class allowlist must be a nonempty array")
	}
	var next ClassSet
	for count := 0; ; count++ {
		if decoder.PeekKind() == jsontext.KindEndArray {
			if count == 0 {
				return 0, errors.New("vehicle-class allowlist must be a nonempty array")
			}
			_, err = decoder.ReadToken()
			return next, err
		}
		if count == len(classSetNames) {
			return 0, errors.New("vehicle-class allowlist has more than 4 entries")
		}
		token, err = decoder.ReadToken()
		if err != nil {
			return 0, err
		}
		if token.Kind() != jsontext.KindString {
			return 0, errors.New("vehicle-class allowlist entries must be text")
		}
		bit := classBit(token.String())
		if bit == 0 {
			return 0, errors.New("vehicle-class allowlist has an unknown class")
		}
		if next&bit != 0 {
			return 0, errors.New("vehicle-class allowlist repeats a class")
		}
		next |= bit
	}
}
