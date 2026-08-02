SQLC ?= $(shell command -v sqlc 2>/dev/null || if command -v go >/dev/null 2>&1; then gopath=$$(go env GOPATH 2>/dev/null); if [ -x "$$gopath/bin/sqlc" ]; then printf '%s/bin/sqlc' "$$gopath"; fi; fi)
GOATOS_LOCAL_TENANT_ID ?= 00000000-0000-4000-8000-000000000001
GOATOS_VACCINATION_SOURCE_DIR ?= $(REPO_ROOT)/fixtures/vaccination-hrms-source-full
GOATOS_SHED_MANAGER_MAPPING ?= $(GOATOS_VACCINATION_SOURCE_DIR)/shed-manager-mapping.jul11-vaccination.csv
GOATOS_DEV_DASHBOARD_ADMIN_EMAILS ?= abhishek@mesha.sg aryaman@mesha.sg manju@mesha.sg manohark@mesha.sg ravi@mesha.sg
GOATOS_STG_DASHBOARD_ADMIN_EMAILS ?= $(GOATOS_DEV_DASHBOARD_ADMIN_EMAILS)
REPO_ROOT ?= $(shell git rev-parse --show-toplevel 2>/dev/null || pwd)
AI_BACKEND ?= auto

.PHONY: seed-state-guard check guardrails git-identity-guard guardrail-registration-guard backend-foundations-guard test-execution-integrity-guard operator-cap-fail-closed-guard stg-operator-scope-guard cascade-event-wiring-guard frontend-foundations-guard domain-event-architecture-guard operational-read-model-contract-guard critical-animal-action-availability-guard leadership-assistant-coverage-guard assistant-route-closure-guard local-ci-evidence-guard kernel-worker-retirement-gate-guard stg-disposable-topology-guard stg-promotion-guard stg-promotion-guard-install e2e-integrity-guard aggregate-projection-guard vaccination-schedule-canonical-guard vaccination-shared-source-sync-guard calendar-endpoint-grain-guard goat-shed-scope-guard goat-shed-integrity-db-proof scale-certification-docs-guard scale-guard clinical-defer-guard ceo-ai-boundary-guard vaccination-drive-clubbing-guard vaccination-adult-drive-contract-guard vaccination-drive-clubbing-db-proof vaccination-shed-ack-guard vaccination-hrms-seed-fixture-guard vaccination-hrms-source-audit fcm-recipient-routing-guard sweeper-deployment-guard deployed-job-flags-guard secret-accessors-guard worker-stage-budgets-guard idempotency-writes-guard atomic-readmodel-sync-guard config-validate-guard ui-vaccine-labels-guard notification-specificity-guard review-lens-ledger-guard seed-migration-guard india-date-guard offline-first-guard local-single-db-guard local-gcp-kernel-parity-guard ci-local land-main land-main-self-test mobile-guard mobile-guard-audit android-row-action-scope-guard android-vaccination-submit-gate-guard android-navigation-stack-guard telemetry-guard telemetry-guard-audit admin-web-request-reads-guard admin-web-request-reads-guard-audit admin-web-prefetch-guard android-bounded-memory-guard android-bounded-memory-guard-audit android-compose-lists-guard android-compose-lists-guard-audit nav-composition-guard nav-composition-guard-audit mobile-contract-ownership-guard mobile-contract-ownership-guard-audit test api-client-generate api-client-check sqlc-generate sqlc-check validate-hot-index-migrations validate-migrations validate-sqlc-plans pre-google-readiness seed-calendar-vaccination-dev seed-dev-email-grants seed-stg-email-grants seed-stg-firebase-password-users seed-stg-9-person-login seed-stg-postflight verify-stg-9-person-login seed-closeout seed-closeout-dry-run seed-vaccination-source-full seed-checkout-staleness-gate seed-vaccination-cpt-operator-drive legacy-god-sheet-sync-dry-run legacy-god-sheet-sync-apply verify-google-dev-seed-fixtures api-latency-policy-test api-latency-gate high-scale-kernel-e2e-all high-scale-kernel-e2e-data high-scale-kernel-e2e-certification bulk-status-kernel-it scale-kernel-gate scale-kernel-gate-smoke admin-web-e2e-smoke docker-storage-report docker-cleanup-goatos-dry-run docker-cleanup-goatos-execute docker-storage-scripts-test db-mutation-guard-test local-stack-service-guard dev-local dev-local-kernel-up dev-local-kernel-status dev-local-kernel-logs dev-local-kernel-smoke dev-local-service-install dev-local-service-start dev-local-service-stop dev-local-service-restart dev-local-service-status dev-local-service-logs dev-local-service-uninstall setup-crg update-docs-graph kernel-worker-cutover-guard seed-feed-ration ceo-ai-eval ceo-ai-eval-selftest grant-assistant-public-read
.PHONY: ai-setup ai-doctor ai-rebuild ai-rebuild-code ai-rebuild-docs ai-rebuild-repowise ai-repowise-coverage docs-graph-open ai-telemetry ai-telemetry-ui
.PHONY: e2e-image-build e2e-parity e2e-smoke e2e-business-chain scale-cert
setup-crg: ai-setup

ai-setup:
	@echo "Installing local AI token-saving tools for this checkout..."
	@if ! command -v code-review-graph >/dev/null 2>&1; then \
		if command -v uv >/dev/null 2>&1; then uv tool install code-review-graph; \
		elif command -v pipx >/dev/null 2>&1; then pipx install code-review-graph; \
		else python3 -m pip install --user code-review-graph; fi; \
	fi
	code-review-graph install --repo "$(REPO_ROOT)" --no-instructions -y
	code-review-graph build --repo "$(REPO_ROOT)"
	@if ! command -v graphify >/dev/null 2>&1; then \
		if command -v uv >/dev/null 2>&1; then uv tool install graphifyy; \
		elif command -v pipx >/dev/null 2>&1; then pipx install graphifyy; \
		else python3 -m pip install --user graphifyy; fi; \
	fi
	@if ! command -v rtk >/dev/null 2>&1; then \
		if command -v brew >/dev/null 2>&1; then brew install rtk; \
		else echo "RTK is missing. Install from https://www.rtk-ai.app/ or use: brew install rtk"; fi; \
	fi
	bash tools/agent-hooks/repowise-setup.sh
	bash tools/agent-hooks/install-stg-push-guard.sh
	$(MAKE) ai-doctor
	@echo ""
	@echo "AI setup ready. CRG/Graphify/repowise outputs are local generated artifacts and stay gitignored."
	@echo "Build or refresh local graphs with: make ai-rebuild AI_BACKEND=$(AI_BACKEND)"
	@echo ""
	@echo "Two dashboards (both local, both free):"
	@echo "  repowise health/risk/graph : repowise serve   ->  http://localhost:3000"
	@echo "  Graphify docs graph        : make docs-graph-open  (build first: make ai-rebuild-docs)"
	@echo "Optional: populate the repowise Coverage tab with: make ai-repowise-coverage  (needs dev DB up)"

ai-doctor:
	bash tools/agent-hooks/ai-doctor.sh

ai-rebuild: ai-rebuild-code ai-rebuild-docs ai-rebuild-repowise

ai-rebuild-code:
	code-review-graph build --repo "$(REPO_ROOT)"

ai-rebuild-docs:
	@mkdir -p graphify-out
	AI_BACKEND="$(AI_BACKEND)" bash tools/agent-hooks/rebuild-docs-graph.sh

ai-rebuild-repowise:
	@if command -v repowise >/dev/null 2>&1 && [ -d "$(REPO_ROOT)/.repowise" ]; then \
		cd "$(REPO_ROOT)" && repowise update; \
	else \
		bash tools/agent-hooks/repowise-setup.sh; \
	fi

# Opt-in: populate the repowise dashboard Coverage tab (free, no LLM). Runs the
# backend test suite with coverage, then ingests it. DB-backed tests need the
# local dev DB up for full coverage; partial coverage still ingests.
ai-repowise-coverage:
	bash tools/agent-hooks/repowise-coverage.sh

# Open the Graphify docs graph (goatos TRDs/ADRs/phase docs — the docs dashboard
# that complements repowise). Regenerate it first with `make ai-rebuild-docs`.
docs-graph-open:
	@if [ -f "$(REPO_ROOT)/graphify-out/graph.html" ]; then \
		if command -v open >/dev/null 2>&1; then open "$(REPO_ROOT)/graphify-out/graph.html"; \
		elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$(REPO_ROOT)/graphify-out/graph.html"; \
		else echo "Open manually: $(REPO_ROOT)/graphify-out/graph.html"; fi; \
	else \
		echo "Graphify docs graph not built yet. Run: make ai-rebuild-docs"; \
	fi

update-docs-graph:
	$(MAKE) ai-rebuild-docs

ai-telemetry:
	python3 tools/ai/analyze-transcripts.py

