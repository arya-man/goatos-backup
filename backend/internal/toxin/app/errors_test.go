package app

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

func TestHTTPErrorMapsInvalidArgumentToBadCursor(t *testing.T) {
	got := HTTPError(fmt.Errorf("decode cursor: %w", ports.ErrInvalidArgument))
	if got.HTTPStatus != http.StatusBadRequest || got.Code != "invalid_cursor" {
		t.Fatalf("HTTPError(invalid argument) = %d/%s, want 400/invalid_cursor", got.HTTPStatus, got.Code)
	}
}
