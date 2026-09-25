package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

const countsParkB = "00000000-0000-4000-8000-000000003002"

// seedApprovalFilterFixture raises, in park A: 3 births, 1 death (its park comes from the goat),
// and in a second park B: 2 births. Returns nothing; every row is written by the repository.
func seedApprovalFilterFixture(t *testing.T, ctx context.Context, repo *Repository) string {
	t.Helper()
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'Second Farm', 'active') ON CONFLICT (location_id) DO NOTHING`, countsParkB, countsTenant); err != nil {
		t.Fatalf("seed park B: %v", err)
	}
	base := time.Now().Add(-time.Hour)
	birth := func(i int, park string) {
		t.Helper()
		sub := birthSubmission(fmt.Sprintf("filter-birth-%d", i))
		sub.Payload = json.RawMessage(`{"species":"goat","sex":"female","dob":"2026-07-01","origin_type":"birth","park_id":"` + park + `"}`)
		sub.RaisedAt = base.Add(time.Duration(i) * time.Minute)
		if _, _, err := repo.CreateApprovalRequest(ctx, sub); err != nil {
			t.Fatalf("submit birth %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		birth(i, countsPark)
	}
	birth(3, countsParkB)
	birth(4, countsParkB)
	goatID := "00000000-0000-4000-8000-00000000f801"
	seedApprovalGoat(t, ctx, repo.pool, goatID, countsShedA)
	if _, err := repo.pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
                              scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', '982000111222333', '982000111222333', 'global', true, 'active', now(), 'test')`,
		countsTenant, goatID); err != nil {
		t.Fatalf("seed tag: %v", err)
	}
	if _, _, err := repo.CreateApprovalRequest(ctx, domain.ApprovalRequestSubmission{
		TenantID: countsTenant, RequestType: domain.ApprovalRequestTypeDeath,
		Payload:       json.RawMessage(`{"goat_id":"` + goatID + `","lifecycle_status":"dead","exit_reason":"died","reason":"Found dead"}`),
		SubjectGoatID: &goatID, RaisedByUserID: countsOperator, RaisedAt: base.Add(10 * time.Minute),
		IdempotencyKey: "filter-death-1", RequestFingerprint: "filter-death-1-fp",
	}); err != nil {
		t.Fatalf("submit death: %v", err)
	}
	return goatID
}

// TestApprovalListFiltersAreServerSideAndCursorBound (2026-09-25): the web filtered type and farm
// client-side over ONE 20-row page, so a farm's requests past it were unreachable. request_type and
// park_id are now SQL predicates; a death matches a farm through its animal's park; the keyset
// cursor carries the filter and refuses to walk another one; unknown values are refused.
func TestApprovalListFiltersAreServerSideAndCursorBound(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
	seedApprovalFilterFixture(t, ctx, repo)
	svc := countsapp.NewApprovalService(repo, nil, nil)
	all := []string{domain.ApprovalRequestTypeBirth, domain.ApprovalRequestTypeDeath, domain.ApprovalRequestTypeShifting}

	list := func(filter domain.ApprovalListFilter, pageSize int, cursor string, types []string, parks []string) (domain.ApprovalRequestPage, error) {
		return svc.ListFiltered(ctx, countsTenant, domain.ApprovalStatusPending, types, parks, filter, pageSize, cursor)
	}
	walk := func(filter domain.ApprovalListFilter, types, parks []string) []domain.ApprovalRequestSummary {
		t.Helper()
		var out []domain.ApprovalRequestSummary
		cursor := ""
		for guard := 0; guard < 10; guard++ {
			page, err := list(filter, 2, cursor, types, parks)
			if err != nil {
				t.Fatalf("list %+v: %v", filter, err)
			}
			out = append(out, page.Items...)
			if page.NextCursor == "" {
				return out
			}
			cursor = page.NextCursor
		}
		t.Fatal("pager did not terminate")
		return nil
	}

	if got := walk(domain.ApprovalListFilter{ParkID: countsPark}, all, nil); len(got) != 4 {
		t.Fatalf("park A (3 births + a death through its animal's park) = %d rows, want 4", len(got))
	}
	if got := walk(domain.ApprovalListFilter{ParkID: countsParkB}, all, nil); len(got) != 2 {
		t.Fatalf("park B = %d rows, want 2", len(got))
	}
	deaths := walk(domain.ApprovalListFilter{RequestType: domain.ApprovalRequestTypeDeath}, all, nil)
	if len(deaths) != 1 || deaths[0].RequestType != domain.ApprovalRequestTypeDeath {
		t.Fatalf("type=death = %+v, want the one death", deaths)
	}
	if got := walk(domain.ApprovalListFilter{RequestType: domain.ApprovalRequestTypeBirth, ParkID: countsParkB}, all, nil); len(got) != 2 {
		t.Fatalf("type=birth park=B = %d, want 2", len(got))
	}
	// Never widened: a type the caller may not decide, or a farm outside their scope, reads empty.
	if got := walk(domain.ApprovalListFilter{RequestType: domain.ApprovalRequestTypeBirth}, []string{domain.ApprovalRequestTypeShifting}, nil); len(got) != 0 {
		t.Fatalf("filtering to a type the caller cannot decide listed %d rows, want 0", len(got))
	}
	if got := walk(domain.ApprovalListFilter{ParkID: countsParkB}, all, []string{countsPark}); len(got) != 0 {
		t.Fatalf("filtering to a farm outside the caller's scope listed %d rows, want 0", len(got))
	}

	// The cursor is bound to its filter.
	first, err := list(domain.ApprovalListFilter{ParkID: countsPark}, 2, "", all, nil)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first park-A page: next=%q err=%v", first.NextCursor, err)
	}
	if _, err := list(domain.ApprovalListFilter{ParkID: countsParkB}, 2, first.NextCursor, all, nil); !errors.Is(err, countsapp.ErrApprovalCursorFilterMismatch) {
		t.Fatalf("park-A cursor on the park-B list: err=%v, want ErrApprovalCursorFilterMismatch", err)
	}
	if _, err := list(domain.ApprovalListFilter{}, 2, first.NextCursor, all, nil); !errors.Is(err, countsapp.ErrApprovalCursorFilterMismatch) {
		t.Fatalf("filtered cursor on the unfiltered list: err=%v, want a mismatch", err)
	}

	// Unknown values are refused, never widened.
	if _, err := list(domain.ApprovalListFilter{RequestType: "weighing"}, 2, "", all, nil); !errors.Is(err, countsapp.ErrInvalidApprovalTypeFilter) {
		t.Fatalf("unknown request_type: err=%v", err)
	}
	if _, err := list(domain.ApprovalListFilter{ParkID: "CBE"}, 2, "", all, nil); !errors.Is(err, countsapp.ErrInvalidApprovalParkFilter) {
		t.Fatalf("malformed park_id: err=%v", err)
	}
}

