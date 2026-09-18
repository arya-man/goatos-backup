package http

import (
	"log/slog"
	"net/http"
	"testing"
)

func TestBulkRoutesMountWithoutConflict(t *testing.T) {
	mux := http.NewServeMux()
	h := NewHandler(nil, slog.Default())
	Register(mux, h)
	RegisterBulk(mux, h)
}
