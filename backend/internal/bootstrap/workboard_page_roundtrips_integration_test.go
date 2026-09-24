package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	processintegritypg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	workboardhttp "github.com/vgoats/goatos/backend/internal/workboard/adapters/http"
	workboardapp "github.com/vgoats/goatos/backend/internal/workboard/app"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
)

// P10, the whole /work-board/page request: auth (allowlist, person access, grants), the
// summary, the vaccination process-integrity source and the four lane reads, counted at the
// connection. Run 3b measured 19 statements a request on real stg, each paying the ~20 ms
// laptop-to-Mumbai RTT; the page must now need at most wbMaxPageRoundTrips round trips with
// the auth cache warm, and serve byte-identical JSON to the per-source path.
const wbMaxPageRoundTrips = 4

const (
	wbTenant   = "00000000-0000-4000-8000-00000000e001"
	wbParty    = "00000000-0000-4000-8000-00000000e101"
	wbPark     = "00000000-0000-4000-8000-00000000e301"
	wbOtherPk  = "00000000-0000-4000-8000-00000000e302"
	wbShed     = "00000000-0000-4000-8000-00000000e311"
	wbOperator = "00000000-0000-4000-8000-00000000e501" // user id: the owner lens
	wbDirector = "00000000-0000-4000-8000-00000000e502" // user id: the caller
	wbMember   = "00000000-0000-4000-8000-00000000e401"
	wbDirMem   = "00000000-0000-4000-8000-00000000e402"
	wbProtocol = "00000000-0000-4000-8000-00000000e801"
	wbVersion  = "00000000-0000-4000-8000-00000000e802"
	wbRule     = "00000000-0000-4000-8000-00000000e803"
	wbBatch    = "00000000-0000-4000-8000-00000000e901"
	wbAssign   = "00000000-0000-4000-8000-00000000e911"
	wbDate     = "2026-09-10"
	wbEmail    = "director@mesha.sg"
)

func wbExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed: %v\n%s", err, sql)
	}
}

