"use client";

/**
 * A textarea that opens a people picker when you type `@`.
 *
 * Standalone and reusable: it takes its candidate list as a prop (no fetching of its own), and it
 * emits BOTH halves of the answer -- the display text the person wrote, and the explicit
 * `user_id`s they picked. The ids are the contract with the backend: display names collide on a
 * real roster, and the Android app ships on its own release train and would render an inline
 * token format as raw characters, so nothing downstream re-derives a person from the prose.
 *
 * FORM USE. It renders a real named `<textarea>` plus a hidden input carrying the ids, so it can
 * drop into an existing `<form action={serverAction}>` without the parent becoming controlled.
 * Without JavaScript the textarea still submits its text; only the picker needs JS.
 *
 * WHAT IS EAGER AND WHAT IS NOT, and why the line is drawn here. This file -- the field, the
 * hint, the chips' host and the hidden mentions input -- is in the shared client chunk of every
 * route, because the no-JS submit depends on the textarea being in the SERVER-RENDERED HTML. The
 * @-picker (`./mention-picker`, which pulls in `./mention-model`) is pure enhancement and is
 * fetched on first use: it was 4.6 KB gzip that every one of the 63 routes carried to render a
 * popup almost none of them can open. Deferring the WHOLE component instead would have taken the
 * textarea out of the HTML and broken the no-JS submit, which is why the split is here and not a
 * `lazy()` around the export.
 *
 * Two properties this split has to keep, both proven in a browser rather than argued:
 *
 *  - The textarea is the SAME DOM node before and after the picker chunk lands. It is rendered
 *    unconditionally, outside the Suspense boundary, so nothing about the load remounts it, moves
 *    focus, or drops a keystroke typed while the chunk is in flight.
 *  - The `@` that starts the load is not swallowed. The trigger IS the load trigger, and the
 *    picker derives whether it is open from the field's current (text, caret) when it mounts --
 *    not from the keystroke that fetched it -- so a reader who types `@ra` before the chunk
 *    arrives gets the popup for `ra` the moment it does.
 *
 * MOBILE (390px, WhatsApp in-app webview): see the picker module; the popup lives inside this
 * component's own relative wrapper so it cannot overflow the viewport.
 *
 * STYLING: existing shared classes only (`.fld`, `.pm-hint`).
 */

import { lazy, Suspense, useCallback, useId, useMemo, useRef, useState } from "react";
import type { MentionComposerCopy } from "./notification-copy";
import {
  MENTION_TRIGGER,
  mentionValue,
  pruneMentionSelections,
  selectedMentionUserIds,
  type MentionCandidate,
  type MentionSelection,
  type MentionValue,
} from "./mention-value.ts";
import type { MentionPopupState } from "./mention-picker";

export type { MentionCandidate, MentionValue } from "./mention-value.ts";

