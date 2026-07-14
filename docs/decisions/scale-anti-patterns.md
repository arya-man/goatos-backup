# Scale Anti-Patterns

Status: active guardrail.

> **Current release target: the 5k-to-50k operational-kernel envelope.** The
> accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md` is
> the authority for operational-kernel deployment scale and worker topology. It
> makes the 5,000-to-50,000-animal envelope the current release target and
> narrows one-million-animal deployment topology to future scale work and
> regression/research material. The anti-patterns below apply in full at the
> current envelope EXCEPT the five explicitly exempted, plan-tested envelope
> reads defined in the "5k-to-50k envelope: exempted canonical read paths"
> section below (Calendar, process-integrity, vaccination shed, execution, and
> operations): they are cheap compute-on-write correctness discipline, not a
> million-animal-only concern, so they hold whether the target is 50k today or a
> future 1M. Where this document still reasons about "1M" it is
> naming the future scale ceiling the shape must survive, not a present
> deployment requirement.

Goat OS keeps future million-animal scale as a design horizon, not a present
release invariant. New request paths, workers, importers, projectors,
dashboards, and reporting paths must be tenant scoped, indexed, bounded,
resumable, and measurable.

`make scale-guard` blocks new static offenders for the highest-risk patterns:

- compute-on-read god CTEs on request paths
- N+1 database calls inside loops (raw driver calls)
- N+1 fan-out: a ctx-taking call to an injected I/O dependency
  (repo/reader/port/client/roster/ownership) inside a loop — the driver call is
  one adapter layer down, invisible to the raw-driver N+1 check. The "small data,
  still slow" class (one round trip per row): a 25-row page becomes 51 serial
  reads. Fix by batching to a single `*ByIDs` / `= ANY($1)` read, as `ShedSummary`
  now does with `ShedOwnerships`.
- infinite paging loops without cursor/progress proof
- deep `OFFSET` pagination where keyset pagination is required
- tenant-wide projection delete/reinsert rebuilds
- non-sargable `lower(col) LIKE '%...'` search predicates
- capped read-time rollups that fetch a larger raw slice, aggregate it in
  app/service/frontend state, then hide pagination/truncation and present the
  collapsed result as business truth

Existing debt is tracked in `tools/scale-guard/baseline.txt`. Do not add a new
baseline count for new work. Fix the query, batch the writes, add a real
projection/read model, use keyset cursors, prove loop progress, or add a narrow
inline `scale-guard:ignore` with a concrete boundedness reason.

Calendar/Action Center/operator worklists need an extra explicit warning here:
if the product wants one park-drive row instead of hundreds of goat/protocol
rows, that grouped row must come from a projector/read model. It is not
acceptable to:

- bump the raw request-path limit (for example to 5000),
- aggregate those raw rows in a service/helper or frontend component,
- clear `NextCursor` or otherwise hide truncation,
- and then show shed/vaccine/goat counts as if they were complete business truth.

That pattern is still compute-on-read, still partial when the raw slice is
truncated, and still unsafe at 1M animals even if it looks fine on a local
fixture. If a temporary UI collapse is needed for a mock/demo, it must be
clearly partial/debug-only and must not invent authoritative totals.

The narrow Calendar exception is accepted vaccination completion history: a
read-only timeline behind the explicit `status=completed` path. That history is
not active work, not a park-drive rollup, and not a second projection row. It
may be derived at read time from canonical accepted completions plus completed
obligations when the query stays tenant-scoped, date-bounded, keyset-paginated,
and honest about truncation. Default month/date-marker responses may include
read-only completed markers so users can see recent completed days in the same
calendar surface, but open-work pages must not turn those markers into
aggregated operational totals, suppress pagination truth, or blur them into the
authoritative active-work list.

## 5k-to-50k envelope: exempted canonical read paths

The accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`
serves five operator screens directly from canonical tables through bounded,
indexed SQL for the 5,000-to-50,000-animal release envelope: **Calendar,
process-integrity, vaccination shed, vaccination execution, and vaccination
operations**. That per-request canonical read is the `compute-on-read` / god-CTE
shape this document bans and that `make scale-guard` blocks mechanically.
Narrowing the deployment scale target does NOT disable the guard, so these five
paths are reconciled with the guard explicitly rather than by weakening it:

