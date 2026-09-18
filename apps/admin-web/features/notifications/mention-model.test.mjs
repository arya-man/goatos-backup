import assert from "node:assert/strict";
import { test } from "node:test";

import {
  activeMentionQuery,
  applyMentionSelection,
  filterMentionCandidates,
  MENTION_QUERY_MAX,
  mentionToken,
  mentionValue,
  moveMentionHighlight,
  pruneMentionSelections,
  selectedMentionUserIds,
} from "./mention-model.ts";

const ROSTER = [
  { user_id: "u-ceo", name: "Ravi Teja", title: "CEO" },
  { user_id: "u-pc", name: "Ramesh Kumar", title: "Preventive Care Director" },
  { user_id: "u-proc", name: "Ramesh Iyer", title: "Procurement Director" },
  { user_id: "u-growth", name: "Divya N", title: "Growth Director" },
];

test("@ triggers only at a word boundary, so an email address is not a people picker", () => {
  assert.deepEqual(activeMentionQuery("@", 1), { start: 0, end: 1, query: "" });
  assert.deepEqual(activeMentionQuery("please @ra", 10), { start: 7, end: 10, query: "ra" });
  assert.deepEqual(activeMentionQuery("(@ra", 4), { start: 1, end: 4, query: "ra" });
  // The whole point: ravi@mesha.sg must not open a picker mid-address.
  assert.equal(activeMentionQuery("ravi@mesha.sg", 13), null);
  assert.equal(activeMentionQuery("no trigger here", 15), null);
});

test("the query ends at the first whitespace: a finished name plus a sentence is not a query", () => {
  // "@Ravi Te" used to keep matching across the space; it now stops at the first whitespace,
  // because "@Manju let us check" was being searched as "Manju let us check" (CEO, 2026-09-18).
  assert.deepEqual(activeMentionQuery("@Ravi", 5), { start: 0, end: 5, query: "Ravi" });
  assert.equal(activeMentionQuery("@Ravi Te", 8), null);
  assert.equal(activeMentionQuery("@Manju let", 10), null);
  assert.equal(activeMentionQuery("@Manju  let us check", 20), null);
  assert.equal(activeMentionQuery("@Ravi Teja said", 15), null);
  assert.equal(activeMentionQuery(`@${"x".repeat(MENTION_QUERY_MAX + 1)}`, MENTION_QUERY_MAX + 2), null);
  assert.equal(activeMentionQuery("@ra\nmore", 8), null);
  assert.equal(activeMentionQuery("@ra\tmore", 8), null);
  // A fresh "@" at a word boundary after the sentence opens a new query.
  assert.deepEqual(activeMentionQuery("@Manju let us ask @Ra", 21), { start: 18, end: 21, query: "Ra" });
});

test("a completed pick inserts the trailing space and the popup does not re-open on it", () => {
  const picked = applyMentionSelection("@Man", 4, ROSTER[0]);
  assert.equal(picked.text, "@Ravi Teja ");
  assert.equal(picked.caret, "@Ravi Teja ".length);
  const completed = [picked.selection.token];
  // Right after the pick: the caret is after the space, so there is no query at all.
  assert.equal(activeMentionQuery(picked.text, picked.caret, completed), null);
  // Typing on: still nothing, on every keystroke.
  for (const typed of [" ", " l", " le", " let", " let us check"]) {
    const text = `${picked.text.trimEnd()}${typed}`;
    assert.equal(activeMentionQuery(text, text.length, completed), null, JSON.stringify(text));
  }
  // The caret placed back inside the finished token does not re-open it either, because the
  // token under the caret is one the reader already picked ...
  assert.equal(activeMentionQuery("@Manju", 6, ["@Manju"]), null);
  // ... but a token that was never picked is a query, and so is a picked one with a letter
  // removed -- that is the reader changing their mind.
  assert.deepEqual(activeMentionQuery("@Manju", 6), { start: 0, end: 6, query: "Manju" });
  assert.deepEqual(activeMentionQuery("@Manj", 5, ["@Manju"]), { start: 0, end: 5, query: "Manj" });
});

test("the caret, not the end of the text, decides which mention is active", () => {
  const text = "@ravi and @div";
  assert.deepEqual(activeMentionQuery(text, 5), { start: 0, end: 5, query: "ravi" });
  assert.deepEqual(activeMentionQuery(text, 14), { start: 10, end: 14, query: "div" });
});

