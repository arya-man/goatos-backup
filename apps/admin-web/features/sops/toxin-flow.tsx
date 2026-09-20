"use client";

import { Clock, Plus } from "lucide-react";
import { useMemo, type ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FlowCanvas, type CanvasEdge, type CanvasLayout, type CanvasNode } from "./flow-canvas";
import { NODE_H, NODE_W } from "./flow-layout";
import type { ToxinRows, ToxinStepRow } from "./toxin-model";

export type ToxinInsert = { index: number };

type Data = { stepId?: string };

const GAP_Y = 54;

/**
 * The FLOW view of the aflatoxin procedure (THE TOXIN PROCEDURE IS AUTHORED, 2026-09-20): the
 * steps down a spine in the order the tester runs them, with each WAIT drawn on the line that
 * waits -- the gate is a property of the edge between two steps, not a box the tester taps, and
 * drawing it that way is what makes an hour of settling legible at a glance.
 *
 * Same rows as the List view; a + on a line inserts a step there.
 */
export function ToxinFlow({
  pc,
  rows,
  kindLabels,
  selected,
  onSelect,
  onInsert,
  renderCard,
}: {
  pc: AdminUiPageContract;
  rows: ToxinRows;
  kindLabels: Record<string, string>;
  selected: string | null;
  onSelect: (stepId: string) => void;
  onInsert: (insert: ToxinInsert) => void;
  renderCard: (stepId: string) => ReactNode;
}) {
  const layout = useMemo<CanvasLayout<Data, ToxinInsert>>(() => {
    const nodes: CanvasNode<Data>[] = [];
    const edges: CanvasEdge<ToxinInsert>[] = [];
    const centre = NODE_W / 2 + 40;
    const link = (from: string, to: string, insert?: ToxinInsert, label = "") => edges.push({ id: `${from}->${to}`, from, to, label, insert });
    let y = 0;
    nodes.push({ id: "start", tid: "start", kind: "start", x: centre - NODE_W / 2, y, w: NODE_W, h: NODE_H, data: {} });
    y += NODE_H + GAP_Y;
    let prev = "start";
    rows.steps.forEach((step, i) => {
      const id = `step:${step.id}`;
      nodes.push({ id, tid: `toxin-step-${i + 1}`, kind: step.kind === "wait" ? "fixed" : "step", x: centre - NODE_W / 2, y, w: NODE_W, h: NODE_H, data: { stepId: step.id }, selectable: true });
      // The gate is the WAIT on the line INTO this step: "1 hour after Mix and shake".
      const gateLabel =
        step.kind !== "wait" && step.gateAfterStep > 0 && step.gateMinutes > 0
          ? `${copy(pc, "tsop.flow.after")} ${waitWords(step.gateMinutes, pc)} · ${rows.steps[step.gateAfterStep - 1]?.title || `#${step.gateAfterStep}`}`
          : "";
      link(prev, id, { index: i }, gateLabel);
      prev = id;
      y += NODE_H + GAP_Y;
    });
    nodes.push({ id: "review", tid: "review", kind: "fixed", x: centre - NODE_W / 2, y, w: NODE_W, h: NODE_H, data: {} });
    link(prev, "review", { index: rows.steps.length });
    y += NODE_H + GAP_Y;
    nodes.push({ id: "finish", tid: "finish", kind: "finish", x: centre - NODE_W / 2, y, w: NODE_W, h: NODE_H, data: {} });
    link("review", "finish");
    y += NODE_H;
    return { nodes, edges, width: Math.max(...nodes.map((n) => n.x + n.w)) + 24, height: y + 24 };
  }, [rows, pc]);

  const renderNode = (node: CanvasNode<Data>): ReactNode => {
    if (node.kind === "start") {
      return (
        <>
          <span className="studio-node-kind">{copy(pc, "studio.flow.start")}</span>
          <b>{copy(pc, "tsop.flow.start")}</b>
          <span className="muted small">{copy(pc, "tsop.flow.start_hint")}</span>
        </>
      );
    }
    if (node.kind === "finish") {
      return (
        <>
          <span className="studio-node-kind">{copy(pc, "studio.flow.finish")}</span>
          <b>{copy(pc, "studio.flow.finish")}</b>
          <span className="muted small">{copy(pc, "tsop.flow.finish_hint")}</span>
        </>
      );
    }
    if (node.id === "review") {
      return (
        <>
          <span className="studio-node-kind">{copy(pc, "tsop.flow.locked")}</span>
          <b>{copy(pc, "tsop.flow.review")}</b>
          <span className="muted small">{copy(pc, "tsop.flow.review_hint")}</span>
        </>
      );
    }
    const step = rows.steps.find((s) => s.id === node.data?.stepId);
    if (!step) return null;
    if (step.kind === "wait") {
      return (
        <>
          <span className="studio-node-kind">
            <Clock size={11} /> {copy(pc, "tsop.flow.wait")}
          </span>
          <b>{step.title || copy(pc, "tsop.flow.wait")}</b>
          <span className="muted small">{waitWords(step.waitMinutes, pc)}</span>
        </>
      );
    }
    return (
      <>
        <span className="studio-node-kind">{kindLabels[step.kind] ?? step.kind}</span>
        <b>{step.title || copy(pc, "tsop.step.untitled")}</b>
        <span className="muted small">{step.instruction || copy(pc, "tsop.problem.instruction")}</span>
      </>
    );
  };

  return (
    <div className="studio-flow" data-testid="flow-view">
      <FlowCanvas
        pc={pc}
        layout={layout}
        selectedId={selected ? `step:${selected}` : ""}
        onSelect={(n) => n.data?.stepId && onSelect(n.data.stepId)}
        onInsert={(insert) => onInsert(insert)}
        renderNode={renderNode}
        hint={copy(pc, "tsop.flow.hint")}
      />
      <aside className="studio-flow-props card" data-testid="flow-props">
        <div className="hd">
          <h3>{copy(pc, "studio.flow.properties")}</h3>
        </div>
        {selected ? renderCard(selected) : <p className="muted" style={{ padding: 16 }}>{copy(pc, "studio.flow.none_selected")}</p>}
      </aside>
    </div>
  );
}

/** waitWords renders a duration the way the farm says it: "1 hour", "8 minutes". */
export function waitWords(minutes: number, pc: AdminUiPageContract): string {
  if (minutes <= 0) return "";
  if (minutes % 60 === 0) {
    const hours = minutes / 60;
    return `${hours} ${copy(pc, hours === 1 ? "tsop.unit.hour" : "tsop.unit.hours")}`;
  }
  return `${minutes} ${copy(pc, minutes === 1 ? "tsop.unit.minute" : "tsop.unit.minutes")}`;
}

/** ToxinStepSummaryLine is the one-line description a list row and the library card share. */
export function toxinStepSummaryLine(step: ToxinStepRow, kindLabels: Record<string, string>, pc: AdminUiPageContract): string {
  if (step.kind === "wait") return `${kindLabels.wait ?? "Wait"} · ${waitWords(step.waitMinutes, pc)}`;
  const gate = step.gateAfterStep > 0 && step.gateMinutes > 0 ? ` · ${copy(pc, "tsop.flow.after")} ${waitWords(step.gateMinutes, pc)}` : "";
  return `${kindLabels[step.kind] ?? step.kind}${gate}`;
}
