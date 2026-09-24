// Package liveload holds the opt-in Herd Signals live-read load harness. It is not part of the
// default suite: it needs GOATOS_RUN_POSTGRES_TESTS=1 (pgtest) AND GOATOS_HERD_LIVE_LOADTEST=1.
package liveload

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	herdhttp "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/http"
	herdpg "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	herdapp "github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	loadTenant = "00000000-0000-4000-8000-0000000000aa"
	loadActor  = "10000000-0000-4000-8000-0000000000aa"
)

type countingTracer struct {
	n     atomic.Int64
	mu    sync.Mutex
	stats map[string]*qstat
}

type qstat struct {
	n          int
	total, max time.Duration
}

type traceKey struct{}

type traceVal struct {
	sql   string
	start time.Time
}

func (c *countingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return context.WithValue(ctx, traceKey{}, traceVal{sql: d.SQL, start: time.Now()})
}

func (c *countingTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	v, ok := ctx.Value(traceKey{}).(traceVal)
	if !ok {
		return
	}
	key := strings.Join(strings.Fields(v.sql), " ")
	if len(key) > 90 {
		key = key[:90]
	}
	el := time.Since(v.start)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stats == nil {
		c.stats = map[string]*qstat{}
	}
	st := c.stats[key]
	if st == nil {
		st = &qstat{}
		c.stats[key] = st
	}
	st.n++
	st.total += el
	if el > st.max {
		st.max = el
	}
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// TestHerdSignalsLiveLoad: N mapped tags over P pens with 24h of hourly 300s-tier windows;
// V SSE viewers on /herd-signals/live/stream; an ingest-shaped UPDATE + pg_notify every 2s for D
// seconds; a sampler hitting GET /herd-signals/live (limit=1 summary and limit=25 page, plus a
// risk_state=attention page) every 250ms. Reports app-pool query count and p50/p95.
func TestHerdSignalsLiveLoad(t *testing.T) {
	if os.Getenv("GOATOS_HERD_LIVE_LOADTEST") != "1" {
		t.Skip("set GOATOS_HERD_LIVE_LOADTEST=1 to run the live load harness")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	admin := pgtest.StartPostgres(t, ctx)
	tags := envInt("LOAD_TAGS", 5000)
	pens := envInt("LOAD_PENS", 50)
	viewers := envInt("LOAD_VIEWERS", 5)
	duration := time.Duration(envInt("LOAD_SECONDS", 60)) * time.Second
	seed(t, ctx, admin, tags, pens)

	if os.Getenv("LOAD_EXPLAIN") == "1" {
		explainLive(t, ctx, admin)
	}
	cfg := admin.Config().Copy()
	cfg.MaxConns = 10 // db-g1-small-sized app pool
	tracer := &countingTracer{}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	svc := herdapp.NewService(herdpg.NewRepository(pool))
	h := herdhttp.NewHandler(svc).WithLiveNotifications(runCtx, herdpg.NewLiveNotificationSource(pool, nil))
	mux := http.NewServeMux()
	herdhttp.Register(mux, h)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := httpmiddleware.WithTenantID(r.Context(), loadTenant)
		c = httpmiddleware.WithActorID(c, loadActor)
		mux.ServeHTTP(w, r.WithContext(c))
	}))
	defer srv.Close()
	time.Sleep(500 * time.Millisecond) // let LISTEN attach

	var snapshots atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < viewers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(runCtx, "GET", srv.URL+"/herd-signals/live/stream?limit=25", nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 1<<20), 64<<20)
			for sc.Scan() {
				if sc.Text() == "event: snapshot" {
					snapshots.Add(1)
				}
			}
		}()
	}

	lat := map[string][]time.Duration{}
	var latMu sync.Mutex
	wg.Add(2)
	go func() { // ingest-shaped writer on the admin pool (not counted)
		defer wg.Done()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		i := 0
		for {
			select {
			case <-runCtx.Done():
				return
			case <-tick.C:
			}
			i++
			_, _ = admin.Exec(ctx, `UPDATE herd_signal_tag_latest SET last_seen_at = now(), motion_delta = motion_delta + 1, updated_at = now()
				WHERE tenant_id = $1 AND tag_id = ANY(SELECT 'T' || lpad(g::text, 6, '0') FROM generate_series($2::int, $2::int + 199) g)`, loadTenant, (i*200)%tags)
			_, _ = admin.Exec(ctx, `SELECT pg_notify('herd_signals_live', json_build_object('tenant_id', $1::text)::text)`, loadTenant)
		}
	}()
	go func() {
		defer wg.Done()
		urls := map[string]string{
			"summary_limit1": "/herd-signals/live?limit=1",
			"page_limit25":   "/herd-signals/live?limit=25",
			"risk_attention": "/herd-signals/live?limit=25&risk_state=attention",
		}
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			for name, u := range urls {
				select {
				case <-runCtx.Done():
					return
				case <-tick.C:
				}
				start := time.Now()
				resp, err := http.Get(srv.URL + u)
				if err == nil {
					_, _ = bufio.NewReader(resp.Body).WriteTo(discard{})
					resp.Body.Close()
					if resp.StatusCode == 200 {
						latMu.Lock()
						lat[name] = append(lat[name], time.Since(start))
						latMu.Unlock()
					}
				}
			}
		}
	}()

	startQ := tracer.n.Load()
	time.Sleep(duration)
	queries := tracer.n.Load() - startQ
	stop()
	srv.CloseClientConnections()
	wg.Wait()

	fmt.Printf("\nLOAD tags=%d pens=%d viewers=%d duration=%s\n", tags, pens, viewers, duration)
	fmt.Printf("LOAD app_pool_queries=%d (%.1f/s) sse_snapshots=%d\n", queries, float64(queries)/duration.Seconds(), snapshots.Load())
	if os.Getenv("LOAD_QSTATS") == "1" {
		printQueryStats(tracer)
	}
	names := make([]string, 0, len(lat))
	for n := range lat {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		d := lat[n]
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		fmt.Printf("LOAD %-15s n=%3d p50=%6.1fms p95=%6.1fms max=%6.1fms\n", n, len(d), ms(d[len(d)/2]), ms(d[int(float64(len(d))*0.95)]), ms(d[len(d)-1]))
	}
}

