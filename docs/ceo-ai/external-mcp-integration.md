# Mesha Leadership Assistant External MCP Integration

Status: operator/developer setup guide for the external MCP endpoint hosted from
the Goat OS staging environment. This document describes how approved leadership
users connect Claude, Claude Code/Desktop, Codex, or ChatGPT-style custom
GPT/app clients to the Mesha read-only leadership assistant.

Canonical companion docs:

- `docs/ceo-ai/access-policy.md` — authoritative user allowlist and visibility
  policy.
- `docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md` — assistant purpose,
  routing order, and implementation status.
- `docs/ceo-ai/mcp-toolbox-plan.md` — internal MCP Toolbox / database tool plan.
- `docs/runbooks/mcp-toolbox-local.md` — local-only Toolbox runbook.

## What Users Do

Users ask normal questions in the chat app they already use. They do not type
tool names, curl commands, JSON, SQL, or MCP protocol messages.

Good prompts look like:

- How many active animals do we have today?
- Which sheds are overdue for vaccination?
- Show feed blocks by park.
- What changed in counts yesterday?
- Which operational exceptions should leadership look at first?

The MCP client discovers the Mesha tools automatically. The user experience is
plain English in, short sourced answer out.

## Hosted Endpoint

The external MCP endpoint is hosted on Cloud Run in:

```text
Organization: vgoats.com
Project:      goatos-stg
Region:       asia-south1
Service:      Mesha / Goat OS external MCP service
```

Use this staging URL. The MCP JSON-RPC path is `/mcp`:

```text
https://mcp.mesha.sg/mcp
```

Do not point external clients at the internal MCP Toolbox service
`mesha-mcp-toolbox-stg`. That service is a backend database-tool dependency, not
the public connector contract for Claude, Codex, ChatGPT, or custom GPTs.

## Access Model

External MCP access is CEO-friendly OAuth-style login. The CEO does not paste a
token. Claude, Codex, Cursor, and similar clients discover the login flow from
the MCP endpoint, open a Goat OS login page, and then keep the session token in
the client.

Under the hood, the MCP service still uses the same Goat OS staging identity
token that the backend already trusts:

```text
Authorization: Bearer <GOATOS_STG_USER_TOKEN>
```

That token is produced by the login flow, not by the user copying anything from
DevTools or documentation. The verified email inside the token must match the
explicit leadership allowlist from `docs/ceo-ai/access-policy.md`. The MCP
facade verifies the token and checks the token email before proxying, and the
upstream Goat OS API still validates the bearer token, tenant scope, and CEO/CXO
authorization. The allowlist check is a full email match, case insensitive. It
is not a domain suffix rule.

The current authorized leadership cohort is:

```text
ravi@mesha.sg
manohark@mesha.sg
manju@mesha.sg
aryaman@mesha.sg
```

Only this cohort gets access. Operators, managers, public users, and unlisted
accounts are rejected before any Mesha tool runs. Client-side login state, app
display name, or workspace membership is not sufficient. A browser OAuth login
flow can be added in front later, but it must still mint or forward a Goat OS
backend-valid bearer token; it must not bypass the Goat OS API role gate.

## Security Model

The external MCP endpoint is a read-only leadership surface.

- Bearer-token auth, CEO/CXO role, tenant scope, and audit remain enforced by
  the Goat OS API.
- The MCP facade also checks the explicit leadership email allowlist before
  proxying a tool call.
- Tenant scope comes from the authenticated server context, never from user text.
- Users can ask for any leadership-visible Mesha read inside their tenant; they
  cannot perform writes.
- The model never receives Cloud SQL credentials.
- SQL, when used as a fallback, is validated server-side, bounded, read-only,
  tenant-scoped, and audited.
- Every request/tool execution is logged for internal audit and debugging.
- Internal traces, prompts, database credentials, and chain-of-thought are never
  shown to end users.

