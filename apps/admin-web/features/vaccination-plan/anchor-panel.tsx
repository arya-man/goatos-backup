"use client";
import { Iconify } from "@/components/minimal/iconify";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { Fragment, useMemo, useState } from "react";

import { fmtDate, todayIso } from "@/lib/format";
import type { AnchorConfig } from "./editor-model";
import { humanDays, type ScheduleRule, type VaccineGroup } from "./plan-model";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/app/table";

type AnchorState = AnchorConfig;

type RuleRow = {
  vaccine: Pick<VaccineGroup, "code" | "name">;
  rule: ScheduleRule;
};

type Props = {
  catalog?: VaccineGroup[];
  rows?: RuleRow[];
  anchors?: Record<string, AnchorConfig>;
  onChange?: (doseCode: string, anchor: AnchorConfig | null) => void;
};

const DEFAULT_REASON = "Anchor/base date for this vaccine rule";

export function VaccinationAnchorPanel({ catalog = [], rows: providedRows, anchors = {}, onChange }: Props) {
  const catalogRows = useMemo(
    () =>
      catalog
        .filter((vaccine) => vaccine.inPlan)
        .flatMap((vaccine) =>
          [...vaccine.firstDoses, ...vaccine.repeats]
            .filter((rule) => Boolean(rule.dose_code))
            .map((rule) => ({ vaccine, rule })),
        ),
    [catalog],
  );
  const rows = providedRows ?? catalogRows;
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [state, setState] = useState<AnchorState>(() => defaultState());

  function openEditor(row: RuleRow) {
    setEditingKey(rowKey(row));
    const existing = row.rule.dose_code ? anchors[row.rule.dose_code] : undefined;
    setState(existing ?? defaultState());
  }

  function update<K extends keyof AnchorState>(key: K, value: AnchorState[K]) {
    setState((current) => ({ ...current, [key]: value }));
  }

  if (rows.length === 0) return null;

  return (
    <Scrollbar>
      <Table sx={{ minWidth: 680 }}>
        <TableHeadCustom
          headCells={[
            { id: "vaccine", label: "Vaccine" },
            { id: "dose", label: "Rule/dose" },
            { id: "timing", label: "Timing" },
            { id: "repeat", label: "Repeat" },
            { id: "anchor", label: "Anchor/base date" },
            { id: "action", label: "Action", width: 88 },
          ]}
        />
        <TableBody>
          {rows.map((row) => {
            const key = rowKey(row);
            const editing = editingKey === key;
            const anchorDate = row.rule.dose_code ? anchors[row.rule.dose_code]?.anchorDate : "";
            const canSave = isValidIsoDate(state.anchorDate);
            return (
              <Fragment key={key}>
                <TableRow selected={editing} hover>
                  <TableCell sx={{ typography: "subtitle2" }}>{row.vaccine.name}</TableCell>
                  {/* The dose's farm name, never its rule code ("et_tt_kid_4w"). */}
                  <TableCell>{doseLabel(row.rule)}</TableCell>
                  <TableCell>{ruleTiming(row.rule)}</TableCell>
                  <TableCell>{repeatLabel(row.rule)}</TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap", color: anchorDate ? "text.primary" : "text.secondary" }}>{anchorDate ? fmtDate(anchorDate) : "No anchor"}</TableCell>
                  <TableCell>
                    <IconButton
                      color="primary"
                      aria-label={anchorDate ? "Edit anchor" : "Add anchor"}
                      title={anchorDate ? "Edit anchor" : "Add anchor"}
                      onClick={() => openEditor(row)}
                    >
                      <Iconify icon={anchorDate ? "solar:pen-bold" : "mingcute:add-line"} width={18} />
                    </IconButton>
                  </TableCell>
                </TableRow>
                {editing ? (
                  <TableRow key={`${key}-editor`}>
                    <TableCell colSpan={6} sx={{ bgcolor: "background.neutral" }}>
                      <Box sx={{ display: "grid", gap: 2, gridTemplateColumns: { xs: "1fr", md: "repeat(3, minmax(0, 1fr))" }, alignItems: "start" }}>
                        <Box>
                          <ThemedDatePicker
                            name="anchor_date"
                            label="Anchor/base date"
                            value={state.anchorDate}
                            onChange={(key) => update("anchorDate", key)}
                            previousMonthLabel="Previous month"
                            nextMonthLabel="Next month"
                            invalidDateText=""
                          />
                        </Box>
                        <TextField fullWidth label="Reason" value={state.reason} onChange={(event) => update("reason", event.target.value)} />
                        <TextField fullWidth label="Source reference" value={state.sourceRef} onChange={(event) => update("sourceRef", event.target.value)} />
                      </Box>
                      <Box aria-label="Anchor behavior" role="group" sx={{ mt: 1.5, display: "flex", flexWrap: "wrap", columnGap: 2 }}>
                        <FormControlLabel
                          control={<Checkbox checked readOnly sx={{ p: { xs: 1.5, sm: 1 } }} />}
                          label={"Apply anchor to this rule's eligible scope"}
                        />
                        <FormControlLabel
                          control={
                            <Checkbox
                              checked={state.suppressBeforeAnchor}
                              onChange={(event) => update("suppressBeforeAnchor", event.target.checked)}
                              sx={{ p: { xs: 1.5, sm: 1 } }}
                            />
                          }
                          label="Suppress earlier catch-up rows before anchor"
                        />
                        <FormControlLabel
                          control={
                            <Checkbox
                              checked={state.chainFutureFromAnchor}
                              onChange={(event) => update("chainFutureFromAnchor", event.target.checked)}
                              sx={{ p: { xs: 1.5, sm: 1 } }}
                            />
                          }
                          label="Chain boosters/revacs from anchor"
                        />
                        <FormControlLabel
                          control={
                            <Checkbox
                              checked={state.enforceAgeEligibility}
                              onChange={(event) => update("enforceAgeEligibility", event.target.checked)}
                              sx={{ p: { xs: 1.5, sm: 1 } }}
                            />
                          }
                          label="Enforce age eligibility"
                        />
                      </Box>
                      <Box sx={{ mt: 1.5, display: "flex", gap: 1, flexWrap: "wrap" }}>
                        <Button
                          variant="contained"
                          color="primary"
                          disabled={!canSave}
                          onClick={() => {
                            if (!canSave) return;
                            if (row.rule.dose_code) onChange?.(row.rule.dose_code, state);
                            setEditingKey(null);
                          }}
                        >
                          Save anchor to draft
                        </Button>
                        <Button variant="outlined" color="inherit" onClick={() => setEditingKey(null)}>
                          Close
                        </Button>
                      </Box>
                    </TableCell>
                  </TableRow>
                ) : null}
              </Fragment>
            );
          })}
        </TableBody>
      </Table>
    </Scrollbar>
  );
}

