# Weighing Implementation Do-Not-Reopen Ledger

**Date:** 2026-07-31  
**Purpose:** Guard against resurrecting fixed bugs and banned designs in the upcoming weighing feature implementation  
**Scope:** Last 30 commits touching weighing, RBAC/bootstrap, verifier, FCM, operator scoping, and Android nav  

## Section A: CLOSED BUGS (Last 30 Commits) — Do Not Resurrect

Each row: **what broke** | **fix SHA** | **regression guard**

### A-1: Weighing Operator Shed Scoping

**Issue:** `ListScopeRoster()` and `ListCampaigns()` fetched ALL campaigns and rosters without operator filtering; an operator could see other operators' weighing work.

**Fixed by:** `6dfe695b9` ("Fix weighing operator shed scoping")

**Root cause:** Repository read methods `listCampaigns` and `listScopeRoster` accepted no operator user ID parameter, so they fell back to tenant-scoped queries that ignored the shed-operator assignment grain.

**Exact regression to prevent:**
- Any `weighing_campaigns.ListCampaigns` or `weighing_campaigns.ListScopeRoster` call without an explicit `operatorUserID` parameter must fail at compile time (method signature).
- SQL queries for weighing campaigns/rosters must check `weighing_campaign_sheds.operator_user_id = $N` INSIDE the WHERE clause before LIMIT, not as a post-query filter.
- Mobile/admin-web routes calling weighing lists must pass the authenticated operator's user ID from the request context.
- See `backend/internal/weighing/adapters/postgres/repository.go:283-296` — the `EXISTS` subquery on `weighing_campaign_sheds.operator_user_id` is the pattern to follow.

**Guards:** 
- `backend/internal/permissions/weighing_permissions_test.go` — operator scoping tests for routes
- Machine: grep for `ListCampaigns\(` and `ListScopeRoster\(` — if called without `operatorUserID`, fail the build

---

### A-2: Weighing Roster Observations Operator Filtering

**Issue:** Backend read path for weighing observations (`WeighingRepository.ListObservations`) was not filtering by the authenticated operator's shed assignments; read queries were tenant-scoped.

**Fixed by:** `6b9a0c889` ("Scope weighing roster observations by operator")

**Root cause:** Repository method accepted no operator scope parameter; observations returned all animals in the campaign shed regardless of the calling operator's work assignment.

**Exact regression to prevent:**
- `ListObservations` queries must include explicit `operator_user_id` predicate matching the shed-level assignment.
- Proof: `backend/internal/weighing/adapters/postgres/repository_observation_integration_test.go:20` — test demonstrates that observations for different operators in the same shed are filtered correctly.
- An operator can only observe animals in their assigned sheds within the campaign.

**Guards:**
- Integration test in repository test file covers operator-scoped observation reads
- Admin-web detail views and mobile rosters must pass the authenticated operator context to observations endpoints

---

### A-3: Weighing Director Execution Gates (Approval) Split

**Issue:** Director approval paths for weighing submissions (verification/rejection) were not separately gated; a director with one role might access verification logic meant for a different role scope.

**Fixed by:** `d7785ed7b` ("Split director execution gates for weighing")

**Root cause:** Weighing approval routes (`/weighing/campaigns/{id}/submissions/{id}/approve`, `/verify/{id}/rework`) did not split by director role (PC director, future director aliases) or verify the director's park scope before processing a submission approval.

**Exact regression to prevent:**
- Weighing submission approval/rework must check BOTH the authenticated user's role (PC director or equivalent) AND their park scope against the submission's campaign park.
- Routes: `backend/internal/weighing/adapters/http/handler.go:22` + `backend/internal/permissions/permissions_orgrole.go:19` (director role constants).
- SQL or handler logic must verify `campaign.park_id` matches the authenticated director's scope before calling `ApproveSubmission`/`BounceSubmissionForRework`.
- Test: `backend/internal/permissions/weighing_permissions_test.go` — director-scoped approval tests.
- See domain event `WeighingSubmissionApprovalEvent` producer wiring — approval is a state transition with mandatory park scope.

