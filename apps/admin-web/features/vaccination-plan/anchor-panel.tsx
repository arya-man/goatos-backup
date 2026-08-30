"use client";

import { useMemo, useState, useTransition } from "react";

import { todayIso } from "@/lib/format";
import type {
  VaccinationAnchorAnimal,
  VaccinationAnchorPreview,
  VaccinationAnchorRequest,
  VaccinationAnchorScopeType,
} from "@/lib/api/server";

import { createAnchor, previewAnchor } from "./plan-actions";
import type { ScheduleRule, VaccineGroup } from "./plan-model";

type AnchorState = {
  vaccineCode: string;
  doseCode: string;
  anchorDate: string;
  scopeType: VaccinationAnchorScopeType;
  targetIds: string;
  partitionLabel: string;
  reason: string;
  sourceRef: string;
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
    scopeType: "tenant",
    targetIds: "",
    partitionLabel: "",
    reason: "",
    sourceRef: "",
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

  function update<K extends keyof AnchorState>(key: K, value: AnchorState[K]) {
    setState((current) => ({ ...current, [key]: value }));
    setApplied(false);
  }

  function runPreview() {
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
            Anchor campaign
          </div>
          <h2>Seed a completed vaccine date</h2>
          <p className="s">
            Use this when older vaccination history is missing and future repeats should start from
            a known campaign date.
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
            <span>Anchor date</span>
            <input
              type="date"
              value={state.anchorDate}
              onChange={(event) => update("anchorDate", event.target.value)}
            />
          </label>
          <label className="field">
            <span>Animals</span>
            <select
              value={state.scopeType}
              onChange={(event) => update("scopeType", event.target.value as VaccinationAnchorScopeType)}
            >
              <option value="tenant">All live animals</option>
              <option value="park">One park</option>
              <option value="shed">One shed</option>
              <option value="partition">One partition</option>
              <option value="animal_set">Specific animals</option>
            </select>
          </label>
          {state.scopeType !== "tenant" ? (
            <label className="field span2">
              <span>{scopeTargetLabel(state.scopeType)}</span>
              <textarea
                value={state.targetIds}
                onChange={(event) => update("targetIds", event.target.value)}
                rows={state.scopeType === "animal_set" ? 3 : 1}
              />
            </label>
          ) : null}
          {state.scopeType === "partition" ? (
            <label className="field">
              <span>Partition</span>
              <input
                value={state.partitionLabel}
                onChange={(event) => update("partitionLabel", event.target.value)}
              />
            </label>
          ) : null}
          <label className="field span2">
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
            <input
              type="checkbox"
              checked={state.suppressBeforeAnchor}
              onChange={(event) => update("suppressBeforeAnchor", event.target.checked)}
            />
            Cancel earlier open rows
          </label>
          <label>
            <input
              type="checkbox"
              checked={state.chainFutureFromAnchor}
              onChange={(event) => update("chainFutureFromAnchor", event.target.checked)}
            />
            Chain future repeats
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
            onClick={runCreate}
          >
            {pending ? "Creating..." : "Create anchor"}
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
    scope_type: state.scopeType,
    scope_payload: scopePayload(state),
    reason: state.reason.trim(),
    flags: {
      suppress_before_anchor: state.suppressBeforeAnchor,
      chain_future_from_anchor: state.chainFutureFromAnchor,
      enforce_age_eligibility: state.enforceAgeEligibility,
    },
  };
  if (state.doseCode) payload.dose_code = state.doseCode;
  if (state.sourceRef.trim()) payload.source_ref = state.sourceRef.trim();
  return payload;
}

function AnchorPreview({ preview }: { preview: VaccinationAnchorPreview }) {
  return (
    <div className="anchorpreview">
      <div className="impact">
        <Stat label="resolved" value={preview.total_resolved_animals} />
        <Stat label="eligible" value={preview.eligible_animals} />
        <Stat label="underage excluded" value={preview.excluded_underage_animals} />
        <Stat label="species mismatch" value={preview.species_mismatch_animals} />
        <Stat label="earlier rows canceled" value={preview.open_rows_before_anchor} />
        <Stat label="same-day rows kept" value={preview.same_day_rows_preserved} />
      </div>
      <AnimalList title="Eligible animals" animals={preview.eligible_sample} />
      <AnimalList title="Underage excluded" animals={preview.underage_sample} />
      <AnimalList title="Species mismatch" animals={preview.species_mismatch_sample} />
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

function AnimalList({ title, animals }: { title: string; animals: VaccinationAnchorAnimal[] }) {
  if (animals.length === 0) return null;
  return (
    <div className="anchordetail">
      <div className="sec-label">{title}</div>
      <div className="scroll" tabIndex={0}>
        <table className="tabl">
          <thead>
            <tr>
              <th>RFID</th>
              <th>Species</th>
              <th>Reason</th>
            </tr>
          </thead>
          <tbody>
            {animals.slice(0, 20).map((animal) => (
              <tr key={`${title}-${animal.identifier}`}>
                <td>
                  <b>{animal.identifier}</b>
                </td>
                <td>{animal.species || "-"}</td>
                <td>{animal.reason || "-"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function scopePayload(state: AnchorState): Record<string, unknown> {
  if (state.scopeType === "tenant") return {};
  const ids = splitLines(state.targetIds);
  if (state.scopeType === "animal_set") return { animal_ids: ids };
  if (state.scopeType === "partition") {
    return { shed_id: ids[0] ?? "", partition_label: state.partitionLabel.trim() };
  }
  return { [`${state.scopeType}_id`]: ids[0] ?? "" };
}

function splitLines(value: string): string[] {
  return value.split(/[\n,]+/).map((item) => item.trim()).filter(Boolean);
}

function scopeTargetLabel(scopeType: VaccinationAnchorScopeType): string {
  if (scopeType === "animal_set") return "Animal IDs";
  if (scopeType === "partition") return "Shed ID";
  return `${scopeType[0].toUpperCase()}${scopeType.slice(1)} ID`;
}

function doseLabel(rule: ScheduleRule): string {
  const sequence = rule.sequence ? `Dose ${rule.sequence}` : "Dose";
  const timing = rule.repeat && rule.repeat !== "none" ? "repeat" : rule.trigger_type?.replaceAll("_", " ") || "scheduled";
  return `${sequence} - ${rule.dose_code} - ${timing}`;
}
