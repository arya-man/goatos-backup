# STG Chatbot / CEO AI Wiring

STG deploy does not count as AI-chatbot-ready unless this checklist is verified.

## Runtime Shape

Browser/admin-web must never call Vertex, Cube, MCP Toolbox, or Postgres directly.

Required path:

admin-web
-> STG backend `/ceo-ai/*`
-> backend leadership/RBAC gate
-> Vertex/Gemini planner if enabled, otherwise deterministic fallback planner
-> Cube / Mesha read API / MCP Toolbox / read-only SQL
-> sourced answer

## Required STG Config

Backend service `goatos-api-stg` must have:

- `MESHA_AI_PROVIDER`
- `MESHA_VERTEX_PROJECT=goatos-stg`
- `MESHA_VERTEX_LOCATION=asia-south1`
- `MESHA_VERTEX_MODEL=gemini-2.5-flash` or the current approved Gemini model
- `MESHA_CUBE_URL` if Cube is deployed
- `MESHA_MCP_TOOLBOX_URL` if MCP Toolbox is deployed
- `MESHA_MCP_TOOLSET=mesha_ceo_toolset`
- access to Secret Manager secret `mesha-cube-api-secret`
- access to Secret Manager secret `mesha-ceo-readonly-db-url`
- access to Secret Manager secret `mesha-cube-readonly-db-url`

## Required Google Cloud State

In project `goatos-stg`:

- Vertex AI API enabled
- backend runtime service account can call Vertex AI
- Secret Manager entries exist:
  - `mesha-cube-api-secret`
  - `mesha-ceo-readonly-db-url`
  - `mesha-cube-readonly-db-url`
  - `mesha-mcp-toolset`
- Cube service `mesha-cube-stg` is either deployed and healthy, or explicitly marked not deployed
- MCP Toolbox service `mesha-mcp-toolbox-stg` is either deployed and healthy, or explicitly marked not deployed
- read-only DB roles exist if Cube/Toolbox are expected to work:
  - `mesha_ceo_readonly`
  - `mesha_cube_readonly`

## Required Admin-Web State

- admin-web STG points to STG backend
- CEO AI bubble is visible only for leadership users
- non-leadership users do not get assistant access
- admin-web proxies `/api/ceo-ai/*` to backend `/ceo-ai/*`

## Smoke Tests

After STG deploy, verify:

1. Authenticated leadership user:
   - `GET /ceo-ai/starters` returns 200
   - `POST /ceo-ai/ask` returns a sourced answer

2. Non-leadership user:
   - `GET /ceo-ai/starters` returns 403 or assistant unavailable
   - `POST /ceo-ai/ask` is denied

3. Logs must show which route was used:
   - Vertex/Gemini planner
   - fallback keyword planner
   - Cube
   - Mesha read API
   - MCP Toolbox
   - read-only SQL fallback

4. If Vertex/Cube/Toolbox is missing:
   - report exact missing config/service/permission
   - do not claim chatbot fully wired
   - fallback planner may work, but that is not full Vertex/Cube readiness

## Not Ready Conditions

Chatbot is NOT fully wired if any of these are true:

- Vertex AI API disabled
- backend service account lacks Vertex permission
- `MESHA_VERTEX_*` env is missing or points outside `goatos-stg`
- admin-web points to old backend
- Cube URL points local/dev/prod
- Cube service missing when Cube metrics are expected
- MCP Toolbox missing when MCP tools are expected
- readonly DB secrets still contain placeholders while staging Cube/Toolbox is expected
- `/ceo-ai/ask` works only through fallback but Vertex was expected
