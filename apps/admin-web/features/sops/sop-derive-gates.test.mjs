import assert from "node:assert/strict";
import test from "node:test";

import { deriveGates } from "./sop-derive.ts";

// The Preventive Care SOP card read "no proof gates" while its five kinds of work carried eleven
// compulsory videos (2026-09-26): its captures live in form_dsl.pc_care, which the summary never read.
test("a PC Care SOP's compulsory captures show on its library card", () => {
  const formDsl = {
    pc_care: {
      categories: {
        deworming: { proofs: [{ key: "video", kind: "video", required: true }] },
        hoof_trimming: {
          proofs: [
            { key: "before_video", kind: "video", required: true },
            { key: "after_video", kind: "video", required: true },
            { key: "extra_photo", kind: "photo", required: false },
          ],
        },
      },
    },
  };
  const policy = { types: ["video", "photo"], required: false, minimum_count: 0, subject_scope: "goat" };
  assert.deepEqual(deriveGates(formDsl, policy), ["3 compulsory captures"]);
});

test("a SOP with no captures anywhere still has no gates", () => {
  assert.deepEqual(deriveGates({ pc_care: { categories: { deworming: { proofs: [] } } } }, { required: false }), []);
});
