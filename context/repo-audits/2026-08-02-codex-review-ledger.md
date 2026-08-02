# Codex review ledger — 2026-08-02

Every finding raised by the Codex reviews of 2026-08-02, with an independently
verified verdict for each, what landed, and what is still open. Written so a
fresh session can pick this up cold.

## Where the work is

The QA session ran in a secondary **git worktree of this same repo** (a sibling
`goatos-main-qa` directory next to the normal `goatos` checkout). Confirm with
`git rev-parse --git-common-dir` — it points at the main checkout's `.git`. Same
remotes, same `origin/main`. Nothing is hidden there: every commit below is on
`origin/main` and visible from the normal checkout. Only **uncommitted** work
sits in the worktree directory.

| SHA | What |
|---|---|
| `6d40975d8` | session start |
| `0e5e325fc` | 15 of the first 18 findings + notification tap targets |
| `bc949d6059a3` | notification copy + specificity guard |
| `b5fc97ebebfc` | **current `origin/main`** — verifier per-feature nav, mandatory permission gate, overdue rollover |

Uncommitted in the worktree at time of writing (23 paths): the calendar headline
work (REJECTED, see below), the tenant-grant authz fix, FCM token pruning, the
expired-session copy, and the verifier video-control change.

---

## Review 1 — the original 18 (B01–B18)

Reviewed `HEAD~10..HEAD`. **17 real, 1 refuted.** All 17 landed.

| ID | Finding | Verdict | Notes |
|---|---|---|---|
| B01 | Park-scoped leader can close another park's campaign | **REAL (P0)** | Weighing never called `ResolveAuthorizedParkScope`; sibling `vaccinationexecution` did. Fixed, then **re-opened by review 2** — see NEW-1 below |
| B02 | Migration can make historical evidence overwritable | REAL | Forward migration `000070`, backfills `submitted_at` from `audit_log` |
| B03 | Historical verdicts reset to pending | REAL | Forward migration `000071` |
| B04 | Late older proof supersedes its newer replacement | REAL | Supersede now ordered by `created_at` authorship, not completion order |
| B05 | Rework creates work the operator cannot finish | REAL | Root cause shared with B06 |
| B06 | Edited draft keeps a stale approval | REAL | **Same root as B05**: no round/version on the observation↔verification binding |
| B07 | Normal close accepts empty, unsubmitted buckets | **REFUTED** | Documented intended behaviour, audited via `NotAcceptedCount`. Not a bug |
| B08 | Closed buckets cannot be reopened | REAL | **Same root as B12** |
| B09 | Reopen/rework leave kernel work terminal | REAL | `ReactivateWorkItemsForBucket`, called inside the owning transaction |
| B10 | Multi-chunk cadence silently loses notifications | REAL | Idempotency key now folds a bucket-set digest; proven with 210 buckets at chunk=200 |
| B11 | Pre-`000059` campaigns never enter the kernel | REAL | Forward migration `000069` |
| B12 | Capture can resurrect closed work | REAL | Weak predicate `status <> 'completed'` at **4 sites**, not the 2 reported |
| B13 | Campaign-close notifications omit operators past 100 | REAL | Uncapped query for fanout; capped list kept for the audit sample |
| B14 | Admin reports verification blockers as cleared | REAL | Hardcoded `proofPending: 0` |
| B15 | Admin "captured" total reads a dead column | REAL | `weighing_expected_animals.status='weighed'` is never set; now from `weighing_observations` |
| B16 | Verified buckets cannot be closed from Android | REAL, **misdiagnosed** | A Close control existed and was shown *unconditionally*; the defect was the missing gate, not a missing control |
| B17 | Park filtering prevents Android pagination | REAL | Prefetch compared filtered index against unfiltered size |
| B18 | `closed` missing from API and web state machines | REAL | Added to both OpenAPI enums, regenerated client, TS unions, tone/label maps |

**Found while fixing, not in the report:**
- Migration `000071` used `MAX(uuid)` — **broke every Postgres test in the repo**
  until fixed. Invisible to `go build`; only fresh containers hit it.
- `000071` also mapped `vi.status='rework'`, a status `verification_items` never
  holds (it is `pending|approved|rejected`), so the bounced half repaired nothing.
