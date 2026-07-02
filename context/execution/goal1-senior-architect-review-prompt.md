# Goat OS — Full Senior-Architect Review (last 15 commits + kernel + vaccination slice + contracts + E2E)

You are the ORCHESTRATOR. Run an adversarial, READ-ONLY senior-architect review of
the current codebase and the last 15 commits. Output is a FINAL BUG LIST ONLY —
do not describe what is good; only what is wrong, missing, risky, or overclaimed.

Two phases:
- PHASE 1: spawn the 9 FINDER agents below IN PARALLEL (one message, multiple
  Agent calls, general-purpose, read-only). Finders are BLIND (no known-bug
  hints) — they hunt fresh. Each gets §A + its own scope.
- PHASE 2: after all return, run ONE SYNTHESIS+VERIFY pass (§C) that gets the
  finder outputs + the SEED LIST (§B), reconciles contradictions by reading code
  at file:line, confirms seeds, de-dupes, ranks, and writes the final bug list.

Relay nothing raw to me — only Phase 2's final report.

================================================================================
## §A SHARED CONTEXT (verbatim to every finder)
================================================================================

ROLE: Adversarial senior architect. Find bugs, uncovered edge cases, contract
drift, security/idempotency/scale defects, and doc↔code overclaims. System must be
robust, replay-safe, extensible for FUTURE modules (feed/breeding/procurement/HR),
and correct at 1,000,000 goat operations. READ-ONLY. Cite EVERY finding as
file:line. Do not trust docs, ledgers, commit messages, memory, or your own
assumptions — confirm against actual code. Distinguish "constant/field/route
exists" from "wired + emitted + consumed + enforced end-to-end." Report ONLY
problems — no praise, no "this is correct" filler.

REPO: /Users/ravi/mesha/goatos (Go backend backend/, admin-web apps/admin-web,
contracts contracts/, generated clients packages/). First run:
`git -C /Users/ravi/mesha/goatos log --oneline -15` and
`git -C /Users/ravi/mesha/goatos diff --stat HEAD~15..HEAD`
— the last 15 commits are the primary review target; recent work claimed all
blockers closed. Assume nothing closed until verified.

LOOKUP ORDER (mandatory, token-efficient):
1. CRG — code-review-graph MCP, repo_root /Users/ravi/mesha/goatos:
   detect_changes(base=HEAD~15) and get_affected_flows + get_impact_radius for the
   diff; get_architecture_overview → semantic_search_nodes → query_graph
   (callers_of/callees_of/tests_for) for structure.
2. Graphify docs (intended behavior + business truth):
   - Mesha wiki / handbooks / SOPs / diagrams:
     uvx --from 'graphifyy[mcp]==0.8.44' graphify query "Q" --graph /Users/ravi/mesha/graphify-out/graph.json
     visual: /Users/ravi/mesha/graphify-visuals-clean-out/graphify-out/graph.json
   - goatos docs: same CLI, --graph /Users/ravi/mesha/goatos/graphify-out/graph.json
3. Legacy parity floor (each has CRG + <repo>/graphify-out/graph.json):
   dashboard/ , vgoats-dashboard/ , procurement_app/ , slack-automation-scripts/
4. Grep/Read ONLY for blind spots: SQL strings, constants, HTTP route strings,
   reflective wiring, OpenAPI/JSON-Schema files, uncommitted code, exact caller
   counts. (CRG embeddings may be off → keyword only; Go backend is the big
   "sqlc-goat" community — read real Go/SQL.)

INTENDED-BEHAVIOR BASELINE (the contract, not a hint):
- context/architecture/operational-kernel.md and operational-kernel-system-design.md
- docs/protocol-engine/{state-machines.md,obligation-engine.md}   (SM-1..SM-7)
- docs/preventive-care-vaccination/{PRD.md,TRD.md,V1-FOUNDATION-SPEC.md}
- context/execution/vaccination-kernel-closure-business-backlog.md  (team CLAIMS — verify each)
- context/frontend/{current-admin-web-scope.md,vaccination-kernel-closure-screen-requirements.md,
  final-frontend-mobile-backend-architecture.md}
- AGENTS.md "Golden frontend rule" (backend owns nav/labels/columns/chips/actions/
  empty+error copy/disabled reasons; frontend owns layout/CSS/local UI state only)
  and the Config/admin_ui_config_entries/bootstrap rules.

