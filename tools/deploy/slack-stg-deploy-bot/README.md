# Slack STG Deploy Bot Ownership

The runnable Slack deploy bot moved to:

```text
/Users/raviteja/mesha/mesha-ops/slack-stg-deploy-bot
```

Patch and deploy bot behavior, Slack interactivity, active-build detection,
quota retry, and bot image rollout from `vgoats/mesha-ops`.

This Goat OS directory intentionally keeps only `deploy-card.json` because the
Goat OS product deploy scripts post the idle panel from the product checkout
after Cloud Build / Cloud Deploy / Android distribution finishes. That card is a
contract fixture shared with the mesha-ops bot tests.

Do not add a second runnable bot implementation here. Goat OS still owns the
product deploy DAG:

- `cloudbuild.stg.yaml`
- `tools/deploy/stg-cloudbuild-release.sh`
- `tools/deploy/stg-clouddeploy-release.sh`
- `tools/deploy/stg-clouddeploy-task.sh`
- `tools/deploy/stg-mobile-distribution.sh`
- `tools/deploy/stg-release-tag-bookkeeping.sh`

