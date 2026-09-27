"use client";

import { useId } from "react";
import Checkbox from "@mui/material/Checkbox";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import MenuItem from "@mui/material/MenuItem";
import Select from "@mui/material/Select";
import Box from "@mui/material/Box";

/**
 * The toolbar's people filter as the CEO asked for it (2026-09-18): a dropdown ("Assignee · All")
 * over a list of real checkboxes -- "All" first, then every person. Tick more than one and the URL
 * carries `a,b,c`; the backend matches `= ANY(uuid[])`.
 *
 * Anatomy: the template list toolbar's multi-select filter (sections/user/user-table-toolbar.tsx
 * "Role": outlined FormControl + InputLabel + Select multiple, a Checkbox per MenuItem). The
 * "All" row is the first checkbox row and is ticked while nobody is picked.
 */
export type PeopleDropdownOption = { id: string; name: string; title?: string };

/** A person as the page contract lists them: `value` is the uuid, `label` the name. */
export type TaskPeopleOption = { value: string; label: string; title?: string };

const ALL = "__all";

/** The dropdown's width from md up (its skeleton twins read it: /tasks, /work-board toolbars). */
export const PEOPLE_DROPDOWN_WIDTH = 200;

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
  const inputId = useId();

  const chosen = options.filter((option) => selected.includes(option.id));
  const stated =
    chosen.length === 0
      ? allLabel
      : chosen.length === 1
        ? chosen[0].name
        : `${chosen[0].name} +${chosen.length - 1}`;

  return (
    <FormControl data-slot={slot} sx={{ flexShrink: 0, width: { xs: 1, md: PEOPLE_DROPDOWN_WIDTH } }}>
      <InputLabel htmlFor={inputId}>{label}</InputLabel>
      <Select
        multiple
        label={label}
        value={selected.length ? selected : [ALL]}
        onChange={(event) => {
          const raw = event.target.value;
          const next = (typeof raw === "string" ? raw.split(",") : raw).filter(Boolean);
          // Ticking "All" (it was not ticked before) clears the pick; any person row drops "All".
          if (next.includes(ALL) && selected.length > 0) {
            onChange([]);
            return;
          }
          onChange(next.filter((id) => id !== ALL));
        }}
        renderValue={() => stated}
        inputProps={{ id: inputId }}
        MenuProps={{ slotProps: { paper: { sx: { maxHeight: 320 } } } }}
      >
        <MenuItem value={ALL}>
          <Checkbox disableRipple size="small" checked={selected.length === 0} />
          {allLabel}
        </MenuItem>
        {options.map((option) => (
          <MenuItem key={option.id} value={option.id}>
            <Checkbox disableRipple size="small" checked={selected.includes(option.id)} />
            <Box component="span" sx={{ minWidth: 0, overflow: "hidden", textOverflow: "ellipsis" }}>
              {option.name}
              {option.title ? (
                <Box component="span" sx={{ color: "text.secondary" }}>
                  {" "}
                  — {option.title}
                </Box>
              ) : null}
            </Box>
          </MenuItem>
        ))}
      </Select>
    </FormControl>
  );
}
