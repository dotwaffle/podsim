package sim

import (
	"errors"
	"maps"
	"testing"
)

func TestCouplingMotionOwnerSealActualLedger(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, false, false)
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := initialCouplingMotion(c, input)
	if err != nil {
		t.Fatal(err)
	}
	base := maps.Clone(input.Owners)
	applyCouplingTestWrites(t, base, initial.Writes)
	// Front at 72 m still needs cell 2, whose raw end is 90 m plus 12 m tail.
	r := resource{kind: trackResource, id: "ab", cell: 2}
	if base[r] != c.owner {
		t.Fatal("independent current-cell control is not group owned")
	}
	for _, test := range []struct {
		name  string
		owner resourceOwner
	}{
		{"free", resourceOwner{}}, {"same_id_pod", podResourceOwner("pair")}, {"unknown_kind", resourceOwner{kind: 99, id: "pair"}}, {"foreign_group", resourceOwner{kind: groupOwnerKind, id: "other"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual := maps.Clone(base)
			actual[r] = test.owner
			before := maps.Clone(actual)
			view, sealErr := sealCouplingMotionOwners(c, initial.State, actual)
			if !errors.Is(sealErr, errCouplingMotionInvariant) || view != nil || !maps.Equal(before, actual) {
				t.Fatalf("actual owner seal failed to deny atomically: %v", sealErr)
			}
		})
	}
	view, err := sealCouplingMotionOwners(c, initial.State, base)
	if err != nil {
		t.Fatal(err)
	}
	base[r] = resourceOwner{}
	if view.owners[r] != c.owner {
		t.Fatal("owner view aliases mutable actual ledger")
	}
	bad := *view
	bad.eventCursor++
	if _, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: initial.State, Owners: &bad}); !errors.Is(err, errCouplingMotionInvariant) {
		t.Fatal("caller counter replaced the actual role-event proof")
	}
}

func TestCouplingMotionInitialStamp(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, true, false)
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	original := make(map[resource]bool)
	for _, claim := range reservation.Claims {
		original[claim.Resource] = true
	}
	var extra resource
	found := false
	for _, claim := range c.claims {
		if !original[claim.Resource] && claim.Resource.kind == trackResource {
			extra = claim.Resource
			found = true
			break
		}
	}
	if !found {
		t.Fatal("initial commit fixture lacks an added actual exit cell")
	}
	for _, test := range []struct {
		name   string
		change func(*couplingReservationInput)
	}{
		{"tick", func(in *couplingReservationInput) { in.Tick++ }},
		{"added_exit_owner", func(in *couplingReservationInput) {
			in.Owners[extra] = resourceOwner{kind: groupOwnerKind, id: "front"}
		}},
		{"consent", func(in *couplingReservationInput) { in.Members[0].Vehicle.Riders[0].SharingConsent = SharedConsent }},
		{"route_version", func(in *couplingReservationInput) { in.Members[0].RouteVersion++ }},
		{"route_fact", func(in *couplingReservationInput) { in.Members[0].Vehicle.Route[0].SpeedLimit = 6 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			current := cloneCouplingInput(input)
			test.change(&current)
			before := cloneCouplingInput(current)
			result, err := initialCouplingMotion(c, current)
			if !errors.Is(err, errCouplingReservationDenied) || len(result.Writes) != 0 || result.State.context != nil || !couplingInputsEqual(before, current) {
				t.Fatalf("changed initial fact was accepted: %v", err)
			}
		})
	}
}

