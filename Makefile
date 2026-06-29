SQLC ?= $(shell command -v sqlc 2>/dev/null || if command -v go >/dev/null 2>&1; then gopath=$$(go env GOPATH 2>/dev/null); if [ -x "$$gopath/bin/sqlc" ]; then printf '%s/bin/sqlc' "$$gopath"; fi; fi)
GOATOS_LOCAL_TENANT_ID ?= 00000000-0000-4000-8000-000000000001
GOATOS_DEV_DASHBOARD_ADMIN_EMAILS ?= abhishek@mesha.sg aryaman@mesha.sg manju@mesha.sg ravi@mesha.sg
REPO_ROOT ?= $(shell git rev-parse --show-toplevel 2>/dev/null || pwd)
AI_BACKEND ?= auto

.PHONY: check guardrails test api-client-generate api-client-check sqlc-generate sqlc-check validate-migrations validate-sqlc-plans seed-calendar-vaccination-dev seed-dev-email-grants admin-web-e2e-smoke replay-live replay-delta docker-storage-report docker-cleanup-goatos-dry-run docker-cleanup-goatos-execute docker-storage-scripts-test dev-local dev-local-service-install dev-local-service-start dev-local-service-stop dev-local-service-restart dev-local-service-status dev-local-service-logs dev-local-service-uninstall setup-crg update-docs-graph
.PHONY: ai-setup ai-doctor ai-rebuild ai-rebuild-code ai-rebuild-docs ai-telemetry

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
	$(MAKE) ai-doctor
	@echo ""
	@echo "AI setup ready. CRG/Graphify outputs are local generated artifacts and stay gitignored."
	@echo "Build or refresh local graphs with: make ai-rebuild AI_BACKEND=$(AI_BACKEND)"

ai-doctor:
	bash tools/agent-hooks/ai-doctor.sh

ai-rebuild: ai-rebuild-code ai-rebuild-docs

ai-rebuild-code:
	code-review-graph build --repo "$(REPO_ROOT)"

ai-rebuild-docs:
	@mkdir -p graphify-out
	AI_BACKEND="$(AI_BACKEND)" bash tools/agent-hooks/rebuild-docs-graph.sh

update-docs-graph:
	$(MAKE) ai-rebuild-docs

ai-telemetry:
	python3 tools/ai/analyze-transcripts.py --project goatos

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

validate-migrations:
	bash backend/tests/integration/validate-postgres-migrations.sh

validate-sqlc-plans:
	bash backend/tests/integration/validate-sqlc-query-plans.sh

seed-calendar-vaccination-dev:
	cd backend && go run ./cmd/seed-calendar-vaccination-dev

seed-dev-email-grants:
	cd backend && go run ./cmd/seed-dev-email-grants -tenant-id "$${GOATOS_TENANT_ID:-$(GOATOS_LOCAL_TENANT_ID)}" -role ceo_internal -source goatos_dev_dashboard_admins $(foreach email,$(GOATOS_DEV_DASHBOARD_ADMIN_EMAILS),-email $(email))

admin-web-e2e-smoke:
	bash tools/dev/admin-web-e2e-smoke.sh

replay-live:
	bash tools/replay/live-replay.sh

replay-delta:
	bash tools/replay/snapshot-delta-replay.sh

docker-storage-report:
	bash tools/dev/docker-storage-report.sh

docker-cleanup-goatos-dry-run:
	bash tools/dev/docker-cleanup-goatos.sh --delete-volumes

docker-cleanup-goatos-execute:
	bash tools/dev/docker-cleanup-goatos.sh --execute --delete-volumes

docker-storage-scripts-test:
	bash tools/dev/test-docker-storage-scripts.sh
