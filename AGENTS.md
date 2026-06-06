# Goat OS Workspace Agent Context

Read first:

- `<mesha-workspace>/goatos/context/README.md`
- `<mesha-workspace>/goatos/context/architecture/final-architecture.md`
- `<mesha-workspace>/goatos/context/frontend/final-frontend-mobile-backend-architecture.md`
- `<mesha-workspace>/goatos/context/forms/final-forms-sop-engine.md`
- `<mesha-workspace>/goatos/context/analytics/final-analytics-infra.md`
- `<mesha-workspace>/goatos/context/agents/ai-agent-context-and-protocols.md`

Ignore unless explicitly asked for historical archaeology:

- `<mesha-workspace>/goatos/docs/archive/planning-history/`

Purpose:

- Goat OS is the operating system for goat identity, health, vaccination, genetics, breeding, workforce, SOP tasks, media proof, verification, devices, commerce interfaces, and analytics.
- Existing UI should be salvaged where useful. Canonical backend/data model/app APIs are built fresh.

Current repos:

- `dashboard/` - current live CEO-style Next.js dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/admin-web/`.
- `vgoats-dashboard/` - current live investor/reduced dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/investor-web-shadow/`.
- `procurement_app/` - reusable React Native camera/upload/team ideas; currently procurement/Firebase coupled.
- `website/` - public MESHA site; business-context reference only, out of Goat OS core.
- `slack-automation-scripts/` - legacy Slack/Sheets/App Script automation.

Do:

- Keep architecture facts in `<mesha-workspace>/goatos/context`.
- Use `<mesha-workspace>/goatos/context/agents/skills/goatos-build/SKILL.md` as the agent reference map.
- Use ports/adapters for replaceable vendors and tools.
- Use OpenAPI REST/JSON for web/mobile app APIs.
- Use JSON Schema for form DSL and event payload contracts.
- Use protobuf/gRPC only behind the app API boundary when a real internal workload needs it.
- Keep frontend/mobile data access behind generated clients and app APIs.
- Read wide, write narrow: agents may inspect the whole tree, but edits must stay within declared task scope.
- Treat million-goat scale as a hard requirement: chunk jobs, use idempotency, avoid full-herd scans, bound goroutines, and keep media/analytics off operational API hot paths.
- Add observability for new APIs/workers: latency, errors, DB pressure, queue lag, DLQ, and media failures.

Do not:

- Do not reintroduce old staging labels as architecture.
- Do not let frontend/mobile read BigQuery, Sheets, Firestore, GCS, or operational databases directly.
- Do not spread vendor SDK calls through product code.
- Do not modify current live dashboard repos while building Goat OS copies.
- Do not add unbounded goroutines, full-table/full-herd API scans, direct media proxying through APIs, or dashboard raw BigQuery scans.
- Do not use direct gRPC for browser/React Native product clients without a new written ADR.
- Do not duplicate architecture decisions across random docs.

Validation expectation:

- Run the narrowest relevant typecheck/build/test command for changed code.
- For docs-only edits, run greps for stale terms when the user has explicitly banned wording.
