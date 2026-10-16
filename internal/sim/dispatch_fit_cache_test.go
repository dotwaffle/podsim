package sim

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

func newDispatchFitFleet(tb testing.TB, pods int) *Simulation {
	tb.Helper()
	network := lineNetwork([]lineStation{{id: "harbor", berths: pods}, {id: "garden", berths: 1}, {id: "market", berths: 1}, {id: "parking", berths: 1, parking: true}})
	fleet := make([]Placement, pods)
	for i := range fleet {
		fleet[i] = Placement{ID: strconv.Itoa(i + 1), Class: CompactClass, StationID: "harbor", BerthID: "harbor-" + strconv.Itoa(i+1)}
	}
	s, err := NewFleet(network, fleet)
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

func TestDispatchFitCacheMatchesFullScan(t *testing.T) {
	t.Parallel()
	base := Request{From: "harbor", To: "garden", PartySize: 4, SharingConsent: PrivateConsent, Service: OnDemandService}
	for _, tc := range []struct {
		name   string
		change func(*Request)
		want   bool
	}{
		{"same options", func(*Request) {}, true},
		{"party size", func(r *Request) { r.PartySize = 5 }, false},
		{"consent", func(r *Request) { r.SharingConsent = "unknown" }, false},
		{"service", func(r *Request) { r.Service = ExpressServiceChoice }, false},
		{"service ID", func(r *Request) { r.ServiceID = "unknown" }, false},
		{"original origin", func(r *Request) { r.From = "missing" }, false},
		{"destination", func(r *Request) { r.To = "missing" }, false},
		{"leg origin", func(r *Request) { r.LegFrom = "missing" }, false},
		{"parking leg", func(r *Request) { r.LegFrom = "parking" }, false},
		{"transfer", func(r *Request) { r.LegFrom = "market" }, true},
		{"omitted options", func(r *Request) { r.PartySize, r.SharingConsent, r.Service = 0, "", "" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, reverse := range []bool{false, true} {
				s := newDispatchFitFleet(t, 2)
				changed := base
				tc.change(&changed)
				requests := []Request{base, changed, base, changed}
				if reverse {
					requests = []Request{changed, base, changed, base}
				}
				pass := new(dispatchPass)
				pass.begin(nil)
				for _, request := range requests {
					// The old full-fleet function does not read the pass or its key.
					want := s.hasFittingPod(request)
					if request == changed && want != tc.want {
						t.Fatalf("full-scan fixture = %v, want %v", want, tc.want)
					}
					if got := s.cachedHasFittingPod(request, pass); got != want {
						t.Fatalf("reverse %v, request %+v: cached %v, full scan %v", reverse, request, got, want)
					}
				}
			}
		})
	}
}

func TestDispatchFitCacheKeepsOriginalServiceOrigin(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		s, _, _ := expressTestFleet(t)
		if err := s.SetExpressServices([]ExpressService{{ID: "hg", From: "harbor", To: "garden", Class: ExpressClass, PartyLimit: 20}}); err != nil {
			t.Fatal(err)
		}
		valid := Request{From: "harbor", LegFrom: "market", To: "garden", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "hg"}
		invalid := valid
		invalid.From = "market"
		if valid.dispatchOptions() != invalid.dispatchOptions() {
			t.Fatal("fixture must collide when only dispatchOptions is keyed")
		}
		if !s.hasFittingPod(valid) || s.hasFittingPod(invalid) {
			t.Fatal("fixture must fit only the original directed service pair")
		}
		requests := []Request{valid, invalid, valid, invalid}
		if reverse {
			requests = []Request{invalid, valid, invalid, valid}
		}
		pass := new(dispatchPass)
		pass.begin(nil)
		for _, request := range requests {
			if got, want := s.cachedHasFittingPod(request, pass), s.hasFittingPod(request); got != want {
				t.Fatalf("reverse %v, original origin %s: cached %v, full scan %v", reverse, request.From, got, want)
			}
		}
	}
}

func TestDispatchFitCacheIgnoresOrderBookkeeping(t *testing.T) {
	t.Parallel()
	s := newDispatchFitFleet(t, 1)
	pass := new(dispatchPass)
	pass.begin(nil)
	request := Request{From: "harbor", To: "garden", PartySize: 1}
	if !s.cachedHasFittingPod(request, pass) {
		t.Fatal("fixture does not fit")
	}
	request.ID, request.PodID, request.DispatchReason = 20, "other", "previous reason"
	request.RequestedTick, request.BoardedTick, request.Completed = 15, 16, true
	request.LegFrom = request.From
	if !s.cachedHasFittingPod(request, pass) || len(pass.fittingPods) != 1 {
		t.Fatalf("equivalent fit inputs did not reuse one entry: %+v", pass.fittingPods)
	}
}

func TestDispatchFitCacheInvalidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		clear func(*dispatchPass)
	}{
		{"reset", func(pass *dispatchPass) { pass.reset() }},
		{"begin", func(pass *dispatchPass) { pass.begin(nil) }},
		{"end", func(pass *dispatchPass) { pass.end() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, initiallyFits := range []bool{false, true} {
				s := newDispatchFitFleet(t, 1)
				request := Request{From: "harbor", To: "garden", PartySize: 4}
				if !initiallyFits {
					s.vehicles[0].Pod.Class = LegacyClass
				}
				pass := &dispatchPass{active: true}
				pass.begin(nil)
				if got := s.cachedHasFittingPod(request, pass); got != initiallyFits || len(pass.fittingPods) != 1 {
					t.Fatalf("initial result %v, cache size %d", got, len(pass.fittingPods))
				}
				tc.clear(pass)
				if len(pass.fittingPods) != 0 {
					t.Fatalf("%s retained fit results", tc.name)
				}
				// Change a dependency between cache lifetimes, in both directions.
				s.vehicles[0].Pod.Class = CompactClass
				if initiallyFits {
					s.vehicles[0].Pod.Class = LegacyClass
				}
				if tc.name == "end" {
					if pass.active {
						t.Fatal("end retained the active pass")
					}
					pass.begin(nil)
				}
				if got, want := s.cachedHasFittingPod(request, pass), s.hasFittingPod(request); got != want || got == initiallyFits {
					t.Fatalf("%s: cached %v, full scan %v, previous %v", tc.name, got, want, initiallyFits)
				}
			}
		})
	}
}

func TestDispatchFitCacheFallbackReasons(t *testing.T) {
	t.Parallel()
	s := newDispatchFitFleet(t, 1)
	pass := new(dispatchPass)
	pass.begin(nil)
	for _, size := range []int{4, 5, 4, 5} {
		request := Request{ID: size, From: "harbor", To: "garden", PartySize: size}
		trip := waitingTrip{request: request, excludedPod: s.vehicles[0].Pod.ID}
		before := trip
		// Independent fallback reference: do not use cachedPickupPod, the fit
		// cache, or either cache key to derive the expected reason.
		if v := s.pickupPodForRequest(request, trip.excludedPod, new(dispatchPass)); v != nil {
			t.Fatal("excluded sole pod must not be available")
		}
		want := "Waiting for an available pod"
		if !s.hasFittingPod(request) {
			want = "Waiting for a certified vehicle that fits this party and route"
		}
		v, ok := s.assignPickupPod(0, &trip, pass)
		if v != nil || ok || trip.request.DispatchReason != want {
			t.Fatalf("size %d: pod %v, ok %v, reason %q, want %q", size, v, ok, trip.request.DispatchReason, want)
		}
		before.request.DispatchReason = want
		if !reflect.DeepEqual(trip, before) {
			t.Fatalf("fallback changed more than the reason: got %+v, want %+v", trip, before)
		}
	}
	if len(pass.fittingPods) != 2 {
		t.Fatalf("fallback did not cache both fit results: %+v", pass.fittingPods)
	}
}

func TestDispatchFitCacheAcrossDispatches(t *testing.T) {
	t.Parallel()
	s := newDispatchFitFleet(t, 1)
	trip := waitingTrip{
		request:     Request{ID: 1, From: "harbor", To: "garden", PartySize: 4},
		excludedPod: s.vehicles[0].Pod.ID,
	}
	s.requestID, s.waiting = 1, []waitingTrip{trip}
	for _, class := range []VehicleClass{CompactClass, LegacyClass, CompactClass} {
		s.vehicles[0].Pod.Class = class
		want := "Waiting for an available pod"
		if !s.hasFittingPod(trip.request) {
			want = "Waiting for a certified vehicle that fits this party and route"
		}
		s.dispatch()
		if len(s.waiting) != 1 {
			t.Fatalf("class %s: dispatch removed the excluded trip", class)
		}
		trip.request.DispatchReason = want
		if !reflect.DeepEqual(s.waiting[0], trip) {
			t.Fatalf("class %s: got %+v, want %+v", class, s.waiting[0], trip)
		}
		if s.pass.active || len(s.pass.fittingPods) != 0 {
			t.Fatal("dispatch retained an active pass or fit results")
		}
		if s.requestID != 1 || s.boarded != 0 || len(s.vehicles[0].Riders) != 0 {
			t.Fatal("fallback changed order history or boarding counters")
		}
	}
}

func BenchmarkDispatchFitCache(b *testing.B) {
	for _, pods := range []int{1, 120} {
		for _, size := range []int{4, 5} {
			for _, cached := range []bool{false, true} {
				b.Run(fmt.Sprintf("pods=%d/party=%d/cached=%v", pods, size, cached), func(b *testing.B) {
					s := newDispatchFitFleet(b, pods)
					request := Request{From: "harbor", To: "garden", PartySize: size}
					want := s.hasFittingPod(request)
					pass := new(dispatchPass)
					pass.begin(nil)
					b.ReportAllocs()
					matches := 0
					for b.Loop() {
						pass.reset()
						matches = 0
						for range 1024 {
							var fits bool
							if cached {
								fits = s.cachedHasFittingPod(request, pass)
							} else {
								fits = s.hasFittingPod(request)
							}
							if fits {
								matches++
							}
						}
					}
					if want && matches != 1024 || !want && matches != 0 {
						b.Fatalf("matches %d, want fit %v", matches, want)
					}
				})
			}
		}
	}
}
