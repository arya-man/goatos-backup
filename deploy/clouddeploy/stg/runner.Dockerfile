FROM node:22-bookworm-slim AS node-runtime
FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:slim

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl python3 \
  && rm -rf /var/lib/apt/lists/*

COPY tools/deploy/stg-clouddeploy-task.sh /usr/local/bin/goatos-stg-clouddeploy-task
COPY tools/deploy/stg-analytics-events-routing.sh /usr/local/bin/goatos-stg-analytics-events-routing

COPY tools/deploy/stg-grafana-sso.py /usr/local/bin/goatos-stg-grafana-sso.py

COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node
COPY tools/deploy/smoke-stg-grafana-dashboards.mjs /opt/goatos/tools/deploy/smoke-stg-grafana-dashboards.mjs
COPY tools/deploy/stg-observability.py /usr/local/bin/goatos-stg-observability.py
COPY infra/grafana /opt/goatos/infra/grafana
COPY infra/observability/faro-log-metrics.json /opt/goatos/infra/observability/faro-log-metrics.json
COPY infra/observability/firebase-initial-export.json /opt/goatos/infra/observability/firebase-initial-export.json

# Minimal source archives may not preserve executable mode bits.
RUN chmod 0755 /usr/local/bin/goatos-stg-clouddeploy-task \
  /usr/local/bin/goatos-stg-analytics-events-routing

ENTRYPOINT ["/usr/local/bin/goatos-stg-clouddeploy-task"]
