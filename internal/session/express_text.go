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

func packOrderText(text string, limit int) (string, error) {
	if len(text) > limit || !utf8.ValidString(text) {
		return "", errors.New("invalid order text bytes")
	}
	return base64.StdEncoding.EncodeToString([]byte(text)), nil
}

// The limits of packed order text. A dispatch reason is free text, and
// each other packed field is an ID.
const (
	orderIDBytes   = 64
	orderTextBytes = 1024
)

// unpackOrderText decodes the packed JSON string raw. When text is true,
// raw holds free text of at most orderTextBytes bytes without a control
// character. Otherwise raw holds an ID of at most orderIDBytes bytes of
// sim.IDCharacters, or no ID.
func unpackOrderText(raw []byte, text bool) (string, error) {
	limit := orderIDBytes
	if text {
		limit = orderTextBytes
	}
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
	switch {
	case text && !sim.ValidText(string(decoded)):
		return "", errors.New("order text has a control character")
	case !text && !sim.ValidIDText(string(decoded)):
		return "", errors.New("order ID has a character other than A-Z, a-z, 0-9, '.', '+' or '-'")
	}
	return string(decoded), nil
}

func transformOrderText(fields []*string, decode bool) error {
	for i, field := range fields {
		// The fourth field is the dispatch reason.
		text, limit := i == 3, orderIDBytes
		if text {
			limit = orderTextBytes
		}
		var next string
		var err error
		if decode {
			next, err = unpackOrderText([]byte(`"`+*field+`"`), text)
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
	if err := transformOrderText([]*string{&r.From, &r.To, &r.PodID, &r.DispatchReason, &r.ServiceID, &r.LegFrom}, false); err != nil {
		return err
	}
	return jsonv2.MarshalEncode(e, packedRequest(r))
}
func decodePackedRequest(d *jsontext.Decoder, r *sim.Request) error {
	// Read the whole order first. An error in the middle of an order that
	// the decoder of the enclosing document reads makes that decoder report
	// its own state in place of the cause, such as the unknown member.
	raw, err := d.ReadValue()
	if err != nil {
		return err
	}
	var wire packedRequest
	if err := jsonv2.Unmarshal(raw, &wire, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true), jsontext.AllowDuplicateNames(false)); err != nil {
		return err
	}
	next := sim.Request(wire)
	if err := transformOrderText([]*string{&next.From, &next.To, &next.PodID, &next.DispatchReason, &next.ServiceID, &next.LegFrom}, true); err != nil {
		return err
	}
	*r = next
	return nil
}
func packedRequestOptions() jsonv2.Options {
	return jsonv2.WithMarshalers(jsonv2.MarshalToFunc(encodePackedRequest))
}

// packedDecodeOptions decode the packed order text of a stream document,
// its faults with decodeFaultView, and its emergencies with
// decodeEmergencyView.
func packedDecodeOptions() jsonv2.Options {
	return jsonv2.JoinOptions(jsonv2.WithUnmarshalers(jsonv2.JoinUnmarshalers(
		jsonv2.UnmarshalFromFunc(decodePackedRequest), jsonv2.UnmarshalFromFunc(decodeFaultView),
		jsonv2.UnmarshalFromFunc(decodeEmergencyView))), jsontext.AllowDuplicateNames(false))
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
		text := false
		switch token.String() {
		case "from", "to", "podID", "serviceID":
		case "legFrom":
			// A save writes the leg origin as a station index.
			if strings.HasPrefix(path, "/simulation/") {
				continue
			}
		case "dispatchReason":
			text = true
		default:
			continue
		}
		raw, err := d.ReadValue()
		if err != nil {
			return err
		}
		if _, err = unpackOrderText(raw, text); err != nil {
			return err
		}
	}
}

// scanContractMarkers keeps marker presence distinct from empty and null.
// express reports the Express marker of the document. Each orderContract
// member must then be the Express marker, and the root must have one.
// Without it, each orderContract member is refused. No document has a
// textEncoding member: the order text is always packed. Each
// incidentContract member must be the incident marker, each faultContract
// member the fault marker, and each emergencyContract member the emergency
// marker, so that an explicit null or empty marker is not read as no
// marker. The typed decode decides where the member can be.
func scanContractMarkers(data []byte, express bool) error {
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
		if name == "incidentContract" || name == "faultContract" || name == "emergencyContract" {
			if markerErr := scanFeatureMarker(d, name); markerErr != nil {
				return markerErr
			}
			continue
		}
		if name != "orderContract" && name != "textEncoding" {
			continue
		}
		path := string(d.StackPointer())
		if seen[path] {
			return errors.New("duplicate contract marker")
		}
		seen[path] = true
		if name == "textEncoding" {
			return errors.New("document contains a text encoding marker")
		}
		if !express {
			return errors.New("document without the root Express marker contains an order marker")
		}
		value, err := d.ReadToken()
		if err != nil {
			return err
		}
		if value.Kind() != jsontext.KindString || value.String() != string(sim.ExpressOrderContract) {
			return errors.New("invalid Express contract marker")
		}
	}
	if express && !seen["/orderContract"] {
		return errors.New("missing Express contract marker")
	}
	return nil
}

// scanFeatureMarker reads the value of the incidentContract, the
// faultContract, or the emergencyContract member name. It refuses each
// value other than the marker of that name.
func scanFeatureMarker(d *jsontext.Decoder, name string) error {
	marker, unknown := string(sim.IncidentV1Contract), sim.ErrUnknownIncidentContract
	switch name {
	case "faultContract":
		marker, unknown = string(sim.FaultV1Contract), sim.ErrUnknownFaultContract
	case "emergencyContract":
		marker, unknown = string(sim.EmergencyV1Contract), sim.ErrUnknownEmergencyContract
	}
	value, err := d.ReadToken()
	if err != nil {
		return err
	}
	if value.Kind() != jsontext.KindString || value.String() != marker {
		return unknown
	}
	return nil
}