func TestCouplingMotionOwnerAdvanceAppliedWrites(t *testing.T) {
	t.Parallel()
	input := couplingMotionFixture(t, false, false)
	reservation, err := planCouplingReservation(input)
	if err != nil {
		t.Fatal(err)
	}
	c, err := prepareCouplingMotionContext(couplingMotionContextInput{Reservation: reservation, Current: input, GroupID: "pair"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := initialCouplingMotion(c, input)
	if err != nil {
		t.Fatal(err)
	}
	actual := maps.Clone(input.Owners)
	applyCouplingTestWrites(t, actual, initial.Writes)
	if len(c.events) == 0 {
		t.Fatal("no actual role events")
	}
	previous, err := c.stateAt(c.events[0].tick - 1)
	if err != nil {
		t.Fatal(err)
	}
	view, err := sealCouplingMotionOwners(c, previous, actual)
	if err != nil {
		t.Fatal(err)
	}
	step, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: previous, Owners: view})
	if err != nil {
		t.Fatal(err)
	}
	applyCouplingTestWrites(t, actual, step.Writes)
	advanced, err := advanceCouplingMotionOwners(view, previous, step.State, step.Writes, actual)
	if err != nil || advanced == nil {
		t.Fatalf("advance control: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func([]couplingOwnerWrite, map[resource]resourceOwner) []couplingOwnerWrite
	}{
		{"omitted", func(w []couplingOwnerWrite, _ map[resource]resourceOwner) []couplingOwnerWrite { return w[:0] }},
		{"wrong_role", func(w []couplingOwnerWrite, _ map[resource]resourceOwner) []couplingOwnerWrite {
			w[0].Role = couplingTransferredOwner
			return w
		}},
		{"not_applied", func(w []couplingOwnerWrite, a map[resource]resourceOwner) []couplingOwnerWrite {
			a[w[0].Resource] = w[0].Expected
			return w
		}},
		{"wrong_kind", func(w []couplingOwnerWrite, a map[resource]resourceOwner) []couplingOwnerWrite {
			a[w[0].Resource] = podResourceOwner("pair")
			return w
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			writes := append([]couplingOwnerWrite(nil), step.Writes...)
			ledger := maps.Clone(actual)
			writes = test.change(writes, ledger)
			before := maps.Clone(ledger)
			got, err := advanceCouplingMotionOwners(view, previous, step.State, writes, ledger)
			if !errors.Is(err, errCouplingMotionInvariant) || got != nil || !maps.Equal(before, ledger) {
				t.Fatalf("invalid applied writes accepted: %v", err)
			}
		})
	}
	if _, err := planCouplingMotion(couplingMotionInput{Context: c, Previous: step.State, Owners: view}); !errors.Is(err, errCouplingMotionInvariant) {
		t.Fatal("stale owner proof crossed a role boundary")
	}
}

// Advance checks only the exact affected writes after atomic application.
// Other relevant external writes must invalidate the view before this call.
func advanceCouplingMotionOwners(view *couplingMotionOwnerView, previous, next couplingMotionState, applied []couplingOwnerWrite, actual map[resource]resourceOwner) (*couplingMotionOwnerView, error) {
	if view == nil || view.context == nil {
		return nil, couplingMotionInvariant("missing prior owner proof")
	}
	c := view.context
	before, err := c.stateAt(previous.Elapsed)
	if err != nil || before != previous {
		return nil, couplingMotionInvariant("owner advance has an invalid previous stamp")
	}
	after, err := c.stateAt(next.Elapsed)
	if err != nil || after != next || next.Elapsed != previous.Elapsed+1 || view.eventCursor != c.ownerEventCursor(previous.Elapsed) {
		return nil, couplingMotionInvariant("owner advance is not the exact next role boundary")
	}
	end := c.ownerEventCursor(next.Elapsed)
	events := c.events[view.eventCursor:end]
	if len(applied) != len(events) {
		return nil, couplingMotionInvariant("owner advance omitted an applied role write")
	}
	for i, event := range events {
		if applied[i] != event.write || view.owners[event.write.Resource] != event.write.Expected || actual[event.write.Resource] != event.write.Next {
			return nil, couplingMotionInvariant("applied affected owner differs from complete role write")
		}
	}
	if len(events) == 0 {
		return view, nil
	}
	return &couplingMotionOwnerView{context: c, eventCursor: end, owners: view.owners}, nil
}
