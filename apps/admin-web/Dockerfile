FROM node:24-alpine AS deps

WORKDIR /repo
ENV NEXT_TELEMETRY_DISABLED=1

COPY apps/admin-web/package.json apps/admin-web/package-lock.json ./apps/admin-web/
COPY packages/api-client ./packages/api-client
RUN --mount=type=cache,target=/root/.npm npm --prefix apps/admin-web ci --no-audit --no-fund

FROM deps AS build

# Build-time configuration for Faro RUM and app metadata.
# These values are baked into the Next.js build at `npm run build`.
ARG NEXT_PUBLIC_FARO_COLLECTOR_URL
ARG NEXT_PUBLIC_GOATOS_ENV
ARG NEXT_PUBLIC_APP_VERSION
ARG NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED

# Set as environment variables so Next.js build can access them.
ENV NEXT_PUBLIC_FARO_COLLECTOR_URL=${NEXT_PUBLIC_FARO_COLLECTOR_URL}
ENV NEXT_PUBLIC_GOATOS_ENV=${NEXT_PUBLIC_GOATOS_ENV}
ENV NEXT_PUBLIC_APP_VERSION=${NEXT_PUBLIC_APP_VERSION}
ENV NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED=${NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED}

COPY apps/admin-web ./apps/admin-web
COPY packages/api-client ./packages/api-client
WORKDIR /repo/apps/admin-web
RUN rm -rf node_modules/@goatos/api-client && \
    mkdir -p node_modules/@goatos && \
    cp -R ../../packages/api-client node_modules/@goatos/api-client
RUN npm run build

FROM node:24-alpine AS runtime

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    PORT=8080

RUN addgroup -S nextjs && adduser -S -G nextjs nextjs

WORKDIR /app
COPY --from=build /repo/apps/admin-web/.next/standalone ./
COPY --from=build /repo/apps/admin-web/.next/static ./.next/static
COPY --from=build /repo/apps/admin-web/.next/static ./apps/admin-web/.next/static
COPY --from=build /repo/apps/admin-web/public ./public
COPY --from=build /repo/apps/admin-web/public ./apps/admin-web/public
RUN mkdir -p /app/.next/cache /app/apps/admin-web/.next/cache && \
    chown -R nextjs:nextjs /app/.next /app/apps/admin-web/.next

USER nextjs
EXPOSE 8080
CMD ["node", "apps/admin-web/server.js"]
