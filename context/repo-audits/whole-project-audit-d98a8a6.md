# Goat OS Whole-Project Audit

Reviewed snapshot: `d98a8a6f4cd452aa56681c52f91c06054c089081` from `vgoats/goatos`.

This is a whole-repository snapshot review, not a last-100-commits review. The audit covered the kernel, backend, PostgreSQL, admin web, investor shadow web, Android, operator mobile, contracts, analytics, notifications, security, infrastructure, deployment, CI, performance, and canonical business rules.

Counter-review amendment (2026-08-09): the ledger now contains **94 findings** (`1 P0`, `59 P1`, `34 P2`). `VAX-001` / `GOS100-002` was removed after the existing real-Postgres reject -> reshoot -> approve regression passed and source inspection confirmed newest-closed verdict ordering. `WEIGH-005` was narrowed from six claims to four verified defects.

Severity:

- `P0`: stop-ship data isolation or catastrophic integrity risk.
- `P1`: serious wrong behavior, security exposure, data loss, or operational outage risk.
- `P2`: important correctness, performance, reliability, UX, or release-control defect.

## P0

### MOB-001 - Logout can restore the previous user's data after it was wiped

**Where:** [LogoutCoordinator.kt:53](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/LogoutCoordinator.kt#L53), [SyncWorker.kt:143](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/sync/SyncWorker.kt#L143)

**Plain English:** Logout wipes Room and the outbox before asynchronous sync cancellation has definitely finished. An old user's in-flight worker can write old-tenant data back after the wipe, so a later user on the shared phone can see it.

**Fix:** Freeze and await all sync work before clearing data, wipe transactionally, and reject late writes with a principal/session generation token.

## P1 - Security And Authorization

### SEC-001 - Investor APIs have no authentication

**Where:** [dashboard layout](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/app/(dashboard)/layout.tsx#L4), [mortality route](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/app/api/mortality/deaths/route.ts#L8), [BigQuery client](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/lib/bigquery.ts#L9)

**Plain English:** If this runnable app is exposed with Google credentials, any network caller can access its 34 data APIs, including animal mortality and shifting data. There is no user, role, or tenant check.

**Fix:** Make the shadow app impossible to deploy by default, or add authenticated tenant-scoped investor authorization to every route.

### SEC-002 - Investor API input can change BigQuery SQL

**Where:** [business route:8](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/app/api/business/route.ts#L8), [delta route:10](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/app/api/delta/route.ts#L10)

**Plain English:** The caller-controlled `farm` value is pasted directly into SQL. A malicious value can alter the query, read other accessible warehouse data, or trigger expensive scans. SEC-001 makes this remotely reachable whenever the app is exposed.

**Fix:** Use BigQuery parameters and validate the farm against the caller's allowed farms.

### AUTH-001 - Park-scoped operators can read and execute another park's work

**Where:** [auth.go:393](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/platform/httpmiddleware/auth.go#L393), [health handler.go:108](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/health/adapters/http/handler.go#L108), [shifting handler.go:383](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/adapters/http/shifting_execution_handler.go#L383)

**Plain English:** A Park A grant is accepted for `/app/*`, but several Health and Counts handlers use only the tenant or a caller-supplied park/resource ID. A Park A operator can list or complete Park B health work and view or execute Park B movements.

**Fix:** Derive allowed parks from the grant that supplies the capability and atomically recheck the stored resource park on every ID-based write.

### AUTH-002 - Approval permission from one park can be combined with another park grant

**Where:** [approval_handler.go:351](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/adapters/http/approval_handler.go#L351), [approval_handler.go:382](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/adapters/http/approval_handler.go#L382), [approval_service.go:201](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/app/approval_service.go#L201)

**Plain English:** The handler flattens roles and separately picks a park scope. A person with approval power in Park B and an unrelated Park A grant can approve Park A birth, death, or shifting work.

**Fix:** Bind each capability to the scope of the same grant and authorize against a capability-specific park set.

### AUTH-003 - Park-scoped users can complete or download another park's proof

**Where:** [proof handler.go:178](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/proof/adapters/http/handler.go#L178), [proof repository.go:151](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/proof/adapters/postgres/repository.go#L151)

**Plain English:** Proof lookup checks only tenant plus proof UUID. If a Park A operator learns a Park B proof UUID, they can obtain a signed media URL or mutate the proof.

**Fix:** Resolve the proof's canonical task, park, and shed, require a matching grant, and limit unfinished mutations to the uploader.

### AUTH-004 - Directors can mutate modules they do not own

**Where:** [permissions_orgrole.go:264](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/permissions/permissions_orgrole.go#L264), [permissions_orgrole.go:296](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/permissions/permissions_orgrole.go#L296), [routes.go:581](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/permissions/routes.go#L581)

**Plain English:** Generic director capabilities are not limited by business vertical. For example, a breeding director can publish vaccination protocols and campaigns.

**Fix:** Mark routes with their owning vertical and require that the grant supplying the capability is authorized for that vertical.

### ID-001 - A direct endpoint bypasses death approval and the two-video rule

**Where:** [identity handler.go:47](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/identity/adapters/http/handler.go#L47), [goat_lifecycle.go:155](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/identity/app/goat_lifecycle.go#L155)

**Plain English:** A principal with health-write permission can call `critical-death-exit` directly. The goat is marked dead without the Counts death request, two required videos, verification, investigation, or admin acceptance.

**Fix:** Remove the public primitive or require and atomically consume an approved death request containing both proofs.

### PROOF-001 - Direct proof uploads have no effective size, MIME, or checksum enforcement

**Where:** [GCS storage.go:57](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/proof/adapters/storage/gcs/storage.go#L57), [storage.go:103](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/proof/adapters/storage/gcs/storage.go#L103)

**Plain English:** A user can upload a multi-gigabyte or wrong-content object through a signed URL, complete it with `size_bytes=0`, and have it accepted. The supplied digest is not verified and a GCS generation is stored as if it were a content hash.

**Fix:** Enforce per-proof size and MIME rules against object metadata, verify SHA-256, delete rejected objects, and apply tenant quotas.

### DATA-001 - Analytics combines all tenants and labels the result as one tenant

**Where:** [funnel.go:22](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/cmd/analytics-rollup/funnel.go#L22), [engagement.go:18](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/cmd/analytics-rollup/engagement.go#L18), [BootstrapViewModel.kt:225](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/boot/BootstrapViewModel.kt#L225)

**Plain English:** Android exports tenant identity, but rollup SQL does not filter or group by it. When two tenants share Firebase/GA4, all funnel, journey, DAU, and session activity is stamped with one configured tenant ID.

**Fix:** Filter or group every analytics query by tenant and write separate tenant rows.

### WEB-001 - Admin bootstrap cache can return one verifier's duties to another

**Where:** [compiler.go:92](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/adminui/app/compiler.go#L92), [verifier_lens.go:145](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/adminui/app/verifier_lens.go#L145)

**Plain English:** The cache key omits actor ID. Two verifiers with the same grants but different module duties can share the first actor's navigation and assignment metadata for up to 60 seconds.

**Fix:** Include actor and duty revision in the key, or apply actor-specific lenses after caching only common data.

### CEO-001 - CEO AI cache can attach another leader's conversation ID

**Where:** [orchestrator.go:211](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/ceoai/app/orchestrator.go#L211), [orchestrator.go:621](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/ceoai/app/orchestrator.go#L621), [ceo-ai-panel.tsx:322](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/ceo-ai/ceo-ai-panel.tsx#L322)

**Plain English:** The cache key uses tenant, day, and question, but not actor or conversation. Two leaders asking the same question can receive the first leader's conversation ID; the second request is not stored or audited.

**Fix:** Cache answer content only. Always persist and audit each request with its own conversation and actor.

### SEC-003 - Deployed web apps use known vulnerable Next.js versions

**Where:** [admin package.json:48](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/package.json#L48), [investor package.json:18](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/package.json#L18)

**Plain English:** `npm audit --omit=dev` reports high-severity production vulnerabilities. Admin Next `16.2.9` is affected by Server Action denial-of-service and related advisories; investor Next `14.2.35` is also vulnerable.

**Fix:** Upgrade to supported patched versions, refresh locks, and make high-severity production audits required in CI.

## P1 - Herd, Shifting, Vaccination, And Weighing

### SHIFT-001 - Temporary ICU/quarantine moves cannot store the return/checkpoint plan (revised GOS100-001)

**Where:** [app_write_handler.go:243](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/adapters/http/app_write_handler.go#L243), [critical guardrails:74](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/docs/features/critical-animal-action-guardrails.md#L74)

**Plain English:** Your clarification changes the old finding. A one-goat catch is correct when the goat is vaccinated in its temporary shed on the last medically safe day before return. The bug is that the system cannot store or consume the expected return/checkpoint, extension, clinical defer, or last-safe-day decision, so it cannot tell that correct catch from an avoidable later home-shed trip.

**Fix:** Add a temporary-placement episode with expected return/checkpoint, clinical extensions, release state, and vaccination planning that consumes those dates.

### SHIFT-002 - ICU/quarantine movements use the routine shifting path

**Where:** [shifting_destinations.go:237](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/app/shifting_destinations.go#L237), [goat_relocate.go:61](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/identity/adapters/postgres/goat_relocate.go#L61)

**Plain English:** A healthy goat can be moved into ICU/quarantine, or a sick goat moved out, without an episode, open-health check, destination fitness, release criteria, or return/checkpoint date.

**Fix:** Fail closed for critical entry and exit until a policy decision and temporary-placement episode are supplied.

### SHIFT-003 - Shifting head counts can contradict the named goats

**Where:** [app_write_handler.go:751](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/adapters/http/app_write_handler.go#L751), [feed_projected_counts.go:184](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/counts/adapters/postgres/feed_projected_counts.go#L184)

**Plain English:** A request can name one goat but claim `head_count=12`. One goat moves, while feed projections add/subtract twelve animals and produce wrong rations.

**Fix:** Derive impacts from the canonical selected goats or require exact cohort totals and dimensions transactionally.

### VAX-002 - Severity filtering hides broken work after page one (GOS100-005)

**Where:** [execution-board.tsx:270](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/vaccination-execution/execution-board.tsx#L270), [server.ts:1285](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/lib/api/server.ts#L1285)

**Plain English:** The browser fetches one capped page, then applies severity locally. Severe rows on later pages are reported as zero.

**Fix:** Apply severity in the backend before pagination and return true totals.

### VAX-003 - Operator selection and configured headcount are ignored

**Where:** [vaccination repository.go:3668](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/vaccinationexecution/adapters/postgres/repository.go#L3668), [repository.go:3800](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/vaccinationexecution/adapters/postgres/repository.go#L3800)

**Plain English:** Selecting A and B with `N=2` can still assign C and D. Selected-only changes are not treated as roster changes, and reassignment ranks all active shift operators.

**Fix:** Treat selected IDs and N as authoritative roster changes and replan through the canonical assignment resolver.

### VAX-004 - Missed vaccination work keeps the old shed after movement or ICU return

**Where:** [obligation repository.go:4737](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/obligation/adapters/postgres/repository.go#L4737), [repository.go:1292](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/obligation/adapters/postgres/repository.go#L1292)

**Plain English:** Rescoping updates scheduled/due/deferred rows but not missed rows. Rescheduling a missed dose copies the old scope, so a moved or recovered goat can be planned in its former shed.

**Fix:** Resolve canonical current location whenever missed work is rescoped or rescheduled.

### VAX-005 - Android turns a task-roster 404 into writable shed-wide data

**Where:** [ExecutionRepository.kt:356](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/ExecutionRepository.kt#L356), [ScanViewModel.kt:703](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt#L703)

**Plain English:** If a task roster returns 404, Android fetches the whole shed and stores those goats under the requested task. The operator can then record another task's goat against the stale task.

**Fix:** Fail closed, or keep shed fallback rows in a separate read-only scope.

### VAX-006 - Full vaccine schedule silently stops at 500 rows

**Where:** [full-vaccine-schedule.tsx:331](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/preventive-care-vaccination/full-vaccine-schedule.tsx#L331), [handler.go:470](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/vaccinationexecution/adapters/http/handler.go#L470)

**Plain English:** The page requests 2,000 rows, the server clamps to 500, and there is no cursor or truncation flag. A 501-row schedule looks complete while hiding work and conflicts.

**Fix:** Add keyset pagination and visibly drain or paginate all rows.

### VAX-007 - The required post-breeding vaccine hold is unreachable

**Where:** [vaccination rules:458](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/docs/preventive-care-vaccination/vaccination-rules.md#L458), [schedule_policy.go:389](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/vaccination/app/schedule_policy.go#L389), [goat_lifecycle.go:572](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/identity/adapters/postgres/goat_lifecycle.go#L572)

**Plain English:** The scheduler understands `bred`/`breeding`, but identity rejects those statuses. Accepted statuses therefore bypass the required one-month vaccine hold.

**Fix:** Define one shared breeding-status vocabulary across identity, scheduler, migrations, contracts, and clients.

### VAX-008 - Reproductive updates reuse stale dates and accept future dates

**Where:** [identity app goat_lifecycle.go:684](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/identity/app/goat_lifecycle.go#L684), [postgres goat_lifecycle.go:591](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/identity/adapters/postgres/goat_lifecycle.go#L591)

**Plain English:** Mark pregnant with a date, mark non-pregnant without one, then mark pregnant again: `COALESCE` reuses the old date. Future dates are also accepted, producing wrong medical safety windows.

**Fix:** Validate the full effective state under lock, reset dates on transitions, and reject future or impossible sex/status/date combinations.

### WEIGH-001 - Rejected weights remain in analytics

**Where:** [growth.go:57](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/postgres/growth.go#L57), [verification_verdict.go:371](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/postgres/verification_verdict.go#L371)

**Plain English:** Analytics excludes status `rejected`, but rejection is actually stored as `rework`. A known false weight still changes growth, sale readiness, demographics, shed summaries, and load reports.

**Fix:** Use one shared explicit analytics eligibility predicate that excludes `rework`.

### WEIGH-002 - Android refresh deletes accepted observations after row 200 (GOS100-016)

**Where:** [WeighingRepository.kt:1877](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/weighing/WeighingRepository.kt#L1877), [WeighingRepository.kt:1907](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/weighing/WeighingRepository.kt#L1907)

**Plain English:** Hydration stops at 200, then accepted local rows missing from that partial page are deleted. Reopening a 500-goat shed can remove 300 accepted drafts and make submission look incomplete.

**Fix:** Drain all pages before generation-based pruning.

### WEIGH-003 - Offline capture time is discarded

**Where:** [WeighingRepository.kt:2246](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/weighing/WeighingRepository.kt#L2246), [backend repository.go:1820](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/postgres/repository.go#L1820)

**Plain English:** Room stores capture time, but the network DTO omits it and the backend stamps upload time. Measurements taken on several offline days can all appear to happen on sync day, corrupting history and ADG.

**Fix:** Send immutable `observed_at` with bounded clock-skew validation.

### WEIGH-004 - Admin weight charts silently omit sheds after 300 (GOS100-015)

**Where:** [shed_weights.go:192](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/domain/shed_weights.go#L192), [weights.tsx:153](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/weighing/weights.tsx#L153)

**Plain English:** The endpoint caps at 300 sheds while the UI treats the response as the whole estate and calculates headlines from incomplete data.

**Fix:** Add pagination and server-computed complete totals.

## P1 - Feed, Procurement, Kernel, And Operations

### FEED-001 - Partitioned packing and distribution share one completion

**Where:** [feed service.go:320](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/app/service.go#L320), [packing_service.go:56](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/app/packing_service.go#L56)

**Plain English:** Completing Part 3 marks every partition in that shed/session/workflow with the same status. Parts 4 and 5 cannot submit independent proof because the database key allows only one completion.

**Fix:** Carry canonical partition identity through requests, rows, unique keys, fingerprints, events, verifier items, and status maps.

### FEED-002 - Experiment-feed authoring cannot address partitions

**Where:** [feedconfig repository.go:382](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feedconfig/adapters/postgres/repository.go#L382), [types.go:316](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feedconfig/domain/types.go#L316)

**Plain English:** Admin cannot distinguish Part 3 from Part 4. Updates can select an arbitrary partition row, while inserts create a `whole` row that does not drive partition-specific formulation.

**Fix:** Add partition identity across domain, API, UI, ordering, lookup, upsert, and status operations.

### FEED-003 - Feed proof gates accept the wrong proof

**Where:** [validator.go:47](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/adapters/proof/validator.go#L47), [distribution_service.go:123](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/app/distribution_service.go#L123)

**Plain English:** One unrelated completed photo/document can satisfy a required packing video, and the same proof can satisfy both distribution and water evidence. Shed, role, capture source, and media type are not adequately enforced.

**Fix:** Require correct role, MIME, live capture source, subject shed, completion state, and distinct proof IDs.

### FEED-004 - Feed transport drops partition display fields (GOS100-008)

**Where:** [handler.go:128](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/adapters/http/handler.go#L128), [app-api.yaml:9350](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/contracts/openapi/app-api.yaml#L9350)

**Plain English:** The repository computes partition and operational-location labels and Android expects them, but the HTTP DTO drops them. Partition tasks show a bare shed name.

**Fix:** Populate both fields in the transport DTO.

### FEED-005 - Android reuses yesterday's feed proof and idempotency key

**Where:** [FeedDistributionCompleteViewModel.kt:74](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedDistributionCompleteViewModel.kt#L74), [FeedPackingCompleteViewModel.kt:70](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedPackingCompleteViewModel.kt#L70)

**Plain English:** Tomorrow's task can restore yesterday's proof and submission key. The new date then conflicts with the old idempotency fingerprint; partition tasks can collide too.

**Fix:** Include park, partition, and target date in durable keys and rotate drafts after terminal completion or rework.

### FEED-006 - A removed endpoint strands older Android offline work

**Where:** [feed handler.go:60](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/adapters/http/handler.go#L60), [app-api.yaml:3677](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/contracts/openapi/app-api.yaml#L3677), [SyncEngine.kt:625](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-sync/src/main/kotlin/sg/mesha/goatos/core/sync/SyncEngine.kt#L625)

**Plain English:** Runtime removed the old completion route, but OpenAPI and Android still use it. An older/offline `FEED_DIRECTION_COMPLETE` operation reconnects to 404/403 and can never drain.

**Fix:** Keep a compatibility endpoint through the queue-drain window or migrate pending Room operations before removing the contract/client path.

### PROC-001 - Procurement health checks allow a goat/load mismatch

**Where:** [procurement service.go:490](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/app/service.go#L490), [repository.go:708](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/adapters/postgres/repository.go#L708)

**Plain English:** A health result for a goat on Load A can be recorded against Load B. Load B changes status and the goat's vaccination work may be cancelled even though the membership row did not match.

**Fix:** Require `(tenant, load, goat)` membership under the same transaction and reject zero updated rows.

### PROC-002 - Any known proof UUID can unlock dispatch and intake

**Where:** [service.go:542](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/app/service.go#L542), [repository.go:1041](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/adapters/postgres/repository.go#L1041)

**Plain English:** An unrelated, pending, or another tenant's known proof UUID can satisfy the truck-proof gate, letting a load enter transit or intake without valid handoff evidence.

**Fix:** Require completed tenant-owned proof bound to the load, action, expected media, and capture policy.

### PROC-003 - Re-adding a goat rewinds advanced procurement state

**Where:** [service.go:339](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/app/service.go#L339), [repository.go:328](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/adapters/postgres/repository.go#L328)

**Plain English:** Calling Add Goat again with a fresh key after acceptance, transit, or intake overwrites advanced state with caller values/defaults such as candidate/pending.

**Fix:** Reject re-add after progression and use a versioned audited correction command for legal changes.

### PROC-004 - Procurement silently loses vaccination cancellations

**Where:** [service.go:519](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/app/service.go#L519), [service.go:536](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/app/service.go#L536)

**Plain English:** If cancellation fails after a failed/deferred/rejected procurement decision commits, the API still succeeds and exact replay skips cancellation. Ineligible goats retain vaccination obligations.

**Fix:** Persist cancellation in the procurement transaction and reconcile unfinished commands.

### PROC-005 - A valid 1,000-goat intake runs thousands of statements in one short transaction

**Where:** [repository.go:24](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/adapters/postgres/repository.go#L24), [repository.go:1637](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/procurement/adapters/postgres/repository.go#L1637)

**Plain English:** Maximum intake performs roughly five database operations per goat under a three-second transaction timeout. Large valid loads can roll back and hold locks.

**Fix:** Use set-based SQL/`UNNEST` or resumable bounded chunks.

### KERN-001 - Missed-work notifications always fail the database constraint

**Where:** [obligation_missed_notify.go:29](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/notificationbridge/obligation_missed_notify.go#L29), [baseline constraint:3443](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql#L3443)

**Plain English:** The producer emits `obligation_missed`, but the database allowlist does not contain it. The event retries and no operator, park head, or director push is queued.

**Fix:** Add the type through a forward migration and a real-Postgres missed-event test.

### KERN-002 - Pending-verification work can commit without a verifier item (GOS100-009)

**Where:** [tasks service.go:212](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/tasks/app/service.go#L212), [feed packing_service.go:136](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/app/packing_service.go#L136)

**Plain English:** Birth/death, milk, feed packing/distribution/transport, and shifting can commit `pending verification`, then enqueue after commit. A crash or enqueue failure leaves work pending forever with no reviewer. Some exact retries explicitly skip re-enqueue.

**Fix:** Write a verifier-enqueue outbox row in the same transaction and reconcile historical gaps.

### KERN-003 - Unresolvable birth/death verdict routing defects are silently acknowledged (GOS100-011)

**Where:** [tasks service.go:472](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/tasks/app/service.go#L472), [event_handlers.go:272](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/tasks/app/event_handlers.go#L272)

**Plain English:** When a verdict points to no birth/death workflow, the code calls that a routing defect, logs a warning, and returns success. Returning success permanently removes the durable event even though no workflow changed and no manual-review or dead-letter record was created. Other error types do retry; this finding is specifically about the swallowed `ErrNotFound` routing defect.

**Fix:** Persist a process exception/awaiting-application state before acknowledging.

### KERN-004 - Weighing verdicts apply but never receive a durable verifier receipt

**Where:** [kernel bus.go:105](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/kernelstages/bus.go#L105), [verification_verdict_handler.go:200](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/app/verification_verdict_handler.go#L200)

**Plain English:** Production creates the weighing handler without an apply acker. The observation changes, but verification remains forever as "decided, not yet in effect."

**Fix:** Wire the acker in both production builders and make receipt failure durable/retryable.

### KERN-005 - Successful external deliveries can retry forever

**Where:** [outbox repository.go:54](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/outbox/adapters/postgres/repository.go#L54), [notification repository.go:196](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/notification/adapters/postgres/repository.go#L196)

**Plain English:** If publish/send succeeds but `MarkPublished`/`MarkSent` fails, stale reclaim decrements the attempt count. Repeated completion-write failures can bypass max attempts and duplicate domain events or notifications indefinitely.

**Fix:** Persist attempt-started before external I/O and never refund an ambiguous post-send attempt.

### KERN-006 - Kernel stage timeouts can exceed the worker cadence

**Where:** [supervisor.go:121](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/platform/worker/supervisor.go#L121), [kernel-worker main.go:167](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/cmd/kernel-worker/main.go#L167)

**Plain English:** Nine stages run serially with 75-second budgets on a five-minute cadence. A degraded pass can take 675 seconds, so later inventory/SOP work starts many minutes late and ticks collapse.

**Fix:** Run independently locked stages concurrently or use one shared deadline below the cadence.

### KERN-007 - Outbox claiming is tenant-unfair and row-at-a-time

**Where:** [outbox repository.go:87](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/outbox/adapters/postgres/repository.go#L87), [repository.go:143](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/outbox/adapters/postgres/repository.go#L143)

**Plain English:** One tenant's old 50,000-message backlog can occupy every claim and starve another tenant. A 500-row claim also performs 501 serial database operations.

**Fix:** Claim with one `UPDATE ... RETURNING` and bounded per-tenant quotas/round-robin fairness.

### DATA-002 - Proof retention deletes canonical metadata but may leave media

**Where:** [proof repository.go:593](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/proof/adapters/postgres/repository.go#L593), [housekeeping.go:126](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/kernelstages/housekeeping.go#L126), [gcs.tf:1](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/infra/envs/stg/gcs.tf#L1)

**Plain English:** Expiry hard-deletes hashes, audit links, decisions, and hold state while the versioned bucket has no matching lifecycle rule. Manual deletion also removes the row before storage deletion, so a GCS failure becomes unrecoverable.

**Fix:** Retain tombstoned metadata and use a durable, version-aware media-deletion state machine.

### WEB-002 - Admin root invents `/vaccination` when bootstrap fails (GOS100-004)

**Where:** [admin page.tsx:20](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/app/(admin)/page.tsx#L20)

**Plain English:** If backend navigation/bootstrap is unavailable, `/` redirects to a made-up vaccination fallback instead of showing that authorization/navigation truth could not load.

**Fix:** Redirect only after successful bootstrap; render a fail-closed unavailable state otherwise.

### WEB-003 - Procurement Source Entry launches up to 201 requests per render

**Where:** [source-entry-board.tsx:151](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/procurement/source-entry-board.tsx#L151), [source-entry-board.tsx:168](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/procurement/source-entry-board.tsx#L168)

**Plain English:** A 200-load page makes one list call plus 200 concurrent detail calls during SSR. One page can saturate backend/database connections and time out.

**Fix:** Return card fields in the list or add one bounded batch-detail endpoint.

### MOB-002 - Re-recording can delete an upload already in flight or completed

**Where:** [SyncRepository.kt:1186](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/SyncRepository.kt#L1186)

**Plain English:** If dispatch wins the race while the user taps Re-record, unconditional deletion removes the outbox row even though the server request can still succeed. The device loses reconciliation and may upload a replacement too.

**Fix:** Atomically cancel only queued/failed uploads, block or await in-flight work, and delete successful server proof before clearing the draft.

### MOB-003 - Android materializes the entire active outbox on every change

**Where:** [OutboxDao.kt:63](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-database/src/main/kotlin/sg/mesha/goatos/core/database/outbox/OutboxDao.kt#L63), [OutboxStore.kt:157](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/sync/OutboxStore.kt#L157)

**Plain English:** A long-offline phone can have thousands of payload-bearing commands. Every mutation re-reads, maps, stores, and sends the entire queue through UI state, causing jank or out-of-memory exactly when offline resilience is needed.

**Fix:** Observe exact aggregate counts plus a bounded recent-row projection without payloads.

### CEO-002 - CEO AI budget resets on restart, races across replicas, and uses UTC day

**Where:** [ceoai service.go:118](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/ceoai/service.go#L118), [budget.go:37](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/ceoai/app/budget.go#L37)

**Plain English:** Production uses an in-memory counter. Restarting or hitting another replica bypasses the limit, concurrent requests do not reserve atomically, and IST users reset at 05:30 instead of midnight. Actual provider retries and costs are undercounted.

**Fix:** Wire the existing persistent IST-aware budget and reserve/reconcile actual provider usage atomically.

### OPS-001 - `/readyz` stays green when schema-version checking fails

**Where:** [api.go:905](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/bootstrap/api.go#L905), [api.go:918](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/bootstrap/api.go#L918)

**Plain English:** If Postgres ping works but migration-table access/version decoding fails, readiness still returns success. Traffic is sent to a binary whose schema compatibility is unknown.

**Fix:** Treat every applied-version error as not ready and return 503 with a bounded diagnostic.

## P1 - Database, CI, And Release

### REL-001 - Migrations 000022, 000031, and 000107 can block hot tables

**Where:** [000022:21](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/migrations/postgres/000022_temporary_identifier_type.sql#L21), [000031:39](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/migrations/postgres/000031_shifting_verification_gate.sql#L39), [000107:34](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/migrations/postgres/000107_notification_requeue_tracking.sql#L34)

**Plain English:** Constraints are dropped/re-added/validated while retaining exclusive locks, and 000107 adds a validated check with no lock timeout. Production identifier, shifting, or notification writes can stop for a table scan.

**Fix:** Use bounded lock timeouts, `NOT VALID`, separate validation, and forward migrations for already-recorded files.

### REL-002 - Migration 000135 rewrites and blocks the feed issue table (GOS100-003)

**Where:** [000135:22](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/migrations/postgres/000135_feed_direction_issue_rows_partition_label.sql#L22), [000135:33](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/migrations/postgres/000135_feed_direction_issue_rows_partition_label.sql#L33)

**Plain English:** A stored generated column rewrites rows, then the live unique index is dropped and rebuilt non-concurrently. Feed reads/writes can become unavailable during deploy.

**Fix:** Backfill safely, build a new index concurrently, then swap in a short no-transaction step.

### REL-003 - Release tagging treats a mismatched remote tag as success (GOS100-012)

**Where:** [create-release-tag.sh:75](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/release/create-release-tag.sh#L75), [create-release-tag.sh:80](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/release/create-release-tag.sh#L80)

**Plain English:** Local tag SHA is checked, but an existing remote tag is only checked for existence. The script can report success while GitHub points that release name to another commit.

**Fix:** Fetch/peel the remote tag and compare its commit before every success path.

### REL-004 - Canonical STG deploy accepts stale, dirty, or untracked source (GOS100-013 expanded)

**Where:** [stg-clouddeploy-release.sh:23](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/deploy/stg-clouddeploy-release.sh#L23), [stg-clouddeploy-release.sh:67](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/deploy/stg-clouddeploy-release.sh#L67)

**Plain English:** The script does not require `HEAD == origin/main` or an exact-SHA green CI receipt. Its dirty check ignores untracked files, while Docker and Cloud Deploy consume `.`. Uncommitted bytes can ship under a clean commit label.

**Fix:** Fetch and compare full SHAs, require the exact-SHA receipt, reject full porcelain output, and build from an archived clean commit.

### REL-005 - Active workflows create a forbidden second STG deployment path

**Where:** [deploy contract](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/context/deploy-contract.json#L3), [stg-deploy.yml:3](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/.github/workflows/stg-deploy.yml#L3)

**Plain English:** Push/workflow-dispatch actions can build and deploy STG even though the canonical contract says staging deploys must use the verified manual origin/main path. Two release authorities can drift.

**Fix:** Remove/disable the second deployment triggers.

### REL-006 - Release identity uses mutable tags instead of immutable digests

**Where:** [backend Dockerfile:1](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/Dockerfile#L1), [admin Dockerfile:1](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/Dockerfile#L1), [deploy script:67](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/deploy/stg-clouddeploy-release.sh#L67)

**Plain English:** Mutable base-image and short-SHA tags can point to different bytes on rebuild. Verification compares tag text rather than the resolved digest, so different artifacts can claim the same release identity.

**Fix:** Pin bases and deployments by digest, reject conflicting tag reuse, and verify running digests.

### CI-001 - Normal acceptance and staging gates skip database-backed safety checks

**Where:** [ci.yml:25](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/.github/workflows/ci.yml#L25), [run-local-ci.sh:464](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/ci/run-local-ci.sh#L464), [stg-pr-gate.yml:43](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/.github/workflows/stg-pr-gate.yml#L43)

**Plain English:** Ordinary PR/push/local runs disable Postgres tests and do not unconditionally run the hot-migration validator. The staging check is even labelled integration while DB tests require an optional label. Unsafe migrations and transaction bugs can land green.

**Fix:** Require migration/static DB gates for backend changes and exact-SHA Postgres replay/tests before staging.

### CI-002 - Android CI tests only the app module

**Where:** [run-local-ci.sh:649](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/ci/run-local-ci.sh#L649), [stg-pr-gate.yml:138](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/.github/workflows/stg-pr-gate.yml#L138)

**Plain English:** `:app:testStgReleaseUnitTest` does not run library-module tests. Core-data, permissions, calendar, and feature-auth can be red while the required Android gate is green.

**Fix:** Add and require one aggregate task that compiles and runs every module's tests for the supported variant.

## P2 - Product Correctness And Performance

### VAX-009 - Android vaccination alerts stop after 20 (GOS100-006)

**Where:** [VaccinationAlertsRepository.kt:61](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/alerts/VaccinationAlertsRepository.kt#L61), [VaccinationAlertsViewModel.kt:66](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/feature/feature-vaccination/src/main/kotlin/sg/mesha/goatos/feature/vaccination/alerts/VaccinationAlertsViewModel.kt#L66)

**Plain English:** The API provides a next cursor, but Android fetches only page one. Alert 21 and later are unreachable.

**Fix:** Persist and drain/append cursor pages with a non-advancing-cursor guard.

### FEED-007 - Packing and distribution verifier items lose shed/partition labels (GOS100-010)

**Where:** [packing_completions.go:183](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/adapters/postgres/packing_completions.go#L183), [packing_service.go:159](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/app/packing_service.go#L159), [packing_enqueue.go:33](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/adapters/verificationbridge/packing_enqueue.go#L33), [distribution_service.go:38](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/app/distribution_service.go#L38), [distribution enqueue.go:38](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/feeddirection/adapters/verificationbridge/enqueue.go#L38)

**Plain English:** The packing bridge has shed/partition fields, but the PostgreSQL result never fills them, so the bridge receives empty values and renders only `Session N`. Distribution has a separate omission: its enqueue request and bridge carry no readable shed/partition label at all. Operators therefore cannot reliably tell identically numbered feed sessions apart in the verifier queue.

**Fix:** Resolve the canonical operational-location display for both completion paths and pass it into every verifier item; add end-to-end tests that assert the final stored subject label.

### WEIGH-005 - Four growth/weight analytics calculations are wrong or silently truncated

**Where:** [growth.go:399](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/postgres/growth.go#L399), [growth.go:488](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/postgres/growth.go#L488), [growth.go:574](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/postgres/growth.go#L574), [weight_history.go:115](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/postgres/weight_history.go#L115)

**Plain English:** `300+` histogram values are folded into `275-300` while the displayed `300+` bucket stays empty; weekly averages weight each capture equally instead of each animal; losing-animal totals cap at 200 (GOS100-017); and 10,001+ history points truncate while `truncated=false`. The same-day ADG exclusion and 400-day predecessor bound are documented policies, so they are no longer claimed as defects.

**Fix:** Correct overflow bucket indexing, use animal-count-weighted weekly sums, separate losing-animal totals from paged rows, and use limit-plus-one history detection.

### WEIGH-006 - Leadership weighing gallery can perform 2,000 serial proof lookups

**Where:** [weighing service.go:838](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/app/service.go#L838), [handler.go:503](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/weighing/adapters/http/handler.go#L503)

**Plain English:** A full page can resolve 100 sheds times 20 observations one at a time. One missing proof aborts the whole page.

**Fix:** Batch proof metadata, bound signing concurrency and total evidence, and mark unavailable media per item.

### HERD-001 - Birth can name a dead, sold, or transferred mother

**Where:** [admin_goat_create.go:114](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/identity/adapters/postgres/admin_goat_create.go#L114)

**Plain English:** Mother lookup checks RFID/species/sex but not alive/unexited state. Newborns and mother-care tasks can be attached to an exited female.

**Fix:** Require alive and `exited_at IS NULL` under the birth transaction lock.

### HERD-002 - Health treatment loses the goat's partition

**Where:** [health repository.go:61](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/health/adapters/postgres/repository.go#L61), [HealthDtos.kt:8](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/dto/HealthDtos.kt#L8)

**Plain English:** Diagnosis does not snapshot the partition and Android omits the fields. In a split shed, a treatment card cannot tell the operator which pen contains the goat.

**Fix:** Snapshot and carry the server-composed operational location end to end.

### HERD-003 - Exact health-case replay depends on the goat's later state

**Where:** [health repository.go:61](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/health/adapters/postgres/repository.go#L61), [repository.go:80](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/health/adapters/postgres/repository.go#L80)

**Plain English:** The code validates current lifecycle/age before checking the existing idempotency key. A retry of a successful request can fail after the goat dies or changes age band instead of returning the original success.

**Fix:** Resolve exact replay before mutable-state validation while retaining fingerprint conflict checks.

### KERN-008 - Expired bulk-status workers can overwrite newer results

**Where:** [bulk repository.go:203](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/bulkstatus/adapters/postgres/repository.go#L203), [worker.go:182](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/bulkstatus/app/worker.go#L182)

**Plain English:** After lease expiry, worker B can finish a row and stale worker A can later change `applied` back to retry/skipped/error. Job counters and status become false.

**Fix:** Add a claim generation/token to every outcome update and check affected rows.

### KERN-009 - SOP task reads have unbounded N+1 queries

**Where:** [SOP repository.go:384](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/sop/adapters/postgres/repository.go#L384), [repository.go:1995](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/sop/adapters/postgres/repository.go#L1995)

**Plain English:** Each task read loads every historical submission, then performs one child query per submission. Reworked tasks grow until reads/retries time out or return huge payloads.

**Fix:** Use bounded set-based queries, paginate history, and look up replay targets directly.

### DATA-003 - Vaccine stock can count expired lots for 5.5 hours each day boundary

**Where:** [vaccination query.sql:386](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/vaccination/adapters/postgres/sqlc/query.sql#L386), [inventory query.sql:12](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/inventory/adapters/postgres/sqlc/query.sql#L12)

**Plain English:** `CURRENT_DATE` follows the database UTC session. Between midnight and 05:30 IST, yesterday's expiry date is still used, so expired vaccine can appear available.

**Fix:** Pass an explicit Asia/Kolkata business date into inventory/vaccination queries.

### OPS-002 - "Proof coverage" counts audit action names, not real evidence

**Where:** [operations audit repository.go:95](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/operationsaudit/adapters/postgres/repository.go#L95)

**Plain English:** An SOP/proof-named audit action counts as a proof event even with no attached proof; duplicates can drive coverage to 100% while required operations lack evidence.

**Fix:** Count distinct proof-required operations with confirmed linked proof.

## P2 - Web, Mobile, Contracts, Analytics

### WEB-004 - A mobile-only weighing module is still reachable on admin web

**Where:** [current-admin-web-scope.md:17](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/context/frontend/current-admin-web-scope.md#L17), [admin service.go:130](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/adminui/app/service.go#L130), [weights route](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/app/(admin)/weighing/weights/page.tsx#L1)

**Plain English:** The canonical scope says weighing is mobile-only, but users still receive navigation and a live web route. The removal test checks obsolete IDs and passes.

**Fix:** Remove the route/nav/page contract or change the canonical product decision and tests explicitly.

### WEB-005 - Operator availability can be false or truncated

**Where:** [vaccination-operators-scope.ts:93](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/people/vaccination-operators-scope.ts#L93), [roster_service.go:205](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/workforce/app/roster_service.go#L205)

**Plain English:** Leave API failure becomes "nobody is on leave"; frontend status rules disagree with the scheduler; and roster/leave lists silently cap at 500. Admin can see an unavailable operator as available and save a plan that differs from backend scheduling.

**Fix:** Fail availability closed, share effective status rules, and page/drain or expose a park/date-scoped availability endpoint.

### WEB-006 - Add Leave is not idempotent and allows double submit

**Where:** [vaccination-operators-screen.tsx:367](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/people/vaccination-operators-screen.tsx#L367), [idempotency.go:58](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/workforce/adapters/postgres/idempotency.go#L58)

**Plain English:** A double click or retry after a lost response sends no idempotency key and can create duplicate absences, audits, and events.

**Fix:** Create one stable intent key per modal and disable submit while pending.

### WEB-007 - Audit filters and counts describe only the current 25 rows

**Where:** [audit-log.tsx:35](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/operations-audit/audit-log.tsx#L35), [audit-log.tsx:508](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/operations-audit/audit-log.tsx#L508)

**Plain English:** Domain badges and actor options are derived from one page. Matching data on page two appears as zero or cannot be selected, and facets change while paging.

**Fix:** Return facets and counts over the complete filtered dataset from the backend.

### WEB-008 - Middleware redirects before refresh-token recovery can run

**Where:** [proxy.ts:25](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/proxy.ts#L25), [server-session.ts:18](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/lib/auth/server-session.ts#L18)

**Plain English:** After laptop sleep/ID-token expiry, middleware redirects to login even when a valid refresh cookie could recover the session. Some users must sign in again.

**Fix:** Allow refresh-cookie requests through and redirect only after refresh fails.

### WEB-009 - Investor mortality summary loads all death history into Node memory

**Where:** [mortality route.ts:46](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/app/api/mortality/route.ts#L46), [route.ts:174](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/app/api/mortality/route.ts#L174)

**Plain English:** Each uncached request runs twelve warehouse queries, downloads every death, and duplicates IDs in maps/sets. Cost, latency, and memory grow without a bound.

**Fix:** Aggregate in BigQuery with date bounds and cache the small projection.

### CEO-003 - Admin CEO AI conversation history stops after the first page

**Where:** [ceo-ai-client.ts:32](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/ceo-ai/ceo-ai-client.ts#L32), [ceo-ai-panel.tsx:230](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/admin-web/features/ceo-ai/ceo-ai-panel.tsx#L230)

**Plain English:** The API returns `next_cursor`, but the UI discards it. Older chats disappear and cannot be resumed, renamed, or deleted.

**Fix:** Retain and page the cursor.

### CEO-004 - Analytics rollup has no daily scheduler

**Where:** [analytics main.go:70](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/cmd/analytics-rollup/main.go#L70), [analytics_rollup.tf:4](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/infra/envs/stg/analytics_rollup.tf#L4)

**Plain English:** Code says Cloud Scheduler runs the job daily, but Terraform creates only a manually invoked job. Analytics and reports go stale unless a person runs it.

**Fix:** Add scheduler, invoker IAM, explicit timezone, and missed-run alert.

### CEO-005 - Contract advertises a CEO conversation-detail endpoint runtime does not have

**Where:** [conversations.go:102](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/internal/ceoai/adapters/http/conversations.go#L102), [app-api.yaml:7440](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/contracts/openapi/app-api.yaml#L7440)

**Plain English:** Generated clients are promised a detail GET, but runtime registers messages/PATCH/DELETE only. Calling the advertised method returns 404/405.

**Fix:** Implement the GET or remove it from contract and clients.

### MOB-004 - Android database migration tests stop at v32 while production is v35 (GOS100-007)

**Where:** [GoatDatabase.kt:262](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/GoatDatabase.kt#L262), [GoatDatabaseMigrationTest.kt:85](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/test/kotlin/sg/mesha/goatos/core/data/GoatDatabaseMigrationTest.kt#L85)

**Plain English:** The release guard itself fails and does not protect migrations 32 to 35. Current production registers them, but future upgrade crashes/data loss can pass the intended test.

**Fix:** Share one production migration list/version with tests and cover v32/v33/v34 to v35 upgrades.

### MOB-005 - Android runtime and OpenAPI have broad route drift

**Where:** [NetworkModule.kt:141](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/NetworkModule.kt#L141), [NetworkModule.kt:561](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-network/src/main/kotlin/sg/mesha/goatos/core/network/NetworkModule.kt#L561)

**Plain English:** Twelve Android runtime endpoints, including proof deletion, alerts/execution, rework/verify, weighing reopen, and vaccination close, are absent from canonical OpenAPI. Backend/client changes can break only at runtime.

**Fix:** Add/generate every endpoint and gate Retrofit-to-OpenAPI parity.

### MOB-006 - Operator-visible status/error copy bypasses localization

**Where:** [WorkflowDetailViewModel.kt:267](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/feature/feature-workflow/src/main/kotlin/sg/mesha/goatos/feature/workflow/WorkflowDetailViewModel.kt#L267), [MilkPreparationViewModel.kt:444](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/feature/feature-milk-preparation/src/main/kotlin/sg/mesha/goatos/feature/milkpreparation/MilkPreparationViewModel.kt#L444)

**Plain English:** Hindi, Kannada, and Telugu users receive English-only recovery instructions and internal words like "backend" during live work.

**Fix:** Emit typed string-resource events and scan ViewModels in localization guards.

### MOB-007 - Corrupt health cache becomes permanently failing or false-empty offline

**Where:** [HealthRepository.kt:74](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/HealthRepository.kt#L74)

**Plain English:** An old-schema/corrupt Room JSON row is retained. Paging repeatedly throws, while metadata/detail decode silently becomes null, so the screen stays broken or blank while offline.

**Fix:** Quarantine bad cache rows and expose an explicit stale/cache-error state.

## P2 - Build, Test, And Release Controls

### REL-007 - Required migration validator is permanently red and mislabels Up as Down

**Where:** [hot-table set:56](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/tests/integration/validate-hot-index-migrations.sh#L56), [section parser:177](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/tests/integration/validate-hot-index-migrations.sh#L177), [section label:444](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/backend/tests/integration/validate-hot-index-migrations.sh#L444), [PR gate](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/.github/workflows/stg-pr-gate.yml#L75)

**Plain English:** The required PR check exits `1` on committed migrations `000022`, `000031`, and `000107`. `shifting_events` is explicitly in the hot-table set, so the `000031` failures are real validator output. The parser removes the `goose Up` marker and later searches the stripped text for that marker, causing `000107`'s Up `ADD CONSTRAINT` violation to be reported as Down. A gate that fails on the repository before a developer changes anything, and gives the wrong section for part of that output, cannot reliably protect releases.

**Fix:** Baseline exact immutable historical violations and pass the parsed section explicitly.

### REL-008 - Release guard checks function text, not that verification is called (GOS100-014)

**Where:** [check-release-tag-contract.mjs:94](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/release/check-release-tag-contract.mjs#L94)

**Plain English:** `indexOf("verify_stg_images")` can match the function definition. Removing the real call can still pass the guard.

**Fix:** Parse or tightly match an invocation before tag creation, excluding definitions/comments.

### REL-009 - Release contract test depends on real date, tags, network, and environment

**Where:** [check-release-tag-contract.mjs:57](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/release/check-release-tag-contract.mjs#L57)

**Plain English:** The dry run computes a real date/SHA tag and inherits ambient variables. It fails merely because today's tag already exists, making full guardrails non-hermetic and date-dependent.

**Fix:** Use an isolated repository/remote, controlled environment, and deterministic synthetic tag.

### CI-003 - Fixture-only changes skip their validators

**Where:** [component-paths.json:59](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/ci/component-paths.json#L59), [ci-scope.mjs:174](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/ci/ci-scope.mjs#L174)

**Plain English:** A `fixtures/**`-only commit runs common checks, while HRMS/CPT materialization and schedule validators live elsewhere. Broken staging seed inputs can land green.

**Fix:** Route relevant fixtures to backend validation or run fixture validators in common.

### CI-004 - Certifying admin CI trusts any existing `node_modules`

**Where:** [run-local-ci.sh:508](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/tools/ci/run-local-ci.sh#L508)

**Plain English:** If `node_modules` exists, acceptance skips `npm ci` and does not enforce Node 24. A certifying run can use dependencies different from the lockfile/clean build.

**Fix:** Always run `npm ci` and enforce Node 24 for receipt-producing runs.

### CI-005 - `feature-auth` tests cannot compile

**Where:** [feature-auth build.gradle.kts:22](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/feature/feature-auth/build.gradle.kts#L22), [RoleBasedPermissionGateTest.kt:3](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/feature/feature-auth/src/test/kotlin/sg/mesha/goatos/feature/auth/RoleBasedPermissionGateTest.kt#L3)

**Plain English:** The module has JUnit/Robolectric/AndroidX/Kotlin tests but declares none of their test dependencies. Its test task fails compilation.

**Fix:** Add test dependencies and include the task in aggregate CI.

### CI-006 - Android behavior tests assert retired rules

**Where:** [AppPermissionTest.kt:18](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/core/core-permissions/src/test/kotlin/sg/mesha/goatos/core/permissions/AppPermissionTest.kt#L18), [DriveCardMetricsTest.kt:164](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/feature/feature-calendar/src/test/kotlin/sg/mesha/goatos/feature/calendar/DriveCardMetricsTest.kt#L164)

**Plain English:** Permission tests reject the intentional Android 10/11 location rule and calendar tests expect the deliberately removed submitted chip. These are stale tests, not product regressions, but they keep the full suite red.

**Fix:** Update assertions to current contracts and run them in CI.

### CI-007 - Declared production Android flavor is not reproducible from a clean checkout

**Where:** [app build.gradle.kts:190](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/goatos-android/app/build.gradle.kts#L190)

**Plain English:** Prod Google-services tasks require `app/src/prod/google-services.json`, but it is absent. A clean standard Gradle aggregate cannot build prod.

**Fix:** Commit the non-secret Firebase client config or disable prod variants until they exist.

### CI-008 - Operator-mobile does not typecheck or test cleanly

**Where:** [state.test.ts:9](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/operator-mobile/src/features/bootstrap/state.test.ts#L9), [package.json:7](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/operator-mobile/package.json#L7)

**Plain English:** The bootstrap fixture omits required `nav_chrome`, so typecheck fails. Test scripts also hard-code TypeScript inside `apps/admin-web/node_modules`, which a normal workspace install can hoist/remove.

**Fix:** Update the fixture and invoke the workspace-local TypeScript binary through the package manager.

### CI-009 - Investor shadow web does not build in the monorepo

**Where:** [root package.json:4](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/package.json#L4), [investor package.json:18](https://github.com/vgoats/goatos/blob/d98a8a6f4cd452aa56681c52f91c06054c089081/apps/investor-web-shadow/package.json#L18)

**Plain English:** A clean workspace build hits incompatible React 18/19 types in `ResponsiveContainer` and an incompatible ESLint rule. The runnable app has no working build gate.

**Fix:** Isolate/remove the legacy app or align its React, type, ESLint, and Next toolchain and require its build.

## Verification Results

Passed:

- Backend `go test ./...`.
- Go vulnerability scan: no reachable vulnerabilities.
- SQL hot-path query-plan validation.
- Admin-web install, typecheck, build, mock-fidelity, lint command, and test suite (279 tests in the broad reviewer run).
- 20,000-row kernel scale gate: completed after crash/resume, 20,000 outbox events, bounded concurrency, no lock explosion.
- API latency-policy evidence tests.

Failed or incomplete:

- `validate-migrations` and `validate-hot-index-migrations`: unsafe/current and historical migration violations.
- Full `make guardrails`: release-tag contract collides with real tag state.
- Full Android `./gradlew test --continue`: module tests/compilation, stale migration/behavior tests, and missing prod Google-services config. A forced isolated `CaptureRepositoryTest` rerun passed all 28 tests, so no capture-repository production bug is claimed.
- Operator-mobile typecheck/tests fail; lint passes.
- Investor-shadow build fails; production dependency audit reports high vulnerabilities.
- High-scale database E2E requiring a caller-supplied isolated database was not run. No physical Android device/emulator, FCM delivery, Bluetooth/RFID hardware, process-death, or long-duration mobile memory test was available.

## Previous 17 Mapping

Sixteen previous findings are retained or revised. One was removed after counter-review:

- `GOS100-001` -> `SHIFT-001` (revised to your last-safe-day temporary-shed rule) plus `VAX-004`.
- `GOS100-002` -> **removed as a false positive**. The write query orders by newest `closed_at`, and `TestReadyClosureCountsLatestVerdictPerProofAndExcludesSupersededRejections_RealPostgres` passes.
- `GOS100-003` -> `REL-002`.
- `GOS100-004` -> `WEB-002`.
- `GOS100-005` -> `VAX-002`.
- `GOS100-006` -> `VAX-009`.
- `GOS100-007` -> `MOB-004`.
- `GOS100-008` -> `FEED-004`.
- `GOS100-009` -> `KERN-002`.
- `GOS100-010` -> `FEED-007`.
- `GOS100-011` -> `KERN-003`.
- `GOS100-012` -> `REL-003`.
- `GOS100-013` -> `REL-004`.
- `GOS100-014` -> `REL-008`.
- `GOS100-015` -> `WEIGH-004`.
- `GOS100-016` -> `WEIGH-002`.
- `GOS100-017` -> `WEIGH-005`.

## Counter-Review Amendment

Claude's counter-review correctly removed `VAX-001` and correctly challenged two sub-claims inside `WEIGH-005`. It did not justify removing the other 13 findings in its "refuted" table:

- `AUTH-002`: capability roles and `callerParkScope` still come from separate grants; the service checks the independently selected park but never proves the same grant supplied the capability.
- `WEIGH-001`: a rejected weighing is stored as `rework`, while analytics excludes only literal `rejected`; this confirms rather than refutes the finding.
- `FEED-003`, `FEED-005`, `FEED-006`: packing/distribution use the weak completed-proof validator, Android draft keys omit date/park/partition, and Android still dispatches the removed instant-completion route with no queue migration.
- `PROC-002` through `PROC-005`: dispatch treats any non-empty proof reference as sufficient, the re-add upsert overwrites advanced state, vaccination-cancellation errors are discarded, and accepted intake performs per-goat event/audit/outbox writes inside the three-second transaction.
- `KERN-003` through `KERN-005`: unroutable verdicts return success, production omits the weighing apply acker, and each stale-reclaim cycle refunds the attempt consumed by a successful external send whose completion write failed.
- `WEB-002`: `contract.ok == false` makes `firstEnabledPublishedHref` return null and therefore redirects to `/vaccination`.

The counter-review also omitted final verdicts for `AUTH-001`, `AUTH-004`, `VAX-003`, `VAX-008`, and `WEIGH-002`; left `FEED-007` unverified; and left `REL-007` unresolved. Direct inspection confirms all six product findings, and executing `validate-hot-index-migrations.sh` confirms `REL-007` by exiting non-zero with the reported section-label/legacy violations.

### Round 3 Adjudication

Claude's Round 3 correctly withdrew its earlier refutations of `AUTH-002`, `WEB-002`, `WEIGH-001`, `WEIGH-002`, `FEED-003`, `FEED-005`, `FEED-006`, `PROC-002` through `PROC-005`, `KERN-004`, and `KERN-005`. Those findings remain unchanged. The surviving three disagreements resolve as follows:

- `KERN-003` remains a bug. Claude is right that only `ErrNotFound` is swallowed and other errors retry, but that is exactly the routing-defect case described by the finding. The handler returns the service result directly, so `nil` acknowledges the verdict without applying it or creating a durable exception.
- `REL-007` remains a bug. Running the committed required validator exits `1`; `shifting_events` is explicitly listed as hot; and `up_section()` strips the marker that line 444 later tries to use to identify Up, proving the mislabel path. The legitimate migration defect in `000107` is separately recorded as `REL-001`; it does not make a permanently failing and partly mislabeled gate healthy.
- `FEED-007` remains one finding and is broadened to both paths. Packing declares and forwards label fields, but `CompletePacking` returns them empty from PostgreSQL. Distribution omits readable location labels from its enqueue request entirely.

Therefore the adjudicated ledger remains **94 findings** (`1 P0`, `59 P1`, `34 P2`), not approximately 92.
