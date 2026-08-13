# Goat OS Whole-Project Audit Refresh

Reviewed repository: `vgoats/goatos`

Reviewed snapshot: `c8b475f6ebb2a45c03cc4d607a26824e55edd8d8`

Incremental range: `d98a8a6f4cd452aa56681c52f91c06054c089081..c8b475f6ebb2a45c03cc4d607a26824e55edd8d8`

The range contains 26 commits, 99 changed files, 7,525 insertions, and 1,017 deletions. It was reviewed together with the full-project baseline rather than in isolation.

ID scope: this whole-project audit has its own stable ID namespace. A matching ID in the older `last-35-commits-consolidated-bug-ledger.md` is historical and must not be assumed to describe the same defect. This refresh plus its linked baseline is the 128-item whole-project snapshot at the reviewed SHA; it does not rewrite the older ledger's historical closure record. It is a point-in-time audit: commits after `c8b475f6` require re-verification before changing a finding's status.

## Current Ledger

- Previous ledger: 94 findings (`1 P0`, `59 P1`, `34 P2`).
- Closed after this review: `FEED-001`, `FEED-002` (both P1).
- Newly added after deduplication: 36 findings (`22 P1`, `14 P2`).
- Current open ledger: **128 findings (`1 P0`, `79 P1`, `48 P2`)**.
- Counter-review adjudication: all 36 new findings remain; two descriptions were clarified, but no ID or severity was removed.
- No new P0 was proven. The existing `MOB-001` P0 remains open.

Severity:

- `P0`: stop-ship data isolation or catastrophic integrity risk.
- `P1`: serious wrong behavior, security exposure, data loss, or operational outage risk.
- `P2`: important correctness, performance, reliability, UX, accessibility, or release-control defect.

## New P1 Findings

### AUTH-005 - Park heads can execute feed work but cannot upload its mandatory proof

**Status:** Preexisting, newly discovered.

**Plain English:** A park head is allowed to finish feed packing/distribution, but the proof endpoint rejects the required video/photo. The screen can offer work that the same user can never submit.

