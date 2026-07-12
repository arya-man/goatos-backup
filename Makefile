SQLC ?= $(shell command -v sqlc 2>/dev/null || if command -v go >/dev/null 2>&1; then gopath=$$(go env GOPATH 2>/dev/null); if [ -x "$$gopath/bin/sqlc" ]; then printf '%s/bin/sqlc' "$$gopath"; fi; fi)
GOATOS_LOCAL_TENANT_ID ?= 00000000-0000-4000-8000-000000000001
GOATOS_DEV_DASHBOARD_ADMIN_EMAILS ?= abhishek@mesha.sg aryaman@mesha.sg manju@mesha.sg manohark@mesha.sg ravi@mesha.sg
GOATOS_STG_DASHBOARD_ADMIN_EMAILS ?= $(GOATOS_DEV_DASHBOARD_ADMIN_EMAILS)
REPO_ROOT ?= $(shell git rev-parse --show-toplevel 2>/dev/null || pwd)
AI_BACKEND ?= auto

.PHONY: check guardrails e2e-integrity-guard scale-guard clinical-defer-guard sweeper-deployment-guard idempotency-writes-guard atomic-readmodel-sync-guard config-validate-guard india-date-guard offline-first-guard ci-local mobile-guard mobile-guard-audit test api-client-generate api-client-check sqlc-generate sqlc-check validate-hot-index-migrations validate-migrations validate-sqlc-plans pre-google-readiness seed-calendar-vaccination-dev seed-dev-email-grants seed-stg-email-grants legacy-god-sheet-sync-dry-run legacy-god-sheet-sync-apply verify-google-dev-seed-fixtures process-integrity-projection-recompute vaccination-shed-projection-recompute process-integrity-latency-gate api-latency-policy-test api-latency-gate high-scale-kernel-e2e-all high-scale-kernel-e2e-data high-scale-kernel-e2e-certification bulk-status-kernel-it scale-kernel-gate scale-kernel-gate-smoke admin-web-e2e-smoke docker-storage-report docker-cleanup-goatos-dry-run docker-cleanup-goatos-execute docker-storage-scripts-test dev-local dev-local-service-install dev-local-service-start dev-local-service-stop dev-local-service-restart dev-local-service-status dev-local-service-logs dev-local-service-uninstall setup-crg update-docs-graph
.PHONY: ai-setup ai-doctor ai-rebuild ai-rebuild-code ai-rebuild-docs ai-rebuild-repowise ai-repowise-coverage docs-graph-open ai-telemetry ai-telemetry-ui

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
# local dev DB up (:55432) for full coverage; partial coverage still ingests.
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
	bash tools/agent-hooks/check-boundaries.sh
	bash tools/agent-hooks/check-contract-drift.sh
	bash tools/agent-hooks/check-e2e-kernel-integrity.sh
	$(MAKE) api-latency-policy-test
	$(MAKE) scale-guard
	$(MAKE) clinical-defer-guard
	$(MAKE) sweeper-deployment-guard
	$(MAKE) idempotency-writes-guard
	$(MAKE) atomic-readmodel-sync-guard
	$(MAKE) config-validate-guard
	$(MAKE) india-date-guard
	$(MAKE) offline-first-guard

# clinical-defer-guard: block the C35-010 medical-safety anti-pattern — a PARTIAL
# clinical defer_states list in production code/seeds. sick/under_treatment/
# quarantine/icu are mandatory safety blocks; a non-empty list omitting any of them
# would let a sick animal's open work be cancelled instead of deferred. Runs its
# adversarial self-test first, then scans the whole production tree. See
# docs/preventive-care-vaccination/vaccination-rules.md.
clinical-defer-guard:
	node tools/agent-hooks/check-clinical-defer-states.mjs --self-test
	node tools/agent-hooks/check-clinical-defer-states.mjs

sweeper-deployment-guard:
	node tools/agent-hooks/check-sweeper-deployment.mjs --self-test
	node tools/agent-hooks/check-sweeper-deployment.mjs

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

# ci-local: run the SAME required CI gates as .github/workflows/ci.yml locally.
# Per AGENTS.md a GitHub Actions billing/platform failure is NEVER a closure
# blocker — a green `make ci-local` on the pushed SHA is the authoritative gate.
# JOB=guardrails|admin-web|android runs one job; default runs all.
ci-local:
	bash tools/ci/run-local-ci.sh $(JOB)

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
mobile-guard:
	node tools/agent-hooks/check-mobile-list-fetch.mjs --self-test
	node tools/agent-hooks/check-mobile-list-fetch.mjs

mobile-guard-audit:
	node tools/agent-hooks/check-mobile-list-fetch.mjs --all

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

check: guardrails docker-storage-scripts-test
	$(MAKE) test

dev-local:
	bash tools/dev/run-local-stack.sh

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

seed-calendar-vaccination-dev:
	cd backend && go run ./cmd/seed-calendar-vaccination-dev

seed-dev-email-grants:
	cd backend && go run ./cmd/seed-dev-email-grants -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -role ceo_internal -source goatos_dev_dashboard_admins $(foreach email,$(GOATOS_DEV_DASHBOARD_ADMIN_EMAILS),-email $(email))

# Sets GOATOS_ENV=stg so the seed routes to the staging Cloud SQL validator (a clear
# "needs GOATOS_ALLOW_STG_CLOUDSQL_TARGET/…_CONNECTION_NAME + a Cloud SQL DATABASE_URL"
# error) instead of the local validator silently rejecting the staging URL. The operator
# still exports DATABASE_URL + the two guard vars; this only fixes the env routing.
seed-stg-email-grants:
	cd backend && GOATOS_ENV=stg go run ./cmd/seed-dev-email-grants -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -role ceo_internal -source goatos_stg_dashboard_admins $(foreach email,$(GOATOS_STG_DASHBOARD_ADMIN_EMAILS),-email $(email))

legacy-god-sheet-sync-dry-run:
	cd backend && go run ./cmd/legacy-god-sheet-sync --json

legacy-god-sheet-sync-apply:
	cd backend && go run ./cmd/legacy-god-sheet-sync --apply --json

verify-google-dev-seed-fixtures:
	python3 tools/dev/verify-google-dev-seed-fixtures.py

process-integrity-projection-recompute:
	cd backend && go run ./cmd/process-integrity-projection-recompute

vaccination-shed-projection-recompute:
	cd backend && go run ./cmd/vaccination-shed-projection-recompute

process-integrity-latency-gate:
	cd backend && go run ./cmd/process-integrity-latency-check

api-latency-policy-test:
	node --test tools/perf/api-latency-policy.test.mjs

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