# HTML telemetry report: saved-vs-missed $ model, per-agent totals,
# by-day adoption trend, top sessions. Output is gitignored; opens in browser.
ai-telemetry-ui:
	python3 tools/ai/analyze-transcripts.py --by-day --html "$(REPO_ROOT)/ai-telemetry.html"
	@if command -v open >/dev/null 2>&1; then open "$(REPO_ROOT)/ai-telemetry.html"; \
	elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$(REPO_ROOT)/ai-telemetry.html"; \
	else echo "Open: $(REPO_ROOT)/ai-telemetry.html"; fi

guardrails:
	$(MAKE) domain-event-envelope-enum-guard
	$(MAKE) design-system-guard
	$(MAKE) git-identity-guard
	$(MAKE) guardrail-registration-guard
	$(MAKE) local-stack-service-guard
	$(MAKE) backend-foundations-guard
	$(MAKE) test-execution-integrity-guard
	$(MAKE) operator-cap-fail-closed-guard
	$(MAKE) stg-operator-scope-guard
	$(MAKE) cascade-event-wiring-guard
	$(MAKE) frontend-foundations-guard
	$(MAKE) domain-event-architecture-guard
	$(MAKE) operational-read-model-contract-guard
	$(MAKE) critical-animal-action-availability-guard
	$(MAKE) leadership-assistant-coverage-guard
	$(MAKE) assistant-route-closure-guard
	$(MAKE) local-ci-evidence-guard
	$(MAKE) stg-promotion-guard
	bash tools/agent-hooks/check-boundaries.sh --self-test
	bash tools/agent-hooks/check-boundaries.sh
	node tools/agent-hooks/check-refresh-binding.mjs
	bash tools/agent-hooks/check-contract-drift.sh
	$(MAKE) aggregate-projection-guard
	$(MAKE) vaccination-adult-drive-contract-guard
	$(MAKE) vaccination-shed-ack-guard
	$(MAKE) vaccination-schedule-canonical-guard
	$(MAKE) vaccination-shared-source-sync-guard
	$(MAKE) fcm-recipient-routing-guard
	$(MAKE) calendar-endpoint-grain-guard
	$(MAKE) goat-shed-scope-guard
	$(MAKE) weighing-free-flow-guard
	$(MAKE) weighing-operator-scope-guard
	$(MAKE) weighing-one-operator-per-bucket-guard
	$(MAKE) weighing-kernel-phase2-guard
	$(MAKE) migration-duplicate-versions-guard
	$(MAKE) scale-certification-docs-guard
	bash tools/agent-hooks/check-e2e-kernel-integrity.sh
	$(MAKE) api-latency-policy-test
	$(MAKE) scale-guard
	$(MAKE) clinical-defer-guard
	$(MAKE) ceo-ai-boundary-guard
	$(MAKE) sweeper-deployment-guard
	$(MAKE) deployed-job-flags-guard
	$(MAKE) kernel-worker-cutover-guard
	$(MAKE) stg-disposable-topology-guard
	$(MAKE) secret-accessors-guard
	$(MAKE) worker-stage-budgets-guard
	$(MAKE) idempotency-writes-guard
	$(MAKE) nav-composition-guard
	$(MAKE) mobile-contract-ownership-guard
	$(MAKE) atomic-readmodel-sync-guard
	$(MAKE) config-validate-guard
	$(MAKE) ui-vaccine-labels-guard
	$(MAKE) notification-specificity-guard
	$(MAKE) no-mismatch-review-queue-guard
	$(MAKE) review-lens-ledger-guard
	$(MAKE) seed-migration-guard
	$(MAKE) vaccination-hrms-seed-fixture-guard
	$(MAKE) india-date-guard
	$(MAKE) offline-first-guard
	$(MAKE) local-single-db-guard
	$(MAKE) room-migration-guard
	$(MAKE) mobile-guard
	$(MAKE) android-row-action-scope-guard
	$(MAKE) android-vaccination-submit-gate-guard
	$(MAKE) android-compose-lists-guard
	$(MAKE) android-navigation-stack-guard
	$(MAKE) admin-web-request-reads-guard
	$(MAKE) admin-web-prefetch-guard
	$(MAKE) admin-web-local-overlay-guard
	$(MAKE) android-bounded-memory-guard
	$(MAKE) telemetry-guard
	$(MAKE) local-gcp-kernel-parity-guard

git-identity-guard:
	node tools/ci/check-git-identity.mjs --self-test
	node tools/ci/check-git-identity.mjs

backend-foundations-guard:
	node tools/agent-hooks/check-backend-foundations.mjs --self-test
	node tools/agent-hooks/check-backend-foundations.mjs

test-execution-integrity-guard:
	node tools/agent-hooks/check-test-execution-integrity.mjs --self-test
	node tools/agent-hooks/check-test-execution-integrity.mjs

operator-cap-fail-closed-guard:
	node tools/agent-hooks/check-operator-cap-fail-closed.mjs --self-test
	node tools/agent-hooks/check-operator-cap-fail-closed.mjs

stg-operator-scope-guard:
	node tools/agent-hooks/check-stg-operator-scope.mjs --self-test
	node tools/agent-hooks/check-stg-operator-scope.mjs

cascade-event-wiring-guard:
	node tools/agent-hooks/check-cascade-event-wiring.mjs --self-test
	node tools/agent-hooks/check-cascade-event-wiring.mjs

frontend-foundations-guard:
	node tools/agent-hooks/check-frontend-foundations.mjs --self-test
	node tools/agent-hooks/check-frontend-foundations.mjs

guardrail-registration-guard:
	node tools/ci/check-guardrail-registration.mjs --self-test
	node tools/ci/check-guardrail-registration.mjs

domain-event-architecture-guard:
	node tools/agent-hooks/check-domain-event-architecture.mjs --self-test
	node tools/agent-hooks/check-domain-event-architecture.mjs

operational-read-model-contract-guard:
	node tools/agent-hooks/check-operational-read-model-contract.mjs --self-test
	node tools/agent-hooks/check-operational-read-model-contract.mjs

critical-animal-action-availability-guard:
	node tools/agent-hooks/check-critical-animal-action-availability.mjs --self-test
	node tools/agent-hooks/check-critical-animal-action-availability.mjs

leadership-assistant-coverage-guard:
	node tools/agent-hooks/check-leadership-assistant-coverage.mjs --self-test
	node tools/agent-hooks/check-leadership-assistant-coverage.mjs

assistant-route-closure-guard:
	node tools/agent-hooks/check-assistant-route-closure.mjs --self-test
	node tools/agent-hooks/check-assistant-route-closure.mjs

local-ci-evidence-guard:
	node tools/ci/check-local-ci-evidence.mjs --self-test
	bash tools/ci/land-main.test.sh

kernel-worker-retirement-gate-guard:
	node tools/agent-hooks/check-kernel-worker-retirement-gate.mjs --self-test
	node tools/agent-hooks/check-kernel-worker-retirement-gate.mjs

stg-promotion-guard:
	node tools/ci/check-stg-promotion.mjs --self-test
	node tools/ci/check-stg-promotion.mjs --repository

stg-promotion-guard-install:
	bash tools/agent-hooks/install-stg-push-guard.sh

aggregate-projection-guard:
	node tools/agent-hooks/check-aggregate-projection-review.mjs --self-test
	node tools/agent-hooks/check-aggregate-projection-review.mjs

vaccination-schedule-canonical-guard:
	node tools/agent-hooks/check-vaccination-schedule-canonical.mjs --self-test
	node tools/agent-hooks/check-vaccination-schedule-canonical.mjs

vaccination-shared-source-sync-guard:
	node tools/agent-hooks/check-vaccination-shared-source-sync.mjs --self-test
	node tools/agent-hooks/check-vaccination-shared-source-sync.mjs

calendar-endpoint-grain-guard:
	node tools/agent-hooks/check-calendar-endpoint-grain.mjs --self-test
	node tools/agent-hooks/check-calendar-endpoint-grain.mjs

goat-shed-scope-guard:
	node tools/agent-hooks/check-goat-shed-scope.mjs --self-test
	node tools/agent-hooks/check-goat-shed-scope.mjs

weighing-free-flow-guard:
	node tools/agent-hooks/check-weighing-free-flow-guard.mjs --self-test
	node tools/agent-hooks/check-weighing-free-flow-guard.mjs

weighing-operator-scope-guard:
	node tools/agent-hooks/check-weighing-operator-scope-guard.mjs --self-test
	node tools/agent-hooks/check-weighing-operator-scope-guard.mjs

weighing-one-operator-per-bucket-guard:
	node tools/agent-hooks/check-weighing-one-operator-per-bucket-guard.mjs --self-test
	node tools/agent-hooks/check-weighing-one-operator-per-bucket-guard.mjs

