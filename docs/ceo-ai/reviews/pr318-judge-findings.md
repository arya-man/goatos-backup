# PR #318 judge findings — `feat/ceo-ai-v3-p1a-tenant-sql` (P1a)

Reviewer: automated judge. Diff `993628743..HEAD` (3 commits). Base `origin/main` @ `f90e43c72`.
Verified against `docs/ceo-ai/plan-v3-one-brain-two-doors.md` D0, D1.1–D1.3, D4(Fact), D5, D6.

## Verdict

**NOT MERGEABLE.**

Two independent blockers:
1. `ExecuteReadOnlyForTenant` still returns other tenants' rows for at least 6
   SQL shapes — proven live against real `ceo_ai.*` views with two seeded
   tenants. D0's central promise ("no layer trusts the one above… a second
   predicate can never widen scope") is false as written.
2. `make guardrails` FAILS on the PR's own new code (exception-guard, CI-required,
   diff-scoped) — so `make land-main` cannot produce a green receipt. This is not
   my scratch files; it is 5 swallowed-error sites the PR introduced.

Everything else (grant revoke, view rebuild, schema cards, timerange, repair
loop, Fact.TenantID stamping, cache/memory/resume scoping, eval, scope
discipline) is solid and largely well-tested. The blockers are in the one place
that matters most.

---

## BLOCKER 1 — model-SQL tenant guard is bypassable; live cross-tenant leak

`backend/internal/ceoai/sqlguard/tenant_predicates.go:51`
(`ExtractAllTenantPredicatesWith`) + `validator.go` set-op / boolean handling.

The guard accepts a draft as long as **every** `tenant_id = '<lit>'` predicate's
RHS literal equals the session tenant. It never checks the predicate's
**polarity**, whether it is a **top-level AND conjunct**, or whether a **second
relation** was appended by a set operation. So a draft can carry the honest
`tenant_id = '<session>'` predicate (which the extractor is happy with) and still
return all tenants' rows.

Root causes in `validator.go`:
- `UNION` / `INTERSECT` / `EXCEPT` are **not** in `bannedKeywords` (they appear
  only as clause terminators at `validator.go:538`). The `TABLE <view>` command
  form is also not rejected and contains no `SELECT`, so the "exactly one SELECT"
  rule (`countKeyword(tokens,"SELECT")==1`) and the WHERE/tenant-predicate rules
  are satisfied by the **first** arm while the second arm (`UNION ALL TABLE
  ceo_ai.x`) is entirely unscoped.
- Boolean inversions (`NOT`, `IS FALSE`, `= false`, `CASE WHEN`, `nullif(...)`)
  are not recognized; the extractor sees `tenant_id = '<session>'` and passes,
  but the predicate's truth value has been negated so the scan returns the
  complement (every other tenant).

**Live proof** (scratch test on real views, two seeded tenants A/B via
`reporting.newDB` + `seedHealthIssueLoad`, `ExecuteReadOnlyForTenant(ctx, A,
sql)`, LEAK = a `Northwind`/tenant-B row returned). Shapes that returned tenant
B's rows with `err=nil`:

| # | Shape (session=A) | Result |
|---|---|---|
| 01 | `WHERE (tenant_id = 'A' OR health_blockers > 0)` | rows=2 **LEAK** |
| 02 | `WHERE NOT tenant_id = 'A'` | rows=1 **LEAK** |
| 04 | `WHERE tenant_id = 'A' IS FALSE` | rows=1 **LEAK** |
| 06 | `WHERE (tenant_id = 'A') = false` | rows=1 **LEAK** |
| 07 | `WHERE CASE WHEN tenant_id = 'A' THEN false ELSE true END` | rows=1 **LEAK** |
| 25 | `SELECT * FROM v WHERE tenant_id = 'A' UNION ALL TABLE ceo_ai.v` | rows=3 **LEAK** |

