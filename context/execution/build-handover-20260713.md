# Build handover — remaining scope (everything except HRMS) — 2026-07-13

Start-here doc for the next session. Replaces stale mid-session chatter. Ledger
bugs are **closed**; this is the **feature/architecture build queue** Ravi wants
finished. HRMS is explicitly **out of scope** for this queue (future).

> ## ⚠️ CROSS-SESSION OWNERSHIP — READ BEFORE CLAIMING WORK
> A separate **"FCM notification system for GCP" (Mesha AI · Max)** session is
> already running and OWNS:
> - **FCM / push** (mobile `FirebaseMessaging`+`onNewToken`+token-reg+deep-link;
>   backend send-side: recipient resolution + status→`notification_requests`
>   producer + raw token). → **REMOVED from this queue.** Do NOT build it here.
> - Its **"Track A" = backend verifier slice + E2E** and **admin-web verify UI**
>   — this OVERLAPS §1 P0 items **1a (verification backend)** and **1c (admin-web
>   approve/rework UI)**. **Before building 1a/1c, confirm with Ravi / check main
>   for the Max session's landed commits.** If Max already shipped the verifier
>   backend + web verify UI, this queue's remaining unique work = **1b verifier
>   mobile app**, **P1 nav registry**, **P1 org role model**, **P2 1M cert**.
> Two coordinators pushing main = collision. Fetch origin/main and re-read this
> block before claiming any Verification/FCM work.

---

## 0. Repo state at handover

```text
repo:         /Users/ravi/mesha/goatos
origin/main:  018dfab4   (re-fetch first: `git fetch origin main`)
```

**First 3 commands in the new session:**
```bash
cd /Users/ravi/mesha/goatos
git fetch origin main && git checkout main && git reset --hard origin/main
git log --oneline -8            # sanity: MOB-002 + lens skills + guards present
```

### Landed but NOT integrated — 4 wip branches (integrate FIRST)
Built in isolated worktrees this session, verified green, **not on main yet**
(push auth was blocked in the building session):

| Branch | SHA | What | Gate |
|---|---|---|---|
| `wip/sc2-bulkstatus` | `b7c20204` | bulkstatus worker N+1 → batch UNNEST/CASE (per-pass BumpJobCounts + per-row marks → O(4)) | build+tests+scale-guard PASS |
| `wip/sc2-identity-admin` | `02c6832c` | identity `admin_goat` + sop 4 review/submission fanouts → bulk UNNEST upsert | build+tests PASS |
| `wip/sc2-obligation-fanouts` | `cb0eb45d` | obligation sweeper 10 fanouts → UNNEST/UPDATE…FROM/ANY + Batch interfaces | verify before merge |
| `wip/gcs-validate` | `fdc6fd0a` | GCS V4-sig unit test + fake-gcs-server round-trip integration test | build+tests(-race) PASS |

**Integration procedure (do NOT let workers do this — coordinator only):**
1. `git fetch origin main`; rebase each wip branch onto fresh main.
2. Merge sequentially; after each: `make guardrails && cd backend && go build ./... && go test ./internal/...`.
3. **Centralize `tools/scale-guard/baseline.txt` edits** — remove the now-fixed
   offenders in ONE commit (bulkstatus worker 2, identity/admin_goat 1, sop 4,
   obligation sweeper entries). Baseline should drop 21 → ~14.
4. Run `make validate-migrations` (this blocked a prior scale-wave push — check
   any new migration is lock-safe / no collision).
5. Push exact accepted SHA to main via `zsh -ic 'git mesha-push main'`.

### Still running at handover (don't dup-spawn; may already be on branches)
- **1M cert** (throwaway DB only) — deferred good-to-have, not a blocker.
- **emulator-E2E-per-role** — proving MOB-002 flows per role on emulator-5554.
  Check `/private/tmp/claude-501/.../tasks/a8a357f99f81b9c18.output` for verdict.

---

## 1. THE BUILD QUEUE (priority order)

### P0 — Verification vertical (the big one: fully designed, ZERO code)
The wiki handbooks demand a **daily independent double-verification** of every
execution SOP video across every vertical. Today: `backend/internal/verification/`
= just `.gitkeep`. This is the largest designed-but-unbuilt gap.

**Design docs (read first):**
- `context/architecture/verification-module-design.md` — generic backend
- `context/architecture/verifier-app-and-flow.md` — verifier mobile app + flow
- `context/architecture/org-role-model.md` — role truth table (Capture/Verify/Act)

