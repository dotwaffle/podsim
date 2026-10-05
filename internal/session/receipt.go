package session

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// digestExtension is one entry of the digest extension registry. An
// extension field has the struct tag digest:"ext=N". The digest walk skips
// it, and hashes it in a trailer after the walk only when it is set. Thus a
// new field does not change the digest of a command that does not set it.
type digestExtension struct {
	// n is the N of the tag. It is unique in the Command type tree.
	n uint64
	// path is the field path from Command, with the field names joined by
	// dots, for example "Project.IncidentContract".
	path string
}

// digestExtensions lists each extension field, in increasing order of N.
// A new extension field takes the next free N. An extension field must
// have a fixed path: it must not be in a slice element or a map element.
// TestDigestExtensionRegistry checks the table against the Command type.
var digestExtensions = []digestExtension{}

// extensionTag returns N when field has the tag digest:"ext=N".
func extensionTag(field reflect.StructField) (n uint64, tagged bool, err error) {
	tag, tagged := field.Tag.Lookup("digest")
	if !tagged {
		return 0, false, nil
	}
	text, found := strings.CutPrefix(tag, "ext=")
	n, err = strconv.ParseUint(text, 10, 64)
	if !found || err != nil || n == 0 {
		return 0, true, fmt.Errorf("field %s has the digest tag %q, want ext=N with a positive N", field.Name, tag)
	}
	return n, true, nil
}

// commandDigest identifies the content of a command. A receipt keeps the
// digest in place of the command, so that a receipt has a fixed size. Two
// commands have the same digest when reflect.DeepEqual reports them equal.
// A command with a NaN value is not equal to itself, so its digest does not
// match any digest.
type commandDigest struct {
	sum [sha256.Size]byte
	// unmatched is true when the command has a NaN value.
	unmatched bool
}

// digestCommand returns the digest of command.
func digestCommand(command Command) commandDigest {
	return digestValue(reflect.ValueOf(command))
}

// digestValue returns the digest of root. It writes the main walk of root,
// and then the trailer of the extension fields that are set. With no set
// extension field, it writes no trailer.
func digestValue(root reflect.Value) commandDigest {
	writer := digestWriter{hash: sha256.New()}
	writer.value(root)
	writer.trailer()
	var digest commandDigest
	writer.hash.Sum(digest.sum[:0])
	digest.unmatched = writer.nan
	return digest
}

// matches reports whether the two digests identify equal commands.
func (digest commandDigest) matches(other commandDigest) bool {
	return !digest.unmatched && !other.unmatched && digest.sum == other.sum
}

// digestWriter writes a value to a hash. It writes the length of each
// string and slice, and a mark for each nil pointer and nil slice, so that
// different values give different input. It writes 0 for negative zero,
// because negative zero is equal to zero. nan is true after a NaN value.
type digestWriter struct {
	hash hash.Hash
	nan  bool
	// extensions holds the set extension fields of the main walk, in walk
	// order.
	extensions []extensionValue
}

// extensionValue is an extension field that is set.
type extensionValue struct {
	n     uint64
	value reflect.Value
}

// value writes v. It panics for a kind that a command does not have, so
// that a new field of such a kind gets a test failure.
func (writer *digestWriter) value(v reflect.Value) {
	switch v.Kind() {
	case reflect.Bool:
		if v.Bool() {
			writer.uint(1)
		} else {
			writer.uint(0)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		writer.int(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		writer.uint(v.Uint())
	case reflect.Float32, reflect.Float64:
		number := v.Float()
		writer.nan = writer.nan || math.IsNaN(number)
		if number == 0 {
			number = 0
		}
		writer.uint(math.Float64bits(number))
	case reflect.String:
		writer.int(int64(v.Len()))
		_, _ = writer.hash.Write([]byte(v.String()))
	case reflect.Pointer:
		if v.IsNil() {
			writer.uint(0)
			return
		}
		writer.uint(1)
		writer.value(v.Elem())
	case reflect.Slice:
		if v.IsNil() {
			writer.uint(0)
			return
		}
		writer.uint(1)
		writer.int(int64(v.Len()))
		for index := range v.Len() {
			writer.value(v.Index(index))
		}
	case reflect.Struct:
		for structField, field := range v.Fields() {
			n, tagged, err := extensionTag(structField)
			if err != nil {
				panic("command digest: " + err.Error())
			}
			if !tagged {
				writer.value(field)
				continue
			}
			// The main walk writes nothing for an extension field. A nil
			// pointer or a nil slice is not set. A negative zero is not
			// set, as reflect.DeepEqual treats it as zero.
			if !field.IsZero() {
				writer.extensions = append(writer.extensions, extensionValue{n: n, value: field})
			}
		}
	default:
		panic(fmt.Sprintf("command digest: unsupported kind %s", v.Kind()))
	}
}

// trailer writes the count of the set extension fields, and then N and the
// value of each field. A count and a value have a known end, so the
// trailer has a known end. It writes nothing when no extension field is
// set, so that such a command keeps the digest that it had before the
// registry. TestDigestExtensionRegistry makes sure that the value of an
// extension field has no extension field in it.
func (writer *digestWriter) trailer() {
	if len(writer.extensions) == 0 {
		return
	}
	extensions := writer.extensions
	writer.extensions = nil
	writer.uint(uint64(len(extensions)))
	for _, extension := range extensions {
		writer.uint(extension.n)
		writer.value(extension.value)
	}
}

// uint writes number as a varint. A varint has a known end, so the input
// of two values cannot be the input of two other values.
func (writer *digestWriter) uint(number uint64) {
	var buffer [binary.MaxVarintLen64]byte
	_, _ = writer.hash.Write(binary.AppendUvarint(buffer[:0], number))
}

// int writes number as a varint.
func (writer *digestWriter) int(number int64) {
	var buffer [binary.MaxVarintLen64]byte
	_, _ = writer.hash.Write(binary.AppendVarint(buffer[:0], number))
}
