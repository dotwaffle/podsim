// Package wirename lists the JSON member names of the wire and file
// formats. Tests use it to keep each member name in lowerCamel case and
// to keep the member names and paths of the bounded scanners in step
// with the struct tags.
package wirename

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Member is one JSON member of a struct type.
type Member struct {
	// Path is the JSON Pointer of the member from its root. "*" stands
	// for each array index and each map key.
	Path string
	// Owner is the struct type that declares the field.
	Owner reflect.Type
	// Field is the Go field name.
	Field string
	// Name is the JSON member name.
	Name string
}

// Custom is a type with its own JSON methods. Walk does not look into it,
// so its member names need their own test.
type Custom struct {
	Path string
	Type reflect.Type
}

var (
	marshalerTo     = reflect.TypeFor[json.MarshalerTo]()
	marshaler       = reflect.TypeFor[json.Marshaler]()
	unmarshalerFrom = reflect.TypeFor[json.UnmarshalerFrom]()
	unmarshaler     = reflect.TypeFor[json.Unmarshaler]()
	rawValue        = reflect.TypeFor[jsontext.Value]()
)

// Walk lists the members that the JSON form of each root can contain. It
// follows the encoding/json/v2 rules for names: a tag name replaces the
// field name, "-" omits the field, and an embedded struct without a tag
// name, or a field with the inline option, puts its members in the parent
// object. A type with its own JSON methods is listed in custom, and Walk
// looks into it only when it is a struct, because such a type often
// delegates to its fields.
func Walk(roots ...any) (members []Member, custom []Custom) {
	w := walker{active: map[reflect.Type]bool{}}
	for _, root := range roots {
		w.value(reflect.TypeOf(root), "")
	}
	return w.members, w.custom
}

type walker struct {
	members []Member
	custom  []Custom
	active  map[reflect.Type]bool
}

func (w *walker) value(t reflect.Type, path string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == rawValue {
		return
	}
	if implementsJSON(t) {
		w.custom = append(w.custom, Custom{Path: path, Type: t})
		if t.Kind() != reflect.Struct {
			return
		}
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		if t.Kind() != reflect.Map && t.Elem().Kind() == reflect.Uint8 {
			return
		}
		w.value(t.Elem(), path+"/*")
	case reflect.Struct:
		if w.active[t] {
			return
		}
		w.active[t] = true
		w.fields(t, t, path)
		delete(w.active, t)
	default:
	}
}

func (w *walker) fields(owner, t reflect.Type, path string) {
	for field := range t.Fields() {
		name, inline, skip := tagName(field)
		if skip {
			continue
		}
		if inline {
			inner := field.Type
			for inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct && !implementsJSON(inner) {
				w.fields(inner, inner, path)
				continue
			}
		}
		w.members = append(w.members, Member{Path: path + "/" + name, Owner: owner, Field: field.Name, Name: name})
		w.value(field.Type, path+"/"+name)
	}
}

// tagName returns the JSON name of a field, whether the field is inlined,
// and whether JSON omits it.
func tagName(field reflect.StructField) (name string, inline, skip bool) {
	tag, tagged := field.Tag.Lookup("json")
	if tag == "-" {
		return "", false, true
	}
	name, options, _ := strings.Cut(tag, ",")
	if strings.HasPrefix(name, "'") {
		name = strings.Trim(name, "'")
	}
	for option := range strings.SplitSeq(options, ",") {
		if option == "inline" || option == "unknown" {
			inline = true
		}
	}
	if !field.IsExported() && (!field.Anonymous || field.Type.Kind() != reflect.Struct) {
		return "", false, true
	}
	if field.Anonymous && (!tagged || name == "") {
		inline = true
	}
	if name == "" {
		name = field.Name
	}
	return name, inline, false
}

func implementsJSON(t reflect.Type) bool {
	p := reflect.PointerTo(t)
	for _, method := range []reflect.Type{marshalerTo, marshaler, unmarshalerFrom, unmarshaler} {
		if t.Implements(method) || p.Implements(method) {
			return true
		}
	}
	return false
}

// LowerCamel reports whether name starts with a lowercase letter and has
// only letters and digits.
func LowerCamel(name string) bool {
	first, _ := utf8.DecodeRuneInString(name)
	if !unicode.IsLower(first) {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// Names returns the set of member names of members.
func Names(members []Member) map[string]bool {
	names := make(map[string]bool, len(members))
	for _, member := range members {
		names[member.Name] = true
	}
	return names
}

// Paths returns the set of member paths of members.
func Paths(members []Member) map[string]bool {
	paths := make(map[string]bool, len(members))
	for _, member := range members {
		paths[member.Path] = true
	}
	return paths
}

// Literal is a string literal in a Go source file.
type Literal struct {
	Position string
	Value    string
}

// Mismatch returns the literals of the Go source files that look like a
// member name or a JSON Pointer but do not use the exact case of the names.
// A literal that is one word, or one segment of a literal that starts with
// "/", is a mismatch when it matches a name only without case.
func Mismatch(files []string, names map[string]bool) ([]Literal, error) {
	folded := make(map[string]bool, len(names))
	for name := range names {
		folded[strings.ToLower(name)] = true
	}
	near := func(word string) bool {
		return !names[word] && folded[strings.ToLower(word)]
	}
	var found []Literal
	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				return true
			}
			words := []string{value}
			if strings.HasPrefix(value, "/") {
				words = strings.Split(value[1:], "/")
			}
			if slices.ContainsFunc(words, near) {
				found = append(found, Literal{Position: fset.Position(literal.Pos()).String(), Value: value})
			}
			return true
		})
	}
	return found, nil
}
