package session

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dotwaffle/podsim/internal/project"
)

const (
	placeQueryBytes = 200
	placeBodyBytes  = 64 << 10
	placeCacheSize  = 128
	placeCacheTTL   = 24 * time.Hour
)

type placeBounds struct {
	South float64 `json:"south"`
	North float64 `json:"north"`
	West  float64 `json:"west"`
	East  float64 `json:"east"`
}

type placeResult struct {
	Name      string       `json:"name"`
	Latitude  float64      `json:"latitude"`
	Longitude float64      `json:"longitude"`
	Bounds    *placeBounds `json:"bounds,omitempty"`
}

type cachedPlaces struct {
	results []placeResult
	expires time.Time
}

// placeSearch belongs to one server session. Network work never holds the
// simulation mutex. Its slot stays occupied until the client and body finish.
type placeSearch struct {
	mu      sync.Mutex
	base    *url.URL
	client  *http.Client
	now     func() time.Time
	timeout time.Duration
	busy    bool
	next    time.Time
	cache   map[string]cachedPlaces
	order   []string
}

// WithGeocodingURL configures explicit place searches for a native server.
// An empty URL disables searches. Providers must implement Nominatim search.
func WithGeocodingURL(value string) (Option, error) {
	search, err := newPlaceSearch(value)
	if err != nil {
		return nil, err
	}
	return func(s *Session) { s.places = search }, nil
}

func newPlaceSearch(value string) (*placeSearch, error) {
	if value == "" {
		return nil, nil
	}
	base, err := url.Parse(value)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || strings.Contains(value, "#") || base.Opaque != "" {
		return nil, errors.New("geocoding URL needs an HTTP or HTTPS endpoint without credentials, query, or fragment")
	}
	authority, authorityErr := normalizeAuthority(base.Host, base.Scheme)
	if authorityErr != nil {
		return nil, errors.New("geocoding URL has an invalid authority")
	}
	base.Host = authority
	// Fresh HTTP/1 connections prevent automatic GET retries outside the limiter.
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		IdleConnTimeout: 30 * time.Second, MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1,
		MaxResponseHeaderBytes: 16 << 10, Protocols: protocols, DisableKeepAlives: true,
	}
	return &placeSearch{
		base: base, client: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		now: time.Now, timeout: 5 * time.Second, cache: make(map[string]cachedPlaces),
	}, nil
}

func (s *Session) placesHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.placeOriginAllowed(r) {
		writePlaceError(w, "cross-origin place searches are not allowed", http.StatusForbidden)
		return
	}
	if len(r.URL.RawQuery) > 1024 {
		writePlaceError(w, "place query is too large", http.StatusBadRequest)
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	query := strings.TrimSpace(values.Get("q"))
	if err != nil || len(values) != 1 || len(values["q"]) != 1 || query == "" || len(query) > placeQueryBytes || !utf8.ValidString(query) {
		writePlaceError(w, "use one place query with 1 to 200 UTF-8 bytes", http.StatusBadRequest)
		return
	}
	if s.places == nil {
		writePlaceError(w, "place search is disabled on this server", http.StatusServiceUnavailable)
		return
	}
	started := time.Now()
	results, cacheHit, retry, lookupErr := s.places.lookup(r.Context(), query)
	status := http.StatusOK
	if lookupErr != nil {
		switch {
		case errors.Is(lookupErr, errPlaceBusy):
			status = http.StatusTooManyRequests
			w.Header().Set("Retry-After", strconv.Itoa(retry))
		case errors.Is(lookupErr, errPlaceTimeout):
			status = http.StatusGatewayTimeout
		default:
			status = http.StatusBadGateway
		}
		writePlaceError(w, lookupErr.Error(), status)
	} else {
		writeJSON(w, struct {
			Results []placeResult `json:"results"`
		}{Results: results})
	}
	s.logger.Info("Place search", slog.Int("status", status), slog.Bool("cache_hit", cacheHit), slog.Duration("duration", time.Since(started)))
}

func writePlaceError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	writeJSON(w, struct {
		Error string `json:"error"`
	}{Error: message})
}

