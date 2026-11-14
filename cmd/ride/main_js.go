// Command ride is the WebAssembly module of the first-person ride page. It
// streams the shared session and gives the page the poses of the pods. See
// docs/design/first-person-ride.md.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json/v2"
	"math"
	"syscall/js"
	"time"

	"github.com/dotwaffle/podsim/internal/remote"
	"github.com/dotwaffle/podsim/internal/ride"
	"github.com/dotwaffle/podsim/internal/session"
)

func main() {
	ctx := context.Background()
	origin := js.Global().Get("location").Get("origin").String()
	client := remote.New(ctx, origin)
	page := &ridePage{client: client}
	api := js.Global().Get("Object").New()
	// The funcs stay for the life of the page, so they are never released.
	api.Set("topology", js.FuncOf(page.topology))
	api.Set("frame", js.FuncOf(page.frame))
	api.Set("pods", js.FuncOf(page.pods))
	api.Set("ids", js.FuncOf(page.ids))
	js.Global().Set("podsimRide", api)
	select {}
}

// ridePage holds the bridge. The browser calls the funcs on its one
// JavaScript thread, so they do not run at the same time.
type ridePage struct {
	client    *remote.Client
	bridge    ride.Bridge
	state     session.State
	connected bool
	poses     []float64
	bytes     []byte
}

// update reads the latest state of the client.
func (p *ridePage) update() {
	state, connected, _ := p.client.View()
	p.connected = connected
	if state.Epoch != "" {
		p.state = state
		p.bridge.Observe(state, time.Now())
	}
}

// topology returns the geometry of the current generation as JSON text, or
// null before the first state.
func (p *ridePage) topology(js.Value, []js.Value) any {
	p.update()
	if p.state.Epoch == "" {
		return nil
	}
	data, err := json.Marshal(ride.NewTopology(p.state))
	if err != nil {
		return nil
	}
	return string(data)
}

// frame fills the Float64Array of args[0] with the poses of the pods, and
// returns { count, version, generation, connected, error }. The array must have
// space for ride.PoseFields numbers for each pod. A short array gets only
// the pods that fit, and count tells how many pods there are.
func (p *ridePage) frame(_ js.Value, args []js.Value) any {
	p.update()
	var info ride.FrameInfo
	p.poses, info = p.bridge.Frame(time.Now(), p.poses)
	if len(args) > 0 && args[0].Type() == js.TypeObject {
		out := args[0]
		count := min(len(p.poses), out.Get("length").Int())
		p.bytes = p.bytes[:0]
		for _, value := range p.poses[:count] {
			p.bytes = binary.LittleEndian.AppendUint64(p.bytes, math.Float64bits(value))
		}
		view := js.Global().Get("Uint8Array").New(out.Get("buffer"), out.Get("byteOffset"), count*8)
		js.CopyBytesToJS(view, p.bytes)
	}
	return map[string]any{"count": info.Count, "version": float64(info.Version), "generation": float64(info.Generation), "connected": p.connected, "error": p.client.ConnectionError()}
}

// ids returns the pod IDs in the order of the poses of the last frame, as
// JSON text.
func (p *ridePage) ids(js.Value, []js.Value) any {
	data, err := json.Marshal(p.bridge.IDs())
	if err != nil {
		return nil
	}
	return string(data)
}

// pods returns the pod list as JSON text.
func (p *ridePage) pods(js.Value, []js.Value) any {
	p.update()
	data, err := json.Marshal(ride.Pods(p.state))
	if err != nil {
		return nil
	}
	return string(data)
}
