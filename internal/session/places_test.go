package session

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/project"
)

const placeFixtureJSON = `[{"display_name":"London","lat":"51.5","lon":"-0.12","boundingbox":["51.4","51.6","-0.3","0.1"],"license":"ignored provider metadata"}]`

func placeFixture(t *testing.T, handler http.HandlerFunc) (*placeSearch, *atomic.Int64) {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	search, err := newPlaceSearch(upstream.URL + "/search")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(search.client.CloseIdleConnections)
	clock := new(atomic.Int64)
	clock.Store(time.Now().UnixNano())
	search.now = func() time.Time { return time.Unix(0, clock.Load()) }
	return search, clock
}

func TestPlaceConfiguration(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"file:///search", "ftp://host/search", "https://user:secret@host/search", "http://:80/search", "http://host:99999/search", "http://host/search?key=secret", "http://host/search?", "http://host/search#", "http://host/search#fragment"} {
		if _, err := WithGeocodingURL(value); err == nil {
			t.Fatalf("accepted invalid provider %q", value)
		}
	}
	option, err := WithGeocodingURL("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{}
	option(s)
	if s.places != nil {
		t.Fatal("empty provider did not disable search")
	}
}

func TestPlaceCacheOwnershipLimitsAndExpiry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	search, clock := placeFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/search" || r.URL.Query().Get("format") != "jsonv2" || r.URL.Query().Get("limit") != "5" || r.URL.Query().Get("accept-language") != "en" || !strings.HasPrefix(r.UserAgent(), "Podsim/") || r.Header.Get("Authorization") != "" {
			t.Error("provider request contract changed")
		}
		_, _ = io.WriteString(w, placeFixtureJSON)
	})
	first, hit, _, err := search.lookup(t.Context(), "London")
	if err != nil || hit || len(first) != 1 || first[0].Bounds == nil {
		t.Fatalf("first search: %+v, %t, %v", first, hit, err)
	}
	first[0].Name, first[0].Bounds.South = "changed", 0
	cached, hit, _, err := search.lookup(t.Context(), "London")
	if err != nil || !hit || cached[0].Name != "London" || cached[0].Bounds.South != 51.4 || calls.Load() != 1 {
		t.Fatal("caller changed cache data or caused another upstream request")
	}
	if _, _, retry, err := search.lookup(t.Context(), "Other"); !errors.Is(err, errPlaceBusy) || retry != 1 || calls.Load() != 1 {
		t.Fatal("one-second start limit failed")
	}
	for index := range placeCacheSize {
		clock.Add(int64(time.Second))
		if _, _, _, err := search.lookup(t.Context(), strconv.Itoa(index)); err != nil {
			t.Fatal(err)
		}
	}
	if len(search.cache) != placeCacheSize || len(search.order) != placeCacheSize {
		t.Fatal("cache count is not bounded")
	}
	if _, present := search.cache["London"]; present {
		t.Fatal("cache did not evict its oldest entry")
	}
	clock.Add(int64(placeCacheTTL))
	if _, hit, _, err := search.lookup(t.Context(), "127"); err != nil || hit {
		t.Fatal("expired cache entry was reused")
	}
	if len(search.cache) != 1 {
		t.Fatal("expired entries remain in the cache")
	}
}

func TestPlaceBusyAndCancellationHoldSlot(t *testing.T) {
	t.Parallel()
	search, clock := placeFixture(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "[]") })
	entered, release := make(chan struct{}), make(chan struct{})
	search.client.Transport = placeRoundTrip(func(_ *http.Request) (*http.Response, error) {
		close(entered)
		<-release // Deliberately ignore cancellation until this network operation ends.
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("[]")), Header: make(http.Header)}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); _, _, _, _ = search.lookup(ctx, "First") }()
	<-entered
	cancel()
	clock.Add(int64(2 * time.Second))
	for range 8 {
		if _, _, _, err := search.lookup(t.Context(), "Second"); !errors.Is(err, errPlaceBusy) {
			t.Error("cancellation released an unfinished upstream slot")
		}
	}
	close(release)
	<-done
	if search.busy {
		t.Fatal("completed network operation retained its slot")
	}
}

