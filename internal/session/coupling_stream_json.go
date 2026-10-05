package session

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/sim"
)

func decodeCouplingStreamJSON(data []byte) (StreamEnvelope, error) {
	packed, scanErr := scanCouplingOrderContract(data)
	if scanErr != nil {
		return StreamEnvelope{}, scanErr
	}
	if err := scanCouplingStreamJSON(data); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanContractMarkers(data, packed, packed); err != nil {
		return StreamEnvelope{}, err
	}
	orderVersion := CouplingStreamVersion
	if packed {
		orderVersion = ExpressStreamVersion
		if err := scanPackedOrders(data); err != nil {
			return StreamEnvelope{}, err
		}
	}
	if err := scanStreamBoardingMembers(data, orderVersion); err != nil {
		return StreamEnvelope{}, err
	}
	if err := scanStreamServiceMembersContract(data, orderVersion, true); err != nil {
		return StreamEnvelope{}, err
	}
	var envelope StreamEnvelope
	var err error
	if packed {
		err = jsonv2.Unmarshal(data, &envelope, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false), jsonv2.RejectUnknownMembers(true), packedDecodeOptions())
	} else {
		err = decodeStreamJSON(data, &envelope)
	}
	return envelope, err
}

// scanCouplingOrderContract reads the order marker of a coupling document
// and bounds the document with the limits of that marker. The packed limits
// contain the unpacked limits, so the first scan bounds the header decode
// without rejecting a document that the exact scan accepts.
func scanCouplingOrderContract(data []byte) (bool, error) {
	if err := prescanJSON(data, couplingStreamLimits(true)); err != nil {
		return false, err
	}
	var header struct {
		OrderContract sim.OrderContract `json:"orderContract"`
	}
	if err := jsonv2.Unmarshal(data, &header, json.DefaultOptionsV1(), jsonv2.MatchCaseInsensitiveNames(false)); err != nil {
		return false, err
	}
	packed := header.OrderContract == sim.ExpressOrderContract
	if !packed {
		if err := prescanJSON(data, couplingStreamLimits(false)); err != nil {
			return false, err
		}
	}
	return packed, nil
}

// couplingStreamLimits bound hello 5 documents and the coupling HTTP
// state. packed reports the Express marker.
func couplingStreamLimits(packed bool) jsonLimits {
	markers := contractMarkers{coupling: sim.CompactPairV1CouplingContract}
	if packed {
		markers.order = sim.ExpressOrderContract
	}
	return streamLimits(markers)
}

func scanCouplingStreamJSON(data []byte) error {
	return scanCouplingPublicJSON(data, false)
}