For approved CEO/CXO users, the policy is broad visibility, not narrow
redaction: all Mesha operating reads are available through governed metrics,
read APIs, curated tools, or validated fallback.

## Internal Toolbox Versus External MCP

There are two different MCP-shaped things. Keep them separate.

| Surface | Audience | Network boundary | Purpose |
| --- | --- | --- | --- |
| Internal MCP Toolbox | Goat OS backend only | Internal/authenticated Cloud Run service-to-service | Curated database tools over `ceo_ai.*` views for the assistant runtime |
| External MCP endpoint | Claude, Codex, ChatGPT-style clients used by approved leaders | Public Cloud Run HTTPS endpoint requiring Goat OS bearer auth plus leadership email allowlist | User-facing connector that accepts normal chat questions and routes through the Mesha assistant security/orchestration layer |

The internal Toolbox is not the product connector. It should not be configured
directly in Claude Desktop, Codex, ChatGPT, or a custom GPT.

The external MCP endpoint is the product connector. It owns MCP protocol
handling, leadership allowlist checks, routing to the Mesha assistant, and safe
exposure of the approved read-only tool catalog. Goat OS API remains the
authority for bearer validation, tenant binding, CEO/CXO role gating, and audit.

## Freshness For New APIs And Tables

The external MCP endpoint does not expose raw new tables automatically. That is
intentional. The safe automatic path is:

```text
new Mesha question
  -> external MCP typed tool when one exists, otherwise ask_goatos fallback
  -> existing covered Goat OS read APIs, Cube metrics, MCP Toolbox tools,
     ceo_ai views, or validated read-only SQL fallback
```

Typed tools take priority over free-text fallback. For example,
`get_vaccination_today` calls the canonical Goat OS
`GET /vaccination/live-tracker` API and returns exact drive-day facts:
scheduled administrations, assigned operators, shed/partition/vaccine progress,
proof videos, scan captures, closed administrations, remaining work, unassigned
scheduled administrations, attention, and verification state. A question like
"What vaccination work is scheduled today and what is the progress?" must use
that typed tool. The generic `ask_goatos` fallback must not be treated as
authoritative for that class because broad assistant fallback can mix overall
dashboard totals with today's drive-day schedule.

Current external typed tool catalog:

| CEO question class | External MCP tool | Canonical source | Grain guard |
| --- | --- | --- | --- |
| Today's vaccination schedule/progress | `get_vaccination_today` | `GET /vaccination/live-tracker` | Administration grain: scheduled administrations, operator assignment, proof/scans, closed/remaining. Do not mix all-history due totals. |
| Cross-module exceptions/action queue | `get_action_center` | `GET /action-center/obligations` | Process-integrity obligation grain. Do not mix with operator schedule totals or verifier verdicts. |
| Proof/evidence backlog | `get_verification_backlog` | `GET /verification/queue` | Verification item grain. Pending verification is not completed work. |
| Feed needed/blocked today | `get_feed_today` | `GET /feed-direction/preview` | Issued feed sheet grain. Blocked/null quantity is a config gap, not zero feed. |
| Procurement source-entry pipeline | `get_procurement_pipeline` | `GET /procurement/source-entry/loads` | Load grain. Keep expected, received, accepted, rejected, and holding distinct. |
| Herd/census counts | `get_counts_summary` | `GET /counts/breakdown` | Aggregate census grain. Keep lifecycle status explicit. |
| Health work/cases | `get_health_work_items` | `GET /app/health/work-items` | Treatment-session grain. Open sick work is not a death event unless health state says so. |
| Weighing campaign progress | `get_weighing_progress` | `GET /weighing/campaigns` | Campaign/shed progress grain. Pending verification weight is not verified weight. |
| Weighing growth/ADG trend | `get_weighing_growth_adg` | `GET /weighing/leadership/growth` | Growth aggregate grain. Use for "are weights improving" and ADG questions. |
| Weighing shed lag/coverage | `get_weighing_shed_weights` | `GET /weighing/shed-weights` | Shed KPI row grain. Use for lagging sheds and latest shed weights. |
| Weighing process gaps | `get_weighing_process_state` | `GET /weighing/process-state` | Calendar/control-tower process grain. Use for overdue, pending proof/review, and process health. |
| Weighing demographics | `get_weighing_weight_demographics` | `GET /weighing/weight-demographics` | Breed/sex/stage demographic grain. Use for group comparisons. |

