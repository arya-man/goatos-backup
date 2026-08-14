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

Use the deployed HTTPS URL supplied by the staging operator. The MCP JSON-RPC
path is `/mcp`:

```text
<STG_EXTERNAL_MCP_URL>/mcp
```

Do not point external clients at the internal MCP Toolbox service
`mesha-mcp-toolbox-stg`. That service is a backend database-tool dependency, not
the public connector contract for Claude, Codex, ChatGPT, or custom GPTs.

## Access Model

External MCP v1 access uses the same Goat OS staging bearer token that the
backend already trusts. The MCP client or local proxy must send:

```text
Authorization: Bearer <GOATOS_STG_USER_TOKEN>
```

The verified email inside that bearer token must match the explicit leadership
allowlist from `docs/ceo-ai/access-policy.md`. The MCP facade verifies the token
and checks the token email before proxying, and the upstream Goat OS API still
validates the bearer token, tenant scope, and CEO/CXO authorization. The
allowlist check is a full email match, case insensitive. It is not a domain
suffix rule.

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

## Claude Desktop / Claude Code Configuration

For clients that support a remote HTTP MCP server with request headers, register
the staging MCP URL and provide the Goat OS bearer token in client-managed
secrets or local user config.

Example shape:

```json
{
  "mcpServers": {
    "mesha-goatos-stg": {
      "type": "http",
      "url": "<STG_EXTERNAL_MCP_URL>/mcp",
      "headers": {
        "Authorization": "Bearer ${GOATOS_STG_USER_TOKEN}"
      }
    }
  }
}
```

If your Claude client expects a command-launched proxy instead of native remote
HTTP MCP, use the proxy recommended by that client and keep the Mesha URL and
token in environment or user-level config, not in the repo:

```json
{
  "mcpServers": {
    "mesha-goatos-stg": {
      "command": "npx",
      "args": ["-y", "<remote-mcp-proxy-package>", "<STG_EXTERNAL_MCP_URL>/mcp"],
      "env": {
        "GOATOS_STG_USER_TOKEN": "<token outside repo>"
      }
    }
  }
}
```

After restart, ask a normal question such as:

```text
Which sheds are overdue for vaccination today?
```

Do not ask Claude to call `mesha_vaccination_due_summary` or any other internal
tool name. Tool selection is part of the MCP/client/runtime contract.

## Codex Configuration

For Codex environments that support remote MCP registration with headers, add
the same staging endpoint to the user-level MCP config and provide the Goat OS
bearer token from local user config or a secret store.

Example shape:

```toml
[mcp_servers.mesha-goatos-stg]
type = "http"
url = "<STG_EXTERNAL_MCP_URL>/mcp"
headers = { Authorization = "Bearer ${GOATOS_STG_USER_TOKEN}" }
```

If the Codex runtime in use only supports command-launched MCP servers, use the
same remote proxy pattern as Claude and keep secrets/tokens outside the Goat OS
repo:

```toml
[mcp_servers.mesha-goatos-stg]
command = "npx"
args = ["-y", "<remote-mcp-proxy-package>", "<STG_EXTERNAL_MCP_URL>/mcp"]
env = { GOATOS_STG_USER_TOKEN = "<token outside repo>" }
```

The Codex user should still type ordinary questions:

```text
Summarize the highest-risk operations exceptions right now.
```

The MCP endpoint handles the tool call path. The user does not paste bearer
tokens, SQL, tenant IDs, or internal tool arguments into chat.

## ChatGPT-Style Custom GPT / App Usage

For a ChatGPT-style custom GPT, app, or connector:

1. Register the external MCP endpoint URL:
   ```text
   <STG_EXTERNAL_MCP_URL>
   ```
2. Configure the client or connector backend to forward the Goat OS bearer
   token.
3. Sign in or provision the connector identity with an allowlisted leadership
   email.
4. Describe the connector to users as "Mesha Goat OS leadership read-only
   operations assistant."
5. Test with one normal operating question before sharing it with other
   allowlisted users.

Do not embed database credentials, long-lived static bearer tokens, tenant IDs,
or raw SQL examples in the GPT/app instructions.

## Troubleshooting

| Symptom | Likely cause | What to check |
| --- | --- | --- |
| Tool call returns `missing_authorization_bearer` | The client/proxy did not send `Authorization` | Put the Goat OS staging token in user-level client config or a secret store |
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
- Run one smoke question through the target client.
- Confirm the answer includes source/freshness metadata and no internal trace.