KERNEL CONTRACT (Postgres = ONLY truth):
EVENT SPINE: goat event → ONE atomic txn {domain row + audit + outbox} (never
row-without-event / event-without-row) → outbox relay polls/publishes/retries →
Pub/Sub at-least-once + DLQ → consumers idempotent + replay-safe.
TIME SPINE: Cloud Scheduler → sweeper scans BOUNDED window, marks
due/overdue/missed, batches shed-wise, Cloud Tasks near-term only (lost task
RECREATED from Postgres), refreshes projections. Far-future work in Postgres.

VACCINATION FLOW: Config → Due List → Shed Drive → SOP Execution → Proof →
Verification → Completion → Alerts, satisfying SM-1..SM-7.

SENIOR-ARCHITECT LENSES on every finding:
- 1M scale: bounded/indexed/keyset/partition, no full-herd scan, bounded
  goroutines, query-plan sanity on hot paths.
- Replay/at-least-once: exact replay = no new effects; same-key different-payload
  rejected (`ON CONFLICT DO UPDATE SET idempotency_key` alone INSUFFICIENT);
  every API/worker/importer/server-action/webhook idempotent.
- Security: tenant scoping on every query; RBAC/permission check on every mutating
  route incl config/publish; no client-supplied trust (hashes, ids, totals);
  no secret in logs; signed media URLs (API never proxies bytes).
- Contract ownership: backend owns visible nav/labels/columns/filters/actions/
  disabled reasons/empty+error copy; flag anything hardcoded in frontend that
  should be a backend contract; flag OpenAPI↔generated-client↔server drift.
- Extensibility: generic kernel vs vaccination-hardcoded coupling.
- Legacy parity: does the kernel/slice regress any vaccination/health/shift/alert/
  arrival/proof edge the legacy repos handled?

FINDER OUTPUT (problems only):
A) Findings CRITICAL→LOW: `file:line — SEVERITY — problem — why (1M/replay/security/
   contract/tz/concurrent) — fix` + a concrete failing scenario each.
B) Edge-case GAPS only: case | partial/missing | evidence file:line. (Omit cases
   that are fully correct.)
C) doc↔code overclaims.
D) legacy-parity regressions.
No finding without file:line. Verify before claiming.

================================================================================
## §A.1 FINDER SCOPES (spawn all 9 in parallel; each also gets §A)
================================================================================

FINDER 1 — LAST-15-COMMITS regression + blast radius.
Run detect_changes(base=HEAD~15) + get_affected_flows + get_impact_radius. For
every changed function: incomplete fix, new idempotency/replay regression, missing
test for the changed path, contract drift, broken invariant, off-by-one, error
swallowed, tenant/permission check dropped. Read the actual diff
(`git -C /Users/ravi/mesha/goatos diff HEAD~15..HEAD`) for SQL/route/constant
changes CRG can't see. Flag any commit that claims a fix but leaves a hole.

FINDER 2 — EVENT SPINE atomicity + outbox + consumer idempotency.
Every mutating goat op (create/procure/shift/exit/stage-change/vaccination-
complete) writes {domain row + audit + outbox} in ONE txn — find split-txn windows
(row-without-event / event-without-row). Relay: claim/lease (FOR UPDATE SKIP
LOCKED), ordered, mark-after-success, retry/backoff, DLQ replay+discard. Consumer:
dedup per event_id co-transactional with side effects. Idempotency key + semantic
fingerprint same txn; same-key different-payload rejected. Verify each domain event
has a real producer→outbox→consumer path (not just a constant).
Files: backend/internal/{identity,movement,locations,vaccination,vaccinationexecution,
outbox,domainconsumer,processintegrity,operationsaudit},
backend/cmd/{outbox-relay,outbox-dlq,domain-event-consumer,idempotency-key-sweeper}.

FINDER 3 — TIME SPINE sweeper + missed-detection + reminders + escalation.
Bounded indexed window, keyset/cursor, bounded batch, lease vs double-process;
shed-wise batching (45 due → 1 drive). Missed/overdue: DURABLE visible exception
or only read-time/log? Do all surfaces agree? Reminders durable + recreatable +
idempotent. Escalation waterfall owner→manager→higher, durable rows, ack/resolve,
incident adapter actually routed (not slack-only). Notifier dedupe on replay;
per-location timezone day boundaries (flag server-tz math). Projection refresh
idempotent + bounded.
Files: backend/cmd/{obligation-sweeper,sweeper,calendar-reminder-sweeper,
calendar-escalation-sweeper,notification-dispatcher,calendar-vaccination-projector},
backend/internal/{obligation,calendar,notification}.

