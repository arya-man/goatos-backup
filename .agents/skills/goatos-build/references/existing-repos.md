# Existing Repos Reference

Load this when inspecting or migrating from current cloned repos.

Canonical docs:

- `context/repo-audits/` when populated
- `context/frontend/final-frontend-mobile-backend-architecture.md`
- `context/execution/target-repo-structure.md`
- root historical docs: `existing-repos-inventory.md`, `existing-repos-deep-audit.md`

Current workspace repos:

```text
dashboard/                 reference CEO/admin dashboard UI
vgoats-dashboard/           reference investor/reduced dashboard UI
procurement_app/            reference React Native camera/upload/team ideas
slack-automation-scripts/   legacy Slack/Sheets/App Script workflows
website/                    business-context reference only; out of Goat OS core
```

Rules:

- Do not modify current live dashboard repos for Goat OS rewiring.
- Snapshot/clone dashboard frontends into `goatos/apps/` and modify only those copies.
- Do not copy old repos blindly; snapshot dashboard UI intentionally because it is the starting UI baseline.
- Salvage UI/components/patterns deliberately.
- Replace data sources with Goat OS backend/analytics APIs.
- Do not extend Slack/App Script as canonical product code.
- For phase discovery, read legacy repos as evidence. Extract source paths,
  column names, aggregate counts, SOP/form names, and behavior summaries. Do
  not commit raw private rows, Slack payloads, tokens, PII, or media URLs.
- Slack/App Script SOP workflows are operating knowledge. Inventory them before
  replacing them with Android/admin SOP forms.
