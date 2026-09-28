//go:build js && wasm

package remote

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"syscall/js"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
)

func streamSupported() error {
	for _, name := range []string{"WebSocket", "DecompressionStream", "AbortController"} {
		if js.Global().Get(name).Type() != js.TypeFunction {
			return errors.New("this browser needs WebSocket and native gzip decompression to display Podsim")
		}
	}
	if js.Global().Get("PodsimStream").Type() != js.TypeObject {
		return errors.New("the state stream decoder did not load; reload the page")
	}
	return nil
}
func inflatePublication(ctx context.Context, data []byte) ([]byte, error) {
	if len(data) > session.MaxStreamMessage {
		return nil, errors.New("compressed state too large")
	}
	array := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(array, data)
	controller := js.Global().Get("AbortController").New()
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	success := js.FuncOf(func(_ js.Value, args []js.Value) any {
		value := args[0]
		size := value.Get("byteLength").Int()
		if size > session.MaxStreamJSON {
			done <- result{err: errors.New("state JSON too large")}
			return nil
		}
		out := make([]byte, size)
		js.CopyBytesToGo(out, value)
		done <- result{data: out}
		return nil
	})
	failure := js.FuncOf(func(_ js.Value, args []js.Value) any {
		done <- result{err: fmt.Errorf("decompress state: %s", args[0].String())}
		return nil
	})
	defer success.Release()
	defer failure.Release()
	js.Global().Get("PodsimStream").Call("inflate", array, session.MaxStreamJSON, controller.Get("signal")).Call("then", success, failure)
	select {
	case value := <-done:
		return value.data, value.err
	case <-ctx.Done():
		controller.Call("abort")
		<-done
		return nil, ctx.Err()
	}
}

func dialStream(ctx context.Context, url string, _ *http.Client) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(ctx, url, nil)
}
