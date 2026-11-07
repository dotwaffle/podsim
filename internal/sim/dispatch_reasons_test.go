package sim

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestDispatchReasonTextIsNamed checks that no file other than
// dispatch_reasons.go holds the text of a dispatch reason, so that
// maxExpressDispatchReasonBytes covers each reason the simulation writes.
func TestDispatchReasonTextIsNamed(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "dispatch_reasons.go" {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err == nil && (strings.HasPrefix(text, "Waiting for") ||
				strings.HasSuffix(text, reasonTravelingSuffix) || strings.HasSuffix(text, reasonTrafficSuffix) || strings.HasSuffix(text, reasonFinishingSuffix)) {
				t.Errorf("%s: dispatch reason text %q outside dispatch_reasons.go", fset.Position(literal.Pos()), text)
			}
			return true
		})
	}
}

// TestExpressDispatchReasonBound pins the longest Express reason: a hold
// for a finishing pod with an ID of expressMaxIDBytes bytes.
func TestExpressDispatchReasonBound(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("p", expressMaxIDBytes)
	if got := len(finishingPodReason(id)); got != maxExpressDispatchReasonBytes {
		t.Fatalf("finishing reason %d bytes, bound %d", got, maxExpressDispatchReasonBytes)
	}
	if maxExpressDispatchReasonBytes != 90 {
		t.Fatalf("bound %d bytes, want 90", maxExpressDispatchReasonBytes)
	}
	for _, suffix := range []string{reasonTravelingSuffix, reasonTrafficSuffix} {
		if got := len(podReason("", id, suffix)); got > maxExpressDispatchReasonBytes {
			t.Fatalf("pod reason %d bytes exceeds the bound", got)
		}
	}
}

// TestExpressDispatchReasonRefusal checks that an Express restore keeps a
// saved reason of the bound and refuses a longer one, on a waiting trip and
// on a rider, at both tiers. A plain restore has no such check.
func TestExpressDispatchReasonRefusal(t *testing.T) {
	t.Parallel()
	s, network, fleet := newStrandedFleet(t)
	services := []ExpressService{{ID: "harbor-market", Class: ExpressClass, From: "harbor", To: "market", PartyLimit: 20}}
	if err := s.SetExpressServices(services); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.SubmitTripOptions(TripOptions{From: "harbor", To: "market", PartySize: 1, SharingConsent: SharedConsent, Service: ExpressServiceChoice, ServiceID: "harbor-market"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.transferRider(s.findVehicle("01"), 0, "garden"); err != nil {
		t.Fatal(err)
	}
	s.Step()
	saved := s.ExportState()
	rider := -1
	for index, pod := range saved.Pods {
		if len(pod.Riders) > 0 {
			rider = index
		}
	}
	if len(saved.Waiting) != 1 || rider < 0 {
		t.Fatalf("fixture has %d waiting trips and rider pod %d", len(saved.Waiting), rider)
	}
	for _, test := range []struct {
		name string
		set  func(*SavedState, string)
	}{
		{"waiting trip", func(state *SavedState, reason string) { state.Waiting[0].Request.DispatchReason = reason }},
		{"rider", func(state *SavedState, reason string) { state.Pods[rider].Riders[0].DispatchReason = reason }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, logical := range []bool{false, true} {
				input := RestoreStateInput{OrderContract: ExpressOrderContract, Network: network, Fleet: fleet, ExpressServices: services, LogicalOnly: logical}
				input.State = s.ExportState()
				test.set(&input.State, strings.Repeat("r", maxExpressDispatchReasonBytes))
				if _, _, err := RestoreState(input); err != nil {
					t.Fatalf("logical %t: a reason of the bound is refused: %v", logical, err)
				}
				input.State = s.ExportState()
				test.set(&input.State, strings.Repeat("r", maxExpressDispatchReasonBytes+1))
				if _, _, err := RestoreState(input); err == nil || err.Error() != "saved Express dispatch reason is too long" {
					t.Fatalf("logical %t: %v", logical, err)
				}
				input.OrderContract = ""
				if err := checkContractDispatchReasons(input); err != nil {
					t.Fatalf("logical %t: the plain check refuses: %v", logical, err)
				}
			}
		})
	}
}
