package parkride

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"

	"github.com/dotwaffle/podsim/internal/project"
)

// CheckFoundationProject rejects the Express and coupling markers before decoding.
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
		if kind == jsontext.KindBeginObject && length%2 == 1 && token.Kind() == jsontext.KindString {
			switch token.String() {
			case "orderContract":
				return errors.New("car runs do not support orderContract")
			case "couplingContract":
				return errors.New("car runs do not support couplingContract")
			}
		}
	}
}

// errFoundationProject refuses a car run origin with a contract marker.
var errFoundationProject = errors.New("car runs require a foundation project: version 1 without orderContract or couplingContract")

// foundationProject reports whether config has the features of a car run
// origin: the current version without the Express or coupling marker.
func foundationProject(config project.Config) bool {
	return config.Version == project.CurrentVersion && config.OrderContract == "" && !project.HasCouplingContract(config)
}

func validateFoundationConfig(config project.Config) error {
	if !foundationProject(config) {
		return errFoundationProject
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return CheckFoundationProject(data)
}
