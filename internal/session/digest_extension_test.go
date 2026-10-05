package session

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// checkDigestExtensions checks the extension fields in the type tree of
// root against table. It reports a malformed tag, a duplicate N, an
// extension field without a table entry, a table entry without its field,
// a table that is not in increasing order of N, and an extension field
// that does not have a fixed path. A field in a slice, array or map
// element, or in the value of another extension field, does not have a
// fixed path. It also reports an extension field with a kind, also in its
// contents, that the digest writer cannot write.
func checkDigestExtensions(root reflect.Type, table []digestExtension) error {
	var errs []error
	fields := map[uint64]string{}
	var walk func(typ reflect.Type, path, inside string)
	walk = func(typ reflect.Type, path, inside string) {
		switch typ.Kind() {
		case reflect.Pointer:
			walk(typ.Elem(), path, inside)
		case reflect.Slice, reflect.Array:
			walk(typ.Elem(), path+"[]", "a slice or array element")
		case reflect.Map:
			walk(typ.Key(), path+"[key]", "a map element")
			walk(typ.Elem(), path+"[]", "a map element")
		case reflect.Struct:
			for field := range typ.Fields() {
				fieldPath := strings.TrimPrefix(path+"."+field.Name, ".")
				n, tagged, err := extensionTag(field)
				switch {
				case err != nil:
					errs = append(errs, fmt.Errorf("%s: %w", fieldPath, err))
				case !tagged:
					walk(field.Type, fieldPath, inside)
					continue
				case inside != "":
					errs = append(errs, fmt.Errorf("extension field %s is in %s", fieldPath, inside))
				case fields[n] != "":
					errs = append(errs, fmt.Errorf("extension fields %s and %s have the same N %d", fields[n], fieldPath, n))
				default:
					fields[n] = fieldPath
				}
				if err := checkDigestKinds(field.Type, fieldPath, map[reflect.Type]bool{}); err != nil {
					errs = append(errs, err)
				}
				walk(field.Type, fieldPath, "the extension field "+fieldPath)
			}
		default:
		}
	}
	walk(root, "", "")
	listed := map[uint64]bool{}
	for index, entry := range table {
		listed[entry.n] = true
		if index > 0 && entry.n <= table[index-1].n {
			errs = append(errs, fmt.Errorf("table entry %d has N %d after N %d, want an increasing N", index, entry.n, table[index-1].n))
		}
		// A table entry must name a field of the walk. Thus it has a
		// positive N and a path, because extensionTag refuses N 0 and
		// each field path has a name.
		if path, found := fields[entry.n]; !found || path != entry.path {
			errs = append(errs, fmt.Errorf("table entry N %d at %s has no extension field with that N at that path", entry.n, entry.path))
		}
	}
	for n, path := range fields {
		if !listed[n] {
			errs = append(errs, fmt.Errorf("extension field %s with N %d has no table entry", path, n))
		}
	}
	return errors.Join(errs...)
}

// checkDigestKinds reports a kind in typ that digestWriter.value cannot
// write. The kinds are the kinds of the switch in value. An unset
// extension field is not written, so without this check a baseline test
// does not find such a kind.
func checkDigestKinds(typ reflect.Type, path string, seen map[reflect.Type]bool) error {
	if seen[typ] {
		return nil
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Bool, reflect.String, reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil
	case reflect.Pointer, reflect.Slice:
		return checkDigestKinds(typ.Elem(), path+"[]", seen)
	case reflect.Struct:
		var errs []error
		for field := range typ.Fields() {
			errs = append(errs, checkDigestKinds(field.Type, path+"."+field.Name, seen))
		}
		return errors.Join(errs...)
	default:
		return fmt.Errorf("extension field %s has the kind %s, which the digest cannot write", path, typ.Kind())
	}
}

// TestDigestExtensionRegistry checks the registry against the Command type
// tree, including project.Config.
func TestDigestExtensionRegistry(t *testing.T) {
	t.Parallel()
	if err := checkDigestExtensions(reflect.TypeFor[Command](), digestExtensions); err != nil {
		t.Fatal(err)
	}
}

type registryInner struct {
	Value string `digest:"ext=2"`
}

type registryValid struct {
	First string `digest:"ext=1"`
	Inner *registryInner
	Plain []int
}