(#01 paren-OR: the top-level-OR guard only fires when the OR is *not* wrapped in
parens; wrapping it in `( … )` — which the guard's own comment invites — puts a
disjunction over the tenant predicate at depth 1 and the executor still binds
only the literal, which equals the session. This is the exact scenario the
`hasTopLevelOrInWhere` comment claims "cannot dissolve the top-level AND tenant
scope" — it can.)

Also passes the guard (nil-pool reached, i.e. would have executed), not
re-proven live but same class: `tenant_id = 'A' IS NOT TRUE`,
`(tenant_id = 'A') = false` with neutral projection, `coalesce(tenant_id='A',
true) IS NOT TRUE`, `IS DISTINCT FROM true`, `false = (tenant_id='A')`,
`tenant_id = 'A' IS UNKNOWN`, `bool_or(tenant_id='A')` in HAVING,
`tenant_id = 'A' ISNULL`.

Rejected correctly (for reference — the guard does stop these): second literal
`AND tenant_id='B'`, `<>`/`IN`/param/column-ref forms, top-level `OR` (unparen),
`||` concat, `"tenant_id"` quoted ident, `::text` cast (drops tenant scope →
rejected by WHERE-scope rule), `E'..'`, `$$..$$`, NBSP/unicode, `--`, `/* */`,
`;`, `UNION SELECT …'B'` (second SELECT → single-SELECT rule).

The plan's soundness argument ("single-relation + no subquery + no OR-at-top +
no comments ⇒ no scope for a second predicate to hide") is **wrong** because the
guard does not actually enforce single-relation (set-op `TABLE` arm), does not
reject negation, and its top-level-OR check is defeated by parentheses.

**Fix required (must land in this PR — this is the entire point of P1a D0):**
- Ban `UNION`, `INTERSECT`, `EXCEPT`, and the `TABLE`/`VALUES` statement forms
  outright in `bannedKeywords` (single flat SELECT means no set operators).
- Reject `NOT`, `IS`, `CASE`, and any boolean-returning function wrapping a
  tenant predicate, OR — better — stop treating "the literal equals session" as
  sufficient: require the tenant predicate to be a **positive top-level AND
  conjunct** of the WHERE clause (depth-0, not under NOT/CASE/OR/parenthesized
  disjunction), which is the only shape that actually confines the scan.
- Extend `TestTenantBypassCorpusAllRejected` with all shapes above; the fuzz
  invariant currently only checks *extracted literals equal session*, which is
  exactly the property that does not imply isolation — add "the executed row set
  contains no foreign tenant" as a pg-gated behavioural assertion (the corpus is
  static-only today, so it never caught these).

Note the fuzz/corpus test `FuzzTenantPredicateBypass` asserts the wrong
invariant (literal == session), so it green-lights every shape above. The 88-shape
corpus is real work but tests the property that isn't the safety property.

## BLOCKER 2 — `make guardrails` fails on the PR's own code (land-main gate)

`make guardrails` → `exception-guard` (diff-scoped vs origin/main, `requiredInCI`)
FAILS with 5 `go:swallowed_err` findings, all in this PR's new lines:

- `backend/internal/ceoai/app/orchestrator.go:359` — `if composeErr != nil { return a.refusal(...) }` discards `composeErr`
- `backend/internal/ceoai/app/orchestrator.go:386` — same, retry path
- `backend/internal/ceoai/app/orchestrator.go:443` — `if chartErr != nil { return a.refusal(...) }` discards `chartErr`
- `backend/internal/ceoai/sqlguard/executor.go:162` — `literals, err := ExtractAllTenantPredicates(sql); if err != nil { return nil, ErrTenantBinding }` discards `err`
- `backend/internal/ceoai/sqlguard/keywords_export.go:44` — `Tokens()` discards the strip error

Land-main requires a green receipt; this PR cannot produce one. Full backend
`go test ./...` (291 pkgs, 156 s) and `make ceo-ai-eval-selftest` DO pass, and
every ceo-ai guard passes — only exception-guard fails, on the swallowed errors.

The orchestrator:359/386/443 sites are also a **substantive** problem, not just a
lint: a mixed/foreign-tenant fact set (a D0 invariant violation) is turned into a
generic refusal with **no `recordAudit`, no log line, no telemetry**. D0 says
"each attempt audited" and the Chart/facts row promises the gate is observable.
Right now a cross-tenant fact leak that the composer *does* catch is
indistinguishable from an ordinary refusal. Fix: log + audit + a telemetry
counter before returning, then wrap/reference the error (which also clears the
guard). At executor.go:162, fold the extractor reason into the trace instead of
flattening to `ErrTenantBinding`.

---

## MAJOR

### M1 — Comparison-window sub-question is un-answerable (D1.2 / D4)
`ResolveWindow` correctly sets `Window.Compare` for "this quarter vs last", and
the planner prompt (`vertex/prompt.go:116`) asks the model to draft **one
sub-question per window**. But `injectWindow` (`app/fallback.go:40`) stamps only
the primary `from`/`to` onto **every** sub-question, and `validateModelSQL` →
`ValidateWindow` then requires *that* primary window's literals. The comparison
sub-question (bound to the compare period, exactly as the prompt instructed) is
rejected:

```
compare-window sub-question (Q2 bound as prompt asked): err=sqlguard: window
requested (2026-07-01 .. 2026-09-30) but the statement must bind it as
event_date >= '2026-07-01' AND event_date < '2026-10-01'
```

