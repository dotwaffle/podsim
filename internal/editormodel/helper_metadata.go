package editormodel

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"
)

const metadataBytes = 256 << 10

var retrievedPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$`)
var licenseLimits = []struct {
	key   string
	limit int
}{{"source", 200}, {"attribution", 500}, {"license", 64}, {"licenseURL", 2048}, {"copyrightURL", 2048}, {"retrieved", 40}, {"method", 10000}, {"notice", 2000}}

func rawObject(raw jsontext.Value, allowed ...string) (map[string]jsontext.Value, error) {
	if raw.Kind() != '{' {
		return nil, errors.New("metadata member must be an object")
	}
	var fields map[string]jsontext.Value
	if err := json.Unmarshal(raw, &fields, jsontext.AllowInvalidUTF8(true)); err != nil {
		return nil, fmt.Errorf("decode metadata fields: %w", err)
	}
	for key := range fields {
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("metadata has an unknown member %s", key)
		}
	}
	return fields, nil
}

func backgroundMetadata(raw jsontext.Value) (jsontext.Value, error) {
	if len(raw) > metadataBytes {
		return nil, errors.New("background metadata exceeds 256 KiB")
	}
	fields, fieldsErr := rawObject(raw, "placement", "asset", "urlFacts")
	if fieldsErr != nil {
		return nil, fieldsErr
	}
	placement := fields["placement"]
	if len(placement) != 0 {
		if _, err := rawObject(placement, "x", "y", "width", "height", "opacity"); err != nil {
			return nil, err
		}
		var value any
		if err := json.Unmarshal(placement, &value); err != nil {
			return nil, fmt.Errorf("decode placement: %w", err)
		}
		if err := backgroundPlacementError(value); err != nil {
			return nil, err
		}
	}
	asset := map[string]jsontext.Value{"frameState": jsontext.Value(`"none"`), "frame": jsontext.Value(`null`), "license": jsontext.Value(`null`)}
	if len(fields["asset"]) != 0 {
		parsed, err := rawObject(fields["asset"], "frameState", "frame", "license")
		if err != nil {
			return nil, err
		}
		for _, key := range []string{"frameState", "frame", "license"} {
			if len(parsed[key]) != 0 {
				asset[key] = parsed[key]
			}
		}
		if len(parsed["frameState"]) == 0 {
			return nil, editorMessageError("The frame state must be \"none\", \"attached\", \"detached\".")
		}
	}
	var state string
	if err := json.Unmarshal(asset["frameState"], &state); err != nil || !slices.Contains([]string{"none", "attached", "detached"}, state) {
		return nil, editorMessageError("The frame state must be \"none\", \"attached\", \"detached\".")
	}
	if state == "none" && asset["frame"].Kind() != 'n' {
		return nil, editorMessageError("A background with the frame state \"none\" cannot have a frame.")
	}
	if state != "none" {
		if asset["frame"].Kind() == 'n' {
			return nil, editorMessageError(fmt.Sprintf("A background with the frame state %q needs a frame.", state))
		}
		var frame any
		if err := json.Unmarshal(asset["frame"], &frame); err != nil {
			return nil, fmt.Errorf("decode frame: %w", err)
		}
		if err := backgroundFrameError(frame); err != nil {
			return nil, err
		}
	}
	facts := map[string]jsontext.Value{}
	if len(fields["urlFacts"]) != 0 {
		var factsErr error
		facts, factsErr = rawObject(fields["urlFacts"], "licenseURL", "copyrightURL")
		if factsErr != nil {
			return nil, factsErr
		}
	}
	if err := metadataLicense(asset["license"], facts); err != nil {
		return nil, err
	}
	// Keep raw license strings, including escaped lone UTF-16 surrogates.
	result := `{"asset":{"frameState":` + string(asset["frameState"]) + `,"frame":` + string(asset["frame"]) + `,"license":` + string(asset["license"]) + `}`
	if len(placement) != 0 {
		result += `,"placement":` + string(placement)
	}
	return jsontext.Value(result + `}`), nil
}

func metadataLicense(raw jsontext.Value, facts map[string]jsontext.Value) error {
	if raw.Kind() == 'n' {
		if len(facts) != 0 {
			return errors.New("a null license cannot have URL facts")
		}
		return nil
	}
	allowed := make([]string, len(licenseLimits))
	for i, field := range licenseLimits {
		allowed[i] = field.key
	}
	fields, err := rawObject(raw, allowed...)
	if err != nil {
		return err
	}
	for _, field := range licenseLimits {
		value, units, err := metadataText(fields[field.key])
		if err != nil {
			return editorMessageError(fmt.Sprintf("The license %s must be text.", field.key))
		}
		if len([]rune(value)) > field.limit {
			return editorMessageError(fmt.Sprintf("The license %s must have at most %d characters.", field.key, field.limit))
		}
		if field.key == "retrieved" && value != "" && !retrievedPattern.MatchString(value) {
			return editorMessageError("The license retrieved time must be an ISO 8601 time.")
		}
		if field.key != "licenseURL" && field.key != "copyrightURL" {
			continue
		}
		fact, exists := facts[field.key]
		if !exists {
			if len(units) != 0 {
				return editorMessageError(fmt.Sprintf("The license %s needs its browser URL fact.", field.key))
			}
			continue
		}
		parsed, err := rawObject(fact, "text", "https")
		if err != nil {
			return err
		}
		_, factUnits, err := metadataText(parsed["text"])
		if err != nil || !slices.Equal(units, factUnits) {
			return errors.New("URL fact text does not match the license URL")
		}
		if parsed["https"].Kind() != 't' && parsed["https"].Kind() != 'f' {
			return errors.New("URL fact HTTPS verdict must be Boolean")
		}
		if value != "" && parsed["https"].Kind() != 't' {
			return editorMessageError(fmt.Sprintf("The license %s must be an HTTPS URL.", field.key))
		}
	}
	return nil
}

// metadataText retains browser code units for exact URL fact comparisons.
func metadataText(raw jsontext.Value) (string, []uint16, error) {
	if raw.Kind() != '"' {
		return "", nil, errors.New("text required")
	}
	input := string(raw[1 : len(raw)-1])
	var units []uint16
	for input != "" {
		if input[0] != '\\' {
			r, size := utf8.DecodeRuneInString(input)
			units = append(units, utf16.Encode([]rune{r})...)
			input = input[size:]
			continue
		}
		switch input[1] {
		case 'u':
			n, err := strconv.ParseUint(input[2:6], 16, 16)
			if err != nil {
				return "", nil, err
			}
			units = append(units, uint16(n))
			input = input[6:]
		default:
			chars := map[byte]uint16{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}
			units = append(units, chars[input[1]])
			input = input[2:]
		}
	}
	return string(utf16.Decode(units)), units, nil
}
