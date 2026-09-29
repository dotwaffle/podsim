package session

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// publicOrigin is one normalized scheme and authority, with no URL path.
type publicOrigin struct {
	scheme    string
	authority string
}

// WithPublicOrigin requires commands and streams to use one public authority.
// An empty value retains the local transport scheme and request Host policy.
// Forwarded headers do not affect this policy. Missing Origin is allowed for
// native clients, but a configured public authority still constrains Host.
func WithPublicOrigin(value string) (Option, error) {
	if value == "" {
		return func(s *Session) { s.publicOrigin = nil }, nil
	}
	origin, err := parseOrigin(value)
	if err != nil {
		return nil, fmt.Errorf("public origin: %w", err)
	}
	return func(s *Session) { s.publicOrigin = &origin }, nil
}

func parseOrigin(value string) (publicOrigin, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return publicOrigin{}, fmt.Errorf("parse origin: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" || parsed.Path != "" || parsed.RawPath != "" || strings.ContainsAny(value, "?#") {
		return publicOrigin{}, errors.New("use an HTTP or HTTPS scheme and authority without credentials, path, query, or fragment")
	}
	authority, err := normalizeAuthority(parsed.Host, parsed.Scheme)
	if err != nil {
		return publicOrigin{}, err
	}
	return publicOrigin{scheme: parsed.Scheme, authority: authority}, nil
}

func normalizeAuthority(authority, scheme string) (string, error) {
	host, port, hasPort := strings.Cut(authority, ":")
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return "", errors.New("invalid bracketed host")
		}
		address, err := netip.ParseAddr(authority[1:end])
		if err != nil || !address.Is6() || address.Zone() != "" {
			return "", errors.New("invalid IPv6 host")
		}
		host = "[" + address.String() + "]"
		suffix := authority[end+1:]
		hasPort = suffix != ""
		if hasPort && !strings.HasPrefix(suffix, ":") {
			return "", errors.New("invalid authority suffix")
		}
		port = strings.TrimPrefix(suffix, ":")
	} else {
		if address, err := netip.ParseAddr(host); err == nil {
			if !address.Is4() {
				return "", errors.New("IPv6 host requires brackets")
			}
			host = address.String()
		} else if !validOriginHostname(host) {
			return "", errors.New("invalid origin hostname")
		}
		host = strings.ToLower(host)
	}
	if hasPort {
		if port == "" {
			return "", errors.New("empty origin port")
		}
		for _, digit := range port {
			if digit < '0' || digit > '9' {
				return "", errors.New("invalid origin port")
			}
		}
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return "", errors.New("origin port must be from 1 to 65535")
		}
		if (scheme == "https" && number == 443) || (scheme == "http" && number == 80) {
			port = ""
		} else {
			port = strconv.FormatUint(number, 10)
		}
	}
	if port != "" {
		return host + ":" + port, nil
	}
	return host, nil
}

func validOriginHostname(host string) bool {
	// Origins use ASCII DNS names. An explicit trailing dot stays significant.
	name := strings.TrimSuffix(host, ".")
	if name == "" || len(name) > 253 {
		return false
	}
	numeric := true
	for label := range strings.SplitSeq(name, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if c >= '0' && c <= '9' {
				continue
			}
			numeric = false
			if c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
				return false
			}
		}
	}
	// Numeric hosts must use an unambiguous IPv4 literal accepted above.
	return !numeric
}

func (s *Session) originAllowed(r *http.Request) bool {
	if s.publicOrigin != nil {
		authority, err := normalizeAuthority(r.Host, s.publicOrigin.scheme)
		if err != nil || authority != s.publicOrigin.authority {
			return false
		}
	}
	var values []string
	for name, entries := range r.Header {
		if strings.EqualFold(name, "Origin") {
			values = append(values, entries...)
		}
	}
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 || values[0] == "" {
		return false
	}
	origin, err := parseOrigin(values[0])
	if err != nil {
		return false
	}
	if s.publicOrigin != nil {
		return origin == *s.publicOrigin
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return values[0] == scheme+"://"+r.Host
}