// TestApprovalsBadgeCountsWhatTheQueueLists: the phone's Approvals badge is the whole-queue count
// of pending requests the caller may decide, and it must equal the rows the queue lists under the
// same decidable types and park scope -- for a tenant-wide approver, a park-scoped one, a
// lifecycle-only one, and a non-approver (0).
func TestApprovalsBadgeCountsWhatTheQueueLists(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
	seedApprovalFilterFixture(t, ctx, repo)
	svc := countsapp.NewApprovalService(repo, nil, nil)

	for _, c := range []struct {
		name  string
		types []string
		parks []string
		want  int
	}{
		{"tenant-wide approver", []string{"birth", "death", "shifting"}, nil, 6},
		{"park A approver", []string{"birth", "death", "shifting"}, []string{countsPark}, 4},
		{"shifting-only approver", []string{"shifting"}, nil, 0},
		{"non-approver", nil, nil, 0},
	} {
		n, err := svc.CountPending(ctx, countsTenant, c.types, c.parks)
		if err != nil {
			t.Fatalf("%s: count: %v", c.name, err)
		}
		listed := 0
		cursor := ""
		for guard := 0; guard < 10; guard++ {
			page, err := svc.ListPending(ctx, countsTenant, domain.ApprovalStatusPending, c.types, c.parks, 2, cursor)
			if err != nil {
				t.Fatalf("%s: list: %v", c.name, err)
			}
			listed += len(page.Items)
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		if n != listed || n != c.want {
			t.Fatalf("%s: badge=%d queue lists=%d, want both %d", c.name, n, listed, c.want)
		}
	}
}

// TestApprovalNamesResolveTheDeathAnimalsTag: the death row names WHICH animal by its RFID tag,
// resolved in the same batched read as its location.
func TestApprovalNamesResolveTheDeathAnimalsTag(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := newApprovalRepo(t, pool, &fakeIdentityTx{})
	goatID := seedApprovalFilterFixture(t, ctx, repo)
	names, err := repo.ResolveApprovalNames(ctx, countsTenant, []string{countsParkB}, nil, []string{goatID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if names.AnimalTags[goatID] != "982000111222333" {
		t.Fatalf("tag = %q, want the animal's animal_identifier_1", names.AnimalTags[goatID])
	}
	if names.Locations[countsParkB] == "" {
		t.Fatalf("the farm named on a shifting line must resolve to its name; got none")
	}
}
