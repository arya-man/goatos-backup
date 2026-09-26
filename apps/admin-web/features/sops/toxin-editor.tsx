"use client";
import Box from "@mui/material/Box";

// THE TOXIN PROCEDURE IS AUTHORED (maintainer decision 2026-09-20,
// docs/decisions/procurement-sop-driven.md).
//
// The aflatoxin procedure editor: how many steps the test has, what each one tells the tester to
// do, whether it is filmed or photographed, how long the extract sits, and which step each wait
// gates. Every word here is the backend contract's; the backend runs the SAME validator on save
// and refuses a document the engine could not run, naming the step.
//
// What this screen does NOT edit, and says so: the round's state machine, the retest an Invalid
// strip mints, the CEO/CXO review, and the fact that every wait is enforced on the SERVER's clock.
// The document says what the procedure IS; the engine still decides what happens when a strip
// comes back void.
import { useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { AlertTriangle, Check, ChevronDown, ChevronUp, Lock, Plus, X } from "lucide-react";
import MuiButton from "@mui/material/Button";
import IconButton from "@mui/material/IconButton";
import MuiTextField from "@mui/material/TextField";

import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { publishedHref } from "./published-href";
import { blankToxinStep, emitToxin, toxinProblems, type ToxinRows, type ToxinStepKind, type ToxinStepRow } from "./toxin-model";
import { ToxinFlow, toxinStepSummaryLine, waitWords, type ToxinInsert } from "./toxin-flow";
import { publishToxinVersion, saveToxinVersion, type ToxinSaveResult } from "./sop-actions";
import Alert from "@mui/material/Alert";
import { SegmentTabs } from "@/components/minimal/list/segment-tabs";
import { EditorHeader, inspectionEditorSx } from "./editor-chrome";

type Props = {
  pageContract: AdminUiPageContract;
  basePath: string;
  sopId: string;
  sopName: string;
  sopCode: string;
  versionLabel: string;
  initial: ToxinRows;
  /** The view the page opened on (`?view=flow`), read on the server so SSR and client agree. */
  initialView?: "list" | "flow";
};

export function ToxinEditor({ pageContract: pc, basePath, sopId, sopName, versionLabel, initial, initialView = "list" }: Props) {
  const router = useRouter();
  const [rows, setRows] = useState<ToxinRows>(initial);
  const [result, setResult] = useState<ToxinSaveResult | null>(null);
  const [pending, startTransition] = useTransition();
  const kinds = optionGroup(pc, "tsop_step_kinds");
  const kindLabels = useMemo(() => Object.fromEntries(kinds.map((k) => [k.key, k.label])), [kinds]);
  const problems = useMemo(() => toxinProblems(rows, (key) => copy(pc, key)), [rows, pc]);

  const [view, setView] = useState<"list" | "flow">(initialView);
  const [selected, setSelected] = useState<string | null>(null);

  function switchView(next: "list" | "flow") {
    setView(next);
    if (typeof window !== "undefined") {
      const url = new URL(window.location.href);
      if (next === "flow") url.searchParams.set("view", "flow");
      else url.searchParams.delete("view");
      window.history.replaceState(window.history.state, "", url.toString());
    }
  }

  function patchStep(id: string, patch: Partial<ToxinStepRow>) {
    setRows((r) => ({ steps: r.steps.map((s) => (s.id === id ? { ...s, ...patch } : s)) }));
  }

  // Moving or removing a step RENUMBERS the document, so every gate that pointed at a moved step
  // has to move with it. Rewriting the gates here is the difference between reordering the test
  // and silently pointing an hour-long wait at the wrong step.
  function reindexGates(steps: ToxinStepRow[], mapping: Map<number, number>): ToxinStepRow[] {
    return steps.map((s, i) => ({
      ...s,
      no: i + 1,
      gateAfterStep: s.gateAfterStep > 0 ? (mapping.get(s.gateAfterStep) ?? 0) : 0,
      gateMinutes: s.gateAfterStep > 0 && !mapping.get(s.gateAfterStep) ? 0 : s.gateMinutes,
    }));
  }

  function moveStep(id: string, dir: -1 | 1) {
    setRows((r) => {
      const idx = r.steps.findIndex((s) => s.id === id);
      const j = idx + dir;
      if (idx < 0 || j < 0 || j >= r.steps.length) return r;
      const next = [...r.steps];
      [next[idx], next[j]] = [next[j], next[idx]];
      const mapping = new Map<number, number>();
      next.forEach((s, i) => mapping.set(s.no, i + 1));
      return { steps: reindexGates(next, mapping) };
    });
  }

  function removeStep(id: string) {
    setRows((r) => {
      const next = r.steps.filter((s) => s.id !== id);
      const mapping = new Map<number, number>();
      next.forEach((s, i) => mapping.set(s.no, i + 1));
      return { steps: reindexGates(next, mapping) };
    });
    setSelected(null);
  }

  function insertAt(insert: ToxinInsert) {
    const row = blankToxinStep("video", insert.index);
    setRows((r) => {
      const next = [...r.steps];
      next.splice(Math.min(Math.max(insert.index, 0), next.length), 0, row);
      const mapping = new Map<number, number>();
      next.forEach((s, i) => mapping.set(s.no, i + 1));
      return { steps: reindexGates(next, mapping) };
    });
    setSelected(row.id);
  }

  function cardFor(id: string) {
    const i = rows.steps.findIndex((s) => s.id === id);
    return i < 0 ? null : <div className="qlist">{stepCard(rows.steps[i], i)}</div>;
  }

  function stepCard(step: ToxinStepRow, index: number) {
    // A gate may only point at an EARLIER step that records a completion: a waiting row has no
    // completion to count from, and a later step has not happened yet.
    const gateChoices = rows.steps.slice(0, index).filter((s) => s.kind !== "wait");
    return (
      <div className="qcard" key={step.id} data-testid={`toxin-step-${index + 1}`}>
        <div className="qhead">
          <span className="qnum">{index + 1}</span>
          <span className="qtype">
            <select
              value={step.kind}
              onChange={(e) => {
                const kind = e.target.value as ToxinStepKind;
                patchStep(step.id, {
                  kind,
                  // A waiting row is never itself gated, and only a waiting row carries a duration.
                  waitMinutes: kind === "wait" ? step.waitMinutes || 30 : 0,
                  gateAfterStep: kind === "wait" ? 0 : step.gateAfterStep,
                  gateMinutes: kind === "wait" ? 0 : step.gateMinutes,
                });
              }}
              aria-label={copy(pc, "tsop.step.kind")}
            >
              {kinds.map((k) => (
                <option key={k.key} value={k.key} title={k.title}>
                  {k.label}
                </option>
              ))}
            </select>
          </span>
          <span className="muted small">{toxinStepSummaryLine(step, kindLabels, pc)}</span>
          <span className="sp" style={{ flex: 1 }} />
          <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_up")} disabled={index === 0} onClick={() => moveStep(step.id, -1)}>
            <ChevronUp className="ic" />
          </IconButton>
          <IconButton type="button" size="small" className="ia" aria-label={copy(pc, "inspection.question.move_down")} disabled={index === rows.steps.length - 1} onClick={() => moveStep(step.id, 1)}>
            <ChevronDown className="ic" />
          </IconButton>
          <IconButton type="button" size="small" className="ia del" aria-label={copy(pc, "tsop.step.remove")} onClick={() => removeStep(step.id)}>
            <X className="ic" />
          </IconButton>
        </div>
        <div className="qbody">
          <MuiTextField label={copy(pc, "tsop.step.title")} fullWidth size="small" className="qtext" value={step.title} onChange={(e) => patchStep(step.id, { title: e.target.value })} />
          <label className="numfield">
            <span className="numlbl">{copy(pc, "tsop.step.instruction")}</span>
            <textarea className="qhelp" rows={2} value={step.instruction} onChange={(e) => patchStep(step.id, { instruction: e.target.value })} />
          </label>
          {step.kind === "wait" ? (
            <MuiTextField label={copy(pc, "tsop.step.wait_minutes")} fullWidth size="small" type="number" slotProps={{ htmlInput: { min: 1 } }} value={step.waitMinutes} onChange={(e) => patchStep(step.id, { waitMinutes: Number(e.target.value) || 0 })} />
          ) : (
            <div className="qcfg">
              <div className="qcfg-head">
                <span className="qcfg-title">{copy(pc, "tsop.gate.title")}</span>
                <span className="muted small">{copy(pc, "tsop.gate.subtitle")}</span>
              </div>
              <label className="numfield">
                <span className="numlbl">{copy(pc, "tsop.gate.after")}</span>
                <select
                  value={step.gateAfterStep || 0}
                  onChange={(e) => {
                    const after = Number(e.target.value) || 0;
                    patchStep(step.id, { gateAfterStep: after, gateMinutes: after === 0 ? 0 : step.gateMinutes || 30 });
                  }}
                >
                  <option value={0}>{copy(pc, "tsop.gate.none")}</option>
                  {gateChoices.map((s, i) => (
                    <option key={s.id} value={rows.steps.indexOf(s) + 1}>
                      {rows.steps.indexOf(s) + 1}. {s.title || copy(pc, "tsop.step.untitled")}
                    </option>
                  ))}
                </select>
              </label>
              {step.gateAfterStep > 0 ? (
                <MuiTextField label={copy(pc, "tsop.gate.minutes")} fullWidth size="small" type="number" slotProps={{ htmlInput: { min: 1 } }} value={step.gateMinutes} onChange={(e) => patchStep(step.id, { gateMinutes: Number(e.target.value) || 0 })} />
              ) : null}
            </div>
          )}
        </div>
      </div>
    );
  }

  function submit(publish: boolean) {
    const doc = emitToxin(rows);
    startTransition(async () => {
      const res = publish ? await publishToxinVersion(sopId, doc) : await saveToxinVersion(sopId, doc);
      setResult(res);
      if (res.ok && publish) {
        router.push(publishedHref(basePath, sopId, res.versionNumber));
        router.refresh();
      }
    });
  }

  const totalWait = rows.steps.reduce((sum, s) => sum + (s.kind === "wait" ? s.waitMinutes : 0), 0);

  return (
    <Box className="screen on sop-inspection sop-weighing sop-feed" sx={inspectionEditorSx}>
      <EditorHeader
        crumbs={[copy(pc, "crumb"), pc.title, sopName]}
        title={copy(pc, "tsop.title")}
        subtitle={copy(pc, "tsop.subtitle")}
        version={versionLabel}
        notice={copy(pc, "tsop.notice.pinned")}
        backHref={basePath}
        actions={
          <SegmentTabs
            className="studio-view-toggle"
            ariaLabel={copy(pc, "studio.view.label")}
            value={view}
            tabs={[
              { value: "list", label: copy(pc, "studio.view.list"), onClick: () => switchView("list"), testId: "studio-view-list" },
              { value: "flow", label: copy(pc, "studio.view.flow"), onClick: () => switchView("flow"), testId: "studio-view-flow" },
            ]}
          />
        }
      />

      {result ? (
        <Alert severity={result.ok ? "success" : "error"} role="status">
          {result.ok ? <Check className="ic" /> : <AlertTriangle className="ic" />}
          <div>{result.message}</div>
        </Alert>
      ) : null}

      {view === "flow" ? (
        <ToxinFlow pc={pc} rows={rows} kindLabels={kindLabels} selected={selected} onSelect={setSelected} onInsert={insertAt} renderCard={cardFor} />
      ) : (
        <section className="card inspection-page">
          <div className="inspection-page-head" style={{ cursor: "default" }}>
            <strong>{copy(pc, "tsop.steps")}</strong>
            <span className="muted small">
              {rows.steps.length} {copy(pc, "tsop.steps.unit")}
              {totalWait > 0 ? ` · ${copy(pc, "tsop.steps.waiting")} ${waitWords(totalWait, pc)}` : ""}
            </span>
          </div>
          <div className="bd">
            <div className="qlist">
              {rows.steps.map((s, i) => stepCard(s, i))}
              <MuiButton type="button" variant="text" size="small" startIcon={<Plus className="ic" />} onClick={() => insertAt({ index: rows.steps.length })} data-testid="toxin-add-step">
                {copy(pc, "tsop.step.add")}
              </MuiButton>
            </div>
          </div>
        </section>
      )}

      <section className="card inspection-page">
        <div className="bd">
          <p className="muted small" style={{ margin: 0 }}>
            <Lock className="ic" style={{ width: 12 }} /> {copy(pc, "tsop.locked")}
          </p>
        </div>
      </section>

      <div className="cfgmf inspection-footer">
        <div>
          {problems.length ? (
            <ul className="small muted" style={{ margin: "4px 0 0 16px" }}>
              {problems.slice(0, 5).map((p, i) => (
                <li key={i}>{p}</li>
              ))}
            </ul>
          ) : (
            <span className="muted small">{copy(pc, "tsop.footer.ready")}</span>
          )}
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <MuiButton type="button" variant="outlined" disabled={pending || problems.length > 0} onClick={() => submit(false)}>
          {copy(pc, "inspection.action.save_draft")}
        </MuiButton>
        <MuiButton type="button" variant="contained" color="primary" startIcon={<Check className="ic" />} disabled={pending || problems.length > 0} onClick={() => submit(true)}>
          {copy(pc, "inspection.action.publish")}
        </MuiButton>
      </div>
    </Box>
  );
}
