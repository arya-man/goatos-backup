import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./push-permission-prompt-lazy.tsx", import.meta.url), "utf8");

function functionBody(name) {
  const marker = `export function ${name}`;
  const start = source.indexOf(marker);
  assert.notEqual(start, -1, `${name} must exist`);
  const next = source.indexOf("\nexport function ", start + marker.length);
  return source.slice(start, next === -1 ? source.length : next);
}

test("push receipt sync does not import the web-push client before notification permission is granted", () => {
  const helper = source.slice(source.indexOf("function hasGrantedNotificationPermission"));
  assert.match(helper, /typeof Notification !== "undefined" && Notification\.permission === "granted"/);

  const body = functionBody("PushReceiptSync");
  const firstGate = body.indexOf("if (!hasGrantedNotificationPermission()) return;");
  const listenerImport = body.indexOf('import("@/lib/web-push")');
  assert.ok(firstGate > -1, "receipt listener must be gated by browser permission");
  assert.ok(listenerImport > -1, "receipt listener must still load web-push for granted browsers");
  assert.ok(firstGate < listenerImport, "permission gate must run before importing web-push");

  const openGate = body.indexOf("if (!hasGrantedNotificationPermission())", listenerImport + 1);
  const openedEventImport = body.lastIndexOf('import("@/lib/web-push")');
  assert.ok(openGate > -1, "push_open receipt must be gated by browser permission");
  assert.ok(openGate < openedEventImport, "push_open gate must run before importing web-push");
});
