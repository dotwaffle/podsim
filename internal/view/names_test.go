package view

import (
	"strings"
	"testing"
)

// TestJourneyTextUsesOrderVerb checks that the journey title uses the verb
// of the Order button, and that the demo hint tells how to stop the demo.
func TestJourneyTextUsesOrderVerb(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		demo            bool
		wantTitle       string
		wantHintSuffix  string
		wantHintContain string
	}{
		{name: "journey", wantTitle: "ORDER A JOURNEY", wantHintSuffix: "Orders wait when all pods are busy.", wantHintContain: "From and To"},
		{name: "traffic demo", demo: true, wantTitle: "TRAFFIC DEMO", wantHintSuffix: " Reset stops the demo.", wantHintContain: "Four pods"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			title, hint := journeyText(test.demo)
			if title != test.wantTitle {
				t.Errorf("title = %q, want %q", title, test.wantTitle)
			}
			if !strings.HasSuffix(hint, test.wantHintSuffix) || !strings.Contains(hint, test.wantHintContain) {
				t.Errorf("hint = %q, want %q and the suffix %q", hint, test.wantHintContain, test.wantHintSuffix)
			}
		})
	}
}

// TestNamedControlsFit checks the map hint text. It also checks at each
// control layout that the Order labels fit in the request button, and that
// the map hint sits between the map title and the zoom buttons without an
// overlap with a control.
func TestNamedControlsFit(t *testing.T) {
	t.Parallel()
	if want := "CLICK POD / CLICK FROM / SHIFT+CLICK TO / TAB POD / SCROLL ZOOM / DRAG PAN"; mapHintLabel.value != want {
		t.Errorf("map hint = %q, want %q", mapHintLabel.value, want)
	}
	for _, layout := range controlLayouts {
		t.Run(layout.name, func(t *testing.T) {
			t.Parallel()
			game := controlTestGame(t, layout.input)
			controls := game.buttons()
			request := findButton(t, controls, "request")
			for _, value := range []string{"Order [Enter]", "Order accepted"} {
				if got := game.fitButtonText(value, 14, request.w); got != value {
					t.Errorf("request label %q shortened to %q in width %g", value, got, request.w)
				}
			}
			hint := game.labelArea(mapHintLabel)
			title := game.labelArea(label{x: 44, y: 113, size: 12, value: "NETWORK"})
			if hint.left <= title.right {
				t.Errorf("map hint starts at %g, map title ends at %g", hint.left, title.right)
			}
			if zoomOut := findButton(t, controls, "map-zoom-out"); hint.right >= zoomOut.x {
				t.Errorf("map hint ends at %g, zoom out button starts at %g", hint.right, zoomOut.x)
			}
			for _, control := range controls {
				if hint.overlaps(buttonArea(control)) {
					t.Errorf("map hint %+v overlaps control %q %+v", hint, control.action, buttonArea(control))
				}
			}
		})
	}
}
