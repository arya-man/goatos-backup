package httpmiddleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, accept string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/x", nil)
	if accept != "" {
		req.Header.Set("Accept-Encoding", accept)
	}
	rec := httptest.NewRecorder()
	Compress(h).ServeHTTP(rec, req)
	return rec
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	z, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(z)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	return out
}

func TestCompressGzipsLargeJSONForAcceptingClients(t *testing.T) {
	body := `{"items":"` + strings.Repeat("x", 4096) + `"}`
	rec := serve(t, "gzip, deflate, br", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", `W/"rev"`)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(body))
	})
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Content-Length") != "" {
		t.Fatalf("Content-Length must be dropped, got %q", rec.Header().Get("Content-Length"))
	}
	if !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Accept-Encoding") {
		t.Fatalf("Vary = %v", rec.Header().Values("Vary"))
	}
	if rec.Header().Get("ETag") != `W/"rev"` {
		t.Fatalf("ETag must survive: %q", rec.Header().Get("ETag"))
	}
	if rec.Body.Len() >= len(body) {
		t.Fatalf("compressed body %d not smaller than %d", rec.Body.Len(), len(body))
	}
	if got := string(gunzip(t, rec.Body.Bytes())); got != body {
		t.Fatalf("round trip mismatch: %d bytes", len(got))
	}
}

func TestCompressPassesThroughSmallNonJSONAndRefusingClients(t *testing.T) {
	small := `{"ok":true}`
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(small))
	})
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != small || rec.Code != 200 {
		t.Fatalf("small body must pass through: enc=%q body=%q code=%d", rec.Header().Get("Content-Encoding"), rec.Body.String(), rec.Code)
	}
	if rec.Header().Get("Content-Length") != "11" {
		t.Fatalf("small body Content-Length = %q", rec.Header().Get("Content-Length"))
	}

	big := strings.Repeat("y", 4096)
	rec = serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		_, _ = w.Write([]byte(big))
	})
	if rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != big {
		t.Fatalf("non-JSON must pass through")
	}

	rec = serve(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a":"` + big + `"}`))
	})
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("no Accept-Encoding must pass through")
	}
	rec = serve(t, "gzip;q=0, identity", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"a":"` + big + `"}`))
	})
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("gzip;q=0 must pass through")
	}
}

func TestCompressLeavesBodilessAndPreEncodedResponsesAlone(t *testing.T) {
	rec := serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `W/"rev"`)
		w.WriteHeader(http.StatusNotModified)
	})
	if rec.Code != 304 || rec.Header().Get("Content-Encoding") != "" || rec.Body.Len() != 0 {
		t.Fatalf("304 altered: code=%d enc=%q len=%d", rec.Code, rec.Header().Get("Content-Encoding"), rec.Body.Len())
	}
	rec = serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "br")
		_, _ = w.Write(bytes.Repeat([]byte("z"), 4096))
	})
	if rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("pre-encoded response must not be re-encoded: %q", rec.Header().Get("Content-Encoding"))
	}
	// A status with a body but written in several small chunks is compressed once the threshold
	// is crossed, and the status code is preserved.
	rec = serve(t, "gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		for i := 0; i < 50; i++ {
			_, _ = w.Write([]byte(`{"chunk":"` + strings.Repeat("c", 100) + `"}`))
		}
	})
	if rec.Code != 201 || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("chunked JSON: code=%d enc=%q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if len(gunzip(t, rec.Body.Bytes())) != 50*112 {
		t.Fatalf("chunked round trip length mismatch")
	}
}
