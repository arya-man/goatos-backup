# Dashboard Post-Main Certification and Daily Production Smoke — Plan v3

Targets:

- Exact code merged to `origin/main`, exercised against an isolated OCI preview stack.
- Production dashboard `https://dashboard.mesha.sg`, exercised read-only after deployment.

Runner: the existing OCI Always Free VM and its existing 100 GB Always Free boot volume.

Date: 2026-09-21

Status: design only. No runner, timer, preview stack, database lifecycle, or notification integration is
implemented merely because this document exists.

## 1. The four layers

All four layers are required. The agent is not an optional footnote and is not a replacement for the
first three layers.

| Layer | Purpose | Coverage | Authority |
|---|---|---|---|
| 1. Static guards | Catch source, route, SQL-bind, contract, and coverage drift before runtime | Every applicable change | Deterministic gate |
| 2. PostgreSQL integration | Prove migrations, generated queries, bind counts/types, and representative API results against real PostgreSQL | Every post-main certification | Deterministic gate |
| 3. Playwright E2E | Exercise every discovered route, tab, meaningful state, desktop viewport, and mobile WebView-sized viewport | Every post-main certification; production-safe subset daily | Deterministic gate |
| 4. AI-agent review | Visually inspect failures and a rotating sample for problems no assertion anticipated | Every failed surface plus sampled passes | Advisory finding, never the sole pass signal |

The certification result is red if Layers 1–3 fail. An agent-only finding is reported with evidence and
must be converted into a deterministic regression test before the eventual fix is considered guarded.
The agent must never turn a deterministic failure green, silently waive it, or substitute a prose
opinion for an assertion.

## 2. Two separate jobs

### 2.1 Post-main exact-SHA certification

Trigger after `origin/main` changes. Coalesce rapid merges, but certify each final observed SHA; never
label a run with one SHA while executing another.

1. Poll or receive the new `origin/main` SHA and atomically claim it so only one run owns it.
2. Create a fresh worktree at that exact SHA.
3. Create a temporary PostgreSQL database from the approved staging-equivalent OCI source.
4. Apply that SHA's migrations to the temporary database.
5. Build and start that SHA's backend and admin-web preview on loopback/private OCI networking.
6. Run Layers 1–3 against the preview.
7. Run Layer 4 against all failures and its sampled passing surfaces.
8. Write an immutable receipt containing identity, coverage, results, timings, cost, artifacts, and
   cleanup state.
9. Stop preview processes and drop only the uniquely named temporary database after the receipt and
   required evidence have been written. Cleanup traps run on success, failure, timeout, and signal.

This job does not deploy, merge, approve, or modify production/staging. It certifies what already landed
on `main`. A failed certification creates a notification and evidence packet; it does not auto-revert.

### 2.2 Daily production smoke

Run once daily against the deployed production dashboard. It is strictly non-mutating:

- no submit, save, approve, delete, deploy, upload, replay, or data-edit actions;
- no direct database writes;
- read-only critical business-data parity against the staging-equivalent OCI clone once a
  non-personal read-only STG identity is installed;
- no direct autonomous repair in production/staging;
- stop with `auth_blocked` if authentication is missing or expired;
- record the deployed version/SHA at start and end, and discard the run if it changes mid-sweep.

Production smoke uses safe navigation, tabs, read-only filters, pagination, and representative
drawers/detail overlays. Mutating interaction tests belong only in the isolated preview with disposable
data. The post-main job catches code/contract regressions; the daily production job catches deployed
environment, authentication, rendering, and availability regressions. Neither replaces the other.

### 2.3 Self-healing lane: PR only, never production mutation

Self-healing means the automation diagnoses a failure, prepares a pull request, runs deterministic
checks, runs a judge/refinement pass, and tags Ravi and Manohar for review. It does not mean editing
production/staging data, auto-merging, auto-deploying, approving its own PR, or hiding a red gate.

The repair lane may start only from a concrete failing receipt: route, viewport, screenshot/trace,
API latency sample, Lighthouse/page-load sample, Firebase/latest-app distribution check, or
business-data parity drift. It must attach that receipt to the PR description and add or update a
deterministic regression guard before asking for review.

Required reviewer tags: Ravi and Manohar. Human approval remains mandatory.

## 3. Layer 1 — static and contract guards

Layer 1 runs repository-owned checks and grows mechanically with the application:

- derive admin-web routes from `app/(admin)/**/page.tsx` and compare with bootstrap/runtime navigation;
- fail when an added or removed route is not reflected in the exercised inventory or an explicit
  owner/reason exclusion;
- validate SQL placeholders against supplied arguments, generated query metadata, and API/request
  contract shapes without hard-coding today's parameter count;
- require adversarial self-tests proving the guard fails when a parameter is added, removed, reordered,
  duplicated, or given an incompatible type;
- register guards in the repository guardrail manifest and local CI;
- scan for the known production failure strings listed in Layer 3.

Expectations come from the query or contract source of truth. A rule such as “this query has 31
parameters” is prohibited because the next legitimate addition or deletion would make the guard stale.

## 4. Layer 2 — real PostgreSQL integration

Use real PostgreSQL, not a mock driver, to catch failures compilation cannot see:

- create the schema from migrations at the exact main SHA;
- load deterministic fixtures with representative non-empty rows;
- execute generated and handwritten queries through the repository/API paths production uses;
- test zero, one, and many rows plus independently composable optional filters;
- fail on placeholder/argument mismatch, scan/type mismatch, missing migration, or unexpected empty data;
- record payload size, row count, request fanout, and latency for hot endpoints.

The source database remains untouched and proves `default_transaction_read_only=on`. Only the uniquely
named temporary database may be created, migrated, seeded, and dropped. Cleanup validates the exact
database name and run id before deletion.

### 4.1 Critical STG-to-OCI business data parity

The OCI clone is useful only if the business data that drives the dashboard is fresh enough to catch
the same class of defects seen in staging. The parity check is read-only on STG and read-only by
default on OCI. It is a blocker for critical business tables:

- goats, goat identifiers, locations, shed partitions, and current herd composition;
- weighing observations/campaigns/sheds and ADG inputs;
- procurement loads/load goats;
- feed catalog, purchases, directions, and completions;
- sales deals, deal lines, and sold-animal tags;
- vaccination source facts, completions, drives, and assignment membership.

Live Bluetooth/herd-signal telemetry is best-effort for now. A mismatch there is reported but does not
block dashboard certification unless it causes page load, contract, or critical count failure.

The STG credential must be a dedicated read-only automation identity/service account with the narrowest
needed Cloud SQL/Secret Manager access plus a read-only database role. Ravi's personal `gcloud` or ADC
session is acceptable for manual investigation only; it is not an acceptable scheduled automation
dependency. If the read identity is missing or expired, the run reports `stg_read_auth_blocked`.

Sentinel parity checks include the concrete failures from Manju's 2026-09-21 notes:

- Herd Analytics CBE weekly window must match STG values and not drift in OCI.
- Castro 2 and Castro 3 purchased/current/dead/ICU reconciliation must be explainable from one
  canonical source of truth.
- Godel 2 Part 1 and Part 2 time-wise ADG must respond to the selected date range when the underlying
  weighing rows differ.
- Sold animals by weight must include deal-line average weights when sale batches have average weight
  but individual sold tags lack per-animal weights.

Parity alone is not enough when the source system is stale or incomplete. Field reconciliations from
validated farm records are configured as explicit sentinels and must be explainable by the dashboard
source of truth. The first configured sentinel is Castro 3 / partition 3 from Manju's 2026-09-21
evidence: 59 now + 6 deaths + 3 ICU + 1 sold + 1 Y1 = 70 total. A run fails if STG and OCI match each
other but cannot explain that field truth.

## 5. Layer 3 — deterministic Playwright E2E

### 5.1 Self-growing surface inventory

Route discovery is the union of filesystem routes, authenticated bootstrap/navigation entries, runtime
links seen during the sweep, and declared dynamic-route resolvers. For every reachable route, discover
and exercise page-owned tabs, safe filters/toggles, pagination, expanders, drawers, modals, popovers,
and representative detail overlays. New surfaces enter automatically and cause a coverage failure until
exercised or explicitly excluded. Additions and removals appear in the receipt; coverage cannot silently
shrink. Deterministic fixtures provide IDs for parameterized routes.

### 5.2 Required viewports and engines

- desktop Chromium: 1440x1000;
- mobile Chromium/WebView-sized: 390x844;
- WebKit smoke for high-risk routes and media/drawer surfaces.

Shard the inventory deterministically while bounding global browser/API concurrency. Every required
surface is still covered; sharding is not sampling.

### 5.3 Required assertions

Fail on:

- `backend_down` or `Admin-web contract unavailable`;
- `The board could not be loaded`;
- `Weights could not be loaded`;
- unexpected login, error boundary, blank page, stuck loading state, page error, hydration error,
  duplicate React key, or undeclared 4xx/5xx;
- clipped/overlapping labels, hidden names, off-viewport controls, impossible horizontal scrolling, or
  an unscrollable wide table;
- blank chart scaffolding, non-empty legends with no marks, false bars for missing/null values, `NaN`,
  `Infinity`, `undefined`, or raw `null` in visible UI;
