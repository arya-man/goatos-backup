// SHIFTING SOP (2026-09-16): the approver sees the raise's SOP capture form before deciding --
// answers in farm words, captures as click-to-open tiles, and the older-app note -- from the
// BACKEND-OWNED `capture` on the list item, never recomposed from the summary payload. Source-shape
// tests, like every other test in this folder.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawer = readFileSync(new URL("./approvals-drawer.tsx", import.meta.url), "utf8");
const server = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");

test("the drawer renders item.capture rows, media and the missing note verbatim", () => {
  assert.match(drawer, /item\.capture \? <CaptureSection capture=\{item\.capture\} \/> : null/);
  assert.match(drawer, /capture\.rows/);
  assert.match(drawer, /capture\.media\.map/);
  assert.match(drawer, /capture\.missing_note/);
  assert.match(drawer, /capture\.version_label/);
});

test("a capture opens through the server-side proof URL bridge, never a hydrated URL", () => {
  assert.match(drawer, /resolveApprovalProofMediaUrl\(proofId\)/);
  assert.doesNotMatch(drawer, /download_url|signed_url/);
});

test("the shared CountsApprovalCapture shape is typed on the approval item", () => {
  assert.match(server, /capture\?: AdminWebApprovalCapture;/);
  assert.match(server, /rows: Array<\{ label: string; value: string; group\?: string \}>;/);
  assert.match(server, /media: Array<\{ proof_id: string; label: string; kind: string \}>;/);
  assert.match(server, /missing_note\?: string;/);
});