.PHONY: weighing-kernel-phase2-guard
weighing-kernel-phase2-guard:
	node tools/agent-hooks/check-weighing-kernel-phase2-guard.mjs --self-test
	node tools/agent-hooks/check-weighing-kernel-phase2-guard.mjs

.PHONY: migration-duplicate-versions-guard
migration-duplicate-versions-guard:
	node tools/agent-hooks/check-migration-duplicate-versions.mjs --self-test
	node tools/agent-hooks/check-migration-duplicate-versions.mjs

goat-shed-integrity-db-proof:
	bash tools/dev/check-goat-shed-integrity.sh

scale-certification-docs-guard:
	node tools/agent-hooks/check-scale-certification-docs.mjs --self-test
	node tools/agent-hooks/check-scale-certification-docs.mjs

# seed-migration-guard: if a migration changes an initial-seed-owned setup table
# or app-visible projection table, the same patch must update the seed command,
# seed/projection tests, or seed runbook. This prevents schema/read-model drift
# where canonical seed rows exist but the live app reads empty/missing projection
# tables after migration. See docs/runbooks/initial-seed-migration-coupling.md.
seed-migration-guard:
	node tools/agent-hooks/check-seed-migration-coupling.mjs --self-test
	node tools/agent-hooks/check-seed-migration-coupling.mjs

vaccination-hrms-seed-fixture-guard:
	node tools/agent-hooks/check-vaccination-hrms-seed-fixture.mjs --self-test
	node tools/agent-hooks/check-vaccination-hrms-seed-fixture.mjs

fcm-recipient-routing-guard:
	node tools/agent-hooks/check-fcm-recipient-routing.mjs --self-test
	node tools/agent-hooks/check-fcm-recipient-routing.mjs

# Validate any normalized vaccination + HRMS source bundle without touching a DB.
# Override SOURCE=/absolute/path and AS_OF=YYYY-MM-DD for a newly supplied sheet export.
vaccination-hrms-source-audit:
	node tools/dev/validate-vaccination-hrms-source.mjs --source "$${SOURCE:-$(GOATOS_VACCINATION_SOURCE_DIR)}" --as-of "$${AS_OF:-2026-07-20}" --strict

# telemetry-guard: block the TELEMETRY GUARDRAIL anti-pattern — a new/changed
# Android screen/viewmodel (or admin-web route) shipped with no Firebase
# Analytics event, no Crashlytics fatal/non-fatal wiring on failure paths, and
# no funnel/journey step. Diff-scoped vs origin/main; `telemetry-guard-audit`
# scans the whole tree. Escape hatch: `// telemetry:exempt <reason>`. See
# docs/observability/TELEMETRY_GUARDRAILS.md.
telemetry-guard:
	python3 -m unittest tools/telemetry-guard/test_telemetry_guard.py
	python3 tools/telemetry-guard/telemetry-guard.py

telemetry-guard-audit:
	python3 tools/telemetry-guard/telemetry-guard.py --all

local-gcp-kernel-parity-guard:
	bash tools/agent-hooks/check-local-gcp-kernel-parity.test.sh
	bash tools/agent-hooks/check-local-gcp-kernel-parity.sh

# clinical-defer-guard: block the C35-010 medical-safety anti-pattern — a PARTIAL
# clinical defer_states list in production code/seeds. sick/under_treatment/
# quarantine/icu are mandatory safety blocks; a non-empty list omitting any of them
# would let a sick animal's open work be cancelled instead of deferred. Runs its
# adversarial self-test first, then scans the whole production tree. See
# docs/preventive-care-vaccination/vaccination-rules.md.
clinical-defer-guard:
	node tools/agent-hooks/check-clinical-defer-states.mjs --self-test
	node tools/agent-hooks/check-clinical-defer-states.mjs

# ceo-ai-boundary-guard: keep the leadership-assistant reporting/assistant
# namespace (ceo_ai SQL schema + ceo_ai_* tables) OUT of core operator layers.
# The assistant reads core data (read APIs / MCP / read-only SQL); core BE read
# paths must never join ceo_ai.* for their own runtime data. Blocks the exact
# regression that 500'd Control Tower when the ceo_ai schema was absent. Runs
# its adversarial self-test first, then scans backend/internal. See
# docs/decisions/ceo-ai-reporting-boundary.md.
ceo-ai-boundary-guard:
	node tools/agent-hooks/check-ceo-ai-core-boundary.mjs --self-test
	node tools/agent-hooks/check-ceo-ai-core-boundary.mjs

vaccination-drive-clubbing-guard:
	cd backend && go test ./internal/obligation/app -run 'Test(DrivePlanner|BatchSession|Normalized|Pick|Park|Combo|SweepVersionWalksEverySafeOverflowDateWhenShotCapFull|ParkMergeStepWalksEverySafeOverflowDateWhenShotCapFull)' -count=1 -timeout=60s
	cd backend && go test ./internal/obligation/adapters/postgres -run 'Test(SM4Sweeper(ClubsNearbyDueDatesWithinSafeWindow|RecordsOneTimeBatchingHoldMetadata|DoesNotBackdateOverdueHoldCap|EnforcesTwoShotsPerAnimalPerDrive)|CreateBatchWithObligationsRecordsHoldOnlyForAttachedRows)' -count=1
	cd backend && go test ./internal/calendar/adapters/postgres -run 'TestCalendar(ParkDriveTargetsIncludeParkScopedBatchMembers|DefaultListKeepsPlannedDriveWhenSameDayCatchupDeferred)' -count=1
	cd backend && go test ./tests/e2e -run TestKernelStoryAK_DriveClubbingWithinBuffer -count=1 -timeout=5m

vaccination-adult-drive-contract-guard:
	node tools/agent-hooks/check-vaccination-adult-drive-contract.mjs --self-test
	node tools/agent-hooks/check-vaccination-adult-drive-contract.mjs
	cd backend && go test ./internal/vaccination/app -run 'Test(GenerateForVersionAutomaticallySchedulesAdultBlankHistoryCampaignByShed|GenerateForVersionClubsAdultBlankHistoryWithSameVaccineRepeatDate|GenerateForVersionRealignsExistingStableBlankHistoryWhenRepeatHistoryArrivesLater|ScheduleNextDoseUsesEachOperatorSubmissionDateAfterDelayedVerification)' -count=1
	cd backend && go test ./internal/obligation/app -run 'TestLimitParkSelection(PacksWholePhysicalShedsBeforeFillingCap|FallsBackToWholePartitionsWhenShedExceedsRemainingCapacity)' -count=1
	cd backend && go test ./internal/vaccinationexecution/app -run 'TestOperatorDrivePlanner(CPTAdultsOneOperatorKeepsWholeShedsAcrossTwoDays|CarriesWholePhysicalShedPastResidualCapacity)' -count=1
	cd backend && go test ./internal/calendar/adapters/postgres -run 'TestDriveSummaryEmitsSharedLogicalDriveNameAndTotal' -count=1

vaccination-drive-clubbing-db-proof:
	bash tools/dev/check-vaccination-drive-clubbing-proof.sh

vaccination-shed-ack-guard:
	node tools/agent-hooks/check-no-vaccination-shed-form-fields.mjs

sweeper-deployment-guard:
	node tools/agent-hooks/check-sweeper-deployment.mjs --self-test
	node tools/agent-hooks/check-sweeper-deployment.mjs

# deployed-job-flags-guard: KERN-001 — deploy/runtime/workers.json is the single
# authoritative manifest of every backend runtime process. This checks every Cloud Run Job
# command+args declared in infra/envs/*/cloud_run_jobs.tf MATCHES its manifest entry, every
# manifest arg is a flag actually defined by that binary's flag parser
# (backend/cmd/<name>/*.go, non-test), and every manifest binary is actually built in a
# backend/Dockerfile* image. Catches a job referencing a deleted binary (container fails to
# start), a job passing a retired flag ("flag provided but not defined", immediate exit 1),
# and the manifest itself drifting from either side — none of which is caught by
# `go build ./...` because Terraform args are plain strings. See deploy/runtime/workers.json
# for the full consumer list (this guard is static/offline; `make e2e-parity` proves the
# same manifest against the real built image).
deployed-job-flags-guard:
	node tools/agent-hooks/check-deployed-job-flags.mjs --self-test
	node tools/agent-hooks/check-deployed-job-flags.mjs

# kernel-worker-cutover-guard (KERN-01): enforces atomic mutual exclusion of legacy jobs
# and kernel-worker stages. Verifies that:
#   (a) legacy_stage_jobs is gated by var.retire_legacy_stage_jobs in cloud_run_jobs.tf;
#   (b) GOATOS_WORKER_STAGES_ENABLED env var in cloud_run_worker.tf is bound to the same flag;
#   (c) the two are mutually exclusive: flag false -> legacy active + stages disabled,
#       flag true -> legacy removed + stages enabled.
# Prevents both-active (double-processing) and both-inactive (zero-owner) defects.
kernel-worker-cutover-guard:
	node tools/agent-hooks/check-kernel-worker-cutover.mjs --self-test
	node tools/agent-hooks/check-kernel-worker-cutover.mjs