**Guards:**
- Permission middleware checks in `backend/internal/permissions/permissions.go:14` (weighing routes handler)
- `weighing_permissions_test.go` includes cross-park director denial tests
- Mobile bootstrap must not show weighing approval routes to directors outside the park scope

---

### A-4: Weighing Verifier Screenshot Fixture Mismatch

**Issue:** Verifier queue integration tests used a stale screenshot fixture filename/path reference; test screenshots did not match the actual mobile proof media naming convention or verification handler.

**Fixed by:** `a9d72bb74` ("Fix weighing verifier screenshot fixture") + `5c4152b89` (earlier fix)

**Root cause:** Test fixture referenced an old `weighing_verifications` screenshot column name or proof storage path that had changed when the proof artifact/verification gate was refactored.

**Exact regression to prevent:**
- Weighing verification proof fixtures must match the actual `verification.proof_ref` / `proof_artifacts.media_reference` grain and naming.
- Tests for weighing submission verification (approval/rework with proof review) must use real S3/GCS media references or a mock that tracks the exact column/path used by the handler.
- Integration tests: `backend/internal/notificationbridge/weighing_submission_notify_consumer_test.go:62` — proof reference parsing must match the live verification payload.
- Screenshot/video proof-reference columns and handlers must be synchronized in every test fixture change.

**Guards:**
- Proof artifact schema validation on weighing submission create/approve
- Test fixtures that use hardcoded proof paths must be updated whenever the `proof_artifacts` or `verifications` schema changes
- Machine: grep for `weighing_verifications` and `proof_ref` to ensure fixture paths align with current schema

---

### A-5: RBAC Bootstrap Coverage for Weighing (Founder Visibility + Operator Grants)

**Issue:** Initial app bootstrap seeding did not grant the five founder/builder accounts proper admin access to weighing, and operator role grants were not materialized correctly (pending-only, not active).

**Fixed by:** Multiple commits (`d4b4b1ed7`, AGENTS.md founder visibility rule, `000057_growth_director_role.sql` migration)

**Root cause:** Bootstrap service (`WorkforceService.BootstrapUser`) only granted `ceo_internal` tenant scope for founders during auth claim; weighing director role (`growth_director` / future variant) was not seeded into `user_scope_grants` with active status.

**Exact regression to prevent:**
- Every founder/builder account (`ravi@mesha.sg`, `manohark@mesha.sg`, `manju@mesha.sg`, `abhishek@mesha.sg`, `aryaman@mesha.sg`) must receive BOTH a `ceo_internal` tenant grant AND a `director` (PC director, growth director, or future variant) park-scoped grant, materialized to `user_scope_grants.status='active'`, not pending.
- Seeding path: `backend/cmd/seed-stg-login-grants` materializes grants directly via `user_scope_grants` insert (line `000057` migration creates the grant structure).
- New weighing-related roles must be added to the founder bootstrap in `bootstrap_copy.go:37` (see the expanded list) AND the nine-person login seed.
- Mobile bootstrap (`/app/bootstrap`) must include all active founder/director grants so they appear in UI role chips and nav.

**Guards:**
- `backend/internal/workforce/app/service_test.go` — bootstrap tests verify founder grants include weighing roles
- `backend/internal/workforce/app/bootstrap_copy_test.go` — tests for the expanded BootstrapUserCopy include weighing director role
- STG login verification: `make seed-stg-9-person-login` materializes grants; `make verify-stg-9-person-login` confirms active status (not pending).
- Machine: grep for `ceo_internal` and `director` in bootstrap to ensure weighing roles are listed alongside

---

### A-6: Weighing Free-Flow vs. Expected-List Read Model Clarity

