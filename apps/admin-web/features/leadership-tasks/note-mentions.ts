/**
 * How a stored note's body is split into prose and mention chips for the Activity feed.
 *
 * A note is PLAIN TEXT exactly as typed ("@Manju all done"), and the people it named ride beside
 * it as `mentions` (`LeadershipTaskNote.mentions`, resolved and stored by the backend when the
 * note was written). The feed draws each named person as a brand-toned chip so a mention reads
 * as a mention and not as a stray "@". The segmenting is driven by the STORED mentions -- for
 * each stored name the literal token "@<name>" is looked up in the body -- never by a regex
 * over the prose, because display names collide and a "@" in an email address is not a person.
 *
 * Import-free so `note-mentions.test.mjs` runs it under `node --test`.
 */

export type NoteMention = { user_id: string; name: string };

export type NoteSegment =
  | { kind: "text"; text: string }
  | { kind: "mention"; text: string; user_id: string };

function isBoundary(character: string | undefined): boolean {
  return character === undefined || /[\s(,;:.!?[\]]/.test(character);
}

/**
 * Splits `body` into runs of prose and mention tokens. Longer names are matched first so
 * "@Ravi Teja" is one chip and not "@Ravi" plus " Teja". A name that is not in the text (the
 * writer deleted it before sending, or an older note) simply draws no chip.
 */
export function segmentNoteBody(body: string, mentions: readonly NoteMention[] = []): NoteSegment[] {
  const tokens = mentions
    .map((mention) => ({ user_id: mention.user_id, token: `@${(mention.name ?? "").trim()}` }))
    .filter((entry) => entry.token.length > 1)
    .sort((left, right) => right.token.length - left.token.length);
  if (!body || tokens.length === 0) return body ? [{ kind: "text", text: body }] : [];

  const out: NoteSegment[] = [];
  let index = 0;
  let textStart = 0;
  while (index < body.length) {
    if (body[index] !== "@" || !isBoundary(body[index - 1])) {
      index += 1;
      continue;
    }
    const hit = tokens.find(
      (entry) =>
        body.startsWith(entry.token, index) && isBoundary(body[index + entry.token.length]),
    );
    if (!hit) {
      index += 1;
      continue;
    }
    if (index > textStart) out.push({ kind: "text", text: body.slice(textStart, index) });
    out.push({ kind: "mention", text: hit.token, user_id: hit.user_id });
    index += hit.token.length;
    textStart = index;
  }
  if (textStart < body.length) out.push({ kind: "text", text: body.slice(textStart) });
  return out;
}