test("name-prefix matches rank above title and substring matches", () => {
  const names = filterMentionCandidates(ROSTER, "ram").map((entry) => entry.user_id);
  assert.deepEqual(names, ["u-pc", "u-proc"]);
  // "director" is a title match for three people and a name match for none.
  const titles = filterMentionCandidates(ROSTER, "procurement").map((entry) => entry.user_id);
  assert.deepEqual(titles, ["u-proc"]);
  assert.deepEqual(filterMentionCandidates(ROSTER, "teja").map((entry) => entry.user_id), ["u-ceo"]);
  assert.deepEqual(filterMentionCandidates(ROSTER, "nobody"), []);
  // An empty query offers the roster, capped.
  assert.equal(filterMentionCandidates(ROSTER, "", 2).length, 2);
  // Duplicate ids and blank ids never reach the popup.
  const dupes = filterMentionCandidates([...ROSTER, ROSTER[0], { user_id: "", name: "Ghost" }], "");
  assert.equal(dupes.filter((entry) => entry.user_id === "u-ceo").length, 1);
  assert.equal(dupes.some((entry) => entry.name === "Ghost"), false);
});

test("picking a person replaces the half-typed query and leaves the caret after the name", () => {
  const applied = applyMentionSelection("please @ra look", 10, ROSTER[1]);
  assert.equal(applied.text, "please @Ramesh Kumar look");
  // The caret lands right after the name; the space that was already there is not doubled.
  assert.equal(applied.caret, "please @Ramesh Kumar".length);
  const atEnd = applyMentionSelection("hi @ra", 6, ROSTER[1]);
  assert.equal(atEnd.text, "hi @Ramesh Kumar ");
  assert.equal(atEnd.caret, atEnd.text.length);
  assert.deepEqual(applied.selection, { user_id: "u-pc", token: "@Ramesh Kumar" });
  assert.equal(mentionToken(ROSTER[0]), "@Ravi Teja");
});

test("two people with the same first name stay two DIFFERENT ids", () => {
  // The reason the backend takes ids and not names: a regex over "@Ramesh" cannot tell these apart.
  const first = applyMentionSelection("@ram", 4, ROSTER[1]);
  const second = applyMentionSelection(`${first.text}and @ram`, first.text.length + 8, ROSTER[2]);
  const ids = selectedMentionUserIds(second.text, [first.selection, second.selection]);
  assert.deepEqual(ids, ["u-pc", "u-proc"]);
});

test("deleting the token stops notifying that person", () => {
  const applied = applyMentionSelection("@ra", 3, ROSTER[0]);
  assert.deepEqual(selectedMentionUserIds(applied.text, [applied.selection]), ["u-ceo"]);
  assert.deepEqual(selectedMentionUserIds("the name is gone", [applied.selection]), []);
  assert.deepEqual(pruneMentionSelections("the name is gone", [applied.selection]), []);
});

test("one person mentioned twice is one notification", () => {
  const selection = { user_id: "u-ceo", token: "@Ravi Teja" };
  const ids = selectedMentionUserIds("@Ravi Teja and @Ravi Teja", [selection, selection]);
  assert.deepEqual(ids, ["u-ceo"]);
  assert.equal(pruneMentionSelections("@Ravi Teja and @Ravi Teja", [selection, selection]).length, 1);
});

test("blank ids never travel", () => {
  assert.deepEqual(selectedMentionUserIds("@Ghost", [{ user_id: "  ", token: "@Ghost" }]), []);
  assert.deepEqual(selectedMentionUserIds("@Ghost", [{ user_id: "u-x", token: "" }]), []);
});

test("the composer emits BOTH halves: the text a person wrote and the ids it notifies", () => {
  const applied = applyMentionSelection("look at this @div", 17, ROSTER[3]);
  assert.deepEqual(mentionValue(applied.text, [applied.selection]), {
    text: "look at this @Divya N ",
    mentionUserIds: ["u-growth"],
  });
});

test("arrow keys wrap at both ends and an empty list stays put", () => {
  assert.equal(moveMentionHighlight(0, 3, 1), 1);
  assert.equal(moveMentionHighlight(2, 3, 1), 0);
  assert.equal(moveMentionHighlight(0, 3, -1), 2);
  assert.equal(moveMentionHighlight(0, 0, 1), 0);
});
