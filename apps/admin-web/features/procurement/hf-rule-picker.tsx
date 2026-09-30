"use client";

import { useState } from "react";
import Grid from "@mui/material/Grid";
import TextField from "@mui/material/TextField";

import { FormSelect } from "./form-select";
import { HiddenField } from "@/components/app/hidden-field";

export type HfRuleOption = {
  protocolVersionId: string;
  ruleId: string;
  doseCode: string;
  label: string;
};

/**
 * Holding-farm vaccination evidence: the published schedule dose is picked from a list instead of
 * typing two UUIDs. The chosen dose fills `protocol_version_id` + `rule_id` (hidden, as the server
 * action reads them) and pre-fills the dose code, which stays editable.
 */
export function HfRulePicker({
  label,
  doseLabel,
  dosePlaceholder,
  emptyLabel,
  options,
}: {
  label: string;
  doseLabel: string;
  dosePlaceholder: string;
  emptyLabel: string;
  options: readonly HfRuleOption[];
}) {
  const [picked, setPicked] = useState(options[0] ? `${options[0].protocolVersionId}|${options[0].ruleId}` : "");
  const [dose, setDose] = useState(options[0]?.doseCode ?? "");
  const current = options.find((option) => `${option.protocolVersionId}|${option.ruleId}` === picked);

  return (
    <>
      <Grid size={{ xs: 12, md: 6 }}>
        <FormSelect
          size="small"
          fullWidth
          label={label}
          required
          value={picked}
          disabled={options.length === 0}
          onValueChange={(next) => {
            setPicked(next);
            const option = options.find((row) => `${row.protocolVersionId}|${row.ruleId}` === next);
            if (option) setDose(option.doseCode);
          }}
          options={
            options.length === 0
              ? [{ value: "", label: emptyLabel }]
              : options.map((option) => ({ value: `${option.protocolVersionId}|${option.ruleId}`, label: option.label }))
          }
        />
        <HiddenField name="protocol_version_id" value={current?.protocolVersionId ?? ""} />
        <HiddenField name="rule_id" value={current?.ruleId ?? ""} />
      </Grid>
      <Grid size={{ xs: 12, md: 6 }}>
        <TextField
          fullWidth
          size="small"
          name="dose_code"
          required
          label={doseLabel}
          placeholder={dosePlaceholder}
          value={dose}
          onChange={(event) => setDose(event.target.value)}
          slotProps={{ inputLabel: { shrink: true } }}
        />
      </Grid>
    </>
  );
}