type registryDuplicate struct {
	First  string `digest:"ext=1"`
	Second string `digest:"ext=1"`
}

type registryTwoPaths struct {
	First  registryInner
	Second *registryInner
}

type registryZero struct {
	Value string `digest:"ext=0"`
}

type registryText struct {
	Value string `digest:"ext=x"`
}

type registryNoExt struct {
	Value string `digest:"1"`
}

type registrySlice struct{ Items []registryInner }

type registryPointerSlice struct{ Items []*registryInner }

type registryArray struct{ Items [2]registryInner }

type registryMapValue struct{ Items map[string]registryInner }

type registryMapKey struct{ Items map[registryInner]int }

type registryInterface struct {
	Value any `digest:"ext=1"`
}

type registryArrayKind struct {
	Value [1]int `digest:"ext=1"`
}

type registryMapKind struct {
	Value map[string]int `digest:"ext=1"`
}

type registryNestedKind struct {
	Value *struct{ Items []complex128 } `digest:"ext=1"`
}

type registryAllKinds struct {
	Value *registryKinds `digest:"ext=1"`
}

type registryKinds struct {
	B   bool
	I   int
	I8  int8
	I16 int16
	I32 int32
	I64 int64
	U   uint
	U8  uint8
	U16 uint16
	U32 uint32
	U64 uint64
	F32 float32
	F64 float64
	S   string
	P   *string
	L   []int
	N   struct{ X int }
}

type registryNested struct {
	Outer registryInner `digest:"ext=1"`
}

