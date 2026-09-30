"use client";

import { useMemo, type ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FlowCanvas, FlowStudio, NodeActions, NodeButton, NodeKind, NodeNote, NodeTitle, type CanvasEdge, type CanvasLayout, type CanvasNode } from "./flow-canvas";
import { NODE_H, NODE_W } from "./flow-layout";
import { PC_CARE_CATEGORIES, type PcCareCategory, type PcCareRows } from "./pc-care-model";
import type { WeighingQuestionRow } from "./weighing-model";
import { EmptyState } from "@/components/app/empty-state";

/** A row the chart can select: one section's capture or question. "removal" is the pen card. */
export type PcCareSection = PcCareCategory | "removal";
export type PcCareRef = { section: PcCareSection; kind: "proof" | "question"; id: string };
export type PcCareInsert = { section: PcCareSection; kind: "proof" | "question"; index: number };

type Data = { ref?: PcCareRef; section?: PcCareSection; fixed?: string };

const GAP_Y = 54;
const GAP_X = 40;
const GROUP_H = NODE_H + 26;

/**
 * The FLOW view of the Preventive Care SOP (the SOP studio's List | Flow pair, 2026-09-18).
 *
 * The shape IS the work: a task is planned, the evening-before removal runs (or does not), and
 * then the operator works ONE kind of care -- so the chart forks into a column per work category,
 * each with its own captures and questions, and the columns rejoin at the submit and the
 * verifier's review. A capture added on the Hoof trimming column applies to hoof trimming alone;
 * that is what the per-category cards mean, drawn.
 *
 * The fixed steps -- how the operator reaches the animal, the free-flow scan, the whole-task
 * submit, the one-item-per-task review -- are drawn LOCKED: they are the module's rules, shown
 * rather than edited.
 */
