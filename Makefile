.PHONY: check guardrails test sqlc-generate sqlc-check validate-migrations validate-sqlc-plans rebuild-identity-counters

guardrails:
	bash tools/agent-hooks/check-boundaries.sh
	bash tools/agent-hooks/check-contract-drift.sh

test:
	@if [ -f backend/go.mod ]; then cd backend && go test ./...; fi

sqlc-generate:
	bash tools/sqlc/dump-schema.sh
	bash tools/sqlc/check-version.sh
	cd backend && sqlc generate -f sqlc.yaml

sqlc-check: sqlc-generate
	git diff --exit-code -- backend/sqlc.yaml backend/internal/identity/adapters/postgres/sqlc backend/internal/legacy_import/adapters/postgres/sqlc backend/internal/reporting/adapters/postgres/sqlc

check: guardrails
	$(MAKE) test

validate-migrations:
	bash backend/tests/integration/validate-postgres-migrations.sh

validate-sqlc-plans:
	bash backend/tests/integration/validate-sqlc-query-plans.sh

rebuild-identity-counters:
	@if [ -z "$(TENANT_ID)" ]; then echo "TENANT_ID is required"; exit 1; fi
	cd backend && go run ./cmd/rebuild-identity-counters -tenant-id "$(TENANT_ID)" $(if $(SOURCE_IMPORT_RUN_ID),-source-import-run-id "$(SOURCE_IMPORT_RUN_ID)") $(if $(GRAINS),-grains "$(GRAINS)")
