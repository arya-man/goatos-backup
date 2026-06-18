.PHONY: check guardrails test api-client-generate api-client-check sqlc-generate sqlc-check validate-migrations validate-sqlc-plans replay-live replay-delta docker-storage-report docker-cleanup-goatos-dry-run docker-cleanup-goatos-execute docker-storage-scripts-test rebuild-identity-counters update-identity-counters dev-local dev-local-service-install dev-local-service-start dev-local-service-stop dev-local-service-restart dev-local-service-status dev-local-service-logs dev-local-service-uninstall

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
	bash tools/sqlc/check-version.sh
	cd backend && sqlc generate -f sqlc.yaml

sqlc-check: sqlc-generate
	git diff --exit-code -- backend/sqlc.yaml backend/internal/identity/adapters/postgres/sqlc backend/internal/legacy_import/adapters/postgres/sqlc backend/internal/reporting/adapters/postgres/sqlc

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

rebuild-identity-counters:
	@if [ -z "$(TENANT_ID)" ]; then echo "TENANT_ID is required"; exit 1; fi
	cd backend && go run ./cmd/rebuild-identity-counters -tenant-id "$(TENANT_ID)" $(if $(SOURCE_IMPORT_RUN_ID),-source-import-run-id "$(SOURCE_IMPORT_RUN_ID)") $(if $(GRAINS),-grains "$(GRAINS)")

update-identity-counters:
	@if [ -z "$(TENANT_ID)" ]; then echo "TENANT_ID is required"; exit 1; fi
	cd backend && go run ./cmd/update-identity-counters -tenant-id "$(TENANT_ID)" $(if $(LIMIT),-limit "$(LIMIT)") $(if $(PROCESSED_EVENTS_RETENTION),-processed-events-retention "$(PROCESSED_EVENTS_RETENTION)")
