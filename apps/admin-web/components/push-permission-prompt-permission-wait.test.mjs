import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Regression: Chrome's QUIET permission UI shows no pop-up, only a bell in the address bar, and
// Notification.requestPermission() stays pending until the person clicks it. A timer around the
// whole enable click turned that wait into "That took too long", so no token was ever minted.
const prompt = readFileSync(new URL("./push-permission-prompt.tsx", import.meta.url), "utf8");
const client = readFileSync(new URL("../lib/web-push.ts", import.meta.url), "utf8");

test("the enable click is not raced against a timer that includes the permission wait", () => {
  const onEnable = prompt.slice(prompt.indexOf("const onEnable"), prompt.indexOf("const onDisable"));
  assert.doesNotMatch(onEnable, /raceControl/);
  assert.match(onEnable, /onAwaitingPermission/);
  assert.match(onEnable, /stepTimeoutMs/);
});

test("requestPermission is awaited outside every bounded step", () => {
  const enable = client.slice(client.indexOf("export async function enableWebPush"), client.indexOf("function isWebPushState"));
  const ask = enable.indexOf("await Notification.requestPermission()");
  assert.ok(ask > 0);
  const line = enable.slice(enable.lastIndexOf("\n", ask), ask);
  assert.doesNotMatch(line, /bounded|withTimeout/);
});

test("the awaiting-permission state tells the person where Chrome put the ask", () => {
  assert.match(prompt, /push\.awaiting_permission/);
});
