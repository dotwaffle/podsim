//go:build !js || !wasm

package session

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/dotwaffle/podsim/internal/project"
)

func TestPublicOriginValidation(t *testing.T) {
	t.Parallel()
	valid := map[string]publicOrigin{
		"http://localhost":               {scheme: "http", authority: "localhost"},
		"HTTPS://Example.COM:00443":      {scheme: "https", authority: "example.com"},
		"http://example.com:80":          {scheme: "http", authority: "example.com"},
		"https://example.com:8443":       {scheme: "https", authority: "example.com:8443"},
		"https://example.com.":           {scheme: "https", authority: "example.com."},
		"http://127.0.0.1:8080":          {scheme: "http", authority: "127.0.0.1:8080"},
		"https://[2001:0DB8:0:0::1]:443": {scheme: "https", authority: "[2001:db8::1]"},
		"https://xn--bcher-kva.example":  {scheme: "https", authority: "xn--bcher-kva.example"},
	}
	for value, want := range valid {
		got, err := parseOrigin(value)
		if err != nil || got != want {
			t.Errorf("parseOrigin(%q)=%+v,%v want %+v", value, got, err, want)
		}
	}
	for _, value := range []string{
		"//example.com", "example.com", "ftp://example.com", "https:", "https:///example.com",
		"https://user@example.com", "https://user:pass@example.com", "https://example.com/", "https://example.com/path", "https://example.com/%2F",
		"https://example.com?", "https://example.com?q=x", "https://example.com#", "https://example.com#x",
		"https://example.com:", "https://example.com:0", "https://example.com:65536", "https://example.com:-1", "https://example.com:abc",
		"https://bad host", "https://a..b", "https://-a.example", "https://a-.example", "https://*.example", "https://bücher.example",
		"https://999.1.2.3", "https://127.1", "https://[127.0.0.1]", "https://::1", "https://[::1]x", "https://[fe80::1%25eth0]",
		" https://example.com", "https://example.com ", "https://example.com,https://other.com", "null",
	} {
		if _, err := WithPublicOrigin(value); err == nil {
			t.Errorf("accepted public origin %q", value)
		}
	}
	option, err := WithPublicOrigin("")
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{publicOrigin: &publicOrigin{scheme: "https", authority: "example.com"}}
	option(s)
	if s.publicOrigin != nil {
		t.Fatal("empty origin did not retain default policy")
	}
}

