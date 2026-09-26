package httpmiddleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Compress gzips JSON responses for clients that accept it.
//
// WHY THE API DOES THIS ITSELF. The API runs on Cloud Run behind Google's frontend, and neither
// Cloud Run nor the HTTPS load balancer compresses a service's responses on its behalf: whatever
// bytes the container writes are the bytes on the wire. Measured 2026-09-19: GET /admin-web/bootstrap
// is 1,032,192 bytes as JSON and 178,197 bytes gzipped, and admin-web (Node fetch, which sends
// Accept-Encoding: gzip and decodes transparently) reads it once per SSR render. Nothing else in
// the repo sets Content-Encoding, so this is the one place.
//
// What it does NOT do: touch a response whose Content-Type is not JSON (proof media, CSV exports,
// text), a response with no body (204/304), a response that already carries a Content-Encoding, a
// body under compressMinBytes (the gzip header would outweigh the saving), or a client that did not
// say gzip. Content-Length is dropped (the compressed length is unknown until the end) and
// Vary: Accept-Encoding is added so any cache keys the two encodings apart. ETags are left alone:
// the handlers that set one compare it BEFORE writing a body, and admin-web revalidates with the
// tag it was given, so the same tag identifies the same entity in either encoding.
func Compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressWriter{ResponseWriter: w}
		defer cw.Close()
		next.ServeHTTP(cw, r)
	})
}

// compressMinBytes: below this the gzip framing costs more than it saves.
const compressMinBytes = 1024

var gzipPool = sync.Pool{New: func() any {
	z, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return z
}}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		token := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if strings.EqualFold(token, "gzip") || token == "*" {
			// "gzip;q=0" is a refusal.
			if q := qValue(part); q == "0" || q == "0.0" || q == "0.00" || q == "0.000" {
				return false
			}
			return true
		}
	}
	return false
}

func qValue(part string) string {
	for _, param := range strings.Split(part, ";")[1:] {
		k, v, ok := strings.Cut(strings.TrimSpace(param), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), "q") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// compressWriter decides ONCE, at the first write (or at WriteHeader when the status has no body),
// whether to compress. Until the buffered bytes reach compressMinBytes it holds them, so a tiny
// JSON envelope is passed through uncompressed with its headers intact.
type compressWriter struct {
	http.ResponseWriter
	status   int
	decided  bool
	compress bool
	buf      []byte
	gz       *gzip.Writer
}

func (c *compressWriter) WriteHeader(status int) {
	if c.decided {
		return
	}
	c.status = status
	if status == http.StatusNoContent || status == http.StatusNotModified || status == http.StatusResetContent || (status >= 100 && status < 200) {
		c.decide(false)
	}
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if !c.decided {
		c.buf = append(c.buf, p...)
		if len(c.buf) < compressMinBytes {
			return len(p), nil
		}
		c.decide(c.eligible())
		return len(p), nil
	}
	if c.compress {
		return c.gz.Write(p)
	}
	return c.ResponseWriter.Write(p)
}

func (c *compressWriter) eligible() bool {
	h := c.ResponseWriter.Header()
	if h.Get("Content-Encoding") != "" {
		return false
	}
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(h.Get("Content-Type"), ";", 2)[0]))
	return ct == "application/json" || strings.HasSuffix(ct, "+json")
}

// decide commits the headers and flushes whatever was buffered, compressed or not.
func (c *compressWriter) decide(compress bool) {
	c.decided = true
	c.compress = compress
	h := c.ResponseWriter.Header()
	if compress {
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")
		h.Add("Vary", "Accept-Encoding")
		c.gz = gzipPool.Get().(*gzip.Writer)
		c.gz.Reset(c.ResponseWriter)
	}
	if c.status == 0 {
		c.status = http.StatusOK
	}
	c.ResponseWriter.WriteHeader(c.status)
	if len(c.buf) > 0 {
		if compress {
			_, _ = c.gz.Write(c.buf)
		} else {
			_, _ = c.ResponseWriter.Write(c.buf)
		}
		c.buf = nil
	}
}

// Close finishes the response: an undecided small body goes out as-is (with its true length),
// a compressed one gets its gzip trailer.
func (c *compressWriter) Close() {
	if !c.decided {
		if c.status == 0 && len(c.buf) == 0 {
			return
		}
		if len(c.buf) > 0 && c.ResponseWriter.Header().Get("Content-Length") == "" {
			c.ResponseWriter.Header().Set("Content-Length", strconv.Itoa(len(c.buf)))
		}
		c.decide(false)
		return
	}
	if c.compress && c.gz != nil {
		_ = c.gz.Close()
		c.gz.Reset(io.Discard)
		gzipPool.Put(c.gz)
		c.gz = nil
	}
}

// Flush supports streaming handlers: an undecided buffer is committed uncompressed first.
func (c *compressWriter) Flush() {
	if !c.decided {
		c.decide(false)
	}
	if c.compress && c.gz != nil {
		_ = c.gz.Flush()
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer (Hijack, deadlines).
func (c *compressWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }
