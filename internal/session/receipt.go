package session

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"reflect"
)

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
	writer := digestWriter{hash: sha256.New()}
	writer.value(reflect.ValueOf(command))
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
		for _, field := range v.Fields() {
			writer.value(field)
		}
	default:
		panic(fmt.Sprintf("command digest: unsupported kind %s", v.Kind()))
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
