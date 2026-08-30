"use client";

import { useMemo, useState, useTransition } from "react";

import { todayIso } from "@/lib/format";
import type {
  VaccinationAnchorPreview,
  VaccinationAnchorRequest,
} from "@/lib/api/server";

import { createAnchor, previewAnchor } from "./plan-actions";
import type { ScheduleRule, VaccineGroup } from "./plan-model";

type AnchorState = {
  vaccineCode: string;
  doseCode: string;
  anchorDate: string;
  applyEligibleScope: boolean;
  suppressBeforeAnchor: boolean;
  chainFutureFromAnchor: boolean;
  enforceAgeEligibility: boolean;
};

type Props = {
  catalog: VaccineGroup[];
};

const EMPTY_PREVIEW: VaccinationAnchorPreview | null = null;

export function VaccinationAnchorPanel({ catalog }: Props) {
  const active = useMemo(() => catalog.filter((v) => v.inPlan), [catalog]);
  const firstVaccine = active[0]?.code ?? catalog[0]?.code ?? "";
  const [state, setState] = useState<AnchorState>(() => ({
    vaccineCode: firstVaccine,
    doseCode: "",
    anchorDate: todayIso(),
    applyEligibleScope: true,
    suppressBeforeAnchor: true,
    chainFutureFromAnchor: true,
    enforceAgeEligibility: true,
  }));
  const [pending, startTransition] = useTransition();
  const [preview, setPreview] = useState<VaccinationAnchorPreview | null>(EMPTY_PREVIEW);
  const [error, setError] = useState<string | null>(null);
  const [applied, setApplied] = useState(false);

  const vaccine = active.find((v) => v.code === state.vaccineCode) ?? active[0];
  const doseOptions = useMemo(() => {
    const rules = vaccine ? [...vaccine.firstDoses, ...vaccine.repeats] : [];
    return rules.filter((rule) => Boolean(rule.dose_code));
  }, [vaccine]);
  const ruleRows = useMemo(
    () =>
      active.flatMap((item) =>
        [...item.firstDoses, ...item.repeats]
          .filter((rule) => Boolean(rule.dose_code))
          .map((rule) => ({ vaccine: item, rule })),
      ),
    [active],
  );

  function update<K extends keyof AnchorState>(key: K, value: AnchorState[K]) {
    setState((current) => ({ ...current, [key]: value }));
    setApplied(false);
  }

  function runPreview() {
    if (!state.applyEligibleScope) return;
    setError(null);
    setApplied(false);
    startTransition(async () => {
      const payload = buildAnchorPayload(state);
      const result = await previewAnchor(payload);
      if (!result.ok) {
        setPreview(null);
        setError(result.error);
        return;
      }
      setPreview(result.preview);
    });
  }

  function runCreate() {
    if (!state.applyEligibleScope) return;
    setError(null);
    setApplied(false);
    startTransition(async () => {
      const payload = buildAnchorPayload(state);
      const result = await createAnchor(payload);
      if (!result.ok) {
        setError(result.error);
        return;
      }
      setPreview(result.preview);
      setApplied(true);
    });
  }

  if (active.length === 0) return null;

  return (
    <section className="card anchorcard">
      <div className="card-h">
        <div>
          <div className="eyebrow" style={{ marginBottom: 4 }}>
            Rule anchor
          </div>
          <h2>Anchor/base date</h2>
          <p className="s">
            Start this vaccine from this date when older history is missing for the rule's eligible scope.
          </p>
        </div>
        {applied ? <span className="pill live">Created</span> : null}
      </div>
      <div className="card-b">
        <div className="anchorform">
          <label className="field">
            <span>Vaccine</span>
            <select
              value={state.vaccineCode}
              onChange={(event) => {
                update("vaccineCode", event.target.value);
                update("doseCode", "");
              }}
            >
              {active.map((item) => (
                <option key={item.code} value={item.code}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Dose</span>
            <select value={state.doseCode} onChange={(event) => update("doseCode", event.target.value)}>
              <option value="">Auto when only one active dose exists</option>
              {doseOptions.map((rule) => (
                <option key={rule.dose_code} value={rule.dose_code}>
                  {doseLabel(rule)}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Anchor/base date</span>
            <input
              type="date"
              value={state.anchorDate}
              onChange={(event) => update("anchorDate", event.target.value)}
            />
          </label>
        </div>
        <div className="anchorflags" aria-label="Anchor behavior">
          <label>
            <input
              type="checkbox"
              checked={state.applyEligibleScope}
              onChange={(event) => update("applyEligibleScope", event.target.checked)}
            />
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
        <div className="anchor-rules">
          <div className="sec-label">Rules</div>
          <div className="scroll" tabIndex={0}>
            <table className="tabl">
              <thead>
                <tr>
                  <th>Vaccine</th>
                  <th>Rule</th>
                  <th>Timing</th>
                  <th>Anchor/base date</th>
                </tr>
              </thead>
              <tbody>
                {ruleRows.map(({ vaccine: item, rule }) => {
                  const selected = item.code === state.vaccineCode && rule.dose_code === state.doseCode;
                  return (
                    <tr key={`${item.code}-${rule.dose_code}`} className={selected ? "selrow" : undefined}>
                      <td>
                        <b>{item.name}</b>
                      </td>
                      <td>{rule.dose_code}</td>
                      <td>{ruleTiming(rule)}</td>
                      <td>
                        <button
                          className="btn ghost sm"
                          type="button"
                          onClick={() => {
                            update("vaccineCode", item.code);
                            update("doseCode", rule.dose_code ?? "");
                          }}
                        >
                          {selected ? "Selected" : "Set anchor"}
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </div>
        {error ? (
          <div className="alert" style={{ marginTop: 14 }}>
            <span className="ic">!</span>
            <span>{error}</span>
          </div>
        ) : null}
        <div className="anchoractions">
          <button className="btn ghost sm" type="button" disabled={pending || !state.applyEligibleScope} onClick={runPreview}>
            {pending ? "Checking..." : "Preview"}
          </button>
          <button
            className="btn sm"
            type="button"
            disabled={pending || !state.applyEligibleScope || !preview || preview.eligible_animals === 0}
            onClick={runCreate}
          >
            {pending ? "Creating..." : "Start this vaccine from this date"}
          </button>
        </div>
        {preview ? <AnchorPreview preview={preview} /> : null}
      </div>
    </section>
  );
}

export function buildAnchorPayload(state: AnchorState): VaccinationAnchorRequest {
  const payload: VaccinationAnchorRequest = {
    vaccine_code: state.vaccineCode,
    anchor_date: state.anchorDate,
    scope_type: "tenant",
    scope_payload: {},
    reason: "Start this vaccine from this date",
    flags: {
      suppress_before_anchor: state.suppressBeforeAnchor,
      chain_future_from_anchor: state.chainFutureFromAnchor,
      enforce_age_eligibility: state.enforceAgeEligibility,
    },
  };
  if (state.doseCode) payload.dose_code = state.doseCode;
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

function doseLabel(rule: ScheduleRule): string {
  const sequence = rule.sequence ? `Dose ${rule.sequence}` : "Dose";
  const timing = ruleTiming(rule);
  return `${sequence} - ${rule.dose_code} - ${timing}`;
}

function ruleTiming(rule: ScheduleRule): string {
  return rule.repeat && rule.repeat !== "none" ? "repeat" : rule.trigger_type?.replaceAll("_", " ") || "scheduled";
}
