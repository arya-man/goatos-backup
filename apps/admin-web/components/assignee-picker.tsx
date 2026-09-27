"use client";

import { ChevronDown } from "lucide-react";
import { useMemo, useState } from "react";
import { usePopover } from "minimal-shared/hooks";
import Box from "@mui/material/Box";
import Checkbox from "@mui/material/Checkbox";
import InputAdornment from "@mui/material/InputAdornment";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import type { Theme } from "@mui/material/styles";
import { Avatar } from "@/components/app/avatar";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { Iconify } from "@/components/minimal/iconify";
import { TAP_MIN } from "@/components/app/tap";

/**
 * THE ASSIGNEE PICKER: the Work Board's avatar stack + "+N" chip + searchable list, lifted out of
 * `features/work-board/work-board-board.tsx` so a second host can use the same control.
 *
 * Two modes, one anatomy: the `.avs` trigger (mesha-theme.css) opens the template menu popover
 * (CustomPopover: search TextField + MenuList rows). MUI portals it, keeps it in the viewport,
 * closes it on an outside click and on Escape (its handler stops the press, so a dialog shell on
 * the same document never hears it -- one press, one layer) and returns focus to the trigger.
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
 *                nothing else.
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

/** Phone tap floor for popover rows (webview rule: >=44px). */
function tapRow(theme: Theme) {
  return { gap: 1.25, [theme.breakpoints.down("sm")]: { minHeight: TAP_MIN } };
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
  stackSize = 6,
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
  /**
   * `multi` only: how many avatars the closed trigger shows before the `+N` (the Work Board's
   * stack is 6). The Tasks toolbar shows 4 at 28px so its two stacks stay legible beside a
   * five-chip segment at 1440 (Gate-1 #6); the picker list is the full roster either way.
   */
  stackSize?: number;
}) {
  const menu = usePopover();
  const open = menu.open;
  const [query, setQuery] = useState("");
  // `single` only: which row Enter would pick. Stored WITH the query it belongs to, so a new
  // query starts on its own first match rather than on a row that is now someone else.
  const [marker, setMarker] = useState<{ query: string; index: number }>({ query: "", index: 0 });
  const close = () => {
    menu.onClose();
    setQuery("");
  };
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
  const visible = current ? [current] : owners.slice(0, stackSize);
  const overflow = current ? 0 : Math.max(0, owners.length - stackSize);
  const toggle = (event: React.MouseEvent<HTMLElement>) => (open ? close() : menu.onOpen(event));
  const highlight =
    marker.query === query ? Math.min(marker.index, Math.max(shown.length - 1, 0)) : 0;

  const listId = name ? `${name}-list` : undefined;

  if (mode === "single") {
    const pick = (id: string) => {
      onSelect(id);
      close();
    };
    return (
      <div className="avs avs-single" aria-label={labels.label}>
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
          {current ? <Avatar name={current.name} initials={initials(current.name)} size={24} decorative /> : null}
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
        <CustomPopover
          open={open}
          anchorEl={menu.anchorEl}
          onClose={close}
          slotProps={{ arrow: { placement: "top-left" }, paper: { sx: { width: 320 } } }}
        >
          <Box sx={{ p: 1 }}>
            <TextField
              fullWidth
              size="small"
              autoFocus
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
              autoComplete="off"
              slotProps={{
                htmlInput: {
                  role: "combobox",
                  "aria-expanded": true,
                  "aria-autocomplete": "list",
                  "aria-controls": listId,
                  "aria-activedescendant": shown.length && listId ? `${listId}-${highlight}` : undefined,
                  "aria-label": labels.search,
                  maxLength: 60,
                },
                input: { startAdornment: <InputAdornment position="start"><Iconify icon="eva:search-fill" /></InputAdornment> },
              }}
            />
          </Box>
          <MenuList role="listbox" id={listId} aria-label={labels.label} sx={{ maxHeight: 320, overflowY: "auto", overscrollBehavior: "contain" }}>
            {shown.map((o, index) => {
              const on = o.id === selected;
              return (
                <MenuItem
                  key={o.id}
                  id={listId ? `${listId}-${index}` : undefined}
                  role="option"
                  aria-selected={on}
                  selected={on || index === highlight}
                  onMouseEnter={() => setMarker({ query, index })}
                  onClick={() => pick(o.id)}
                  sx={tapRow}
                >
                  <Avatar name={o.name} initials={initials(o.name)} size={24} decorative />
                  <Typography variant="body2" component="span" sx={{ minWidth: 0, overflowWrap: "anywhere", whiteSpace: "normal" }}>
                    <b className="avs-name">{o.name}</b>
                    {o.title ? <Box component="span" className="avs-title" sx={{ color: "text.secondary" }}> — {o.title}</Box> : null}
                  </Typography>
                  {on ? <Iconify icon="eva:checkmark-fill" sx={{ ml: "auto", color: "primary.main", flex: "none" }} /> : null}
                </MenuItem>
              );
            })}
            {shown.length === 0 ? (
              <Typography component="li" variant="body2" sx={{ px: 1, py: 1, color: "text.secondary" }}>{labels.none}</Typography>
            ) : null}
          </MenuList>
        </CustomPopover>
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
    <div className="avs" aria-label={stateLabel} title={stateLabel} data-picked={current ? "one" : "all"}>
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
      <CustomPopover open={open} anchorEl={menu.anchorEl} onClose={close} slotProps={{ arrow: { placement: "top-left" }, paper: { sx: { width: 300 } } }}>
        <Box sx={{ p: 1 }}>
          <TextField
            fullWidth
            size="small"
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={labels.search}
            autoComplete="off"
            slotProps={{
              htmlInput: { "aria-label": labels.search },
              input: { startAdornment: <InputAdornment position="start"><Iconify icon="eva:search-fill" /></InputAdornment> },
            }}
          />
        </Box>
        <MenuList role="listbox" aria-label={labels.label} sx={{ maxHeight: 320, overflowY: "auto", overscrollBehavior: "contain" }}>
          {/* "Select all" is the FIRST row, a checkbox like the rest: ticked while nobody is
              picked (the board shows everyone), and clicking it clears a pick. */}
          {query.trim() === "" ? (
            <MenuItem role="option" aria-selected={!selected} className="all" onClick={() => { onSelect(undefined); close(); }} sx={tapRow}>
              <Checkbox size="small" checked={!selected} disableRipple tabIndex={-1} sx={{ p: 0 }} slotProps={{ input: { "aria-hidden": true } }} />
              {labels.selectAll}
            </MenuItem>
          ) : null}
          {shown.map((o) => {
            const on = selected ? o.id === selected : true;
            return (
              <MenuItem key={o.id} role="option" aria-selected={o.id === selected} onClick={() => { onSelect(o.id === selected ? undefined : o.id); close(); }} sx={tapRow}>
                <Checkbox size="small" checked={on} disableRipple tabIndex={-1} sx={{ p: 0 }} slotProps={{ input: { "aria-hidden": true } }} />
                <Avatar name={o.name} initials={initials(o.name)} size={24} decorative />
                <Box component="span" sx={{ minWidth: 0, flex: 1, overflowWrap: "anywhere", whiteSpace: "normal" }}>{o.name}</Box>
                <Typography variant="caption" sx={{ color: "text.secondary", flex: "none" }}>{cardsByOwner[o.id] ?? 0} {labels.rows}</Typography>
              </MenuItem>
            );
          })}
          {shown.length === 0 ? (
            <Typography component="li" variant="body2" sx={{ px: 1, py: 1, color: "text.secondary" }}>{labels.none}</Typography>
          ) : null}
        </MenuList>
      </CustomPopover>
    </div>
  );
}