func TestPublicOriginPolicy(t *testing.T) {
	t.Parallel()
	option, err := WithPublicOrigin("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{}
	option(s)
	for _, test := range []struct {
		name, host string
		origins    []string
		want       bool
	}{
		{name: "native", host: "EXAMPLE.COM:443", want: true},
		{name: "browser", host: "example.com", origins: []string{"https://EXAMPLE.COM:0443"}, want: true},
		{name: "native wrong host", host: "other.com"},
		{name: "wrong host port", host: "example.com:444", origins: []string{"https://example.com"}},
		{name: "empty host port", host: "example.com:", origins: []string{"https://example.com"}},
		{name: "host URL", host: "https://example.com", origins: []string{"https://example.com"}},
		{name: "empty Origin", host: "example.com", origins: []string{""}},
		{name: "null Origin", host: "example.com", origins: []string{"null"}},
		{name: "duplicate Origin", host: "example.com", origins: []string{"https://example.com", "https://example.com"}},
		{name: "multiple Origin", host: "example.com", origins: []string{"https://example.com https://other.com"}},
		{name: "comma Origin", host: "example.com", origins: []string{"https://example.com,https://other.com"}},
		{name: "path Origin", host: "example.com", origins: []string{"https://example.com/"}},
		{name: "credentials Origin", host: "example.com", origins: []string{"https://user@example.com"}},
		{name: "wrong scheme", host: "example.com", origins: []string{"http://example.com"}},
		{name: "wrong port", host: "example.com", origins: []string{"https://example.com:8443"}},
		{name: "trailing dot differs", host: "example.com.", origins: []string{"https://example.com"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			policy := &Session{publicOrigin: s.publicOrigin}
			t.Parallel()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://backend/", http.NoBody)
			request.Host = test.host
			if test.origins != nil {
				request.Header["Origin"] = test.origins
			}
			request.Header.Set("Forwarded", "proto=https;host=example.com")
			request.Header.Set("X-Forwarded-Proto", "https")
			request.Header.Set("X-Forwarded-Host", "example.com")
			if got := policy.originAllowed(request); got != test.want {
				t.Fatalf("allowed=%t want %t", got, test.want)
			}
		})
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", http.NoBody)
	request.Header["Origin"] = []string{"https://example.com"}
	request.Header["origin"] = []string{"https://example.com"}
	if s.originAllowed(request) {
		t.Fatal("accepted duplicate Origin with alternate casing")
	}
	// Default policy derives the scheme from the actual backend connection.
	s.publicOrigin = nil
	request.Header = http.Header{"Origin": []string{"https://example.com"}, "X-Forwarded-Proto": []string{"https"}}
	if s.originAllowed(request) {
		t.Fatal("trusted forwarded scheme without configuration")
	}
	request.TLS = &tls.ConnectionState{}
	if !s.originAllowed(request) {
		t.Fatal("rejected direct TLS default")
	}
	request.Header.Set("Origin", "https://EXAMPLE.COM")
	if s.originAllowed(request) {
		t.Fatal("changed default exact Host matching")
	}
}

func TestPublicOriginCommandAndStream(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, configured, host, origin         string
		tls, local, missing, duplicate, denied bool
	}{
		{name: "default HTTP", local: true},
		{name: "default TLS", tls: true, local: true},
		{name: "forwarded scheme alone", host: "example.com", origin: "https://example.com", denied: true},
		{name: "HTTPS public plaintext backend", configured: "https://example.com", host: "example.com:443", origin: "https://example.com"},
		{name: "HTTPS public TLS backend", configured: "https://EXAMPLE.COM:443", host: "example.com", origin: "https://example.com:443", tls: true},
		{name: "HTTP public", configured: "http://example.com:80", host: "EXAMPLE.COM", origin: "http://example.com:080"},
		{name: "IPv6 normalized", configured: "https://[::1]", host: "[0:0:0:0:0:0:0:1]:443", origin: "https://[::1]:443"},
		{name: "native missing Origin", configured: "https://example.com", host: "example.com", missing: true},
		{name: "native wrong Host", configured: "https://example.com", host: "other.com", missing: true, denied: true},
		{name: "wrong Host", configured: "https://example.com", host: "other.com", origin: "https://example.com", denied: true},
		{name: "wrong Origin", configured: "https://example.com", host: "example.com", origin: "https://other.com", denied: true},
		{name: "wrong port", configured: "https://example.com", host: "example.com", origin: "https://example.com:444", denied: true},
		{name: "wrong scheme", configured: "https://example.com", host: "example.com", origin: "http://example.com", denied: true},
		{name: "null", configured: "https://example.com", host: "example.com", origin: "null", denied: true},
		{name: "duplicate", configured: "https://example.com", host: "example.com", origin: "https://example.com", duplicate: true, denied: true},
		{name: "multiple", configured: "https://example.com", host: "example.com", origin: "https://example.com https://other.com", denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			option, err := WithPublicOrigin(test.configured)
			if err != nil {
				t.Fatal(err)
			}
			s, err := NewWithProject(project.Default(), option)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var server *httptest.Server
			if test.tls {
				server = httptest.NewTLSServer(s.HandlerFS(nil))
			} else {
				server = httptest.NewServer(s.HandlerFS(nil))
			}
			defer server.Close()
			origin, host := test.origin, test.host
			if test.local {
				origin = server.URL
				host = strings.TrimPrefix(strings.TrimPrefix(server.URL, "https://"), "http://")
			}
			headers := http.Header{"X-Forwarded-Proto": []string{"https"}, "X-Forwarded-Host": []string{"example.com"}}
			if !test.missing {
				headers["Origin"] = []string{origin}
			}
			if test.duplicate {
				headers.Add("Origin", origin)
			}
			command, err := json.Marshal(Command{Client: "origin", Sequence: 1, Epoch: s.Frame().Epoch, Action: "pause", Paused: true})
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/api/command", bytes.NewReader(command))
			if err != nil {
				t.Fatal(err)
			}
			request.Host = host
			request.Header = headers.Clone()
			request.Header.Set("Content-Type", "application/json")
			response, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			want := http.StatusOK
			if test.denied {
				want = http.StatusForbidden
			}
			if response.StatusCode != want {
				t.Fatalf("command status=%d want %d", response.StatusCode, want)
			}
			conn, response, err := websocket.Dial(t.Context(), strings.Replace(server.URL, "http", "ws", 1)+"/api/state/stream", &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: headers, Host: host})
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			if conn != nil {
				_ = conn.CloseNow()
			}
			want = http.StatusSwitchingProtocols
			if test.denied {
				want = http.StatusForbidden
			}
			if response == nil || response.StatusCode != want || !test.denied && err != nil {
				t.Fatalf("stream response=%v error=%v want %d", response, err, want)
			}
		})
	}
}
