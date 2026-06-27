SQLC ?= $(shell command -v sqlc 2>/dev/null || if command -v go >/dev/null 2>&1; then gopath=$$(go env GOPATH 2>/dev/null); if [ -x "$$gopath/bin/sqlc" ]; then printf '%s/bin/sqlc' "$$gopath"; fi; fi)

.PHONY: check guardrails test api-client-generate api-client-check sqlc-generate sqlc-check validate-migrations validate-sqlc-plans seed-calendar-vaccination-dev admin-web-e2e-smoke replay-live replay-delta docker-storage-report docker-cleanup-goatos-dry-run docker-cleanup-goatos-execute docker-storage-scripts-test dev-local dev-local-service-install dev-local-service-start dev-local-service-stop dev-local-service-restart dev-local-service-status dev-local-service-logs dev-local-service-uninstall setup-crg update-docs-graph

setup-crg:
	@echo "Installing code-review-graph for structural code graph (Layer 1)..."
	pip install code-review-graph || pipx install code-review-graph
	code-review-graph install
	code-review-graph build
	@echo ""
	@echo "CRG ready. goatos-docs Graphify graph already committed at graphify-out/graph.json."
	@echo "To rebuild it after major doc changes: make update-docs-graph"

update-docs-graph:
	@echo "Rebuilding goatos-docs Graphify graph (requires graphify + LLM calls)..."
	@echo "Run /graphify in Claude Desktop/Terminal:"
	@echo "  /graphify docs context .agents/skills/goatos-build/references --update"
	@echo "Then commit the updated graphify-out/graph.json and graphify-out/GRAPH_REPORT.md"

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
