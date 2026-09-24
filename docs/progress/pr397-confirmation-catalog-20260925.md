# PR 397 confirmation and feed catalog fixes

Scope: preserve the web sale draft when the server refuses it; reject unknown/retired feed variants before a sale can write a stock movement. PR branch only.
Base SHA: 1c6f91088a3e2895fda65460601674b62da3e2a7. Candidate SHA: the commit containing this document.

## Done
- Record-sale refusals return structured results without navigation or React form-action reset. The existing drawer preserves native and controlled inputs, shows the server message and prevents overlapping submits. Success retains the normal list redirect.
- Feed lines are checked against the tenant's active catalog before stock lookup, even with shortage acknowledgement. The repository repeats validation within its write transaction, after idempotency replay resolution.
- Added refusal-action and unknown-feed service regression tests, plus an opt-in repository integration test. The browser scenario now confirms the original draft without refilling fields and asserts draft preservation.

## Validation
- `go test -p 1 ./internal/sales/...`: passed; database tests skipped by default.
- Node sale-lines and sales-record-action tests: 6 passed.
- ESLint on both changed TSX/TS files: passed.
- `make postgres-bind-contract-guard aggregate-projection-guard`: passed.
- `node --check tools/e2e/sales-feed-browser-e2e.mjs`: passed.
- `git diff --check`: passed.
- First typecheck using a shared dependency symlink failed on stale/missing dependencies. Removed that symlink and installed the lockfile locally. The fresh typecheck identified an optional error-code type, corrected with a fallback; final `npm run typecheck` rerun passed.

## Before/after and remaining proof
- Before: shortage response redirected away from the draft; after: structured refusal preserves the mounted form. Before: unknown feed reached stock/write; after: field validation rejects it before either operation, with or without acknowledgement.
- QA server at 127.0.0.1:3423 is unavailable. Real-route browser E2E, mobile failure-string checks, latency checks and opt-in database integration were not executed in this follow-up.
- No performance improvement claimed. No independent judge run. Full local CI not run.
- Deployment: not started. No main merge or staging promotion. PR push is the next operation; the final task response records remote SHA readback.