`compare_from`/`compare_to` are written to params (`fallback.go:57`) and reserved
in `cubeParams` but **no SQL path ever consumes them** to relax the window guard
for the second sub-question. Net effect: every two-window comparison ("vs",
"compared to", "this quarter vs last") loses its comparison arm to a guard
rejection, then the repair loop burns its one shot and the answer degrades to a
single-window honest-partial. D4's "Δ abs+% when exactly two period labels" can
never trigger through the SQL tier. Either thread the compare window to the
matching sub-question and have `validateModelSQL` accept either the primary or
the compare window, or drop the two-sub-question comparison prompt for P1a.

### M2 — Model can override the server-resolved window silently (D1.2)
`validateModelSQL` only checks the window carried in the sub-question's own
`from`/`to` params, and `injectWindow` **skips** any sub-question that already
has a `from` key (`fallback.go:48`). Proven: a draft that binds
`event_date >= '2020-01-01' AND event_date < '2021-01-01'` with matching
model-supplied `from`/`to` params passes `validateModelSQL` with `err=nil` even
though the server resolved "last month" = 2026-08. The plan's promise is the
**server-resolved** window is bound exactly; the guard actually validates
whatever window the params carry, and the model influences params. The window
should be taken from the server resolution and forced onto the sub-question, not
read back from a field the plan pipeline lets the model-influenced draft seed.
(Lower severity only because the model is instructed to bind the given window and
this is the fallback tier; still a real hole in the "concrete guard, not a
promise".)

---

## MINOR

- **`PLANNED:P<n>` is a page-level loophole (D6).** `check-ceo-ai-page-contract-drift.mjs`
  classifies a whole page surface PLANNED and passes; a *new* KPI tile added to a
  page already tagged `PLANNED:P2` (9 such pages today) is invisible to the guard
  — the drift guard only reacts to a page whose surface is entirely absent from
  the matrix. Tighten: require a PLANNED row to enumerate the specific
  kpi/chart copy-keys it defers (the guard already parses them), and fail when a
  page contract grows a key not listed in its PLANNED row. As written, "a new
  tile fails CI until the bot can answer it" (the guard's own doc string) is
  false for any page already in PLANNED debt.
- **No size bound on the planner prompt (D1.1).** The card block is snapshot-locked
  by `prompt.golden` (16 KB) but there is no assertion bounding total prompt
  bytes; adding the 28th–40th view card silently grows the prompt with no gate.
  The plan calls the golden "bounded"; it is fixed, not bounded. Add a byte cap
  assertion.
- **Fuzz corpus is static-only.** `TestTenantBypassCorpusAllRejected` never
  touches a pool and `FuzzTenantPredicateBypass` asserts literal==session, so the
  "10k-case corpus / fuzz over the tokenizer" the D0 table cites cannot catch a
  guard that accepts a session-literal draft which still leaks (Blocker 1). Needs
  a behavioural (pg-gated) leg.

## NIT

- The planner prompt still injects the whole tenant UUID as a literal string to
  the model (`Always include WHERE tenant_id = %q`) and lists park_id UUIDs; this
  is pre-existing (identical at base `993628743`) and out of P1a scope, but it
  means the tenant literal is in model context — the executor's server-binding is
  what saves it, which is exactly the layer Blocker 1 shows is porous. Worth a
  P1d note.
- `ExecuteReadOnly` (no-tenant, `executor.go:219`) remains exported; no non-test
  caller uses it (grep clean), but it is a foot-gun that bypasses tenant binding
  entirely. Consider unexporting.

---

## Positives verified (not padding — these are the load-bearing D0 pieces that hold)

- **Migration 000359**: REVOKE public.* + `ALTER DEFAULT PRIVILEGES … REVOKE`
  over every owner with a default-ACL entry; Cube role `mesha_cube_readonly`
  correctly left with public.* (its governed metrics read raw tables) — that role
  IS a remaining `public.*` blast radius and the migration says so explicitly; it
  should be tracked in the plan as the one deliberate public reader. View rebuild
  is append-only (park_location_id last), keeps the `projection-review:` header,
  collapses `arrival_intake_reviews` to the latest via LATERAL LIMIT 1. Both
  grant scripts (`grant-assistant-public-read.sh`, `setup-ceo-ai-local-role.sh`)
  no longer re-grant public to the ceo role and actively revoke on re-run. All 8
  pg-gated tests pass against a real PG16 (I ran them):
  `TestAssistantRoleCannotReadPublic`, `TestEveryCeoAiViewHasTenantID`,
  `TestTrustedSQL_HealthIssue_TenantScoped`,
  `TestSourceEntryHealthStatusOneToManyReviews`, `TestSchemaCards*`.
- **Trusted SQL** ($1-bound) rewrite of `healthIssueSQL` is correct: no tenant
  literal in text, park scope rides as $2, executor prepends session tenant as
  $1, `TrustedSQL` is not model-settable (`domain/types.go:77`, never mapped in
  `parsePlan`). `trustedTenantParamCheck` rejects string/other-param/non-tenant-
  join RHS.
- **Fact.TenantID stamping**: all 12 API executors go through `stampTenant`
  (overwrites, never reads a row); Cube (`wiring.go:273`), SQL
  (`wiring.go:456`/`700`), Toolbox all stamp from actor; `validateFactTenants`
  refuses empty/foreign/mixed (not silently dropped). Cache Set is gated on
  `validateFactTenants == nil` (`orchestrator.go`), key is tenant-first, memory
  key now `(tenant|user|conversation)`, resume does a scoped `Get` → 404.
- **Repair loop**: at most one repair per failed model-SQL sub, only for
  `isModelSQL` (not trusted, not natural_sql regex), second failure falls to
  honest-partial, both errors land in the step trace, `SQLReject`/`SQLPGError`
  telemetry with bounded reason labels.
- **Timerange** (8 CEO phrasings I asserted): last 2 weeks ✓, Q2 ✓, this quarter
  vs last (+Compare) ✓, since June ✓, yesterday ✓, August ✓, "between 3 and 10
  August" → resolves to whole-of-August (acceptable degrade, not wrong-month),
  "FY"/"H1"/"week 36"/"fortnight" → not resolved (ok=false, honest). Cube tier
  threads window via `time_range`→timeDimension; API `feed_weight_band_summary`
  lists `from`/`to` params.
- **Scope discipline**: zero files changed outside `internal/ceoai/`, `docs/ceo-ai/`,
  `tools/ceo-ai/`, `tools/agent-hooks/`, `tools/ci/`, `Makefile`, the two grant
  scripts, migration 000359, the skill, and the two hook JSONs. `cmd/mcp`
  untouched (P1c), no regex narrowing in `natural_sql.go` (P1b), validator
  banned-keyword list unchanged (only `ExtractTenantEquals` deleted). `.claude/
  settings.json` + `.codex/hooks.json` are valid JSON and mirror the existing
  coverage-hook pattern; guardrail-manifest + run-local-ci wiring correct;
  guardrail-registration guard passes (138 guards).
- Full `go test ./...` backend green (291 pkg, 156 s). `ceo-ai-eval-selftest`
  green (100 Q / 75 classes). Golden diff is additive only (adversarial +2,
  vaccination/ops/feed/tenant-isolation new files); no existing question edited;
  `refusal_rawdump`/`aggregate_first` still present (plan says drop them in a
  later phase, so leaving them is fine for P1a).

## Bypass shapes tried (session=A, victim=B)
LEAK (live, executed, returned B): #01 paren-OR, #02 NOT, #04 IS FALSE, #06
`(pred)=false`, #07 CASE WHEN, #25 UNION ALL TABLE.
PASSED GUARD (would execute): IS NOT TRUE, IS UNKNOWN, IS DISTINCT FROM true,
coalesce(...) IS NOT TRUE, false=(pred), nullif(...) IS NULL, bool_or(pred) in
HAVING, ISNULL, uppercase+tabs/newlines.
REJECTED (correct): 2nd literal AND, `<>`,`!=`,`IN`,`ANY`,`BETWEEN`,param,column-
ref,self-eq, top-level OR (unparen), `||`, `= true`, `"tenant_id"`, `::text`,
`E''`, `$$`, `;`, `--`, `/* */`, NBSP/cyrillic/fullwidth, UNION SELECT, WITH,
subquery IN/scalar, public.*, no-LIMIT, LIMIT 1000, empty literal.

## Window phrasings tried (now = 2026-09-19)
last 2 weeks → 09-06..09-19 ✓; Q2 → 04-01..06-30 ✓; between 3 and 10 August →
Aug 1..31 (whole month, degrade); this quarter vs last → 07-01..09-30 +cmp
04-01..06-30 ✓; since June → 06-01..09-19 ✓; yesterday → 09-18 ✓; August →
Aug ✓; FY → unresolved; H1 → unresolved; week 36 → unresolved; fortnight →
unresolved; mtd → 09-01..09-19 ✓; last 30 days → 08-21..09-19 ✓.

---

# Round 2 — re-judge of fix commit `1b7290079`

Diff reviewed: `git diff 1844d1b37..1b7290079` (26 files). Worktree clean, no
uncommitted changes. Throwaway PG16 on :5599 reused for every pg-gated proof.
All scratch tests deleted after use.

## Round-2 verdict: **NOT MERGEABLE** (one new BLOCKER, narrow fix; then MERGEABLE)

Every round-1 finding is fixed and verified (see "Round-1 items closed" below).
The harder adversarial pass found **one new leak class** that the structural
tenant-conjunct rule cannot see: SQL executed *inside a function argument*.
It is a real, live, as-the-production-role cross-tenant read through
`ExecuteReadOnlyForTenant`. The fix is small and bounded (deny/allow-list of
function identifiers), so I expect the next round to be MERGEABLE, but D0 is
non-negotiable and I will not call a proven live leak mergeable.

## BLOCKER R2-1 — query-executing functions dump other tenants' rows

`backend/internal/ceoai/sqlguard/validator.go` `bannedFunctions`: only
`query_to_xml` is banned. Postgres ships several **other** functions that take
a table name or a *query string* and execute it with the caller's privileges.
The query/table text lives inside a string literal, which `Validate` strips as
opaque data, so the structural rules (one FROM, one tenant conjunct, no
set-ops) are all satisfied by the *outer* statement while the *inner* read is
unscoped. `mesha_ceo_readonly` holds SELECT on `ceo_ai.*`, so the inner read
succeeds on every governed view.

Live proof, `ExecuteReadOnlyForTenant(ctx, A, …)` against two seeded tenants,
then re-run under `SET ROLE mesha_ceo_readonly` after applying 000359:

| # | Shape (session = A, projection over `ceo_ai.source_entry_health_status`) | owner pool | as `mesha_ceo_readonly` |
|---|---|---|---|
| R18 | `SELECT table_to_xml('ceo_ai.source_entry_health_status', true, false, '')::text AS value FROM v WHERE tenant_id='A' LIMIT 1` | rows=1 **LEAK B** | err=nil **LEAK B** |
| R19 | `SELECT schema_to_xml('ceo_ai', true, false, '')::text …` (dumps **every** ceo_ai view) | rows=1 **LEAK B** | err=nil **LEAK B** |
| R20 | `SELECT query_to_xml_and_xmlschema('select source_label from ceo_ai.source_entry_health_status', true, false, '')::text …` | rows=1 **LEAK B** | err=nil **LEAK B** |
| R17 | `SELECT (ts_stat('select to_tsvector(source_label) from ceo_ai.source_entry_health_status')).word AS value …` (arbitrary query text) | rows=4 **LEAK B** | err=nil, rows returned (first word was A's; same class) |
| R27 | `SELECT xpath('/a', table_to_xml(…))::text …` | passes guard | — |

Same family not yet exercised but in the same boat: `query_to_xmlschema`,
`table_to_xmlschema`, `table_to_xml_and_xmlschema`, `schema_to_xmlschema`,
`schema_to_xml_and_xmlschema`, `database_to_xml*`, `cursor_to_xml*`,
`xmltable` (needs FROM — blocked), tablefunc `crosstab`/`connectby` (extension,
not installed here — verify on STG), `pg_get_viewdef` (leaks view SQL, not
tenant rows — NIT).

**Fix required:** a deny-list is the wrong shape here (I found four names in
ten minutes; there will be more, and extensions add more). Replace
`bannedFunctions` with an **allow-list** of function identifiers the fallback
may call (count/sum/avg/min/max/coalesce/nullif/round/abs/lower/upper/left/
right/length/date_trunc/to_char/extract/now/current_date/greatest/least/
string_agg/bool_and/bool_or, plus `cast`/`filter`/`over` keywords) and reject
any `ident (` whose ident is not on it. Add the five shapes above to
`TenantBypassCorpus` so `TestModelSQLNoForeignRowsEver` covers them
behaviourally. If an allow-list is judged too restrictive for P1a, the minimum
is: ban by **prefix** `query_to_xml`, `table_to_xml`, `schema_to_xml`,
`database_to_xml`, `cursor_to_xml`, and ban `ts_stat`, `xmltable`, `crosstab`,
`connectby`, `xpath` outright — and record that the deny-list is P1d debt.

## MAJOR R2-2 — `Window:` line lies when a comparison plan is degenerate (D4)

End-to-end through `Ask` (fake provider, fake SQL tier, real orchestrator +
window threading + composer), question `"Compare August with September
widgets"` (resolves 2026-08 with compare 2026-09):

- **E4** plan = two arms, **both** bound to August → both pass (either-window
  rule), no repair, body = `Deaths: 7. Deaths: 7. Window: august (01/08/2026 to
  31/08/2026) vs september (01/09/2026 to 30/09/2026).`
- **E5** plan = **one** arm bound to August → body = `Deaths: 7. Window:
  august … vs september ….`

The period line describes the *resolved question*, not the windows that
*actually ran*; a CEO reads two identical numbers labelled as a month-on-month
comparison, or one number labelled as a comparison. D4 says the window line
must state what ran. The either-window acceptance (M1 fix) is correct per arm
but nothing checks the arms **cover both windows**. Fix: have
`validateModelSQL` record which window each model-SQL sub bound (a param such
as `bound_window=primary|compare`), and have `windowResult` render only windows
that at least one successful sub bound; when a comparison question ends up
with arms on a single window, render that single window and let the composer's
"partial" wording apply (or repair the missing arm). `TestCompareWindowsThreadedAndValidated`
only tests the happy path (one arm each).

## MINOR

- **R2-3 BETWEEN with string literals is rejected** (`WHERE tenant_id='A' AND
  load_label BETWEEN 'A' AND 'Z'` → `malformed WHERE clause (empty conjunct)`):
  the literal-stripped `AND` inside BETWEEN is split as a top-level conjunct and
  the trailing literal becomes an empty conjunct. Numeric BETWEEN passes.
  Functional false positive, not a leak; the date guard requires `>=`/`<` anyway,
  so it mostly bites text ranges. Either handle `BETWEEN … AND` in
  `splitTopLevelConjuncts` or document it in the prompt so the model never
  drafts it.
- **R2-4 `rejection_reason` column never populated.** The persisted audit row
  for a tenant-gate refusal is correct (`status=refused`,
  `review_verdict=… reasons=tenant_gate:compose:foreign`, step
  `tenant_gate:compose` with the error) — verified live via
  `PostgresTraceStore` + `GetTrace`, one row in `ceo_ai_assistant_audit`. But
  `audit_bridge.go toTraceRecord` never maps `FailReasons` to
  `RejectionReason`, so `rejection_reason` is `""` and anyone filtering the
  table by that column sees nothing. Map `tenant_gate:*` (and sqlguard reject
  classes) into it.
- **R2-5 `recordAudit` still ignores the sink error** (`_ = a.audit.Record`,
  pre-existing). For the tenant-gate path specifically, a failed insert means
  the only durable evidence of a D0 violation is a log line. At minimum log
  the error at the tenant-gate call site.

## NIT

- `LIMIT 1e9` passes `enforceLimit` (tokenizes as `1` + ident `e9`); harmless
  because the executor's outer wrap caps at 100, but the validator's own claim
  "LIMIT must be a single integer constant" is not true for exponent literals.
- `pg_get_viewdef('ceo_ai.v')` is callable and returns the governed view's SQL
  (schema, not tenant data). Add to the deny/allow-list decision above.

## Round-1 items closed (verified, not taken on trust)

- **Blocker 1** — structural tenant conjunct: `checkTenantConjunct` requires
  exactly one `tenant_id` token in the statement, one depth-0 WHERE, the tenant
  predicate as a positive top-level AND conjunct of shape `tenant_id = '<lit>'`
  (optionally `::cast`, optionally alias-qualified, optionally once-parenthesised);
  `UNION/INTERSECT/EXCEPT/TABLE/VALUES` banned; exactly one depth-0 FROM;
  balanced parens; adjacent literals rejected; `::cast 'lit'` rejected. All 6
  round-1 live-leak shapes now rejected, and the corpus grew to 122 shapes
  incl. those 6 + IS/NOT/CASE/coalesce variants; `FuzzTenantPredicateBypass`
  now asserts `structurallyScoped`; `TestModelSQLNoForeignRowsEver` runs the
  whole corpus against real views (pg-gated) — I ran it: PASS.
  Round-2 structural shapes (coordinator's list), all handled as expected:
  `t.tenant_id` alias PASS(legit); `tenant_id` inside a literal PASS(legit,
  opaque); `AND (x OR y)` no-tenant paren-OR PASS(legit); `LATERAL` reject;
  `WITH ORDINALITY` reject; `FROM v v,` reject; `FROM (ceo_ai.v)` reject;
  `HAVING bool_and(tenant_id='A')` with no WHERE reject; `WHERE true HAVING
  bool_or(tenant…)` reject; `tenant_id='A' AND tenant_id='A'` reject (count=2);
  `OFFSET` / `FETCH` reject (banned); window fn `count(*) OVER ()` PASS(legit,
  WHERE applies first); `AND false OR true` reject; `AND (false OR true)`
  PASS(legit); `FILTER (WHERE tenant_id='A')` with no top WHERE reject;
  `(tenant_id='A') AND x` PASS(legit); `IS TRUE` reject; `'A'\n''` adjacent
  reject; alias `is` reject.
- **Blocker 2** — `make guardrails`: exception-guard now **PASS** on the diff.
  Full run: 112/113 targets pass; the sole failure is
  `local-gcp-kernel-parity-guard` (target #113, last in the chain), whose
  self-test shells `docker compose config` and fails without Docker. I ran the
  same target on a clean `origin/main` (`756f7dadf`) worktree: **identical
  failure** → environmental, not the PR. Runtime 246 s.
- **M1** — comparison arm answerable: E1 "Compare August with September
  widgets" and E2 "widgets last month compared to this month" both run 2 arms,
  0 repairs, 0 rejects, `Window: august (…) vs september (…)` printed once.
- **M2** — server window is truth: E3 plan seeds `from/to=2020` and drafts July
  for "widgets in August" → `injectWindow` overwrites, guard rejects (`window`),
  repair called once, repaired draft rejected again, exec=0 → honest partial
  ("sql: could not be retrieved."). E6 no-period question with a model-bound
  window → params stripped, executes (allowed by plan).
- **Tenant-gate refusal audited** — `tenantGateRefusal` logs at error level
  with stage+reason, counts `ceoai_tenant_gate_reject_total{reason}`, and
  `recordAudit`s a `ModeRefused` row with a synthetic `tenant_gate:<stage>`
  step. Verified **in Postgres** (not just the in-memory bridge): row present in
  `ceo_ai_assistant_audit`, `GetTrace` returns it with the step + verdict.
- **PLANNED per-tile** — `PLANNED:P<n>[kpi.x,chart.y]` now required; a bare
  PLANNED row fails ("does not enumerate"), an unlisted tile on a PLANNED page
  fails; self-test 14 cases pass; real run passes (25 surfaces).
- **Prompt bound** — `MaxPlannerPromptBytes` assertion added with headroom
  check.
- **executor.go:162** now wraps the extractor reason (`%w: %w`);
  `keywords_export.go` swallow fixed.
- Regressions: full backend `go test ./...` 291 pkgs green; `ceo-ai-eval-selftest`
  green (100 Q / 75 classes); `go test ./internal/ceoai/...` green; worktree clean.

## Round-2 shapes tried (30 static + 6 live + 6 end-to-end)
See tables above. Leaks: R17/R18/R19/R20 (function-argument query execution).
False positive: R31 (string BETWEEN). Everything structural: rejected.

---

# Round 3 — final pass on `ba0485621`

Diff reviewed: `git diff 1b7290079..ba0485621` (15 files) and the whole PR
`993628743..ba0485621` (90 files, +9980/−556) read once end-to-end for
coherence. Worktree clean; throwaway PG16 reused for the role-level proofs;
all scratch tests deleted.

## Round-3 verdict: **MERGEABLE** (via `make land-main`; remaining items are MINOR/NIT, none block)

No leak found. The allow-list holds against every operator-like / hidden-call
shape I could construct, the structural tenant conjunct from round 2 still
holds, and every real SQL shape the backend emits still passes the guard.

## (1) Allow-list attack — 38 shapes static, 7 live as `mesha_ceo_readonly`

Rejected (correct): `OPERATOR(pg_catalog.=)` (`operator(`), `ROW(` ,
`ARRAY[table_to_xml(...)]` (inner call caught), `count(*) FILTER (WHERE
table_to_xml(...) …)` (inner call caught), `EXTRACT(x FROM y)` / `OVERLAY(…
FROM …)` / `SUBSTRING(x FROM y)` / `TRIM(BOTH FROM x)` (second FROM → one
relation rule), `POSITION(x IN y)`, `CAST(x AS numeric(10,2))` (`numeric(`),
`COLLATE "C"` (double quote), `E''`, `U&''`, `$1`, `CURRENT_SETTING` bare
(banned identifier), unqualified `run_readonly_sql(` (not listed; EXECUTE is
also revoked from the role by 000001), `pg_catalog.count(` (schema ref),
`xpath(` even on a literal, `regexp_replace(`, `to_tsvector(` (so `ts_stat`
cannot be reached through `@@` either), `LIMIT 1e9` (R2 nit fixed).

Passed and confirmed harmless live as the role: `= ANY (ARRAY[1,2])`,
`'ceo_ai.v'::regclass::text` (returns the name; nothing to feed it to),
`AT TIME ZONE`, `CURRENT_USER` / `SESSION_USER` / `CURRENT_SCHEMA` (info only),
`?` and `in(1)` (Postgres syntax errors, never execute), `substring(x,1,2)`,
`trim(x)`, `CAST(x AS text)`, `interval '1 month'`, `date_part`,
`percentile_cont WITHIN GROUP`, `jsonb ->>`, string `BETWEEN` (R2-3 fixed).

**One syntactic gap, no exploit today — MINOR R3-1 (attribute-notation calls).**
Postgres lets `f(composite)` be written `composite.f`. `SELECT (v.row_to_json)
FROM ceo_ai.v v WHERE v.tenant_id='A'` and `v.to_jsonb` both executed live as
the role with `err=nil` — a function call the "ident followed by `(`"
detector cannot see. It is only reachable for functions whose single argument
is the row's composite type, and none of those execute query text or read
another relation (they serialise the caller's own already-tenant-scoped row),
so it is not a leak. Cheap hardening: in `scanTokens`, reject any
`alias.ident` where `ident` is not a column of the referenced view's schema
card (the card is already resolved by `CardForSQL`). Record as P1d debt if not
done here.

## (2) Real usage through the guard

Every natural_sql.go builder — `activeAnimalsSQL` (breed / species / shed,
scoped and unscoped), `weighingSQL`, `mortalitySQL`, `vaccinationOperatorSQL`
(±scope), `feedSQL`, `opsRiskSQL` — passes `Validate` +
`ExtractAllTenantPredicates`; `healthIssueSQL` passes `ValidateTrusted`. Driven
through `naturalSQLPlan` with 23 regex-matching questions: every SQL sub passes
(the one non-pass is `feed_weight_band_summary`, an API route with no SQL). The
goldens carry `public.*` oracle SQL that the harness runs directly, not through
the guard; the two `ceo_ai.*` oracles are plain `count(*)`/`WHERE tenant_id`
shapes and validate. The planner prompt now lists the allow-list verbatim
(`AllowedFunctions()` joined) and tells the model to use `date_part` /
`CAST`/`::text`, so the model is steered inside the list; golden re-snapshotted.

## (3) Regressions

- `go test ./...` backend: 291 packages green (cached run 12 s; cold 156 s round 1).
- `make guardrails`: 112/113 targets pass, 179 s. Sole failure is
  `local-gcp-kernel-parity-guard` (last target), which shells `docker compose
  config`; same failure on clean `origin/main` (verified round 2). Not the PR.
- `make ceo-ai-eval-selftest`: 100 Q / 75 classes OK.
- Comparison window end-to-end (my own probes, "Compare August with September
  widgets"): both arms → `Window: august … vs september …`; both arms on
  August → `Window: august … (comparison with september … could not be
  answered)`; single arm August → same honest line; single arm September →
  `Window: september … (comparison with august … could not be answered)`.
  R2-2 closed.

## (4) Whole-diff coherence

- No references to removed helpers (`ExtractTenantEquals`,
  `hasTopLevelOrInWhere`, `hasTenantScopeInWhere`) outside one historical
  sentence in `tenant_predicates.go:22` that is explicitly past tense. The
  validator's 5a/5b comments were rewritten to the "exactly one relation"
  rule; no stale "binds only the FIRST literal" text remains.
- No dead non-test helpers in `sqlguard` or `app` (checked every unexported
  func for a non-test reference).
- Plan doc D0 sqlguard row, D1.2 window text and the Chart/facts row updated
  to match rounds 2–3 (structural conjunct, server-truth window, either-arm
  acceptance, observable tenant gate). `coverage-matrix.md` PLANNED rows carry
  per-tile lists. `bypass_corpus.go` grew each round (88 → 122 → 122+43 lines)
  and each round's live-leak shapes are in it.
- Prompt golden re-snapshotted for the FUNCTIONS line; `TestPlanPromptGolden`
  and the byte-bound test pass.
- Round boundaries are clean: round-2's deny-list comment block in
  `validator.go` was replaced by the `functions.go` header, and
  `bannedFunctions` is kept deliberately as belt-and-braces with a comment
  saying so (not a leftover).

## Remaining findings (none blocking)

- **MINOR R3-1** attribute-notation function calls bypass the `ident (`
  detector (no exploitable function today; harden or file as P1d).
- **NIT R3-2** a comparison plan whose two arms bind the same window composes
  two identical fact lines ("Deaths: 7." twice) above the honest Window line;
  dedupe identical facts across arms or drop the duplicate arm.
- **NIT (carried)** planner prompt still embeds the session tenant UUID and
  park UUIDs as literals (pre-existing, P1d note).
- **NIT (carried)** `mesha_cube_readonly` keeps `public.*` SELECT by design;
  plan should track it as the one remaining public reader (it does now, in the
  000359 header; add to the plan's D0 table for visibility).

## Final tally across rounds

| Round | Blockers found → fixed | Majors → fixed | Verdict |
|---|---|---|---|
| 1 | 2 (guard bypass ×6 live; guardrails red) → 2 | 2 (compare arm; model window) → 2 | NOT MERGEABLE |
| 2 | 1 (function-argument SQL ×4 live as role) → 1 | 1 (Window line lies) → 1 | NOT MERGEABLE |
| 3 | 0 | 0 | **MERGEABLE** |

Land only via `make land-main` on a clean worktree rebased on fresh
`origin/main`; the parity-guard failure is environmental and needs Docker
present in the landing environment for the receipt to be green.
