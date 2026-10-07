// Package jsonerr gives json/v2 semantic errors fixed wording.
//
// The json/v2 package chooses "cannot" or "unable to" at random once per
// process when it formats a [json.SemanticError]. Text that a user or a
// log reader sees must not change from one run to the next, so this
// package builds the text from the fields of the error.
package jsonerr

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"strconv"
	"strings"
)

// Wrap returns err with fixed wording for its json/v2 semantic error.
// The result unwraps to err, so errors.Is and errors.As still match.
// Wrap returns err when it holds no semantic error, and nil for nil.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	text := Text(err)
	if text == err.Error() {
		return err
	}
	return &fixedError{text: text, err: err}
}

type fixedError struct {
	text string
	err  error
}

func (e *fixedError) Error() string { return e.text }

func (e *fixedError) Unwrap() error { return e.err }

// Text returns the text of err with the library wording of the first
// semantic error in the chain replaced. The fixed form is
//
//	json: invalid JSON number at /network/lanes/3/speedLimit: expected Go int
//
// The text of every other error stays as it is.
func Text(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	semantic, ok := errors.AsType[*json.SemanticError](err)
	if !ok {
		return text
	}
	return strings.Replace(text, semantic.Error(), semanticText(semantic), 1)
}

func semanticText(e *json.SemanticError) string {
	var sb strings.Builder
	sb.WriteString("json: invalid ")
	if errors.Is(e.Err, json.ErrUnknownName) {
		sb.WriteString("JSON object")
	} else {
		sb.WriteString(kindName(e.JSONKind))
	}
	if e.JSONPointer != "" {
		sb.WriteString(" at ")
		sb.WriteString(string(e.JSONPointer))
	}
	switch {
	case errors.Is(e.Err, json.ErrUnknownName):
		sb.WriteString(": ")
		sb.WriteString(json.ErrUnknownName.Error())
		sb.WriteByte(' ')
		sb.WriteString(strconv.Quote(e.JSONPointer.LastToken()))
		return sb.String()
	case e.GoType != nil:
		sb.WriteString(": expected Go ")
		sb.WriteString(e.GoType.String())
	}
	if e.Err != nil {
		sb.WriteString(": ")
		sb.WriteString(Text(e.Err))
	}
	return sb.String()
}

func kindName(kind jsontext.Kind) string {
	names := map[jsontext.Kind]string{
		jsontext.KindNull:        "JSON null",
		jsontext.KindFalse:       "JSON boolean",
		jsontext.KindTrue:        "JSON boolean",
		jsontext.KindString:      "JSON string",
		jsontext.KindNumber:      "JSON number",
		jsontext.KindBeginObject: "JSON object",
		jsontext.KindEndObject:   "JSON object",
		jsontext.KindBeginArray:  "JSON array",
		jsontext.KindEndArray:    "JSON array",
	}
	if name, ok := names[kind]; ok {
		return name
	}
	return "JSON value"
}
