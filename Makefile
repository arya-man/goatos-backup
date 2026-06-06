.PHONY: check guardrails

guardrails:
	bash tools/agent-hooks/check-boundaries.sh
	bash tools/agent-hooks/check-contract-drift.sh

check: guardrails
	@if [ -f backend/go.mod ]; then cd backend && go test ./...; fi