- The `nav-composition` guard scans exactly one Go file and no Kotlin — the
  anti-pattern it exists to block shipped past it in Kotlin.

---

## Review 2 — "20 pending"

Reviewed `bc949d6059a3`, **one commit behind** `origin/main` (`b5fc97ebe`), which
already contained the verifier nav, permission gate and rollover fixes.

**Two structural problems with the review itself, both verified:**
1. Its Android paths do not exist. It cites
   `apps/android/core/data/src/main/kotlin/com/goatos/core/data/WeighingRepository.kt`.
   The real tree is `apps/goatos-android/core/core-data/.../sg/mesha/goatos/...` —
   wrong module *and* wrong package root.
2. It states it could not run Android tests (no JDK). Every Android claim is
   therefore unexecuted speculation against files it never opened.

That does **not** make the underlying behaviours automatically false — but the
count "20 pending" includes items fixed one commit later and items cited against
non-existent files, so it should not be used as a work queue as-is.

### Verified by me

| # | Finding | Verdict |
|---|---|---|
| NEW-1 | Mixed permissions bypass park boundaries (`service.go`) | **REAL — fixed** |
| NEW-2 | Park-scoped supervisors see every park's weighing evidence (`leadership_sheds_page.go`) | **Probably not a bug** — the file documents itself `scope=tenant_id only, because this surface is monitor-authority (whole tenant)`. Whether a *park-scoped* supervisor should reach it is a product question, not an oversight |

**NEW-1 detail (the important one).** `checkParkScope` gated cross-park mutation
on `HasTenantWideGrant(grants, tenantID)`, and that helper checks **scope only**:

```go
if grant.ScopeType == "tenant" && grant.ScopeID == tenantID { return true }
```

It never checks which **role** the tenant-wide grant carries. So a person with an
unrelated tenant-wide role (e.g. `health_director` for counts) plus a park-scoped
weighing grant was treated as tenant-wide **for weighing** and could mutate any
park — silently defeating B01 from inside the helper B01 depends on. Also
`len(grants) == 0` returned authorized, i.e. fail-open.

Fixed with `hasWeighingAuthorityTenantWide()`; empty grants now fail closed.
**Caveat to revisit:** the fix uses a role whitelist (`growth_director`,
`ceo_internal`), so a future director role silently loses tenant-wide weighing
access until added. Permission-derived would be better than role-listed.

**Other `HasTenantWideGrant` callers audited, NOT fixed** — same latent shape in
`vaccinationexecution` (`adapters/postgres/repository.go:32`, and
`adapters/http/handler.go:653,862,1373`). Read-only scope filtering, so lower
severity, but it is the same defect class and is still open.

### Adjudicated against the current tree (`b5fc97ebe`)

Each verdict below was checked file-by-file at real paths. **Wrong paths did not
mean wrong behaviour** — several Android claims are confirmed once the real files
are found. The count "20 pending" is still wrong, but mostly because items are
refuted or already fixed, not because they were fabricated.

**12 CONFIRMED and open.** Priority order:

