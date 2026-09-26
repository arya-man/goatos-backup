"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { Fragment, useMemo, useState } from "react";
import { Pencil, Plus } from "lucide-react";

import { fmtDate, todayIso } from "@/lib/format";
import type { AnchorConfig } from "./editor-model";
import { humanDays, type ScheduleRule, type VaccineGroup } from "./plan-model";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

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
    <div className="scroll anchor-rule-table" tabIndex={0}>
      <Table className="tabl">
        <TableHead>
          <TableRow>
            <TableCell component="th">Vaccine</TableCell>
            <TableCell component="th">Rule/dose</TableCell>
            <TableCell component="th">Timing</TableCell>
            <TableCell component="th">Repeat</TableCell>
            <TableCell component="th">Anchor/base date</TableCell>
            <TableCell component="th">Action</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((row) => {
            const key = rowKey(row);
            const editing = editingKey === key;
            const anchorDate = row.rule.dose_code ? anchors[row.rule.dose_code]?.anchorDate : "";
            const canSave = isValidIsoDate(state.anchorDate);
            return (
              <Fragment key={key}>
                <TableRow className={editing ? "selrow" : undefined}>
                  <TableCell>
                    <b>{row.vaccine.name}</b>
                  </TableCell>
                  {/* The dose's farm name, never its rule code ("et_tt_kid_4w"). */}
                  <TableCell>{doseLabel(row.rule)}</TableCell>
                  <TableCell>{ruleTiming(row.rule)}</TableCell>
                  <TableCell>{repeatLabel(row.rule)}</TableCell>
                  <TableCell>{anchorDate ? fmtDate(anchorDate) : "No anchor"}</TableCell>
                  <TableCell>
                    <button
                      className="btn ghost sm icon"
                      type="button"
                      aria-label={anchorDate ? "Edit anchor" : "Add anchor"}
                      title={anchorDate ? "Edit anchor" : "Add anchor"}
                      onClick={() => openEditor(row)}
                    >
                      {anchorDate ? <Pencil size={18} strokeWidth={2.6} aria-hidden /> : <Plus size={18} strokeWidth={2.8} aria-hidden />}
                    </button>
                  </TableCell>
                </TableRow>
                {editing ? (
                  <TableRow className="anchoredit" key={`${key}-editor`}>
                    <TableCell colSpan={6}>
                      <div className="anchorform inline">
                        <div className="field">
                          <span>Anchor/base date</span>
                          <ThemedDatePicker
                            name="anchor_date"
                            label="Anchor/base date"
                            value={state.anchorDate}
                            onChange={(key) => update("anchorDate", key)}
                            previousMonthLabel="Previous month"
                            nextMonthLabel="Next month"
                            invalidDateText=""
                          />
                        </div>
                        <label className="field">
                          <span>Reason</span>
                          <input value={state.reason} onChange={(event) => update("reason", event.target.value)} />
                        </label>
                        <label className="field">
                          <span>Source reference</span>
                          <input value={state.sourceRef} onChange={(event) => update("sourceRef", event.target.value)} />
                        </label>
                      </div>
                      <div className="anchorflags" aria-label="Anchor behavior">
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
                      </div>
                      <div className="anchoractions">
                        <button
                          className="btn sm"
                          type="button"
                          disabled={!canSave}
                          onClick={() => {
                            if (!canSave) return;
                            if (row.rule.dose_code) onChange?.(row.rule.dose_code, state);
                            setEditingKey(null);
                          }}
                        >
                          Save anchor to draft
                        </button>
                        <button className="btn ghost sm" type="button" onClick={() => setEditingKey(null)}>
                          Close
                        </button>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : null}
              </Fragment>
            );
          })}
        </TableBody>
      </Table>
    </div>
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
