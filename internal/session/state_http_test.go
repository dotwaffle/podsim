package session

import (
	"encoding/json/jsontext"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/sim"
)

// The state endpoint of every project kind replies only to a request that
// accepts StateMediaType. The media types of earlier servers get HTTP 406.
func TestStateHTTPNegotiation(t *testing.T) {
	t.Parallel()
	plain, _ := streamFixture(t)
	t.Cleanup(plain.Close)
	data := couplingPhaseFixtures(t)
	coupling, _, _ := couplingStreamFixture(t, data.Frames[0], "")
	for _, test := range []struct {
		name    string
		shared  *Session
		markers contractMarkers
	}{
		{"plain", plain, contractMarkers{}},
		{"express", expressSession(t), contractMarkers{order: sim.ExpressOrderContract}},
		{"coupling", coupling, contractMarkers{coupling: sim.CompactPairV1CouplingContract}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := test.shared.Handler("missing")
			for _, accept := range [][]string{
				nil, {"application/json"}, {"*/*"},
				{"application/vnd.podsim.express-v1+json"}, {"application/vnd.podsim.compact-pair-v1+json"},
				{"application/json, " + StateMediaType}, {"application/json", StateMediaType},
			} {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody)
				for _, value := range accept {
					request.Header.Add("Accept", value)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				accepted := len(accept) > 0 && strings.HasSuffix(accept[len(accept)-1], StateMediaType)
				if !accepted {
					if response.Code != http.StatusNotAcceptable {
						t.Fatalf("Accept %q: status %d, want 406", accept, response.Code)
					}
					continue
				}
				if response.Code != http.StatusOK || response.Header().Get("Content-Type") != StateMediaType || !slices.Contains(response.Header().Values("Vary"), "Accept") {
					t.Fatalf("Accept %q: status %d, Content-Type %q, Vary %q", accept, response.Code, response.Header().Get("Content-Type"), response.Header().Get("Vary"))
				}
				raw := response.Body.Bytes()
				assertMarkerSections(t, raw, test.markers)
				if _, err := DecodeStateJSON(raw); err != nil {
					t.Fatalf("Accept %q: %v", accept, err)
				}
			}
		})
	}
}

// TestAcceptsStateMedia parses each media range of the Accept headers. A
// range that names StateMediaType with a quality above zero accepts it.
func TestAcceptsStateMedia(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		accept []string
		want   bool
	}{
		{nil, false},
		{[]string{""}, false},
		{[]string{StateMediaType}, true},
		{[]string{StateMediaType + ";q=1"}, true},
		{[]string{StateMediaType + ";q=1.000"}, true},
		{[]string{StateMediaType + ";q=0.5"}, true},
		{[]string{StateMediaType + ";q=0.001"}, true},
		{[]string{StateMediaType + ";q=0."}, false},
		{[]string{"  " + StateMediaType + " ; q=0.8 "}, true},
		{[]string{"Application/VND.podsim.state-6+JSON"}, true},
		{[]string{StateMediaType + ";charset=utf-8;q=0.9"}, true},
		{[]string{"application/json;q=0.9, " + StateMediaType + ";q=0.1"}, true},
		{[]string{"application/json, text/plain", StateMediaType + ";q=0.7"}, true},
		{[]string{StateMediaType + ";q=0", "application/json"}, false},
		{[]string{StateMediaType + ";q=0"}, false},
		{[]string{StateMediaType + ";q=0.000"}, false},
		{[]string{StateMediaType + ";q=1.001"}, false},
		{[]string{StateMediaType + ";q=2"}, false},
		{[]string{StateMediaType + ";q=-1"}, false},
		{[]string{StateMediaType + ";q=0.5000"}, false},
		{[]string{StateMediaType + ";q=abc"}, false},
		{[]string{StateMediaType + ";q=.5"}, false},
		{[]string{StateMediaType + ";q=1;q=0"}, false},
		{[]string{"*/*"}, false},
		{[]string{"application/*"}, false},
		{[]string{"*/*;q=1, application/*"}, false},
		{[]string{StateMediaType + "x"}, false},
		{[]string{"application/vnd.podsim.state-5+json"}, false},
		{[]string{"application/vnd.podsim.express-v1+json"}, false},
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/state", http.NoBody)
		for _, value := range test.accept {
			request.Header.Add("Accept", value)
		}
		if got := acceptsMedia(request, StateMediaType); got != test.want {
			t.Errorf("Accept %q: got %v, want %v", test.accept, got, test.want)
		}
	}
}

