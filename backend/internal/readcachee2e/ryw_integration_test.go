package readcachee2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	gdpg "github.com/vgoats/goatos/backend/internal/growthdirector/adapters/postgres"
	gddomain "github.com/vgoats/goatos/backend/internal/growthdirector/domain"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	locationspg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationsports "github.com/vgoats/goatos/backend/internal/locations/ports"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
	procurementpg "github.com/vgoats/goatos/backend/internal/procurement/adapters/postgres"
	procurementdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// instance is one API process as far as the analytics cache is concerned: its own cache, its own
// LISTEN connection, and the module repositories wired to that cache exactly as bootstrap does.
type instance struct {
	name  string
	cache *readcache.Cache
	gd    *gdpg.Repository
	wg    *weighingpg.Repository
	id    *identitypg.Repository
	loc   *locationspg.Repository
	obl   *obligationpg.Repository
	proc  *procurementpg.Repository
}

func startInstance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) *instance {
	t.Helper()
	c := readcache.New(readcache.DefaultOptions("e2e-" + name))
	lctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	readcache.NewListener(pool, nil, c).Start(lctx)
	deadline := time.Now().Add(10 * time.Second)
	for !c.Coherent() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: eviction listener never connected", name)
		}
		time.Sleep(10 * time.Millisecond)
	}
	return &instance{
		name:  name,
		cache: c,
		gd:    gdpg.NewRepository(pool, 30*time.Second).WithReadCache(c),
		wg:    weighingpg.NewRepository(pool, 30*time.Second).WithReadCache(c),
		id:    identitypg.NewRepository(pool, 30*time.Second).WithReadCacheInvalidator(c),
		loc:   locationspg.NewRepository(pool, 30*time.Second).WithReadCacheInvalidator(c),
		obl:   obligationpg.NewRepository(pool, 30*time.Second).WithReadCacheInvalidator(c),
		proc:  procurementpg.NewRepository(pool, 30*time.Second).WithReadCacheInvalidator(c),
	}
}

var (
	winFrom = time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)
	winTo   = time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
)

// fcrPage is the Growth Director FCR tab read, through the instance's cache.
func (in *instance) fcrPage(t *testing.T, ctx context.Context) map[string]gddomain.FCRPen {
	t.Helper()
	got, err := in.gd.GetFCR(ctx, tenant, []string{park}, winFrom, winTo, "", "", "")
	if err != nil {
		t.Fatalf("%s: GetFCR: %v", in.name, err)
	}
	out := map[string]gddomain.FCRPen{}
	for _, p := range got.Pens {
		out[p.LocationID+"|"+p.PartitionLabel] = p
	}
	return out
}

func (in *instance) shedWeightsPage(t *testing.T, ctx context.Context) weighingdomain.ShedWeights {
	t.Helper()
	got, err := in.wg.GetShedWeights(ctx, tenant, []string{park}, "", winFrom, winTo, "", "", "", 0, 0, 0)
	if err != nil {
		t.Fatalf("%s: GetShedWeights: %v", in.name, err)
	}
	return got
}

