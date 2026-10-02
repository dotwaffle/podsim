package editormodel

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"strconv"
	"testing"
)

type serviceCheckFixture struct {
	Base  map[string]any `json:"base"`
	Cases []struct {
		Name    string             `json:"name"`
		Changes []serviceCheckEdit `json:"changes"`
		Checks  checkReport        `json:"checks"`
	} `json:"cases"`
}

type serviceCheckEdit struct {
	Path  []string `json:"path"`
	Value any      `json:"value"`
}

func applyServiceCheckEdits(t *testing.T, base map[string]any, edits []serviceCheckEdit) map[string]any {
	t.Helper()
	draft := object(cloneEditValue(base))
	for _, edit := range edits {
		var container any = draft
		for index, key := range edit.Path {
			if record := object(container); record != nil {
				if index == len(edit.Path)-1 {
					record[key] = cloneEditValue(edit.Value)
				} else {
					container = record[key]
				}
			} else {
				row, err := strconv.Atoi(key)
				if err != nil || row < 0 || row >= len(items(container)) {
					t.Fatal("invalid service fixture path", edit.Path)
				}
				if index == len(edit.Path)-1 {
					items(container)[row] = cloneEditValue(edit.Value)
				} else {
					container = items(container)[row]
				}
			}
		}
	}
	return draft
}

func TestServiceChecksReferenceParity(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/service_checks.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture serviceCheckFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, item := range fixture.Cases {
		t.Run(item.Name, func(t *testing.T) {
			t.Parallel()
			draft := applyServiceCheckEdits(t, fixture.Base, item.Changes)
			got, err := json.Marshal(draftChecks(draft))
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(item.Checks)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("service checks differ: got %s want %s (%v)", got, want, err)
			}
		})
	}
}