func scanCouplingPublicJSON(data []byte, httpState bool) error {
	d := jsontext.NewDecoder(bytes.NewReader(data))
	seen := map[string]bool{}
	full := false
	framePrefix := "/full"
	if httpState {
		framePrefix = "/frame"
	}
	for {
		token, err := d.ReadToken()
		if errors.Is(err, io.EOF) {
			if !seen["/couplingContract"] || full && !seen[framePrefix+"/state/simulation/couplingContract"] ||
				httpState && (!full || !seen["/topology"]) {
				return errors.New("stream coupling contract marker is missing")
			}
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
		if path == framePrefix {
			full = true
		}
		if httpState && path == "/topology" {
			if seen[path] {
				return errors.New("duplicate HTTP topology")
			}
			seen[path] = true
			value, err := d.ReadValue()
			if err != nil {
				return err
			}
			if len(value) > project.MaxFileBytes+4096 {
				return errors.New("HTTP topology exceeds supported limit")
			}
			if _, err := scanCouplingJSON(value, true); err != nil {
				return err
			}
			continue
		}
		if path == "/delta/groups/coupling" {
			if seen[path] {
				return errors.New("duplicate coupling replacement")
			}
			seen[path] = true
			if err := scanCouplingReplacementObject(d); err != nil {
				return err
			}
			continue
		}
		if token.String() == "couplingID" {
			if !couplingCabinIDPath(path, framePrefix) || seen[path] {
				return errors.New("unexpected or duplicate cabin coupling ID")
			}
			seen[path] = true
			value, err := d.ReadToken()
			if err != nil {
				return err
			}
			if value.Kind() != jsontext.KindString || value.String() == "" || len(value.String()) > 64 {
				return errors.New("cabin coupling ID needs bounded nonempty text")
			}
			continue
		}
		if !couplingMember(token.String()) {
			continue
		}
		name := token.String()
		valid := path == "/couplingContract" || path == framePrefix+"/state/simulation/"+name &&
			(name == "couplingContract" || name == "couplingEnabled" || name == "couplingGroups")
		if !valid || seen[path] {
			return errors.New("unexpected or duplicate stream coupling member")
		}
		seen[path] = true
		if err := scanCouplingStreamValue(d, name); err != nil {
			return err
		}
	}
}

func couplingCabinIDPath(path, framePrefix string) bool {
	for _, scope := range []struct{ prefix, suffix string }{
		{framePrefix + "/state/simulation/vehicles/", "couplingID"},
		{"/delta/vehicles/", "metadata/value/couplingID"},
	} {
		index, tail, ok := strings.Cut(strings.TrimPrefix(path, scope.prefix), "/")
		if !strings.HasPrefix(path, scope.prefix) || !ok || tail != scope.suffix {
			continue
		}
		if n, err := strconv.Atoi(index); err == nil && n >= 0 && n < project.MaxPods {
			return true
		}
	}
	return false
}

func scanCouplingStreamValue(d *jsontext.Decoder, name string) error {
	switch name {
	case "couplingGroups":
		return scanCouplingViewArray(d)
	case "couplingContract":
		token, err := d.ReadToken()
		if err != nil {
			return err
		}
		if token.Kind() != jsontext.KindString || token.String() != string(sim.CompactPairV1CouplingContract) {
			return sim.ErrUnknownCouplingContract
		}
	case "couplingEnabled":
		token, err := d.ReadToken()
		if err != nil {
			return err
		}
		if token.Kind() != jsontext.KindTrue && token.Kind() != jsontext.KindFalse {
			return errors.New("coupling enabled must be Boolean")
		}
	}
	return nil
}

func scanCouplingReplacement(raw []byte) error {
	if err := prescanJSON(raw, couplingStreamLimits(false)); err != nil {
		return err
	}
	d := jsontext.NewDecoder(bytes.NewReader(raw))
	if err := scanCouplingReplacementObject(d); err != nil {
		return err
	}
	if _, err := d.ReadToken(); !errors.Is(err, io.EOF) {
		return errors.New("coupling replacement contains multiple values")
	}
	return nil
}

func scanCouplingReplacementObject(d *jsontext.Decoder) error {
	start, startErr := d.ReadToken()
	if startErr != nil {
		return startErr
	}
	if start.Kind() != jsontext.KindBeginObject {
		return errors.New("coupling replacement must be an object")
	}
	seen := map[string]bool{}
	for d.PeekKind() != jsontext.KindEndObject {
		token, err := d.ReadToken()
		if err != nil {
			return err
		}
		name := token.String()
		if token.Kind() != jsontext.KindString || seen[name] || name != "couplingContract" && name != "couplingEnabled" && name != "couplingGroups" {
			return errors.New("unexpected or duplicate coupling replacement member")
		}
		seen[name] = true
		if valueErr := scanCouplingStreamValue(d, name); valueErr != nil {
			return valueErr
		}
	}
	if len(seen) != 3 {
		return errors.New("coupling replacement has missing members")
	}
	_, err := d.ReadToken()
	return err
}

func scanCouplingViewArray(d *jsontext.Decoder) error {
	return scanCouplingFixedArray(d, project.MaxPods/2, false, scanCouplingViewRecord)
}

func scanCouplingFixedArray(d *jsontext.Decoder, limit int, exact bool, item func(*jsontext.Decoder) error) error {
	start, startErr := d.ReadToken()
	if startErr != nil {
		return startErr
	}
	if start.Kind() != jsontext.KindBeginArray {
		return errors.New("coupling geometry must be an array")
	}
	count := 0
	for d.PeekKind() != jsontext.KindEndArray {
		if count == limit {
			return errJSONArrayTooLong
		}
		if err := item(d); err != nil {
			return err
		}
		count++
	}
	if exact && count != limit {
		return errors.New("coupling geometry array has an invalid length")
	}
	_, err := d.ReadToken()
	return err
}

func scanCouplingViewRecord(d *jsontext.Decoder) error {
	required := append(couplingRecordFields("group"), "profile", "ownerID", "resourceClaims", "bodies")
	return scanCouplingViewObject(d, required, []string{"commonSpeed", "connector", "maneuverEnvelope"}, func(d *jsontext.Decoder, field string) error {
		switch field {
		case "bodies":
			return scanCouplingFixedArray(d, 2, true, scanCouplingRectangle)
		case "connector", "maneuverEnvelope":
			return scanCouplingRectangle(d)
		case "commonSpeed":
			return scanCouplingNumber(d, false)
		case "resourceClaims":
			return scanCouplingNumber(d, true)
		default:
			return scanCouplingValue(d, field)
		}
	})
}

func scanCouplingRectangle(d *jsontext.Decoder) error {
	return scanCouplingViewObject(d, []string{"corners"}, nil, func(d *jsontext.Decoder, _ string) error {
		return scanCouplingFixedArray(d, 4, true, func(d *jsontext.Decoder) error {
			return scanCouplingViewObject(d, []string{"x", "y"}, nil, func(d *jsontext.Decoder, _ string) error {
				return scanCouplingNumber(d, false)
			})
		})
	})
}

func scanCouplingViewObject(d *jsontext.Decoder, required, optional []string, value func(*jsontext.Decoder, string) error) error {
	start, startErr := d.ReadToken()
	if startErr != nil {
		return startErr
	}
	if start.Kind() != jsontext.KindBeginObject {
		return errors.New("coupling view record must be an object")
	}
	seen := make(map[string]bool, len(required)+len(optional))
	for d.PeekKind() != jsontext.KindEndObject {
		token, err := d.ReadToken()
		if err != nil {
			return err
		}
		name := token.String()
		allowed := false
		for _, fields := range [][]string{required, optional} {
			for _, field := range fields {
				allowed = allowed || field == name
			}
		}
		if token.Kind() != jsontext.KindString || !allowed || seen[name] {
			return errors.New("unexpected or duplicate coupling view member")
		}
		seen[name] = true
		if valueErr := value(d, name); valueErr != nil {
			return valueErr
		}
	}
	for _, field := range required {
		if !seen[field] {
			return errors.New("coupling view record has missing members")
		}
	}
	_, err := d.ReadToken()
	return err
}

func scanCouplingNumber(d *jsontext.Decoder, integer bool) error {
	token, err := d.ReadToken()
	if err != nil {
		return err
	}
	if token.Kind() != jsontext.KindNumber {
		return errors.New("coupling view scalar must be a number")
	}
	if integer {
		_, err = strconv.Atoi(token.String())
	} else {
		_, err = strconv.ParseFloat(token.String(), 64)
	}
	return err
}
