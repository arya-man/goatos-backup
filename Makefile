.PHONY: check guardrails test validate-migrations

guardrails:
	bash tools/agent-hooks/check-boundaries.sh
	bash tools/agent-hooks/check-contract-drift.sh

test:
	@if [ -f backend/go.mod ]; then cd backend && go test ./...; fi

check: guardrails
	$(MAKE) test

validate-migrations:
	bash backend/tests/integration/validate-postgres-migrations.sh