**Issue:** Weighing had both a free-flow input model (scanned RFID, no pre-expected list) and a legacy expected-list model (herd register preload); handlers did not cleanly separate these and could leak expected-list reads into free-flow submissions.

**Fixed by:** `2d7d722d1` ("Remove stale weighing expected-list read"), `36b209d3b` ("Clarify free-flow weighing boundaries")

**Root cause:** Repository read method `GetExpectedAnimals()` was called unconditionally in some submission paths, pulling herd data even when the campaign was free-flow (no pre-expected list defined).

**Exact regression to prevent:**
- Free-flow weighing campaigns (`weighing_campaigns.expected_animals_source = 'free_flow'`) must NEVER call `GetExpectedAnimals()` or read `weighing_expected_animals` table.
- Proof: `backend/internal/weighing/adapters/postgres/repository.go:455` — `GetExpectedAnimals()` method must only be called by old admin/review surfaces (not mobile), and only when the campaign's source is NOT free-flow.
- Submission validation and roster-listing must check the campaign's `expected_animals_source` field and take different paths: free-flow accepts any scanned RFID; expected-list rejects unknown animals.
- See domain contract: `backend/internal/weighing/domain.Campaign.ExpectedAnimalsSource` — field must be checked before any expected-list operation.

**Guards:**
- Compilation: expected-list read methods are marked as deprecated/legacy and must have inline documentation of their scope.
- Integration test: `backend/internal/weighing/app/service_test.go:59` — free-flow submission must not populate expected animals even if herd data exists.
- Guard: any route calling `GetExpectedAnimals()` must be listed in `docs/features/weighing-free-flow-scope.md` as a legacy surface, not a production mobile path.

---

## Section B: BANNED DESIGNS — Do Not Reintroduce

### B-1: Weighing Operator Scope Only Via Shed Assignment (No Broad Campaign Grant)

**Authority:** `AGENTS.md` → Operator scope invariant + `docs/decisions/shifting-verification.md` (movement scope rule applies here)

**Ban:** An operator must NOT receive `user_scope_grants.scope_type='tenant'` for any weighing work. An operator is always scoped to exactly one park (`scope_type='park'`, `scope_id=<park_location_id>`) PLUS explicit shed/campaign assignment rows in `weighing_campaign_sheds`.

**Why:** Tenant-wide operator scope makes audit/multiparty isolation impossible; weighing data (animal weights, health annotations) must be shed-level within a park.

**How to check:** 
- Grep: `weighing` + `scope_type='tenant'` — must return zero results
- Seeding: no STG operator seed line creates a weighing operator with tenant scope
- Mobile bootstrap: `GetLeadershipMember()` and `GetOperatorMember()` queries must return role, tenant scope, and explicit shed list separately — do not synthesize tenant scope from missing park assignments

---

### B-2: Director Execution Gates Without Park Scope (Medical Safety P0)

**Authority:** `AGENTS.md` → "Critical Animal Action Guardrails Are Mandatory" + `docs/decisions/weighing-verification.md`

**Ban:** Weighing submission approval/rejection/rework must check the authenticated director's park scope BEFORE any state transition. A director outside the campaign's park must receive a 403 permission_denied, NOT a 200 with silent no-op or a fallback to COO-scope approval.

**Why:** Wrong park approval is a medical safety issue (wrong park → wrong treatment/quarantine recorded).

**How to check:**
- All routes: `POST /weighing/campaigns/{id}/submissions/{id}/approve`, `POST /verify/{id}/rework` must call a permission middleware that checks park scope.
- Code: `backend/internal/permissions/permissions_orgrole.go` must include an `AssertDirectorCanApproveSubmission(ctx, directorUserID, campaignParkID)` guard that fails 403 if the director's park grants don't include campaignParkID.
- Tests: `backend/internal/permissions/weighing_permissions_test.go` must include "director outside park denied" assertions.

---

