package project

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// referenceProjectBanks retains the full-path token scan as an oracle for
// the optimized scan, including malformed shapes and the first refusal.
func referenceProjectBanks(data []byte) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		tokenKind := token.Kind()
		path := strings.Split(string(decoder.StackPointer()), "/")
		if len(path) < 5 || path[1] != "network" || path[2] != "stations" || path[4] != "banks" {
			continue
		}
		if len(path) == 5 {
			kind, length := decoder.StackIndex(decoder.StackDepth())
			if kind == jsontext.KindBeginObject && length%2 == 1 && decoder.PeekKind() != jsontext.KindBeginArray {
				return errors.New("station banks must be a nonempty array")
			}
			if tokenKind == jsontext.KindBeginArray && decoder.PeekKind() == jsontext.KindEndArray {
				return errors.New("station banks must be a nonempty array")
			}
		}
		kind, length := decoder.StackIndex(decoder.StackDepth())
		if kind == jsontext.KindBeginArray {
			limit := int64(0)
			switch {
			case len(path) == 6:
				limit = sim.MaxStationBanks
			case len(path) == 8 && path[6] == "berthIDs":
				limit = MaxBerths
			}
			if limit > 0 && length > limit {
				return fmt.Errorf("bank array has more than %d elements", limit)
			}
		}
	}
}

// referenceProjectFields retains the old path selection. Member checks
// are shared, so this oracle isolates the traversal and refusal order.
func referenceProjectFields(data []byte) (projectFields, error) {
	scan := projectFieldScan{decoder: jsontext.NewDecoder(bytes.NewReader(data))}
	for {
		token, err := scan.decoder.ReadToken()
		if errors.Is(err, io.EOF) {
			return scan.fields, nil
		}
		if err != nil {
			return projectFields{}, err
		}
		path := strings.Split(string(scan.decoder.StackPointer()), "/")
		kind, length := scan.decoder.StackIndex(scan.decoder.StackDepth())
		if kind == jsontext.KindBeginArray && len(path) == 3 && path[1] == "expressServices" && length > MaxExpressServices {
			return projectFields{}, fmt.Errorf("express registry has more than %d services", MaxExpressServices)
		}
		if token.Kind() != jsontext.KindString || kind != jsontext.KindBeginObject || length%2 != 1 {
			continue
		}
		if len(path) == 2 {
			err = scan.topMember(path[1])
		} else {
			err = scan.nestedMember(path)
		}
		if err != nil {
			return projectFields{}, err
		}
	}
}

func checkProjectScans(t *testing.T, data []byte) {
	t.Helper()
	if got, want := scanProjectBanks(data), referenceProjectBanks(data); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("bank scan = %v, want %v; JSON %s", got, want, data)
	}
	got, gotErr := scanProjectFields(data)
	want, wantErr := referenceProjectFields(data)
	if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
		t.Fatalf("field scan = %+v, %v, want %+v, %v; JSON %s", got, gotErr, want, wantErr, data)
	}
}

func TestProjectScansMatchFullPaths(t *testing.T) {
	t.Parallel()
	banks := `[` + strings.Repeat(`{},`, sim.MaxStationBanks) + `{}]`
	berths := `[{"berthIDs":[` + strings.Repeat(`"b",`, MaxBerths) + `"b"]}]`
	registry := `[` + strings.Repeat(`{},`, MaxExpressServices) + `{}]`
	for _, test := range []struct{ name, raw string }{
		{"empty", `{}`},
		{"top-level markers", `{"incidentContract":"incident-v1","orderContract":"express-v1","onboardPickups":true}`},
		{"bank presence", `{"network":{"stations":[{"banks":null}]}}`},
		{"empty banks", `{"network":{"stations":[{"banks":[]}]}}`},
		{"bank bound", `{"network":{"stations":[{"banks":` + banks + `}]}}`},
		{"berth bound", `{"network":{"stations":[{"banks":` + berths + `}]}}`},
		{"registry bound", `{"expressServices":` + registry + `}`},
		{"trailing array after registry", `{"expressServices":[]} [[` + strings.Repeat(`0,`, MaxExpressServices) + `0]]`},
		{"fleet class", `{"fleet":[{"class":"bus"}]}`},
		{"station classes", `{"network":{"stations":[{"vehicleClasses":null}]}}`},
		{"lane classes", `{"network":{"lanes":[{"vehicleClasses":[]}]}}`},
		{"berth classes", `{"network":{"stations":[{"berths":[{"vehicleClasses":["compact","compact"]}]}]}}`},
		{"irrelevant lookalikes", `{"demandProfiles":[{"fleet":[{"class":"bus"}],"network":{"stations":[{"banks":null,"vehicleClasses":[]}]},"expressServices":` + registry + `}]}`},
		{"return to root", `{"network":{"stations":[]},"demandProfiles":[],"fleet":[{"class":"compact"}],"expressServices":[]}`},
		{"wrong case", `{"Network":{"stations":[{"banks":null}]},"Fleet":[{"class":"bus"}]}`},
		{"escaped root", `{"net\u0077ork":{"stations":[{"banks":null}]}}`},
		{"escaped pointer", `{"network/stations":{"stations":[{"banks":null}]},"network":{"stations~1":[{"banks":null}]}}`},
		{"duplicate irrelevant name", `{"demandProfiles":[{"weights":[],"weights":[]}]}`},
		{"bad irrelevant syntax", `{"demandProfiles":[[0,1,]]}`},
		{"first bank refusal", `{"faults":null,"network":{"stations":[{"banks":null}]}}`},
		{"first field refusal", `{"faults":null,"fleet":[{"class":"bus"}]}`},
		{"field before syntax", `{"fleet":[{"class":"bus"}],"demandProfiles":[[0,]]}`},
		{"bank before syntax", `{"network":{"stations":[{"banks":null}]},"demandProfiles":[[0,]]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			checkProjectScans(t, []byte(test.raw))
		})
	}
}

func FuzzProjectScansMatchFullPaths(f *testing.F) {
	for _, raw := range []string{
		`{}`, `null`, `[]`, `{"network":{"stations":[{"banks":[]}]}}`,
		`{"network":{"stations":[{"banks":[{"berthIDs":["b"]}]}]}}`,
		`{"fleet":[{"class":"group"}],"demandProfiles":[[0,1,2]]}`,
		`{"expressServices":[{}],"network":{"lanes":[{"vehicleClasses":["express"]}]}}`,
		`{"expressServices":[]} [[` + strings.Repeat(`0,`, MaxExpressServices) + `0]]`,
	} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, data []byte) { checkProjectScans(t, data) })
}

func BenchmarkProjectScansDemand(b *testing.B) {
	data := []byte(`{"demandProfiles":[{"flows":[` + strings.Repeat(`[0,1,0.123456,1,2,3,4,5],`, 9999) + `[0,1,0.123456,1,2,3,4,5]]}],"network":{"stations":[]}}`)
	for _, scan := range []struct {
		name string
		run  func([]byte) error
	}{
		{"banks", scanProjectBanks},
		{"banks reference", referenceProjectBanks},
		{"fields", func(data []byte) error { _, err := scanProjectFields(data); return err }},
		{"fields reference", func(data []byte) error { _, err := referenceProjectFields(data); return err }},
	} {
		b.Run(scan.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := scan.run(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