FINDER 4 — SM-1 generation + config publish + triggers.
Publish→generate via durable retryable run row; existing-cohort backfill on publish
+ new-goat on goat.created (both). Draft/retired→no work. Version select: tenant +
scope(park OR tenant-default) with PARK precedence, per-(protocol,scope) non-
overlap. trigger_type due_at incl repeat recurrence (yearly/every_n_days/until_age/
after_age). Eligibility incl reproductive-exclude; defer_states→VISIBLE deferred
(not skip/missed); true-ineligible→nothing. History catch-up + missed_dose_policy.
null-DOB/null-entry handling. Idempotency hash + unique guard NULLS NOT DISTINCT,
replay=0 rows. Published rules/triggers immutable (app+db). Backfill chunked at 1M.
Files: backend/internal/{protocol,obligation,vaccination/app},
backend/cmd/{generate-vaccination-obligations,seed-vaccination-trigger}.

FINDER 5 — Lifecycle: SM-2 shift + SM-3 death/sale + defer/recovery.
Shift: re-scope pending INCL deferred, reopen at CURRENT shed, park-aware version,
no cross-vaccine repoint, dec old/inc new estimated_targets, ineligible-after-shift
cancel, ordered by occurred_at, idempotent on shift_event_id. Death/sale
(goat.exited same txn): cancel scheduled/due/in_progress, release reserved stock/
dec batch, zero active after, no double-release on death-during-drive. Defer/
recovery: deferred visible w/ reason; recovery event emitted on sick→healthy AND
consumed → deferred reopened. Trace producer + consumer.
Files: backend/internal/{obligation,domainconsumer,movement,locations,identity,vaccination/app}.

FINDER 6 — SM-4 batch + SM-5 stock + SM-7 booster.
ONE open batch per (scope,version,window); ONE sop_task; reserve at create at
boundary NEVER per-goat; supersede on regenerate; close→completed/missed/waived +
reconcile. Stock: FEFO skip expired; hard-fail on missing/insufficient/expired;
balance=Σ movements never negative; movement idempotency_key unique; one shed's
stock-out must not stall sibling sheds. Booster: only after primary COMPLETED; due
= ACTUAL administered_at + interval; idempotent; primary missed/canceled→no booster.
Files: backend/internal/{obligation,inventory,vaccination,sop}, backend/cmd/obligation-sweeper.

FINDER 7 — Execution→proof→verify→complete→rework + read-model single-truth +
extensibility + legacy parity.
Proof missing→not completable; reject→stays open + resubmit; completion keeps
protocol/rule/SOP version identity; double-complete + concurrent-verify = one
effect; media via signed URL, API never proxies bytes. Read models (Calendar/
Action Center/Control Tower/Adherence/Passport) read ONE truth for due/overdue/
missed/blocked/deferred — flag divergent computation. Extensibility: generic vs
vaccination-coupled. Legacy parity: pull vaccination/health/shift/alert/proof edges
from legacy repos and list regressions.
Files: backend/internal/{vaccination,vaccinationexecution,proof,media,sop,submissions,
verification,calendar,operationsaudit}.

FINDER 8 — Config API schema + backend contracts for frontend (admin-web).
Config is a GENERIC Admin/Data-Ops authority screen (/config), category/schema-
driven: changing category changes fields + rule_dsl; vaccination fields must not
show for feed_direction. Verify: rule_dsl is JSON-Schema-validated + versioned;
config writes are RBAC-gated (CEO/COO/superadmin) + idempotent + audited + tenant-
scoped; published config immutable. Contract ownership (AGENTS.md golden rule):
backend OpenAPI/bootstrap owns visible nav/route availability/page titles/table+
filter labels/chips/tabs/row-click params/drawer+action labels/empty+error copy/
disabled reasons/summary-vs-detail field sets — find anything hardcoded in
apps/admin-web that should be backend-owned. Verify OpenAPI ↔ generated client
(packages/) ↔ server handler agree (no drift); contract-drift/OpenAPI checks
actually cover it. admin_ui_config_entries must not relabel live module-DB options
(parks/sheds/breeds/SOP labels/roles) or override semantic metadata
(publishability). No frontend local defaults later overwritten by async config.
Files: backend/internal/{adminui,protocol,permissions,sop}, contracts/, packages/,
apps/admin-web/ (bootstrap consumer + generated client usage).

