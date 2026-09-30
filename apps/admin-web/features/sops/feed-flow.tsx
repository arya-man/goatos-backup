"use client";

import { EmptyState } from "@/components/app/empty-state";

import { Iconify } from "@/components/minimal/iconify";
import { useMemo, type ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FlowCanvas, type CanvasEdge, type CanvasLayout, type CanvasNode } from "./flow-canvas";
import { NODE_H, NODE_W } from "./flow-layout";
import type { FeedRows, FeedStage } from "./feed-model";
import type { RemovalProofRow, WeighingQuestionRow } from "./weighing-model";

export type FeedRef = { stage: FeedStage; kind: "proof" | "question"; id: string };
export type FeedInsert = { stage: FeedStage; kind: "proof" | "question"; index: number };

type Data = { ref?: FeedRef; stage?: FeedStage; fixed?: string };

const GAP_Y = 54;
const FEED_NODE_H = NODE_H + 20;
const GROUP_H = FEED_NODE_H + 34;

/**
 * The FLOW view of a feed SOP (SOP studio phase 2, 2026-09-18): the cards the crew runs, in the
 * order the feed chain runs them -- for feed.direction the distribution card then the leftover
 * card -- each a header with its captures and questions chained under it, then the verifier's
 * review. Same rows as the List view; a + on a line inserts on that card.
 */
