FROM node:22-alpine AS deps

WORKDIR /repo
ENV NEXT_TELEMETRY_DISABLED=1

COPY apps/admin-web/package.json apps/admin-web/package-lock.json ./apps/admin-web/
COPY packages/api-client ./packages/api-client
RUN --mount=type=cache,target=/root/.npm npm --prefix apps/admin-web ci --no-audit --no-fund

FROM deps AS build

COPY apps/admin-web ./apps/admin-web
COPY packages/api-client ./packages/api-client
WORKDIR /repo/apps/admin-web
RUN rm -rf node_modules/@goatos/api-client && \
    mkdir -p node_modules/@goatos && \
    cp -R ../../packages/api-client node_modules/@goatos/api-client
RUN npm run build

FROM node:22-alpine AS runtime

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
CMD ["node", "server.js"]
