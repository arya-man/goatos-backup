/**
 * The @-PICKER's logic, as pure functions with NO imports but one: parsing the query at the
 * caret, ranking candidates, inserting a chosen person, and moving the highlight.
 *
 * Everything here is reached only from `mention-picker.tsx`, which is fetched on first use, so
 * none of it sits in the shared client chunk of every route. Its sibling `mention-value.ts`
 * holds the other half -- which accepted mentions are still written in the text -- because the
 * EAGER, server-rendered textarea and its hidden mentions input need that to keep submitting
 * without JavaScript. Both halves are re-exported from here, so a reader (and the test) can go
 * on treating this file as the composer's rulebook.
 *
 * Import-free apart from that sibling so `mention-model.test.mjs` runs it under `node --test`
 * without the `@/` alias, and because these are the rules that decide WHO gets notified. The
 * composer emits explicit `user_id`s alongside the text; nothing downstream re-derives a person
 * from the prose, because display names collide (two Rameshes is not a hypothetical on a farm
 * roster) and a regex over "@Ramesh" would notify the wrong one or both.
 */

import {
  MENTION_TRIGGER,
  type MentionCandidate,
  type MentionSelection,
} from "./mention-value.ts";

export {
  MENTION_TRIGGER,
  mentionValue,
  pruneMentionSelections,
  selectedMentionUserIds,
  type MentionCandidate,
  type MentionSelection,
  type MentionValue,
} from "./mention-value.ts";

/**
 * Key names used by the popup. They live here rather than inline in the .tsx because
 * `scripts/check-ui-contract-literals.mjs` reads every quoted capitalised string in a .tsx as
 * visible copy that should have come from the page contract; a KeyboardEvent key name is neither.
 */
export const MENTION_KEYS = {
  down: "ArrowDown",
  up: "ArrowUp",
  enter: "Enter",
  tab: "Tab",
  escape: "Escape",
} as const;

/** How many rows the popup offers at once. A phone shows about five without scrolling. */
export const MENTION_RESULT_LIMIT = 6;

/**
 * How long a half-typed name may get before the popup gives up. A "@" followed by a paragraph is
 * someone writing an email address or a sentence, not picking a person.
 */
export const MENTION_QUERY_MAX = 40;

export type ActiveMention = {
  /** Index of the "@" in the text. */
  start: number;
  /** Index just past the query, i.e. the caret. */
  end: number;
  /** What has been typed after the "@", which may be empty right after the trigger. */
  query: string;
};

function isBoundary(character: string | undefined): boolean {
  return character === undefined || /[\s(,;:[\]]/.test(character);
}

/**
 * The mention being typed at the caret, or null.
 *
 * A "@" only triggers at the start of the text or after whitespace/punctuation, so
 * "ravi@mesha.sg" never opens a people picker mid-address. The query may hold ONE space, because
 * rosters are full names and "@Ravi Te" has to keep matching; a second space ends it.
 */
export function activeMentionQuery(text: string, caret: number): ActiveMention | null {
  const position = Math.max(0, Math.min(caret, text.length));
  const before = text.slice(0, position);
  const at = before.lastIndexOf(MENTION_TRIGGER);
  if (at < 0) return null;
  if (!isBoundary(text[at - 1])) return null;
  const query = before.slice(at + 1);
  if (query.length > MENTION_QUERY_MAX) return null;
  if (/[\n\r\t@]/.test(query)) return null;
  if ((query.match(/ /g) ?? []).length > 1) return null;
  return { start: at, end: position, query };
}

/**
 * The rows the popup shows for a query.
 *
 * Name-prefix matches come first, then title-prefix, then anything containing the query, so
 * typing the first letters of a name puts that person on top instead of ranking by roster order.
 * Matching is accent-blind-free on purpose: it is a plain case-insensitive compare, the same one
 * the reader can predict.
 */
export function filterMentionCandidates(
  candidates: readonly MentionCandidate[],
  query: string,
  limit: number = MENTION_RESULT_LIMIT,
): MentionCandidate[] {
  const needle = query.trim().toLowerCase();
  const seen = new Set<string>();
  const unique = candidates.filter((candidate) => {
    const id = (candidate.user_id ?? "").trim();
    if (!id || seen.has(id)) return false;
    seen.add(id);
    return true;
  });
  if (!needle) return unique.slice(0, Math.max(0, limit));
  const ranked = unique
    .map((candidate) => {
      const name = (candidate.name ?? "").toLowerCase();
      const title = (candidate.title ?? "").toLowerCase();
      if (name.startsWith(needle)) return { candidate, rank: 0 };
      if (title.startsWith(needle)) return { candidate, rank: 1 };
      if (name.includes(needle) || title.includes(needle)) return { candidate, rank: 2 };
      return { candidate, rank: -1 };
    })
    .filter((entry) => entry.rank >= 0);
  ranked.sort((left, right) => (left.rank === right.rank ? 0 : left.rank - right.rank));
  return ranked.slice(0, Math.max(0, limit)).map((entry) => entry.candidate);
}

/** The token a candidate writes into the text. */
export function mentionToken(candidate: MentionCandidate): string {
  return `${MENTION_TRIGGER}${(candidate.name ?? "").trim()}`;
}

/**
 * Replaces the half-typed mention at the caret with the chosen person, and reports where the
 * caret must land afterwards.
 *
 * A trailing space is added so the next word is not glued to the name, and so the finished token
 * has a boundary on both sides for `selectedMentionUserIds` to find.
 */
export function applyMentionSelection(
  text: string,
  caret: number,
  candidate: MentionCandidate,
): { text: string; caret: number; selection: MentionSelection } {
  const active = activeMentionQuery(text, caret);
  const token = mentionToken(candidate);
  const start = active ? active.start : Math.max(0, Math.min(caret, text.length));
  const end = active ? active.end : start;
  const rest = text.slice(end);
  // A trailing space so the next word is not glued to the name -- but only when there is not one
  // already, or picking mid-sentence would leave a double space where the reader typed one.
  const pad = /^[ \t]/.test(rest) ? "" : " ";
  const next = `${text.slice(0, start)}${token}${pad}${rest}`;
  return {
    text: next,
    caret: start + token.length + pad.length,
    selection: { user_id: (candidate.user_id ?? "").trim(), token },
  };
}

/** Arrow-key movement through the popup, wrapping at both ends. An empty list stays at 0. */
export function moveMentionHighlight(index: number, length: number, delta: number): number {
  if (length <= 0) return 0;
  const next = (index + delta) % length;
  return next < 0 ? next + length : next;
}
