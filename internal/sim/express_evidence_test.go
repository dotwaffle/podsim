package sim

import (
	"cmp"
	"slices"
	"strconv"
	"sync"
	"testing"
)

type expressEvidenceKey struct {
	test       *testing.T
	simulation *Simulation
}

var expressTickEvidence sync.Map

// Each native simulation has its own ledger, including each cold and warm continuation.
type expressRuntimeEvidence struct {
	Number    int                          `json:"number"`
	Checks    int64                        `json:"checks"`
	FirstTick int64                        `json:"firstTick"`
	LastTick  int64                        `json:"lastTick"`
	Failed    bool                         `json:"failed"`
	Orders    map[int]expressOrderEvidence `json:"orders"`
	State     SavedState                   `json:"state"`
	Snapshot  Snapshot                     `json:"snapshot"`
	Owners    []expressOwnerEvidence       `json:"owners"`
}

type expressOrderEvidence struct {
	ID            int            `json:"id"`
	From          string         `json:"from"`
	To            string         `json:"to"`
	PartySize     int            `json:"partySize"`
	Consent       SharingConsent `json:"consent"`
	Service       ServiceChoice  `json:"service"`
	ServiceID     string         `json:"serviceID"`
	RequestedTick int64          `json:"requestedTick"`
	BoardedTick   int64          `json:"boardedTick"`
	AlightedTick  *int64         `json:"alightedTick"`
}

type expressOwnerEvidence struct {
	Kind  int    `json:"kind"`
	ID    string `json:"id"`
	Cell  int    `json:"cell"`
	Owner string `json:"owner"`
}

func recordExpressTick(t *testing.T, s *Simulation) {
	t.Helper()
	key := expressEvidenceKey{t, s}
	value, known := expressTickEvidence.Load(key)
	var record *expressRuntimeEvidence
	if known {
		record = func() *expressRuntimeEvidence {
			r, ok := value.(*expressRuntimeEvidence)
			if !ok {
				t.Fatal("invalid native evidence record")
			}
			return r
		}()
	} else {
		number := 0
		expressTickEvidence.Range(func(k, v any) bool {
			prior, ok := k.(expressEvidenceKey)
			if !ok {
				t.Fatal("invalid native evidence key")
			}
			if prior.test == t {
				number++
			}
			return true
		})
		record = &expressRuntimeEvidence{Number: number, FirstTick: s.tick, Orders: make(map[int]expressOrderEvidence)}
		expressTickEvidence.Store(key, record)
		t.Cleanup(func() {
			record.Failed = t.Failed()
			record.State = s.ExportState()
			record.Snapshot = s.Snapshot()
			for r, owner := range s.owners {
				record.Owners = append(record.Owners, expressOwnerEvidence{int(r.kind), r.id, r.cell, owner})
			}
			slices.SortFunc(record.Owners, func(a, b expressOwnerEvidence) int {
				if n := cmp.Compare(a.Kind, b.Kind); n != 0 {
					return n
				}
				if n := cmp.Compare(a.ID, b.ID); n != 0 {
					return n
				}
				return cmp.Compare(a.Cell, b.Cell)
			})
			expressEvidence(t, "runtime-"+strconv.Itoa(record.Number)+"-first-tick-"+strconv.FormatInt(record.FirstTick, 10), record)
			expressTickEvidence.Delete(key)
		})
	}
	record.Checks++
	record.LastTick = s.tick
	remember := func(request Request) {
		order := expressOrderEvidence{ID: request.ID, From: request.From, To: request.To, PartySize: request.PartySize, Consent: request.SharingConsent, Service: request.Service, ServiceID: request.ServiceID, RequestedTick: request.RequestedTick, BoardedTick: request.BoardedTick}
		if prior, exists := record.Orders[request.ID]; exists {
			if prior.From != order.From || prior.To != order.To || prior.PartySize != order.PartySize || prior.Consent != order.Consent || prior.Service != order.Service || prior.ServiceID != order.ServiceID || prior.RequestedTick != order.RequestedTick {
				t.Fatal("native accepted order facts changed", request.ID)
			}
			order.AlightedTick = prior.AlightedTick
		}
		record.Orders[request.ID] = order
	}
	for _, trip := range s.waiting {
		remember(trip.request)
	}
	for _, v := range s.vehicles {
		for _, request := range v.Riders {
			remember(request)
		}
	}
	for _, completion := range s.StepCompletions() {
		order := record.Orders[completion.RequestID]
		tick := completion.AlightedTick
		order.AlightedTick = &tick
		record.Orders[completion.RequestID] = order
	}
}