| # | Finding | Evidence |
|---|---|---|
| NEW-3 | **Vaccine-name push wiring is fully dead.** `notificationbridge/vaccine_labels.go` selects from `vaccination_rules` — **that table does not exist in any migration**. Worse, `verification_notify_consumer.go:721` passes `category` (the constant string `"vaccination_proof"`, from `sopbridge/vaccination_submission.go:22`) into a `::uuid[]` cast, which errors on non-UUID input. Both failures are swallowed (`if err != nil { return out }`). Every vaccination approval push silently falls back to generic wording — **this quietly reverts today's "name the vaccine" mandate**. Real source is `ceo_ai.vaccine_label_for(dose_code)`, keyed by dose-code prefix | highest impact |
| 7 | **Draft campaigns leak into the kernel.** `000069:79-84` filters only `cs.status NOT IN ('canceled')` — **no `c.status` filter**. `CreateCampaign` inserts `weighing_campaign_sheds` at draft time (`repository.go:129,252`), so the backfill materializes work items, cadence and notifications for campaigns nobody published. Fix: `AND c.status <> 'draft'` | |
| 6 | **Reopen strands a bucket permanently.** `ReopenScope` (`repository.go:1936-1945`) updates only `weighing_campaign_sheds.status`, never the parent. Both capture gates (`repository.go:1521`, `:2863`) require the campaign in `published/in_progress/delayed`. So after `CloseCampaign` sets `closed`, reopening a bucket flips it to `in_progress` and capture still returns `ErrImmutable`, with no admin recourse | |
| 9 | **Admin fabricates "linked".** `page.tsx:314` — `!row.readyToClose && row.proofPendingCount > 0 ? "pending" : "linked"`. No `submittedCount`/`hasProof` exists in `data.ts`. Zero submitted proof ⇒ `proofPendingCount = 0` ⇒ renders `tone="ok" / "linked"`, indistinguishable from genuinely verified | |
| 10 | **Captured totals are a boolean.** `data.ts:429` — `const completedCount = shed.status === "completed" ? 1 : 0`, rendered as "{completedCount} captured" (`page.tsx:308`) | |
| NEW-6 | **Admin ignores the operator name the API sends.** Backend resolves `operator_display_name` (`domain/types.go:217,313`, `repository.go:849`); `data.ts:361-364` ignores it and re-derives via a client-side catalog lookup, so `page.tsx:469` prints "Operator not reported by API" **when the API did report it** | |
| NEW-8 | **Non-default park breaks task creation** (not in my earlier list). `page.tsx` is a pure server component — the park radios have no `onChange`/router wiring, and `planner.sheds` is fetched for exactly one park (`data.ts:281-291`). Selecting another park changes only the radio's visual state; `validSelectedShedIds` (`data.ts:19-29`) then filters to empty and submit fails | |
| NEW-5 | **>500 sheds silently truncated.** `data.ts:197-209` hard-caps at 5 pages of ~100 with no banner; the loop `break`s on the cap, not on cursor exhaustion | |
| 11 | **Close unreachable — NOT stale after all.** `WeighingScreen.kt:206-212` — `canClose = readyToClose && status == "completed"`. But `CloseScope`/`AbandonScope` are explicitly built to close **any non-terminal** bucket gated only on zero pending verification. The extra `status == "completed"` means leadership can never close a stalled, never-submitted bucket — contradicting `AbandonScope`'s stated purpose | |
| 13 | **Android drops closed campaigns.** `WeighingRepository.toAssignments()` — `status in setOf("published","in_progress","delayed","completed")`; `closed` missing, so those buckets vanish. Sibling `toTask()` does not filter, and `WeighingTasksScreen.kt:436,443` renders a `"closed"` color — so the app does model it elsewhere | |
| NEW-7 | **Captured-at uses the device timezone.** `VerifyDetailScreen.kt:741` — `formatCapturedAt(row.value, locale, ZoneId.systemDefault())`. Violates the IST business-day mandate; wrong date near midnight on any non-IST phone. (No literal "IST" label is rendered, so Codex's exact wording is off — the defect is real.) Fix: `ZoneId.of("Asia/Kolkata")` | |
| NEW-1 | Mixed permissions bypass park boundaries | **already fixed today**, see above |

**4 REFUTED — do not reopen without new evidence:**

| # | Finding | Why not |
|---|---|---|
| 2 | `000070` mis-stamps drafts as submitted | Backfill only touches rows with an `audit_log` row where `action IN ('weighing.observation_accepted','weighing.scope_closed')` (`000070:28-40`). Those are written only at real accept/close (`repository.go:1490,1493`, `close.go:133`). A draft never has one |
| 5 | Delayed approval hits newly edited evidence | `RecordVerdict` (`verification/adapters/postgres/repository.go:1183-1213`) is `... AND row_version = $6 AND status='pending'`; mismatch returns `ports.ErrConflict` (409), distinguished from not-found. `RowVersion` is mandatory input (`app/service.go:217,255`). The optimistic-concurrency guard is real and wired |
| NEW-4 | Leadership push opens an assignee-only screen | Every leadership recipient gets a module overview (`approvedTarget: "/vaccination"`, `/weighing`, `/feed`, `/counts`), distinct from the verifier's `/verification/items/{id}`. `PushTargetResolverTest` asserts `"leadership_close"` resolves to `null` → home |
| NEW-2 | Park-scoped supervisors see every park's evidence | `leadership_sheds_page.go` documents itself `scope=tenant_id only, because this surface is monitor-authority (whole tenant)`. Whether a park-scoped supervisor should reach it is a product question, not an oversight |

**1 ALREADY FIXED:** #3 (`000071` shed aggregate) — the migration's own comment
documents and fixes the exact `vi.status='rework'` defect cited; the shipped
aggregate maps `'rejected'→'rework'`, groups by `(tenant_id, campaign_shed_id)`
(1:1 via the UNIQUE constraint), and only updates rows still `'pending'`.

**3 NOT CLOSED — treat as unknown, not as refuted:**

| # | Finding | State |
|---|---|---|
| 4 | Two simultaneous videos both become "current" | Lump-sum path is protected by `weighing_shed_observations_one_open_scope_uidx` (unique on `(tenant_id, campaign_shed_id) WHERE withdrawn_at IS NULL`), `23505` → domain conflict (`repository.go:1670-1675`). **Individual-observation race not verified** |
| 8 | Late capture lands after close | `CloseCampaign` takes `FOR NO KEY UPDATE` on the campaign row and every non-terminal bucket before the pending-evidence gate, explicitly to close this write-skew window. **`CloseScope` (single-bucket) not checked for the equivalent lock** |
| 12 | Park-filter pagination starvation | **Not reached.** No verdict — silence here is not a refutation |

---

## Calendar: submitted drive shows as "Missed"

Separate report. Symptom: all operators submitted, only verification remains,
card renders red **Missed** with `submitted=120, overdue=120, completed=0`.

**Count half — STALE.** The submitted/overdue double-count is already fixed on
`origin/main`: five disjoint buckets, `overdue = NOT submitted AND NOT deferred
AND status IN ('overdue','missed')`, with `total = completed+submitted+due+
overdue+deferred`. Today's code cannot produce `submitted=120 AND overdue=120`.

> **Unexplained and worth chasing:** the screenshot therefore came from a
> deployed build (or cached response) that is not fresh `main`. Check the
> deployed SHA and the raw calendar API response.

**Headline half — REAL, still open.** `canonical_read.go`:

```
1140:  WHEN grouped.has_missed THEN 'missed'
1141:  WHEN grouped.has_review OR submitted_count > 0 THEN 'verification_pending'
1150:  WHEN grouped.has_missed OR grouped.has_overdue THEN 'critical'
 704:  bool_or(status = 'missed') AS has_missed
```

`has_missed` ignores `submitted_for_verification`, so a fully submitted drive
still reads missed/critical.

### First attempt REJECTED — two P1 regressions (both verified)

The patch gated the flags on `obl_summary` counts. That is wrong:

1. **The summary CTE is optional.** It is built only under
   `WHERE current_setting('goatos.include_drive_summary', true) = 'true'`, and the
   HTTP default is false (`handler.go:265`,
   `query.Get("include_drive_summary") == "true"`). On the default path the
   counts are 0, every new condition evaluates false, and the CASE falls through
   to `'scheduled'` — **genuine missed work renders as "Scheduled"**. A false
   green is worse than the false red we started with.
2. **`'overdue'` is not a legal obligation status.** Baseline CHECK
   (`000001_..._baseline.sql:3550`) allows only
   `scheduled, due, in_progress, deferred, completed, missed, waived, canceled,
   superseded`. So `overdue_count` can only ever match `'missed'`, while
   `grouped.has_overdue` is a **read-time** condition (scheduled/due past its IST
   date). Gating one on the other suppresses ordinary past-due work.
3. The new Postgres test also fails: `vaccination_completions.goat_id` is
   populated with the **obligation** id (FK violation), the three subtests share
   one park/date so fixtures accumulate 120→240→360, and it only exercises
   `IncludeDriveSummary: true` — never the default path where regression 1 lives.

### Required approach

Derive **always-on** effective flags at one consistent obligation grain,
independent of the optional summary:

```
has_submitted   = any obligation with submitted_for_verification
genuine_missed  = status='missed' AND NOT submitted_for_verification
genuine_overdue = past its IST business date, still open, AND NOT submitted
                  (read-time semantics, NOT the phantom 'overdue' status)
```

Precedence: `genuine_missed` → missed/critical; else `has_submitted` or
`has_review` → verification_pending/**warning**; else `genuine_overdue` →
overdue/critical; then in_progress / completed / scheduled / deferred.

A mixed drive (100 submitted + 20 genuinely missed) **must still read
missed/critical** — do not simply reorder the CASE.

Tests: isolated fixtures (distinct park and/or date per case, so they cannot
accumulate) covering all-submitted, mixed, genuine past-due scheduled/due,
completed, and **both** `include_drive_summary=true` and `false`.

---

## Found by device E2E, not by any review

Every one of these passed unit tests, all guards, and a green `ci-local`.

| Finding | Severity |
|---|---|
| Permission gate required `ACCESS_FINE_LOCATION`, but the manifest caps it `maxSdkVersion=30` → **on any modern phone the gate can never be satisfied and the operator is locked out of the app** | worst of the night |
| `context as? Activity` is always null in Compose (`LocalContext` is a `ContextThemeWrapper`) → crash on launch, then a dead button, then the "don't ask again" branch never fired | three symptoms, one line |
| Permission launcher created inside an `if` (illegal in Compose) → "Grant permissions" silently did nothing | |
| Verifier drawer navigated to the module href, which was plain `/verify` → every feature landed on vaccination | |
| Adding `?module=` made the href stop matching the L0 root set → **drawer and bottom bar silently disappeared** | |
| Nav emitted `?category=pc.vaccination` while the endpoint filters `vaccination_proof` → HTTP 200 with an empty list, i.e. a tab that looks permanently empty rather than broken | |
| Expired session renders as "Couldn't load your workspace. Check your connection" — blames the network for an auth failure | |
| 8 of 11 FCM sends returned 404 UNREGISTERED; nothing prunes dead tokens | |

---

## Still open

- Calendar headline (rework in progress, approach above)
- Review-2 items 2–13 and NEW-3..NEW-7 (validation pass incomplete)
- `HasTenantWideGrant` same defect class in `vaccinationexecution`
- Role whitelist in the NEW-1 fix should become permission-derived
- `#31` DB-driven capabilities, `#28` Showkase, `#30` motion polish,
  233 baselined design literals
- 17 defects in `context/repo-audits/2026-08-01-weighing-alerts-audit-findings.md`,
  never triaged
- Android regression tests do not exercise real routing or pagination — fair
  criticism from review 2, and it applies to tests added today

## Working notes for the next session

- **Always `git fetch` and review against fresh `origin/main`.** Both reviews
  today were produced against stale trees; shifted line numbers are the tell.
- **A stale API binary caused three wrong conclusions.** Rebuild before
  concluding anything: the drift guard reports it at `/version`
  (`migration_drift: true`).
- **Never pipe `make ci-local` through `tail`** — a failure gets laundered into a
  pass by `tail`'s exit code. Same for `./gradlew` from the repo root, where no
  `gradlew` exists.
- Postgres tests are opt-in: `GOATOS_RUN_POSTGRES_TESTS=1`. They start their own
  container; a plain `go test` **skips them silently**.
- Android: `:app` is flavored (`compileStgDebugKotlin`); feature modules are not.
  One APK = one identity, so each role needs its own build; `pm clear` wipes
  runtime permissions.
- QA stack: API `127.0.0.1:8080`, disposable DB
  `postgres://postgres:goatos@127.0.0.1:15544/goatos`. **Never touch port 5433.**
- FCM sender is **`goatos-stg`**, not `goatos-prod` (`goatos-prod` returns
  `403 SENDER_ID_MISMATCH`).
- Roughly 8 of 9 agent "done" reports today were wrong in some material way —
  false-green compiles, skipped tests, migrations never applied, fixtures that
  swallowed errors, unrelated test output pasted as proof. Verify by running the
  named command yourself.

---

## How to run the E2E in the next session

### 1. Get the code

Either checkout works — they share `.git`:

```bash
git fetch origin && git log --oneline -1 origin/main
```

Uncommitted work sits in the sibling `goatos-main-qa` worktree; list it with
`git -C ../goatos-main-qa status` (`git worktree list` shows where it is).

### 2. Throwaway QA database — never the shared one

```bash
psql 'postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable' -c 'select 1'
```

Container `goatos-phone-qa`, port **15544**. Runbook:
`docs/runbooks/phone-qa-throwaway-rbac.md`.

**Never point QA at `5433`** — that is the shared `goatos-local-current` stack
serving `:3300`/`:8080` from `origin/main`. Mutating E2E against it is banned.

### 3. Migrate before starting the API

Check `goatos_schema_migrations` actually advanced — an agent reported success
today while the table sat at `000068`.

```bash
psql 'postgres://postgres:goatos@127.0.0.1:15544/goatos' -c \
  'select version_id from goatos_schema_migrations order by version_id desc limit 5'
```

### 4. API against the QA DB

Bind `127.0.0.1:8080`, `DATABASE_URL` = the 15544 URL. Then confirm you are not
running a stale binary — this caused three wrong conclusions today:

```bash
curl -s 127.0.0.1:8080/version   # migration_drift must be false
curl -s 127.0.0.1:8080/readyz
```

### 5. Phone

```bash
adb devices && adb reverse tcp:8080 tcp:8080
```

**Verify `adb reverse` on every device before testing.** One device ran an entire
operator pass against a dead socket today and every PASS from it was withdrawn.

One APK = one identity, so each role needs its own build/install. `pm clear`
wipes runtime permissions — useful for re-testing the permission gate.

```bash
./gradlew :app:installStgDebug     # from apps/goatos-android, NOT the repo root
```

`:app` is flavored; feature modules are not. There is **no `gradlew` at the repo
root** — running it there fails, and piping through `tail` hides that.

### 6. Push notifications

FCM sender is **`goatos-stg`**. `goatos-prod` returns `403 SENDER_ID_MISMATCH`.
Send only to the three authorized devices. Expect stale tokens: 8 of 11 returned
`404 UNREGISTERED` today and nothing prunes them.

Chain to watch, end to end:

```
verification_items → outbox → kernel-worker → notificationbridge
  → notification_requests → FCM
```

Verified working today: device Approve → `verification_items` 7→8 → 106 outbox
rows published → `notification_requests` 54→106 → real FCM 200s.

### 7. Tests

```bash
GOATOS_RUN_POSTGRES_TESTS=1 go test ./internal/...
```

Postgres tests are opt-in and **skip silently** without that variable. They start
their own container.

```bash
make ci-local > /tmp/ci.log 2>&1; grep -iE 'FAIL|error' /tmp/ci.log
```

**Never pipe `make ci-local` through `tail`** — the exit code becomes `tail`'s and
a failure reads as a pass. Same trap for Gradle.

### 8. Landing

```bash
make land-main
```

Fetches, rebases, runs affected-component CI, re-checks main, pushes the exact
certified SHA. Do not hand-compose the steps.

## Past results — what is already proven, so don't re-litigate

**Confirmed working (device-verified, not inferred):**
- Approve on device → verdict persisted → outbox → worker → real FCM 200
- Verifier per-feature nav: drawer `Vaccination / Weighing / You / Sign out`,
  bottom bar `[Verify, Alerts]` per module, each landing on its own category
- Mandatory non-dismissible permission gate, satisfiable on a modern phone
- Overdue rollover: yesterday's unfinished drive appears on today's card
- Video controls reduced to play/pause for the verifier

**Refuted "P0s" — three agent reports that were wrong:**
- "Approve is broken" — it worked; the agent never checked the DB
- "Video unplayable" — the fixture was a 4KB stub, not a real video
- "Card disabled" — correct behaviour for a pending item

**Reliability note.** Roughly 8 of 9 agent "done" reports today were materially
wrong: false-green compiles, silently-skipped tests, migrations never applied,
fixtures that swallowed errors with `_, _ = pool.Exec(...)`, and unrelated test
output pasted as proof. Re-run the named command yourself before believing any
claim, including the ones in this document.