func TestPlaceConnectionFailureDoesNotRetry(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	search, clock := placeFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 2 {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("test server cannot hijack HTTP/1 connection")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		_, _ = io.WriteString(w, "[]")
	})
	if _, _, _, err := search.lookup(t.Context(), "First"); err != nil {
		t.Fatal(err)
	}
	clock.Add(int64(time.Second))
	if _, _, _, err := search.lookup(t.Context(), "Second"); !errors.Is(err, errPlaceUpstream) {
		t.Fatalf("connection failure: got %v, want upstream failure", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("two explicit searches caused %d upstream attempts", calls.Load())
	}
	if _, _, _, err := search.lookup(t.Context(), "Third"); !errors.Is(err, errPlaceBusy) || calls.Load() != 2 {
		t.Fatal("connection failure bypassed the start limiter")
	}
}

type placeRoundTrip func(*http.Request) (*http.Response, error)

func (f placeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPlaceUpstreamFailuresAndTimeouts(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name    string
		status  int
		body    string
		gzip    bool
		timeout bool
		want    error
	}{
		{"status", 500, "sensitive upstream body", false, false, errPlaceUpstream},
		{"redirect", 302, "", false, false, errPlaceUpstream},
		{"malformed", 200, "[", false, false, errPlaceUpstream},
		{"null", 200, "null", false, false, errPlaceUpstream},
		{"oversized", 200, strings.Repeat(" ", placeBodyBytes+1), false, false, errPlaceUpstream},
		{"gzip oversized", 200, strings.Repeat(" ", placeBodyBytes+1), true, false, errPlaceUpstream},
		{"timeout", 200, "", false, true, errPlaceTimeout},
		{"empty result", 200, "[]", false, false, nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			search, _ := placeFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if row.timeout {
					<-r.Context().Done()
					return
				}
				if row.status == 302 {
					w.Header().Set("Location", "/redirected")
				}
				if row.gzip {
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.WriteHeader(row.status)
				if row.gzip {
					writer := gzip.NewWriter(w)
					_, _ = io.WriteString(writer, row.body)
					_ = writer.Close()
				} else {
					_, _ = io.WriteString(w, row.body)
				}
			})
			if row.timeout {
				search.timeout = 50 * time.Millisecond
			}
			results, hit, _, err := search.lookup(t.Context(), "Probe")
			if !errors.Is(err, row.want) || hit || calls.Load() != 1 {
				t.Fatalf("lookup error %v, want %v", err, row.want)
			}
			if row.want == nil {
				if results == nil || len(results) != 0 {
					t.Fatal("empty results did not remain an array")
				}
				if _, hit, _, err := search.lookup(t.Context(), "Probe"); err != nil || !hit || calls.Load() != 1 {
					t.Fatal("empty result was not cached")
				}
			} else if len(search.cache) != 0 {
				t.Fatal("failure entered the successful cache")
			}
		})
	}
}

func TestPlaceResultsBoundsAndNames(t *testing.T) {
	t.Parallel()
	for _, bounds := range []string{`["51.4","51.6","-0.3","0.1"]`, `["51.5","51.5","-0.3","0.1"]`, `["51.6","51.4","-0.3","0.1"]`, `["51.4","51.6","170","-170"]`, `["NaN","51.6","-0.3","0.1"]`, `["-90","90","-0.3","0.1"]`, `[]`, `null`, `"malformed"`, `[1,2,3,4]`} {
		body := []byte(`[{"display_name":"London","lat":"51.5","lon":"-0.12","boundingbox":` + bounds + `}]`)
		results, err := decodePlaces(body)
		if err != nil || len(results) != 1 || (results[0].Bounds != nil) != (bounds == `["51.4","51.6","-0.3","0.1"]`) {
			t.Fatalf("bounds %s: %+v %v", bounds, results, err)
		}
	}
	for _, lat := range []string{"NaN", "+Inf", "80.1", "-80.1", ""} {
		if _, err := decodePlaces([]byte(`[{"display_name":"Place","lat":"` + lat + `","lon":"0"}]`)); err == nil {
			t.Fatalf("accepted latitude %q", lat)
		}
	}
	item := providerPlace{Name: strings.Repeat("é", 300), Lat: "0", Lon: "180"}
	body, err := json.Marshal([]providerPlace{item, item, item, item, item, item})
	if err != nil {
		t.Fatal(err)
	}
	results, err := decodePlaces(body)
	if err != nil || len(results) != 5 || len(results[0].Name) != 512 || !utf8.ValidString(results[0].Name) {
		t.Fatal("result or UTF-8 name limit failed")
	}
}