- Each of the five named reads carries a scoped, sanctioned annotation on the
  exempted read:
  `// scale-guard:ignore: 5k-50k-envelope; see operational-kernel-5k-50k-scale-envelope.md`
  with a matching `tools/scale-guard/baseline.txt` entry where the guard
  requires one. The guard is **NOT** globally disabled: it stays fully active for
  every other path in `backend/internal/**`, and compute-on-read remains banned
  everywhere else. Only these five specific, named reads are exempted, and only
  under this envelope.
- The exemption is valid ONLY for a read that is query-plan-tested per the ADR —
  both the keyset-paginated list shape and the indexed summary-aggregate shape,
  the aggregate proven against the upper-bound obligation-row count (up to ~500k
  obligation rows at 50k animals), not just the 5k list case. An exempted read
  with no query-plan test is a defect, not an exemption.
- The annotation is **removed** when a screen later earns its own projection (the
  ADR scale-out ladder): that read then returns under full guard enforcement. The
  exemption is a measured, temporary envelope allowance, never a standing licence
  to compute-on-read.

This keeps the machine gate honest: the anti-pattern rule is suspended only for
these specific, measured, plan-tested envelope reads, never blanket-disabled.
This document backs `make scale-guard`; the guard code itself is unchanged by
this reconciliation note.

## Projection rebuild anti-patterns

The `full (stop-the-world) MV refresh` rule above bans the delete+reinsert
*mechanism*. This section bans the *rebuild trigger and availability* failures
that surfaced in the Calendar/CT/AC/PA/Vaccination projection rebuild (Codex
"Fix vaccination calendar rules", 2026-07-13). A projection that reads correctly
at 1k rows can still take the whole surface down or lie about freshness at 1M.
Root cause under all four: **rebuild should be read-through, dirty-scoped, and
honest about what it actually recomputed — never take the read model offline,
re-scan the whole tenant on a timer, rebuild twice, or blanket-stamp fresh.**

1. **Rebuild-by-unavailability (availability regression).** A rebuild that flips
   the read model to `unavailable`/stale (`ErrProjectionUnavailable`, 503, blank
   wall) *before* recomputing, so Control Tower / Action Center / Protocol
   Adherence / Calendar / Vaccination fail to load even though the last
   successful projection is still valid. Rebuild must be **read-through /
   version-swap**: the previous projection version keeps serving until the new
   version is built and atomically swapped in. Only a genuine cold start (no
   prior successful version for that scope) may serve `unavailable`. Never
   degrade a warm surface to rebuild it.

2. **Scheduled whole-tenant rebuild on a timer.** A cron that unconditionally
   recomputes the *entire tenant* projection every N minutes regardless of what
   changed (the whole-tenant `RecomputeProjection(tenantID)` shape). Cheap at 1k
   rows, a full-herd re-read every cycle at 1M. Replace with **event/dirty-set-
   driven incremental maintenance scoped to the changed unit** — per-shed /
   per-park dirty projections keyed off outbox deltas. Whole-tenant recompute is
   allowed ONLY as an explicit, off-request, manual/seed/backfill path (labeled
   as such), never as the steady-state refresh.

3. **Double / duplicate rebuild per cycle.** The same projection rebuilt more
   than once per cycle — two schedules, or a projector AND a sweeper both
   rebuilding the same rows. One projection = one owner = one trigger. A
   duplicate rebuild schedule is a defect, not a safety margin.

4. **False-freshness "incremental" worker.** A worker *labeled* incremental that
   actually copies the whole tenant and stamps freshness/watermark on scopes it
   did not recompute (e.g. marking mixed-age sheds `fresh`). The freshness
   envelope (`as_of` / `last_success_at` / `freshness_status` / source watermark
   / projection version) must reflect ONLY what was actually recomputed. Never
   blanket-stamp fresh. An "incremental recovery" path that is really a
   whole-tenant copy with a false freshness claim is a band-aid — reject and
   remove it, do not ship another unsafe rebuild on top of it.