**1a. Backend generic Verification module**
- New `backend/internal/verification/` (hexagonal: domain/app/ports/adapters).
- `verification_item` store — module-agnostic: `{vertical, module, category,
  media[], status, verdict, reason, operator, shed, park, captured_at}`.
- Producers emit items: vaccination first (on proof submit), then feed/diagnosis/
  death/breeding register their category into a **type registry** (plug-and-play).
- `POST /verification/items/{id}/verdict {approved|rejected, reason}` — reject
  requires reason. New permission **`verification.review`** (Verifier role only).
- Bounded/paginated queue read (`~20` keyset), category-filtered, media via
  streamed signed URLs (scale rules apply — no god query).
- Verdict + reason feed daily "SOP Video Double Verification" metrics (videos
  reviewed, violations flagged, penalties issued).
- Migrations: `verification_items` table + `verification.review` grant + seed
  Verifier role. Lock-safe, `make validate-migrations`.
- **Separation of duty:** capturer ≠ verifier ≠ actor. Enforce in permissions.

**1b. Verifier mobile app (standalone section)**
- New `apps/goatos-android/feature/feature-verify`.
- Verifier opens app → sees **ONLY** the verification queue, nothing else (no
  capture, no roster, no config). Category-filtered (assigned categories).
- Per item: play video(s) + context (shed/park/operator/timestamp from capture
  metadata) → **Approve** or **Reject + mandatory reason**.
- Room SSOT offline-first, ~20 keyset page, signed-URL streamed media, bounded
  memory (all mobile-anti-patterns skill rules apply).
- Nav comes from backend contract (module grant), NOT `role ==` (guard blocks it).

**1c. admin-web approve/rework UI (Act authority = frontend, NOT mobile)**
- Backend routes already exist: `POST /admin/tasks/{id}/verify|rework`.
- No admin-web page found under `apps/admin-web/app/`. Build the leadership
  review surface: Head/Director/CEO acts on flagged items — penalty/rework/
  re-assign. Matches mock fidelity (see mock rules in memory).
- **Business rule (Ravi, confirmed):** approve/rework lives on **web**, not the
  phone. Mobile is capture-only. Verifier app = approve/reject the VIDEO only;
  the authority ACT (penalty/rework) is admin-web.

### P1 — Nav = module-grant registry composition
- `backend/internal/workforce/app/bootstrap_copy.go` still binary
  `isLeadershipPrincipal(grants)` → static `operatorNavigation` /
  `leadershipNavigation`. `nav-composition-guard` **baselines** these (tracked
  debt, expires 2026-09-30).
- Build: compose nav from `department_module_grants` (mig 000148 already has the
  tables) — reusable nav chrome across modules/verticals, driven by what the
  person is granted, not a hardcoded per-role array.
- Rule (memory `goatos-nav-chrome-and-module-ownership`): sidebar only when ≥2
  modules owned; single-feature = bottom-bar only, extras → You/Settings.
  Backend-driven chrome, BOTH surfaces (mobile + admin-web).
- Remove the two baseline entries in `check-nav-composition.mjs` once composed.

### P1 — Org role model (tier × vertical × park) full backend
- `context/architecture/org-role-model.md` has the truth table; `permissions.go`
  only has 4 flat roles (ParkHead/PCDirector/Operator/CEOInternal).
- Build the real model: 9 verticals (Procurement/Preventive Care/Breeding/Health/
  Growth/Infrastructure/Feed/Milk/Sales) × 5 tiers (CEO/CxO → Director → Head →
  Manager → Asst Manager) × park-scope (CBE/CPT/Bangalore). Role = vertical+tier.
- Verifier role slots in here (`verification.review`, cross-vertical category
  assignment). Feeds 1a permissions.

### ~~P1 — FCM / push mobile wiring~~ — REMOVED (owned by the Max/"FCM notification system for GCP" session)
Do NOT build here. See the cross-session ownership block at the top. The Max
session owns both the mobile FCM wiring and the backend send-side producer.

### P2 — Scale → 1M certification (good-to-have, NOT a ledger bug — Ravi's call)
- Baseline 47 → 21 → ~14 (after integrating the 4 wip branches).
- Full cert bar: baseline → 0 + high-scale-kernel-e2e + latency (p50≤100/p95≤300/
  p99≤500ms) + query-plan proof + lock-safe migrations. Needs staging/cloud DB at
  1M rows — local gates only prove shape. Do AFTER the verification vertical.