// TestStreamMarkerValues adds root markers to small documents, which every
// order table admits. The marker scan refuses each marker that is not a
// single valid order marker, and every textEncoding marker.
func TestStreamMarkerValues(t *testing.T) {
	t.Parallel()
	plain, plainFrame := streamFixture(t)
	t.Cleanup(plain.Close)
	expressShared := expressSession(t)
	expressFrame, err := expressShared.presentationFrame()
	if err != nil {
		t.Fatal(err)
	}
	documents := map[string][]byte{}
	for name, fixture := range map[string]struct {
		topology TopologySnapshot
		frame    StreamFrame
	}{"plain": {plain.Topology(), plainFrame}, "express": {expressShared.Topology(), expressFrame}} {
		full, err := EncodeStreamJSON(fullStreamEnvelope(fixture.frame))
		if err != nil {
			t.Fatal(err)
		}
		state, err := EncodeStateJSON(fixture.topology, fixture.frame)
		if err != nil {
			t.Fatal(err)
		}
		documents[name+" full"], documents[name+" HTTP"] = full, state
	}
	decoders := map[string]func([]byte) error{
		"full": func(raw []byte) error { _, err := DecodeStreamJSON(raw); return err },
		"HTTP": func(raw []byte) error { _, err := DecodeStateJSON(raw); return err },
	}
	express := `"` + string(sim.ExpressOrderContract) + `"`
	text := `"order-text-base64-v1"`
	for name, raw := range documents {
		kind, format, _ := strings.Cut(name, " ")
		decode := decoders[format]
		if err := decode(raw); err != nil {
			t.Fatalf("%s control: %v", name, err)
		}
		members := splitObject(t, raw)
		var edits map[string][]rootMember
		if kind == "plain" {
			edits = map[string][]rootMember{
				"unknown marker":        {{"orderContract", jsontext.Value(`"express-v2"`)}},
				"null marker":           {{"orderContract", jsontext.Value(`null`)}},
				"empty marker":          {{"orderContract", jsontext.Value(`""`)}},
				"text encoding":         {{"textEncoding", jsontext.Value(text)}},
				"null coupling marker":  {{"couplingContract", jsontext.Value(`null`)}},
				"empty coupling marker": {{"couplingContract", jsontext.Value(`""`)}},
			}
		} else {
			edits = map[string][]rootMember{
				"duplicate marker": {{"orderContract", jsontext.Value(express)}},
				"text encoding":    {{"textEncoding", jsontext.Value(text)}},
			}
		}
		for edit, added := range edits {
			err := decode(joinObject(t, append(slices.Clone(added), members...)))
			if err == nil {
				t.Errorf("%s with %s: accepted", name, edit)
				continue
			}
			if edit == "text encoding" && kind == "express" && !strings.Contains(err.Error(), "text encoding marker") {
				t.Errorf("%s with %s: got %v, want the marker scan", name, edit, err)
			}
		}
	}
}

// TestStateEnvelopeMarkersMatchTopology adds an Express root marker to the
// HTTP state of a plain project, so that the root markers differ from the
// markers of the topology. The decoder refuses the state. The marker scan
// refuses the reverse case, an Express topology under a plain root.
func TestStateEnvelopeMarkersMatchTopology(t *testing.T) {
	t.Parallel()
	plain, plainFrame := streamFixture(t)
	t.Cleanup(plain.Close)
	plainState, err := EncodeStateJSON(plain.Topology(), plainFrame)
	if err != nil {
		t.Fatal(err)
	}
	express := jsontext.Value(`"` + string(sim.ExpressOrderContract) + `"`)
	raw := joinObject(t, append([]rootMember{{"orderContract", express}}, splitObject(t, plainState)...))
	if _, err := DecodeStateJSON(raw); err == nil || !strings.Contains(err.Error(), "contracts disagree") {
		t.Errorf("got %v, want the contracts to disagree", err)
	}
}
