# Zero-downtime STG deploy default progress

## Scope

- Make the normal GoatOS STG backend/web deploy paths default to zero-downtime mode.
- Cover Cloud Build defaults, the Cloud Build release wrapper, and the Slack deploy trigger.
- Preserve the existing post-rollout scaling posture; this change must not intentionally leave duplicate public API/admin revisions serving forever.

## Done

- `cloudbuild.stg.yaml` now defaults `_GOATOS_STG_ZERO_DOWNTIME_DEPLOY` to `true`.
- `tools/deploy/stg-cloudbuild-release.sh` now defaults `GOATOS_STG_ZERO_DOWNTIME_DEPLOY` to `true`.
- Slack-triggered deploys now explicitly pass `_GOATOS_STG_ZERO_DOWNTIME_DEPLOY=true`.
- Added a Slack bot unit guard so both the Slack substitution and Cloud Build default stay enabled.

## Proof

- Focused deploy-order proof passed before landing:
  - `node --test tools/deploy/stg-admin-web-traffic-order.test.mjs`
  - `go test ./...` from `tools/deploy/slack-stg-deploy-bot`

## Pending

- Run the exact `make land-main` landing receipt from this clean worktree.
- Confirm `origin/main` readback after the landing gate pushes.

## State

- Starting base: `origin/main` at `8c42a6e6f7b96f292319577be3d2fa40f9ebaf0b`.
- No staging deployment has been started by this source change yet.
