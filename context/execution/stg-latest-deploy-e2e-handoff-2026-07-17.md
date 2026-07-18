# STG Latest Deploy + Vaccination Read-Model Handoff - 2026-07-17

This is the handoff for the next session. Do not continue from screenshots or
memory. Re-prove the live repo, STG images, and STG data first, then implement
the unfinished Full Schedule read-model/cache work.

## Current Verified State

- Repo/worktree used:
  `/Users/ravi/mesha/goatos-bug-batch-fix`
- Last verified `origin/main` and local `HEAD`:
  `d9ba115a764a7dc67348e18586b5f06ec0aab8d0`
- Last verified STG image tag:
  `d9ba115a764a`
- Google Cloud project:
  `goatos-stg`
- Region:
  `asia-south1`
- Expected Google account:
  `ravi@mesha.sg`

As of the last verification on 2026-07-17, all STG deploy targets matched
`d9ba115a764a`:

- `goatos-api-stg` -> `backend:d9ba115a764a`
- `goatos-admin-web-stg` -> `admin-web:d9ba115a764a`
- `goatos-kernel-worker-stg` -> `backend:d9ba115a764a`
- `goatos-stg-migrate` -> `migrate:d9ba115a764a`
- `goatos-stg-outbox-dlq` -> `backend:d9ba115a764a`
- `goatos-stg-analytics-rollup` -> `backend:d9ba115a764a`

This evidence is not permanent. If `origin/main` has moved, STG is stale again
until a new Cloud Deploy release finishes and the exact new SHA is verified
across services and jobs.

## What Is Already Fixed

1. STG deployment now waits for Cloud Deploy rollout and verifies image parity.
   `tools/deploy/stg-clouddeploy-release.sh` fails if any service/job still
   points to an old image.
2. The browser grant blocker was fixed by seeding/materializing active STG
   dashboard grants.
3. STG vaccination seed completed and was verified from fresh backend code.
4. Calendar global-nav no longer inherits a stale top-bar `as_of` date.
   Clicking Calendar opens today's operating date unless the user intentionally
   navigates inside Calendar.
5. Protocol Adherence info help is plain explanatory text plus an example, not
   fake live-looking DB numbers.
6. The admin-web high-cardinality prefetch storm was fixed by disabling Next
   prefetch on vaccination/full-schedule row links.

## What Is Not Done Yet

The Full Schedule performance architecture is not done. There is currently no
schedule read-model/cache table and therefore no cache reset path.

The current Full Schedule still reads through the canonical vaccination
operations path. That is correct for source of truth, but it can still be
heavier than necessary when the UI asks for a full-year schedule. The next
session should build a dedicated month/window read model instead of treating a
spinner as acceptable.

## Non-Negotiable STG Freshness Rule

Never call STG ready, deployed, or E2E-valid unless all of these are true at the
same time:

1. Local `HEAD` equals current `origin/main`.
2. The deployed API Cloud Run service image tag/digest matches that exact main
   commit.
3. The deployed admin-web Cloud Run service image tag/digest matches that exact
   main commit.
4. The kernel worker Cloud Run service image matches that exact backend commit.
5. Migration, DLQ, analytics, seed, or any other Cloud Run job involved in the
   feature matches that exact backend/migration commit.
6. Database migrations and seed/generated rows were produced by that same code
   generation, or were explicitly re-run after it.
7. Browser E2E uses the deployed STG URL, not localhost.

If `origin/main` moves during or after a deploy, repeat the deploy and parity
verification for the new SHA. Do not debug UI symptoms against mixed FE/BE/job/DB
state.

## Start-Of-Session Checks

Run these before making a change:

```bash
cd /Users/ravi/mesha/goatos-bug-batch-fix
git fetch origin main
git status --short
git rev-parse HEAD
git rev-parse origin/main
git ls-remote origin refs/heads/main
gcloud config list --format="text(core.account,core.project)"
```

Expected:

- active account is `ravi@mesha.sg`
- active project is `goatos-stg`
- `HEAD == origin/main`
- the only allowed dirty file before new work is this handoff if it has not yet
  been committed

Verify STG images:

```bash
for svc in goatos-api-stg goatos-admin-web-stg goatos-kernel-worker-stg; do
  printf '%s\t' "$svc"
  gcloud run services describe "$svc" \
    --project=goatos-stg \
    --region=asia-south1 \
    --format='value(spec.template.spec.containers[0].image,status.latestCreatedRevisionName,status.latestReadyRevisionName)'
done

for job in goatos-stg-migrate goatos-stg-outbox-dlq goatos-stg-analytics-rollup; do
  printf '%s\t' "$job"
  gcloud run jobs describe "$job" \
    --project=goatos-stg \
    --region=asia-south1 \
    --format='value(spec.template.spec.template.spec.containers[0].image)'
done
```

All image tags must match the current main short SHA. For services, latest
created revision must equal latest ready revision.

## Target Outcome For The Next Session

Make Full Schedule fast without lying:

- the schedule page reads a bounded month/window summary by default;
- month clicks fetch only the selected month/window, not a full year;
- a full-year view, if kept, reads a precomputed/year-paged summary;
- clicking any row drills into canonical live data;
- finishing a drive, accepting proof, deferring work, regenerating obligations,
  or reseeding invalidates/rebuilds only the affected scope;
- the UI shows freshness honestly if a summary is rebuilding or stale.

## Read-Model Shape

Add a schedule read model keyed by:

- `tenant_id`
- `scope_type` / `scope_id` or equivalent tenant/park/shed scope
- `year_month` or `window_start` + `window_end`
- `business_date` or source watermark
- `projection_version`
- `freshness_status`

