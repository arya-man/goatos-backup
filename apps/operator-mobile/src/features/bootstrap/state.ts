import { evaluateDeviceGate } from "../../../../../packages/device-client/src/index.js";
import type { BootstrapResponse } from "../../shared/api/client.js";

export type BootstrapScreenState =
  | { kind: "loading" }
  | { kind: "denied"; reason: string }
  | { kind: "revoked_device"; reason: string }
  | { kind: "incompatible_app"; minVersion: string }
  | { kind: "ready"; navigation: Array<{ key: string; label: string; href: string }>; taskQueues: BootstrapResponse["task_queue_descriptors"] }
  | { kind: "no_tasks"; navigation: Array<{ key: string; label: string; href: string }> };

export function deriveBootstrapScreen(bootstrap: BootstrapResponse, currentAppVersion: string): BootstrapScreenState {
  const deviceGate = evaluateDeviceGate(bootstrap.device_state);
  if (deviceGate.kind === "revoked") {
    return { kind: "revoked_device", reason: deviceGate.reason };
  }
  if (deviceGate.kind === "registration_required") {
    return { kind: "denied", reason: deviceGate.reason };
  }
  if (compareVersions(currentAppVersion, bootstrap.app_min_supported_version) < 0) {
    return { kind: "incompatible_app", minVersion: bootstrap.app_min_supported_version };
  }
  if (bootstrap.task_queue_descriptors.length === 0) {
    return { kind: "no_tasks", navigation: bootstrap.visible_navigation };
  }
  return {
    kind: "ready",
    navigation: bootstrap.visible_navigation,
    taskQueues: bootstrap.task_queue_descriptors,
  };
}

function compareVersions(left: string, right: string): number {
  const a = left.split(".").map((item) => Number.parseInt(item, 10) || 0);
  const b = right.split(".").map((item) => Number.parseInt(item, 10) || 0);
  for (let i = 0; i < Math.max(a.length, b.length); i += 1) {
    const diff = (a[i] ?? 0) - (b[i] ?? 0);
    if (diff !== 0) return diff;
  }
  return 0;
}