// seedWorkBoardPage lays down a real vaccination drive on one pen (operated by wbOperator, in
// every per-animal state), approvals raised by the same operator, and a tenant-wide director
// caller allowed through the DB-backed email allowlist.
func seedWorkBoardPage(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	wbExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Board Page Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, wbTenant)
	wbExec(t, ctx, pool, `INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'org', 'Board Custodian', 'active') ON CONFLICT DO NOTHING`, wbParty)
	wbExec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'CBE', 'Coimbatore', 'active'),
       ($3::uuid, $2::uuid, 'park', 'CPT', 'Channapatna', 'active'),
       ($4::uuid, $2::uuid, 'shed', 'GODEL1', 'Godel 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, wbPark, wbTenant, wbOtherPk, wbShed)
	wbExec(t, ctx, pool, `UPDATE locations SET parent_location_id = $1::uuid WHERE location_id = $2::uuid`, wbPark, wbShed)
	wbExec(t, ctx, pool, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'DIN', 'Dinakar', 'active', 'operator', $4::uuid),
       ($5::uuid, $2::uuid, $6::uuid, 'DIR', 'Director', 'active', 'park_head', $4::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, wbMember, wbTenant, wbOperator, wbPark, wbDirMem, wbDirector)
	wbExec(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'ceo_internal', 'tenant', $1::uuid, 'active', now() - interval '1 day')`, wbTenant, wbDirector)
	wbExec(t, ctx, pool, `
INSERT INTO auth_allowed_emails (tenant_id, email, normalized_email, status, source)
VALUES ($1::uuid, $2, $2, 'active', 'workboard_page_test')`, wbTenant, wbEmail)

	// Vaccination: a published protocol, one batched drive on Godel 1 operated by wbOperator.
	wbExec(t, ctx, pool, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.board', 'Preventive Care Vaccination Matrix', 'vaccination', 'active')`, wbProtocol, wbTenant)
	wbExec(t, ctx, pool, `
INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy, published_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'tenant', 1, 'draft', DATE '2026-06-01', '{}'::jsonb, '{}'::jsonb, NULL)`, wbVersion, wbTenant, wbProtocol)
	wbExec(t, ctx, pool, `
INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, eligibility_json, proof_policy)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'et_tt_adult_w1', 1, 'birth_age', '{}'::jsonb, '{}'::jsonb)`, wbRule, wbTenant, wbVersion)
	wbExec(t, ctx, pool, `UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1::uuid`, wbVersion)
	wbExec(t, ctx, pool, `
INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4::uuid, 'in_progress', $5::date)`, wbBatch, wbTenant, wbVersion, wbShed, wbDate)
	wbExec(t, ctx, pool, `
INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::date, $5::uuid, $6::uuid, $7::uuid, 'Godel 1', 'whole', 6)`, wbAssign, wbTenant, wbBatch, wbDate, wbMember, wbPark, wbShed)
	goats := []struct{ status, completion, partition string }{
		{"completed", "accepted", ""}, {"in_progress", "recorded", ""}, {"in_progress", "rejected", ""},
		{"scheduled", "", ""}, {"missed", "", ""}, {"deferred", "", ""}, {"scheduled", "", "Part 3"},
	}
	for i, g := range goats {
		goat := fmt.Sprintf("00000000-0000-4000-8000-00000000e6%02d", i+1)
		obligation := fmt.Sprintf("00000000-0000-4000-8000-00000000e7%02d", i+1)
		wbExec(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, custodian_party_id, park_id, shed_id, current_location_id, origin_type, dob, entry_date, management_stage)
VALUES ($1::uuid, $2::uuid, $3, 'goat', 'female', 'alive', $4::uuid, $5::uuid, $6::uuid, $6::uuid, 'procured', DATE '2024-01-01', DATE '2024-01-01', 'adult')`,
			goat, wbTenant, fmt.Sprintf("G-77%04d", i+1), wbParty, wbPark, wbShed)
		if g.partition != "" {
			wbExec(t, ctx, pool, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'Godel 1 - ' || $4)`, wbTenant, goat, wbShed, g.partition)
		}
		wbExec(t, ctx, pool, `
INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'goat', $6::uuid, 'shed', $7::uuid, ($8 || ' 00:00:00+05:30')::timestamptz, $9, 'wb-page-obl-' || $1, $10)`,
			obligation, wbTenant, wbVersion, wbRule, wbBatch, goat, wbShed, wbDate, g.status, i+1)
		if g.partition == "" {
			wbExec(t, ctx, pool, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)`, wbTenant, wbAssign, obligation, goat)
		}
		if g.completion != "" {
			wbExec(t, ctx, pool, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, batch_id, goat_id, administered_at, status, rejection_reason, verified_at, idempotency_key, recorded_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, ($5 || ' 10:00:00+05:30')::timestamptz, $6,
        CASE WHEN $6 = 'rejected' THEN 'Wrong animal in frame' END,
        CASE WHEN $6 IN ('accepted', 'rejected') THEN now() END, 'wb-page-comp-' || $2, $7::uuid)`,
				wbTenant, obligation, wbBatch, goat, wbDate, g.completion, wbOperator)
		}
	}

	// Approvals raised by the operator: the owner lens keeps what they raised.
	birth := `{"park_id":"` + wbPark + `","shed_id":"` + wbShed + `","litter_size":1,"children":[{"child_ordinal":1}]}`
	for i, status := range []string{"pending", "pending", "rejected"} {
		wbExec(t, ctx, pool, `
INSERT INTO counts_approval_requests (tenant_id, request_type, payload, status, raised_by_user_id, raised_at,
  decided_by_user_id, decided_at, decision_reason, idempotency_key, request_fingerprint)
VALUES ($1::uuid, 'birth', $2::jsonb, $3, $4::uuid, ($5 || ' 11:00:00')::timestamp AT TIME ZONE 'Asia/Kolkata',
  CASE WHEN $3 = 'pending' THEN NULL ELSE $6::uuid END,
  CASE WHEN $3 = 'pending' THEN NULL ELSE ($5 || ' 12:00:00')::timestamp AT TIME ZONE 'Asia/Kolkata' END,
  CASE WHEN $3 = 'rejected' THEN 'not this pen' END,
  'wb-page-appr-' || $7, 'fp-wb-page-appr-' || $7)`,
			wbTenant, birth, status, wbOperator, wbDate, wbDirector, fmt.Sprint(i))
	}
}