// The picker's chunk. Requested the first time the text holds a `@`, and never on a route where
// nobody types one.
const MentionPicker = lazy(() => import("./mention-picker"));

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
  const [caret, setCaret] = useState(defaultValue.length);
  const [selections, setSelections] = useState<MentionSelection[]>([]);
  // Whether the picker's chunk has been asked for. One way only: once a `@` has been typed the
  // popup may be needed again at any keystroke, and a second fetch would be a second stall.
  const [pickerWanted, setPickerWanted] = useState(false);
  // Blur and Escape close the popup without unloading it. The picker reads this rather than
  // keeping its own open flag, so the field stays the one owner of whether a popup is wanted.
  const [suppressed, setSuppressed] = useState(false);
  const [popup, setPopup] = useState<MentionPopupState>({ open: false });
  const keyHandlerRef = useRef<((event: React.KeyboardEvent<HTMLTextAreaElement>) => boolean) | null>(null);

  const emit = useCallback(
    (nextText: string, nextSelections: MentionSelection[]) => {
      onValueChange?.(mentionValue(nextText, nextSelections));
    },
    [onValueChange],
  );

  const syncFromField = useCallback(
    (nextText: string, nextCaret: number) => {
      // A deleted token must stop notifying that person, and nothing else in the composer is
      // watching for it, so every keystroke re-prunes.
      const pruned = pruneMentionSelections(nextText, selections);
      setText(nextText);
      setCaret(nextCaret);
      setSelections(pruned);
      setSuppressed(false);
      // The trigger starts the load. Typing `@` is the only thing that can open the popup, so it
      // is also the only thing that has to pay for it.
      if (nextText.includes(MENTION_TRIGGER)) setPickerWanted(true);
      emit(nextText, pruned);
    },
    [emit, selections],
  );

  const applyPick = useCallback(
    (nextText: string, nextCaret: number, nextSelections: MentionSelection[]) => {
      setText(nextText);
      setCaret(nextCaret);
      setSelections(nextSelections);
      // A finished token is not a query. Closing here keeps a one-word name from re-opening the
      // popup on the name it just inserted.
      setSuppressed(true);
      emit(nextText, nextSelections);
      // The caret has to land after the inserted name, or the next character lands inside it.
      requestAnimationFrame(() => {
        const element = textareaRef.current;
        if (!element) return;
        element.focus();
        element.setSelectionRange(nextCaret, nextCaret);
      });
    },
    [emit],
  );

  const applyRemoval = useCallback(
    (nextText: string, nextSelections: MentionSelection[]) => {
      setText(nextText);
      setCaret(Math.min(caret, nextText.length));
      setSelections(nextSelections);
      emit(nextText, nextSelections);
    },
    [caret, emit],
  );

  // Stable identities: the picker registers its key handler and reports its ARIA state from
  // effects, so a new function on every render of this component would re-run them every time.
  const registerKeyHandler = useCallback(
    (handler: ((event: React.KeyboardEvent<HTMLTextAreaElement>) => boolean) | null) => {
      keyHandlerRef.current = handler;
    },
    [],
  );
  const onPopupStateChange = useCallback((state: MentionPopupState) => {
    setPopup((current) =>
      current.open === state.open && current.activeDescendant === state.activeDescendant ? current : state,
    );
  }, []);
  const dismiss = useCallback(() => setSuppressed(true), []);

  const mentionedIds = useMemo(() => selectedMentionUserIds(text, selections), [selections, text]);

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
        aria-expanded={popup.open}
        aria-controls={listId}
        aria-autocomplete="list"
        aria-activedescendant={popup.activeDescendant}
        onChange={(event) =>
          syncFromField(event.target.value, event.target.selectionStart ?? event.target.value.length)
        }
        onKeyDown={(event) => {
          // The picker gets first refusal on navigation keys while its popup is open; everything
          // else, and every key at all before the chunk lands, behaves like a plain textarea.
          keyHandlerRef.current?.(event);
        }}
        onClick={(event) => {
          const field = event.currentTarget;
          setCaret(field.selectionStart ?? field.value.length);
          setSuppressed(false);
          if (field.value.includes(MENTION_TRIGGER)) setPickerWanted(true);
        }}
        onBlur={() => {
          // Closed on blur, but AFTER the click on a row has had its chance: the rows use
          // onMouseDown so the pick lands before focus leaves.
          setSuppressed(true);
        }}
      />
      <span className="pm-hint" style={{ display: "block", padding: "2px 0 0", border: 0, margin: 0 }}>
        {composerCopy.hint}
      </span>

      {/* The ids that actually travel. Comma-separated so a Server Action can read one field.
          Server-rendered with the textarea, so the pair is always posted together. */}
      <input type="hidden" name={mentionsName} value={mentionedIds.join(",")} />

      {/* The chips and the popup, once someone has typed a `@`. The fallback is deliberately
          nothing: the field above is already on screen and must not flicker, move or remount
          while this arrives. */}
      {pickerWanted ? (
        <Suspense fallback={null}>
          <MentionPicker
            text={text}
            caret={caret}
            suppressed={suppressed}
            candidates={candidates}
            composerCopy={composerCopy}
            listId={listId}
            selections={selections}
            onApply={applyPick}
            onRemove={applyRemoval}
            onDismiss={dismiss}
            registerKeyHandler={registerKeyHandler}
            onPopupStateChange={onPopupStateChange}
          />
        </Suspense>
      ) : null}
    </div>
  );
}
