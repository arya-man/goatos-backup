SQLC ?= $(shell command -v sqlc 2>/dev/null || if command -v go >/dev/null 2>&1; then gopath=$$(go env GOPATH 2>/dev/null); if [ -x "$$gopath/bin/sqlc" ]; then printf '%s/bin/sqlc' "$$gopath"; fi; fi)
GOATOS_LOCAL_TENANT_ID ?= 00000000-0000-4000-8000-000000000001
GOATOS_DEV_DASHBOARD_ADMIN_EMAILS ?= abhishek@mesha.sg aryaman@mesha.sg manju@mesha.sg manohark@mesha.sg ravi@mesha.sg
GOATOS_STG_DASHBOARD_ADMIN_EMAILS ?= $(GOATOS_DEV_DASHBOARD_ADMIN_EMAILS)
REPO_ROOT ?= $(shell git rev-parse --show-toplevel 2>/dev/null || pwd)
AI_BACKEND ?= auto

.PHONY: check guardrails test api-client-generate api-client-check sqlc-generate sqlc-check validate-hot-index-migrations validate-migrations validate-sqlc-plans pre-google-readiness seed-calendar-vaccination-dev seed-dev-email-grants seed-stg-email-grants legacy-god-sheet-sync-dry-run legacy-god-sheet-sync-apply verify-google-dev-seed-fixtures process-integrity-projection-recompute process-integrity-latency-gate api-latency-gate high-scale-kernel-e2e-all high-scale-kernel-e2e-data high-scale-kernel-e2e-certification bulk-status-kernel-it scale-kernel-gate scale-kernel-gate-smoke admin-web-e2e-smoke docker-storage-report docker-cleanup-goatos-dry-run docker-cleanup-goatos-execute docker-storage-scripts-test dev-local dev-local-service-install dev-local-service-start dev-local-service-stop dev-local-service-restart dev-local-service-status dev-local-service-logs dev-local-service-uninstall setup-crg update-docs-graph
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

process-integrity-latency-gate:
	cd backend && go run ./cmd/process-integrity-latency-check

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
