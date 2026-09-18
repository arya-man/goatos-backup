"use client";

import { GitBranch, Minus, Plus, Scan } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { branchPhrase } from "./branch-field";
import { layoutTrack, nextBranchCondition, NODE_W, type FlowCondition, type FlowInsert, type FlowLayout } from "./flow-layout";
import { stepAnswerKind, type FollowUpStepRow, type FollowUpTrackRows } from "./followup-model";

/**
 * The FLOW view of one follow-up track (SOP studio phase 2, 2026-09-18): the same steps the List
 * view edits, drawn as the chart the engine runs -- Start, the steps down a spine, a decision
 * under every question whose answer gates later steps, one column per branch, an "otherwise"
 * line, Finish. Click a step to edit it in the properties panel (the same card the List view
 * shows); click + on a line to insert a step THERE, on that path, with that branch's condition;
 * "Add branch" on a decision starts a new column. Layout is computed, never dragged: the order
 * the chart shows is the order the phone runs.
 */
export function FollowUpFlow({
  pc,
  track,
  answerKinds,
  answerKindLabels,
  taskTypes,
  selectedId,
  onSelect,
  onInsert,
  renderCard,
}: {
  pc: AdminUiPageContract;
  track: FollowUpTrackRows;
  answerKinds: Record<string, string>;
  /** Answer kind key -> farm label (the sop_answer_kinds option group). */
  answerKindLabels: Record<string, string>;
  taskTypes: { key: string; label: string }[];
  selectedId: string;
  onSelect: (id: string) => void;
  /** Insert a new step at `insert.index` carrying `insert.when`; returns the new row id. */
  onInsert: (insert: FlowInsert) => void;
  /** Renders the selected step's editing card. */
  renderCard: (step: FollowUpStepRow, index: number) => ReactNode;
}) {
  const answerKindOf = (s: FollowUpStepRow) => stepAnswerKind(s, answerKinds);
  const layout: FlowLayout = useMemo(
    () =>
      layoutTrack(
        track.steps,
        answerKindOf,
        (c: FlowCondition) => {
          const q = track.steps.find((s) => s.key === c.whenStep);
          return branchPhrase(pc, { ...track.steps[0], whenStep: c.whenStep, whenOp: c.whenOp, whenValues: c.whenValues } as FollowUpStepRow, q?.title || c.whenStep).replace(`${copy(pc, "studio.branch.note")} `, "");
        },
        copy(pc, "studio.flow.otherwise"),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [track, answerKinds, pc],
  );
  const [zoom, setZoom] = useState(1);
  const canvasRef = useRef<HTMLDivElement>(null);
  const fit = () => {
    const el = canvasRef.current;
    if (!el) return;
    const scale = Math.min(1, (el.clientWidth - 24) / layout.width);
    setZoom(Math.max(0.35, Math.round(scale * 100) / 100));
  };
  useEffect(() => {
    fit();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [layout.width]);
  const typeLabel = (key: string) => taskTypes.find((t) => t.key === key)?.label ?? key;
  const selected = track.steps.find((s) => s.id === selectedId);
  const selectedIndex = track.steps.findIndex((s) => s.id === selectedId);
  // Stable, human test ids: a step's key (not its row id), the decision as key:decision.
  const testId = (id: string) => {
    const n = layout.nodes.find((x) => x.id === id);
    if (!n || !n.step) return id;
    return n.kind === "decision" ? `${n.step.key}:decision` : n.step.key || `row${n.index}`;
  };
  const nodeCentre = (id: string) => {
    const n = layout.nodes.find((x) => x.id === id)!;
    return { top: { x: n.x + n.w / 2, y: n.y }, bottom: { x: n.x + n.w / 2, y: n.y + n.h } };
  };

  return (
    <div className="studio-flow" data-testid="flow-view">
      <div className="studio-flow-canvas-wrap">
        <div className="studio-flow-toolbar">
          <button type="button" className="btn sm ghost" onClick={() => setZoom((z) => Math.max(0.35, Math.round((z - 0.1) * 100) / 100))} aria-label={copy(pc, "studio.flow.zoom_out")}>
            <Minus size={14} />
          </button>
          <span className="muted small" data-testid="flow-zoom">
            {Math.round(zoom * 100)}%
          </span>
          <button type="button" className="btn sm ghost" onClick={() => setZoom((z) => Math.min(1.5, Math.round((z + 0.1) * 100) / 100))} aria-label={copy(pc, "studio.flow.zoom_in")}>
            <Plus size={14} />
          </button>
          <button type="button" className="btn sm ghost" onClick={fit}>
            <Scan size={14} /> {copy(pc, "studio.flow.fit")}
          </button>
          <span className="muted small studio-flow-hint">{copy(pc, "studio.flow.select_hint")}</span>
        </div>
        <div className="studio-flow-canvas" ref={canvasRef}>
          <div className="studio-flow-scale" style={{ width: layout.width, height: layout.height, transform: `scale(${zoom})` }}>
            <svg className="studio-flow-edges" width={layout.width} height={layout.height} aria-hidden="true">
              {layout.edges.map((e) => {
                const a = nodeCentre(e.from).bottom;
                const b = nodeCentre(e.to).top;
                const my = (a.y + b.y) / 2;
                const d = `M ${a.x} ${a.y} C ${a.x} ${my}, ${b.x} ${my}, ${b.x} ${b.y}`;
                return <path key={e.id} d={d} className="studio-flow-edge" markerEnd="url(#studio-arrow)" />;
              })}
              <defs>
                <marker id="studio-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse">
                  <path d="M 0 0 L 10 5 L 0 10 z" className="studio-flow-arrow" />
                </marker>
              </defs>
            </svg>
            {layout.edges.map((e) => {
              const a = nodeCentre(e.from).bottom;
              const b = nodeCentre(e.to).top;
              // Points along the same cubic the SVG draws: the label near the top of the line
              // (a branch line is read at its fork), the + past the middle.
              const at = (t: number) => {
                const my = (a.y + b.y) / 2;
                const u = 1 - t;
                return {
                  x: u * u * u * a.x + 3 * u * u * t * a.x + 3 * u * t * t * b.x + t * t * t * b.x,
                  y: u * u * u * a.y + 3 * u * u * t * my + 3 * u * t * t * my + t * t * t * b.y,
                };
              };
              const plus = at(e.label ? 0.62 : 0.5);
              const label = at(0.42);
              return (
                <div key={e.id + ":ins"}>
                  {e.label ? (
                    <span className="studio-flow-edge-label" style={{ left: label.x, top: label.y }}>
                      {e.label}
                    </span>
                  ) : null}
                  <button type="button" className="studio-flow-plus" style={{ left: plus.x, top: plus.y }} title={copy(pc, "studio.flow.insert")} aria-label={copy(pc, "studio.flow.insert")} onClick={() => onInsert(e.insert)} data-testid={`flow-insert-${testId(e.from)}-${testId(e.to)}`}>
                    +
                  </button>
                </div>
              );
            })}
            {layout.nodes.map((n) => {
              if (n.kind === "start" || n.kind === "finish") {
                return (
                  <div key={n.id} className={`studio-node studio-node-${n.kind}`} style={{ left: n.x, top: n.y, width: n.w, height: n.h }}>
                    <span className="studio-node-kind">{copy(pc, n.kind === "start" ? "studio.flow.start" : "studio.flow.finish")}</span>
                    <b>{n.kind === "start" ? track.label || track.key : copy(pc, "studio.flow.finish")}</b>
                    <span className="muted small">{copy(pc, n.kind === "start" ? "studio.flow.start_hint" : "studio.flow.finish_hint")}</span>
                  </div>
                );
              }
              if (n.kind === "decision") {
                const q = n.step!;
                const kind = answerKindOf(q);
                const branches = track.steps.filter((s) => s.whenStep === q.key);
                const seen = new Map<string, FlowCondition>();
                for (const b of branches) seen.set(`${b.whenOp}|${b.whenValues.join(",")}`, { whenStep: q.key, whenOp: b.whenOp, whenValues: b.whenValues });
                return (
                  <div key={n.id} className="studio-node studio-node-decision" style={{ left: n.x, top: n.y, width: n.w, height: n.h }} data-testid={`flow-decision-${q.key}`}>
                    <span className="studio-node-kind">
                      <GitBranch size={12} /> {copy(pc, "studio.flow.decision")}
                    </span>
                    <button
                      type="button"
                      className="btn sm ghost"
                      onClick={() => {
                        const insertAt = Math.max(...[n.index!, ...branches.map((b) => track.steps.indexOf(b))]) + 1;
                        onInsert({ index: insertAt, when: nextBranchCondition(q, kind, [...seen.values()].map((c) => ({ condition: c }))) });
                      }}
                      data-testid={`flow-add-branch-${q.key}`}
                    >
                      <Plus size={12} /> {copy(pc, "studio.flow.add_branch")}
                    </button>
                  </div>
                );
              }
              const s = n.step!;
              const kind = answerKindOf(s);
              const proof = [s.proofVideos > 0 ? `${s.proofVideos} ${copy(pc, "studio.flow.videos")}` : "", s.proofPhotos > 0 ? `${s.proofPhotos} ${copy(pc, "studio.flow.photos")}` : ""].filter(Boolean).join(" · ");
              return (
                <button
                  key={n.id}
                  type="button"
                  className={`studio-node studio-node-${n.kind}${selectedId === n.id ? " on" : ""}`}
                  style={{ left: n.x, top: n.y, width: n.w, height: n.h }}
                  onClick={() => onSelect(n.id)}
                  data-testid={`flow-node-${s.key || n.index}`}
                >
                  <span className="studio-node-kind">{typeLabel(s.taskType)}</span>
                  <b>{s.title || s.titlePattern || copy(pc, "followup.step.title")}</b>
                  <span className="muted small">{[kind && kind !== "none" ? answerKindLabels[kind] ?? kind : "", proof].filter(Boolean).join(" · ")}</span>
                </button>
              );
            })}
          </div>
        </div>
      </div>
      <aside className="studio-flow-props card" data-testid="flow-props">
        <div className="hd">
          <h3>{copy(pc, "studio.flow.properties")}</h3>
        </div>
        {selected ? renderCard(selected, selectedIndex) : <p className="muted" style={{ padding: 16 }}>{copy(pc, "studio.flow.none_selected")}</p>}
      </aside>
    </div>
  );
}

export { NODE_W as FLOW_NODE_WIDTH };
