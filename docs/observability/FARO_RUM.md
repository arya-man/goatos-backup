# Admin-web browser telemetry

The browser's Faro SDK sends to the public Alloy `/collect` endpoint. The
collector URL is a Next.js **build-time** value; absence from Cloud Run runtime
environment does not prove it is absent from the served JavaScript.

## Data paths

- Traces: Faro receiver -> Google Cloud Trace.
- Vitals, exceptions, session and route events: Faro JSON log output ->
  `otelcol.receiver.loki` -> JSON map body -> Cloud Logging
  `projects/goatos-stg/logs/goatos-stg-faro`.
- Seven log-based metrics in `infra/observability/faro-log-metrics.json` provide
  measured LCP/INP/TTFB distributions (milliseconds), CLS (unitless), exceptions,
  route-view events and session-start events. The latter counts starts, not
  distinct sessions active in an arbitrary window. No events is not zero errors
  unless collection itself is known healthy.

Faro trace output alone does **not** export its Web Vitals or exception logs.
Historical events dropped by the previous log-less collector cannot be recovered.
New log-based metrics are not retroactive. Verify source logs and new metric
samples after deployment, then reconcile Grafana with those sources.

## Runtime boundaries

The image pins Alloy1.19.2. Faro listens on8080; Alloy's management listener
is loopback12345 to prevent the two servers competing for Cloud Run's port.
Both dashboard.mesha.sg and stg.dashboard.mesha.sg are allowed CORS origins.
Sourcemap downloading is disabled; payloads are limited to1MiB and the receiver
rate limit is50requests/second with a100request burst. CORS is a browser
restriction, not caller authentication. The workload identity needs Logging
Writer and Cloud Trace Agent permissions; no service-account key is used.

## Verification and release

1. Validate the actual image with `alloy validate
   --feature.community-components.enabled=true /etc/alloy/config.alloy`.
2. In an isolated local receiver test, verify approved and unapproved Origin
   preflights and send representative Faro events through the complete JSON
   adapter. Assert numeric `value_lcp`/`value_cls`, `event_name`, and session data
   reach the output. Synthetic test events are not user-traffic evidence.
3. Use the repository's Cloud Deploy observability mode with an immutable Alloy
   digest and the certified dashboard bundle. The helper creates/verifies the
   log metrics before routing traffic to the new collector.
4. Visit a real admin route in Chrome, confirm `/collect` responses and CORS,
   inspect the new Cloud Logging payload, and confirm the corresponding
   Cloud Monitoring series and Grafana panel. Check multiple page interactions
   for INP; it is not emitted merely because a page loads.

No deployment is complete based only on a receiver202 or healthy process.
