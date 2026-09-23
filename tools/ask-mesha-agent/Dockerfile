# Ask Mesha coding agent (Claude Agent SDK) for Cloud Run: goatos-ask-mesha-stg.
# Build context = repo root at the deployed SHA:
#   docker build -f tools/ask-mesha-agent/Dockerfile .
# Ignore rules: tools/ask-mesha-agent/Dockerfile.dockerignore (BuildKit per-Dockerfile
# ignore file; it replaces the root .dockerignore for THIS image only).
FROM node:24-slim AS deps
WORKDIR /app
COPY tools/ask-mesha-agent/package.json tools/ask-mesha-agent/package-lock.json ./
# npm ci pulls the glibc linux-x64 native `claude` binary as an optional dep.
RUN npm ci --omit=dev --no-audit --no-fund \
 && test -d node_modules/@anthropic-ai/claude-agent-sdk-linux-x64 \
 || (echo "missing @anthropic-ai/claude-agent-sdk-linux-x64 (build with --platform linux/amd64)" >&2; exit 1)

FROM node:24-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-client ca-certificates git ripgrep \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --home-dir /home/agent --uid 10001 --shell /usr/sbin/nologin agent
ARG GIT_SHA=unknown
# Read-only repo snapshot: owned by root, no write bits, agent can only read.
COPY --chown=root:root . /repo
RUN chmod -R a-w,a+rX /repo \
 && rm -rf /repo/tools/ask-mesha-agent/node_modules
WORKDIR /app
COPY --from=deps --chown=root:root /app/node_modules ./node_modules
COPY --chown=root:root tools/ask-mesha-agent/ ./
COPY --chown=root:root tools/ask-mesha-agent/deploy/entrypoint.sh /usr/local/bin/ask-mesha-entrypoint
RUN rm -rf /app/deploy/*.md && chmod 0755 /usr/local/bin/ask-mesha-entrypoint
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