export function FeedFlow({
  pc,
  rows,
  stages,
  proofKindLabels,
  selected,
  onSelect,
  onInsert,
  renderCard,
}: {
  pc: AdminUiPageContract;
  rows: FeedRows;
  stages: FeedStage[];
  proofKindLabels: Record<string, string>;
  selected: FeedRef | null;
  onSelect: (ref: FeedRef) => void;
  onInsert: (insert: FeedInsert) => void;
  renderCard: (ref: FeedRef) => ReactNode;
}) {
  const layout = useMemo<CanvasLayout<Data, FeedInsert>>(() => {
    const nodes: CanvasNode<Data>[] = [];
    const edges: CanvasEdge<FeedInsert>[] = [];
    const centre = NODE_W / 2 + 40;
    const link = (from: string, to: string, insert?: FeedInsert, label = "") => edges.push({ id: `${from}->${to}`, from, to, label, insert });
    let y = 0;
    nodes.push({ id: "start", tid: "start", kind: "start", x: centre - NODE_W / 2, y, w: NODE_W, h: FEED_NODE_H, data: {} });
    y += FEED_NODE_H + GAP_Y;
    let prev = "start";
    let prevInsert: FeedInsert | undefined;
    for (const stage of stages) {
      const block = rows.stages[stage];
      if (!block) continue;
      const gid = `stage:${stage}`;
      nodes.push({ id: gid, tid: gid, kind: "group", x: centre - NODE_W / 2, y, w: NODE_W, h: GROUP_H, data: { stage } });
      link(prev, gid, prevInsert);
      y += GROUP_H + GAP_Y;
      prev = gid;
      block.proofs.forEach((p, i) => {
        const id = `${stage}:proof:${p.id}`;
        nodes.push({ id, tid: `${stage}-proof-${p.key || i}`, kind: "step", x: centre - NODE_W / 2, y, w: NODE_W, h: FEED_NODE_H, data: { ref: { stage, kind: "proof", id: p.id } }, selectable: true });
        link(prev, id, { stage, kind: "proof", index: i });
        prev = id;
        y += FEED_NODE_H + GAP_Y;
      });
      block.questions.forEach((q, i) => {
        const id = `${stage}:question:${q.id}`;
        nodes.push({ id, tid: `${stage}-question-${q.key || i}`, kind: "question", x: centre - NODE_W / 2, y, w: NODE_W, h: FEED_NODE_H, data: { ref: { stage, kind: "question", id: q.id } }, selectable: true });
        const dep = q.onlyIfQuestion ? block.questions.find((d) => d.key === q.onlyIfQuestion) : undefined;
        const label = q.onlyIfQuestion ? `${copy(pc, "studio.branch.note")} “${dep?.title || q.onlyIfQuestion}” ${copy(pc, "studio.branch.op.eq")} ${q.onlyIfValue}` : "";
        link(prev, id, i === 0 && block.proofs.length === 0 ? { stage, kind: "proof", index: 0 } : { stage, kind: "question", index: i }, label);
        prev = id;
        y += FEED_NODE_H + GAP_Y;
      });
      prevInsert = { stage, kind: "question", index: block.questions.length };
    }
    nodes.push({ id: "verify", tid: "verify", kind: "fixed", x: centre - NODE_W / 2, y, w: NODE_W, h: FEED_NODE_H, data: { fixed: "fsop.flow.verify" } });
    link(prev, "verify", prevInsert);
    y += FEED_NODE_H + GAP_Y;
    nodes.push({ id: "finish", tid: "finish", kind: "finish", x: centre - NODE_W / 2, y, w: NODE_W, h: FEED_NODE_H, data: {} });
    link("verify", "finish");
    y += FEED_NODE_H;
    return { nodes, edges, width: Math.max(...nodes.map((n) => n.x + n.w)) + 24, height: y + 24 };
  }, [rows, stages, pc]);

  const selectedId = selected ? `${selected.stage}:${selected.kind}:${selected.id}` : "";
  const renderNode = (node: CanvasNode<Data>): ReactNode => {
    const d = node.data ?? {};
    if (node.kind === "start") {
      return (
        <>
          <span className="studio-node-kind">{copy(pc, "studio.flow.start")}</span>
          <b>{copy(pc, "fsop.flow.start")}</b>
          <span className="muted small">{copy(pc, "fsop.flow.start_hint")}</span>
        </>
      );
    }
    if (node.kind === "finish") {
      return (
        <>
          <span className="studio-node-kind">{copy(pc, "studio.flow.finish")}</span>
          <b>{copy(pc, "studio.flow.finish")}</b>
          <span className="muted small">{copy(pc, "studio.flow.finish_hint")}</span>
        </>
      );
    }
    if (node.kind === "fixed") {
      return (
        <>
          <span className="studio-node-kind">
            <Iconify icon="solar:lock-password-outline" width={11} /> {copy(pc, "wsop.capture.individual.locked_short")}
          </span>
          <b>{copy(pc, d.fixed!)}</b>
          <span className="muted small">{copy(pc, `${d.fixed}_hint`)}</span>
        </>
      );
    }
    if (node.kind === "group") {
      const stage = d.stage!;
      const block = rows.stages[stage]!;
      return (
        <>
          <span className="studio-node-kind">{copy(pc, "fsop.flow.card")}</span>
          <b>{copy(pc, `fsop.stage.${stage}`)}</b>
          <span className="studio-node-actions">
            <button type="button" className="btn sm ghost" onClick={() => onInsert({ stage, kind: "proof", index: block.proofs.length })} data-testid={`flow-add-${stage}-proof`}>
              <Iconify icon="mingcute:add-line" width={12} /> {copy(pc, "fsop.flow.add_capture")}
            </button>
            <button type="button" className="btn sm ghost" onClick={() => onInsert({ stage, kind: "question", index: block.questions.length })} data-testid={`flow-add-${stage}-question`}>
              <Iconify icon="mingcute:add-line" width={12} /> {copy(pc, "fsop.flow.add_question")}
            </button>
          </span>
        </>
      );
    }
    const ref = d.ref!;
    const block = rows.stages[ref.stage]!;
    if (ref.kind === "question") {
      const q = block.questions.find((x) => x.id === ref.id) as WeighingQuestionRow | undefined;
      return (
        <>
          <span className="studio-node-kind">{copy(pc, "fsop.flow.question")}</span>
          <b>{q?.title || copy(pc, "wsop.question.title")}</b>
          <span className="muted small">{q?.required ? copy(pc, "fsop.flow.compulsory") : copy(pc, "fsop.flow.optional")}</span>
        </>
      );
    }
    const p = block.proofs.find((x) => x.id === ref.id) as RemovalProofRow;
    return (
      <>
        <span className="studio-node-kind">{copy(pc, "fsop.flow.capture")}</span>
        <b>{p.title || proofKindLabels[p.kind] || p.kind}</b>
        <span className="muted small">
          {proofKindLabels[p.kind] ?? p.kind} · {p.required ? copy(pc, "fsop.flow.compulsory") : copy(pc, "fsop.flow.optional")}
        </span>
      </>
    );
  };

  return (
    <div className="studio-flow" data-testid="flow-view">
      <FlowCanvas pc={pc} layout={layout} selectedId={selectedId} onSelect={(n) => n.data?.ref && onSelect(n.data.ref)} onInsert={(insert) => onInsert(insert)} renderNode={renderNode} hint={copy(pc, "fsop.flow.hint")} />
      <aside className="studio-flow-props card" data-testid="flow-props">
        <div className="hd">
          <h3>{copy(pc, "studio.flow.properties")}</h3>
        </div>
        {selected ? renderCard(selected) : <EmptyState title={copy(pc, "studio.flow.none_selected")} icon={<Iconify icon="eva:diagonal-arrow-left-down-fill" />} style={{ padding: 16 }} />}
      </aside>
    </div>
  );
}