When a user asks a question in one of these classes, Claude, Codex, or ChatGPT
should call the typed tool directly. `ask_goatos` remains a read-only fallback
for questions not yet covered by a typed external tool.

If a future change adds a new leadership-relevant API, OpenAPI path, table,
view, reporting read, KPI, mobile workflow, admin-web route, or domain event, it
must also update the leadership assistant coverage layer in the same change.
That means one of:

- map it to an existing Mesha read API / Cube metric / wired reader,
- add or update a `ceo_ai.*` reporting view,
- add or update a curated MCP Toolbox tool,
- add a validated read-only SQL fallback/query class, or
- add an external MCP typed tool when an external client should answer the class
  without free-text fallback, or
- add an explicit exclusion row in `docs/ceo-ai/coverage-matrix.md` explaining
  why leadership should not see it.

This is machine-gated. `make leadership-assistant-coverage-guard`, registered in
`tools/ci/guardrail-manifest.json` and wired into `make guardrails` plus normal
`make ci-local`, detects new APIs/tables/surfaces and fails if coverage or a
documented exclusion is missing. Claude and Codex also get the same reminder
through the repo hooks and the `goatos-leadership-assistant` skill.

## Claude Desktop / Claude Code Configuration

For Claude clients with remote custom connector support:

1. Open Claude settings.
2. Go to Connectors / MCP servers.
3. Add a custom connector named `Mesha Goat OS`.
4. Paste this URL:
   ```text
   https://mcp.mesha.sg/mcp
   ```
5. Save. Claude should open the Goat OS login page.
6. Sign in with an approved leadership account.
7. Ask normal Mesha questions.

Config shape for clients that use JSON:

```json
{
  "mcpServers": {
    "mesha-goatos-stg": {
      "type": "http",
      "url": "https://mcp.mesha.sg/mcp"
    }
  }
}
```

If your Claude client expects a command-launched proxy instead of native remote
HTTP MCP, use `mcp-remote`. The proxy will open the Goat OS login flow; no token
goes in the config:

```json
{
  "mcpServers": {
    "mesha-goatos-stg": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://mcp.mesha.sg/mcp"]
    }
  }
}
```

After restart, ask a normal question such as:

```text
What vaccination work is scheduled today and what is the progress?
```

Do not ask Claude to call `mesha_vaccination_due_summary` or any other internal
tool name. Tool selection is part of the MCP/client/runtime contract. The client
should discover and call `get_vaccination_today` for the question above.

## Codex Configuration

For Codex, add the staging endpoint to the user-level MCP config. Codex should
open the browser login during MCP startup/connection through the proxy.

Native remote HTTP shape, where supported:

```toml
[mcp_servers.mesha-goatos-stg]
type = "http"
url = "https://mcp.mesha.sg/mcp"
```

Current Codex Desktop/CLI-compatible proxy shape:

```toml
[mcp_servers.mesha-goatos-stg]
command = "npx"
args = ["-y", "mcp-remote", "https://mcp.mesha.sg/mcp"]
startup_timeout_sec = 30.0
tool_timeout_sec = 120.0
```

The Codex user should still type ordinary questions:

```text
Summarize the highest-risk operations exceptions right now.
```

The MCP endpoint handles the tool call path. The user does not paste bearer
tokens, SQL, tenant IDs, or internal tool arguments into chat.

## ChatGPT-Style Custom GPT / App Usage

For ChatGPT, there are two separate flows.

### Admin publishes the Mesha app