func TestPlaceHTTPQueryOriginsAndErrors(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	search, _ := placeFixture(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, placeFixtureJSON) })
	var logs bytes.Buffer
	s, err := NewWithProject(project.Default(), WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	if err != nil {
		t.Fatal(err)
	}
	s.places = search
	t.Cleanup(s.Close)
	handler := s.HandlerFS(fstest.MapFS{})
	for _, row := range []struct {
		name    string
		raw     string
		headers http.Header
		status  int
	}{
		{"native", "q=London", nil, 200},
		{"browser", "q=London", http.Header{"Referer": {"http://example.com/editor.html"}, "Sec-Fetch-Site": {"same-origin"}}, 200},
		{"trim", "q=+London+", nil, 200},
		{"foreign origin", "q=London", http.Header{"Origin": {"https://foreign.test"}}, 403},
		{"multiple origins", "q=London", http.Header{"Origin": {"http://example.com", "http://example.com"}}, 403},
		{"foreign referer", "q=London", http.Header{"Referer": {"https://foreign.test/"}}, 403},
		{"multiple referers", "q=London", http.Header{"Referer": {"http://example.com/a", "http://example.com/b"}}, 403},
		{"cross site", "q=London", http.Header{"Sec-Fetch-Site": {"cross-site"}}, 403},
		{"same site", "q=London", http.Header{"Sec-Fetch-Site": {"same-site"}}, 403},
		{"empty", "q=+", nil, 400},
		{"repeated", "q=London&q=Other", nil, 400},
		{"unknown", "q=London&other=1", nil, 400},
		{"escaped", "q=%zz", nil, 400},
		{"utf8", "q=%ff", nil, 400},
		{"long", "q=" + strings.Repeat("x", 201), nil, 400},
		{"raw long", "q=" + strings.Repeat("x", 1025), nil, 400},
		{"throttle", "q=Other", nil, 429},
	} {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/api/places?"+row.raw, http.NoBody)
		request.Header = row.headers
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != row.status || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: code%d, body%s", row.name, response.Code, response.Body.String())
		}
		if row.status == 429 && response.Header().Get("Retry-After") != "1" {
			t.Fatal("throttle omitted Retry-After")
		}
	}
	if calls.Load() != 1 || strings.Contains(logs.String(), "London") || strings.Contains(logs.String(), "Other") {
		t.Fatal("invalid/cache queries reached upstream or query text entered logs")
	}
	s.places = nil
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/places?q=London", http.NoBody))
	if response.Code != 503 || !strings.Contains(response.Body.String(), `"error"`) {
		t.Fatal("disabled search did not use the API error form")
	}
	public, err := WithPublicOrigin("https://demo.test")
	if err != nil {
		t.Fatal(err)
	}
	public(s)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://demo.test/api/places?q=London", http.NoBody)
	request.Header.Set("Referer", "https://demo.test/editor.html")
	if !s.placeOriginAllowed(request) {
		t.Fatal("configured proxy origin was rejected")
	}
	request.Host = "wrong.test"
	if s.placeOriginAllowed(request) {
		t.Fatal("configured authority accepted another Host")
	}
	if _, err := url.ParseQuery("q=%zz"); err == nil {
		t.Fatal("malformed-query fixture stopped being malformed")
	}
}
