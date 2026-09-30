"use client";

import { useState } from "react";

import Box from "@mui/material/Box";
import Select from "@mui/material/Select";
import MenuItem from "@mui/material/MenuItem";
import Checkbox from "@mui/material/Checkbox";
import InputLabel from "@mui/material/InputLabel";
import FormControl from "@mui/material/FormControl";

type SubcategoryOption = {
  category: string;
  label: string;
};

/**
 * The selected module's subcategories as the template list toolbar's multi-select (InvoiceTableToolbar
 * "Service": FormControl + Select multiple + Checkbox rows). The choice rides the queue filter form as
 * one hidden `category` input per pick, so Apply submits exactly what the old chip toggles did.
 */
export function SubcategoryFilter({
  ariaLabel,
  label,
  options,
  selectedCategories,
}: {
  ariaLabel: string;
  label: string;
  options: SubcategoryOption[];
  selectedCategories: string[];
}) {
  const [selected, setSelected] = useState<string[]>(() => [...new Set(selectedCategories)]);
  const labelOf = new Map(options.map((option) => [option.category, option.label]));

  return (
    <FormControl sx={{ flexShrink: 0, width: { xs: 1, md: 200 } }}>
      <InputLabel htmlFor="vr-subcategory-select">{label}</InputLabel>
      <Select
        multiple
        label={label}
        value={selected}
        onChange={(event) => {
          const value = event.target.value;
          setSelected(typeof value === "string" ? value.split(",") : value);
        }}
        renderValue={(picked) => picked.map((category) => labelOf.get(category) ?? category).join(", ")}
        inputProps={{ id: "vr-subcategory-select", "aria-label": ariaLabel }}
        MenuProps={{ slotProps: { paper: { sx: { maxHeight: 240 } } } }}
      >
        {options.map((option) => (
          <MenuItem key={option.category} value={option.category}>
            <Checkbox disableRipple size="small" checked={selected.includes(option.category)} slotProps={{ input: { id: `${option.category}-checkbox` } }} />
            {option.label}
          </MenuItem>
        ))}
      </Select>
      {selected.map((category) => (
        <Box component="input" key={category} type="hidden" name="category" value={category} />
      ))}
    </FormControl>
  );
}
