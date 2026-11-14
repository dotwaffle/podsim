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
	"github.com/dotwaffle/podsim/internal/sim"
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
	api.Set("hud", js.FuncOf(page.hud))
	api.Set("setSpeed", js.FuncOf(page.setSpeed))
	api.Set("nextPod", js.FuncOf(page.nextPod))
	api.Set("requestTrip", js.FuncOf(page.requestTrip))
	api.Set("passenger", js.FuncOf(page.passengerState))
	api.Set("endTrip", js.FuncOf(page.endTrip))
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
	// message is the outcome of the last command, for the HUD.
	message string
	// passenger is the passenger flow of the page.
	passenger ride.Passenger
}

// update reads the latest state of the client and the result of a command.
// The client keeps a command pending until its result is read.
func (p *ridePage) update() {
	select {
	case result := <-p.client.Results():
		if result.Command.Action == "trip" {
			p.passenger = p.passenger.Reply(result.Reply, result.Err)
		}
		switch {
		case result.Err != nil:
			p.message = result.Err.Error()
		case result.Reply.Error != "":
			p.message = result.Reply.Error
		default:
			p.message = ""
		}
	default:
	}
	state, connected, _ := p.client.View()
	p.connected = connected
	if state.Epoch != "" {
		p.state = state
		p.bridge.Observe(state, time.Now())
	}
	p.passenger = p.passenger.Step(p.state)
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
// returns { count, version, generation, session, connected, error }. The array must have
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
	return map[string]any{"count": info.Count, "version": float64(info.Version), "generation": float64(info.Generation), "session": info.Session, "connected": p.connected, "error": p.client.ConnectionError()}
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

// hud returns the HUD data of the pod with the ID args[0] as JSON text.
func (p *ridePage) hud(_ js.Value, args []js.Value) any {
	p.update()
	podID := ""
	if len(args) > 0 && args[0].Type() == js.TypeString {
		podID = args[0].String()
	}
	connection := ride.HUDSession{Connected: p.connected, Error: p.client.ConnectionError(), Message: p.message}
	data, err := json.Marshal(p.bridge.HUD(p.state, podID, connection))
	if err != nil {
		return nil
	}
	return string(data)
}

// nextPod returns the pod that auto mode rides after the pod with the ID
// args[0], or an empty string.
func (p *ridePage) nextPod(_ js.Value, args []js.Value) any {
	p.update()
	if len(args) == 0 || args[0].Type() != js.TypeString {
		return ""
	}
	return ride.NextPod(p.state, args[0].String())
}

// requestTrip sends a trip order from the station args[0] to the station
// args[1] for a party of args[2], with shared consent when args[3] is true,
// and starts the passenger flow. It returns the error text when the page
// cannot send the order, or an empty string. passenger shows the reply.
func (p *ridePage) requestTrip(_ js.Value, args []js.Value) any {
	p.update()
	if len(args) < 4 || args[0].Type() != js.TypeString || args[1].Type() != js.TypeString || args[2].Type() != js.TypeNumber || args[3].Type() != js.TypeBoolean {
		return "the trip needs an origin, a destination, a party size, and the consent"
	}
	switch p.passenger.Phase {
	case ride.Requested, ride.Waiting, ride.Riding:
		return "the page follows a trip already"
	}
	consent := sim.PrivateConsent
	if args[3].Bool() {
		consent = sim.SharedConsent
	}
	origin, destination := args[0].String(), args[1].String()
	command := session.Command{Action: "trip", Origin: origin, Destination: destination, PartySize: args[2].Int(), SharingConsent: consent, Service: sim.OnDemandService}
	if err := p.client.SubmitIn(remote.SessionOf(p.state), command); err != nil {
		return err.Error()
	}
	p.passenger = ride.Request(p.state, origin, destination)
	p.message = "Sending the trip"
	return ""
}

// passengerState returns the passenger flow as JSON text.
func (p *ridePage) passengerState(js.Value, []js.Value) any {
	p.update()
	data, err := json.Marshal(p.passenger)
	if err != nil {
		return nil
	}
	return string(data)
}

// endTrip stops the passenger flow. The order stays in the session.
func (p *ridePage) endTrip(js.Value, []js.Value) any {
	p.passenger = ride.Passenger{}
	return nil
}

// setSpeed sends a speed command with the speed args[0] for the session
// that the page shows. It returns the error text when the client cannot
// send the command, or an empty string. The HUD shows the reply.
func (p *ridePage) setSpeed(_ js.Value, args []js.Value) any {
	p.update()
	if len(args) == 0 || args[0].Type() != js.TypeNumber {
		return "the speed is not a number"
	}
	err := p.client.SubmitIn(remote.SessionOf(p.state), session.Command{Action: "speed", Speed: args[0].Int()})
	if err != nil {
		p.message = err.Error()
		return p.message
	}
	p.message = "Changing the session speed"
	return ""
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