Corollary (compute-on-read), scoped by scale target: under the 5k-to-50k
envelope the Calendar day/month-marker and completion-history read is served
directly from canonical obligations/batches/SOP/proof through bounded, indexed
SQL under the scoped `// scale-guard:ignore: 5k-50k-envelope` exemption — see the
"5k-to-50k envelope: exempted canonical read paths" section above and the
accepted ADR `docs/decisions/operational-kernel-5k-50k-scale-envelope.md`, which
removes `calendar_event_projections` and frames serving Calendar from canonical
tables as the correct choice that "deletes the entire projection-drift bug
class" (citing exactly the rebuild-trigger anti-patterns in this section). At
that envelope this is the sanctioned day-1 read, not a banned one. At future/1M
scale — or the moment this screen earns its own projection on the ADR scale-out
ladder, at which point the annotation is removed — that same Calendar
completion-history and month/date-marker read must instead be served from a
materialized projection rather than from canonical joins run live per request:
at that ceiling "measured" is not "safe" and a timed god-join is still
compute-on-read and still 1M-unsafe. Either way the accepted narrow Calendar
history exception above stays read-only, tenant-scoped, date-bounded, keyset,
and truncation-honest, and outside the named envelope exemption it does not
license live joins on the open-work path.

These four are **review-caught, not statically caught** — `make scale-guard`
blocks the delete+reinsert mechanism (`full-mv-refresh`) but cannot see rebuild
cadence, a serving-state flip, or a false freshness stamp. Reviewers must name
them; see `.agents/skills/goatos-code-review/references/kernel-and-scale.md`.

When an E2E run, scale audit, or feature proof is generated while fixing one of
these issues, commit the report and publish it through the GitHub Pages report
site. Local-only proof must say that it is local-only and must not be described
as staging or production certification.

## Admin-web SSR full-table request reads

`make scale-guard` scans Go (`backend/internal/**`) only. The same compute-on-read
disease crosses into `apps/admin-web/**`: a Next.js server component or
`lib/api/server.ts` helper that **drains a paginated backend endpoint cursor-by-
cursor into one big array** to compute a KPI on the request path. That is exactly
the `searchAllGoats` full-herd walk removed in commit `810bc1b3` — it looks fine
against a 1k-goat fixture and melts at 1M:

```ts
// BANNED — full-herd SSR walk
export async function searchAllGoats(params: Omit<HerdSearchParams, "limit" | "cursor">) {
  const items = [];
  let cursor;
  for (;;) {
    const page = await searchGoats({ ...params, limit: 100, cursor });
    items.push(...page.data.items);           // accumulate every page into memory
    if (!page.data.next_cursor) break;
    cursor = page.data.next_cursor;           // drain the whole cursor
  }
  return { ok: true, data: items };           // then filter/count in the component
}
```

The fix is a **projection/summary endpoint** that returns pre-aggregated counts;
the request does one indexed lookup, never a row walk:

```ts
// CORRECT — read the read model
export async function getHerdRegisterSummary(params: HerdRegisterSummaryParams) {
  return request(() => client.request("/herd-register/summary", { query: params }));
}
```

`make admin-web-request-reads-guard`
(`tools/agent-hooks/check-admin-web-request-reads.mjs`) blocks the `cursor-drain-
loop` shape: a `for`/`while` whose body both accumulates (`.push(...)` / `.concat`)
and advances a cursor from `next_cursor`. It is **diff-scoped** (a commit with no
admin-web TS passes instantly), skips `'use client'` modules and test/mock/seed
files, and offers an inline `// scale-guard:ignore: <reason>` escape hatch for a
genuinely-bounded, small-cardinality read. The `Omit<Params, "limit" | "cursor">`
signature alone is **not** flagged — a projection/summary reader legitimately takes
no page bound (e.g. `getOperationsAuditSummary`); only the actual drain loop is.
`make admin-web-request-reads-guard-audit` runs the whole-tree audit; the legacy
`OutboxDao.observeAll` and any other pre-existing whole-set reader surface there
and should migrate to a projection/keyset read.
