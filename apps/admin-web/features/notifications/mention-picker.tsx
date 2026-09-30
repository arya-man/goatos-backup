"use client";

/**
 * The @-people picker: the popup, its filtering and ranking, and its keyboard movement.
 * Everything here is ENHANCEMENT, which is why it is a separate module the textarea fetches on
 * demand. (It used to also draw a "Mentioned @Name x" chip row under the field; the name is
 * already in the text, so that row went -- see the note above the render.)
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
 * STYLING: the template dropdown paper + MenuItem rows (components/app/dropdown-paper), plus
 * `.mention-pop` for the popover geometry.
 */

import { useCallback, useEffect, useMemo, useState } from "react";
import type { MentionComposerCopy } from "./notification-copy";
import Box from "@mui/material/Box";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import Typography from "@mui/material/Typography";
import { DropdownPaper } from "@/components/app/dropdown-paper";
import { TAP_MIN } from "@/components/app/tap";
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
  onDismiss,
  registerKeyHandler,
  onPopupStateChange,
}: MentionPickerProps) {
  // The highlight is stored WITH the query it belongs to, rather than reset from an effect when
  // the query changes: a new query must start at the top of its own list, or the highlight lands
  // on a different person than the one under the reader's eye.
  const [marker, setMarker] = useState<{ query: string | null; index: number }>({ query: null, index: 0 });

  // A PICKED token is not a query (CEO, 2026-09-18: "@Manju " + "let" re-opened the popup on
  // "Manju let" and drew "Nobody ... matches that." over Send). The model stops the query at the
  // first whitespace and treats a token already in `selections` as finished, so the popup opens
  // again only for a fresh "@" at a word boundary.
  const completed = useMemo(() => selections.map((selection) => selection.token), [selections]);
  const active = useMemo(
    () => (suppressed ? null : activeMentionQuery(text, caret, completed)),
    [caret, completed, suppressed, text],
  );
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

  // No chip row. The mention is already visible IN the text ("@Manju"), and a second "Mentioned
  // @Manju x" line under the field said the same thing twice; deleting the name from the text is
  // how a mention is withdrawn (`pruneMentionSelections` drops its id the moment the token goes).
  // The hidden `mention_user_ids` input in `mention-textarea.tsx` still carries the ids.
  return (
    <>
      {popupOpen ? (
        // The popup. Absolute inside the textarea's wrapper (never fixed), full wrapper width so
        // it cannot overflow a 390px viewport.
        //
        // The sx carries the geometry (and the `vh` cap, lifted to `dvh` under `@supports`: an engine
        // without `dvh` keeps the `vh` cap). `data-mention-popup` is the hook a host card keys
        // off (`&:has([data-mention-popup])`) to lift its own `overflow: hidden` for exactly as long
        // as this popup exists: absolute positioning alone did NOT save it, measured in Chromium,
        // one of eight rows was reachable at every width until the clipping ancestor let go.
        // Template dropdown paper IN PLACE (never a portalled Popover: its focus trap would take the
        // caret out of the textarea while the reader is still typing the name).
        <DropdownPaper
          data-mention-popup
          sx={{ position: "absolute", left: 0, right: 0, top: "100%", zIndex: 60, mt: 0.5, maxHeight: "40vh", "@supports (height: 1dvh)": { maxHeight: "40dvh" }, overflowY: "auto", overscrollBehavior: "contain" }}
        >
          <MenuList id={listId} role="listbox" aria-label={composerCopy.peopleLabel}>
            {matches.map((candidate, index) => (
              <MenuItem
                key={candidate.user_id}
                id={`${listId}-${index}`}
                role="option"
                aria-selected={index === highlighted}
                selected={index === highlighted}
                tabIndex={-1}
                // 44px: a real tap target at phone width.
                sx={{ minHeight: TAP_MIN, whiteSpace: "normal" }}
                onMouseDown={(event) => {
                  event.preventDefault();
                  choose(candidate);
                }}
                onMouseEnter={() => setMarker({ query, index })}
              >
                <Box component="span" sx={{ minWidth: 0 }}>
                  <Typography variant="subtitle2" component="span" sx={{ display: "block" }}>
                    {candidate.name}
                  </Typography>
                  {candidate.title ? (
                    <Typography variant="caption" component="span" sx={{ display: "block", color: "text.secondary" }}>
                      {candidate.title}
                    </Typography>
                  ) : null}
                </Box>
              </MenuItem>
            ))}
          </MenuList>
          {matches.length === 0 ? (
            <Typography variant="caption" component="div" sx={{ px: 1, py: 1, color: "text.secondary" }}>{composerCopy.noMatches}</Typography>
          ) : null}
        </DropdownPaper>
      ) : null}
    </>
  );
}
