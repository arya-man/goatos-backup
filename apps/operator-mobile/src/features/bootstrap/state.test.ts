import assert from "node:assert/strict";
import { test } from "node:test";

import { deriveBootstrapScreen } from "./state.js";
import type { BootstrapResponse } from "../../shared/api/client.js";

const now = "2026-06-19T00:00:00Z";

function baseBootstrap(overrides: Partial<BootstrapResponse> = {}): BootstrapResponse {
  return {
    actor: {
      actor_id: "00000000-0000-4000-8000-000000000011",
      tenant_id: "00000000-0000-4000-8000-000000000001",
    },
    operator_profile: {
      operator_id: "00000000-0000-4000-8000-000000000021",
      user_id: "00000000-0000-4000-8000-000000000011",
      display_code: "OP-001",
      display_name: "Operator One",
      status: "active",
      primary_role_hint: "operator",
      primary_location_id: null,
      primary_location: null,
      grant_count: 1,
      capability_count: 1,
      active_device_count: 1,
      metadata: {},
      row_version: 1,
      created_at: now,
      updated_at: now,
    },
    roles_and_scopes: [],
    capabilities: [],
    device_state: {
      required: true,
      device: {
        device_id: "00000000-0000-4000-8000-000000000031",
        operator_id: "00000000-0000-4000-8000-000000000021",
        platform: "android",
        app_install_id: "install-1",
        device_public_key_hash: null,
        push_token_hash: null,
        app_version: "1.2.0",
        os_version: "15",
        status: "active",
        last_seen_at: now,
        registered_at: now,
        revoked_at: null,
        metadata: {},
        row_version: 1,
      },
      status: "active",
      reason: null,
    },
    app_min_supported_version: "1.0.0",
    feature_flags: {},
    visible_navigation: [{ key: "tasks", label: "Tasks", href: "/tasks" }],
    task_queue_descriptors: [{ key: "assigned", label: "Assigned", required_capabilities: ["task.execute"] }],
    pinned_sop_versions: [],
    supported_field_types: ["text", "goat_lookup", "video_proof"],
    supported_rule_operators: ["equals"],
    supported_proof_actions: ["video_required"],
    option_source_descriptors: [],
    sync_policy: {
      offline_draft_ttl_hours: 72,
      heartbeat_interval_seconds: 300,
      max_retry_backoff_seconds: 3600,
    },
    server_time: now,
    trace_id: "trace-bootstrap",
    ...overrides,
  };
}

test("deriveBootstrapScreen returns ready when device, app, and queues are valid", () => {
  const state = deriveBootstrapScreen(baseBootstrap(), "1.2.0");

  assert.equal(state.kind, "ready");
  assert.equal(state.kind === "ready" ? state.taskQueues[0]?.key : undefined, "assigned");
});

test("deriveBootstrapScreen blocks revoked devices before app version checks", () => {
  const state = deriveBootstrapScreen(
    baseBootstrap({
      app_min_supported_version: "99.0.0",
      device_state: {
        required: true,
        device: null,
        status: "revoked",
        reason: "Lost phone.",
      },
    }),
    "1.0.0",
  );

  assert.deepEqual(state, { kind: "revoked_device", reason: "Lost phone." });
});

test("deriveBootstrapScreen blocks unsupported app versions", () => {
  const state = deriveBootstrapScreen(baseBootstrap({ app_min_supported_version: "1.3.0" }), "1.2.9");

  assert.deepEqual(state, { kind: "incompatible_app", minVersion: "1.3.0" });
});

test("deriveBootstrapScreen keeps navigation visible when no queues are assigned", () => {
  const state = deriveBootstrapScreen(baseBootstrap({ task_queue_descriptors: [] }), "1.2.0");

  assert.equal(state.kind, "no_tasks");
  assert.equal(state.kind === "no_tasks" ? state.navigation[0]?.href : undefined, "/tasks");
});