FINDER 9 — E2E checklist + test/seed coverage.
Locate the E2E checklist + integration/E2E tests + seed scripts (grep "e2e",
"checklist", *.md in docs/context; cmd/seed-*; load-tests/; *_integration_test.go;
contract-validation). Assess whether the checklist + tests actually exercise the
real edge cases: draft-no-work, history-no-dup, defer+recovery, shift-rescope,
death-cancel, overdue/missed, stock-block, proof-missing, reject-rework, rule-
version, booster, replay/idempotency (first call / exact replay / same-key diff
payload / downstream dup), DLQ replay+discard, escalation waterfall, timezone,
1M-scale skew. List every edge with NO test and every checklist line that is
asserted but not actually backed by a test or a runnable lane. Flag tests that
assert on mocks/stubs instead of real behavior.

================================================================================
## §B SEED LIST (Synthesis pass ONLY — never to finders)
================================================================================
Recently-claimed fixes + prior-pass suspicions. Synthesis must RE-VERIFY each at
file:line (still real / fully fixed / partial / mis-stated) + flag any finder that
missed it. New issues are not capped by this list.
Recent-commit fix claims to confirm hold (and have tests):
- Herd import commit bound to the previewed CSV (not a re-derive).
- Commit idempotency namespace derived from NORMALIZED rows, not a client-supplied
  hash. (verify no client-trusted hash path remains)
- Stale preview edits blocked with backend-owned Admin UI copy.
- Second valid stock repair on same batch/lot creates a NEW idempotent release
  (not swallowed by ON CONFLICT DO NOTHING).
- E2E checklist updated to include these exact missed cases.
Prior-pass kernel suspicions to confirm/refute:
1. Completion non-atomic + no `vaccination.completed` outbox event (5 separate
   txns; MarkCompleted writes status-event ledger but no outbox). completion.go /
   obligation repository.go MarkCompleted.
2. Sweeper aborts whole version on first shed stock-out (sweeper.go return-on-err).
3. `protocol_rules.repeat` recurrence never read.
4. `protocol_rules.catch_up` missed_dose_policy never applied.
5. birth_age + null DOB → silent skip, no visible gap row.
6. Recovery recheck IS wired (goat.health.changed emitted in-txn + GoatRecheckHandler
   consumes) — confirm any "never re-checked" claim is stale.
7. Calendar reads stale obligation_instances.status for missed (read-time divergence).

================================================================================
## §C SYNTHESIS + VERIFY (final — bad-only output)
================================================================================
Inputs: 9 finder reports + §B seeds. Steps:
1. RECONCILE every contradiction by reading code at file:line; state the verified
   answer. No unresolved conflicts.
2. VERIFY §B seeds: still-real / fixed / partial / mis-stated, each file:line + a
   concrete failing scenario. Flag seeds finders missed.
3. DE-DUPE + MERGE (same root cause = one entry).
4. RANK CRITICAL→LOW. Upgrade atomicity/security/1M-scale; downgrade by-design/
   reconstructable.
5. OUTPUT — problems only, no praise:
   A) FINAL BUG LIST, CRITICAL→LOW: `file:line — SEV — problem — why — fix` +
      failing scenario. Tag `verified` (read myself) vs `reported` (single finder).
   B) Edge-case GAPS (partial/missing only) | evidence file:line.
   C) doc↔code overclaims (closure ledger items 1-9 + CEO promise + wiki/handbook/
      SOP claims that overstate the code).
   D) Contract/Config defects: OpenAPI↔client↔server drift, frontend-hardcoded
      contract violations, config RBAC/idempotency/immutability gaps.
   E) E2E/test coverage gaps: every must-cover edge with no real test; checklist
      lines not backed by a runnable lane.
   F) Legacy-parity regressions.
   G) Last-15-commits defects: incomplete fixes / regressions introduced.
   H) Top fix order (just the ordered list + one-line rationale each).
Be exhaustive. Nothing claimed without file:line. Resolve every contradiction.
Do NOT report what is good.
