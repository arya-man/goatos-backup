"use client";

import { useCallback, useState } from "react";
import { Plus } from "lucide-react";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Autocomplete from "@mui/material/Autocomplete";

import { MinimalDrawer } from "@/components/minimal/drawer";
import { useBackCloses } from "@/components/use-back-closes";

/**
 * Source entry's header "New load" primary and the template right drawer it opens (portal,
 * backdrop, full width on phones). The form inside is server-rendered and passed as children, so
 * its server action and idempotency key are exactly what the inline form carried.
 */
export function NewLoadDrawer({ label, closeLabel, children }: { label: string; closeLabel: string; children: React.ReactNode }) {
  const [open, setOpen] = useState(false);
  const close = useCallback(() => setOpen(false), []);
  useBackCloses(open, close);

  return (
    <>
      <Button
        type="button"
        variant="contained"
        color="primary"
        startIcon={<Plus className="ic" aria-hidden="true" />}
        onClick={() => setOpen(true)}
        data-testid="new-load-open"
      >
        {label}
      </Button>
      <MinimalDrawer open={open} onClose={close} title={label} closeLabel={closeLabel} width={380} keepMounted>
        {children}
      </MinimalDrawer>
    </>
  );
}

export type SupplierOption = { id: string; name: string };

/**
 * Supplier for a new load: picked by name from the suppliers this desk already buys from (every
 * load on the board names its source party), or pasted as an id for a supplier with no load yet.
 * The server action still reads `source_party_id`.
 */
export function SupplierField({ label, placeholder, options }: { label: string; placeholder: string; options: readonly SupplierOption[] }) {
  const [value, setValue] = useState<SupplierOption | string | null>(null);
  const submitted = value === null ? "" : typeof value === "string" ? value.trim() : value.id;
  return (
    <>
      <Autocomplete
        freeSolo
        size="small"
        options={options as SupplierOption[]}
        value={value}
        getOptionLabel={(option) => (typeof option === "string" ? option : option.name)}
        isOptionEqualToValue={(option, picked) => typeof picked !== "string" && option.id === picked.id}
        onChange={(_event, next) => setValue(next)}
        onInputChange={(_event, text, reason) => {
          if (reason === "input") setValue(text);
        }}
        renderInput={(params) => (
          <TextField
            {...params}
            required
            label={label}
            placeholder={placeholder}
            slotProps={{ ...params.slotProps, inputLabel: { ...params.slotProps?.inputLabel, shrink: true } }}
          />
        )}
      />
      <input type="hidden" name="source_party_id" value={submitted} />
    </>
  );
}
