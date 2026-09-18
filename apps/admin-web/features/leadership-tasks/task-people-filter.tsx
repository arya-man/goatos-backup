"use client";

/**
 * THE PERSON FILTER: one searchable control, used for BOTH people parameters.
 *
 * ── WHAT IT REPLACES, AND WHY THERE IS NOW ONLY ONE OF THEM ───────────────────────────────────
 * This desk had TWO person filters that wrote the SAME `t_assignee` parameter:
 *   - the board's overlapping avatar group, which showed six initials and collapsed the rest into
 *     a `+5` rendered as `<span aria-hidden>` — not a link, no handler, invisible to assistive
 *     technology. Five of eleven people were unreachable and pressing the chip did nothing;
 *   - the toolbar's native `<select>`, which could reach everyone but could not be TYPED into.
 * Neither was usable for the actual job. At this tenant the avatars read `D D M D M A` — two D's
 * and two M's — because initials are not a name, and a CXO narrowing 414 tasks across eleven
 * people is looking for a PERSON, by name.
 *
 * So the two converged into this one control and the avatar group is gone. They were never two
 * different filters; they were two spellings of one parameter, and the reader hit the broken
 * spelling. Convergence also means the person filter now behaves identically in Board and List:
 * the toolbar is on screen in both, so nothing was lost by deleting the board-only copy.
 *
 * It is used TWICE on the bar, for `t_assignee` and `t_raiser` — two different questions about
 * two different people, which is not the duplication that was the defect.
 *
 * ── REUSE, NOT A SECOND PICKER ────────────────────────────────────────────────────────────────
 * The repo has no combobox component and `@base-ui/react` is a dead dependency that is NOT being
 * woken up. The ranking, the result cap and the wrapping arrow movement come from
 * `features/notifications/mention-model.ts`, which the @-mention picker already uses: name-prefix
 * matches first, then title-prefix, then substring. One ranking rule on this screen, so typing
 * "man" puts the same person on top in the composer and in this filter.
 *
 * ── URL, NOT LOCAL STATE ──────────────────────────────────────────────────────────────────────
 * Picking someone navigates, exactly as the old avatar links did: the choice lives in the URL
 * (`t_assignee=<uuid>`), so a narrowed board is still a shareable link, the browser's back button
 * still undoes a filter, and the toolbar's own optimistic-value machinery keeps working. The
 * popup holds nothing but the query and the highlight.
 *
 * ── PHONE (390px) ─────────────────────────────────────────────────────────────────────────────
 * The popup is absolutely positioned inside this control's own relative wrapper — never
 * `position: fixed`, which drifts when the WhatsApp webview's chrome moves — is capped in `vh`
 * and THEN `dvh` in the stylesheet (React emits one `max-height` declaration, so an inline
 * `dvh` cap gives an engine without `dvh` no cap at all), never exceeds the wrapper's width, and
 * every row and the trigger clear 44px. Inside the filter sheet the popup goes static so it
 * cannot escape the sheet's own scroller.
 */

import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { Check, ChevronDown, Search, UserRound } from "lucide-react";
import { faro } from "@grafana/faro-web-sdk";

import {
  filterMentionCandidates,
  MENTION_KEYS,
  moveMentionHighlight,
  type MentionCandidate,
} from "@/features/notifications/mention-model";

import { initials } from "./task-presentation";

/**
 * The Faro event for this surface's primary action (TELEMETRY_GUARDRAILS.md §2.2: a new
 * interactive control wires its own analytics event). A module constant, never an inline string.
 * It records WHICH parameter was set and whether the reader got there by typing, because the
 * number that justifies this control over the old `<select>` is how often a name is typed rather
 * than scrolled to.
 */
const PEOPLE_FILTER_EVENT = "leadership_task_people_filter_pick";

/** How many people the popup lists at once before the reader has to narrow. */
const PEOPLE_RESULT_LIMIT = 8;

export type TaskPeopleOption = { value: string; label: string; title?: string };

