"use client";

/**
 * The @-people picker: the popup, its filtering and ranking, its keyboard movement, and the chips
 * for the people already accepted. Everything here is ENHANCEMENT, which is why it is a separate
 * module the textarea fetches on demand.
 *
 * It renders NO field. The textarea and the hidden mentions input are server-rendered by
 * `mention-textarea.tsx` and stay eager, so the note still submits with JavaScript off and the
 * text field is the SAME DOM node before and after this chunk arrives -- a remount would drop the
 * keystrokes typed while it was in flight.
 *
 * It also holds no state of its own about WHETHER it is open: `text`, `caret` and `suppressed`
 * come from the field, and the query is derived from them on every render. That is what makes an
 * early `@` safe -- the trigger that starts the load is still true when this module lands, so the
 * popup opens from the derived state rather than from an event that has already been and gone.
 *
 * MOBILE (390px, WhatsApp in-app webview). The popup is absolutely positioned INSIDE the
 * textarea's own relative wrapper -- never `position: fixed`, which drifts when the webview's
 * retractable chrome moves -- is capped in `vh` then `dvh`, never exceeds the wrapper's width,
 * and every row clears 44px. It is rendered after the textarea in the DOM, so the on-screen
 * keyboard pushes the field up and the list stays attached to it.
 *
 * STYLING: existing shared classes only (`.card`, `.pm-item`, `.pm-hint`, `.achip`, `.pn`,
 * `.muted`), plus `.mention-pop` for the popover geometry.
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import { X } from "lucide-react";
import type { MentionComposerCopy } from "./notification-copy";
import {
  activeMentionQuery,
  applyMentionSelection,
  filterMentionCandidates,
  MENTION_KEYS,
  MENTION_RESULT_LIMIT,
  moveMentionHighlight,
  pruneMentionSelections,
  type MentionCandidate,
  type MentionSelection,
} from "./mention-model";

/** What the popup reports back so the eager textarea can carry the right ARIA state. */
export type MentionPopupState = { open: boolean; activeDescendant?: string };

export type MentionPickerProps = {
  /** The field's current text and caret. The query is derived from these, never stored. */
  text: string;
  caret: number;
  /** True while the field is blurred or the reader pressed Escape. */
  suppressed: boolean;
  candidates: readonly MentionCandidate[];
  composerCopy: MentionComposerCopy;
  /** The id the textarea's `aria-controls` already points at. */
  listId: string;
  /** The mentions accepted so far, owned by the textarea because the hidden input carries them. */
  selections: readonly MentionSelection[];
  /** A pick: the rewritten text, where the caret must land, and the new selection list. */
  onApply: (text: string, caret: number, selections: MentionSelection[]) => void;
  /** Chip removal: the token comes out of the text too, so the text moves as well. */
  onRemove: (text: string, selections: MentionSelection[]) => void;
  /** Escape. The field stays focused; only the popup closes. */
  onDismiss: () => void;
  /** Lets the textarea's own `onKeyDown` offer each key here first. */
  registerKeyHandler: (handler: ((event: React.KeyboardEvent<HTMLTextAreaElement>) => boolean) | null) => void;
  onPopupStateChange: (state: MentionPopupState) => void;
};

