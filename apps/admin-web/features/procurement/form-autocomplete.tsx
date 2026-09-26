"use client";

import { useState } from "react";
import TextField from "@mui/material/TextField";
import Autocomplete from "@mui/material/Autocomplete";

import type { FormSelectOption } from "./option-utils";

/**
 * The template `Field.Autocomplete multiple` (components/hook-form/rhf-autocomplete: soft small
 * chips, outlined TextField) wired into a plain `<form>`.
 *
 * The picked values ride on hidden inputs so the server action reads exactly what it read before:
 * `csv` submits one comma-joined field (the old "ids, comma separated" text box), `repeat` submits
 * one field per value (the old one-checkbox-per-option shape).
 */
export function FormAutocomplete({
  label,
  name,
  options,
  defaultValue = [],
  value,
  onValueChange,
  placeholder,
  submit = "csv",
  disabled = false,
  size = "small",
  testId,
}: {
  label: string;
  name?: string;
  options: readonly FormSelectOption[];
  defaultValue?: readonly string[];
  value?: readonly string[];
  onValueChange?: (next: string[]) => void;
  placeholder?: string;
  submit?: "csv" | "repeat";
  disabled?: boolean;
  size?: "small" | "medium";
  testId?: string;
}) {
  const [own, setOwn] = useState<string[]>([...defaultValue]);
  const current = value ? [...value] : own;
  const selected = options.filter((option) => current.includes(option.value));

  function commit(next: string[]) {
    if (value === undefined) setOwn(next);
    onValueChange?.(next);
  }

  return (
    <>
      <Autocomplete
        multiple
        disableCloseOnSelect
        disabled={disabled}
        size={size}
        options={options as FormSelectOption[]}
        value={selected}
        getOptionLabel={(option) => option.label}
        isOptionEqualToValue={(option, picked) => option.value === picked.value}
        onChange={(_event, next) => commit(next.map((option) => option.value))}
        slotProps={{ chip: { size: "small", variant: "soft" } }}
        data-testid={testId}
        renderInput={(params) => (
          <TextField
            {...params}
            label={label}
            placeholder={selected.length === 0 ? placeholder : undefined}
            slotProps={{ ...params.slotProps, inputLabel: { ...params.slotProps?.inputLabel, shrink: true } }}
          />
        )}
      />
      {name ? (
        submit === "csv" ? (
          <input type="hidden" name={name} value={current.join(",")} />
        ) : (
          current.map((picked) => <input key={picked} type="hidden" name={name} value={picked} />)
        )
      ) : null}
    </>
  );
}