export type TaskPeopleFilterCopy = {
  /** "Assignee" / "Raised by" — the question this control answers. */
  label: string;
  /** The empty choice, e.g. "All". */
  allLabel: string;
  /** The search field's own label and placeholder. */
  searchLabel: string;
  /** Nothing matched what was typed. */
  noMatchesLabel: string;
};

export function TaskPeopleFilter({
  param,
  value,
  options,
  copy: labels,
  disabled = false,
  disabledTitle,
  hrefFor,
  onPick,
}: {
  /** The search parameter this control writes, e.g. `t_assignee`. Reported to telemetry. */
  param: string;
  /** The currently applied user id, or "" for everyone. */
  value: string;
  options: TaskPeopleOption[];
  copy: TaskPeopleFilterCopy;
  /** The scope already pins this side of the pair, so the control states that and stays inert. */
  disabled?: boolean;
  disabledTitle?: string;
  /** The URL this choice produces. Rendered as a `title` so the choice is inspectable, and it is
   *  what `onPick` navigates to — the parameter grammar stays with the caller. */
  hrefFor: (value: string) => string;
  onPick: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  // The highlight is stored WITH the query it belongs to, the same way `mention-picker.tsx` does
  // it: a new query starts at the top of its OWN list rather than keeping an index that now
  // points at a different person.
  const [marker, setMarker] = useState<{ query: string; index: number }>({ query: "", index: 0 });
  const wrapRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const listId = useId();

  const selected = options.find((option) => option.value === value);

  /**
   * "All" is a row in the list, not a separate affordance: clearing the filter is the commonest
   * thing a reader does with it, and a control that can be set but not unset from the same place
   * is the `+5` defect in another costume.
   */
  const candidates: MentionCandidate[] = useMemo(
    () => options.map((option) => ({ user_id: option.value, name: option.label, title: option.title ?? "" })),
    [options],
  );
  const matches = useMemo(
    () => filterMentionCandidates(candidates, query, PEOPLE_RESULT_LIMIT),
    [candidates, query],
  );
  /** The rows the reader can actually move through: "All" first, then the matches. */
  const rows = useMemo(
    () => [
      { value: "", label: labels.allLabel, title: "" },
      ...matches.map((candidate) => ({
        value: candidate.user_id ?? "",
        label: candidate.name ?? "",
        title: candidate.title ?? "",
      })),
    ],
    [labels.allLabel, matches],
  );
  const highlight = marker.query === query ? Math.min(marker.index, rows.length - 1) : 0;

  const close = useCallback(
    (returnFocus: boolean) => {
      setOpen(false);
      setQuery("");
      setMarker({ query: "", index: 0 });
      if (returnFocus) triggerRef.current?.focus();
    },
    [],
  );

  const pick = useCallback(
    (next: string, typed: boolean) => {
      try {
        faro.api?.pushEvent(PEOPLE_FILTER_EVENT, {
          param,
          cleared: next ? "false" : "true",
          typed: typed ? "true" : "false",
        });
      } catch {
        // Telemetry must never break a filter.
      }
      close(true);
      onPick(next);
    },
    [close, onPick, param],
  );

  // Escape and an outside press both close, the same contract every other popup on this bar has.
  useEffect(() => {
    if (!open) return;
    function onDown(event: MouseEvent) {
      const node = event.target as Node | null;
      if (node && wrapRef.current?.contains(node)) return;
      close(false);
    }
    function onKey(event: KeyboardEvent) {
      if (event.key === MENTION_KEYS.escape) close(true);
    }
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [close, open]);

  // The field takes focus on open, because the point of this control is that it can be TYPED
  // into. Opening it and landing on a scrollable list would be the old `<select>` again.
  //
  // It is also scrolled INTO VIEW. Inside the phone filter sheet the popup renders static, so
  // adding eight rows below the trigger grows the sheet's own scroller and pushed the focused
  // field off the top of it -- a control that had taken the keyboard and could not be seen.
  // `block: "nearest"` so a desktop viewport, where nothing moved, does not jump.
  useEffect(() => {
    if (!open) return;
    const field = inputRef.current;
    field?.focus();
    field?.scrollIntoView({ block: "nearest" });
  }, [open]);

  return (
    <div className="lt-pf" ref={wrapRef}>
      <span className="lt-pf-label">{labels.label}</span>
      <button
        type="button"
        ref={triggerRef}
        className={`lt-pf-trigger${value ? " set" : ""}`}
        disabled={disabled}
        title={disabled ? disabledTitle : undefined}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={`${labels.label}: ${selected?.label ?? labels.allLabel}`}
        onClick={() => (open ? close(false) : setOpen(true))}
      >
        {value ? (
          <span className="lt-avx" aria-hidden="true">
            {initials(selected?.label ?? "")}
          </span>
        ) : (
          <UserRound className="ic" style={{ width: 14 }} aria-hidden="true" />
        )}
        <span className="lt-pf-value">{selected?.label ?? labels.allLabel}</span>
        <ChevronDown className="ic" style={{ width: 13 }} aria-hidden="true" />
      </button>

      {open ? (
        <div className="card lt-pf-pop" data-people-popup>
          <span className="lt-pf-search">
            <Search className="ic" style={{ width: 14 }} aria-hidden="true" />
            <input
              ref={inputRef}
              type="search"
              role="combobox"
              value={query}
              placeholder={labels.searchLabel}
              aria-label={labels.searchLabel}
              aria-controls={listId}
              aria-expanded="true"
              aria-autocomplete="list"
              aria-activedescendant={rows.length ? `${listId}-${highlight}` : undefined}
              autoComplete="off"
              maxLength={60}
              onChange={(event) => {
                setQuery(event.target.value);
                setMarker({ query: event.target.value, index: 0 });
              }}
              onKeyDown={(event) => {
                if (event.key === MENTION_KEYS.down || event.key === MENTION_KEYS.up) {
                  event.preventDefault();
                  const delta = event.key === MENTION_KEYS.down ? 1 : -1;
                  setMarker((current) => ({
                    query,
                    index: moveMentionHighlight(
                      current.query === query ? current.index : 0,
                      rows.length,
                      delta,
                    ),
                  }));
                  return;
                }
                if (event.key === MENTION_KEYS.enter) {
                  event.preventDefault();
                  const row = rows[highlight];
                  if (row) pick(row.value, query.trim().length > 0);
                }
              }}
            />
          </span>

          <ul
            id={listId}
            role="listbox"
            aria-label={labels.label}
            style={{ listStyle: "none", margin: 0, padding: 4 }}
          >
            {rows.map((row, index) => (
              <li
                key={`${row.value}:${index}`}
                id={`${listId}-${index}`}
                role="option"
                aria-selected={row.value === value}
                title={hrefFor(row.value)}
                className={`pm-item lt-pf-item${index === highlight ? " on" : ""}`}
                // 44px: a real tap target at phone width.
                style={{ minHeight: 44, cursor: "pointer" }}
                onMouseDown={(event) => {
                  event.preventDefault();
                  pick(row.value, query.trim().length > 0);
                }}
                onMouseEnter={() => setMarker({ query, index })}
              >
                {row.value ? (
                  <span className="lt-avx" aria-hidden="true">
                    {initials(row.label)}
                  </span>
                ) : (
                  <span className="lt-avx" aria-hidden="true">
                    <UserRound className="ic" style={{ width: 13 }} aria-hidden="true" />
                  </span>
                )}
                <span style={{ minWidth: 0, flex: 1 }}>
                  <span className="pn" style={{ display: "block" }}>
                    {row.label}
                  </span>
                  {row.title ? (
                    <span className="muted" style={{ display: "block", fontSize: 11 }}>
                      {row.title}
                    </span>
                  ) : null}
                </span>
                {row.value === value ? (
                  <Check className="ic" style={{ width: 14, color: "var(--brand)" }} aria-hidden="true" />
                ) : null}
              </li>
            ))}
          </ul>
          {matches.length === 0 ? <div className="pm-hint">{labels.noMatchesLabel}</div> : null}
        </div>
      ) : null}
    </div>
  );
}
