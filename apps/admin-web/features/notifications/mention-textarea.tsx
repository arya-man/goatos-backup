"use client";

/**
 * A textarea that opens a people picker when you type `@`.
 *
 * Standalone and reusable: it takes its candidate list as a prop (no fetching of its own), and it
 * emits BOTH halves of the answer — the display text the person wrote, and the explicit
 * `user_id`s they picked. The ids are the contract with the backend: display names collide on a
 * real roster, and the Android app ships on its own release train and would render an inline
 * token format as raw characters, so nothing downstream re-derives a person from the prose.
 *
 * FORM USE. It renders a real named `<textarea>` plus a hidden input carrying the ids, so it can
 * drop into an existing `<form action={serverAction}>` without the parent becoming controlled.
 * Without JavaScript the textarea still submits its text; only the picker needs JS.
 *
 * MOBILE (390px, WhatsApp in-app webview). The popup is absolutely positioned INSIDE this
 * component's own relative wrapper — never `position: fixed`, which drifts when the webview's
 * retractable chrome moves — is capped in `dvh` (never `vh`, for the same reason), never exceeds
 * the wrapper's width, and every row clears 44px. It is rendered after the textarea in the DOM,
 * so the on-screen keyboard pushes the field up and the list stays attached to it.
 *
 * STYLING: existing shared classes only (`.fld`, `.card`, `.pm-item`, `.pm-hint`, `.achip`,
 * `.pn`, `.pr`, `.muted`). `app/mesha-theme.css` is owned by another agent this round, so the few
 * popover rules no shared class carries are inline and commented.
 */

import { useCallback, useId, useMemo, useRef, useState } from "react";
import { X } from "lucide-react";
import type { MentionComposerCopy } from "./notification-copy";
import {
  activeMentionQuery,
  applyMentionSelection,
  filterMentionCandidates,
  MENTION_KEYS,
  MENTION_RESULT_LIMIT,
  mentionValue,
  moveMentionHighlight,
  pruneMentionSelections,
  selectedMentionUserIds,
  type MentionCandidate,
  type MentionSelection,
  type MentionValue,
} from "./mention-model";

export type { MentionCandidate, MentionValue } from "./mention-model";

