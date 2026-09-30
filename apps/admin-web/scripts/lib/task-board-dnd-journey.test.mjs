// guard: task-board-touch-dnd -- the pure halves of the /tasks drag journey.
import assert from "node:assert/strict";
import { test } from "node:test";

import { bodyHasField, isOpaqueColor } from "./task-board-dnd-journey.mjs";

test("isOpaqueColor: a transparent drag ghost fails, a paper card passes", () => {
  assert.equal(isOpaqueColor("rgba(0, 0, 0, 0)"), false);
  assert.equal(isOpaqueColor("transparent"), false);
  assert.equal(isOpaqueColor(""), false);
  assert.equal(isOpaqueColor("rgba(28, 37, 46, 0.5)"), false);
  assert.equal(isOpaqueColor("rgb(28, 37, 46)"), true);
  assert.equal(isOpaqueColor("rgba(255, 255, 255, 1)"), true);
  assert.equal(isOpaqueColor("color(srgb 0.1 0.1 0.1 / 0.4)"), false);
});

test("bodyHasField: reads a Next server-action multipart body (fields prefixed _1_)", () => {
  const body = '------B\r\nContent-Disposition: form-data; name="_1_status"\r\n\r\ndone\r\n------B\r\nContent-Disposition: form-data; name="_1_from_status"\r\n\r\nin_progress\r\n------B--\r\n';
  assert.equal(bodyHasField(body, "status", "done"), true);
  assert.equal(bodyHasField(body, "from_status", "in_progress"), true);
  assert.equal(bodyHasField(body, "status", "in_progress"), false, "status must not match from_status");
  assert.equal(bodyHasField("status=open&x=1", "status", "open"), true);
});
