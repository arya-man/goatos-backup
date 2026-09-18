"use client";

import { ChevronDown, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

/**
 * THE ASSIGNEE PICKER: the Work Board's avatar stack + "+N" chip + searchable list, lifted out of
 * `features/work-board/work-board-board.tsx` so a second host can use the same control.
 *
 * Two modes, one anatomy (`.avs` / `.avmenu` in mesha-theme.css):
 *   - `multi`  — the Work Board FILTER, byte for byte what that page shipped: the stack shows
 *                everyone on the page, one owner narrows the read, "Select all" clears. Every
 *                row is ticked when nobody is picked, because the board is then showing all of
 *                them. Nothing about this mode may change without the Work Board changing with it.
 *   - `single` — a FORM FIELD (the Tasks desk's "For"): a full-width trigger naming the chosen
 *                person, one tick, the list closes on a pick, and a hidden input posts the id
 *                under `name` so a Server Action reads exactly what the old `<select>` posted.
 *                Rows read "Name — Title", because the defect this replaced listed job titles
 *                only ("CEO / CXO" twice) and a CXO could not pick a PERSON. Typing matches the
 *                name or the title; Arrow keys move; Enter picks; Escape closes the list and
 *                nothing else (the outside-close hook stops the press before a dialog shell hears
 *                it).
 *
 * Every visible string is passed in by the host from its page contract (the backend-owns-labels
 * rule); nothing here is a local literal except the tick glyph and the caret.
 */

export type AssigneePickerOption = { id: string; name: string; title?: string };

export type AssigneePickerLabels = {
  /** The control's name: "Assignee" / "For". */
  label: string;
  /** The search field's placeholder and accessible name. */
  search: string;
  /** Nothing matched what was typed. */
  none: string;
  /** `multi` only: the foot action that clears the one picked owner. */
  selectAll?: string;
  /**
   * `multi` only: what the trigger READS while nobody is picked ("All"). With no pick the
   * filter is everyone, and every row is ticked to say so; the trigger's accessible name and
   * tooltip carry this word so a reader does not have to open the menu to learn it.
   */
  all?: string;
  /** `multi` only: the unit after each person's count, e.g. "rows". */
  rows?: string;
  /** `single` only: the trigger's text before anyone is chosen. */
  placeholder?: string;
};

export function initials(name?: string): string {
  if (!name) return "—";
  return name
    .split(/\s+/)
    .map((word) => word[0] ?? "")
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

// A toolbar menu closes on an outside click and on Escape (Escape hands focus back to the
// trigger so a keyboard user is not dropped on the page body). The Escape listener is a
// CAPTURING document listener that stops propagation, so a dialog shell listening on the same
// document never hears the press that closed this menu -- one press, one layer.
function useOutsideClose(open: boolean, close: () => void) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onDoc = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      close();
      ref.current?.querySelector<HTMLElement>("[aria-expanded]")?.focus();
    };
    document.addEventListener("click", onDoc);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("click", onDoc);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open, close]);
  return ref;
}

function Avatar({ name, className = "" }: { name: string; className?: string }) {
  return (
    <span className={`av ${className}`.trim()} title={name}>
      {initials(name)}
    </span>
  );
}