### B-3: Weighing Observations Without Operator Filtering

**Authority:** `AGENTS.md` → Layer 1 CRG query pattern

**Ban:** Any SQL query or repository method returning weighing observations must include an `operator_user_id` predicate in the SQL WHERE clause BEFORE LIMIT. Post-query filtering in Go or UI is banned.

**Why:** A post-query filter is not enforced on API or mobile clients that bypass the Go layer.

**How to check:**
- Grep: `weighing_observations` + `SELECT` → all queries must have `WHERE ... operator_user_id = $N`
- No `SelectAll()` / `List()` methods that return unconstrained observations
- If a legacy admin page needs all observations for review, it must be a separate admin-only handler, explicitly documented as "bypasses operator filtering"

---

### B-4: Weighing Free-Flow Campaigns With Pre-Expected List (Mutually Exclusive)

**Authority:** `docs/features/weighing-free-flow-scope.md` + migration `000007_weighing_free_flow_scanned_identifier.sql`

**Ban:** `weighing_campaigns.expected_animals_source` must be either `'free_flow'` (no pre-load, accept any RFID) or `'herd_register'` (pre-load from herd register), never both. A campaign must not switch between the two after creation.

**Why:** Free-flow and expected-list are incompatible submission validation rules; mixing them creates ambiguity (is an unmapped RFID valid or an error?).

**How to check:**
- Schema: `expected_animals_source` is a NOT NULL enum with two values only.
- Create campaign validation must reject any attempt to set both `expected_animals` and `free_flow: true`.
- Migration/schema: if both columns are present, a CI guard must fail (e.g., `make weighing-campaign-source-guard`).

---

## Section C: INVARIANTS Implementation Must Preserve

### C-1: Weighing Shed-Level Operator Scope (SQL Grain)

**Assertion:**
```
INSERT INTO weighing_campaign_sheds(tenant_id, campaign_id, campaign_shed_id, operator_user_id, status)
  VALUES($1, $2, $3, $4, 'active');

SELECT * FROM weighing_campaigns
WHERE tenant_id = $tenantID
  AND EXISTS (
    SELECT 1 FROM weighing_campaign_sheds
    WHERE weighing_campaign_sheds.tenant_id = $tenantID
      AND weighing_campaign_sheds.campaign_id = weighing_campaigns.campaign_id
      AND weighing_campaign_sheds.operator_user_id = $operatorUserID
      AND weighing_campaign_sheds.status <> 'canceled'
  );
```

**File:Line:** `backend/internal/weighing/adapters/postgres/repository.go:283-296` (the listCampaigns SQL with operator filter)

**Why:** Ensures every campaign list read is scoped by the calling operator's active shed assignments.

---

### C-2: Director Approval Requires Park Scope Check

**Assertion:**
```go
// In approval handler
campaignParkID, err := getCampaignPark(ctx, campaignID)
if err != nil { return err }

if !hasRoleAndParkScope(ctx.User(), "pc_director", campaignParkID) {
  return ErrPermissionDenied // 403
}

// Only then proceed with approval
```

**File:Line:** `backend/internal/weighing/adapters/http/handler.go:22` (ApproveSubmission handler) + `backend/internal/permissions/permissions_orgrole.go:19` (role/scope check)

**Why:** Prevents a director in Park A from approving submissions from Park B.

---

### C-3: Free-Flow Weighing Never Reads Expected Animals

**Assertion:**
```go
if campaign.ExpectedAnimalsSource != "free_flow" {
  return repo.GetExpectedAnimals(ctx, campaignID)
}
return nil // free-flow: no pre-load
```

**File:Line:** `backend/internal/weighing/app/service.go` (GetExpectedAnimalsForCampaign or equivalent)

**Why:** Free-flow buckets accept any scanned RFID without pre-validation against a roster.

---

### C-4: Bootstrap Founders Include Weighing Director Role

