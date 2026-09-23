package postgres

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	countshttp "github.com/vgoats/goatos/backend/internal/counts/adapters/http"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// RECONCILE PARK FILTER through the REAL route -> service -> SQL (maintainer request
// 2026-09-15). The phone's Reconcile tab shows each card's park and a park filter on top; this
// drives GET /app/counts/pen-reconciliation/cards exactly as the phone does, as a CEO and as
// park-scoped people, and asserts the rows, the status-chip counts and the offered parks agree.
//
// Fixture note: both cards are raised by the real weighing raise in one park; the second card
// is then re-pointed at the other park with one UPDATE, because raising a card in a second
// park needs a second park's goats, campaign and bucket that this test is not about.

const penRecOtherPark = "00000000-0000-4000-8000-000000003002"

type penRecRouteWorkflowEngine struct{}

func (penRecRouteWorkflowEngine) OpenSubjectWorkflow(context.Context, string, string, string, string, time.Time, string, string) (string, error) {
	return "00000000-0000-4000-8000-00000000aa21", nil
}

type penRecRouteVerificationEnqueuer struct{}

func (penRecRouteVerificationEnqueuer) EnqueuePenReconciliationVerification(context.Context, countsapp.PenReconciliationVerificationEnqueueRequest) error {
	return nil
}

type penRecListBody struct {
	Items []struct {
		CardID   string `json:"card_id"`
		ParkID   string `json:"park_id"`
		ParkCode string `json:"park_code"`
	} `json:"items"`
	StatusCounts struct {
		All  int `json:"all"`
		Open int `json:"open"`
	} `json:"status_counts"`
	Filters struct {
		Parks []struct {
			ParkID   string `json:"park_id"`
			Label    string `json:"label"`
			Code     string `json:"code"`
			Selected bool   `json:"selected"`
		} `json:"parks"`
		SelectedParkID string `json:"selected_park_id"`
	} `json:"filters"`
}

