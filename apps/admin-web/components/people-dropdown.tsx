"use client";

import { ChevronDown } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import { DropdownPaper } from "@/components/app/dropdown-paper";

/**
 * The toolbar's people filter as the CEO asked for it (2026-09-18): a plain dropdown button
 * ("Assignee · All") that opens a list of real checkboxes -- "All" first, then every person.
 * Tick more than one and the URL carries `a,b,c`; the backend matches `= ANY(uuid[])`.
 *
 * No avatar stack, no overflow chip, no custom square standing in for a checkbox: the box IS
 * the tick, drawn with the same icon the rest of the console uses.
 *
 * The list is the template dropdown paper IN PLACE (not a portalled popover): inside the phone
 * filter sheet it opens in flow, full width, so the sheet's own scroll reaches every row.
 */
export type PeopleDropdownOption = { id: string; name: string; title?: string };

/** A person as the page contract lists them: `value` is the uuid, `label` the name. */
export type TaskPeopleOption = { value: string; label: string; title?: string };

export function TaskPeopleDropdown({
  label,
  allLabel,
  options,
  selected,
  onChange,
  slot,
}: {
  label: string;
  allLabel: string;
  options: PeopleDropdownOption[];
  /** Selected ids, in URL order. Empty means "All". */
  selected: string[];
  onChange: (next: string[]) => void;
  slot: "assignee" | "raiser";
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const listID = useId();

  useEffect(() => {
    if (!open) return;
    const onDoc = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation();
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey, true);
    };
  }, [open]);

  const chosen = options.filter((option) => selected.includes(option.id));
  const stated =
    chosen.length === 0
      ? allLabel
      : chosen.length === 1
        ? chosen[0].name
        : `${chosen[0].name} +${chosen.length - 1}`;

  const toggle = (id: string) => {
    const next = selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
    onChange(next);
  };

  return (
    <div className="lt-fdrop lt-people" ref={ref} data-slot={slot}>
      <button
        type="button"
        aria-expanded={open}
        aria-haspopup="true"
        aria-controls={listID}
        onClick={() => setOpen((current) => !current)}
      >
        <span className="lt-fkey">{label}</span>
        <span className="lt-people-stated">{stated}</span>
        <ChevronDown className="ic" style={{ width: 13 }} aria-hidden="true" />
      </button>
      {open ? (
        <DropdownPaper className="lt-fdrop-pop lt-people-pop" role="group" id={listID} aria-label={label}>
          <FormControlLabel
            className={`lt-people-row${selected.length === 0 ? " is-on" : ""}`}
            control={
              <Checkbox
                checked={selected.length === 0}
                onChange={() => {
                  onChange([]);
                  setOpen(false);
                }}
                sx={{ p: { xs: 1.5, sm: 1 } }}
              />
            }
            label={<span className="lt-people-name">{allLabel}</span>}
          />
          {options.map((option) => {
            const on = selected.includes(option.id);
            return (
              <FormControlLabel
                key={option.id}
                className={`lt-people-row${on ? " is-on" : ""}`}
                control={<Checkbox checked={on} onChange={() => toggle(option.id)} sx={{ p: { xs: 1.5, sm: 1 } }} />}
                label={
                  <span className="lt-people-name">
                    {option.name}
                    {option.title ? <span className="lt-people-title"> — {option.title}</span> : null}
                  </span>
                }
              />
            );
          })}
        </DropdownPaper>
      ) : null}
    </div>
  );
}
