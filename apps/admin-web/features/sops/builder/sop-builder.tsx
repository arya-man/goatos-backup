"use client";

import { useMemo, useReducer, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import "./builder.css";
import type { BuilderState, ServerVersionFacts, SopMeta } from "./model";
import { initialState, reducer, templateToSeed, type BuilderSeed } from "./state";
import { SOP_TEMPLATES, blankDraft } from "./sop-templates";
import { inferFlowPattern } from "./templates";
import { compatibility, fromFormDsl, normalizeCode, proofPolicy, sampleDryRun, toFormDsl } from "./dsl";
import { validateDraft } from "./validate";
import { computeOutcome, DEFAULT_SCENARIO, type Scenario } from "./preview-engine";
import { CatalogTab, type CustomDraftInput } from "./tabs/catalog-tab";
import { FormTab } from "./tabs/form-tab";
import { RulesTab } from "./tabs/rules-tab";
import { FlowTab } from "./tabs/flow-tab";
import { AndroidPreview } from "./android-preview";
import {
  createSopAction,
  createVersionAction,
  publishVersionAction,
  retireVersionAction,
} from "../actions";

export interface SopCatalogRow {
  sop_id: string;
  code: string;
  name: string;
  status: string;
}

export interface SopBuilderProps {
  sops: SopCatalogRow[];
  selected: SopCatalogRow | null;
  selectedFormDsl: unknown | null;
  versionFacts: ServerVersionFacts;
}

const TABS: { id: BuilderState["activeTab"]; label: string }[] = [
  { id: "catalog", label: "1 · Choose SOP" },
  { id: "form", label: "2 · Operator Form" },
  { id: "rules", label: "3 · Form Logic" },
  { id: "flow", label: "4 · Task Flow" },
];

function defaultMeta(row: SopCatalogRow | null): SopMeta {
  return {
    title: row?.name ?? "Shifting SOP",
    subtitle: "",
    code: row?.code ?? "movement.shifting",
    domain: "Movement",
    trigger: "Manual task / request",
    runner: "Operator Android",
    submitLabel: row ? `Submit ${row.name.toLowerCase()}` : "Submit shifting",
    typeLabel: "TASK",
    repeatForEachGoat: false,
  };
}

function buildInitialSeed(props: SopBuilderProps): BuilderSeed {
  if (props.selectedFormDsl && props.selected) {
    const loaded = fromFormDsl(props.selectedFormDsl, defaultMeta(props.selected));
    return {
      ...loaded,
      flowPattern: "custom",
      activeTemplate: props.selected.code in SOP_TEMPLATES ? props.selected.code : null,
    };
  }
  return templateToSeed("shifting", SOP_TEMPLATES.shifting, "approval");
}

export function SopBuilder(props: SopBuilderProps) {
  const router = useRouter();
  const [seed] = useState(() => buildInitialSeed(props));
  const [state, dispatch] = useReducer(reducer, seed, initialState);
  const [scenario, setScenario] = useState<Scenario>(DEFAULT_SCENARIO);
  const [sopId, setSopId] = useState<string | null>(props.selected?.sop_id ?? null);
  const [facts, setFacts] = useState<ServerVersionFacts>(props.versionFacts);
  const [pending, setPending] = useState<null | "create" | "publish" | "retire">(null);
  const [notice, setNotice] = useState<{ tone: "ok" | "warn" | "error"; text: string } | null>(null);
  const stampSeq = useRef(0);

  const validation = useMemo(() => validateDraft(state), [state]);
  const outcome = useMemo(() => computeOutcome(state, scenario), [state, scenario]);

  const serverValid = facts.validationValid === true;
  const isPublished = facts.status === "published";
  const canCreate = validation.valid && pending === null;
  const canPublish = !!facts.sopVersionId && serverValid && !isPublished && !state.dirty && pending === null;
  const canRetire = !!facts.sopVersionId && facts.status !== "retired" && pending === null;

  function loadTemplate(key: string) {
    const template = SOP_TEMPLATES[key];
    if (!template) return;
    dispatch({ kind: "loadSeed", seed: templateToSeed(key, template, inferFlowPattern(key)) });
    dispatch({ kind: "setTab", tab: "form" });
    const match = props.sops.find((s) => s.code === template.meta.code);
    setSopId(match?.sop_id ?? null);
    setFacts({ sopId: match?.sop_id ?? null, sopVersionId: null, versionLabel: null, status: null, rowVersion: null, validationValid: null });
    setNotice(null);
  }

  async function createCustom(input: CustomDraftInput) {
    setPending("create");
    setNotice(null);
    const code = normalizeCode(input.code || input.name);
    const result = await createSopAction({ code, name: input.name });
    if (!result.ok) {
      setNotice({ tone: "error", text: result.message });
      setPending(null);
      return;
    }
    setSopId(result.sopId ?? null);
    setFacts({ sopId: result.sopId ?? null, sopVersionId: null, versionLabel: null, status: null, rowVersion: null, validationValid: null });
    const seedNext = blankDraft({ ...input, code });
    dispatch({ kind: "loadSeed", seed: { ...seedNext, flowPattern: "simple", activeTemplate: null } });
    dispatch({ kind: "setTab", tab: "form" });
    setNotice({ tone: "ok", text: result.message });
    setPending(null);
    router.refresh();
  }

  function runValidate() {
    if (validation.valid) setNotice({ tone: "ok", text: "Draft passes client checks. Create a version for the authoritative server dry-run." });
    else setNotice({ tone: "error", text: `${validation.issues.filter((i) => i.level === "error").length} issue(s) must be fixed before creating a version.` });
    dispatch({ kind: "setTab", tab: state.activeTab });
  }

  async function createVersion() {
    if (!validation.valid) { runValidate(); return; }
    setPending("create");
    setNotice(null);

    let resolvedSopId = sopId;
    if (!resolvedSopId) {
      const code = normalizeCode(state.meta.code);
      const match = props.sops.find((s) => s.code === code);
      if (match) {
        resolvedSopId = match.sop_id;
      } else {
        const created = await createSopAction({ code, name: state.meta.title });
        if (!created.ok) { setNotice({ tone: "error", text: created.message }); setPending(null); return; }
        resolvedSopId = created.sopId ?? null;
      }
      setSopId(resolvedSopId);
    }
    if (!resolvedSopId) { setNotice({ tone: "error", text: "Could not resolve a SOP to attach this version to." }); setPending(null); return; }

    const shortName = state.meta.title.replace(/\s+SOP$/i, "");
    stampSeq.current += 1;
    const result = await createVersionAction({
      sopId: resolvedSopId,
      versionLabel: `${shortName} v${(Date.now() % 100000) + stampSeq.current}`,
      formDsl: toFormDsl(state) as unknown as Record<string, unknown>,
      proofPolicy: proofPolicy(state.fields),
      compatibility: compatibility(),
      sample: sampleDryRun(state) as unknown as import("@/lib/api/server").DryRunRequest,
    });

    if (!result.ok) { setNotice({ tone: "error", text: result.message }); setPending(null); return; }

    const valid = (result.validationValid ?? false) && (result.dryRunValid ?? false);
    setFacts({
      sopId: resolvedSopId,
      sopVersionId: result.sopVersionId ?? null,
      versionLabel: null,
      status: result.versionStatus ?? "draft",
      rowVersion: result.rowVersion ?? null,
      validationValid: valid,
    });
    dispatch({ kind: "markSaved" });
    const problems = [...(result.validationErrors ?? []), ...(result.dryRunErrors ?? [])];
    setNotice(
      valid
        ? { tone: "ok", text: `Version created and server dry-run passed. Ready to publish.` }
        : { tone: "warn", text: `Version created but validation flagged: ${problems.slice(0, 3).join("; ") || "see server report"}.` },
    );
    setPending(null);
    router.refresh();
  }

  async function publish() {
    if (!sopId || !facts.sopVersionId || facts.rowVersion == null) return;
    setPending("publish");
    const result = await publishVersionAction({ sopId, sopVersionId: facts.sopVersionId, rowVersion: facts.rowVersion });
    if (result.ok) setFacts((f) => ({ ...f, status: result.status ?? "published", rowVersion: result.rowVersion ?? f.rowVersion }));
    setNotice({ tone: result.ok ? "ok" : "error", text: result.message });
    setPending(null);
    router.refresh();
  }

  async function retire() {
    if (!sopId || !facts.sopVersionId || facts.rowVersion == null) return;
    setPending("retire");
    const result = await retireVersionAction({ sopId, sopVersionId: facts.sopVersionId, rowVersion: facts.rowVersion });
    if (result.ok) setFacts((f) => ({ ...f, status: result.status ?? "retired", rowVersion: result.rowVersion ?? f.rowVersion }));
    setNotice({ tone: result.ok ? "ok" : "error", text: result.message });
    setPending(null);
    router.refresh();
  }

  return (
    <div className="sopb">
      <header className="topbar">
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="eyebrow">SOP Builder · Phase 2</div>
          <h2>{state.meta.title} <span className="muted" style={{ fontWeight: 400 }}>— {state.meta.subtitle || state.meta.trigger}</span></h2>
          <div className="sub">Build the operator form, the if/then form logic, and the task flow around it.</div>
        </div>
        <VersionPill facts={facts} dirty={state.dirty} valid={validation.valid} />
        <button className="btn ghost" type="button" onClick={runValidate}>✓ Validate draft</button>
        <button className="btn primary" type="button" disabled={!canCreate} onClick={createVersion}>{pending === "create" ? "Saving…" : "Create version"}</button>
        <button className="btn" type="button" disabled={!canPublish} onClick={publish}>{pending === "publish" ? "Publishing…" : "Publish"}</button>
        {facts.sopVersionId ? <button className="btn" type="button" disabled={!canRetire} onClick={retire}>{pending === "retire" ? "Retiring…" : "Retire"}</button> : null}
      </header>

      {notice ? <div className={`banner ${notice.tone === "error" ? "block" : notice.tone === "warn" ? "warn" : ""}`} style={notice.tone === "ok" ? { border: "1px solid rgba(34,197,94,.4)", color: "#86efac", background: "rgba(34,197,94,.12)", borderRadius: 8, padding: "9px 11px", marginBottom: 14 } : { marginBottom: 14 }}>{notice.text}</div> : null}

      <div className="stage">
        <section>
          <div className="tabs">
            {TABS.map((t) => (
              <button key={t.id} type="button" className={`tab${state.activeTab === t.id ? " active" : ""}`} onClick={() => dispatch({ kind: "setTab", tab: t.id })}>{t.label}</button>
            ))}
          </div>

          {validation.issues.length > 0 ? (
            <ul className="issues" style={{ marginBottom: 14 }}>
              {validation.issues.slice(0, 6).map((issue, i) => (
                <li key={i} className={issue.level}>{issue.message}</li>
              ))}
            </ul>
          ) : (
            <ul className="issues" style={{ marginBottom: 14 }}><li className="ok">Draft passes client validation.</li></ul>
          )}

          {state.activeTab === "catalog" ? <CatalogTab state={state} onLoadTemplate={loadTemplate} onCreateCustom={createCustom} creating={pending === "create"} /> : null}
          {state.activeTab === "form" ? <FormTab state={state} dispatch={dispatch} /> : null}
          {state.activeTab === "rules" ? <RulesTab state={state} dispatch={dispatch} /> : null}
          {state.activeTab === "flow" ? <FlowTab state={state} dispatch={dispatch} lit={outcome.lit} /> : null}
        </section>

        <AndroidPreview state={state} scenario={scenario} onScenario={(patch) => setScenario((s) => ({ ...s, ...patch }))} outcome={outcome} />
      </div>
    </div>
  );
}

function VersionPill({ facts, dirty, valid }: { facts: ServerVersionFacts; dirty: boolean; valid: boolean }) {
  if (!facts.sopVersionId) {
    return <span className={`pill ${valid ? "teal" : "amber"}`}>{valid ? "draft · ready to save" : "draft · unvalidated"}</span>;
  }
  if (facts.status === "published") return <span className="pill green">published · live</span>;
  if (facts.status === "retired") return <span className="pill red">retired</span>;
  if (dirty) return <span className="pill amber">draft · unsaved edits</span>;
  return <span className={`pill ${facts.validationValid ? "teal" : "amber"}`}>{facts.validationValid ? "draft · validated ✓" : "draft · review"}</span>;
}