- unexpected request fanout, payload-size regression, or hot-path latency beyond repository policy.

Screenshots are evidence only after they are opened and visually verified. Login, loading, stale,
wrong-route, or unrelated-error screenshots cannot count as page proof. Capture console errors, failed
requests, trace, and HAR when relevant. Sanitize bearer tokens, cookies, passwords, signed URLs, DSNs,
and secret headers before any artifact is persisted.

### 5.4 Baselines

A merge never automatically blesses a new visual baseline. Promotion requires deterministic green
results plus an explicitly reviewed intentional-change manifest. Preserve the previous baseline and an
audit trail so an accidental regression can be compared and rolled back.

## 6. Layer 4 — AI-agent exploratory review

### 6.1 What the agent reviews

The deterministic runner visits every required surface. The agent receives:

- every Layer 1–3 failure with screenshot, previous-green comparison when available, DOM/geometry
  summary, console/network summary, and assertion result;
- a deterministic rotating 10% sample of passing route-state/viewport/persona combinations;
- all newly discovered routes/components until they have produced three green runs;
- historically risky surfaces: Work Board, Weights, charts, long labels, drawers, mobile tables,
  permission navigation, and admin-web contract loading.

Sampling applies only to agent interpretation. It never reduces Playwright route coverage.

### 6.2 Required output and failure semantics

For each finding the agent returns structured JSON: route, state, viewport, severity, visible symptom,
bounding box when applicable, evidence references, confidence, and recommended deterministic assertion.
Free-form prose alone is not a valid finding. The agent rejects evidence showing login, loading, wrong
route, or stale content.

- deterministic failure: certification fails even if the agent disagrees;
- agent-only finding: report it and require reproduction/deterministic guard during the fix;
- agent timeout or budget exhaustion: preserve the Layers 1–3 result and mark agent coverage incomplete;
- authentication/infrastructure failure: `no_verdict`, bounded retry, never a false pass.

When self-healing is enabled, the agent's maximum authority is to open or update a pull request with
the deterministic evidence and a proposed fix. A second judge agent reviews the PR for correctness,
frontend performance, API latency, route coverage, and missing regression guards. The PR remains red or
draft until deterministic checks pass; it is never merged by the automation.

### 6.3 Cost and availability controls

Supported modes:

1. **Codex scheduled task**: no separately metered API invoice, but it consumes the account's Codex
   allowance and may be unavailable at the plan limit. It is suitable for exploratory daily review, not
   as the sole mandatory post-main gate.
2. **API-funded agent on OCI**: metered by model input/output. Record usage and estimated cost, apply a
   hard limit, and stop agent review cleanly at the limit while retaining Layers 1–3 results.

Initial API policy:

- use the lowest-cost capable vision model for first-pass review;
- escalate only ambiguous/high-severity evidence to a stronger model;
- default cap: USD 1 per run and USD 25 per month;
- additionally cap screenshots, image resolution, tokens, turns, retries, and wall time;
- at 80% of a cap, review failures only and suspend the passing sample;
- at 100%, stop calls and report `agent_budget_exhausted`;
- model names/prices are configuration, not hard-coded assumptions; each receipt includes the dated
  pricing snapshot used for its estimate.

Planning estimate only: failure review plus a 10% passing sample using a low-cost model is expected to
be approximately USD 0.25–0.75 per run, or USD 8–23 for a daily run. Actual spend depends on image
count/resolution, tokens, retries, and current pricing; the measured receipt is authoritative.

## 7. Performance and latency protection

The automation must not create the failure it measures:

- bound concurrency and ramp browser workers gradually;
- measure cold/first-hit traffic before the sweep and steady-state traffic after it;
- record request count, concurrency, bytes, row counts, p50/p90/p95/p99, and total run load;
- compare old fanout and new bundled endpoints using the same tenant, user, parks, date, filters, and
  page size;
- enforce repository policy: p90 <= 300 ms and p95/p99 <= 500 ms unless stricter route policy exists;
- treat normal dashboard/API reads above 500 ms as failures; allow explicitly declared long-running
  upload/import/export/proof-media endpoints to use their own policy instead of the dashboard hot-path
  policy;
- run Lighthouse/page-load checks for production and preview dashboards, recording LCP/CLS/INP,
  transfer size, JS execution time, failed requests, and route-level regressions;
- check Firebase latest-app distribution health separately from admin-web: current release visibility,
  download/install metadata where available, and any publish/distribution error;
- never hide a slow API with frontend retries, more parallel calls, or an unproved cache;
- abort production smoke if it begins causing sustained errors or abnormal load.

