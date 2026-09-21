package session

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// compressResponse keeps range responses byte-addressable and varies cached representations.
func compressResponse(next http.Handler) http.Handler {
	pool := sync.Pool{New: func() any { writer, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return writer }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		if r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		writer := pool.Get().(*gzip.Writer)
		writer.Reset(w)
		response := &compressedResponse{ResponseWriter: w, writer: writer, bodyAllowed: r.Method != http.MethodHead}
		defer func() {
			if response.bodyAllowed {
				if !response.wroteHeader {
					response.WriteHeader(http.StatusOK)
				}
				_ = writer.Close()
			}
			writer.Reset(io.Discard)
			pool.Put(writer)
		}()
		next.ServeHTTP(response, r)
	})
}

type compressedResponse struct {
	http.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
	bodyAllowed bool
}

func (w *compressedResponse) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.Header().Del("Content-Length")
	if status != http.StatusNoContent && status != http.StatusNotModified {
		w.Header().Set("Content-Encoding", "gzip")
	} else {
		w.bodyAllowed = false
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *compressedResponse) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", http.DetectContentType(body))
		}
		w.WriteHeader(http.StatusOK)
	}
	return w.writer.Write(body)
}

func acceptsGzip(header string) bool {
	wildcard := false
	for entry := range strings.SplitSeq(header, ",") {
		parts := strings.Split(entry, ";")
		encoding := strings.TrimSpace(parts[0])
		quality := 1.0
		for _, parameter := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(key, "q") {
				parsed, err := strconv.ParseFloat(value, 64)
				if err != nil || !(parsed >= 0 && parsed <= 1) {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if strings.EqualFold(encoding, "gzip") {
			return quality > 0
		}
		if encoding == "*" {
			wildcard = quality > 0
		}
	}
	return wildcard
}
