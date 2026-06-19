export type DeviceRegistrationInput = {
  app_install_id: string;
  app_version: string;
  os_version: string;
  device_public_key_hash?: string | null;
  push_token_hash?: string | null;
};

export type DeviceGate =
  | { kind: "ready"; device_id?: string }
  | { kind: "registration_required"; reason: string }
  | { kind: "revoked"; reason: string };

export function evaluateDeviceGate(deviceState: { required: boolean; status: string; reason?: string | null; device?: { device_id: string } | null }): DeviceGate {
  if (!deviceState.required) return readyGate(deviceState.device?.device_id);
  if (deviceState.status === "active") return readyGate(deviceState.device?.device_id);
  if (deviceState.status === "revoked") return { kind: "revoked", reason: deviceState.reason ?? "Device was revoked." };
  return { kind: "registration_required", reason: deviceState.reason ?? "Register this Android device before executing tasks." };
}

function readyGate(deviceId?: string): DeviceGate {
  return deviceId ? { kind: "ready", device_id: deviceId } : { kind: "ready" };
}