**Assertion:**
```go
// In BootstrapUserCopy(user)
grants := []Grant{
  {role: "ceo_internal", scope: "tenant"},
  {role: "pc_director", scope: "park", parkID: "default_or_all"},
  // + growth_director or future weighing director role for all founders
}
```

**File:Line:** `backend/internal/workforce/app/bootstrap_copy.go:37-51` (the expanded grant list)

**Why:** Ensures founders can access weighing verification screens after first login.

---

### C-5: Mobile Verifier Routes Require Director Role + Park Scope

**Assertion:**
```
GET /app/verification/weighing/{id}
  -> middleware checks: has role "pc_director" or "growth_director"
  -> queries verificationParentCampaignParkID
  -> checks: park in user's active director grants
  -> 403 if denied
```

**File:Line:** `apps/goatos-android/app/src/main/.../WeighingVerifyScreen.kt` + `backend/internal/permissions/routes.go` (route wiring)

**Why:** Mobile verification screens must not be rendered to operators outside the director scope.

---

### C-6: Verifier Proof Fixtures Match Verification Handler Schema

**Assertion:**
```go
// In *_test.go fixtures
ProofRef: "gs://bucket/verification/20260729-xxx.mp4", // real media reference format
CreatedAt: time.Now(),
Handler expects: verification.ProofRef to match /verification/ path pattern
```

**File:Line:** `backend/internal/notificationbridge/weighing_submission_notify_consumer_test.go:62` (fixture setup)

**Why:** Ensures tests exercise the same media reference paths as production handlers.

---

## Section D: EXISTING MACHINE GUARDS (What's Already Enforced)

### D-1: RBAC Permission Checks (Weighing-Specific)

**Guard:** `backend/internal/permissions/weighing_permissions_test.go`  
**What it checks:**
- Operator routes reject non-operator principals
- Director approval routes reject operators + non-director roles  
- Park-scope denials for cross-park approval attempts

**Scope:** Weighing HTTP handlers only (does not cover bootstrap or mobile route visibility)

**Limitation:** Guard does not verify that directors are **materialized** (active status in user_scope_grants) — only that the principal claims the role.

---

### D-2: Free-Flow vs. Expected-List Separation (No Guard)

**Status:** NO MACHINE GUARD EXISTS YET

**Needed:** `tools/agent-hooks/check-weighing-free-flow-scope.mjs` should grep for:
- `GetExpectedAnimals()` calls (must be outside mobile/main handlers)
- `expected_animals_source` checks before any expected-list read

**Defer to:** Implementation, but document the absence here so a later fix catches it.

---

### D-3: Operator Scoping in SQL Queries (No Guard)

**Status:** NO GENERAL GUARD EXISTS; must be verified by code review

**What would catch it:** A static analyzer that checks all `weighing_campaigns` / `weighing_observations` SELECT statements for an explicit `operator_user_id` predicate in WHERE BEFORE LIMIT.

**Defer to:** Manual code review + integration test coverage in `repository_*_test.go` files.

---

### D-4: Verification Proof Fixtures (Test-Only)

**Guard:** None (test-only validation)

**What would help:** Pre-test assertion in `weighing_submission_notify_consumer_test.go` that hardcoded proof paths match the current `proof_artifacts` schema.

---

## Section E: TRAPS FROM RECENT COMMITS (Do Not Repeat)

### E-1: Operator Scoping at Shed Level, Not Park Level

**Trap:** Building operator filtering at the park level (`WHERE campaign.park_id = $parkID`) instead of the shed level (`WHERE EXISTS (weighing_campaign_sheds WHERE operator_user_id = $opID)`) caused operators to see other operators' weighing in the same park.

**Prevention:** Always check the `weighing_campaign_sheds` assignment table for operator filtering, not the parent campaign's park.

**Proof commit:** `6dfe695b9` — the EXISTS subquery is the correct pattern.

---

### E-2: Director Approval Path Bypass (No Park Check)