func f(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

// eventually polls a page read until cond holds, failing after limit.
func eventually(t *testing.T, what string, limit time.Duration, cond func() (bool, string)) time.Duration {
	t.Helper()
	start := time.Now()
	var last string
	for {
		ok, state := cond()
		if ok {
			return time.Since(start)
		}
		last = state
		if time.Since(start) > limit {
			t.Fatalf("%s: still stale after %v (last read: %s)", what, limit, last)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// READ-YOUR-WRITES ACROSS TWO INSTANCES. Every case: both instances read the page (so both hold
// it cached), the write goes through the REAL module repository on instance A, then instance A's
// next read must show it at once (the writer evicts locally after commit) -- or within one second
// when the caller owns the transaction and the eviction arrives by NOTIFY -- and instance B's read
// within one second.
func TestReadYourWritesAcrossTwoInstances(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedFixture(t, ctx, pool)
	a := startInstance(t, ctx, pool, "A")
	b := startInstance(t, ctx, pool, "B")
	lump := shedLump + "|"
	scan := shedPhys + "|Part 2"

	type step struct {
		name       string
		write      func(t *testing.T)
		localFirst bool // the writer evicts its own instance synchronously
		fresh      func(pens map[string]gddomain.FCRPen, sw weighingdomain.ShedWeights) (bool, string)
	}
	steps := []step{
		{
			name:       "growthdirector PutAssumptions: goat sale price 425 -> 500",
			localFirst: true,
			write: func(t *testing.T) {
				p, loaded := 500.0, 425.0
				if _, err := a.gd.PutAssumptions(ctx, tenant, operator, time.Now(), gddomain.AssumptionsUpdate{
					SalePrices: []gddomain.SalePriceUpdate{{Species: "goat", PricePerKgINR: &p, LoadedPricePerKgINR: &loaded}},
				}); err != nil {
					t.Fatalf("PutAssumptions: %v", err)
				}
			},
			fresh: func(pens map[string]gddomain.FCRPen, _ weighingdomain.ShedWeights) (bool, string) {
				v := f(pens[lump].GainValueINR)
				return v > 17.5*425+1, fmt.Sprintf("lump gain value %.2f", v)
			},
		},
		{
			name:       "procurement CreateFeedPurchase: a dearer load before the window re-prices it",
			localFirst: true,
			write: func(t *testing.T) {
				batch, total := 7, 30000.0
				if _, err := a.proc.CreateFeedPurchase(ctx, tenant, procurementdomain.FeedPurchaseWrite{
					PurchaseDate: "2026-07-02", FarmLabel: "CBE", FeedItemLabel: "Maize Crush", BatchNo: &batch,
					QuantityKg: 1000, TotalCost: &total,
				}, operator, "e2e:purchase:1"); err != nil {
					t.Fatalf("CreateFeedPurchase: %v", err)
				}
			},
			fresh: func(pens map[string]gddomain.FCRPen, _ weighingdomain.ShedWeights) (bool, string) {
				v := f(pens[lump].FeedCostINR)
				return v > 1400+1, fmt.Sprintf("lump feed cost %.2f", v)
			},
		},
		{
			name:       "locations UpdateLocation: rename the lump shed",
			localFirst: true,
			write: func(t *testing.T) {
				var rv int
				if err := pool.QueryRow(ctx, `SELECT row_version FROM locations WHERE location_id = $1::uuid`, shedLump).Scan(&rv); err != nil {
					t.Fatal(err)
				}
				name := "Lump Renamed"
				if _, err := a.loc.UpdateLocation(ctx, locationsports.UpdateLocationCommand{
					TenantID: tenant, ActorID: operator, ClientIdempotencyKey: "e2e:rename", StoredIdempotencyKey: tenant + ":e2e:rename",
					IdempotencyScope: "locations.update", RequestHash: "e2e-rename", LocationID: shedLump, Name: &name, RowVersion: rv,
				}); err != nil {
					t.Fatalf("UpdateLocation: %v", err)
				}
			},
			fresh: func(pens map[string]gddomain.FCRPen, _ weighingdomain.ShedWeights) (bool, string) {
				d := pens[lump].OperationalLocationDisplay
				return strings.Contains(d, "Lump Renamed"), "lump display " + d
			},
		},
		{
			name: "identity RelocateGoatsToShedInTx (caller-owned tx): move kid A out of the scanned pen",
			write: func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				from, fromShed := park, shedPhys
				res, err := a.id.RelocateGoatsToShedInTx(ctx, tx, identityports.RelocateGoatsCommand{
					TenantID: tenant, ActorID: operator, GoatIDs: []string{kidA}, FromParkID: &from, FromShedID: &fromShed,
					ToParkID: park, ToShedID: shedOther, DestinationShedName: "Other Shed", Reason: "e2e", OccurredAt: time.Now(),
					OutboxIdempotencyPrefix: "e2e:relocate",
				})
				if err != nil {
					t.Fatalf("RelocateGoatsToShedInTx: %v", err)
				}
				if len(res.MovedGoatIDs) != 1 {
					t.Fatalf("relocate moved %v", res.MovedGoatIDs)
				}
				if err := tx.Commit(ctx); err != nil {
					t.Fatal(err)
				}
			},
			fresh: func(pens map[string]gddomain.FCRPen, _ weighingdomain.ShedWeights) (bool, string) {
				// Only the Beetal kid is left resident.
				return strings.EqualFold(pens[scan].Breed, "beetal"), "scanned pen breed " + pens[scan].Breed
			},
		},
		{
			name:       "obligation SyncPartitionMoveForGoat: move kid B to another partition",
			localFirst: true,
			write: func(t *testing.T) {
				if _, err := a.obl.SyncPartitionMoveForGoat(ctx, tenant, "33333333-0000-4000-8000-000000000002", shedPhys, "Part 9", "Fcr Shed"); err != nil {
					t.Fatalf("SyncPartitionMoveForGoat: %v", err)
				}
			},
			fresh: func(pens map[string]gddomain.FCRPen, _ weighingdomain.ShedWeights) (bool, string) {
				// No resident left: the pen is described by the kids weighed in it (both breeds).
				return !strings.EqualFold(pens[scan].Breed, "beetal"), "scanned pen breed " + pens[scan].Breed
			},
		},
		{
			name:       "weighing CorrectObservationWeight: the W2 whole-shed total",
			localFirst: true,
			write: func(t *testing.T) {
				if _, err := a.wg.CorrectObservationWeight(ctx, weighingdomain.WeightCorrectionCommand{
					TenantID: tenant, ObservationID: shedObsW2, RefType: weighingdomain.VerificationRefTypeShed,
					WeightKg: 550, CorrectedBy: operator, IdempotencyKey: "e2e:correct:1",
				}); err != nil {
					t.Fatalf("CorrectObservationWeight: %v", err)
				}
			},
			fresh: func(pens map[string]gddomain.FCRPen, sw weighingdomain.ShedWeights) (bool, string) {
				v := f(pens[lump].ADGGPerDay)
				return v > 101, fmt.Sprintf("lump adg %.2f", v)
			},
		},
	}
	for _, s := range steps {
		// Both instances hold the page before the write.
		for _, in := range []*instance{a, b} {
			pens := in.fcrPage(t, ctx)
			in.shedWeightsPage(t, ctx)
			if ok, state := s.fresh(pens, weighingdomain.ShedWeights{}); ok {
				t.Fatalf("%s: %s already shows the write before it happened (%s)", s.name, in.name, state)
			}
		}
		s.write(t)
		read := func(in *instance) func() (bool, string) {
			return func() (bool, string) { return s.fresh(in.fcrPage(t, ctx), in.shedWeightsPage(t, ctx)) }
		}
		if s.localFirst {
			if ok, state := read(a)(); !ok {
				t.Fatalf("%s: the WRITING instance's next read is stale (%s)", s.name, state)
			}
		} else {
			eventually(t, s.name+" (writer instance, caller-owned tx)", time.Second, read(a))
		}
		took := eventually(t, s.name+" (sibling instance)", time.Second, read(b))
		t.Logf("%s: sibling fresh after %v", s.name, took.Round(time.Millisecond))
	}
}

// The idle LISTEN connection is probed on a timer; a probe must not break it. After many probe
// cycles, a committed eviction still reaches the sibling within a second.
func TestListenerStillEvictsAfterIdleHealthProbes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	c := readcache.New(readcache.DefaultOptions("probe"))
	lctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	readcache.NewListener(pool, nil, c).WithHealthCheck(100*time.Millisecond, time.Second).Start(lctx)
	eventually(t, "listener connect", 10*time.Second, func() (bool, string) { return c.Coherent(), "not coherent" })
	key := readcache.Key{Tenant: tenant, Params: "probe"}
	if _, err := readcache.Load(ctx, c, key, func(context.Context) (string, error) { return "before", nil }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond) // ~15 idle probe cycles
	if !c.Coherent() {
		t.Fatal("health probes dropped a healthy LISTEN connection")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := readcache.CommitAndEvict(ctx, tx, nil, tenant); err != nil {
		t.Fatal(err)
	}
	eventually(t, "eviction after idle probes", time.Second, func() (bool, string) {
		v, _ := readcache.Load(ctx, c, key, func(context.Context) (string, error) { return "after", nil })
		return v == "after", "still " + v
	})
}
