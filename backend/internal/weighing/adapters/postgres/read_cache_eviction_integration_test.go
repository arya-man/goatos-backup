package postgres

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// evictListener LISTENs on the read-cache channel on a dedicated connection and collects payloads.
type evictListener struct {
	t        *testing.T
	payloads chan string
}

func listenEvictions(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *evictListener {
	t.Helper()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "LISTEN "+readcache.NotifyChannel); err != nil {
		t.Fatal(err)
	}
	l := &evictListener{t: t, payloads: make(chan string, 64)}
	lctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			n, err := conn.Conn().WaitForNotification(lctx)
			if err != nil {
				return
			}
			l.payloads <- n.Payload
		}
	}()
	t.Cleanup(func() { cancel(); <-done; conn.Release() })
	return l
}

type evictPayloadT struct {
	TenantID string   `json:"tenant_id"`
	ParkIDs  []string `json:"park_ids"`
}

func (l *evictListener) next(timeout time.Duration) (evictPayloadT, bool) {
	select {
	case p := <-l.payloads:
		var out evictPayloadT
		if err := json.Unmarshal([]byte(p), &out); err != nil {
			l.t.Fatalf("payload %q: %v", p, err)
		}
		sort.Strings(out.ParkIDs)
		return out, true
	case <-time.After(timeout):
		return evictPayloadT{}, false
	}
}

func cachePut(t *testing.T, repo *Repository, parks []string, params string) {
	t.Helper()
	if _, err := readcache.Load(context.Background(), repo.cache, analyticsReadKey(repoTenant, parks, params), func(context.Context) (string, error) { return "cached", nil }); err != nil {
		t.Fatal(err)
	}
}

func cacheHas(repo *Repository, parks []string, params string) bool {
	v, _ := readcache.Load(context.Background(), repo.cache, analyticsReadKey(repoTenant, parks, params), func(context.Context) (string, error) { return "reloaded", nil })
	return v == "cached"
}

// Moving a campaign to another park changes BOTH parks' analytics: the old park loses the
// campaign's weighs and the new one gains them. The write must evict (locally and through the
// NOTIFY feed) the park it moved FROM as well as the park it moved TO.
func TestCampaignParkMoveEvictsTheOldAndTheNewPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	// No defer pool.Close(): StartPostgres closes the pool in t.Cleanup, AFTER the LISTEN
	// connection's own cleanup (registered later, run first) has released it.
	pool := pgtest.StartPostgres(t, ctx)
	seedWeighingObservationFixture(t, ctx, pool)
	const newPark = "00000000-0000-4000-8000-000000003002"
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status, display_order)
VALUES ($1::uuid, $2::uuid, 'park', 'CPT', 'active', 91) ON CONFLICT (location_id) DO NOTHING`, newPark, repoTenant)
	repo := NewRepository(pool, 5*time.Second)
	repo.cache.SetCoherent(true)
	cachePut(t, repo, []string{repoPark}, "old-park")
	cachePut(t, repo, []string{newPark}, "new-park")
	events := listenEvictions(t, ctx, pool)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	oldPark, err := repo.campaignParkTx(ctx, tx, repoTenant, repoCampaign)
	if err != nil || oldPark != repoPark {
		t.Fatalf("old park = %q, %v", oldPark, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE weighing_campaigns SET park_id = $3::uuid WHERE tenant_id = $1::uuid AND campaign_id = $2::uuid`, repoTenant, repoCampaign, newPark); err != nil {
		t.Fatal(err)
	}
	if err := repo.commitAndEvict(ctx, tx, repoTenant, repoCampaign, oldPark); err != nil {
		t.Fatal(err)
	}
	if cacheHas(repo, []string{repoPark}, "old-park") || cacheHas(repo, []string{newPark}, "new-park") {
		t.Fatal("a park move left one of the two parks' analytics cached")
	}
	got, ok := events.next(3 * time.Second)
	if !ok {
		t.Fatal("no eviction was published for sibling instances")
	}
	want := []string{repoPark, newPark}
	sort.Strings(want)
	if got.TenantID != repoTenant || strings.Join(got.ParkIDs, ",") != strings.Join(want, ",") {
		t.Fatalf("published eviction = %+v, want tenant %s parks %v", got, repoTenant, want)
	}
}

// UpdateCampaign itself reads the park BEFORE its UPDATE and hands it to the eviction.
func TestUpdateCampaignEvictsThePreUpdatePark(t *testing.T) {
	src := readSource(t, "repository.go")
	start := strings.Index(src, "func (r *Repository) UpdateCampaign(")
	body := src[start:]
	body = body[:strings.Index(body[1:], "\nfunc ")+1]
	read := strings.Index(body, "previousPark, err := r.campaignParkTx(ctx, tx, cmd.TenantID, campaignID)")
	update := strings.Index(body, "UPDATE weighing_campaigns")
	evict := strings.Index(body, "r.commitAndEvict(ctx, tx, cmd.TenantID, campaignID, previousPark)")
	if read < 0 || update < 0 || evict < 0 || !(read < update && update < evict) {
		t.Fatal("UpdateCampaign must read the campaign's park before the UPDATE and evict it with the new park")
	}
}

// Kernel cadence claims (carry-over closes buckets, roll-forward moves due dates) change what the
// Weights reads show, so they publish a tenant+park eviction like every other weighing write.
func TestKernelClaimsPublishTenantAndParkEviction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	// No defer pool.Close(): StartPostgres closes the pool in t.Cleanup, AFTER the LISTEN
	// connection's own cleanup (registered later, run first) has released it.
	pool := pgtest.StartPostgres(t, ctx)
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:evict")
	repo.cache.SetCoherent(true)
	cachePut(t, repo, []string{repoPark}, "before-sweep")
	events := listenEvictions(t, ctx, pool)

	if _, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: businessInstant(t, kernelLaterDate)}); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if cacheHas(repo, []string{repoPark}, "before-sweep") {
		t.Fatal("a kernel claim left the park's analytics cached")
	}
	got, ok := events.next(3 * time.Second)
	if !ok {
		t.Fatal("the kernel claim published no eviction")
	}
	if got.TenantID != repoTenant || strings.Join(got.ParkIDs, ",") != repoPark {
		t.Fatalf("kernel eviction = %+v, want tenant %s park %s", got, repoTenant, repoPark)
	}
}