**Evidence:** Proof writes accept task, weighing, or health execution only at [routes.go:156](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/permissions/routes.go#L156). Park heads receive `FeedDirectionComplete`, but none of those proof permissions, at [permissions.go:474](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/permissions/permissions.go#L474). The accepted authorization decision requires proof capture to use the same execution right at [proof-capture-authorization.md:7](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/docs/decisions/proof-capture-authorization.md#L7).

**Fix:** Add `FeedDirectionComplete` to the proof-write OR gate and add a role-to-route regression test.

### AUTH-006 - Operators can list transport tasks but cannot submit them

**Status:** Preexisting, newly discovered; recent work exposed the listing side.

**Plain English:** The operator can open a transport assignment and record evidence, but final Submit returns 403.

**Evidence:** Listing needs `FeedTransportRead`, submission needs `FeedDirectionComplete` at [routes.go:393](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/permissions/routes.go#L393). The scoped-route allowlist admits listing but omits submission at [auth.go:393](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/platform/httpmiddleware/auth.go#L393). The repository submission lookup also lacks a park check at [transport.go:273](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/transport.go#L273).

**Fix:** Admit the route only after atomically comparing the stored task park with the grant supplying the capability.

### FEED-008 - Completion accepts feed work that never existed

**Status:** Preexisting, widened by the new pen field.

**Plain English:** A caller can submit `Part 999`, session 99, the wrong workflow, or a date with no issued sheet. The server creates approvable proof for imaginary work while the real worklist remains Pending.

**Evidence:** Packing validates only basic field shape at [packing_service.go:103](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/packing_service.go#L103), and persistence checks only that the shed belongs to the park at [packing_completions.go:70](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/packing_completions.go#L70). Distribution has the same shape. Existing success tests do not create an issued sheet first.

**Fix:** Bind completion to an immutable issued-sheet row/revision and validate exact park, shed, pen, session, date, and workflow membership.

### FEED-009 - PostgreSQL and Go disagree about pen identity

**Status:** New in this range.

**Plain English:** The first `Part 3` submission works. A fresh-key retry or rework then returns 500 because the database stored `part 3` while Go searches for `part_3`.

**Evidence:** Migration 137 lowercases and trims only at [000137:24](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000137_feed_completions_partition_label.sql#L24). Go collapses spaces, hyphens, and underscores at [rounding.go:163](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/domain/rounding.go#L163), then uses that value in conflict recovery at [packing_completions.go:125](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/packing_completions.go#L125). The same mismatch exists for distribution.

**Verification:** Reproduced against real PostgreSQL: first insert succeeded and a fresh-key resubmission failed in `read existing packing completion`.

**Fix:** Use one canonical normalization in generated columns, natural keys, lookups, events, and tests.

### FEED-010 - Completion idempotency fingerprints omit the pen

**Status:** New in this range.

**Plain English:** If the same request key/proof is reused for Pen 1 and Pen 2, Pen 2 is silently treated as a replay of Pen 1 and receives no completion or verifier item.

**Evidence:** Packing omits `PartitionLabel` from the effect fingerprint at [packing_completions.go:76](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/packing_completions.go#L76); distribution does the same at [distribution_completions.go:76](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/distribution_completions.go#L76).

**Verification:** A real-PostgreSQL reproduction submitted two pens with one key and proof. Both calls returned the first completion ID, and only one row existed.

**Fix:** Include canonical partition identity in both fingerprints and test same-key/different-pen behavior.

### FEED-011 - Old mobile work becomes an invisible whole-shed completion

**Status:** New upgrade defect.

**Plain English:** Work queued before the app upgrade has no pen field. After upgrade it syncs successfully into a fake `whole` bucket; no real pen shows completion, so operators may repeat the work.

**Evidence:** Legacy payloads decode `partitionLabel=null` at [SyncPayloads.kt:248](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncPayloads.kt#L248), are forwarded at [SyncEngine.kt:649](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncEngine.kt#L649), and become `whole` at [000137:27](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000137_feed_completions_partition_label.sql#L27). Current status overlays match exact pen identity at [service.go:353](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/service.go#L353).

**Fix:** Migrate/quarantine legacy outbox rows and reject blank pen identity whenever the issued work is partitioned.

### FEED-012 - A pen-only issued-sheet change is ignored as unchanged

**Status:** Preexisting, newly discovered.

**Plain English:** Moving an otherwise identical experiment from Part 3 to Part 4 can leave the frozen instruction on Part 3 because the sheet fingerprint does not include the pen.

**Evidence:** Issued-sheet fingerprints omit `PartitionLabel` at [issue.go:160](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/domain/issue.go#L160). Equal fingerprints skip the row diff at [issues_repository.go:175](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/issues_repository.go#L175).

**Fix:** Add canonical partition identity to every row fingerprint and a Part 3 -> Part 4 amendment test.

### FEED-013 - An existing normal sheet prevents the default read from freezing a due experiment sheet

**Status:** Preexisting, newly discovered.

**Plain English:** The normal phone view asks for both workflows together. If normal feed froze in the morning and the experiment scheduler missed its later run, the afternoon read sees the existing normal sheet and never freezes the now-due experiment sheet.

**Evidence:** `Workflow` is optional and empty explicitly means both workflows at [types.go:135](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/domain/types.go#L135); Android's default selection is empty at [FeedDirectionViewModel.kt:307](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedDirectionViewModel.kt#L307). The SQL correctly uses the supplied workflow, but an empty value deliberately returns every existing header at [issues_repository.go:316](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/issues_repository.go#L316). Once that returns the morning normal header, `served=true`, so the gate at [lifecycle.go:198](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/lifecycle.go#L198) is skipped instead of checking which configured workflow is still missing.

**Verification:** A temporary regression probe performed the real sequence on one service/store: all-workflows read at 08:00 froze normal; the same read at 15:00 still had one header instead of normal plus experiment. The probe failed with `15:00 headers = 1, want normal and experiment` and was removed after reproduction.

**Fix:** For an all-workflows read, compare configured due workflows with the issued headers and freeze each missing due workflow independently.

### FEED-014 - Packing and distribution record no actual quantities or variance

**Status:** Preexisting, newly discovered.

**Plain English:** A planned 12.5 kg bag can contain 8 kg and still be approved because the system stores videos but no typed actual kilograms, shortage, or discrepancy.

**Evidence:** Packing input is proof-only at [packing_service.go:67](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/packing_service.go#L67); distribution is proof-only at [distribution_service.go:54](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/distribution_service.go#L54). The required planned/actual/variance contract is stated at [SOP-MOBILE-CUTOVER-PRD.md:157](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/docs/feed-direction/SOP-MOBILE-CUTOVER-PRD.md#L157).

**Fix:** Capture actual quantities per feed item and derive discrepancy/shortfall before approval.

### FEED-015 - Packing verification is approvable without the expected ration

**Status:** New in this range.

**Plain English:** If the issued sheet cannot be read, the verifier gets an ordinary approvable item containing only a video and cannot know what feed or quantity should be present.

**Evidence:** The expectation lookup deliberately returns blanks for missing wiring, read errors, unserved sheets, and no matching row at [packing_service.go:202](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/packing_service.go#L202). Blank context is omitted at [packing_enqueue.go:93](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/verificationbridge/packing_enqueue.go#L93), and the test blesses that behavior at [packing_context_test.go:70](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/verificationbridge/packing_context_test.go#L70).

**Fix:** Preserve the operator submission as `context_pending`, but block verdict approval until expected context is restored.

### FEED-016 - Delayed mobile work can be judged against a newer ration

**Status:** New in this range.

**Plain English:** An operator can pack yesterday's downloaded instruction offline; if the sheet changes before sync, the verifier sees today's amended ration beside yesterday's video.

**Evidence:** The durable payload contains no issue ID, fingerprint, or revision at [SyncPayloads.kt:320](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncPayloads.kt#L320). Submission reloads the current sheet at [packing_service.go:218](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/packing_service.go#L218), while issued sheets can be amended at [lifecycle.go:65](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/lifecycle.go#L65).

**Fix:** Carry and validate the exact issued-sheet ID/revision followed by the operator.

### FEED-017 - Editing a retired experiment bypasses the explicit restore action

**Status:** Preexisting, newly discovered.

**Plain English:** A manager can open a retired pen merely to correct a number and Save switches the entire pen back to experiment feeding, bypassing the separate Restore control. A stale form can also undo another manager's later retirement.

**Evidence:** The domain calls the status change a business-meaningful workflow switch at [types.go:590](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/domain/types.go#L590), and the UI provides a separate consequence-labelled Restore/Withdraw control at [feed-config-editor.tsx:422](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-editor.tsx#L422). Retired rows nevertheless render the ordinary cell editor at [feed-config.tsx:961](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L961), the form carries no row version at [feed-config-editor.tsx:251](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-editor.tsx#L251), and Save deliberately reactivates every row in the pen at [repository.go:1139](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L1139). The adapter comment proves the side effect is intentional code; it does not resolve the conflict with the explicit switch contract or the stale-form overwrite.

**Fix:** Separate edit from restore, require explicit restore intent, and enforce optimistic concurrency.

### FEED-018 - Feed writers accept names absent from the catalog

**Status:** Preexisting, extended to the new batch endpoint.

**Plain English:** A typo such as `Concentratee` can become a live feeding instruction even though it cannot be selected or procured as a valid feed item.

**Evidence:** Batch validation checks nonblank/duplicates only at [service.go:727](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/app/service.go#L727). Persistence inserts labels without catalog membership at [repository.go:545](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L545), and experiment rows have no catalog foreign key at [baseline migration:13719](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql#L13719).

**Fix:** Resolve every authored label to a canonical tenant catalog item in the write transaction.

### FEED-019 - Experiment enrollment can reuse Pen A values for Pen B

**Status:** New in this range.

**Plain English:** After enrolling one pen, the form remains open. That pen disappears from choices, another pen becomes selected, but the old quantities/category remain and can be applied to the wrong pen.

**Evidence:** Success leaves the form open at [feed-config-editor.tsx:64](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-editor.tsx#L64). Candidates change at [feed-config.tsx:361](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L361), while pen and quantity fields are uncontrolled at [feed-config-editor.tsx:566](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-editor.tsx#L566) and [feed-config-editor.tsx:620](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-editor.tsx#L620).

**Fix:** Reset all form state after success or key controlled form state by the selected pen.

### FEED-020 - The admin editor cannot create missing base configuration

**Status:** Preexisting, newly discovered.

**Plain English:** The screen can edit existing rates, factors, and schedules but cannot create the first row. Adding a new feed item can therefore leave direction generation blocked unless someone calls the API manually.

**Evidence:** The repository lists existing rows only at [repository.go:85](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L85), and the UI renders editors only for returned rows at [feed-config.tsx:515](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L515). New feed items explicitly remain unconfigured at [service.go:451](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/app/service.go#L451), while missing rates block generation at [strategy.go:259](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/domain/strategy.go#L259).

**Counter-review note:** The backend upsert API is precisely why this is a frontend completeness bug, not a missing backend capability. The `CreateFeedItem` comment intentionally refuses to invent a quantity; it also says someone must author the new rate. It does not make the absent Add control an accepted workflow.

**Fix:** Render backend-owned missing cells with Add controls and create them through the existing upsert API.

### FEED-021 - Shed factors after row 25 disappear from admin

**Status:** Preexisting, newly discovered.

**Plain English:** Factor row 26 onward still changes live feed quantities but cannot be seen or edited, and the page gives no warning that more rows exist.

**Evidence:** The page requests a fixed 25 rows at [feed-config.tsx:69](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L69) and [feed-config.tsx:282](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L282), then ignores `has_more` and provides no pager through [feed-config.tsx:746](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L746).

**Fix:** Paginate/drain the server result and disclose incomplete results.

### FEED-022 - Multi-park operators cannot open Packing or Transport

**Status:** New in this range.

**Plain English:** The server says "choose a park," but the app needs that failed response to learn which parks are available. The empty selector stays disabled forever.

**Evidence:** Multiple grants with no `park_id` produce `park_selection_required` at [auth.go:573](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/platform/httpmiddleware/auth.go#L573). Handlers enforce it before returning filters at [handler.go:601](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/http/handler.go#L601). Android starts blank at [FeedPackingViewModel.kt:54](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedPackingViewModel.kt#L54) and disables the empty picker at [FeedPackingScreen.kt:254](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/feature/feature-feed/src/main/kotlin/sg/mesha/goatos/feature/feed/FeedPackingScreen.kt#L254).

**Fix:** Supply authorized park choices from bootstrap or a scope endpoint before requiring a selected park.

### FEED-023 - Coarse verifier grants hide Feed and Health

**Status:** Preexisting, newly discovered.

**Plain English:** A verifier allowed to review all work sees Vaccination, Weighing, and Counts, but Feed and Health are missing from the phone.

**Evidence:** The coarse fallback hard-codes three features at [bootstrap_copy.go:723](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/workforce/app/bootstrap_copy.go#L723) and returns them at [bootstrap_copy.go:756](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/workforce/app/bootstrap_copy.go#L756), although Feed and Health are supported at [bootstrap_copy.go:797](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/workforce/app/bootstrap_copy.go#L797).

**Fix:** Derive fallback features from the shipped verifier registry instead of a stale hard-coded list.

### KERN-010 - Feed verdicts report settled before feed state is updated

**Status:** Preexisting, newly discovered.

**Plain English:** Verification can say an approval/rejection is finished while the feed completion is still waiting for the asynchronous consumer to apply it.

**Evidence:** Decided items are considered settled unless `ApplierAckExpected` is true at [types.go:148](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/verification/domain/types.go#L148). Neither feed bridge sets it at [packing_enqueue.go:49](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/verificationbridge/packing_enqueue.go#L49), and feed verdict handlers do not acknowledge application at [feed_packing_verification_handler.go:79](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/feed_packing_verification_handler.go#L79).

**Fix:** Mark feed items as expecting an applier receipt and durably acknowledge only after feed state changes.

### MOB-008 - Upgrading hides already-recorded feed evidence

**Status:** New upgrade defect.

**Plain English:** A video recorded before upgrade still exists, but the new app searches under a date-and-pen key and behaves as if it was never recorded. The operator may have to catch and film the animals again.

**Evidence:** The new key includes date and pen at [FeedCaptureGroupKey.kt:42](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedCaptureGroupKey.kt#L42). Reopen looks up only the new key at [FeedPackingCompleteViewModel.kt:97](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedPackingCompleteViewModel.kt#L97), and Room uses exact-key lookup with no legacy migration at [CaptureDraftRepository.kt:93](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/CaptureDraftRepository.kt#L93).

**Fix:** Migrate legacy capture-draft keys or perform a one-time unambiguous fallback and re-key.

### REL-010 - Migration-first rollout breaks every old feed-completion binary

**Status:** New in this range.

**Plain English:** The database migration runs before the new API revision. During that window, old API instances cannot submit any packing/distribution completion and PostgreSQL returns `42P10`.

**Evidence:** Migration 137 replaces the six-column unique index at [000137:43](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000137_feed_completions_partition_label.sql#L43). Deployment migrates before updating API at [stg-clouddeploy-task.sh:166](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/tools/deploy/stg-clouddeploy-task.sh#L166) and [stg-clouddeploy-task.sh:193](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/tools/deploy/stg-clouddeploy-task.sh#L193). The previous binary's `ON CONFLICT` target no longer has a matching unique constraint.

**Verification:** The previous packing and distribution SQL both failed with PostgreSQL `42P10` against schema 138.

**Fix:** Use expand/contract rollout: add compatible identity first, deploy dual-compatible code, then remove the old key later.

### REL-011 - Rolling back migration 137 deletes valid pen completions and proof links

**Status:** New in this range.

**Plain English:** If two pens completed the same session, rollback keeps only the earliest one and permanently deletes the other pen's completion/proof linkage.

**Evidence:** Down drops the pen-aware indexes and deletes duplicates by the old shed-level key at [000137:52](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000137_feed_completions_partition_label.sql#L52), [000137:58](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000137_feed_completions_partition_label.sql#L58), and [000137:65](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000137_feed_completions_partition_label.sql#L65).

**Verification:** A populated rollback reproduction reduced two valid pen rows to one.

**Fix:** Make Down refuse destructive collapse or archive/export all per-pen rows before any irreversible downgrade.

## New P2 Findings

### AUTH-007 - Scoped feed responses disclose other parks

**Status:** New interaction in this range.

**Plain English:** A Park A operator receives Park A work rows but can see other parks' names/IDs in filter options and gets 403 if they select them.

**Evidence:** Packing builds filters from every tenant park at [service.go:537](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/service.go#L537). Transport's park-option query omits the authorized `ParkID` at [transport.go:119](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/transport.go#L119).

**Fix:** Build filter metadata from the same authorized park scope as the work rows.

### FEED-024 - All-parks experiment enrollment hides pen 201 onward

**Status:** New in this range.

**Plain English:** In a tenant with more than 200 operational pens, later pens never appear in the enrollment selector and there is no truncation warning.

**Evidence:** Admin requests one 200-row page at [feed-config.tsx:259](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L259), builds candidates only from that page at [feed-config.tsx:361](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L361), and ignores backend `has_more` from [repository.go:657](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L657).

**Fix:** Drain bounded pages or add paged/searchable enrollment.

### FEED-025 - Batch enrollment audit cannot identify the complete affected set

**Status:** New in this range.

**Plain English:** One action can enroll five feed items, but the audit ledger points to only one row, so investigators cannot reconstruct the complete original set later.

**Evidence:** Batch persistence deliberately calls the batch one authoring act but retains only the lowest-keyed row ID at [repository.go:552](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L552) and [repository.go:589](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L589). The ledger stores that one representative ID plus an opaque request hash at [repository.go:1483](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L1483); it stores neither the normalized request nor the full row-ID set. Calling it one act is reasonable, but the act is not reconstructible from its audit record after later edits.

**Fix:** Persist the complete affected-row set or immutable normalized request body.

### FEED-026 - Simultaneous first config writes return a false 500

**Status:** Preexisting, newly discovered.

**Plain English:** If two admins create the same empty rate/factor/schedule with different keys, one succeeds and the other receives an internal error instead of a deterministic conflict/current result.

**Evidence:** `FOR UPDATE` locks only an existing row at [repository.go:704](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L704). Both empty-key transactions can reach insert at [repository.go:720](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L720), and the unknown unique violation maps to 500 at [handler.go:891](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/http/handler.go#L891).

**Verification:** A real-PostgreSQL two-transaction probe made both writers run the production empty-key `SELECT ... FOR UPDATE` before either inserted. Both saw no row; writer one committed; writer two failed with `feed_ration_rates_natural_key_uidx` / SQLSTATE `23505`. The experiment-shed lock cited by the counter-review is called only by experiment writes at [repository.go:526](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/postgres/repository.go#L526) and does not protect ration-rate, shed-factor, or schedule first writes. The probe was removed after reproduction.

**Fix:** Serialize the natural key or handle insert conflict by loading and comparing the winning row.

### FEED-027 - Oversized decimals pass validation and fail as 500

**Status:** Preexisting, newly discovered; exposed by the new feed-item endpoint.

**Plain English:** A large number can look valid to the API but exceed PostgreSQL precision, producing a generic server error instead of a field validation message.

**Evidence:** Decimal normalization limits scale but not integer digits at [types.go:665](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/domain/types.go#L665). Columns such as energy are finite `numeric(10,3)` at [baseline migration:13500](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql#L13500).

**Fix:** Enforce each column's precision before persistence and return field errors.

### FEED-028 - `Infinity` is accepted and silently becomes missing

**Status:** New in this range.

**Plain English:** An author can enter `Infinity` or `1e309`, get success, and later discover the value was discarded.

**Evidence:** Frontend parsing rejects `NaN` but not non-finite values at [feed-config-actions.ts:188](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-actions.ts#L188) and [feed-config-actions.ts:208](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-actions.ts#L208). JSON serializes infinity to `null`, which the backend treats as absent at [service.go:468](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/app/service.go#L468).

**Fix:** Require `Number.isFinite` and backend finite/range validation.

### FEED-029 - Pen-catalog failure is shown as "every pen is configured"

**Status:** New in this range.

**Plain English:** A server outage looks like healthy empty data, so managers cannot distinguish failure from completion.

**Evidence:** The failed request becomes `null` at [feed-config.tsx:341](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L341), then an empty candidate list at [feed-config.tsx:361](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L361). The enroller renders normal empty copy at [feed-config-editor.tsx:528](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-editor.tsx#L528).

**Fix:** Keep failed, empty, loading, and complete states distinct.

### FEED-030 - All-parks location failure becomes false empty configuration

**Status:** Preexisting, newly discovered.

**Plain English:** If the location service fails, the page says no configuration exists instead of saying parks could not be loaded.

**Evidence:** Location failure returns empty arrays plus `available=false` at [herd-locations.ts:49](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/lib/api/herd-locations.ts#L49). Feed Config ignores `available` at [feed-config.tsx:233](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config.tsx#L233).

**Fix:** Fail visibly and do not render domain-empty states when scope loading failed.

### FEED-031 - Feed-config runtime errors violate OpenAPI

**Status:** Preexisting, newly discovered.

**Plain English:** Strict generated clients can reject ordinary feed-config errors, while trace and retry information promised by the contract is missing.

**Evidence:** OpenAPI requires `code`, `message`, `field_errors`, `trace_id`, and `retryable` at [app-api.yaml:11022](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/contracts/openapi/app-api.yaml#L11022). Runtime emits only `code`, `message`, and optional `field` at [handler.go:754](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feedconfig/adapters/http/handler.go#L754).

**Fix:** Use the canonical error-envelope writer and validate runtime responses against OpenAPI.

### FEED-032 - Feed-config save results are not announced to screen readers

**Status:** Preexisting, newly discovered.

**Plain English:** A screen-reader user can press Apply and receive no audible success or error, so they cannot know whether a live feeding change was accepted.

**Evidence:** Async results render as a normal `div` without `role=status`, `role=alert`, or `aria-live` at [feed-config-editor.tsx:92](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/admin-web/features/feed/feed-config-editor.tsx#L92).

**Fix:** Add appropriate live regions and focus management for validation errors.

### FEED-033 - Every packing completion reloads and rebuilds the whole park sheet

**Status:** New in this range.

**Plain English:** Completing many pens repeats the same full-park reads/calculation once per pen during the busiest feed period.

**Evidence:** Each completion calls `loadServedRows` and rebuilds every packing row at [packing_service.go:218](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/packing_service.go#L218). The loader reads all headers/cells for the park scope at [lifecycle.go:325](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/app/lifecycle.go#L325).

**Fix:** Resolve the exact immutable issued row directly by issue/shed/pen/session identity.

### FEED-034 - Pen identity is missing from completion audits and events

**Status:** New in this range.

**Plain English:** Two pens in one shed emit records that look identical, so downstream reconciliation cannot tell which pen was approved.

**Evidence:** Packing audit/event payloads omit partition at [packing_completions.go:399](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/packing_completions.go#L399) and [packing_completions.go:438](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/packing_completions.go#L438). Distribution has the same omission at [distribution_completions.go:402](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/distribution_completions.go#L402).

**Fix:** Include canonical partition identity in audit scope, before/after state, and completed events.

### FEED-035 - Feed Alerts shows distribution only

**Status:** Preexisting, newly discovered.

**Plain English:** Feed verifiers get Distribution, Packing, and Transport review tabs, but Alerts silently hides packing and transport alerts.

**Evidence:** Feed registers three categories at [bootstrap_copy.go:1321](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/workforce/app/bootstrap_copy.go#L1321). Alerts uses only `pages[0].category` at [bootstrap_copy.go:858](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/workforce/app/bootstrap_copy.go#L858).

**Fix:** Support a category set or aggregate all registered feed categories.

### MOB-009 - Offline-submitted feed work still looks untouched in the list

**Status:** Preexisting, newly discovered.

**Plain English:** The app says Queued, returns to the list, and shows the same task as Pending/tappable. Operators can repeat work because the list did not learn about the local submission.

**Evidence:** Navigation pops after queued status at [AppNavHost.kt:2430](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt#L2430). Resume assigns an equal state value and emits no refresh at [FeedDirectionViewModel.kt:167](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedDirectionViewModel.kt#L167). Gated completion flows do not update the list's local overlay.

**Fix:** Overlay queued outbox state onto work rows and explicitly refresh after returning.

## Changed Existing Findings

### FEED-001 - Closed as the original cross-pen collision

The original defect was one shed-level database key that made Part 3 mark Parts 4 and 5. Pen-aware natural keys and status maps remove that cause. `FEED-009` can still make a real completion look Pending, but that is a different canonicalization defect with its own reproduction and fix; it does not reopen the removed cross-pen collision.

### FEED-002 - Closed

Partition identity now reaches experiment authoring domain models, API requests, generated keys, pen-scoped status updates, and pagination tests. `FEED-009` concerns packing/distribution completion-key normalization, not experiment authoring, so it does not make this closure conditional.

### FEED-005 - Still open, narrowed

The new Android capture key now includes date and pen, fixing current-client collision. Rework still restores/reuses the old successful submit key instead of creating the fresh attempt required by backend rework, and upgrade migration is separately tracked as `MOB-008`/`FEED-011`.

### FEED-007 - Still open, narrowed to packing

Distribution now carries partition and the verification read can compose its location. Packing still declares shed/partition result fields but PostgreSQL leaves them empty at [packing_completions.go:184](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/postgres/packing_completions.go#L184); its bridge test injects labels manually at [packing_context_test.go:35](https://github.com/vgoats/goatos/blob/c8b475f6ebb2a45c03cc4d607a26824e55edd8d8/backend/internal/feeddirection/adapters/verificationbridge/packing_context_test.go#L35).

### REL-001 - Expanded

Migrations 136 and 138 repeat the retained-lock pattern: each `Up` runs in one transaction, so the earlier `ALTER TABLE` `ACCESS EXCLUSIVE` lock is retained while `VALIDATE CONSTRAINT` scans. Migration 136 has no lock or statement timeout and scans the growing feed-config write ledger. Migration 138 is better bounded (`lock_timeout=2s`, `statement_timeout=30s`), but after acquiring its metadata lock it can still block the shared verification queue until validation completes or the statement timeout aborts. `NOT VALID` followed by `VALIDATE` is only a lock-safety split when the validation runs in a separate transaction/migration.

### REL-002 - Expanded

Migration 137 repeats migration 135's stored-column rewrite plus non-concurrent index replacement on both completion tables. PostgreSQL probes confirmed physical rewrites and exclusive locks.

### REL-007 - Expanded

The validator remains red on historical debt and is also blind to migrations 136-138 because the new feed tables are absent and `verification_items` is excluded from the relevant checks.

### CI-001 - Expanded

The migration validator checks for Down markers but never executes Down. That is why `REL-011` can pass the gate; PostgreSQL-backed checks also remain label-gated.

### KERN-002 - Reconfirmed open

Packing/distribution completion still commits before verifier enqueue. A crash/failure can leave `pending_verification` with no queue item, and exact retry returns `NewlyPending=false` instead of healing it.

### FEED-003, FEED-004, FEED-006 - Reconfirmed open

- `FEED-003`: proof validation still accepts wrong role/media/subject combinations.
- `FEED-004`: transport HTTP DTO still drops required partition display fields.
- `FEED-006`: the removed legacy completion endpoint remains in OpenAPI/Android and can strand old outbox work.

## Counter-Review Adjudication

Claude accepted 31 of the 36 new findings without challenging them. Its seven proposed ledger changes were checked against the exact same snapshot and adjudicated as follows:

| Counter claim | Adjudication |
|---|---|
| Remove `FEED-013` | Rejected. The workflow filter is correct for a non-empty workflow, but the product's default request is intentionally empty/all-workflows. One existing header suppresses the gate for the missing due workflow; the sequence was reproduced. |
| Remove `FEED-026` | Rejected. The cited shed lock protects only experiment writes. Empty rate/factor/schedule keys have no lockable row; PostgreSQL `23505` was reproduced. |
| Downgrade/reframe `FEED-017` | Wording clarified, finding and P1 remain. Reactivation is deliberate adapter behavior, but it bypasses the separately modelled consequential restore action and has no stale-write protection. |
| Mark `FEED-020` partial/accepted | Rejected. A backend upsert with no reachable Add control proves a frontend workflow gap; refusing to invent a rate is not approval to make authoring inaccessible. |
| Weaken `FEED-025` | Wording clarified, P2 remains. One batch may be one act, but its ledger still cannot identify or reconstruct the act's complete affected set. |
| Narrow the `REL-001` expansion | Rejected. Migration 138 is bounded, but both files retain the earlier exclusive lock across same-transaction validation; migration 136 is unbounded. |
| Make `FEED-001`/`FEED-002` closure conditional on `FEED-009` | Rejected. Ledger IDs track root defects. `FEED-001`'s cross-pen key and `FEED-002`'s missing authoring identity are fixed; `FEED-009` is a separate completion-key mismatch. |

**Adjudicated ledger:** **128 open (`1 P0`, `79 P1`, `48 P2`)**. No finding was removed.

## Unchanged Baseline Findings

The following 86 findings remain open with the same evidence and layman explanation in the [previous full ledger](whole-project-audit-d98a8a6.md):

`MOB-001`, `SEC-001`, `SEC-002`, `AUTH-001`, `AUTH-002`, `AUTH-003`, `AUTH-004`, `ID-001`, `PROOF-001`, `DATA-001`, `WEB-001`, `CEO-001`, `SEC-003`, `SHIFT-001`, `SHIFT-002`, `SHIFT-003`, `VAX-002`, `VAX-003`, `VAX-004`, `VAX-005`, `VAX-006`, `VAX-007`, `VAX-008`, `WEIGH-001`, `WEIGH-002`, `WEIGH-003`, `WEIGH-004`, `FEED-003`, `FEED-004`, `FEED-006`, `PROC-001`, `PROC-002`, `PROC-003`, `PROC-004`, `PROC-005`, `KERN-001`, `KERN-002`, `KERN-003`, `KERN-004`, `KERN-005`, `KERN-006`, `KERN-007`, `DATA-002`, `WEB-002`, `WEB-003`, `MOB-002`, `MOB-003`, `CEO-002`, `OPS-001`, `REL-003`, `REL-004`, `REL-005`, `REL-006`, `CI-002`, `VAX-009`, `WEIGH-005`, `WEIGH-006`, `HERD-001`, `HERD-002`, `HERD-003`, `KERN-008`, `KERN-009`, `DATA-003`, `OPS-002`, `WEB-004`, `WEB-005`, `WEB-006`, `WEB-007`, `WEB-008`, `WEB-009`, `CEO-003`, `CEO-004`, `CEO-005`, `MOB-004`, `MOB-005`, `MOB-006`, `MOB-007`, `REL-008`, `REL-009`, `CI-003`, `CI-004`, `CI-005`, `CI-006`, `CI-007`, `CI-008`, `CI-009`.

The changed-but-open findings `FEED-005`, `FEED-007`, `REL-001`, `REL-002`, `REL-007`, and `CI-001` are described above and are not included in this unchanged list.

## Verification

Passed:

- Full backend internal suite: `go test ./internal/...`.
- Targeted feed-config, feed-direction, verification, workforce, permissions, and middleware tests.
- Real-PostgreSQL reproduction of cross-pen idempotency replay, `Part 3` fresh-key failure, and nonexistent-pen acceptance.
- Fresh PostgreSQL migration `000001 -> 000138`.
- Previous-binary completion SQL against schema 138 reproduced PostgreSQL `42P10`.
- Migration 137 Down reproduction confirmed two valid pen rows become one.
- Admin web: 281 tests passed; clean-snapshot TypeScript typecheck passed.
- Android targeted Staging unit tests and `FeedCaptureGroupKeyTest` passed.
- API client generation check and `git diff --check` passed.

Known failing controls / boundaries:

- Both committed migration validators still exit 1 on historical `000022`, `000031`, and `000107` debt (`REL-007`).
- One agent's full opt-in feed-direction PostgreSQL package hit `TestScheduleReaderReadsTheDispatchClock` (`0` clocks, expected `2`); it was not attributed to the findings above and needs separate rerun/adjudication.
- No physical-device run, cloud deployment, or production-scale load test was performed.