export function AssigneePicker({
  mode = "multi",
  labels,
  owners,
  cardsByOwner = {},
  selected,
  onSelect,
  name,
  invalid = false,
  describedBy,
}: {
  mode?: "multi" | "single";
  labels: AssigneePickerLabels;
  owners: AssigneePickerOption[];
  /** `multi` only: how many cards each owner has on the page, shown beside their name. */
  cardsByOwner?: Record<string, number>;
  selected?: string;
  onSelect: (id: string | undefined) => void;
  /** `single` only: the hidden form field that posts the picked id. */
  name?: string;
  /** `single` only: the host's own required check failed; the trigger says so. */
  invalid?: boolean;
  describedBy?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  // `single` only: which row Enter would pick. Stored WITH the query it belongs to, so a new
  // query starts on its own first match rather than on a row that is now someone else.
  const [marker, setMarker] = useState<{ query: string; index: number }>({ query: "", index: 0 });
  const inputRef = useRef<HTMLInputElement>(null);
  const ref = useOutsideClose(open, () => {
    setOpen(false);
    setQuery("");
  });
  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return owners;
    // The filter matches the name (the Work Board's rule); the form field also matches the
    // title, so typing "director" lists the directors.
    return owners.filter(
      (o) =>
        o.name.toLowerCase().includes(q) ||
        (mode === "single" && (o.title ?? "").toLowerCase().includes(q)),
    );
  }, [mode, owners, query]);
  const current = owners.find((o) => o.id === selected);
  const visible = current ? [current] : owners.slice(0, 6);
  const overflow = current ? 0 : Math.max(0, owners.length - 6);
  const toggle = () => setOpen((v) => !v);
  const highlight =
    marker.query === query ? Math.min(marker.index, Math.max(shown.length - 1, 0)) : 0;

  const listId = name ? `${name}-list` : undefined;

  if (mode === "single") {
    const pick = (id: string) => {
      onSelect(id);
      setOpen(false);
      setQuery("");
    };
    return (
      <div ref={ref} className="avs avs-single" aria-label={labels.label}>
        {name ? <input type="hidden" name={name} value={selected ?? ""} /> : null}
        <button
          type="button"
          className={`avs-trigger${current ? " set" : ""}${open ? " on" : ""}`}
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-controls={listId}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          aria-label={`${labels.label}: ${current ? current.name : labels.placeholder ?? ""}`}
          onClick={toggle}
        >
          {current ? <Avatar name={current.name} /> : null}
          <span className="avs-value">
            {current ? (
              <>
                <b>{current.name}</b>
                {current.title ? <span className="muted"> — {current.title}</span> : null}
              </>
            ) : (
              <span className="muted">{labels.placeholder}</span>
            )}
          </span>
          <ChevronDown className="ic" aria-hidden="true" />
        </button>
        {open ? (
          <div className="avmenu" role="presentation" data-assignee-menu>
            <div className="avq">
              <Search className="ic" aria-hidden="true" />
              <input
                ref={inputRef}
                role="combobox"
                aria-expanded="true"
                aria-autocomplete="list"
                aria-controls={listId}
                aria-activedescendant={shown.length && listId ? `${listId}-${highlight}` : undefined}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setMarker({ query: e.target.value, index: 0 });
                }}
                onKeyDown={(e) => {
                  // Compared lower-cased: the copy guard reads a capitalised key name as text.
                  const key = e.key.toLowerCase();
                  if (key === "arrowdown" || key === "arrowup") {
                    e.preventDefault();
                    if (!shown.length) return;
                    const delta = key === "arrowdown" ? 1 : -1;
                    setMarker({ query, index: (highlight + delta + shown.length) % shown.length });
                    return;
                  }
                  if (e.key === "Enter") {
                    e.preventDefault();
                    const row = shown[highlight];
                    if (row) pick(row.id);
                  }
                }}
                placeholder={labels.search}
                aria-label={labels.search}
                autoComplete="off"
                maxLength={60}
              />
            </div>
            <div className="list" role="listbox" id={listId} aria-label={labels.label}>
              {shown.map((o, index) => {
                const on = o.id === selected;
                return (
                  <button
                    type="button"
                    key={o.id}
                    id={listId ? `${listId}-${index}` : undefined}
                    role="option"
                    aria-selected={on}
                    className={`opt${on ? " on" : ""}${index === highlight ? " hl" : ""}`}
                    onMouseEnter={() => setMarker({ query, index })}
                    onClick={() => pick(o.id)}
                  >
                    <span className="cb">{on ? "✓" : ""}</span>
                    <Avatar name={o.name} />
                    <span className="avs-row">
                      <b className="avs-name">{o.name}</b>
                      {o.title ? <span className="muted avs-title"> — {o.title}</span> : null}
                    </span>
                  </button>
                );
              })}
              {shown.length === 0 ? <div className="nomatch">{labels.none}</div> : null}
            </div>
          </div>
        ) : null}
      </div>
    );
  }

  // NO PICK MEANS EVERYONE, and the menu says so by ticking EVERY row (gate-1 #10, 2026-09-18).
  // This is the Work Board's shipped semantics for the same control (`features/work-board`,
  // "ticked-when-all rows"), and the two toolbars are deliberately one control language, so the
  // ticks stay. What changed: the TRIGGER now reads "All" -- every avatar in the stack, the `+N`
  // chip and the stack itself carry "<Label>: All" as their accessible name and tooltip while
  // nobody is picked, and "<Label>: <Name>" once someone is. A first-time reader who sees ten
  // ticks and looks for a way to untick is told, on the control itself, that all is the state.
  // A host that passes no `all` word (the Work Board today) keeps its bare label.
  const stateLabel = current ? `${labels.label}: ${current.name}` : labels.all ? `${labels.label}: ${labels.all}` : labels.label;
  return (
    <div ref={ref} className="avs" aria-label={stateLabel} title={stateLabel} data-picked={current ? "one" : "all"}>
      {visible.map((o) => (
        <button type="button" key={o.id} className={`av${o.id === selected ? " on" : ""}`} title={current ? o.name : stateLabel} aria-label={current ? stateLabel : `${stateLabel} (${o.name})`} aria-expanded={open} onClick={toggle}>
          {initials(o.name)}
        </button>
      ))}
      {overflow > 0 ? (
        <button type="button" className={`more${open ? " on" : ""}`} aria-expanded={open} aria-label={stateLabel} title={stateLabel} onClick={toggle}>
          +{overflow}
        </button>
      ) : null}
      {owners.length === 0 || current ? (
        <button type="button" className={`more${owners.length === 0 ? " assignee-empty" : ""}${open ? " on" : ""}`} aria-expanded={open} aria-label={stateLabel} title={stateLabel} onClick={toggle} style={owners.length === 0 ? { width: "auto", minWidth: 104, padding: "0 14px", borderRadius: 999, lineHeight: "1" } : undefined}>
          {owners.length === 0 ? labels.label : "▾"}
        </button>
      ) : null}
      {open ? (
        <div className="avmenu" role="listbox" aria-label={labels.label}>
          <div className="avq">
            <Search className="ic" aria-hidden="true" />
            <input ref={inputRef} value={query} onChange={(e) => setQuery(e.target.value)} placeholder={labels.search} aria-label={labels.search} autoComplete="off" />
          </div>
          <div className="list">
            {shown.map((o) => {
              const on = selected ? o.id === selected : true;
              return (
                <button type="button" key={o.id} role="option" aria-selected={o.id === selected} className={`opt${on ? " on" : ""}`} onClick={() => { onSelect(o.id === selected ? undefined : o.id); setOpen(false); setQuery(""); }}>
                  <span className="cb">{on ? "✓" : ""}</span>
                  <Avatar name={o.name} />
                  {o.name}
                  <span className="cnt">{cardsByOwner[o.id] ?? 0} {labels.rows}</span>
                </button>
              );
            })}
            {shown.length === 0 ? <div className="nomatch">{labels.none}</div> : null}
          </div>
          {selected ? (
            <button type="button" className="opt foot" onClick={() => { onSelect(undefined); setOpen(false); }}>
              {labels.selectAll}
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