function defaultState(): AnchorState {
  return {
    anchorDate: todayIso(),
    reason: DEFAULT_REASON,
    sourceRef: "",
    suppressBeforeAnchor: true,
    chainFutureFromAnchor: true,
    enforceAgeEligibility: true,
  };
}

function isValidIsoDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const date = new Date(`${value}T00:00:00Z`);
  return !Number.isNaN(date.valueOf()) && date.toISOString().slice(0, 10) === value;
}

function rowKey(row: RuleRow): string {
  return `${row.vaccine.code}:${row.rule.dose_code ?? ""}`;
}

function doseLabel(rule: ScheduleRule): string {
  if (rule.repeat && rule.repeat !== "none") return "Repeat";
  return rule.sequence ? `Dose ${rule.sequence}` : "Dose";
}

/** When the dose falls, in the words a person would say it: never the trigger's snake_case key. */
function ruleTiming(rule: ScheduleRule): string {
  const after = rule.offset_days ? humanDays(rule.offset_days) : "";
  switch (rule.trigger_type) {
    case "birth_age":
      return after ? `${after} from date of birth` : "From date of birth";
    case "manual_campaign":
      return "On a drive";
    case "after_previous_completion":
      return after ? `${after} after the previous dose` : "After the previous dose";
    default:
      return after ? `After ${after}` : "Scheduled";
  }
}

function repeatLabel(rule: ScheduleRule): string {
  return rule.repeat && rule.repeat !== "none" ? "Repeats" : "Does not repeat";
}