stg-disposable-topology-guard:
	node tools/agent-hooks/check-stg-disposable-topology.mjs --self-test
	node tools/agent-hooks/check-stg-disposable-topology.mjs

# secret-accessors-guard (KERN-REV-04): terraform validate cannot detect a
# google_service_account.runtime["X"] index or a secret_containers accessor
# naming a retired service account (only `terraform plan` catches the missing map
# key, and plan needs the GCS backend). This static guard asserts every accessor,
# database_clients entry, and literal runtime[...] reference resolves to a
# runtime_service_accounts key in both envs.
secret-accessors-guard:
	node tools/agent-hooks/check-secret-accessors.mjs --self-test
	node tools/agent-hooks/check-secret-accessors.mjs

# worker-stage-budgets-guard (KERN-REV-05 / 05B): the consolidated kernel worker
# must not silently shrink the retired jobs' processing budgets. Asserts each
# env's cloud_run_worker.tf sets GOATOS_OUTBOX_LIMIT>=500 + GOATOS_NOTIFICATION_LIMIT>=100
# AND that main.go keeps the outbox relay on a cadence whose effective per-run
# budget is >= 54s and on a SEPARATE cadence from notification dispatch.
worker-stage-budgets-guard:
	node tools/agent-hooks/check-worker-stage-budgets.mjs --self-test
	node tools/agent-hooks/check-worker-stage-budgets.mjs

# --- Docker-image E2E foundation (KERN-001 follow-up) -----------------------------------
# e2e-image-build / e2e-parity / e2e-smoke / scale-cert: see deploy/runtime/workers.json
# (manifest), deploy/e2e/docker-compose.e2e.yml (compose topology), and
# docs/decisions/operational-kernel-5k-50k-scale-envelope.md (target topology). Phase 1
# proves the REAL built image runs the REAL deployed commands; Phase 2 layers the
# business-chain + resilience + scale assertions on top (see the compose file's seam
# comment).

# e2e-image-build: builds the real backend image the api + kernel-worker E2E services run
# from — no `go run`, no mounted script. Stamped with GIT_SHA the same way
# infra CI builds it (internal/platform/buildinfo.SHA / GET /version).
e2e-image-build:
	docker build --platform linux/amd64 --build-arg GIT_SHA="$$(git rev-parse HEAD)" \
		-f backend/Dockerfile -t goatos-backend:e2e .

# e2e-parity: proves deploy/runtime/workers.json is true of the goatos-backend:e2e image
# just built — every manifest binary exists in the image and every manifest arg is accepted
# by that binary's real flag parser INSIDE the image, plus a compose/manifest kernel-worker
# drift check. This is the check `go build ./...` and deployed-job-flags-guard cannot do:
# neither runs the actual built container.
e2e-parity:
	node tools/e2e/check-image-parity.mjs --self-test
	node tools/e2e/check-image-parity.mjs --image goatos-backend:e2e

# e2e-smoke: brings up deploy/e2e/docker-compose.e2e.yml (real Postgres, real Pub/Sub
# emulator, the real image), asserts the API becomes ready, kernel-worker stays running
# with ZERO restarts and starts every cadence cleanly, and no fatal log signature appears —
# then tears the whole stack + its volumes down, even on failure. Skips loudly (not a
# silent pass) if docker is unavailable; see tools/dev/e2e-smoke.sh's header.
e2e-smoke:
	bash tools/dev/e2e-smoke.sh

# e2e-business-chain: Phase 2a. Brings up its OWN ephemeral deploy/e2e/docker-compose.e2e.yml
# stack (real goatos-backend:e2e image — build it first with `make e2e-image-build`; a stale
# image is refused at startup by the migrationguard, exactly as in production) and drives the
# REAL operational kernel chain end-to-end, asserting each hop from the resulting canonical
# rows / real HTTP responses — NOTHING downstream of the ingress is hand-seeded:
#   input goat + authored protocol config -> goat.created via the real identity outbox path ->
#   Pub/Sub emulator publish -> domain-event consumer -> obligation generation -> operational
#   sweep/batch -> canonical GET /calendar/vaccination/events.
# Plus resilience: duplicate Pub/Sub delivery (idempotency, P0), kernel-worker restart catch-up,
# and two-worker advisory-lock concurrency. Skips loudly (exit 0, NOT a silent pass) when docker
# is unavailable — same posture as e2e-smoke. Tears its stack + volumes down on any exit.
e2e-business-chain:
	bash tools/e2e/business-chain-driver.sh

# idempotency-writes-guard: block the insufficient idempotency pattern where
# `ON CONFLICT DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key` is the
# only conflict action. AGENTS.md mandates key + request fingerprint persisted in
# the same txn as side effects, exact-replay returning the original result, and
# same-key/different-payload rejection. Baseline-ratcheted; new offenders fail.
idempotency-writes-guard:
	node tools/agent-hooks/check-idempotency-writes.mjs --self-test
	node tools/agent-hooks/check-idempotency-writes.mjs

# atomic-readmodel-sync-guard: a state transition and the sync of a derived read
# model it OWNS must be ONE atomic txn; a record must never publish/commit while
# its owned read-model upsert failed. Flags Publish*/Finalize* methods that upsert
# a derived read model but lack a rollback regression test. Baseline-ratcheted.
atomic-readmodel-sync-guard:
	node tools/agent-hooks/check-atomic-readmodel-sync.mjs --self-test
	node tools/agent-hooks/check-atomic-readmodel-sync.mjs

# config-validate-guard: authored config/business values are validate-or-reject,
# never silently-default. A field PRESENT but out of range must FAIL the save with
# a clear error, not be clamped/rewritten to a default the author never entered.
# Defaults apply ONLY to genuinely-absent fields. Baseline-ratcheted.
config-validate-guard:
	node tools/agent-hooks/check-config-validate-or-reject.mjs --self-test
	node tools/agent-hooks/check-config-validate-or-reject.mjs

ui-vaccine-labels-guard:
	node tools/agent-hooks/check-ui-vaccine-labels.mjs --self-test
	node tools/agent-hooks/check-ui-vaccine-labels.mjs

# notification-specificity-guard: maintainer decision 2026-08-02. Every user-facing notification
# must be MEANINGFUL (park, shed/partition, human vaccine/work-item name, count, farm-readable
# IST due date), never an abstract count-only sentence like "Vaccination(s) due soon · 3".
# Composes with (does not duplicate) ui-vaccine-labels-guard. See
# docs/decisions/2026-08-02-meaningful-notification-copy.md.
notification-specificity-guard:
	node tools/agent-hooks/check-notification-specificity.mjs --self-test
	node tools/agent-hooks/check-notification-specificity.mjs

# review-lens-ledger-guard: the review-lens closed-decisions ledger
# (.agents/skills/goatos-code-review/references/review-lens-ledger.md) is the
# always-loaded record of what was already fixed/banned/locked, so agents don't
# reopen closed decisions or rebuild banned patterns. This fails a push if a
# closed-decision block is malformed; warns (non-fatal) on a stale guard citation.
review-lens-ledger-guard:
	node tools/ci/check-review-lens-ledger.mjs --self-test
	node tools/ci/check-review-lens-ledger.mjs

# india-date-guard: Goat OS time semantics are India-business-calendar. UTC must
# never define a business day — day/date buckets, due/missed, reminder keys,
# reporting groups, and labels must convert to Asia/Kolkata first (biztime helper).
# Flags UTC day-truncation/formatting on business paths. Baseline-ratcheted.
india-date-guard:
	node tools/agent-hooks/check-india-business-date.mjs --self-test
	node tools/agent-hooks/check-india-business-date.mjs

# offline-first-guard: every Android READ screen is offline-first with Room as SSOT.
# A network-only read repository (thin api.xxx() pass-through with no Room persist +
# Flow observe) is BANNED for screen-facing reads. Flags such repos in
# apps/goatos-android. See docs/decisions/android-offline-first.md. Baseline-ratcheted.
offline-first-guard:
	node tools/agent-hooks/check-offline-first-reads.mjs --self-test
	node tools/agent-hooks/check-offline-first-reads.mjs

local-single-db-guard:
	bash tools/agent-hooks/check-local-single-db.sh

# ci-local: run the SAME affected-component CI gates as .github/workflows/ci.yml.
# Per AGENTS.md a GitHub Actions billing/platform failure is NEVER a closure
# blocker — a green `make ci-local` on the pushed SHA is the authoritative gate.
# Default auto-scopes against origin/main. MODE=all forces the full suite.
# JOB=common|backend|guardrails|admin-web|android is partial and writes no receipt.
ci-local:
	bash tools/ci/run-local-ci.sh $(if $(JOB),$(JOB),$(MODE))

