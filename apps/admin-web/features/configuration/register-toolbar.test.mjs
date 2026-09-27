import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: config-register-toolbar-menu (TR1-#28). The template user list's CardHeader has a title and
// nothing else; secondary list actions sit in the toolbar ⋮ popover. /configuration/items had
// outlined "Sheet" / "Setup workbook" / "Edit list" buttons in the register CardHeader.
test("register secondary actions live in the toolbar ⋮, not the CardHeader", () => {
  const page = readFileSync(new URL("./items-page.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(page, /data-testid="(sheet|workbook)-open"/);
  const toolbar = page.slice(page.indexOf("<RegisterToolbar"), page.indexOf("filters={filterColumns"));
  for (const key of ["edit-list", "sheet", "workbook"]) assert.match(toolbar, new RegExp(`key: "${key}"`), key);
  const client = readFileSync(new URL("./register-toolbar.tsx", import.meta.url), "utf8");
  assert.match(client, /^"use client";/);
  assert.match(client, /pushLocalOverlayUrl\(link\.href\)/, "menu items open the same local drawers as the old links");
});