Suggested summary row grain:

- park id/name/code
- shed id/name/code
- cohort/stage/course label
- vaccine code/list
- due date
- status bucket: scheduled, due, completed, deferred, blocked/review
- animal count
- manager/backup ids and display names
- source obligation ids or drilldown key, not a denormalized full roster

Do not store a row that cannot be traced back to canonical obligations,
completions, deferrals, or proof verification events.

## Rebuild And Invalidation Rules

This is not a blind cache TTL. Treat it as a projection/read model:

- mark dirty when obligation instances are created, scheduled, rescheduled,
  completed, deferred, rejected, or cancelled;
- mark dirty when proof is accepted/rejected and that changes completion state;
- mark dirty when a drive batch/session is generated, finished, or reconciled;
- mark dirty when rule DSL/protocol version changes;
- mark dirty after destructive seed or vaccination generation;
- mark dirty when shed ownership/backup assignment changes because manager
  labels on schedule rows must update;
- rebuild by tenant + affected park/shed + affected month/window;
- keep serving last good rows with freshness metadata during warm rebuilds;
- fail closed only on cold start when no previous successful summary exists;
- never blanket-stamp unrelated scopes as fresh.

Full rebuilds are allowed only for explicit seed/backfill/admin repair commands.
The steady-state path must be dirty-scope incremental maintenance.

## Backend Work Plan

1. Inspect current `/vaccination/operations` handler and repo shape:
   `backend/internal/vaccinationexecution/adapters/http/handler.go`,
   `backend/internal/vaccinationexecution/adapters/postgres/*`, and existing
   operation cursor/domain types.
2. Add migration(s) for the schedule read-model table and freshness metadata.
   Use concurrent indexes if any hot table is touched.
3. Add a projector/rebuilder service that can recompute one tenant/scope/month
   window idempotently.
4. Wire dirty marking from obligation/completion/deferral/proof/drive generation
   events. Prefer outbox/event-driven hooks where available.
5. Add a lightweight API, for example:
   `GET /vaccination/schedule?year=2026&month=08&scope_mode=company|park&park=...`
   returning only the fields the schedule table needs.
6. Keep canonical drilldown endpoints unchanged or route detail clicks to live
   canonical reads.
7. Add latency/query-plan tests for the month endpoint and a bounded full-year
   path if full year remains visible.

## Frontend Work Plan

1. Change Full Schedule to request the selected month/window by default.
2. Preserve selected month/year in URL state without shifting due to timezone or
   stale top-bar `as_of`.
3. Do not prefetch high-cardinality row detail links.
4. Virtualize or paginate if the month table can exceed a comfortable desktop
   row count.
5. Show freshness metadata only when it helps the operator; do not expose
   internal projection language as scary errors.
6. Hide global date/scope chrome on Full Schedule only if the backend contract
   says this screen owns its own month/window selector.

## Edge Cases To Cover

- `origin/main` moves while deployment is running.
- API, admin-web, worker, or a job is on a different image tag.
- Migration ran from a different backend commit than the deployed API.
- Seed/generated obligations predate the current backend code.
- A drive is completed while Full Schedule is open.
- Proof is accepted/rejected after rows were already summarized.
- Deferred sick/ICU/quarantine work changes bucket but must remain audit-visible.
- Sold/dead/culled animals must not reappear as open work.
- Shed manager/backup assignment changes after schedule rows were generated.
- One goat has multiple vaccine obligations on the same date.
- One shed exceeds daily capacity and splits across sessions/dates.
- Calendar month selection must stay on the clicked month.
- India business date must define due/missed windows, not UTC.
- Empty months should render a clear empty state, not a loader wall.
- Rebuild failure should serve last good warm data with freshness metadata.
- Cold start with no read-model rows should fail closed and name the missing
  rebuild, not invent zero work.

## Required Verification Before Pushing

Minimum local checks for the read-model change:

```bash
make validate-migrations
make validate-sqlc-plans
make scale-guard
make aggregate-projection-guard
go test ./internal/vaccinationexecution/... ./internal/obligation/... ./internal/adminui/app
npm --prefix apps/admin-web run typecheck
npm --prefix apps/admin-web run check:mock-fidelity
```

For main landing, use only:

```bash
make land-main
```

Do not direct-push to main. `make land-main` must run against a clean worktree.

## STG Deploy After Main Lands

After landing, deploy only the current main SHA:

```bash
git fetch origin main
git rev-parse HEAD origin/main
tools/deploy/stg-clouddeploy-release.sh
```

The release script must wait for rollout success and print:

```text
STG image parity verified for <short-sha>
```

Then rerun the independent service/job image checks above and compare against
`git ls-remote origin refs/heads/main`. If main moved again, redeploy the new
main before running browser E2E.

## Browser E2E Must Use Deployed STG

Use:

```text
https://stg.dashboard.mesha.sg
```

Minimum browser checks after the read-model work:

1. `/login` while signed in redirects away from login.
2. Dashboard loads without `permission_denied`.
3. Vaccination shed board loads and manager/backup names are not unassigned when
   STG seed says they exist.
4. Full Schedule opens to the current month/window.
5. Clicking Aug/Sep/Oct stays on the clicked month.
6. Full Schedule month load is fast and does not trigger a row-link prefetch
   storm.
7. Row click opens the correct dated detail and reads canonical live data.
8. Calendar opens today's operating date from global nav.
9. Protocol Adherence info copy is explanatory text/example only; live KPI cards
   can still show DB-backed counts.
10. Action Center, Calendar, Protocol Adherence, Workflows, and Vaccination all
    agree on the same current obligation/completion state after a drive/proof
    mutation.

If any check fails, do not call STG ready.