export function PcCareFlow({
  pc,
  rows,
  categoryLabel,
  proofKindLabels,
  selected,
  onSelect,
  onInsert,
  renderCard,
}: {
  pc: AdminUiPageContract;
  rows: PcCareRows;
  categoryLabel: (category: PcCareCategory) => string;
  proofKindLabels: Record<string, string>;
  selected: PcCareRef | null;
  onSelect: (ref: PcCareRef) => void;
  onInsert: (insert: PcCareInsert) => void;
  renderCard: (ref: PcCareRef) => ReactNode;
}) {
  const layout = useMemo<CanvasLayout<Data, PcCareInsert>>(() => {
    const nodes: CanvasNode<Data>[] = [];
    const edges: CanvasEdge<PcCareInsert>[] = [];
    const colW = NODE_W + GAP_X;
    const columns = PC_CARE_CATEGORIES.length;
    const centre = (columns * colW) / 2 + 24 - GAP_X / 2;
    const link = (from: string, to: string, insert?: PcCareInsert, label = "") => edges.push({ id: `${from}->${to}`, from, to, label, insert });
    const spine = (id: string, kind: CanvasNode<Data>["kind"], y: number, data: Data = {}) => {
      nodes.push({ id, tid: id, kind, x: centre - NODE_W / 2, y, w: NODE_W, h: kind === "group" ? GROUP_H : NODE_H, data });
    };

    let y = 0;
    spine("start", "start", y);
    y += NODE_H + GAP_Y;
    let prev = "start";
    let prevInsert: PcCareInsert | undefined;

    // The evening-before removal, when the document has one.
    if (rows.removal.mode === "off") {
      spine("removal-off", "fixed", y, { fixed: "pcsop.flow.removal_off" });
      link(prev, "removal-off");
      prev = "removal-off";
      y += NODE_H + GAP_Y;
    } else {
      spine("removal", "group", y, { section: "removal" });
      link(prev, "removal");
      y += GROUP_H + GAP_Y;
      prev = "removal";
      rows.removal.proofs.forEach((p, i) => {
        const id = `removal:proof:${p.id}`;
        nodes.push({
          id,
          tid: `removal-proof-${p.key || i}`,
          kind: "step",
          x: centre - NODE_W / 2,
          y,
          w: NODE_W,
          h: NODE_H,
          data: { ref: { section: "removal", kind: "proof", id: p.id } },
          selectable: true,
        });
        link(prev, id, { section: "removal", kind: "proof", index: i });
        prev = id;
        y += NODE_H + GAP_Y;
      });
      rows.removal.questions.forEach((q, i) => {
        const id = `removal:question:${q.id}`;
        nodes.push({
          id,
          tid: `removal-question-${q.key || i}`,
          kind: "question",
          x: centre - NODE_W / 2,
          y,
          w: NODE_W,
          h: NODE_H,
          data: { ref: { section: "removal", kind: "question", id: q.id } },
          selectable: true,
        });
        link(prev, id, { section: "removal", kind: "question", index: i });
        prev = id;
        y += NODE_H + GAP_Y;
      });
      prevInsert = { section: "removal", kind: "question", index: rows.removal.questions.length };
    }

    // The fork: one column per kind of work.
    spine("decision", "decision", y, {});
    link(prev, "decision", prevInsert);
    y += NODE_H + GAP_Y;
    const forkY = y;
    let deepest = y;
    PC_CARE_CATEGORIES.forEach((category, ci) => {
      const x = 24 + ci * colW;
      let cy = forkY;
      const gid = `cat:${category}`;
      nodes.push({ id: gid, tid: gid, kind: "group", x, y: cy, w: NODE_W, h: GROUP_H, data: { section: category } });
      link("decision", gid, undefined, categoryLabel(category));
      cy += GROUP_H + GAP_Y;
      let columnPrev = gid;
      const block = rows.categories[category];
      block.proofs.forEach((p, i) => {
        const id = `${category}:proof:${p.id}`;
        nodes.push({ id, tid: `${category}-proof-${p.key || i}`, kind: "step", x, y: cy, w: NODE_W, h: NODE_H, data: { ref: { section: category, kind: "proof", id: p.id } }, selectable: true });
        link(columnPrev, id, { section: category, kind: "proof", index: i });
        columnPrev = id;
        cy += NODE_H + GAP_Y;
      });
      block.questions.forEach((q, i) => {
        const id = `${category}:question:${q.id}`;
        nodes.push({
          id,
          tid: `${category}-question-${q.key || i}`,
          kind: "question",
          x,
          y: cy,
          w: NODE_W,
          h: NODE_H,
          data: { ref: { section: category, kind: "question", id: q.id } },
          selectable: true,
        });
        const dep = q.onlyIfQuestion ? block.questions.find((d) => d.key === q.onlyIfQuestion) : undefined;
        const label = q.onlyIfQuestion ? `${copy(pc, "studio.branch.note")} “${dep?.title || q.onlyIfQuestion}” ${copy(pc, "studio.branch.op.eq")} ${q.onlyIfValue}` : "";
        link(columnPrev, id, i === 0 && block.proofs.length === 0 ? { section: category, kind: "proof", index: 0 } : { section: category, kind: "question", index: i }, label);
        columnPrev = id;
        cy += NODE_H + GAP_Y;
      });
      deepest = Math.max(deepest, cy);
    });

    // The columns rejoin: the task is submitted whole and reviewed as one item.
    y = deepest;
    spine("submit", "fixed", y, { fixed: "pcsop.flow.submit" });
    for (const category of PC_CARE_CATEGORIES) {
      const block = rows.categories[category];
      const last =
        block.questions.length > 0
          ? `${category}:question:${block.questions[block.questions.length - 1].id}`
          : block.proofs.length > 0
            ? `${category}:proof:${block.proofs[block.proofs.length - 1].id}`
            : `cat:${category}`;
      link(last, "submit", { section: category, kind: "question", index: block.questions.length });
    }
    y += NODE_H + GAP_Y;
    spine("verify", "fixed", y, { fixed: "pcsop.flow.verify" });
    link("submit", "verify");
    y += NODE_H + GAP_Y;
    spine("finish", "finish", y);
    link("verify", "finish");
    y += NODE_H;
    return { nodes, edges, width: Math.max(...nodes.map((n) => n.x + n.w)) + 24, height: y + 24 };
  }, [rows, pc, categoryLabel]);

  const selectedId = selected ? `${selected.section}:${selected.kind}:${selected.id}` : "";
  const renderNode = (node: CanvasNode<Data>): ReactNode => {
    const d = node.data ?? {};
    if (node.kind === "start") {
      return (
        <>
          <NodeKind>{copy(pc, "studio.flow.start")}</NodeKind>
          <NodeTitle>{copy(pc, "pcsop.flow.start")}</NodeTitle>
          <NodeNote>{copy(pc, "pcsop.flow.start_hint")}</NodeNote>
        </>
      );
    }
    if (node.kind === "finish") {
      return (
        <>
          <NodeKind>{copy(pc, "studio.flow.finish")}</NodeKind>
          <NodeTitle>{copy(pc, "studio.flow.finish")}</NodeTitle>
          <NodeNote>{copy(pc, "studio.flow.finish_hint")}</NodeNote>
        </>
      );
    }
    if (node.kind === "decision") {
      return (
        <>
          <NodeKind>{copy(pc, "studio.flow.decision")}</NodeKind>
          <NodeTitle>{copy(pc, "pcsop.flow.decision")}</NodeTitle>
        </>
      );
    }
    if (node.kind === "fixed") {
      return (
        <>
          <NodeKind icon="solar:lock-password-outline">{copy(pc, "wsop.capture.individual.locked_short")}</NodeKind>
          <NodeTitle>{copy(pc, d.fixed!)}</NodeTitle>
          <NodeNote>{copy(pc, `${d.fixed}_hint`)}</NodeNote>
        </>
      );
    }
    if (node.kind === "group") {
      const section = d.section!;
      const isRemoval = section === "removal";
      const block = isRemoval ? rows.removal : rows.categories[section as PcCareCategory];
      return (
        <>
          <NodeKind>{copy(pc, isRemoval ? "pcsop.flow.removal" : "pcsop.flow.card")}</NodeKind>
          <NodeTitle>{isRemoval ? copy(pc, "pcsop.section.removal") : categoryLabel(section as PcCareCategory)}</NodeTitle>
          {!isRemoval ? (
            <NodeNote>
              {(section as PcCareCategory) === "hoof_trimming" || (section as PcCareCategory) === "hair_trimming" ? copy(pc, "pcsop.flow.reach_roster") : copy(pc, "pcsop.flow.reach_scan")}
            </NodeNote>
          ) : null}
          <NodeActions>
            <NodeButton onClick={() => onInsert({ section, kind: "proof", index: block.proofs.length })} testId={`flow-add-${section}-proof`}>
              {copy(pc, "pcsop.category.add_capture")}
            </NodeButton>
            <NodeButton onClick={() => onInsert({ section, kind: "question", index: block.questions.length })} testId={`flow-add-${section}-question`}>
              {copy(pc, "inspection.question.add")}
            </NodeButton>
          </NodeActions>
        </>
      );
    }
    const ref = d.ref!;
    const block = ref.section === "removal" ? rows.removal : rows.categories[ref.section as PcCareCategory];
    if (ref.kind === "question") {
      const q = block.questions.find((x) => x.id === ref.id) as WeighingQuestionRow | undefined;
      return (
        <>
          <NodeKind>{copy(pc, "pcsop.flow.question")}</NodeKind>
          <NodeTitle>{q?.title || copy(pc, "wsop.question.title")}</NodeTitle>
          <NodeNote>{q?.required ? copy(pc, "pcsop.flow.compulsory") : copy(pc, "pcsop.flow.optional")}</NodeNote>
        </>
      );
    }
    const p = block.proofs.find((x) => x.id === ref.id)!;
    return (
      <>
        <NodeKind>{copy(pc, "pcsop.flow.capture")}</NodeKind>
        <NodeTitle>{p.title || proofKindLabels[p.kind] || p.kind}</NodeTitle>
        <NodeNote>
          {proofKindLabels[p.kind] ?? p.kind} · {p.required ? copy(pc, "pcsop.flow.compulsory") : copy(pc, "pcsop.flow.optional")}
        </NodeNote>
      </>
    );
  };

  return (
    <FlowStudio
      pc={pc}
      canvas={
        <FlowCanvas
          pc={pc}
          layout={layout}
          selectedId={selectedId}
          onSelect={(n) => n.data?.ref && onSelect(n.data.ref)}
          onInsert={(insert) => onInsert(insert)}
          renderNode={renderNode}
          hint={copy(pc, "pcsop.flow.hint")}
        />
      }
    >
      {/* Nothing picked is an EMPTY STATE, not a paragraph under the heading: the frame has one
            shape for "there is nothing here yet" and this panel uses it like every other. */}
      {selected ? renderCard(selected) : <EmptyState title={copy(pc, "studio.flow.none_selected")} />}
    </FlowStudio>
  );
}
