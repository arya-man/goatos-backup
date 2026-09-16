# Budget Slack delivery

The budget Pub/Sub stream sends periodic status, not only threshold transitions.
`cost-alert-bridge` stores one small JSON object per billing account, budget ID
(display-name fallback for direct payloads), and billing interval. State lives in
the dedicated `GOATOS_COST_ALERT_STATE_BUCKET`, not the proof/media bucket.

A new forecast or actual threshold is delivered immediately. Updates at an
already announced threshold are acknowledged without Slack traffic until 24 hours
have passed since the last successful budget notification. The next eligible
update then delivers the reminder; this is not a fixed-time scheduler. Pub/Sub `publishTime` determines freshness: duplicate or older publications are
acknowledged without posting, even if their threshold is higher. The latest
publication is recorded even when a same-day or below-threshold update is
suppressed. A fresh lower forecast still qualifies for a reminder after 24 hours.
Historical threshold maxima determine immediate first-time escalations only;
they do not gate reminders. Direct/legacy payloads without publish time use the
arrival/reminder clock and cannot provide source-order replay suppression. A new billing
interval or budget has independent state. Updates without a crossed threshold
are acknowledged without posting. Monitoring and billing-anomaly paths are
unchanged.

GCS generation preconditions serialize competing instances. A two-minute lease
permits crash recovery. Storage failures and active leases return HTTP 503 for
Pub/Sub retry. Slack failures release the claim without marking it sent.
`cost_alert_budget_processed` records `sent=true/false`; delivery failures use
`cost_alert_budget_delivery_failed`. A missing bucket environment variable fails
startup rather than reverting to noisy, in-memory suppression.

Slack and GCS cannot commit atomically: if Slack accepts a message but its response
is lost, or the state commit fails after posting, a retry may duplicate that
message after lease recovery. This deliberately favors delivering a warning over
silently losing it. It does not promise exactly-once Slack delivery.

Authentication fails closed when credentials are absent. Budget Pub/Sub and
billing-anomaly requests support Google-signed OIDC for the configured audience
and allowed service-account emails, including verified email and Google issuer
checks. Monitoring retains shared-token authentication. Terraform supplies the
matching audience on the Pub/Sub token and Cloud Run service; the budget push
URL has no query token. Signature, expiry and audience validation use Google
`idtoken.Validate`. Tests exercise it with locally signed JWTs and a local key
response, without calling Google or Slack.

Provision the dedicated bucket and bucket-scoped object-user IAM, then deploy the
bridge with the bucket environment variable. Terraform defines these dependencies.
Do not deploy the new binary without that configuration. Never apply the dirty
workspace's unrelated infrastructure as part of this fix. Promotion still requires
the repository's local CI receipt, followed by live revision/config readback and
notification delivery verification. Do not send synthetic messages into Slack
without maintainer authorization.

Focused proof: `cd backend && go test -race ./cmd/cost-alert-bridge`.
The package is included in the ordinary backend `go test ./...` CI job.
Runtime generation fencing plus handler replay/concurrency/failure tests are the
recurrence control; a literal source grep would not establish those properties.