func printQueryStats(c *countingTracer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, st := range c.stats {
		fmt.Printf("QSTAT n=%5d avg=%7.1fms max=%7.1fms total=%8.0fms  %s\n", st.n, ms(st.total)/float64(st.n), ms(st.max), ms(st.total), k)
	}
}

const explainJoin = `LEFT JOIN LATERAL (SELECT gi.goat_id FROM public.goat_identifiers gi WHERE gi.tenant_id = tl.tenant_id AND gi.status = 'active' AND gi.smart_tag_capable IS TRUE AND gi.normalized_value IN (UPPER(BTRIM(tl.tag_id)), UPPER(BTRIM(COALESCE(tl.tag_mac, '')))) LIMIT 1) mapped_goat ON true LEFT JOIN public.goats g ON g.tenant_id = tl.tenant_id AND g.goat_id = mapped_goat.goat_id LEFT JOIN public.locations shed_loc ON shed_loc.tenant_id = tl.tenant_id AND shed_loc.location_id = g.shed_id`

func explainLive(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	qs := map[string]string{
		"1_summary_aggregate":                `SELECT count(*), count(*) FILTER (WHERE tl.mapping_state='mapped'), count(*) FILTER (WHERE (CASE WHEN now()-tl.last_seen_at > interval '30 minutes' THEN 'stale' ELSE tl.movement_state END)='moving'), count(*) FILTER (WHERE tl.signal_state='weak') FROM public.herd_signal_tag_latest tl ` + explainJoin + ` WHERE tl.tenant_id = '` + loadTenant + `'`,
		"2_pen_medians_new":                  `SELECT g.shed_id::text, percentile_cont(0.5) WITHIN GROUP (ORDER BY tl.motion_delta::float8) FILTER (WHERE tl.motion_delta IS NOT NULL AND tl.gap_delta IS NOT TRUE), percentile_cont(0.5) WITHIN GROUP (ORDER BY tl.tag_temperature_c::float8) FILTER (WHERE tl.tag_temperature_c IS NOT NULL) FROM public.herd_signal_tag_latest tl ` + explainJoin + ` WHERE tl.tenant_id = '` + loadTenant + `' AND tl.mapping_state = 'mapped' AND g.shed_id IS NOT NULL GROUP BY g.shed_id`,
		"3_page_limit26":                     `SELECT tl.* FROM public.herd_signal_tag_latest tl ` + explainJoin + ` WHERE tl.tenant_id = '` + loadTenant + `' ORDER BY tl.last_seen_at DESC, tl.tag_id LIMIT 26`,
		"4_old_cohort_page_5001":             `SELECT tl.* FROM public.herd_signal_tag_latest tl ` + explainJoin + ` WHERE tl.tenant_id = '` + loadTenant + `' ORDER BY tl.last_seen_at DESC, tl.tag_id LIMIT 5001`,
		"5_old_cohort_baseline_p75_all_tags": `SELECT w.tag_id, percentile_disc(0.75) WITHIN GROUP (ORDER BY w.motion_delta) FROM public.herd_signal_activity_windows w WHERE w.tenant_id = '` + loadTenant + `' AND w.tag_id = ANY(ARRAY(SELECT tag_id FROM herd_signal_tag_latest WHERE tenant_id='` + loadTenant + `')) AND w.bucket_seconds = 300 AND w.bucket_start >= now() - interval '24 hours' AND w.packet_count > 0 AND NOT w.gap_delta GROUP BY w.tag_id`,
	}
	names := make([]string, 0, len(qs))
	for n := range qs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for i := 0; i < 2; i++ { // second run = warm cache
			rows, err := pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+qs[n])
			if err != nil {
				t.Fatalf("%s: %v", n, err)
			}
			var exec, bufs string
			for rows.Next() {
				var line string
				_ = rows.Scan(&line)
				if strings.Contains(line, "Execution Time") {
					exec = strings.TrimSpace(line)
				}
				if bufs == "" && strings.Contains(line, "Buffers:") {
					bufs = strings.TrimSpace(line)
				}
			}
			rows.Close()
			if i == 1 {
				fmt.Printf("EXPLAIN %-36s %s | %s\n", n, exec, bufs)
			}
		}
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tags, pens int) {
	t.Helper()
	stmts := []string{
		`INSERT INTO tenants (tenant_id, name, status) VALUES ('` + loadTenant + `', 'Live Load', 'active')`,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ('20000000-0000-4000-8000-000000000000', '` + loadTenant + `', 'park', 'LP', 'Load Park', 'active')`,
		fmt.Sprintf(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 SELECT ('21000000-0000-4000-8000-' || lpad(p::text, 12, '0'))::uuid, '%s', 'shed', 'LS' || p, 'Pen ' || p, '20000000-0000-4000-8000-000000000000', 'active'
		 FROM generate_series(1, %d) p`, loadTenant, pens),
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ('22000000-0000-4000-8000-000000000000', 'org', 'Load Custodian', 'active')`,
		fmt.Sprintf(`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex)
		 SELECT ('23000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, '%s', 'alive', 'goat', '22000000-0000-4000-8000-000000000000',
		        ('21000000-0000-4000-8000-' || lpad((g %% %d + 1)::text, 12, '0'))::uuid, '20000000-0000-4000-8000-000000000000',
		        ('21000000-0000-4000-8000-' || lpad((g %% %d + 1)::text, 12, '0'))::uuid, 'Boer', 'female'
		 FROM generate_series(0, %d) g`, loadTenant, pens, pens, tags-1),
		fmt.Sprintf(`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable)
		 SELECT '%s', ('23000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, 'animal_identifier_1', 'T' || lpad(g::text, 6, '0'), 'T' || lpad(g::text, 6, '0'), 'global', true, 'active', now(), 'load_v1', true
		 FROM generate_series(0, %d) g`, loadTenant, tags-1),
		fmt.Sprintf(`INSERT INTO herd_signal_tag_latest (tenant_id, tag_id, tag_mac, gateway_id, last_seen_at, signal_state, battery_state, battery_mv, tag_temperature_c,
		   motion_count, motion_delta, motion_delta_60s, last_packet_motion_delta, motion_window_seconds, movement_state, pattern_state, mapping_state, gap_delta)
		 SELECT '%s', 'T' || lpad(g::text, 6, '0'), NULL, 'gw-1', now() - (g %% 120) * interval '1 second', 'strong', 'healthy', 3000, 36.5 + (g %% 7) * 0.3,
		        1000 + g, (g %% 50), (g %% 3), (g %% 2), 900,
		        (ARRAY['moving','quiet','not_moving'])[g %% 3 + 1], (ARRAY['normal','inactive','quiet_watch','normal'])[g %% 4 + 1], 'mapped', false
		 FROM generate_series(0, %d) g`, loadTenant, tags-1),
		fmt.Sprintf(`INSERT INTO herd_signal_activity_windows (tenant_id, tag_id, bucket_start, bucket_seconds, motion_delta, packet_count, gap_delta, first_seen_at, last_seen_at)
		 SELECT '%s', 'T' || lpad(g::text, 6, '0'), date_trunc('hour', now()) - h * interval '1 hour', 300, (g + h) %% 40, 10, false,
		        date_trunc('hour', now()) - h * interval '1 hour', date_trunc('hour', now()) - h * interval '1 hour' + interval '4 minutes'
		 FROM generate_series(0, %d) g, generate_series(0, 23) h`, loadTenant, tags-1),
		`ANALYZE`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}
}
