//go:build !js

package remote

import (
	"context"
	"net/http"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/session"
)

func streamSupported() error { return nil }
func inflatePublication(ctx context.Context, data []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return session.InflateStream(data)
}

func dialStream(ctx context.Context, url string, client *http.Client) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(ctx, url, &websocket.DialOptions{HTTPClient: client, CompressionMode: websocket.CompressionDisabled})
}