// TestDigestExtensionRegistryRejects checks that the registry check refuses
// each kind of fault in a type tree or in a table.
func TestDigestExtensionRegistryRejects(t *testing.T) {
	t.Parallel()
	validTable := []digestExtension{{1, "First"}, {2, "Inner.Value"}}
	tests := []struct {
		name  string
		root  reflect.Type
		table []digestExtension
		// want is a part of the error, or empty for no error.
		want string
	}{
		{"valid", reflect.TypeFor[registryValid](), validTable, ""},
		{"no extension field", reflect.TypeFor[extensionPlain](), nil, ""},
		{"duplicate N", reflect.TypeFor[registryDuplicate](), []digestExtension{{1, "First"}}, "have the same N 1"},
		{"one field at two paths", reflect.TypeFor[registryTwoPaths](), []digestExtension{{2, "First.Value"}}, "have the same N 2"},
		{"missing table entry", reflect.TypeFor[registryValid](), validTable[:1], "Inner.Value with N 2 has no table entry"},
		{"table entry without field", reflect.TypeFor[registryValid](), append(validTable[:2:2], digestExtension{3, "Plain"}), "N 3 at Plain has no extension field"},
		{"table entry at another path", reflect.TypeFor[registryValid](), []digestExtension{{1, "Inner"}, {2, "Inner.Value"}}, "N 1 at Inner has no extension field"},
		{"misordered table", reflect.TypeFor[registryValid](), []digestExtension{validTable[1], validTable[0]}, "want an increasing N"},
		{"duplicate table entry", reflect.TypeFor[registryValid](), []digestExtension{validTable[0], validTable[0], validTable[1]}, "want an increasing N"},
		{"N 0", reflect.TypeFor[registryZero](), nil, "want ext=N with a positive N"},
		{"N not a number", reflect.TypeFor[registryText](), nil, "want ext=N with a positive N"},
		{"no ext=", reflect.TypeFor[registryNoExt](), nil, "want ext=N with a positive N"},
		{"in a slice", reflect.TypeFor[registrySlice](), []digestExtension{{2, "Items[].Value"}}, "is in a slice or array element"},
		{"in a slice of pointers", reflect.TypeFor[registryPointerSlice](), []digestExtension{{2, "Items[].Value"}}, "is in a slice or array element"},
		{"in an array", reflect.TypeFor[registryArray](), []digestExtension{{2, "Items[].Value"}}, "is in a slice or array element"},
		{"in a map value", reflect.TypeFor[registryMapValue](), []digestExtension{{2, "Items[].Value"}}, "is in a map element"},
		{"in a map key", reflect.TypeFor[registryMapKey](), []digestExtension{{2, "Items[key].Value"}}, "is in a map element"},
		{"table entry without a path", reflect.TypeFor[extensionPlain](), []digestExtension{{1, ""}}, "N 1 at  has no extension field"},
		{"table entry with N 0", reflect.TypeFor[extensionPlain](), []digestExtension{{0, "A"}}, "N 0 at A has no extension field"},
		{"each kind that the digest writes", reflect.TypeFor[registryAllKinds](), []digestExtension{{1, "Value"}}, ""},
		{"interface", reflect.TypeFor[registryInterface](), []digestExtension{{1, "Value"}}, "has the kind interface"},
		{"array", reflect.TypeFor[registryArrayKind](), []digestExtension{{1, "Value"}}, "has the kind array"},
		{"map", reflect.TypeFor[registryMapKind](), []digestExtension{{1, "Value"}}, "has the kind map"},
		{"kind in the contents", reflect.TypeFor[registryNestedKind](), []digestExtension{{1, "Value"}}, "Value[].Items[] has the kind complex128"},
		{"in an extension field", reflect.TypeFor[registryNested](), []digestExtension{{1, "Outer"}, {2, "Outer.Value"}}, "is in the extension field Outer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkDigestExtensions(tc.root, tc.table)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("error %v, want none", err)
			case tc.want != "" && err == nil:
				t.Fatalf("no error, want %q", tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), tc.want):
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
}

// extensionPlain is extensionHost without its extension fields.
type extensionPlain struct {
	A     int
	Inner *extensionPlainInner
	C     string
}

type extensionPlainInner struct {
	F int
}

// extensionHost has extension fields at the root and under a pointer, of
// the kinds that a command has.
type extensionHost struct {
	A     int
	B     []int `digest:"ext=1"`
	Inner *extensionInner
	C     string
	D     *float64       `digest:"ext=2"`
	G     string         `digest:"ext=4"`
	H     float64        `digest:"ext=5"`
	K     extensionFloat `digest:"ext=6"`
}

type extensionFloat struct {
	X float32
}

type extensionInner struct {
	E string `digest:"ext=3"`
	F int
}

// recordingHash keeps its input, so that a test can check the bytes that
// the digest writer writes.
type recordingHash struct{ bytes.Buffer }

func (h *recordingHash) Sum(b []byte) []byte { return append(b, h.Bytes()...) }
func (*recordingHash) Size() int             { return 0 }
func (*recordingHash) BlockSize() int        { return 1 }

// digestInput returns the bytes that the digest writer hashes for root.
func digestInput(root any) []byte {
	var input recordingHash
	writer := digestWriter{hash: &input}
	writer.value(reflect.ValueOf(root))
	writer.trailer()
	return input.Bytes()
}

// TestDigestExtensionInput checks the hash input. An unset extension field
// writes nothing, not even a nil mark, so the input equals the input of the
// type without the field. A set field goes in the trailer after the
// complete main walk, as the count and then N and the value of each field
// in walk order.
func TestDigestExtensionInput(t *testing.T) {
	t.Parallel()
	uvarint := func(numbers ...uint64) []byte {
		var out []byte
		for _, number := range numbers {
			out = binary.AppendUvarint(out, number)
		}
		return out
	}
	text := func(value string) []byte { return append(binary.AppendVarint(nil, int64(len(value))), value...) }
	// main is the input of the main walk with A 1, Inner{F 5} and C "c".
	main := slices.Concat(binary.AppendVarint(nil, 1), uvarint(1), binary.AppendVarint(nil, 5), text("c"))
	half := 0.5
	tests := []struct {
		name string
		host extensionHost
		want []byte
	}{
		{"nothing set", extensionHost{}, digestInput(extensionPlain{Inner: nil})},
		{"nothing set, as the plain type", extensionHost{A: 1, Inner: &extensionInner{F: 5}, C: "c"},
			digestInput(extensionPlain{A: 1, Inner: &extensionPlainInner{F: 5}, C: "c"})},
		{"nothing set, bytes", extensionHost{A: 1, Inner: &extensionInner{F: 5}, C: "c"}, main},
		{"empty slice", extensionHost{A: 1, B: []int{}, Inner: &extensionInner{F: 5}, C: "c"},
			slices.Concat(main, uvarint(1, 1, 1), binary.AppendVarint(nil, 0))},
		{"nested field at the end", extensionHost{A: 1, Inner: &extensionInner{E: "e", F: 5}, C: "c"},
			slices.Concat(main, uvarint(1, 3), text("e"))},
		{"walk order", extensionHost{A: 1, B: []int{7}, Inner: &extensionInner{E: "e", F: 5}, C: "c", D: &half, G: "g"},
			slices.Concat(main, uvarint(4, 1, 1), binary.AppendVarint(nil, 1), binary.AppendVarint(nil, 7),
				uvarint(3), text("e"), uvarint(2, 1, math.Float64bits(half)), uvarint(4), text("g"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := digestInput(tc.host); !bytes.Equal(got, tc.want) {
				t.Fatalf("input %x, want %x", got, tc.want)
			}
		})
	}
}

// TestDigestExtensionMatchesDeepEqual checks that two values with extension
// fields have matching digests exactly when reflect.DeepEqual reports them
// equal. It also checks that each extension field set alone changes the
// digest, and that two values that set the same value in different
// extension fields have different digests.
func TestDigestExtensionMatchesDeepEqual(t *testing.T) {
	t.Parallel()
	zero, negativeZero, one, nan := 0.0, math.Copysign(0, -1), 1.0, math.NaN()
	base := func() extensionHost { return extensionHost{A: 1, Inner: &extensionInner{F: 5}, C: "c"} }
	tests := []struct {
		name string
		// change changes the two values.
		change func(first, second *extensionHost)
	}{
		{"same content", func(_, _ *extensionHost) {}},
		{"slice set alone", func(_, second *extensionHost) { second.B = []int{1} }},
		{"pointer set alone", func(_, second *extensionHost) { second.D = &one }},
		{"pointer to zero set alone", func(_, second *extensionHost) { second.D = &zero }},
		{"nested text set alone", func(_, second *extensionHost) { second.Inner.E = "x" }},
		{"root text set alone", func(_, second *extensionHost) { second.G = "x" }},
		{"same text in another field", func(first, second *extensionHost) { first.Inner.E, second.G = "x", "x" }},
		{"empty in place of nil", func(_, second *extensionHost) { second.B = []int{} }},
		{"equal set fields", func(first, second *extensionHost) {
			first.B, first.D, first.G = []int{2}, &one, "g"
			second.B, second.D, second.G = []int{2}, new(1.0), "g"
		}},
		{"extension with negative zero", func(first, second *extensionHost) { first.D, second.D = &zero, &negativeZero }},
		{"float extension at negative zero", func(first, second *extensionHost) { first.H, second.H = zero, negativeZero }},
		{"float extension set alone", func(_, second *extensionHost) { second.H = one }},
		{"float in a struct extension at negative zero", func(first, second *extensionHost) {
			first.K.X, second.K.X = float32(zero), float32(negativeZero)
		}},
		{"float in a struct extension set alone", func(_, second *extensionHost) { second.K.X = 1 }},
		{"extension with NaN in one", func(first, second *extensionHost) { first.D, second.D = &one, &nan }},
		{"extension with NaN in both", func(first, second *extensionHost) { first.D, second.D = new(nan), new(nan) }},
		{"no inner and an inner without extension", func(_, second *extensionHost) { second.Inner = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first, second := base(), base()
			tc.change(&first, &second)
			want := reflect.DeepEqual(first, second)
			firstDigest, secondDigest := digestValue(reflect.ValueOf(first)), digestValue(reflect.ValueOf(second))
			if got := firstDigest.matches(secondDigest); got != want {
				t.Fatalf("digests match = %v, reflect.DeepEqual = %v", got, want)
			}
			if !firstDigest.unmatched && !secondDigest.unmatched && (firstDigest.sum == secondDigest.sum) != want {
				t.Fatalf("sums equal = %v, reflect.DeepEqual = %v", firstDigest.sum == secondDigest.sum, want)
			}
		})
	}
}

// TestDigestPanicsOnMalformedTag checks that the digest walk refuses a
// digest tag that is not ext=N with a positive N, as it refuses a kind that
// a command does not have.
func TestDigestPanicsOnMalformedTag(t *testing.T) {
	t.Parallel()
	for _, root := range []any{registryZero{}, registryText{}, registryNoExt{}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("digest of %T did not panic", root)
				}
			}()
			digestValue(reflect.ValueOf(root))
		}()
	}
}