# land-main is the Codex/Claude landing entry point. It refuses dirty worktrees,
# rebases onto fresh origin/main before CI, reruns CI if main moves, and pushes
# only the exact certified SHA through the Mesha credential path.
land-main:
	bash tools/ci/land-main.sh

land-main-self-test:
	bash tools/ci/land-main.test.sh

e2e-integrity-guard:
	bash tools/agent-hooks/check-e2e-kernel-integrity.sh

# scale-guard: static block on million-animal scale anti-patterns
# (compute-on-read god-CTEs, N+1 loops, OFFSET pagination, full-MV-refresh,
# non-sargable LIKE). Zero deps (stdlib go run). Blocks NEW offenders; existing
# debt is tracked in tools/scale-guard/baseline.txt. See
# docs/decisions/scale-anti-patterns.md.
scale-guard:
	cd tools/scale-guard && go run . -root "$(CURDIR)"

# mobile-guard: block mobile/web list-fetch anti-patterns (fetch > ~20 rows/screen,
# calendar overview parsing events instead of day-markers, O(n^2) date scans). Diff-scoped:
# a commit with no mobile Kotlin passes instantly. See
# docs/decisions/mobile-data-fetch-anti-patterns.md. `mobile-guard-audit` scans the whole tree.
.PHONY: domain-event-envelope-enum-guard
domain-event-envelope-enum-guard: ## Fail if a module emits a domain event absent from the envelope enum (the relay would drop it as invalid_event_envelope)
	node tools/agent-hooks/check-domain-event-envelope-enum.mjs --self-test
	node tools/agent-hooks/check-domain-event-envelope-enum.mjs

.PHONY: design-system-guard
design-system-guard: ## Fail if a screen invents its own colour or text style instead of using the central design system
	node tools/agent-hooks/check-design-system-tokens.mjs

mobile-guard:
	bash tools/android/check-no-hardcoded-design.sh
	node tools/agent-hooks/check-android-ui-foundations.mjs --self-test
	node tools/agent-hooks/check-android-ui-foundations.mjs
	node tools/agent-hooks/check-android-ui-copy-layout.mjs --self-test
	node tools/agent-hooks/check-android-ui-copy-layout.mjs
	node tools/agent-hooks/check-android-orientation-lock.mjs --self-test
	node tools/agent-hooks/check-android-orientation-lock.mjs
	node tools/agent-hooks/check-android-camera-only-proof-capture.mjs --self-test
	node tools/agent-hooks/check-android-camera-only-proof-capture.mjs
	node tools/agent-hooks/check-android-row-action-scope.mjs --self-test
	node tools/agent-hooks/check-android-row-action-scope.mjs
	node tools/agent-hooks/check-android-alerts-gate-composed.mjs --self-test
	node tools/agent-hooks/check-android-alerts-gate-composed.mjs
	node tools/agent-hooks/check-mobile-list-fetch.mjs --self-test
	node tools/agent-hooks/check-mobile-list-fetch.mjs

mobile-guard-audit:
	bash tools/android/check-no-hardcoded-design.sh
	node tools/agent-hooks/check-android-ui-foundations.mjs
	node tools/agent-hooks/check-android-ui-copy-layout.mjs
	node tools/agent-hooks/check-android-orientation-lock.mjs
	node tools/agent-hooks/check-android-camera-only-proof-capture.mjs --all
	node tools/agent-hooks/check-android-row-action-scope.mjs
	node tools/agent-hooks/check-android-alerts-gate-composed.mjs
	node tools/agent-hooks/check-mobile-list-fetch.mjs --all

# android-row-action-scope-guard: row actions in repeated Android cards must use
# row/animal-scoped in-flight state, not a screen-wide busy flag. This blocks the
# individual weighing free-flow regression where a previous animal's slow save
# disabled/ignored the next animal's Save.
android-row-action-scope-guard:
	node tools/agent-hooks/check-android-row-action-scope.mjs --self-test
	node tools/agent-hooks/check-android-row-action-scope.mjs

# android-vaccination-submit-gate-guard: proof-rescan is an accepted reader hit and must
# repair the durable scan capture before proof capture, otherwise Submit can show
# proof-ready > scanned and stay disabled.
android-vaccination-submit-gate-guard:
	node tools/agent-hooks/check-android-vaccination-submit-gate.mjs --self-test
	node tools/agent-hooks/check-android-vaccination-submit-gate.mjs

# android-navigation-stack-guard: root chrome belongs to exact backend-composed
# L0 destinations only. Calendar and every other structural drill must use a
# distinct hosted child route with Up/Back and no bottom bar/drawer.
android-navigation-stack-guard:
	node tools/agent-hooks/check-android-navigation-stack.mjs --self-test
	node tools/agent-hooks/check-android-navigation-stack.mjs

# nav-composition-guard: block hardcoded per-role/per-module nav templates. Navigation (nav bar,
# bottom-bar icons/labels, screens) must be COMPOSED from the person's granted modules and reused
# across modules, not a fixed literal list naming a vertical. Diff-scoped. See
# docs/decisions/role-module-nav-composition.md.
nav-composition-guard:
	node tools/agent-hooks/check-nav-composition.mjs --self-test
	node tools/agent-hooks/check-nav-composition.mjs

nav-composition-guard-audit:
	node tools/agent-hooks/check-nav-composition.mjs --all

# no-mismatch-review-queue-guard: ban runtime review/repair queues for impossible-state data
# contradictions. A state impossible with clean ingestion (stage='kid' but age>=182) must be
# rejected at the INGESTION boundary, never persisted and then "repaired" by a runtime queue.
# See docs/decisions/ingestion-validation-not-runtime-review.md.
no-mismatch-review-queue-guard:
	node tools/agent-hooks/check-no-mismatch-review-queue.mjs --self-test
	node tools/agent-hooks/check-no-mismatch-review-queue.mjs

# mobile-contract-ownership-guard: backend contract owns what the mobile user sees. Clean slice:
# mobile UI must not gate visibility by role (`role ==`). See AGENTS.md golden frontend rule.
mobile-contract-ownership-guard:
	node tools/agent-hooks/check-mobile-contract-ownership.mjs --self-test
	node tools/agent-hooks/check-mobile-contract-ownership.mjs

mobile-contract-ownership-guard-audit:
	node tools/agent-hooks/check-mobile-contract-ownership.mjs --all

# admin-web-request-reads-guard: block the Next.js SSR full-table request-read anti-pattern in
# apps/admin-web (the searchAllGoats full-herd walk, commit 810bc1b3) — a server data helper that
# drains a paginated endpoint cursor-by-cursor into one array to compute a KPI. Read a
# projection/summary endpoint instead. Diff-scoped: a commit with no admin-web TS passes instantly.
# `admin-web-request-reads-guard-audit` scans the whole tree. See docs/decisions/scale-anti-patterns.md.
admin-web-request-reads-guard:
	node tools/agent-hooks/check-admin-web-request-reads.mjs --self-test
	node tools/agent-hooks/check-admin-web-request-reads.mjs

admin-web-request-reads-guard-audit:
	node tools/agent-hooks/check-admin-web-request-reads.mjs --all

# admin-web-prefetch-guard: block Next.js route prefetch in admin-web. Route
# prefetch is cheap for static pages but dangerous for authenticated operational
# pages because hovering/seeing links can silently trigger expensive SSR/API
# reads before a user clicks. Admin-web links must use the no-prefetch wrapper.
admin-web-prefetch-guard:
	node tools/agent-hooks/check-admin-web-prefetch.mjs --self-test
	node tools/agent-hooks/check-admin-web-prefetch.mjs

# admin-web-local-overlay-guard: prevent same-page drawers from navigating through
# Next Server Components. The whole feature tree has a zero-tolerance baseline:
# route-driven open, close, veil, and schedule-drawer controls are forbidden.
.PHONY: admin-web-local-overlay-guard
admin-web-local-overlay-guard:
	node tools/agent-hooks/check-admin-web-local-overlays.mjs --self-test
	node tools/agent-hooks/check-admin-web-local-overlays.mjs

# android-bounded-memory-guard: block unbounded in-memory growth in the Android data layer
# (an in-heap cache/accumulator with no cap/TTL/eviction, or a DAO reading a whole table into
# memory — commits 7058fff2 + d58acac2). Distinct from mobile-guard (which owns fetch/page SIZE).
# Diff-scoped: a commit with no android Kotlin passes instantly.
# `android-bounded-memory-guard-audit` scans the whole tree. See docs/decisions/mobile-data-fetch-anti-patterns.md.
android-bounded-memory-guard:
	node tools/agent-hooks/check-android-bounded-memory.mjs --self-test
	node tools/agent-hooks/check-android-bounded-memory.mjs

