package http

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
)

func TestBulkRoutesMountWithoutConflict(t *testing.T) {
	mux := http.NewServeMux()
	h := NewHandler(nil, slog.Default())
	Register(mux, h)
	RegisterBulk(mux, h)
}

// The workbook routes live at the literal /admin/configuration/workbook/*, beside the
// {register} pattern; a register keyed "workbook" would be shadowed by them.
func TestNoRegisterIsKeyedWorkbook(t *testing.T) {
	if _, ok := domain.RegisterByKey("workbook"); ok {
		t.Fatal("a register is keyed 'workbook'; the workbook routes would shadow it")
	}
	if got := domain.MatchSheetName("workbook", domain.Registers); got != "" {
		t.Fatalf("a tab named workbook matches register %q", got)
	}
}