export default function MentionPicker({
  text,
  caret,
  suppressed,
  candidates,
  composerCopy,
  listId,
  selections,
  onApply,
  onRemove,
  onDismiss,
  registerKeyHandler,
  onPopupStateChange,
}: MentionPickerProps) {
  // The highlight is stored WITH the query it belongs to, rather than reset from an effect when
  // the query changes: a new query must start at the top of its own list, or the highlight lands
  // on a different person than the one under the reader's eye.
  const [marker, setMarker] = useState<{ query: string | null; index: number }>({ query: null, index: 0 });

  const active = useMemo(() => (suppressed ? null : activeMentionQuery(text, caret)), [caret, suppressed, text]);
  const query = active ? active.query : null;
  const popupOpen = query !== null;
  const matches = useMemo(
    () => (query === null ? [] : filterMentionCandidates(candidates, query, MENTION_RESULT_LIMIT)),
    [candidates, query],
  );
  const highlight = marker.query === query ? marker.index : 0;
  const highlighted = matches.length > 0 ? Math.min(highlight, matches.length - 1) : 0;
  const moveHighlight = useCallback(
    (delta: number, length: number) =>
      setMarker((current) => ({
        query,
        index: moveMentionHighlight(current.query === query ? current.index : 0, length, delta),
      })),
    [query],
  );

  const choose = useCallback(
    (candidate: MentionCandidate) => {
      const applied = applyMentionSelection(text, caret, candidate);
      const next = pruneMentionSelections(applied.text, [...selections, applied.selection]);
      onApply(applied.text, applied.caret, next);
    },
    [caret, onApply, selections, text],
  );

  // The textarea owns the keydown, because it owns the field; it offers each key here first and
  // handles what this returns false for, so Enter still breaks a line while the popup is shut.
  useEffect(() => {
    registerKeyHandler((event) => {
      if (!popupOpen) return false;
      if (event.key === MENTION_KEYS.escape) {
        event.preventDefault();
        onDismiss();
        return true;
      }
      if (matches.length === 0) return false;
      if (event.key === MENTION_KEYS.down) {
        event.preventDefault();
        moveHighlight(1, matches.length);
        return true;
      }
      if (event.key === MENTION_KEYS.up) {
        event.preventDefault();
        moveHighlight(-1, matches.length);
        return true;
      }
      if (event.key === MENTION_KEYS.enter || event.key === MENTION_KEYS.tab) {
        // Enter picks the highlighted person INSTEAD of breaking the line, but only while the
        // popup is open -- otherwise a multi-line note becomes impossible to type.
        event.preventDefault();
        const candidate = matches[highlighted];
        if (candidate) choose(candidate);
        return true;
      }
      return false;
    });
    return () => registerKeyHandler(null);
  }, [choose, highlighted, matches, moveHighlight, onDismiss, popupOpen, registerKeyHandler]);

  useEffect(() => {
    onPopupStateChange({
      open: popupOpen,
      activeDescendant: popupOpen && matches.length > 0 ? `${listId}-${highlighted}` : undefined,
    });
  }, [highlighted, listId, matches.length, onPopupStateChange, popupOpen]);

  const removeSelection = useCallback(
    (selection: MentionSelection) => {
      // Removing the chip removes the TOKEN too: a name left in the sentence that notifies nobody
      // is the one outcome the reader cannot see or explain.
      const nextText = text.replace(selection.token, "").replace(/ {2,}/g, " ");
      const nextSelections = pruneMentionSelections(nextText, selections).filter(
        (entry) => entry.user_id !== selection.user_id || entry.token !== selection.token,
      );
      onRemove(nextText, nextSelections);
    },
    [onRemove, selections, text],
  );

  const chips = pruneMentionSelections(text, selections);

  return (
    <>
      {/* The picked people, as chips -- the "renders distinctly" half. A textarea cannot style a
          run of its own text, so the mention is shown as a chip beside the field rather than
          faked with an overlay that drifts out of alignment on a phone. */}
      {chips.length > 0 ? (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 6 }}>
          <span className="muted" style={{ fontSize: 11 }}>
            {composerCopy.mentionedLabel}
          </span>
          {chips.map((selection) => (
            <span className="achip" key={`${selection.user_id}:${selection.token}`}>
              {selection.token}
              <button
                type="button"
                className="btn sm ghost"
                style={{ minWidth: 40, minHeight: 40, padding: 0, border: 0, background: "transparent" }}
                onClick={() => removeSelection(selection)}
                title={composerCopy.removeMention}
                aria-label={composerCopy.removeMention}
              >
                <X className="ic" aria-hidden="true" />
              </button>
            </span>
          ))}
        </div>
      ) : null}

      {popupOpen ? (
        // The popup. Absolute inside the textarea's wrapper (never fixed), full wrapper width so
        // it cannot overflow a 390px viewport.
        //
        // `.mention-pop` carries the geometry (and the `vh`-then-`dvh` cap: React emits a single
        // `max-height` declaration, so an inline `40dvh` gave an engine without `dvh` NO cap and
        // an unbounded list). `data-mention-popup` is the hook the detail card's
        // `:has([data-mention-popup])` rule keys off to lift its own `overflow:hidden` -- and the
        // generic `.card .bd` overflow -- for exactly as long as this popup exists. Absolute
        // positioning alone did NOT save it: measured in Chromium, one of eight rows was
        // reachable at every width until those two ancestors stopped clipping.
        <div className="card mention-pop" data-mention-popup>
          <ul id={listId} role="listbox" aria-label={composerCopy.peopleLabel} style={{ listStyle: "none", margin: 0, padding: 4 }}>
            {matches.map((candidate, index) => (
              <li
                key={candidate.user_id}
                id={`${listId}-${index}`}
                role="option"
                aria-selected={index === highlighted}
                className={`pm-item ${index === highlighted ? "on" : ""}`}
                // 44px: a real tap target at phone width.
                style={{ minHeight: 44, cursor: "pointer" }}
                onMouseDown={(event) => {
                  event.preventDefault();
                  choose(candidate);
                }}
                onMouseEnter={() => setMarker({ query, index })}
              >
                <span style={{ minWidth: 0 }}>
                  <span className="pn" style={{ display: "block" }}>
                    {candidate.name}
                  </span>
                  {candidate.title ? (
                    <span className="muted" style={{ display: "block", fontSize: 11 }}>
                      {candidate.title}
                    </span>
                  ) : null}
                </span>
              </li>
            ))}
          </ul>
          {matches.length === 0 ? <div className="pm-hint">{composerCopy.noMatches}</div> : null}
        </div>
      ) : null}
    </>
  );
}