export function MentionTextarea({
  name,
  mentionsName,
  candidates,
  composerCopy,
  defaultValue = "",
  rows = 3,
  maxLength,
  required = false,
  placeholder,
  textareaId,
  onValueChange,
}: {
  /** The textarea's form field name, e.g. the comment body. */
  name: string;
  /**
   * The hidden field carrying the picked ids. Comma-separated, because a Server Action reads a
   * FormData string; `formData.get(mentionsName).split(",")` is the whole parse.
   */
  mentionsName: string;
  candidates: readonly MentionCandidate[];
  composerCopy: MentionComposerCopy;
  defaultValue?: string;
  rows?: number;
  maxLength?: number;
  required?: boolean;
  /** Backend-owned copy from the caller's page contract. */
  placeholder?: string;
  textareaId?: string;
  /** Fires on every keystroke and every pick, with both halves of the value. */
  onValueChange?: (value: MentionValue) => void;
}) {
  const generatedId = useId();
  const listId = `${generatedId}-mention-list`;
  const fieldId = textareaId ?? `${generatedId}-mention-field`;
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [text, setText] = useState(defaultValue);
  const [selections, setSelections] = useState<MentionSelection[]>([]);
  const [query, setQuery] = useState<string | null>(null);
  const [highlight, setHighlight] = useState(0);

  const matches = useMemo(
    () => (query === null ? [] : filterMentionCandidates(candidates, query, MENTION_RESULT_LIMIT)),
    [candidates, query],
  );
  const popupOpen = query !== null;

  const emit = useCallback(
    (nextText: string, nextSelections: MentionSelection[]) => {
      onValueChange?.(mentionValue(nextText, nextSelections));
    },
    [onValueChange],
  );

  const syncFromField = useCallback(
    (nextText: string, caret: number) => {
      // A deleted token must stop notifying that person, and nothing else in the composer is
      // watching for it, so every keystroke re-prunes.
      const pruned = pruneMentionSelections(nextText, selections);
      const active = activeMentionQuery(nextText, caret);
      setText(nextText);
      setSelections(pruned);
      setQuery(active ? active.query : null);
      setHighlight(0);
      emit(nextText, pruned);
    },
    [emit, selections],
  );

  const choose = useCallback(
    (candidate: MentionCandidate) => {
      const field = textareaRef.current;
      const caret = field?.selectionStart ?? text.length;
      const applied = applyMentionSelection(text, caret, candidate);
      const nextSelections = pruneMentionSelections(applied.text, [...selections, applied.selection]);
      setText(applied.text);
      setSelections(nextSelections);
      setQuery(null);
      setHighlight(0);
      emit(applied.text, nextSelections);
      // The caret has to land after the inserted name, or the next character lands inside it.
      requestAnimationFrame(() => {
        const element = textareaRef.current;
        if (!element) return;
        element.focus();
        element.setSelectionRange(applied.caret, applied.caret);
      });
    },
    [emit, selections, text],
  );

  const removeSelection = useCallback(
    (selection: MentionSelection) => {
      // Removing the chip removes the TOKEN too: a name left in the sentence that notifies nobody
      // is the one outcome the reader cannot see or explain.
      const nextText = text.replace(selection.token, "").replace(/ {2,}/g, " ");
      const nextSelections = pruneMentionSelections(nextText, selections).filter(
        (entry) => entry.user_id !== selection.user_id || entry.token !== selection.token,
      );
      setText(nextText);
      setSelections(nextSelections);
      emit(nextText, nextSelections);
    },
    [emit, selections, text],
  );

  const mentionedIds = selectedMentionUserIds(text, selections);
  const mentionedChips = pruneMentionSelections(text, selections);

  function onKeyDown(event: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (!popupOpen) return;
    if (event.key === MENTION_KEYS.escape) {
      event.preventDefault();
      setQuery(null);
      return;
    }
    if (matches.length === 0) return;
    if (event.key === MENTION_KEYS.down) {
      event.preventDefault();
      setHighlight((current) => moveMentionHighlight(current, matches.length, 1));
      return;
    }
    if (event.key === MENTION_KEYS.up) {
      event.preventDefault();
      setHighlight((current) => moveMentionHighlight(current, matches.length, -1));
      return;
    }
    if (event.key === MENTION_KEYS.enter || event.key === MENTION_KEYS.tab) {
      // Enter picks the highlighted person INSTEAD of breaking the line, but only while the popup
      // is open — otherwise a multi-line note becomes impossible to type.
      event.preventDefault();
      const candidate = matches[Math.min(highlight, matches.length - 1)];
      if (candidate) choose(candidate);
    }
  }

  return (
    <div className="fld" style={{ position: "relative" }}>
      <textarea
        ref={textareaRef}
        id={fieldId}
        name={name}
        rows={rows}
        maxLength={maxLength}
        required={required}
        placeholder={placeholder}
        value={text}
        role="combobox"
        aria-expanded={popupOpen}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={popupOpen && matches.length > 0 ? `${listId}-${Math.min(highlight, matches.length - 1)}` : undefined}
        onChange={(event) => syncFromField(event.target.value, event.target.selectionStart ?? event.target.value.length)}
        onKeyDown={onKeyDown}
        onClick={(event) => {
          const field = event.currentTarget;
          const active = activeMentionQuery(field.value, field.selectionStart ?? field.value.length);
          setQuery(active ? active.query : null);
        }}
        onBlur={() => {
          // Closed on blur, but AFTER the click on a row has had its chance: the rows use
          // onMouseDown so the pick lands before focus leaves.
          setQuery(null);
        }}
      />
      <span className="pm-hint" style={{ display: "block", padding: "2px 0 0", border: 0, margin: 0 }}>
        {composerCopy.hint}
      </span>

      {/* The picked people, as chips — the "renders distinctly" half. A textarea cannot style a
          run of its own text, so the mention is shown as a chip beside the field rather than
          faked with an overlay that drifts out of alignment on a phone. */}
      {mentionedChips.length > 0 ? (
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 6 }}>
          <span className="muted" style={{ fontSize: 11 }}>
            {composerCopy.mentionedLabel}
          </span>
          {mentionedChips.map((selection) => (
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

      {/* The ids that actually travel. Comma-separated so a Server Action can read one field. */}
      <input type="hidden" name={mentionsName} value={mentionedIds.join(",")} />

      {popupOpen ? (
        // The popup. Absolute inside this wrapper (never fixed), full wrapper width so it cannot
        // overflow a 390px viewport.
        //
        // `.mention-pop` carries the geometry (and the `vh`-then-`dvh` cap: React emits a single
        // `max-height` declaration, so an inline `40dvh` gave an engine without `dvh` NO cap and an
        // unbounded list). `data-mention-popup` is the hook the detail card's
        // `:has([data-mention-popup])` rule keys off to lift its own `overflow:hidden` -- and the
        // generic `.card .bd` overflow -- for exactly as long as this popup exists. Absolute
        // positioning alone did NOT save it: measured in Chromium, one of eight rows was reachable
        // at every width until those two ancestors stopped clipping.
        <div className="card mention-pop" data-mention-popup>
          <ul id={listId} role="listbox" aria-label={composerCopy.peopleLabel} style={{ listStyle: "none", margin: 0, padding: 4 }}>
            {matches.map((candidate, index) => (
              <li
                key={candidate.user_id}
                id={`${listId}-${index}`}
                role="option"
                aria-selected={index === Math.min(highlight, matches.length - 1)}
                className={`pm-item ${index === Math.min(highlight, matches.length - 1) ? "on" : ""}`}
                // 44px: a real tap target at phone width.
                style={{ minHeight: 44, cursor: "pointer" }}
                onMouseDown={(event) => {
                  event.preventDefault();
                  choose(candidate);
                }}
                onMouseEnter={() => setHighlight(index)}
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
    </div>
  );
}