Agent analysis happens after evidence capture and makes no dashboard/API requests of its own.

## 8. OCI Always Free and storage guard

Use only already-available Always Free capacity. Before every run record free-tier classification,
volume allocation, disk/inodes, source/projected database size, projected artifacts, CPU, and memory.

Refuse to start if the run requires paid capacity, Oracle marks the resource non-free, less than 20 GB
filesystem headroom would remain, or the temporary database and artifacts do not fit. Never create a
new volume/backup/public load balancer, resize into paid capacity, change performance tiers, or enable a
paid service automatically.

Use one-run locking, bounded containers/processes, and failure-safe cleanup. Retain compressed receipts,
failure evidence, and representative passing summaries—not every redundant screenshot forever. Default
retention: seven days for passes and 30 days for failures, with a total size cap. Delete only validated
run-owned directories/databases. The persistent staging-equivalent source database is never cleanup
material.

## 9. Authentication, secrets, and safety

- Prefer OCI instance/workload identity or Vault over copied long-lived keys; otherwise use the
  narrowest read-only credential and document rotation.
- Secrets live outside Git and are injected at runtime.
- Redact logs/HARs/traces/screenshots before retention or notification.
- Production credentials cannot reach mutation endpoints.
- Preview credentials/disposable data cannot address production or staging mutation endpoints.
- Missing/expired auth stops the run without invented coverage.
- No agent may merge, deploy, approve, delete business data, or edit production/staging.

## 10. Receipts and notifications

Every run writes a compact immutable receipt containing:

- requested, checked-out, built, and deployed SHA as applicable, plus migration hash;
- temporary database id and cleanup proof;
- discovered/exercised route-state counts by viewport, engine, and persona, including inventory diff;
- separate Layer 1–3 results and Layer 4 sample seed/count/verdict/completeness;
- durations, latency/load, model/tokens/cost/budget, artifact hashes, and redaction result;
- authentication, infrastructure, and cleanup status.

Notify only for a meaningful product failure, authentication/infrastructure blocker, cleanup failure,
budget threshold, or required user action. A green daily production run stays quiet except for its local
receipt.

## 11. Build order

1. Route/state inventory and Layer 1 registration with adversarial self-tests.
2. Exact-SHA OCI worktree, preview lifecycle, temporary PostgreSQL lifecycle, cleanup trap, and receipt.
3. Layer 2 integration tests with deterministic non-empty fixtures.
4. Layer 3 desktop/mobile coverage, assertions, console/network capture, and artifact redaction.
5. Add critical STG-to-OCI business-data parity using a dedicated read-only automation identity.
6. Run report-only post-main certification until three consecutive lifecycle runs complete cleanly.
7. Add Layer 4 structured review with the API caps above, or Codex-plan mode with usage-limit reporting.
8. Add daily production-safe smoke after authentication and non-mutation controls are proven.
9. Enable PR-only self-healing after the first three layers and judge review are stable.

## 12. Acceptance criteria before enabling the timer

- A new route, tab, SQL parameter, or contract field is detected without editing a hard-coded count.
- Deliberate placeholder/argument mismatch fails Layer 1 or 2.
- Deliberate `backend_down`, Work Board, Weights, clipping, blank-chart, and false-null-bar fixtures fail
  Layer 3 on the required viewport.
- The agent identifies a seeded visual defect, emits structured evidence, and stays within its cap.
- Critical STG-to-OCI parity detects deliberate drift in goats, weighing, sales, procurement, feed, and
  vaccination tables while ignoring best-effort Bluetooth telemetry drift.
- API latency, page-load/Lighthouse, and Firebase/latest-app checks are present in receipts.
- PR-only self-healing can create a draft PR from a seeded failure, add a deterministic regression
  guard, run judge review, and tag Ravi and Manohar without merging/deploying.
- Killing the runner at every lifecycle stage leaves no preview process or temporary database.
- Authentication failure produces no verdict, never a false green.
- Artifact scanning proves no credential/token is retained.
- OCI readback proves no paid resource was created and required headroom remains.
- A green receipt can be reproduced from the exact recorded SHA.

## 13. Explicitly out of scope

- Android deep functional parity beyond Firebase/latest-app availability and explicitly requested
  release smoke.
- Automatic merge, deploy, approval, revert, or production/staging data repair.
- Direct autonomous production/staging healing. PR-only fix proposals are in scope only after the
  self-healing lane is enabled and remain human-approved.
- Treating screenshots or agent prose as a substitute for deterministic checks.
- Full agent review of every passing screenshot; Playwright covers every required surface while the
  agent reviews failures, new surfaces, risky surfaces, and a rotating sample.
