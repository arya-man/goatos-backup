"use client";

import { useMemo, useState, useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Checkbox from "@mui/material/Checkbox";
import FormControl from "@mui/material/FormControl";
import InputLabel from "@mui/material/InputLabel";
import ListSubheader from "@mui/material/ListSubheader";
import MenuItem from "@mui/material/MenuItem";
import Select from "@mui/material/Select";
import Button from "@mui/material/Button";
import { FilterBar } from "@/components/app/filter-bar";
import TextField from "@mui/material/TextField";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { breakdownFilterQuery } from "./counts-breakdown-query";

// Server-side filtering for the Counts Breakdown census, applied the template UserTableToolbar way:
// no Apply button (TR2-P1-9; guard: breakdown-filters-no-apply). A single select (Farm, Gender)
// applies on pick. Stage, Breed and Shed are MULTI-SELECT (checkbox dropdowns, repeated URL params,
// OR within a dimension): ticks stay STAGED while the menu is open (the 2026-09-03 instruction that
// one tick must not reload the census) and apply once when the menu closes. Each apply rewrites
// every filter param at once, preserving every other param (date scope, page size). Farm writes the
// SHARED `park` parameter (with its scope_mode), because the top-bar park chip is hidden on this
// page and this control is the park control; a page-private key would strand the choice here. And
// the filtering itself still happens server-side on the next render — never client-side row
// hiding.
//
// Every option list is passed in from the server component. Farm, stage, breed AND shed all come
// from the breakdown response's own `facets`, which reports the values actually present, so a
// dropdown can never offer an option matching zero rows. Sheds carry a composite `key`
// (park_id + shed_id) because shed names repeat across parks.

export type BreakdownFilterOption = {
  value: string;
  label: string;
  /**
   * Optional distinct React key. `value` is what the backend receives, but some vocabularies
   * (sheds) share a value shape across scopes and need a composite key (park_id + shed_id) so
   * two same-named entries never collapse. Falls back to `value` when absent.
   */
  key?: string;
  /**
   * Optional group heading. Consecutive options sharing a group render under one heading;
   * options with no group render loose. Used by the Shed vocabulary to file a subdivided shed's
   * pens under the shed itself instead of listing 148 flat rows.
   */
  group?: string;
};

export type BreakdownFilterField = {
  /** URL param name, e.g. "bd_stage", or PARK_PARAM for the Farm control. */
  param: string;
  label: string;
  /** The currently APPLIED selection (from the URL). Single-select fields carry 0 or 1 entry. */
  values: string[];
  options: BreakdownFilterOption[];
  /** Multi-select fields render a checkbox dropdown; the rest render a native single select. */
  multi?: boolean;
  /** When set, the control renders disabled and shows this as its reason. */
  disabledReason?: string;
};

/**
 * Split an option list into consecutive same-group runs, preserving order.
 *
 * Deliberately CONSECUTIVE rather than gathered by name: the option order is the vocabulary's own
 * (sheds arrive grouped already), and re-gathering would silently reorder the list and merge two
 * same-named groups from different parks — the exact name-keyed merge the operational-location
 * convention bans. Two runs may therefore share a heading; they stay separate groups.
 */
function groupRuns(options: BreakdownFilterOption[]): { group: string | undefined; options: BreakdownFilterOption[] }[] {
  const runs: { group: string | undefined; options: BreakdownFilterOption[] }[] = [];
  for (const option of options) {
    const last = runs[runs.length - 1];
    if (last && last.group === option.group) last.options.push(option);
    else runs.push({ group: option.group, options: [option] });
  }
  return runs;
}

/**
 * One multi-select filter: a button summarizing the staged selection, opening a checkbox
 * dropdown. Selection edits reach the parent's staged state; closing the menu navigates.
 */
function MultiSelectFilter({
  field,
  selected,
  allLabel,
  selectedSuffix,
  onToggle,
  onClose,
}: {
  field: BreakdownFilterField;
  selected: string[];
  allLabel: string;
  selectedSuffix: string;
  onToggle: (value: string) => void;
  /** The menu closed: apply the staged ticks. */
  onClose: () => void;
}) {
  const selectedSet = new Set(selected);
  // 0 selected reads "All" (the same sentinel the single selects use); 1 selected shows the
  // option's own label (data, not composed copy); more show the count with the backend-owned
  // suffix ("3 selected").
  const summary =
    selected.length === 0
      ? allLabel
      : selected.length === 1
        ? (field.options.find((option) => option.value === selected[0])?.label ?? selected[0])
        : `${selected.length} ${selectedSuffix}`;

  // Template UserTableToolbar role filter: FormControl + multi Select whose MenuItems carry a
  // Checkbox; group runs render as ListSubheader. A tick only stages; closing the menu applies.
  const items: React.ReactNode[] = [];
  groupRuns(field.options).forEach((run, runIndex) => {
    if (run.group !== undefined) items.push(<ListSubheader key={`g:${runIndex}`}>{run.group}</ListSubheader>);
    for (const option of run.options) {
      items.push(
        <MenuItem key={option.key ?? option.value} value={option.value}>
          <Checkbox disableRipple size="small" checked={selectedSet.has(option.value)} slotProps={{ input: { "aria-label": option.label } }} />
          {option.label}
        </MenuItem>,
      );
    }
  });

  return (
    <FormControl className="cb-multi" disabled={Boolean(field.disabledReason)} title={field.disabledReason || undefined} sx={{ minWidth: { xs: 0, sm: 170 }, flexShrink: 0 }}>
      <InputLabel shrink>{field.label}</InputLabel>
      <Select
        multiple
        displayEmpty
        notched
        label={field.label}
        value={selected}
        renderValue={() => summary}
        onClose={onClose}
        onChange={(event) => {
          const next = typeof event.target.value === "string" ? event.target.value.split(",") : event.target.value;
          const toggled = next.find((value) => !selectedSet.has(value)) ?? selected.find((value) => !next.includes(value));
          if (toggled !== undefined) onToggle(toggled);
        }}
        inputProps={{ "aria-label": field.label }}
        MenuProps={{ slotProps: { paper: { sx: { maxHeight: 300, maxWidth: 320 } } } }}
      >
        {items}
      </Select>
    </FormControl>
  );
}

export function CountsBreakdownFilters({
  fields,
  penParks = {},
  pageContract,
}: {
  fields: BreakdownFilterField[];
  /** Pen value -> park id for every park, so a farm change keeps the new farm's pens. */
  penParks?: Readonly<Record<string, string>>;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const routerSearchParams = useSearchParams();
  const current = routerSearchParams?.toString() ?? "";
  const serverValues = useMemo(
    () => Object.fromEntries(fields.map((field) => [field.param, field.values])),
    [fields],
  );
  // Staged (not-yet-applied) selections. Keyed to the URL they were staged FROM, so a completed
  // navigation (ours or anything else's) resets the staging to what the server actually applied —
  // the same idiom the previous optimistic single-select bar used.
  const [staged, setStaged] = useState<{ from: string; values: Record<string, string[]> } | null>(null);

  const allLabel = copy(pageContract, "filter.all_option");
  const selectedSuffix = copy(pageContract, "filter.selected_count");
  const stagedValues = staged?.from === current ? staged.values : null;
  const fieldValues = (field: BreakdownFilterField) => stagedValues?.[field.param] ?? field.values;
  const hasAnySelection = fields.some((field) => fieldValues(field).length > 0);

  function stage(param: string, values: string[]) {
    setStaged({ from: current, values: { ...serverValues, ...stagedValues, [param]: values } });
  }

  function navigateWith(values: Record<string, string[]>) {
    const qs = breakdownFilterQuery(current, fields.map((field) => field.param), values, penParks);
    startTransition(() => {
      router.replace(qs ? `/counts/breakdown?${qs}` : "/counts/breakdown", { scroll: false });
    });
  }

  /** Apply the staged selection (a multi-select menu closed); no-op when nothing changed. */
  function applyStaged() {
    if (!stagedValues) return;
    const values: Record<string, string[]> = {};
    for (const field of fields) values[field.param] = fieldValues(field);
    const same = fields.every((field) => values[field.param].join("\u0000") === field.values.join("\u0000"));
    if (!same) navigateWith(values);
  }

  /** A single select applies on pick (template toolbar), keeping any other staged ticks. */
  function applyNow(param: string, next: string[]) {
    const values: Record<string, string[]> = {};
    for (const field of fields) values[field.param] = field.param === param ? next : fieldValues(field);
    setStaged({ from: current, values });
    navigateWith(values);
  }

  function clearAll() {
    const cleared: Record<string, string[]> = {};
    for (const field of fields) cleared[field.param] = [];
    setStaged({ from: current, values: cleared });
    navigateWith(cleared);
  }

  // Single-select fields (Farm, Gender) use the MUI TextField select: same reported value as the native
  // control it replaces, so the staged-apply logic above is untouched. The multi-select fields keep
  // their checkbox dropdown - a listbox cannot express "three sheds OR-ed".
  return (
    <FilterBar
      className="counts-breakdown-filterbar"
      actions={
        <>
          {hasAnySelection ? (
            <Button color="primary" type="button" variant="text" size="small" onClick={clearAll} disabled={isPending}>
              {copy(pageContract, "filter.clear_all")}
            </Button>
          ) : null}
        </>
      }
    >
      <div role="group" aria-label={copy(pageContract, "filter.bar_aria")} style={{ display: "contents" }}>
      {fields.map((field) =>
        field.multi ? (
          <MultiSelectFilter
              key={field.param}
              field={field}
              selected={fieldValues(field)}
              allLabel={allLabel}
              selectedSuffix={selectedSuffix}
              onToggle={(value) => {
                const currentValues = fieldValues(field);
                stage(
                  field.param,
                  currentValues.includes(value)
                    ? currentValues.filter((entry) => entry !== value)
                    : [...currentValues, value],
                );
              }}
              onClose={applyStaged}
            />
        ) : (
          <TextField
            key={field.param}
            select
            label={field.label}
            value={field.options.some((option) => option.value === fieldValues(field)[0]) ? fieldValues(field)[0] : ""}
            disabled={Boolean(field.disabledReason)}
            title={field.disabledReason || (isPending ? copy(pageContract, "state.loading") : undefined)}
            onChange={({ target: { value } }) => applyNow(field.param, value ? [value] : [])}
            sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
          >
            <MenuItem value="">{allLabel}</MenuItem>
            {field.options.map((option) => (
              <MenuItem key={option.value} value={option.value}>
                {option.label}
              </MenuItem>
            ))}
          </TextField>
        ),
      )}
      </div>
    </FilterBar>
  );
}