---

## 2. Guardrails / skills already in place (workers MUST use)
- **Skills** (`.agents/skills/**` + `.claude/skills` symlinks — both Claude+Codex):
  `scale-anti-patterns`, `mobile-anti-patterns`, `kernel-scale-lens`,
  `nav-composition`, `frontend-anti-patterns`, `db-migration-safety`.
- **Machine guards** (`make guardrails` / `ci-local`): scale-guard, mobile-guard,
  android-bounded-memory-guard, clinical-defer-guard, idempotency-writes-guard,
  atomic-readmodel-sync-guard, admin-web-request-reads-guard, **nav-composition-guard**,
  **mobile-contract-ownership-guard**, validate-migrations, validate-sqlc-plans,
  e2e-integrity-guard.
- New backend/mobile code: run the matching skill BEFORE writing, `make guardrails`
  BEFORE claiming pass. No `@Ignore`/`scale-guard:ignore` dodges on real loops.

## 3. Hard rules (from memory — do not violate)
- **Mobile never fetches >~20/screen**; every drill = keyset ~20 infinite-scroll.
  Calendar overview = DOTS only. Drive = mix of sheds, never grouped by vaccine.
- **Backend contract owns visibility** — mobile must not gate on `role ==`.
- **Brand = "Mesha"** in all user-facing UI, never "Goat OS" (codename in
  pkg/class/repo only). Logo = exported web PNG (Image), never re-rendered glyph.
- **No fake truth** — no invented ownership/data at runtime; missing source →
  reviewed mapping / provisional fixture / loud blocker.
- **Mock = UI source of truth** (`goatos/mock/goatos-dashboard-mock.html` for
  admin-web); un-backed elements = mock look + disabled-with-reason.
- **Push:** `zsh -ic 'git mesha-push main'`, never `gh`. Never `git add -A`.
- **Workers:** isolated branches/worktrees, never push main, never edit the ledger.
  Coordinator integrates sequentially + pushes accepted SHA.
- **Camera-only proof capture** (anti-fraud, live recording, no file picker); 3
  mandatory + 2 optional videos (min3/max5) — already built in MOB-002.

## 4. Suggested session order
0. **Confirm Verification ownership vs the Max session** (top block). If Max
   already shipped verifier backend (1a) + admin-web verify UI (1c), skip them.
1. Integrate the 4 wip branches + centralize baseline + push (§0). ~30 min.
   (In progress by the coordinator as of this handover — check main first.)
2. Verification backend (1a) — migrations + module + vaccination producer + route
   + `verification.review` permission + tests. **ONLY if Max didn't build it.**
3. Verifier mobile app (1b) — depends on 1a routes/DTOs. **This session's clearest
   unique piece** (Max is doing FCM, not the verifier queue app).
4. admin-web approve/rework UI (1c) — **ONLY if Max didn't build it.**
5. Nav registry composition (P1) — backend + both surfaces.
6. Org role model (P1) — can inform 1a permissions; may pull earlier.
7. 1M cert (P2) — last, good-to-have. (FCM removed — owned by Max session.)

## 5. Design-doc pointers (all on main)
```text
context/architecture/verification-module-design.md
context/architecture/verifier-app-and-flow.md
context/architecture/org-role-model.md
context/architecture/staff-org-data.md            # (HRMS-adjacent; source note only)
docs/decisions/role-module-nav-composition.md
docs/mobile/proof-capture-sync-and-e2e.md
docs/decisions/scale-anti-patterns.md
tools/scale-guard/baseline.txt                    # tracked scale debt map
context/repo-audits/last-35-commits-consolidated-bug-ledger.md   # closed ledger
```

## 6. Layman status for Ravi
Ledger bugs: **done.** Phone capture (MOB-002): built + build-proven on emulator
(full runtime flow needs a physical phone — BT-HID + camera). GCS upload:
**proven** (real-cloud step = maintainer-only). Push notifications (FCM): being
built by the **other (Max) session**, not here. What's left to BUILD in THIS
queue: the **Verification vertical** (verifier mobile app for sure; backend +
web approve screen only if the Max session didn't already do them) — the daily
video double-check — plus **nav-from-grants** and the **full org role model**.
HRMS excluded. 1M-scale is good-to-have, last.