android-bounded-memory-guard-audit:
	node tools/agent-hooks/check-android-bounded-memory.mjs --all

# android-compose-lists-guard: block the Compose lazy-list key crash class — a
# LazyColumn/LazyRow keyed by a per-ENTITY id on a per-ROW list (a goat with two
# due vaccines => duplicate key => "Key was already used" crash, shipped in
# 0.1.6-stg, fixed a9c35a1d), and items()/itemsIndexed() with no stable key.
# Diff-scoped; `android-compose-lists-guard-audit` scans the whole tree.
# See docs/decisions/mobile-data-fetch-anti-patterns.md + docs/mobile/android-ui-quality.md.
android-compose-lists-guard:
	node tools/agent-hooks/check-android-compose-lists.mjs --self-test
	node tools/agent-hooks/check-android-compose-lists.mjs

android-compose-lists-guard-audit:
	node tools/agent-hooks/check-android-compose-lists.mjs --all

# room-migration-guard: block the Room-migration crash anti-pattern — an @Entity added to an
# Android @Database with no migration to CREATE its table (the roster_timetable_cache /
# roster_coverage_cache defect). Fresh installs work (Room's createAllTables); every in-place
# upgrade of an already-installed APK crashes on open. Also enforces exportSchema=true + a committed
# golden schema JSON per version, and that a version bump ships its Migration. Diff-scoped: a commit
# touching no Room DB/migration/schema passes instantly. Runs its self-test first. See
# docs/decisions/room-migration-safety.md.
room-migration-guard:
	node tools/agent-hooks/check-room-migration-safety.mjs --self-test
	node tools/agent-hooks/check-room-migration-safety.mjs

room-migration-guard-audit:
	node tools/agent-hooks/check-room-migration-safety.mjs --all

test:
	@if [ -f backend/go.mod ]; then cd backend && go test ./...; fi

api-client-generate:
	@if [ ! -d packages/api-client/node_modules ]; then npm --prefix packages/api-client ci --no-audit --no-fund; fi
	npm --prefix packages/api-client run generate

api-client-check: api-client-generate
	git diff --exit-code -- packages/api-client/src/generated

sqlc-generate:
	bash tools/sqlc/dump-schema.sh
	SQLC="$(SQLC)" bash tools/sqlc/check-version.sh
	cd backend && "$(SQLC)" generate -f sqlc.yaml

sqlc-check: sqlc-generate
	git diff --exit-code -- backend/sqlc.yaml backend/internal/identity/adapters/postgres/sqlc backend/internal/protocol/adapters/postgres/sqlc backend/internal/obligation/adapters/postgres/sqlc backend/internal/inventory/adapters/postgres/sqlc backend/internal/vaccination/adapters/postgres/sqlc backend/internal/feed/adapters/postgres/sqlc

check: guardrails docker-storage-scripts-test db-mutation-guard-test local-stack-service-guard
	$(MAKE) test

dev-local:
	bash tools/dev/run-local-stack.sh

dev-local-kernel-up:
	docker compose -f compose.local-kernel.yml up -d --build api outbox-relay domain-event-consumer kernel-workers kernel-maintenance

dev-local-kernel-status:
	docker compose -f compose.local-kernel.yml ps -a

dev-local-kernel-logs:
	docker compose -f compose.local-kernel.yml logs --tail=120 api outbox-relay domain-event-consumer kernel-workers kernel-maintenance

dev-local-kernel-smoke:
	bash tools/dev/local-gcp-kernel-parity-smoke.sh

dev-local-service-install:
	bash tools/dev/local-stack-service.sh install

dev-local-service-start:
	bash tools/dev/local-stack-service.sh start

dev-local-service-stop:
	bash tools/dev/local-stack-service.sh stop

dev-local-service-restart:
	bash tools/dev/local-stack-service.sh restart

dev-local-service-status:
	bash tools/dev/local-stack-service.sh status

dev-local-service-logs:
	bash tools/dev/local-stack-service.sh logs

dev-local-service-uninstall:
	bash tools/dev/local-stack-service.sh uninstall

validate-hot-index-migrations:
	bash backend/tests/integration/validate-hot-index-migrations.sh

validate-migrations:
	bash backend/tests/integration/validate-postgres-migrations.sh

validate-sqlc-plans:
	bash backend/tests/integration/validate-sqlc-query-plans.sh

pre-google-readiness:
	bash tools/dev/pre-google-readiness.sh

# Loads the authored feed ration configuration (migration 000003) from the embedded source
# ration grid: breed->ration-group map, shed-tag vocabulary, feed-item catalog, the park-scoped
# grams-per-head grid for CBE + CPT, and the default 2-session 50/50 split. Idempotent.
seed-feed-ration:
	cd backend && go run ./cmd/seed-feed-ration -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}"

seed-dev-email-grants:
	cd backend && go run ./cmd/seed-dev-email-grants -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -role ceo_internal -source goatos_dev_dashboard_admins $(foreach email,$(GOATOS_DEV_DASHBOARD_ADMIN_EMAILS),-email $(email))

# Sets GOATOS_ENV=stg so the seed routes to the staging Cloud SQL validator (a clear
# "needs GOATOS_ALLOW_STG_CLOUDSQL_TARGET/…_CONNECTION_NAME + a Cloud SQL DATABASE_URL"
# error) instead of the local validator silently rejecting the staging URL. The operator
# still exports DATABASE_URL + the two guard vars; this only fixes the env routing.
seed-stg-email-grants:
	cd backend && GOATOS_ENV=stg go run ./cmd/seed-dev-email-grants -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -role ceo_internal -source goatos_stg_dashboard_admins $(foreach email,$(GOATOS_STG_DASHBOARD_ADMIN_EMAILS),-email $(email))

# Permanent, idempotent seeder for the 9 canonical STG login accounts
# (docs/runbooks/stg-login-seed-contract.md): 5 ceo_internal SSO leadership +
# 3 operators + 1 pc_director. Unlike seed-stg-email-grants, this MATERIALIZES
# the active user_scope_grants row directly (does not just insert a pending
# row waiting on a first-sign-in claim event), and binds operators/director
# onto their existing named workforce_members roster row so /app/bootstrap
# grants the vaccination bottom bar immediately. See
# backend/cmd/seed-stg-login-grants/main.go for the full rationale.
# GOATOS_AUTH_ISSUER must be the stg Firebase issuer, e.g.
# https://securetoken.google.com/goatos-stg.
seed-stg-9-person-login:
	cd backend && GOATOS_ENV=stg GOATOS_TENANT_ID="$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" GOATOS_AUTH_ISSUER="$${GOATOS_AUTH_ISSUER:?set GOATOS_AUTH_ISSUER, e.g. https://securetoken.google.com/goatos-stg}" go run ./cmd/seed-stg-login-grants

# Post-seed verification for the 9-person login contract: confirms all 9
# accounts have a MATERIALIZED (status='active') user_scope_grants row, not
# just a pending email grant, and that the 4 field users are department-bound.
# See docs/runbooks/stg-9-person-login-verification.md for the full checklist
# this only partially automates (SQL only — bootstrap/app checks stay manual).
verify-stg-9-person-login:
	@echo "verify-stg-9-person-login: run the SQL checklist in docs/runbooks/stg-9-person-login-verification.md against the stg DATABASE_URL"
	@echo "quick check (requires DATABASE_URL + psql):"
	@psql "$${DATABASE_URL:?set DATABASE_URL to the stg Cloud SQL proxy target}" -c "select role, status, count(*) from user_scope_grants where tenant_id = '$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}' and scope_type = 'tenant' group by role, status order by role, status;"

# STG Firebase Auth credential setup for the canonical seed accounts.
# This is intentionally STG-only and requires the Firebase users to already
# exist: backend grant materialization derives user_id from the committed
# Firebase UID table, so creating a replacement user during seed would produce
# a different UID and a broken grant. The target sets the documented throwaway
# passwords for 5 leadership + 4 field users + Jyothi, while leaving Google SSO
# linked for leadership users.
GOATOS_STG_FIREBASE_PROJECT ?= goatos-stg
GOATOS_STG_AUTH_CONTINUE_URL ?= https://stg.dashboard.mesha.sg/login
seed-stg-firebase-password-users:
	@if [ "$${GOATOS_ENV:-}" != "stg" ]; then \
		echo "seed-stg-firebase-password-users: GOATOS_ENV must be stg"; \
		exit 2; \
	fi
	npm --prefix apps/admin-web run auth:seed-password-users -- \
	  --project "$${GOATOS_STG_FIREBASE_PROJECT:-$(GOATOS_STG_FIREBASE_PROJECT)}" \
	  --continue-url "$${GOATOS_STG_AUTH_CONTINUE_URL:-$(GOATOS_STG_AUTH_CONTINUE_URL)}" \
	  --require-existing \
	  --require-provider "ravi@mesha.sg=google.com" \
	  --require-provider "manohark@mesha.sg=google.com" \
	  --require-provider "manju@mesha.sg=google.com" \
	  --require-provider "abhishek@mesha.sg=google.com" \
	  --require-provider "aryaman@mesha.sg=google.com" \
	  --user-password "ravi@mesha.sg=Ravi@2026" \
	  --user-password "manohark@mesha.sg=Manohar@2026" \
	  --user-password "manju@mesha.sg=Manju@2026" \
	  --user-password "abhishek@mesha.sg=Abhishek@2026" \
	  --user-password "aryaman@mesha.sg=Aryaman@2026" \
	  --user-password "amit797069@gmail.com=Amit@2026" \
	  --user-password "darshantalawar033@gmail.com=Darshan@2026" \
	  --user-password "sagarmahoor143@gmail.com=Sagar@2026" \
	  --user-password "chandrakanth119527@gmail.com=Chandrakant@2026" \
	  --user-password "jyothipvg12345@gmail.com=Jyothi@2026"

