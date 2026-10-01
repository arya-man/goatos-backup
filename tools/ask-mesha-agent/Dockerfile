# Ask Mesha coding agent (Gemini on Vertex AI) for Cloud Run: goatos-ask-mesha-stg.
# Build context = repo root at the deployed SHA:
#   docker build -f tools/ask-mesha-agent/Dockerfile .
# Ignore rules: tools/ask-mesha-agent/Dockerfile.dockerignore (BuildKit per-Dockerfile
# ignore file; it replaces the root .dockerignore for THIS image only).
FROM node:24-slim AS deps
WORKDIR /app
COPY tools/ask-mesha-agent/package.json tools/ask-mesha-agent/package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund

FROM node:24-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-client ca-certificates git ripgrep \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --home-dir /home/agent --uid 10001 --shell /usr/sbin/nologin agent
ARG GIT_SHA=unknown
# Read-only repo snapshot: owned by root, no write bits, agent can only read.
COPY --chown=root:root . /repo
# Second net after the .dockerignore list: delete anything whose name or content looks like a
# credential (new secret kinds included), and symlinks that escape /repo. Every build route
# (Cloud Build STG, deploy-stg.sh, a local docker build by Claude/Codex) runs this step.
RUN node /repo/tools/ask-mesha-agent/deploy/scrub-snapshot.mjs /repo \
 && chmod -R a-w,a+rX /repo \
 && rm -rf /repo/tools/ask-mesha-agent/node_modules
WORKDIR /app
COPY --from=deps --chown=root:root /app/node_modules ./node_modules
COPY --chown=root:root tools/ask-mesha-agent/ ./
COPY --chown=root:root tools/ask-mesha-agent/deploy/entrypoint.sh /usr/local/bin/ask-mesha-entrypoint
# Runtime = server.mjs gemini.mjs code-tools.mjs instructions.mjs lib.mjs ... sql/*.sql package*.json;
# data-map-core.md is read from /repo (GOATOS_REPO). Drop tests, bench, dev tooling.
RUN rm -rf /app/deploy /app/test /app/*.test.mjs /app/bench.mjs /app/schedule \
      /app/gen-data-map.mjs /app/refresh-data-map.sh /app/start-admin-web.sh /app/README.md \
 && for f in server.mjs gemini.mjs code-tools.mjs instructions.mjs checker.mjs lib.mjs events.mjs store.mjs uploads.mjs watch.mjs sql/001_init.sql sql/002_events.sql; do \
      test -f "/app/$f" || { echo "missing runtime file $f" >&2; exit 1; }; done \
 && test -f /repo/tools/ask-mesha-agent/data-map-core.md \
 && chmod 0755 /usr/local/bin/ask-mesha-entrypoint
ENV NODE_ENV=production \
    HOST=0.0.0.0 \
    PORT=8080 \
    HOME=/home/agent \
    GOATOS_REPO=/repo \
    GOATOS_BASE_SHA=${GIT_SHA} \
    ASK_MESHA_READONLY=1 \
    ASK_MESHA_STATE_DIR=/home/agent/.ask-mesha-agent
USER agent
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/ask-mesha-entrypoint"]
CMD ["node", "/app/server.mjs"]