**Trap:** Approval handlers were wired without checking the director's park scope, allowing a director to approve/reject submissions from any park. Caught at routing layer, not handler layer.

**Prevention:** Director approval must call a dedicated permission check INSIDE the handler that validates park scope before ANY state change (before even reading the submission).

**Proof commit:** `d7785ed7b` — shows the split approval route that enforces park scope.

---

### E-3: Verifier Screenshot References in Test Fixtures (Column Rename)

**Trap:** Test fixture hardcoded an old screenshot column name that was renamed when the proof artifact schema changed. Test compiled but failed at runtime with "column not found."

**Prevention:** Before changing any `proof_artifacts` or `verification_*` schema column, search for hardcoded references in ALL test fixture files and update them in the SAME commit.

**Proof commit:** `a9d72bb74` — the fix updates both the schema and the test reference.

---

### E-4: Founder Bootstrap Missing Weighing Director Role

**Trap:** Five founder accounts were seeded with `ceo_internal` tenant scope only; when weighing director role was added, founders were not included, causing 403 errors on first login to weighing verification screens.

**Prevention:** Whenever a new director role is added (weighing, breeding, procurement, etc.), update the bootstrap logic to grant it to all five founders. Use a test to assert founder grants include all active director roles.

**Proof commit:** `d4b4b1ed7` + migration `000057_growth_director_role.sql` — shows founder inclusion in the role list.

---

### E-5: Free-Flow Campaigns Calling Expected-List Reads

**Trap:** A free-flow campaign (no pre-expected list) still called `GetExpectedAnimals()`, populating the roster with herd data and confusing the submission validation (animals were "expected" when they shouldn't be pre-defined).

**Prevention:** Always check `campaign.ExpectedAnimalsSource == 'free_flow'` before ANY call to expected-animal read methods. Write a test that proves free-flow campaigns do not call those reads.

**Proof commit:** `2d7d722d1` — shows the removal of the stale read from the free-flow path.

---

### E-6: Weighing Roster Observations Not Operator-Filtered

**Trap:** `ListObservations` returned all observations in a shed for any caller, not filtering by the authenticated operator's assignment. An operator could see health notes or weight changes from other operators' work.

**Prevention:** Add an operator parameter to all weighing observation repository methods. Every observation query must include `operator_user_id` in the WHERE clause. Write an integration test that verifies operator A cannot see operator B's observations in the same shed.

**Proof commit:** `6b9a0c889` — shows the operator parameter added to `listScopeRoster` with the EXISTS subquery pattern.

---

## Summary: Before You Start Implementation

**Read first:**
1. `AGENTS.md` → "Operator scope invariant" + "Critical Animal Action Guardrails Are Mandatory"
2. `docs/decisions/shifting-verification.md` — movement/verification scope rules (identical pattern applies to weighing)
3. `backend/internal/weighing/app/service.go` — existing service layer to understand the boundaries
4. `backend/internal/permissions/weighing_permissions_test.go` — existing permission tests
5. Recent weighing commits (`6dfe695b9`, `d7785ed7b`, `6b9a0c889`) — code examples of the patterns

**Do not:**
- Fetch campaigns/observations without operator filtering
- Approve weighing submissions without park scope check
- Mix free-flow and expected-list logic in the same path
- Call expected-animal reads for free-flow campaigns
- Forget founder/director bootstrap grants for new roles
- Use post-query filtering instead of SQL WHERE predicates

**Machine checks:**
- `backend/internal/permissions/weighing_permissions_test.go` runs on every PR
- Manual review: grep for weighing route handlers and verify permission checks are INSIDE handlers, not just at routing
- Integration tests: prove operator A cannot see operator B's weighing data, even in the same park/shed

---

**Generated:** 2026-07-31  
**Applies to:** Any upcoming weighing feature work (new commands, reports, reconciliation, multi-operator collaboration, etc.)