func placeHeader(h http.Header, name string) []string {
	var values []string
	for key, entries := range h {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	return values
}

func (s *Session) placeOriginAllowed(r *http.Request) bool {
	if !s.originAllowed(r) {
		return false
	}
	if sites := placeHeader(r.Header, "Sec-Fetch-Site"); len(sites) != 0 && (len(sites) != 1 || sites[0] != "same-origin" && sites[0] != "none") {
		return false
	}
	refs := placeHeader(r.Header, "Referer")
	if len(refs) == 0 {
		return true
	}
	if len(refs) != 1 {
		return false
	}
	ref, err := url.Parse(refs[0])
	if err != nil || (ref.Scheme != "http" && ref.Scheme != "https") || ref.Host == "" || ref.User != nil || ref.Opaque != "" {
		return false
	}
	copyRequest := r.Clone(r.Context())
	copyRequest.Header = make(http.Header)
	copyRequest.Header.Set("Origin", ref.Scheme+"://"+ref.Host)
	return s.originAllowed(copyRequest)
}

var (
	errPlaceBusy     = errors.New("place search is busy; try again later")
	errPlaceTimeout  = errors.New("place search timed out")
	errPlaceUpstream = errors.New("place search provider is unavailable or returned invalid data")
)

func clonePlaces(results []placeResult) []placeResult {
	owned := make([]placeResult, len(results))
	copy(owned, results)
	for index := range owned {
		if owned[index].Bounds != nil {
			owned[index].Bounds = new(*owned[index].Bounds)
		}
	}
	return owned
}

func (p *placeSearch) lookup(ctx context.Context, query string) (results []placeResult, cacheHit bool, retry int, err error) {
	p.mu.Lock()
	now := p.now()
	if cached, ok := p.cache[query]; ok && now.Before(cached.expires) {
		results = clonePlaces(cached.results)
		p.mu.Unlock()
		return results, true, 0, nil
	}
	if p.busy || now.Before(p.next) {
		retry = max(1, int(math.Ceil(p.next.Sub(now).Seconds())))
		p.mu.Unlock()
		return nil, false, retry, errPlaceBusy
	}
	p.busy, p.next = true, now.Add(time.Second)
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.busy = false
		if err == nil {
			p.remember(query, results)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	results, err = p.fetch(ctx, query)
	return results, false, 0, err
}

// remember runs under mu. Expired entries and older entries leave the cache
// before a new query can increase it beyond the fixed count.
func (p *placeSearch) remember(query string, results []placeResult) {
	now := p.now()
	kept := p.order[:0]
	for _, key := range p.order {
		cached := p.cache[key]
		if key == query || !now.Before(cached.expires) {
			delete(p.cache, key)
		} else {
			kept = append(kept, key)
		}
	}
	p.order = kept
	if len(p.order) == placeCacheSize {
		delete(p.cache, p.order[0])
		p.order = p.order[1:]
	}
	p.order = append(p.order, query)
	p.cache[query] = cachedPlaces{results: clonePlaces(results), expires: now.Add(placeCacheTTL)}
}

func (p *placeSearch) fetch(ctx context.Context, query string) ([]placeResult, error) {
	endpoint := *p.base
	params := url.Values{"q": {query}, "format": {"jsonv2"}, "limit": {"5"}, "accept-language": {"en"}}
	endpoint.RawQuery = params.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return nil, errPlaceUpstream
	}
	request.Header.Set("User-Agent", "Podsim/1 (+https://github.com/dotwaffle/podsim)")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Language", "en")
	response, err := p.client.Do(request)
	if err != nil {
		if placeTimedOut(ctx, err) {
			return nil, errPlaceTimeout
		}
		return nil, errPlaceUpstream
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errPlaceUpstream
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, placeBodyBytes+1))
	if placeTimedOut(ctx, err) {
		return nil, errPlaceTimeout
	}
	if err != nil || len(body) > placeBodyBytes {
		return nil, errPlaceUpstream
	}
	return decodePlaces(body)
}

func placeTimedOut(ctx context.Context, err error) bool {
	var timeout net.Error
	return errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()
}

type providerPlace struct {
	Name   string         `json:"display_name"`
	Lat    string         `json:"lat"`
	Lon    string         `json:"lon"`
	Bounds jsontext.Value `json:"boundingbox,omitempty"`
}

func coordinate(value string, limit float64) (float64, bool) {
	valueNumber, err := strconv.ParseFloat(value, 64)
	return valueNumber, err == nil && !math.IsNaN(valueNumber) && !math.IsInf(valueNumber, 0) && math.Abs(valueNumber) <= limit
}

func decodePlaces(body []byte) ([]placeResult, error) {
	var raw []providerPlace
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		return nil, errPlaceUpstream
	}
	results := make([]placeResult, 0, min(5, len(raw)))
	for _, item := range raw[:min(5, len(raw))] {
		latitude, latOK := coordinate(item.Lat, project.MaxGeoLatitude)
		longitude, lonOK := coordinate(item.Lon, 180)
		name := strings.TrimSpace(item.Name)
		if !latOK || !lonOK || name == "" || !utf8.ValidString(name) {
			return nil, errPlaceUpstream
		}
		if len(name) > 512 {
			name = name[:512]
			for !utf8.ValidString(name) {
				name = name[:len(name)-1]
			}
		}
		results = append(results, placeResult{Name: strings.Clone(name), Latitude: latitude, Longitude: longitude, Bounds: decodePlaceBounds(item.Bounds)})
	}
	return results, nil
}

func decodePlaceBounds(raw jsontext.Value) *placeBounds {
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil || len(values) != 4 {
		return nil
	}
	south, sOK := coordinate(values[0], project.MaxGeoLatitude)
	north, nOK := coordinate(values[1], project.MaxGeoLatitude)
	west, wOK := coordinate(values[2], 180)
	east, eOK := coordinate(values[3], 180)
	if !sOK || !nOK || !wOK || !eOK || south >= north || west >= east {
		return nil
	}
	return &placeBounds{South: south, North: north, West: west, East: east}
}