1. Open ChatGPT settings and enable Developer Mode if the workspace requires it.
2. Create a new app/connector named `Mesha Goat OS`.
3. Add the remote MCP endpoint:
   ```text
   https://mcp.mesha.sg/mcp
   ```
4. Let ChatGPT discover OAuth and tools from the endpoint.
5. Click the auth/connect step. ChatGPT should open `Connect Mesha Goat OS`.
6. Sign in with an allowlisted leadership email.
7. Scan/test the tools with one operating question before sharing it:
   ```text
   What vaccination work is scheduled today and what is the progress?
   ```
8. Describe the connector to users as "Mesha Goat OS leadership read-only
   operations assistant."

When tool descriptions change, refresh/rescan the app before approval/sharing so
ChatGPT sees the latest MCP catalog. Treat approval as a publishing step: do not
assume a previously approved app automatically picked up new tools.

### CEO connects the Mesha app

1. Open ChatGPT.
2. Choose the `Mesha Goat OS` app/connector.
3. Click Connect.
4. On the Mesha page, enter the approved Mesha leadership email and password.
5. Ask normal questions. The CEO should not paste tokens, SQL, tenant IDs, or
   endpoint URLs into chat.

Do not embed database credentials, long-lived static bearer tokens, tenant IDs,
or raw SQL examples in the GPT/app instructions.

Current read-boundary reminders for ChatGPT, Claude, and Codex:

- Feed MCP answers planned/issued feed. It does not prove actual feeding was
  completed until the `feed_adherence` source/API ships.
- Inventory reorder thresholds are not configured yet. Do not rank or alert on
  `reorder_flag`; answer that reorder thresholds are not covered/configured.
- Procurement Action Center/Control Tower remains intentionally excluded until
  the top-level read routes are mounted.

## Troubleshooting

| Symptom | Likely cause | What to check |
| --- | --- | --- |
| Client opens login page | Expected first-time connection flow | Sign in with an allowlisted Goat OS staging leadership account |
| Tool call returns `authorization_required` or `missing_authorization_bearer` | The client/proxy did not finish the login flow or did not send `Authorization` | Reconnect the MCP server and complete Goat OS login |
| Tool call returns `actor_email_not_allowed` | The verified bearer-token email is missing, unverified, or not on the explicit allowlist | Confirm the exact token email and compare it to `docs/ceo-ai/access-policy.md` |
| Mesha returns forbidden/unauthorized | Bearer token is invalid, expired, wrong environment, wrong tenant, or lacks CEO/CXO authorization | Refresh the Goat OS staging token and verify the user's assistant authorization |
| Client asks for tool commands or JSON | The GPT/app instructions are overfitted to protocol mechanics | Tell the user to ask plain English operating questions |
| Tool list does not appear | Client cannot reach the Cloud Run URL or does not support the selected remote MCP transport | Verify `<STG_EXTERNAL_MCP_URL>` in a browser and confirm the client's MCP transport support |
| Cloud Run 404/405 | Wrong URL path or internal Toolbox URL used by mistake | Use the external MCP endpoint URL, not `mesha-mcp-toolbox-stg` |
| Answer says data source is unavailable | Downstream Cube, Mesha API, internal Toolbox, or Cloud SQL read path is unhealthy | Check staging service health and assistant audit logs |
| Cross-tenant or write request is refused | Expected guardrail behavior | Rephrase as a read-only question within the authenticated tenant |

## Operator Checklist

Before handing the endpoint to a leadership user:

- Confirm the service is deployed in `goatos-stg`, region `asia-south1`.
- Confirm the endpoint is the external bearer-auth MCP service, not the
  internal Toolbox service.
- Confirm the user's exact token email is on the allowlist.
- Confirm the user has the CEO/CXO assistant authorization.
- Run one first-time connect flow through the target client and confirm the Goat
  OS login page appears.
- Run one smoke question through the target client after login.
- Confirm the answer includes source/freshness metadata and no internal trace.
