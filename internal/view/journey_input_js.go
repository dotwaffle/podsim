//go:build js && wasm

package view

import (
	"strconv"
	"sync"
	"syscall/js"
)

type journeyInputEvent struct {
	kind, value string
	side        int
}

type browserJourney struct {
	mu        sync.Mutex
	events    []journeyInputEvent
	fields    [2]js.Value
	callbacks []js.Func
	ready     bool
}

func (b *browserJourney) listen(field js.Value, name string, callback func(js.Value)) {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any { callback(args[0]); return nil })
	b.callbacks = append(b.callbacks, fn)
	field.Call("addEventListener", name, fn)
}

func (b *browserJourney) enqueue(event journeyInputEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
}

func (b *browserJourney) initialize() {
	document := js.Global().Get("document")
	for index := range b.fields {
		side := index + 1
		field := document.Call("createElement", "input")
		b.fields[index] = field
		field.Set("type", "text")
		field.Set("id", []string{"journeyFrom", "journeyTo"}[index])
		field.Set("placeholder", "Station name or code")
		field.Set("maxLength", 80)
		field.Call("setAttribute", "aria-label", []string{"From station name or code", "To station name or code"}[index])
		field.Set("autocomplete", "off")
		field.Set("spellcheck", false)
		field.Get("style").Set("cssText", "position:fixed;z-index:5;box-sizing:border-box;border:1px solid #91a6ba;border-radius:0;background:#354b5e;color:#e5edf5;padding:3px 8px;font-family:sans-serif;")
		b.listen(field, "focus", func(_ js.Value) { field.Call("select"); b.enqueue(journeyInputEvent{kind: "focus", side: side}) })
		b.listen(field, "blur", func(_ js.Value) { b.enqueue(journeyInputEvent{kind: "blur", side: side}) })
		b.listen(field, "input", func(_ js.Value) {
			b.enqueue(journeyInputEvent{kind: "input", side: side, value: field.Get("value").String()})
		})
		b.listen(field, "keydown", func(event js.Value) {
			event.Call("stopPropagation")
			switch event.Get("key").String() {
			case "Enter":
				event.Call("preventDefault")
				b.enqueue(journeyInputEvent{kind: "enter", side: side})
				if side == 1 {
					b.fields[1].Call("focus")
				} else {
					field.Call("blur")
					document.Call("querySelector", "canvas").Call("focus")
				}
			case "Escape":
				event.Call("preventDefault")
				b.enqueue(journeyInputEvent{kind: "cancel", side: side})
				field.Call("blur")
				document.Call("querySelector", "canvas").Call("focus")
			}
		})
		b.listen(field, "keyup", func(event js.Value) { event.Call("stopPropagation") })
		document.Get("body").Call("appendChild", field)
	}
	b.ready = true
}

// update uses native fields so rendering delays cannot merge keystrokes
// across a Tab or lose pasted text. Events retain browser delivery order.
func (b *browserJourney) update(g *Game) (bool, bool) {
	if !b.ready {
		b.initialize()
	}
	b.mu.Lock()
	events := b.events
	b.events = nil
	b.mu.Unlock()
	for _, event := range events {
		switch event.kind {
		case "focus":
			g.startJourneySearch(event.side)
		case "input":
			g.journeySearch.focus = event.side
			g.searchJourney(event.value)
		case "blur":
			if g.journeySearch.focus == event.side {
				g.journeySearch.focus = 0
			}
		case "cancel":
			g.journeySearch.unresolved[event.side-1] = false
			g.journeySearch.query[event.side-1] = ""
			g.journeySearch.focus = 0
			g.message = ""
		case "enter":
			if event.side == 2 {
				g.request()
			}
		}
	}
	g.ensureLayout()
	canvas := js.Global().Get("document").Call("querySelector", "canvas")
	if canvas.IsNull() {
		return len(events) > 0, true
	}
	rect := canvas.Call("getBoundingClientRect")
	sx, sy := rect.Get("width").Float()/float64(g.layout.width), rect.Get("height").Float()/float64(g.layout.height)
	active := js.Global().Get("document").Get("activeElement")
	focused := false
	controls := g.journeySearchButtons(g.state.Simulation.Demo || !g.connected)
	for index, field := range b.fields {
		control := g.layoutButton(controls[index])
		style := field.Get("style")
		pixel := func(value float64) string { return strconv.FormatFloat(value, 'f', 2, 64) + "px" }
		style.Set("left", pixel(rect.Get("left").Float()+control.x*sx))
		style.Set("top", pixel(rect.Get("top").Float()+control.y*sy))
		style.Set("width", pixel(control.w*sx))
		style.Set("height", pixel(control.h*sy))
		style.Set("fontSize", pixel(12*g.layout.unit*sx))
		field.Set("hidden", len(g.network.Stations) == 0)
		field.Set("disabled", control.disabled)
		field.Call("setAttribute", "aria-invalid", strconv.FormatBool(g.journeySearch.unresolved[index]))
		border := "#91a6ba"
		if g.journeySearch.unresolved[index] {
			border = "#f3c479"
		}
		style.Set("borderColor", border)
		if active.Equal(field) {
			focused = true
			continue
		}
		value := g.journeySearch.query[index]
		if !g.journeySearch.unresolved[index] {
			id := g.origin
			if index == 1 {
				id = g.destination
			}
			if station, ok := g.network.Station(id); ok {
				value = station.Name
				if code := stationCode(id); code != "" {
					value = code + "  " + value
				}
			}
		}
		if field.Get("value").String() != value {
			field.Set("value", value)
		}
	}
	return focused || len(events) > 0, true
}