seed-stg-postflight:
	@if [ "$${GOATOS_ENV:-}" != "stg" ]; then \
		echo "seed-stg-postflight: GOATOS_ENV must be stg"; \
		exit 2; \
	fi
	bash tools/dev/stg-seed-postflight.sh

# Full source-backed vaccination seed chain for local/dev rehearsals. This is the
# safe "whole setup" path after clean slate: access grants, HRMS roster/leave,
# vaccination source/config seed (which creates canonical shed locations), strict
# shed ownership, position duties, then every vaccination read model needed by
# live pages.
# Keep every DB-mutating command in the recipe, after the exact selected source
# has passed the read-only preflight. Do not move grants/roster into prerequisites:
# Make may execute prerequisites before a custom SOURCE has been rejected.
seed-vaccination-source-full: vaccination-hrms-seed-fixture-guard
	$(MAKE) seed-checkout-staleness-gate
	node tools/dev/validate-vaccination-hrms-source.mjs --source "$(GOATOS_VACCINATION_SOURCE_DIR)" --as-of "$${AS_OF:-2026-07-20}" --strict
	$(MAKE) seed-dev-email-grants
	cd backend && go run ./cmd/seed-roster-real -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -source "$(GOATOS_VACCINATION_SOURCE_DIR)" -strict
	cd backend && go run ./cmd/seed-vaccination-real -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -source "$(GOATOS_VACCINATION_SOURCE_DIR)" -expect-null-false-dob=0
	cd backend && go run ./cmd/seed-shed-positions -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -mapping "$(GOATOS_VACCINATION_SOURCE_DIR)/shed-manager-mapping.jul11-vaccination.csv" -strict -only-sheds-with-goats
	cd backend && go run ./cmd/seed-position-duties -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -strict
	$(MAKE) seed-closeout
	@if [ "$${GOATOS_ENV:-}" = "stg" ]; then \
		$(MAKE) seed-stg-firebase-password-users; \
		$(MAKE) seed-stg-9-person-login; \
		$(MAKE) seed-stg-postflight; \
	else \
		echo "GOATOS_ENV != stg: skipping stg login-grant materialization (only required for stg seeds)"; \
	fi

# BUG-023: fixtures/vaccination-cpt-operator-drive-2026-07-23/LOCAL_DB_RESEED_VALIDATION.md
# lists "the checkout SHA is not the latest origin/main" as an AUTOMATIC FAILURE, but nothing
# enforced it, so a stale checkout could produce a "clean reseed proof" built from out-of-date
# rule/seed code. This gate is read-only and runs BEFORE any DB mutation in the seed recipes.
# Fails closed: an unreachable origin, a diverged/behind HEAD, or a dirty tree stops the seed.
# GOATOS_ALLOW_STALE_SEED_CHECKOUT=1 is an explicit, loud, throwaway-experiment escape hatch;
# a run that used it is not a valid reseed proof.
seed-checkout-staleness-gate:
	@bash -c 'set -euo pipefail; \
	repo="$(REPO_ROOT)"; \
	if [ "$${GOATOS_ALLOW_STALE_SEED_CHECKOUT:-0}" = "1" ]; then \
	  echo "seed-checkout-staleness-gate: BYPASSED via GOATOS_ALLOW_STALE_SEED_CHECKOUT=1 — this run is NOT a valid reseed proof"; \
	  exit 0; \
	fi; \
	git -C "$$repo" rev-parse --is-inside-work-tree >/dev/null 2>&1 || { echo "seed-checkout-staleness-gate: $$repo is not a git checkout; refusing to seed" >&2; exit 1; }; \
	git -C "$$repo" fetch --quiet origin main || { echo "seed-checkout-staleness-gate: cannot fetch origin/main; refusing to seed from an unverifiable checkout (set GOATOS_ALLOW_STALE_SEED_CHECKOUT=1 only for a throwaway experiment)" >&2; exit 1; }; \
	head="$$(git -C "$$repo" rev-parse HEAD)"; \
	remote="$$(git -C "$$repo" rev-parse origin/main)"; \
	if [ "$$head" != "$$remote" ]; then \
	  echo "seed-checkout-staleness-gate: HEAD $$head != origin/main $$remote — a reseed from a non-origin/main checkout is an automatic failure (LOCAL_DB_RESEED_VALIDATION.md)" >&2; \
	  exit 1; \
	fi; \
	dirty="$$(git -C "$$repo" status --porcelain)"; \
	if [ -n "$$dirty" ]; then \
	  echo "seed-checkout-staleness-gate: working tree is dirty; the seeded code would not match $$head" >&2; \
	  echo "$$dirty" >&2; \
	  exit 1; \
	fi; \
	echo "seed-checkout-staleness-gate: HEAD == origin/main ($$head), tree clean"'

# BUG-009: canonical, executable CPT operator-drive rehearsal reseed. The committed packet is
# documented around raw/ filenames, while the validator and both seed commands require the
# normalized root-level bundle; this target performs that transformation (materialize-source.mjs)
# and then runs the documented chain against the materialized bundle, so the documented command
# and the executable shape agree. CPT/Channapatna only, business date 2026-07-23, the three equal
# vaccination operators at 200 unique animals/day each, and Chandrakant director-only — all read
# from the committed cpt-operator-roster.json, never synthesized, and never CBE/Coimbatore.
GOATOS_CPT_OPERATOR_DRIVE_PACKET ?= $(REPO_ROOT)/fixtures/vaccination-cpt-operator-drive-2026-07-23
# Outside fixtures/ on purpose: the materialized bundle carries reviewed runtime staff names and
# receives the seed's audit output. `build/` is gitignored, so it is never committed.
GOATOS_CPT_OPERATOR_DRIVE_BUILD_DIR ?= $(REPO_ROOT)/build/cpt-operator-drive-source
GOATOS_CPT_OPERATOR_DRIVE_AS_OF ?= 2026-07-24

seed-vaccination-cpt-operator-drive:
	$(MAKE) seed-checkout-staleness-gate
	node "$(GOATOS_CPT_OPERATOR_DRIVE_PACKET)/materialize-source.mjs" --out "$(GOATOS_CPT_OPERATOR_DRIVE_BUILD_DIR)"
	node "$(GOATOS_CPT_OPERATOR_DRIVE_PACKET)/check-expected-drive-schedules.mjs" --self-test
	node tools/dev/validate-vaccination-hrms-source.mjs --source "$(GOATOS_CPT_OPERATOR_DRIVE_BUILD_DIR)" --as-of "$(GOATOS_CPT_OPERATOR_DRIVE_AS_OF)" --strict
	cd backend && go run ./cmd/seed-roster-real -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -source "$(GOATOS_CPT_OPERATOR_DRIVE_BUILD_DIR)" -strict
	cd backend && GOATOS_CPT_EXCLUDE_PPR_2026=1 go run ./cmd/seed-vaccination-real -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -source "$(GOATOS_CPT_OPERATOR_DRIVE_BUILD_DIR)" -expect-null-false-dob=0
	cd backend && go run ./cmd/seed-shed-positions -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -mapping "$(GOATOS_CPT_OPERATOR_DRIVE_BUILD_DIR)/shed-manager-mapping.jul11-vaccination.csv" -strict -only-sheds-with-goats
	cd backend && go run ./cmd/seed-position-duties -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -strict
	GOATOS_EXPECTED_DRIVE_SCHEDULES="$(GOATOS_CPT_OPERATOR_DRIVE_PACKET)/expected-drive-schedules.json" \
	  GOATOS_EXPECTED_DRIVE_VARIANT="final_discussed_plan_et_tt_only_no_ppr" \
	  GOATOS_SEED_CLOSEOUT_SWEEP_AS_OF="$${GOATOS_SEED_CLOSEOUT_SWEEP_AS_OF:-$(GOATOS_CPT_OPERATOR_DRIVE_AS_OF)T00:00:00+05:30}" \
	  GOATOS_RUN_CPT_PASSPORT_DISPLAY_PROOF="$${GOATOS_RUN_CPT_PASSPORT_DISPLAY_PROOF:-1}" \
	  $(MAKE) seed-closeout
	@if [ "$${GOATOS_ENV:-}" = "stg" ]; then \
		$(MAKE) seed-stg-firebase-password-users; \
		$(MAKE) seed-stg-9-person-login; \
		$(MAKE) seed-stg-postflight; \
	else \
		echo "GOATOS_ENV != stg: skipping stg login-grant materialization (only required for stg seeds)"; \
	fi

