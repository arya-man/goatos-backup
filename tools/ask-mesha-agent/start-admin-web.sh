#!/bin/zsh
# Local admin-web (live commit) -> live stg API, with Ask Mesha diverted to the local agent.
cd "$(dirname "$0")/../../apps/admin-web"
export GOATOS_ENV=stg NEXT_PUBLIC_GOATOS_ENV=stg
export GOATOS_API_BASE_URL=https://api.goatos.mesha.sg/
export GOATOS_TENANT_ID=00000000-0000-4000-8000-000000000001
export GOATOS_GOOGLE_SIGN_IN_CLIENT_ID=514832198871-vjnkll058jgr2ee1qkn7aclsuq7017fb.apps.googleusercontent.com
export GOATOS_FIREBASE_WEB_CONFIG="$(gcloud secrets versions access latest --secret goatos-stg-firebase-web-config --project goatos-stg)"
export NEXT_PUBLIC_APP_VERSION=a67be34c0781-local-agent
export CEO_AI_AGENT_URL=http://127.0.0.1:8787   # feature flag: unset => old ceo-ai
if [ "${1:-}" = "prod" ]; then npx next build --webpack && exec npx next start -H localhost -p 3300; fi
exec npx next dev --webpack -H localhost -p 3300
