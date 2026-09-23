# Cloud, Google Account, GitHub and Organization Boundary Rules

> Moved verbatim from `AGENTS.md` (split 2026-09-24 to keep session start small).
> These rules are as binding as `AGENTS.md` itself. Only file location changed.

## GCP Billing Console Landing Rule

When investigating Google Cloud billing for Goat OS, always land directly on
the working billing reports page for the Mesha account:

```text
https://console.cloud.google.com/billing/01FEDE-96BCB3-76D992/reports?authuser=2&organizationId=563962826703&project=goatos-stg
```

Use the `ravi@mesha.sg` Google account. Do not use the personal Gmail accounts
for billing reports; they land on the Google Cloud "You need additional access"
error and are missing permissions such as `billing.resourceCosts.get`.

For the 2026-08-26 billing investigation, the reports page showed August
forecasted cost of about `₹34,382.31`, mostly driven by Cloud Run
(`₹17,869.22` for 1-25 Aug), then Cloud SQL (`₹4,954.30`) and BigQuery
(`₹2,152.09`). Always read the report table before guessing from the overview
balance.

## Google Account Selection Rule

For Goat OS work in Firebase, Google Cloud Console / `gcloud`, Google Drive,
Docs, Sheets, Gmail, or any other Google surface where multiple signed-in
accounts are present, always use `ravi@mesha.sg` unless the maintainer
explicitly names a different account for that task. In browser URLs, prefer the
matching `authuser` for `ravi@mesha.sg`; if the visible page is on another
account, switch accounts instead of continuing from the wrong identity.

Do not stop just because the first visible Google account lacks access. Switch
to `ravi@mesha.sg`, retry the target page or command, and only report a blocker
after verifying that `ravi@mesha.sg` itself lacks the required permission or the
session needs an interactive reauth that Codex cannot complete.

Current repos:

- `dashboard/` - current live CEO-style Next.js dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/admin-web/`.
- `vgoats-dashboard/` - current live investor/reduced dashboard; do not edit for Goat OS rewiring. Snapshot/copy into `goatos/apps/investor-web-shadow/`.
- `procurement_app/` - reusable React Native camera/upload/team ideas; currently procurement/Firebase coupled.
- `website/` - public MESHA site; business-context reference only, out of Goat OS core.
- `slack-automation-scripts/` - legacy Slack/Sheets/App Script automation.

> "Organization boundaries" (Heva/Slice separation, verify account/project before cloud writes, GitHub token path, Mesha-only commit identity) moved to core `AGENTS.md` because it applies to every commit/cloud action.
