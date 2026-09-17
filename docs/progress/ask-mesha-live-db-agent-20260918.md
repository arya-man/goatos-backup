# Ask Mesha Live DB Agent Progress - 2026-09-18

## Scope

Make Ask Mesha answer natural operational questions from the real read-only
database path and governed in-process read APIs instead of surfacing MCP/read
API plumbing failures. Covered verticals: counts, weighing, sales, feed,
procurement, source-entry health, and operations exceptions.

## Done

- Verified Git-tracked instructions require real `goatos-stg` Cloud SQL for live
  staging/dashboard data.
- Verified live `goatos-stg` answer for CPT from `ceo_ai.animal_current_scope`:
  716 active animals, split 185 goats / 531 sheep.
- Located current assistant route stack: Cube -> API -> Toolbox -> SQL fallback.
- Located guarded SQL executor and chart composer support.
- Added deterministic SQL routing for natural CPT/CBE active-animal questions,
  typo-heavy variants, breed follow-ups, weighing rankings, feed variance,
  source-entry health, and operations exceptions.
- Added SQL row shaping for `label` / `value` / `scope` rows so answers and
  charts render from structured facts rather than raw column dumps.
- Wired the backend bootstrap to create the guarded SQL fallback from
  `MESHA_CEO_READONLY_DATABASE_URL`, `MESHA_CEO_READONLY_DB_URL`, or
  `MESHA_MCP_DB_DSN` when available.
- Added sales overview as an in-process read tool backed by the same sales
  service used by the Sales page.
- Switched the Vertex model config to `gemini-3.8-flash`.
- Removed vaccination starter prompts from Ask Mesha defaults and replaced them
  with sales/weighing/procurement/feed/ops starters.
- Added tests for active animal questions, typo handling, breed follow-ups,
  graph/pen breakdowns, weighing shorthand, sales priority, feed `as_of`,
  adult goat count routing, and conversation-aware cache keys.

## Pending

- Raise PR and complete PR review/judge loop.
- Run full local landing gate before any merge/deploy.

## Proof

- Current live DB query was run through Cloud SQL Auth Proxy to
  `goatos-stg:asia-south1:goatos-stg-core-db` with Secret Manager read-only DSN.
- Local tests passed:
  `cd backend && go test ./internal/ceoai/app ./internal/bootstrap ./internal/ceoai/adapters/readtools ./internal/ceoai/adapters/keywordplanner ./internal/ceoai/safety ./internal/ceoai/sqlguard`
- Live local API probes against `goatos-stg` returned:
  sales this month 114 animals; CPT adult goats 95; CPT feed variance rows for
  today's feed day; CPT active split 185 goats / 531 sheep; and breed follow-up
  split from the remembered CPT scope.
- Chrome/in-app browser verified Ask Mesha starters have no vaccination prompts
  and a sales question answers through `Mesha read API · Sales overview`.
- No merge, deploy, or push has happened.

## Current SHA

33ffceb0d before committing this work.

## Deployment State

Local only. No staging deploy started.