# Deterministic post-seed/post-migration closeout. Runs only projectors/backfills
# that derive app-visible read models from canonical source truth; it never
# fabricates goats, owners, protocol facts, completions, or operational events.
seed-closeout:
	bash tools/dev/seed-closeout.sh

seed-closeout-dry-run:
	bash tools/dev/seed-closeout.sh --dry-run

# seed-state-guard: VACC-REV-02 promotion gate. Fails unless the target database has a
# `verified`/ready seed_run and its latest run is not `failed`/reset_required. Deployment
# promotion of a source-backed environment must run this against that environment's DB, so a
# never-verified or half-seeded database is never promoted. See
# docs/runbooks/vaccination-seed-audit-and-fix-plan-2026-07-14.md (gates C).
seed-state-guard:
	cd backend && go run ./cmd/seed-state-check -mode promotion

legacy-god-sheet-sync-dry-run:
	cd backend && go run ./cmd/legacy-god-sheet-sync --json

legacy-god-sheet-sync-apply:
	cd backend && go run ./cmd/legacy-god-sheet-sync --apply --json

verify-google-dev-seed-fixtures:
	python3 tools/dev/verify-google-dev-seed-fixtures.py

api-latency-policy-test:
	node --test tools/perf/api-latency-policy.test.mjs tools/perf/api-latency-evidence.test.mjs tools/perf/request-path-evidence.test.mjs

api-latency-gate:
	node tools/perf/api-latency-gate.mjs --manifest tools/perf/hot-paths.vaccination.json

high-scale-kernel-e2e-all:
	GOATOS_KERNEL_E2E_RUN_BROWSER=1 bash tools/dev/high-scale-kernel-e2e-all.sh

high-scale-kernel-e2e-data:
	GOATOS_KERNEL_E2E_RUN_BROWSER=0 bash tools/dev/high-scale-kernel-e2e-all.sh

high-scale-kernel-e2e-certification:
	GOATOS_KERNEL_E2E_CERTIFICATION=1 GOATOS_KERNEL_E2E_ALLOW_NOT_IMPLEMENTED=0 GOATOS_KERNEL_E2E_ALLOW_LOCAL_CHECKSUM_DRIFT=0 GOATOS_KERNEL_E2E_RUN_BROWSER=1 bash tools/dev/high-scale-kernel-e2e-all.sh

# Focused bulk status-update kernel integration tests (docker Postgres, all
# migrations). Runs in the normal suite when docker is present; here as a target
# for convenience. Proves the worker -> identity transition path end to end.
bulk-status-kernel-it:
	cd backend && go test ./internal/bulkstatus/... -count=1 -v

# Opt-in 1,000,000-row bulk status kernel gate (build tag scale_kernel). Seeds +
# drains a 1M-row job against a throwaway Postgres and asserts zero double-apply,
# zero missing outbox, zero stuck rows, crash/resume, bounded locks, throttle, and
# prints drain-time/throughput/backlog. Override GOATOS_SCALE_GATE_ROWS to resize.
scale-kernel-gate:
	cd backend && go test -tags scale_kernel -run TestBulkStatusKernelScaleGate -count=1 -v -timeout 90m ./tests/scale/...

# Faster smoke of the same gate at a smaller row count (still exercises every
# assertion, crash/resume and throttle).
scale-kernel-gate-smoke:
	cd backend && GOATOS_SCALE_GATE_ROWS=$${GOATOS_SCALE_GATE_ROWS:-20000} go test -tags scale_kernel -run TestBulkStatusKernelScaleGate -count=1 -v -timeout 20m ./tests/scale/...

# scale-cert: pre-push/scheduled certification gate for the 5k-50k envelope (NOT part of
# ci-local's inner loop — this is deliberately heavier). Runs both the canonical Calendar
# read-plan scale test and the worker cadence-drain scale test, both gated behind GOATOS_SCALE_CERT
# (see backend/internal/calendar/adapters/postgres/canonical_read_plan_test.go).
# See docs/decisions/operational-kernel-5k-50k-scale-envelope.md for scale gate charter.
scale-cert:
	cd backend && GOATOS_SCALE_CERT=1 SCALE_CERT_SIZE=$${SCALE_CERT_SIZE:-500k} go test -run "TestCalendarCanonicalReadPlanAtScale|TestReminderCadenceDrainAtScale" -timeout 30m -v ./internal/calendar/adapters/postgres/

admin-web-e2e-smoke:
	bash tools/dev/admin-web-e2e-smoke.sh

docker-storage-report:
	bash tools/dev/docker-storage-report.sh

docker-cleanup-goatos-dry-run:
	bash tools/dev/docker-cleanup-goatos.sh --delete-volumes

docker-cleanup-goatos-execute:
	bash tools/dev/docker-cleanup-goatos.sh --execute --delete-volumes

docker-storage-scripts-test:
	bash tools/dev/test-docker-storage-scripts.sh

# db-mutation-guard-test: DRV-R3 — proves the local stack scripts only migrate/seed a TRUSTED local DB
# (auto-detected docker), require GOATOS_ALLOW_DB_MUTATION=1 for an inherited/fallback DATABASE_URL, do
# single-owner prep (no dev:local double-mutation), and never re-mutate on a supervisor restart.
db-mutation-guard-test:
	bash tools/dev/test-db-mutation-guard.sh
	node --test apps/admin-web/scripts/lib/db-mutation-guard.test.mjs

local-stack-service-guard:
	bash tools/dev/test-local-stack-service-guard.sh

# Diagnose the pinned JDK/SDK/AVD setup. The scripts resolve their own environment
# and do not rely on an agent session having sourced ~/.zshrc.
.PHONY: android-doctor android-emulator-ensure android-dev-run
android-doctor:
	bash tools/dev/android-doctor.sh

android-emulator-ensure:
	bash tools/dev/android-emulator-ensure.sh

# Run the Android dev app on a physical phone when present, otherwise start the
# configured emulator automatically. Mints+validates a fresh dev token, builds,
# installs, tunnels (adb reverse), and launches. See android-dev-device.md.
android-dev-run:
	bash tools/dev/android-dev-run.sh

# grant-assistant-public-read: (re)apply read-only public + ceo_ai SELECT grants
# to the assistant DB roles after they exist (migration 000031 is guarded and
# no-ops for a role created after it ran). Idempotent. See the secrets runbook.
grant-assistant-public-read:
	tools/dev/grant-assistant-public-read.sh

# ─── CEO-AI answer-quality eval harness (tools/ceo-ai/eval) ──────────────────
# ceo-ai-eval-selftest: the always-runnable structural gate. No assistant, no
# database, no Vertex — it validates the golden set (schema, kebab ids, read-only
# oracles, tier vocab, >=40 Qs / >=20 classes) and unit-tests the deterministic
# scorer. Cheap enough to run on every backend CI pass. See docs/ceo-ai/eval.md.
ceo-ai-eval-selftest:
	cd tools/ceo-ai/eval && test -z "$$(gofmt -l .)" || { echo "gofmt: files need formatting:"; gofmt -l .; exit 1; }
	cd tools/ceo-ai/eval && go vet ./...
	cd tools/ceo-ai/eval && go test ./...
	cd tools/ceo-ai/eval && go run . -validate

# ceo-ai-eval: the LIVE answer-quality regression. Runs every golden question
# through the assistant endpoint, scores each answer against an independent
# Postgres oracle, and writes a self-contained HTML + JSON report under
# tools/ceo-ai/eval/out/. Requires MESHA_ASSISTANT_URL, GOATOS_EVAL_DATABASE_URL,
# GOATOS_EVAL_TENANT_ID (and usually MESHA_EVAL_BEARER). STRICT=1 turns a missing
# prerequisite into a hard failure (this target was explicitly requested, so a
# misconfiguration is an error, not a silent skip). NOT part of the default gate.
CEO_AI_EVAL_HTML ?= out/ceo-ai-eval-report.html
CEO_AI_EVAL_JSON ?= out/ceo-ai-eval-report.json
ceo-ai-eval:
	cd tools/ceo-ai/eval && mkdir -p out && CEO_AI_EVAL_STRICT=1 go run . -html "$(CEO_AI_EVAL_HTML)" -json "$(CEO_AI_EVAL_JSON)"
