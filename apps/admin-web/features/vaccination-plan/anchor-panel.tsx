"use client";

import { Fragment, useMemo, useState, useTransition } from "react";

import { todayIso } from "@/lib/format";
import type { VaccinationAnchorPreview, VaccinationAnchorRequest } from "@/lib/api/server";

import { previewAnchor } from "./plan-actions";
import type { ScheduleRule, VaccineGroup } from "./plan-model";

type AnchorState = {
  anchorDate: string;
  reason: string;
  sourceRef: string;
  suppressBeforeAnchor: boolean;
  chainFutureFromAnchor: boolean;
  enforceAgeEligibility: boolean;
};

type RuleRow = {
  vaccine: Pick<VaccineGroup, "code" | "name">;
  rule: ScheduleRule;
};

type Props = {
  catalog?: VaccineGroup[];
  rows?: RuleRow[];
};

const DEFAULT_REASON = "Anchor/base date for this vaccine rule";

export function VaccinationAnchorPanel({ catalog = [], rows: providedRows }: Props) {
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
  const [pending, startTransition] = useTransition();
  const [preview, setPreview] = useState<VaccinationAnchorPreview | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [configured, setConfigured] = useState<Record<string, string>>({});

  function openEditor(row: RuleRow) {
    setEditingKey(rowKey(row));
    setState(defaultState());
    setPreview(null);
    setError(null);
  }

  function update<K extends keyof AnchorState>(key: K, value: AnchorState[K]) {
    setState((current) => ({ ...current, [key]: value }));
  }

  function selectedRow(): RuleRow | null {
    return rows.find((row) => rowKey(row) === editingKey) ?? null;
  }

  function runPreview() {
    const row = selectedRow();
    if (!row) return;
    setError(null);
    startTransition(async () => {
      const result = await previewAnchor(buildAnchorPayload(row, state));
      if (!result.ok) {
        setPreview(null);
        setError(result.error);
        return;
      }
      setPreview(result.preview);
    });
  }

  function skipAnchor(key: string) {
    setConfigured((current) => clearKey(current, key));
    if (editingKey === key) {
      setEditingKey(null);
      setPreview(null);
      setError(null);
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
            const anchorDate = configured[key];
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
                      <button className="btn ghost sm" type="button" onClick={() => openEditor(row)}>
                        {anchorDate ? "Edit anchor" : "Add anchor"}
                      </button>
                      <button className="btn ghost sm" type="button" onClick={() => skipAnchor(key)}>
                        Skip anchor
                      </button>
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
                      {error ? (
                        <div className="alert" style={{ marginTop: 14 }}>
                          <span className="ic">!</span>
                          <span>{error}</span>
                        </div>
                      ) : null}
                      <div className="anchoractions">
                        <button className="btn ghost sm" type="button" disabled={pending} onClick={runPreview}>
                          {pending ? "Checking..." : "Preview"}
                        </button>
                        <button
                          className="btn sm"
                          type="button"
                          disabled={pending || !preview || preview.eligible_animals === 0}
                          onClick={() => {
                            const row = selectedRow();
                            if (!row) return;
                            setConfigured((current) => ({ ...current, [rowKey(row)]: state.anchorDate }));
                            setEditingKey(null);
                          }}
                        >
                          Save anchor to draft
                        </button>
                        <button className="btn ghost sm" type="button" disabled={pending} onClick={() => setEditingKey(null)}>
                          Close
                        </button>
                      </div>
                      {preview ? <AnchorPreview preview={preview} /> : null}
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

export function buildAnchorPayload(row: RuleRow, state: AnchorState): VaccinationAnchorRequest {
  const payload: VaccinationAnchorRequest = {
    vaccine_code: row.vaccine.code,
    dose_code: row.rule.dose_code,
    anchor_date: state.anchorDate,
    scope_type: "tenant",
    scope_payload: {},
    reason: state.reason.trim() || DEFAULT_REASON,
    suppress_before_anchor: state.suppressBeforeAnchor,
    chain_future_from_anchor: state.chainFutureFromAnchor,
    enforce_age_eligibility: state.enforceAgeEligibility,
  };
  if (state.sourceRef.trim()) payload.source_ref = state.sourceRef.trim();
  return payload;
}

function AnchorPreview({ preview }: { preview: VaccinationAnchorPreview }) {
  return (
    <div className="anchorpreview">
      <div className="impact">
        <Stat label="eligible" value={preview.eligible_animals} />
        <Stat label="underage excluded" value={preview.excluded_underage_animals} />
        <Stat label="species mismatch" value={preview.species_mismatch_animals} />
        <Stat label="earlier open rows to cancel" value={preview.open_rows_before_anchor} />
        <Stat label="same-day rows kept" value={preview.same_day_rows_preserved} />
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat">
      <div className="n">{value.toLocaleString("en-IN")}</div>
      <div className="l">{label}</div>
    </div>
  );
}

function rowKey(row: RuleRow): string {
  return `${row.vaccine.code}:${row.rule.dose_code ?? ""}`;
}

function clearKey(current: Record<string, string>, key: string): Record<string, string> {
  const next = { ...current };
  delete next[key];
  return next;
}

function ruleTiming(rule: ScheduleRule): string {
  return rule.trigger_type?.replaceAll("_", " ") || "scheduled";
}

function repeatLabel(rule: ScheduleRule): string {
  return rule.repeat && rule.repeat !== "none" ? "Repeats" : "Does not repeat";
}