func TestPenReconciliationParkFilterThroughTheRoutePg(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	// Two parks with their real short codes; ON CONFLICT keeps whatever the template holds, so
	// the codes are read back rather than assumed.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'Channapatna', 'active')
ON CONFLICT (location_id) DO NOTHING`, countsTenant, penRecOtherPark); err != nil {
		t.Fatalf("seed other park: %v", err)
	}
	codeOf := func(parkID string) string {
		var code string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(location_code, '') FROM locations WHERE location_id = $1::uuid`, parkID).Scan(&code); err != nil {
			t.Fatalf("read park code: %v", err)
		}
		return code
	}
	homeCode, otherCode := codeOf(countsPark), codeOf(penRecOtherPark)
	if homeCode == "" || otherCode == "" || homeCode == otherCode {
		t.Fatalf("fixture parks need distinct codes, got %q and %q", homeCode, otherCode)
	}

	seedPenRecGoatWithTag(t, ctx, pool, "00000000-0000-4000-8000-00000000f401", countsShedB, "whole", "1420 4001")
	seedPenRecGoatWithTag(t, ctx, pool, "00000000-0000-4000-8000-00000000f402", countsShedB, "whole", "1420 4002")
	bucket := "00000000-0000-4000-8000-00000000e401"
	seedPenRecBucket(t, ctx, pool, bucket, countsShedA, "", []string{"1420 4001", "1420 4002"})
	if raised := raisePenRec(t, ctx, repo, bucket); raised != 2 {
		t.Fatalf("raised = %d, want two cards", raised)
	}
	if _, err := pool.Exec(ctx, `
UPDATE pen_reconciliation_cards SET park_id = $2::uuid
WHERE tenant_id = $1::uuid AND scanned_identifier = '1420 4002'`, countsTenant, penRecOtherPark); err != nil {
		t.Fatalf("re-point second card: %v", err)
	}

	mux := http.NewServeMux()
	reconcileService := countsapp.NewPenReconciliationService(repo, nil).
		WithWorkflowEngine(penRecRouteWorkflowEngine{}).
		WithVerificationEnqueuer(penRecRouteVerificationEnqueuer{})
	countshttp.RegisterPenReconciliation(mux, countshttp.NewAppWriteHandler(nil, nil).
		WithPenReconciliationWorkflow(reconcileService))

	type caller func(context.Context) context.Context
	ceo := func(c context.Context) context.Context {
		return httpmiddleware.WithAuthGrants(c, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: countsTenant}})
	}
	personIn := func(parks ...string) caller {
		return func(c context.Context) context.Context {
			return httpmiddleware.WithPersonParkScope(c, httpmiddleware.PersonParkScope{ParkIDs: parks})
		}
	}
	withBaseContext := func(req *http.Request, who caller) *http.Request {
		ctx := httpmiddleware.WithTenantID(req.Context(), countsTenant)
		ctx = httpmiddleware.WithActorID(ctx, penRecOperator)
		return req.WithContext(who(ctx))
	}
	get := func(who caller, query string) (int, penRecListBody, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/app/counts/pen-reconciliation/cards"+query, nil)
		req = withBaseContext(req, who)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var body penRecListBody
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v body=%s", err, rec.Body.String())
			}
		}
		return rec.Code, body, rec.Body.String()
	}
	post := func(who caller, path, body, key string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req = withBaseContext(req, who)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	codesOf := func(b penRecListBody) []string {
		out := []string{}
		for _, item := range b.Items {
			out = append(out, item.ParkCode)
		}
		return out
	}
	cardForPark := func(parkID string) string {
		t.Helper()
		var cardID string
		if err := pool.QueryRow(ctx, `
SELECT card_id::text
FROM pen_reconciliation_cards
WHERE tenant_id = $1::uuid AND park_id = $2::uuid
ORDER BY scanned_identifier
LIMIT 1`, countsTenant, parkID).Scan(&cardID); err != nil {
			t.Fatalf("read card for park %s: %v", parkID, err)
		}
		return cardID
	}

	t.Run("CEO sees both parks, all cards, each card naming its park", func(t *testing.T) {
		code, body, raw := get(ceo, "")
		if code != http.StatusOK {
			t.Fatalf("status = %d body=%s", code, raw)
		}
		if len(body.Items) != 2 || body.StatusCounts.All != 2 {
			t.Fatalf("items=%d counts=%+v, want both cards", len(body.Items), body.StatusCounts)
		}
		seen := map[string]bool{}
		for _, c := range codesOf(body) {
			seen[c] = true
		}
		if !seen[homeCode] || !seen[otherCode] {
			t.Fatalf("card park codes = %v, want %s and %s", codesOf(body), homeCode, otherCode)
		}
		if body.Filters.SelectedParkID != "" {
			t.Fatalf("selected = %q, want none for a tenant-wide reader", body.Filters.SelectedParkID)
		}
		offered := map[string]bool{}
		for i, p := range body.Filters.Parks {
			offered[p.ParkID] = true
			if p.Selected {
				t.Fatalf("park %s selected with no park picked", p.Code)
			}
			if i > 0 && body.Filters.Parks[i-1].Code > p.Code {
				t.Fatalf("parks not in code order: %+v", body.Filters.Parks)
			}
		}
		if !offered[countsPark] || !offered[penRecOtherPark] {
			t.Fatalf("offered parks = %+v, want both", body.Filters.Parks)
		}
	})

	t.Run("CEO picking a park narrows rows AND chip counts, keeps every option", func(t *testing.T) {
		code, body, raw := get(ceo, "?park_id="+penRecOtherPark)
		if code != http.StatusOK {
			t.Fatalf("status = %d body=%s", code, raw)
		}
		if len(body.Items) != 1 || body.Items[0].ParkCode != otherCode || body.StatusCounts.All != 1 {
			t.Fatalf("items=%v counts=%+v, want only the %s card", codesOf(body), body.StatusCounts, otherCode)
		}
		if body.Filters.SelectedParkID != penRecOtherPark || len(body.Filters.Parks) < 2 {
			t.Fatalf("filters = %+v", body.Filters)
		}
	})

	t.Run("one-park person is clamped and pre-selected, never sees the other park", func(t *testing.T) {
		code, body, raw := get(personIn(penRecOtherPark), "")
		if code != http.StatusOK {
			t.Fatalf("status = %d body=%s", code, raw)
		}
		if len(body.Items) != 1 || body.Items[0].ParkCode != otherCode || body.StatusCounts.All != 1 {
			t.Fatalf("items=%v counts=%+v, want only their park's card", codesOf(body), body.StatusCounts)
		}
		if len(body.Filters.Parks) != 1 || body.Filters.Parks[0].ParkID != penRecOtherPark ||
			!body.Filters.Parks[0].Selected || body.Filters.SelectedParkID != penRecOtherPark {
			t.Fatalf("filters = %+v, want exactly their park, selected", body.Filters)
		}
	})

	t.Run("one-park person asking for another park is refused", func(t *testing.T) {
		if code, _, raw := get(personIn(penRecOtherPark), "?park_id="+countsPark); code != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", code, raw)
		}
	})

	t.Run("one-park person cannot open another park card workflow by card id", func(t *testing.T) {
		otherParkCard := cardForPark(countsPark)
		code, raw := post(personIn(penRecOtherPark),
			"/app/counts/pen-reconciliation/cards/"+otherParkCard+"/workflow", "", "workflow-park-denied-1")
		if code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", code, raw)
		}
		var workflowID string
		if err := pool.QueryRow(ctx, `
SELECT COALESCE(workflow_id::text, '')
FROM pen_reconciliation_cards
WHERE tenant_id = $1::uuid AND card_id = $2::uuid`, countsTenant, otherParkCard).Scan(&workflowID); err != nil {
			t.Fatalf("read workflow id: %v", err)
		}
		if workflowID != "" {
			t.Fatalf("unauthorized workflow open wrote workflow_id=%q", workflowID)
		}
	})

	t.Run("one-park person cannot complete another park card by card id", func(t *testing.T) {
		otherParkCard := cardForPark(countsPark)
		code, raw := post(personIn(penRecOtherPark),
			"/app/counts/pen-reconciliation/cards/"+otherParkCard+"/complete",
			`{"proof_ref":"proof-other-park-denied"}`, "complete-park-denied-1")
		if code != http.StatusNotFound {
			t.Fatalf("status = %d body=%s, want 404", code, raw)
		}
		var status string
		if err := pool.QueryRow(ctx, `
SELECT status
FROM pen_reconciliation_cards
WHERE tenant_id = $1::uuid AND card_id = $2::uuid`, countsTenant, otherParkCard).Scan(&status); err != nil {
			t.Fatalf("read status: %v", err)
		}
		if status != domain.PenReconciliationStatusOpen {
			t.Fatalf("unauthorized complete changed status to %q", status)
		}
	})

	t.Run("person covering two parks is offered both instead of an error", func(t *testing.T) {
		code, body, raw := get(personIn(countsPark, penRecOtherPark), "")
		if code != http.StatusOK {
			t.Fatalf("status = %d body=%s", code, raw)
		}
		if len(body.Items) != 2 || len(body.Filters.Parks) != 2 || body.Filters.SelectedParkID != "" {
			t.Fatalf("items=%v filters=%+v", codesOf(body), body.Filters)
		}
	})

	t.Run("a park nobody offered is a bad filter, never an empty page", func(t *testing.T) {
		if code, _, raw := get(ceo, "?park_id=not-a-park"); code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", code, raw)
		}
	})
}
