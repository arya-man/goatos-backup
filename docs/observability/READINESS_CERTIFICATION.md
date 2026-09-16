# Deployment validity and data readiness

A successful Cloud Deploy rollout validates provisioning, authentication,
all committed datasource queries, and the existing representative/feature data
checks. Its smoke uses `--query-validity-only`. SQL errors, permission errors,
missing datasource results, or errors on any individual query reference fail.
A valid query returning no samples is reported as missing data, never zero.
**This deployment result is not full-data certification.**

Fresh Faro log-based metrics do not backfill historical logs. After an authorized
deployment, open the real staging product in an authenticated browser and exercise
real routes. Verify the collector accepts actual traffic, Cloud Logging receives
it under the expected app/environment, and metric samples propagate. Do not send
synthetic production-labelled events merely to turn the gate green.

Then run the separate data gate from the certified source checkout:

```sh
node tools/deploy/smoke-stg-grafana-dashboards.mjs \
  --url https://grafana.mesha.sg --no-proxy --require-all-panel-data \
  --firebase-initial-export-receipt infra/observability/firebase-initial-export.json
```

The receipt can classify only the exact Firebase crash/session/network panels as
initial-provider-pending, only while verified export datasets are still empty and
within48hours of their immutable creation times. Query failures never qualify.
The output must retain provider-pending status; it does not certify those sources.
All other required data, including Faro, must be present. Expired receipts or
new export tables remove this exception; do not renew the clock.

Normal deployment verifies this same receipt before executing the seven-day
rollup. Only providers proven to be in the bounded initial-export wait are
omitted through per-execution empty table flags; the persistent job environment
remains configured. The receipt must cover the exact configured table IDs.
Expired receipts and nonempty datasets provide no omission, and metadata,
permission, source-identity, and query errors remain failures. The deployment
still reports full-data certification pending.

Every scheduled or manual rollup refreshes all first-party dates before querying
Firebase, so an external provider failure cannot strand newer activity summaries.
Such a failure still writes a failed audit and is retried by the existing worker.
When exports arrive, verify live app/table schemas and project/location, execute
the bounded seven-day backfill, and rerun data certification. Keep the readiness
gap open until the real query and denominator evidence exists.

Source changes to packaged scripts/dashboards/receipt require a fresh runner build
and both digest pins before any future rollout. Artifact-only builds may run when explicitly authorized for source certification.
The active hold still prohibits service rollouts and main landing; a successful
artifact build does not lift that hold.

## Deployment runner source receipt

Cloud Deploy executes scripts embedded in its pinned runner image. After editing
any runner `COPY` input, rebuild with `cloudbuild.stg-runner.yaml`. The build
compares every installed script/dashboard/config byte against its source and
exports `runner-receipt.json` alongside `runner-image.env`. Copy that receipt to
`deploy/clouddeploy/stg/runner-receipt.json` and update both runner image pins.
`make grafana-durability-guard` rejects changed, added or removed packaged files,
Dockerfile changes, and pins that disagree with this receipt. A source-only test
pass never certifies a previously built image.
