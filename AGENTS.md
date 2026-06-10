# Goat OS Workspace Agent Context

Read first:

- `context/README.md`
- `SKILLS.md`
- `.agents/skills/goatos-build/SKILL.md`
- `context/architecture/final-architecture.md`
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/forms/final-forms-sop-engine.md`
- `context/analytics/final-analytics-infra.md`
- `context/agents/ai-agent-context-and-protocols.md`

Ignore unless explicitly asked for historical archaeology:

- `docs/archive/planning-history/`

Purpose:

- Goat OS is the operating system for goat identity, health, vaccination, genetics, breeding, workforce, SOP tasks, media proof, verification, devices, commerce interfaces, and analytics.
- Existing UI should be salvaged where useful. Canonical backend/data model/app APIs are built fresh.

Current repos:

- `dashboard/` - current live CEO-style Next.js dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/admin-web/`.
- `vgoats-dashboard/` - current live investor/reduced dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/investor-web-shadow/`.
- `procurement_app/` - reusable React Native camera/upload/team ideas; currently procurement/Firebase coupled.
- `website/` - public MESHA site; business-context reference only, out of Goat OS core.
- `slack-automation-scripts/` - legacy Slack/Sheets/App Script automation.

Organization boundaries:

- Mesha/VGoats, Heva, and Slice are separate businesses and must never be
  mixed in GitHub or Google Cloud operations.
- Goat OS belongs to Mesha/VGoats. Google Cloud work for Goat OS targets the
  `vgoats.com` organization and future `goatos-dev`, `goatos-stg`, and
  `goatos-prod` projects.
- Do not use Heva projects/orgs, Slice projects/orgs, or `hevaplatform` for
  Goat OS work.
- Do not modify or replace the legacy `goatos-sheets` project while creating
  Goat OS projects.
- Before any cloud/GitHub command that creates, updates, deletes, grants IAM,
  links billing, deploys, or changes configuration, verify and state the active
  account, organization, folder, project, and target repo. If the target is not
  Mesha/VGoats for Goat OS work, stop and correct context first.
- Create Goat OS cloud resources under `vgoats.com`, preferably in a `goat-os`
  folder, or directly under the org if folder creation is not available. Do not
  create Goat OS resources inside `system-gsuite` or `apps-script`.

Do:

- Keep architecture facts in `context/`.
- Use `.agents/skills/goatos-build/SKILL.md` as the active agent reference map.
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
- At phase closeout, compare code/contracts/migrations/tests against PRD/TRD and
  update context/skills/agent references if implementation changed the truth.
- For docs-only edits, run greps for stale terms when the user has explicitly banned wording.

Morning README update expectation:

- For Goat OS work sessions that start in the morning, check whether `README.md`
  reflects the latest pushed project status before moving deep into new
  implementation work.
- If phase progress, shipped backend/frontend pieces, deploy gates, real-data
  import status, or next-step priorities changed, update `README.md` with
  executive status wording and push it.
- Be precise: do not call Phase 1 shippable until auth/RBAC, frontend screens,
  production event egress, and real data-run gaps are actually closed.

Workflow documentation expectation:

- If GitHub Actions workflows or CI guardrail scripts change, update
  `docs/runbooks/github-workflows.md` with clear project-facing wording in the
  same change.
- The runbook must explain what each workflow does, when it runs, what temporary
  services it starts, and what common failures mean.
