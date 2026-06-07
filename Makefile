.PHONY: check guardrails validate-migrations

guardrails:
	bash tools/agent-hooks/check-boundaries.sh
	bash tools/agent-hooks/check-contract-drift.sh

check: guardrails
	@if [ -f backend/go.mod ]; then cd backend && go test ./...; fi

validate-migrations:
	bash backend/tests/integration/validate-postgres-migrations.sh
