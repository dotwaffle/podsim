package parkride

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"

	"github.com/dotwaffle/podsim/internal/project"
)

// CheckFoundationProject rejects later order contract presence before decoding.
// The caller must still use the authoritative project decoder and validation.
func CheckFoundationProject(data []byte) error {
	if len(data) > project.MaxFileBytes {
		return errors.New("project exceeds 10 MiB")
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if kind == jsontext.KindBeginObject && length%2 == 1 && token.Kind() == jsontext.KindString && token.String() == "orderContract" {
			return errors.New("car runs do not support orderContract")
		}
	}
}
func validateFoundationConfig(config project.Config) error {
	if config.Version < 1 || config.Version > 3 {
		return errors.New("car runs require a foundation project version 1 through 3")
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return CheckFoundationProject(data)
}
