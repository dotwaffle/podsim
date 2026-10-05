package session

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/sim"
)

// Express wire markers select the packed order text contract.
const (
	ExpressTextEncoding = "order-text-base64-v1"
	ExpressMediaType    = "application/vnd.podsim.express-v1+json"
	expressStateVersion = 7
)

func packOrderText(text string, limit int) (string, error) {
	if len(text) > limit || !utf8.ValidString(text) {
		return "", errors.New("invalid order text bytes")
	}
	return base64.StdEncoding.EncodeToString([]byte(text)), nil
}

func unpackOrderText(raw []byte, limit int) (string, error) {
	// Check the literal token before allocating the decoded bytes.
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' || len(raw)-2 > base64.StdEncoding.EncodedLen(limit) {
		return "", errors.New("invalid packed order text size")
	}
	packed := raw[1 : len(raw)-1]
	for _, c := range packed {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '+' && c != '/' && c != '=' {
			return "", errors.New("packed order text must use literal base64")
		}
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(string(packed))
	if err != nil || len(decoded) > limit || !utf8.Valid(decoded) || base64.StdEncoding.EncodeToString(decoded) != string(packed) {
		return "", errors.New("invalid canonical order text")
	}
	return string(decoded), nil
}

func transformOrderText(fields []*string, decode bool) error {
	for i, field := range fields {
		limit := 64
		if i == 3 {
			limit = 1024
		}
		var next string
		var err error
		if decode {
			next, err = unpackOrderText([]byte(`"`+*field+`"`), limit)
		} else {
			next, err = packOrderText(*field, limit)
		}
		if err != nil {
			return err
		}
		*field = next
	}
	return nil
}

type packedRequest sim.Request
type packedSavedRequest sim.SavedRequest

func encodePackedRequest(e *jsontext.Encoder, r sim.Request) error {
	if err := transformOrderText([]*string{&r.From, &r.To, &r.PodID, &r.DispatchReason, &r.ServiceID}, false); err != nil {
		return err
	}
	return jsonv2.MarshalEncode(e, packedRequest(r))
}
func encodePackedSavedRequest(e *jsontext.Encoder, r sim.SavedRequest) error {
	if err := transformOrderText([]*string{&r.From, &r.To, &r.PodID, &r.DispatchReason, &r.ServiceID}, false); err != nil {
		return err
	}
	return jsonv2.MarshalEncode(e, packedSavedRequest(r))
}
func decodePackedRequest(d *jsontext.Decoder, r *sim.Request) error {
	var wire packedRequest
	if err := jsonv2.UnmarshalDecode(d, &wire, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true), jsontext.AllowDuplicateNames(false)); err != nil {
		return err
	}
	next := sim.Request(wire)
	if err := transformOrderText([]*string{&next.From, &next.To, &next.PodID, &next.DispatchReason, &next.ServiceID}, true); err != nil {
		return err
	}
	*r = next
	return nil
}
func decodePackedSavedRequest(d *jsontext.Decoder, r *sim.SavedRequest) error {
	var wire packedSavedRequest
	if err := jsonv2.UnmarshalDecode(d, &wire, jsonv2.RejectUnknownMembers(true)); err != nil {
		return err
	}
	next := sim.SavedRequest(wire)
	if err := transformOrderText([]*string{&next.From, &next.To, &next.PodID, &next.DispatchReason, &next.ServiceID}, true); err != nil {
		return err
	}
	*r = next
	return nil
}

func packedRequestOptions() jsonv2.Options {
	return jsonv2.WithMarshalers(jsonv2.MarshalToFunc(encodePackedRequest))
}
func packedDecodeOptions() jsonv2.Options {
	return jsonv2.JoinOptions(jsonv2.WithUnmarshalers(jsonv2.UnmarshalFromFunc(decodePackedRequest)), jsontext.AllowDuplicateNames(false))
}

// scanPackedOrders rejects noncanonical text before typed order allocation.
func scanPackedOrders(data []byte) error {
	d := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := d.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		kind, n := d.StackIndex(d.StackDepth())
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || n%2 != 1 {
			continue
		}
		path := string(d.StackPointer())
		order := strings.Contains(path, "/riders/") || strings.Contains(path, "/pending/") || strings.Contains(path, "/waiting/") && strings.Contains(path, "/request/")
		if !order {
			continue
		}
		limit := 64
		switch token.String() {
		case "from", "to", "podID", "serviceID":
		case "dispatchReason":
			limit = 1024
		default:
			continue
		}
		raw, err := d.ReadValue()
		if err != nil {
			return err
		}
		if _, err = unpackOrderText(raw, limit); err != nil {
			return err
		}
	}
}

// scanContractMarkers keeps marker presence distinct from empty and null.
func scanContractMarkers(data []byte, express, requireText bool) error {
	d := jsontext.NewDecoder(bytes.NewReader(data))
	seen := map[string]bool{}
	for {
		token, err := d.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		kind, n := d.StackIndex(d.StackDepth())
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || n%2 != 1 {
			continue
		}
		name := token.String()
		if name != "orderContract" && name != "textEncoding" {
			continue
		}
		path := string(d.StackPointer())
		if seen[path] {
			return errors.New("duplicate contract marker")
		}
		seen[path] = true
		if !express {
			return errors.New("legacy version contains Express marker")
		}
		value, err := d.ReadToken()
		if err != nil {
			return err
		}
		expected := string(sim.ExpressOrderContract)
		if name == "textEncoding" {
			expected = ExpressTextEncoding
		}
		if value.Kind() != jsontext.KindString || value.String() != expected {
			return errors.New("invalid Express contract marker")
		}
	}
	if express && (!seen["/orderContract"] || requireText && !seen["/textEncoding"]) {
		return errors.New("missing Express contract marker")
	}
	return nil
}
