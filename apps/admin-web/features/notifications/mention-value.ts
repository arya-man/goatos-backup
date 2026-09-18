/**
 * The half of the composer's rulebook the EAGER textarea needs: what the hidden input carries.
 *
 * It lives apart from `mention-model.ts` for one measured reason. The textarea and its hidden
 * mentions input are server-rendered and eager, because without JavaScript the textarea still
 * submits its text; the @-picker is loaded on demand. An eager file may therefore import ONLY
 * what the field itself cannot answer without: which of the accepted mentions are still written
 * in the text. Query parsing, candidate ranking, keyboard movement and token insertion are the
 * PICKER's rules and stay in `mention-model.ts`, which the picker chunk pulls in with it.
 *
 * `mention-model.ts` re-exports everything here, so nothing outside this file's two readers has
 * to know the split -- and the ids are still derived from the accepted selections, never from a
 * regex over the prose, because display names collide on a real roster.
 *
 * Import-free, like its sibling, so `mention-model.test.mjs` runs it under `node --test`.
 */

export type MentionCandidate = {
  user_id: string;
  /** The person's name, as the roster endpoint gave it. Never composed here. */
  name: string;
  /** Their business title, shown as the second line of the row. */
  title?: string;
};

/** One accepted mention: the id that travels to the backend, plus the exact text it wrote. */
export type MentionSelection = {
  user_id: string;
  /** The literal token inserted into the textarea, e.g. "@Ravi Teja", WITHOUT the trailing space. */
  token: string;
};

export type MentionValue = {
  /** What the person typed and sees. Sent as the comment/task body. */
  text: string;
  /** The explicit ids the backend notifies. Derived from `selections`, never from the text. */
  mentionUserIds: string[];
};

/** The trigger character. Also what the eager textarea watches for to fetch the picker. */
export const MENTION_TRIGGER = "@";

/**
 * The ids that travel with the text.
 *
 * A selection survives only while its token is still in the text: deleting "@Ravi Teja" must stop
 * notifying Ravi, and nothing else in the composer watches for that. Duplicates collapse, so
 * mentioning one person twice is one notification.
 */
export function selectedMentionUserIds(text: string, selections: readonly MentionSelection[]): string[] {
  const out: string[] = [];
  for (const selection of selections) {
    const id = (selection.user_id ?? "").trim();
    if (!id || out.includes(id)) continue;
    if (!selection.token || !text.includes(selection.token)) continue;
    out.push(id);
  }
  return out;
}

/** Drops selections whose token is gone, so the list cannot grow without bound while editing. */
export function pruneMentionSelections(
  text: string,
  selections: readonly MentionSelection[],
): MentionSelection[] {
  const kept: MentionSelection[] = [];
  for (const selection of selections) {
    if (!selection.token || !text.includes(selection.token)) continue;
    if (kept.some((entry) => entry.user_id === selection.user_id && entry.token === selection.token)) continue;
    kept.push(selection);
  }
  return kept;
}

/** Both halves of the composer's output in one object, for a caller that submits a form. */
export function mentionValue(text: string, selections: readonly MentionSelection[]): MentionValue {
  return { text, mentionUserIds: selectedMentionUserIds(text, selections) };
}