type wbStaticVerifier struct{ claims platformauth.Claims }

func (v wbStaticVerifier) Verify(string) (platformauth.Claims, error) { return v.claims, nil }

// workBoardPageHandler serves /work-board/page behind the production auth middleware and its
// read cache, every read on `pool`.
func workBoardPageHandler(t *testing.T, pool *pgxpool.Pool, svc *workboardapp.Service) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	const timeout = 15 * time.Second
	verified := true
	verifier := wbStaticVerifier{claims: platformauth.Claims{
		Subject: wbDirector, TenantID: wbTenant, Issuer: "goatos-test", Audience: "goatos-test",
		Email: wbEmail, EmailVerified: &verified, Expires: time.Now().Add(time.Hour),
	}}
	cache := newAuthReadCache()
	authz, err := buildAuthMiddleware(AuthConfig{Mode: "bearer", Environment: "test", AllowedEmails: []string{"someone-else@mesha.sg"}},
		verifier, nil, cachedGrantSource{inner: permissionspg.NewGrantSource(pool, timeout), cache: cache},
		permissionspg.NewAllowedEmailSource(pool, timeout, log), log)
	if err != nil {
		t.Fatal(err)
	}
	authz.SetPersonAccessSource(cachedPersonAccess{inner: workforcepg.NewAccessRepository(pool), cache: cache})
	mux := http.NewServeMux()
	workboardhttp.Register(mux, workboardhttp.NewHandler(svc, log))
	return httpmiddleware.RequestContext(log)(authz.Wrap(mux))
}

func wbPageURL(owner string) string {
	u := "/work-board/page?park=" + wbPark + "&business_date=" + wbDate + "&limit=25&page_lane=todo%2Cin_progress%2Cin_review%2Cdone"
	if owner != "" {
		u += "&owner=" + owner
	}
	return u
}

func wbGet(t *testing.T, h http.Handler, url string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer test")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func TestWorkBoardPageRoundTripsIncludingAuth(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedWorkBoardPage(t, ctx, pool)
	counted, trips := pgtest.CountingPool(t, ctx, pool)
	const timeout = 15 * time.Second
	piRepo := processintegritypg.NewRepository(counted, timeout)
	// The reference: every source read on its own, no batching -- the per-source path whose
	// output the batched page must reproduce exactly.
	reference := workBoardPageHandler(t, counted, workboardapp.NewService(newWorkBoardSources(counted, timeout, piRepo)...))
	batched := workBoardPageHandler(t, counted, newWorkBoardService(counted, timeout, piRepo))

	for _, owner := range []string{wbOperator, ""} {
		url := wbPageURL(owner)
		refBody := wbGet(t, reference, url)
		trips.Reset()
		cold := wbGet(t, batched, url)
		coldTrips, coldSQL := trips.Trips(), trips.Trace()
		trips.Reset()
		warm := wbGet(t, batched, url) // auth read cache warm, as for every call after the first in a burst
		warmTrips, warmStatements, warmSQL := trips.Trips(), trips.Statements(), trips.Trace()
		t.Logf("owner=%q: cold-auth round trips %d, warm round trips %d (%d statements)\ncold:\n  %s\nwarm:\n  %s",
			owner, coldTrips, warmTrips, warmStatements, strings.Join(coldSQL, "\n  "), strings.Join(warmSQL, "\n  "))
		var probe struct {
			Lanes map[string]struct {
				Rows []json.RawMessage `json:"rows"`
			} `json:"lanes"`
		}
		if err := json.Unmarshal(warm, &probe); err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, lane := range probe.Lanes {
			n += len(lane.Rows)
		}
		if n < 2 {
			t.Fatalf("owner=%q: page served %d rows; the seed must reach the lanes to prove equivalence:\n%s", owner, n, warm)
		}
		if !bytes.Equal(refBody, warm) || !bytes.Equal(refBody, cold) {
			t.Fatalf("owner=%q: batched page differs from the per-source page:\nref=%s\nnew=%s", owner, refBody, warm)
		}
		if warmTrips > wbMaxPageRoundTrips {
			t.Errorf("owner=%q: /work-board/page took %d round trips, want <= %d", owner, warmTrips, wbMaxPageRoundTrips)
		}
	}
}
