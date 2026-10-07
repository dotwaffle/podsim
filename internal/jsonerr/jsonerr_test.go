package jsonerr

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type inner struct {
	Speed int `json:"speed"`
}

type outer struct {
	Lanes []inner `json:"lanes"`
}

func decodeError(t *testing.T, data string, options ...json.Options) error {
	t.Helper()
	err := json.Unmarshal([]byte(data), new(outer), options...)
	if err == nil {
		t.Fatalf("decode %s: no error", data)
	}
	return err
}

func TestTextSemantic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, data string
		options    []json.Options
		want       string
	}{
		{"type", `{"lanes":[{},{"speed":"x"}]}`, nil, `json: invalid JSON string at /lanes/1/speed: expected Go int`},
		{"unknown", `{"lanes":[{"bogus":1}]}`, []json.Options{json.RejectUnknownMembers(true)}, `json: invalid JSON object at /lanes/0/bogus: unknown object member name "bogus"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := decodeError(t, test.data, test.options...)
			wrapped := fmt.Errorf("decode project: %w", err)
			got := Text(wrapped)
			if want := "decode project: " + test.want; got != want {
				t.Fatalf("Text = %q, want %q", got, want)
			}
			if strings.Contains(got, "cannot") || strings.Contains(got, "unable to") {
				t.Fatalf("Text %q has the library modal verb", got)
			}
			fixed := Wrap(wrapped)
			if fixed.Error() != got {
				t.Fatalf("Wrap text %q, want %q", fixed.Error(), got)
			}
			semantic, ok := errors.AsType[*json.SemanticError](fixed)
			if !ok || semantic.JSONPointer == "" || !strings.HasPrefix(string(semantic.JSONPointer), "/lanes/") {
				t.Fatalf("AsType on %v: %v %v", fixed, semantic, ok)
			}
			if !errors.Is(fixed, err) {
				t.Fatal("Wrap lost the original error")
			}
		})
	}
}

func TestTextKeepsOtherErrors(t *testing.T) {
	t.Parallel()
	if Text(nil) != "" || Wrap(nil) != nil {
		t.Fatal("nil error changed")
	}
	plain := errors.New("plain")
	if !errors.Is(Wrap(plain), plain) || Wrap(plain).Error() != "plain" {
		t.Fatal("Wrap changed a plain error")
	}
	syntactic := json.Unmarshal([]byte(`{"lanes":`), new(outer))
	if _, ok := errors.AsType[*jsontext.SyntacticError](syntactic); !ok {
		t.Fatalf("want a syntactic error, got %v", syntactic)
	}
	if got := Text(syntactic); got != syntactic.Error() {
		t.Fatalf("Text = %q, want %q", got, syntactic.Error())
	}
}
