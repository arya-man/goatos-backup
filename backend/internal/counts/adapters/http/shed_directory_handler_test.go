package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// shedDirectoryService serves a fixed 77-row catalog, the size the live tenant actually has.
type shedDirectoryService struct{ HerdRegisterService }

func (s shedDirectoryService) GetShedDirectory(context.Context, string) (domain.ShedDirectory, error) {
	items := make([]domain.ShedDirectoryRow, 0, 77)
	for i := 0; i < 77; i++ {
		items = append(items, domain.ShedDirectoryRow{
			ShedName:                   fmt.Sprintf("Shed %02d", i),
			OperationalLocationDisplay: fmt.Sprintf("Shed %02d", i),
			Cells:                      map[string]domain.ShedDirectoryCell{},
		})
	}
	return domain.ShedDirectory{Items: items, TotalRows: int64(len(items))}, nil
}

func getShedDirectory(t *testing.T, query string) domain.ShedDirectory {
	t.Helper()
	handler := NewHandler(shedDirectoryService{}, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/counts/sheds"+query, nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "tenant-1"))
	rec := httptest.NewRecorder()

	handler.GetShedDirectory(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var got domain.ShedDirectory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// TestShedDirectoryPageBoundaryKeepsTheWholeCatalogTotal is the summary-vs-page rule: `items` is a
// display window, `total_rows` is the whole catalog, and no page may shrink the total to its own
// length. A pager reading the page length would tell an operator the farm has 25 pens.
//
// It also covers both real boundaries: the last partial page, and an offset past the end (which a
// pager can legitimately reach if the catalog shrinks between two requests) returning an EMPTY page
// rather than an error.
func TestShedDirectoryPageBoundaryKeepsTheWholeCatalogTotal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		query     string
		wantItems int
	}{
		{name: "default page", query: "", wantItems: 25},
		{name: "explicit first page", query: "?limit=25&offset=0", wantItems: 25},
		{name: "last partial page", query: "?limit=25&offset=75", wantItems: 2},
		{name: "offset exactly at the end", query: "?limit=25&offset=77", wantItems: 0},
		{name: "offset past the end", query: "?limit=25&offset=500", wantItems: 0},
		{name: "page larger than the catalog", query: "?limit=100", wantItems: 77},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := getShedDirectory(t, tc.query)
			if len(got.Items) != tc.wantItems {
				t.Fatalf("want %d items, got %d", tc.wantItems, len(got.Items))
			}
			if got.TotalRows != 77 {
				t.Fatalf("total_rows must stay the WHOLE catalog, got %d", got.TotalRows)
			}
		})
	}
}

// TestShedDirectoryPageBoundaryRejectsAnInvalidPagingValue: a present-but-invalid value is a 400,
// never silently rewritten to a default the caller never asked for -- a caller that sent limit=0
// must not receive 25 rows believing it asked for them.
func TestShedDirectoryPageBoundaryRejectsAnInvalidPagingValue(t *testing.T) {
	handler := NewHandler(shedDirectoryService{}, slog.Default())
	for _, query := range []string{"?limit=abc", "?limit=0", "?limit=101", "?offset=-1", "?offset=nope"} {
		req := httptest.NewRequest(http.MethodGet, "/counts/sheds"+query, nil)
		req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "tenant-1"))
		rec := httptest.NewRecorder()

		handler.GetShedDirectory(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", query, rec.Code)
		}
	}
}
