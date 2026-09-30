import assert from "node:assert/strict";
import test from "node:test";

import { applyViolationChange } from "./totals.ts";

const person = (over = {}) => ({
  person_id: "p1", person_name: "Bhavya", designation: "", park_label: "Channapatna",
  count: 0, fine_rupees: 0, fine_label: "₹0", pending: 1, closed: 0, leave_days: 0, leave_pending_days: 0, leave_label: "", ...over,
});
const start = { summary: { count: 0, fine_rupees: 0, fine_label: "₹0", people: 0, pending: 1 }, byPerson: [person()] };
const v = (over = {}) => ({ person_id: "p1", person_name: "Bhavya", designation: "", park_label: "Channapatna", fine_rupees: 200, ...over });

test("keeping a waiting one makes it count with HR's fine", () => {
  const out = applyViolationChange(start, v(), "kept", true);
  assert.equal(out.summary.count, 1);
  assert.equal(out.summary.fine_label, "₹200");
  assert.equal(out.summary.pending, 0);
  assert.equal(out.summary.people, 1);
  assert.deepEqual([out.byPerson[0].count, out.byPerson[0].pending, out.byPerson[0].fine_rupees], [1, 0, 200]);
});

test("closing a waiting one never counts; the person stays for their closed column", () => {
  const out = applyViolationChange(start, v({ fine_rupees: 0 }), "closed", true);
  assert.equal(out.summary.count, 0);
  assert.equal(out.summary.pending, 0);
  assert.equal(out.summary.people, 0);
  assert.deepEqual([out.byPerson[0].closed, out.byPerson[0].pending], [1, 0]);
});

test("a person with nothing left drops out, but one with leave stays", () => {
  const recorded = { summary: { count: 1, fine_rupees: 100, fine_label: "₹100", people: 1, pending: 0 }, byPerson: [person({ count: 1, fine_rupees: 100, pending: 0 })] };
  assert.equal(applyViolationChange(recorded, v({ fine_rupees: 100 }), "withdrawn", true).byPerson.length, 0);
  const onLeave = { ...recorded, byPerson: [person({ count: 1, fine_rupees: 100, pending: 0, leave_days: 2, leave_label: "2 days" })] };
  assert.equal(applyViolationChange(onLeave, v({ fine_rupees: 100 }), "withdrawn", true).byPerson.length, 1);
});

test("on a status tab only the waiting count moves", () => {
  const out = applyViolationChange(start, v(), "kept", false);
  assert.equal(out.summary.count, 0);
  assert.equal(out.summary.pending, 0);
});
