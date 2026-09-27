// guard: shell-settings-cookie. A corrupt `mesha.shell.settings` cookie (the template detectSettings
// JSON.parses it unguarded) falls back to the defaults and is cleared; the legacy rail cookie carries
// a collapsed rail over once and is cleared.
import assert from "node:assert/strict";
import test from "node:test";

import { resolveShellSettings, LEGACY_NAV_RAIL_COOKIE } from "./shell-settings.ts";
const defaultSettings = { mode: "dark", navLayout: "vertical", compactLayout: false };
const SETTINGS_STORAGE_KEY = "mesha.shell.settings";
const opts = { defaultSettings, settingsKey: SETTINGS_STORAGE_KEY };

test("a corrupt settings cookie falls back to defaults and is cleared", async () => {
  const detect = async () => JSON.parse("{not json");
  const out = await resolveShellSettings(detect, true, undefined, opts);
  assert.deepEqual(out.settings, defaultSettings);
  assert.deepEqual(out.clearCookies, [SETTINGS_STORAGE_KEY]);
});

test("a valid cookie passes through untouched", async () => {
  const mini = { ...defaultSettings, navLayout: "mini" };
  const out = await resolveShellSettings(async () => mini, true, undefined, opts);
  assert.equal(out.settings.navLayout, "mini");
  assert.deepEqual(out.clearCookies, []);
});

test("the legacy rail cookie keeps a collapsed rail once, then is cleared", async () => {
  const out = await resolveShellSettings(async () => defaultSettings, false, "1", opts);
  assert.equal(out.settings.navLayout, "mini");
  assert.deepEqual(out.clearCookies, [LEGACY_NAV_RAIL_COOKIE]);
  const already = await resolveShellSettings(async () => ({ ...defaultSettings, navLayout: "vertical" }), true, "1", opts);
  assert.equal(already.settings.navLayout, "vertical", "an existing settings cookie wins");
});
