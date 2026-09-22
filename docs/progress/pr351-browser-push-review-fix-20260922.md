# PR 351 browser push review fix progress

Scope: fix PR 351 review finding where stored leadership audiences still used the raw Android-only roster, leaving Chrome/browser recipients out of configured leadership alert audiences.

Current SHA before fix: 98321ebdda52fd5fb029dbad9d67f432631ab5ca

Done:
- Review found `NewStoredAudience(rosterService, ...)` still bypassed `WithBrowserRecipients`.
- Switched production stored-audience wiring to use the browser-aware recipient resolver.
- Tightened `audience_wiring_test.go` so `NewStoredAudience` cannot be built from the raw roster service.
- Added browser-aware batch recipient support for reminder cadence duty and leadership audiences.
- Pushed PR branch `codex/web-push-notifications` to `aa72343f352d266fc4015425b67f5e9cf10ca01a`.

Pending:
- Post-push review agent pass.

Tests/E2E:
- Before fix, PR-listed tests passed in isolated worktree.
- `cd backend && go test ./internal/browserpush ./internal/notificationbridge` passed.
- `cd backend && go test ./internal/browserpush/adapters/postgres` passed.
- `cd backend && go test ./internal/kernelstages` passed.

Known failures:
- None in focused tests.

Judge status:
- Review finding fixed and pushed; review agent pass pending.

Deployment state:
- No merge, no main push, no staging deploy.
