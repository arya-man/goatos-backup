"use client";

import { Fragment, useMemo, useState } from "react";
import { CircleSlash, Pencil, Plus } from "lucide-react";

import { todayIso } from "@/lib/format";
import type { AnchorConfig } from "./editor-model";
import type { ScheduleRule, VaccineGroup } from "./plan-model";

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

  function skipAnchor(key: string) {
    const row = rows.find((item) => rowKey(item) === key);
    if (row?.rule.dose_code) onChange?.(row.rule.dose_code, null);
    if (editingKey === key) {
      setEditingKey(null);
    }
  }

  if (rows.length === 0) return null;

  return (
    <div className="scroll anchor-rule-table" tabIndex={0}>
      <table className="tabl">
        <thead>
          <tr>
            <th>Vaccine</th>
            <th>Rule/dose</th>
            <th>Timing</th>
            <th>Repeat</th>
            <th>Anchor/base date</th>
            <th>Action</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => {
            const key = rowKey(row);
            const editing = editingKey === key;
            const anchorDate = row.rule.dose_code ? anchors[row.rule.dose_code]?.anchorDate : "";
            const canSave = isValidIsoDate(state.anchorDate);
            return (
              <Fragment key={key}>
                <tr className={editing ? "selrow" : undefined}>
                  <td>
                    <b>{row.vaccine.name}</b>
                  </td>
                  <td>{row.rule.dose_code}</td>
                  <td>{ruleTiming(row.rule)}</td>
                  <td>{repeatLabel(row.rule)}</td>
                  <td>{anchorDate || "No anchor"}</td>
                  <td>
                    <div className="anchorrow-actions">
                      <button
                        className="btn ghost sm icon"
                        type="button"
                        aria-label={anchorDate ? "Edit anchor" : "Add anchor"}
                        title={anchorDate ? "Edit anchor" : "Add anchor"}
                        onClick={() => openEditor(row)}
                      >
                        {anchorDate ? <Pencil size={18} strokeWidth={2.6} aria-hidden /> : <Plus size={18} strokeWidth={2.8} aria-hidden />}
                      </button>
                      {anchorDate ? (
                        <button
                          className="btn ghost sm icon"
                          type="button"
                          aria-label="Skip anchor"
                          title="Skip anchor"
                          onClick={() => skipAnchor(key)}
                        >
                          <CircleSlash size={18} strokeWidth={2.6} aria-hidden />
                        </button>
                      ) : null}
                    </div>
                  </td>
                </tr>
                {editing ? (
                  <tr className="anchoredit" key={`${key}-editor`}>
                    <td colSpan={6}>
                      <div className="anchorform inline">
                        <label className="field">
                          <span>Anchor/base date</span>
                          <input
                            type="date"
                            value={state.anchorDate}
                            onChange={(event) => update("anchorDate", event.target.value)}
                            onInput={(event) => update("anchorDate", event.currentTarget.value)}
                          />
                        </label>
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
                        <label>
                          <input type="checkbox" checked readOnly />
                          Apply anchor to this rule's eligible scope
                        </label>
                        <label>
                          <input
                            type="checkbox"
                            checked={state.suppressBeforeAnchor}
                            onChange={(event) => update("suppressBeforeAnchor", event.target.checked)}
                          />
                          Suppress earlier catch-up rows before anchor
                        </label>
                        <label>
                          <input
                            type="checkbox"
                            checked={state.chainFutureFromAnchor}
                            onChange={(event) => update("chainFutureFromAnchor", event.target.checked)}
                          />
                          Chain boosters/revacs from anchor
                        </label>
                        <label>
                          <input
                            type="checkbox"
                            checked={state.enforceAgeEligibility}
                            onChange={(event) => update("enforceAgeEligibility", event.target.checked)}
                          />
                          Enforce age eligibility
                        </label>
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
                    </td>
                  </tr>
                ) : null}
              </Fragment>
            );
          })}
        </tbody>
      </table>
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

function ruleTiming(rule: ScheduleRule): string {
  return rule.trigger_type?.replaceAll("_", " ") || "scheduled";
}

function repeatLabel(rule: ScheduleRule): string {
  return rule.repeat && rule.repeat !== "none" ? "Repeats" : "Does not repeat";
}
