"use client";

import { EmptyState } from "@/components/app/empty-state";

import { useMemo, type ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { FlowCanvas, FlowStudio, NodeActions, NodeButton, NodeKind, NodeNote, NodeTitle, type CanvasEdge, type CanvasLayout, type CanvasNode } from "./flow-canvas";
import { Iconify } from "@/components/minimal/iconify";
import { DECISION_H, NODE_H, NODE_W } from "./flow-layout";
import type { CountedProofRow, RemovalProofRow, WeighingQuestionRow, WeighingRows } from "./weighing-model";

/** Which authored list a chart item belongs to. */
export type WeighingList = "removalProofs" | "removalQuestions" | "individualProofs" | "individualQuestions" | "lumpSumProofs" | "lumpSumQuestions";
/** The three question lists; the other three hold proof slots. */
export function isQuestionList(list: WeighingList): boolean {
  return list === "removalQuestions" || list === "individualQuestions" || list === "lumpSumQuestions";
}

/** A chart item: one row of one list. */
export type WeighingRef = { list: WeighingList; id: string };

/** What a + on a line does: insert into `list` at `index`. */
export type WeighingInsert = { list: WeighingList; index: number };

type Data = { ref?: WeighingRef; fixed?: string; group?: "individual" | "lump_sum" | "removal"; offered?: boolean };

const GAP_Y = 54;
const COL_X = 300;
/** A section header carries its two add buttons under the title, so it is taller than a step. */
const GROUP_H = NODE_H + 26;

/**
 * The FLOW view of the weighing session SOP (SOP studio phase 2, 2026-09-18). The session is
 * not a step list -- it is a plan, an optional removal card, and TWO capture sections, one per
 * way of weighing -- so the chart draws exactly that: a decision "Weighed as" with a Per animal
 * column and a Whole pen column. A capture or question inserted on the Whole pen side applies
 * to whole-pen weighs only, which is the maintainer's "add something only for lump sum". The
 * fixed steps (scan, weight, submit, verify) are drawn locked: they are the scan-and-submit
 * rules the document cannot change.
 */
export function WeighingFlow({
  pc,
  rows,
  proofKindLabels,
  selected,
  onSelect,
  onInsert,
  renderCard,
}: {
  pc: AdminUiPageContract;
  rows: WeighingRows;
  proofKindLabels: Record<string, string>;
  selected: WeighingRef | null;
  onSelect: (ref: WeighingRef) => void;
  onInsert: (insert: WeighingInsert) => void;
  renderCard: (ref: WeighingRef) => ReactNode;
}) {
  const layout = useMemo<CanvasLayout<Data, WeighingInsert>>(() => {
    const nodes: CanvasNode<Data>[] = [];
    const edges: CanvasEdge<WeighingInsert>[] = [];
    const centre = NODE_W / 2 + COL_X + 12;
    const push = (n: CanvasNode<Data>) => nodes.push(n);
    const link = (from: string, to: string, insert?: WeighingInsert, label = "") => edges.push({ id: `${from}->${to}`, from, to, label, insert });
    const fixed = (id: string, x: number, y: number, key: string, group?: Data["group"]) => {
      push({ id, tid: id, kind: "fixed", x: x - NODE_W / 2, y, w: NODE_W, h: NODE_H, data: { fixed: key, group } });
      return id;
    };
    const questionLabel = (q: WeighingQuestionRow, list: WeighingQuestionRow[]) => {
      if (!q.onlyIfQuestion) return "";
      const dep = list.find((d) => d.key === q.onlyIfQuestion);
      return `${copy(pc, "studio.branch.note")} “${dep?.title || q.onlyIfQuestion}” ${copy(pc, "studio.branch.op.eq")} ${q.onlyIfValue}`;
    };
    // A column of one section's items: slots then questions, chained; every line inserts.
    const column = (x: number, y: number, from: string, slots: WeighingList, questions: WeighingList, group: Data["group"]): { tail: string; bottom: number } => {
      let prev = from;
      let cursor = y;
      const slotRows = rows[slots] as (RemovalProofRow | CountedProofRow)[];
      slotRows.forEach((p, i) => {
        const id = `${slots}:${p.id}`;
        push({ id, tid: `${slots}-${p.key || i}`, kind: "step", x: x - NODE_W / 2, y: cursor, w: NODE_W, h: NODE_H, data: { ref: { list: slots, id: p.id }, group }, selectable: true });
        link(prev, id, { list: slots, index: i });
        prev = id;
        cursor += NODE_H + GAP_Y;
      });
      const qRows = rows[questions] as WeighingQuestionRow[];
      qRows.forEach((q, i) => {
        const id = `${questions}:${q.id}`;
        push({ id, tid: `${questions}-${q.key || i}`, kind: "question", x: x - NODE_W / 2, y: cursor, w: NODE_W, h: NODE_H, data: { ref: { list: questions, id: q.id }, group }, selectable: true });
        link(prev, id, i === 0 && slotRows.length === 0 ? { list: slots, index: 0 } : { list: questions, index: i }, questionLabel(q, qRows));
        prev = id;
        cursor += NODE_H + GAP_Y;
      });
      return { tail: prev, bottom: cursor };
    };

    let y = 0;
    push({ id: "start", tid: "start", kind: "start", x: centre - NODE_W / 2, y, w: NODE_W, h: NODE_H, data: {} });
    y += NODE_H + GAP_Y;
    let prev = "start";
    if (rows.removalMode !== "off") {
      push({ id: "removal", tid: "removal", kind: "group", x: centre - NODE_W / 2, y, w: NODE_W, h: GROUP_H, data: { group: "removal" } });
      link(prev, "removal");
      y += GROUP_H + GAP_Y;
      const col = column(centre, y, "removal", "removalProofs", "removalQuestions", "removal");
      prev = col.tail;
      y = col.bottom;
      // The line out of the removal card inserts a removal question at the end.
    }
    push({ id: "decision", tid: "decision", kind: "decision", x: centre - NODE_W / 2, y, w: NODE_W, h: DECISION_H, data: {} });
    link(prev, "decision", rows.removalMode !== "off" ? { list: "removalQuestions", index: rows.removalQuestions.length } : undefined);
    y += DECISION_H + GAP_Y;
    // Per animal (left) and Whole pen (right).
    const leftX = centre - COL_X;
    const rightX = centre + COL_X;
    push({ id: "individual", tid: "individual", kind: "group", x: leftX - NODE_W / 2, y, w: NODE_W, h: GROUP_H, data: { group: "individual", offered: rows.modes.includes("individual_animal") } });
    link("decision", "individual", undefined, copy(pc, "wsop.flow.individual"));
    push({ id: "lump_sum", tid: "lump_sum", kind: "group", x: rightX - NODE_W / 2, y, w: NODE_W, h: GROUP_H, data: { group: "lump_sum", offered: rows.modes.includes("per_shed_partition") } });
    link("decision", "lump_sum", undefined, copy(pc, "wsop.flow.lump_sum"));
    y += GROUP_H + GAP_Y;
    let ly = y;
    fixed("scan", leftX, ly, "wsop.flow.scan", "individual");
    link("individual", "scan");
    ly += NODE_H + GAP_Y;
    fixed("weight", leftX, ly, "wsop.flow.weight", "individual");
    link("scan", "weight");
    ly += NODE_H + GAP_Y;
    const left = column(leftX, ly, "weight", "individualProofs", "individualQuestions", "individual");
    let ry = y;
    fixed("total", rightX, ry, "wsop.flow.total", "lump_sum");
    link("lump_sum", "total");
    ry += NODE_H + GAP_Y;
    const right = column(rightX, ry, "total", "lumpSumProofs", "lumpSumQuestions", "lump_sum");
    y = Math.max(left.bottom, right.bottom);
    fixed("submit", centre, y, "wsop.flow.submit");
    link(left.tail, "submit", { list: "individualQuestions", index: rows.individualQuestions.length });
    link(right.tail, "submit", { list: "lumpSumQuestions", index: rows.lumpSumQuestions.length });
    y += NODE_H + GAP_Y;
    fixed("verify", centre, y, "wsop.flow.verify");
    link("submit", "verify");
    y += NODE_H + GAP_Y;
    push({ id: "finish", tid: "finish", kind: "finish", x: centre - NODE_W / 2, y, w: NODE_W, h: NODE_H, data: {} });
    link("verify", "finish");
    y += NODE_H;
    return { nodes, edges, width: Math.max(...nodes.map((n) => n.x + n.w)) + 24, height: y + 24 };
  }, [rows, pc]);

  const selectedId = selected ? `${selected.list}:${selected.id}` : "";
  const renderNode = (node: CanvasNode<Data>): ReactNode => {
    const d = node.data ?? {};
    if (node.kind === "start") {
      return (
        <>
          <NodeKind>{copy(pc, "studio.flow.start")}</NodeKind>
          <NodeTitle>{copy(pc, "wsop.flow.start")}</NodeTitle>
          <NodeNote>{rows.modes.map((m) => copy(pc, `wsop.planning.mode.${m}`)).join(" · ")}</NodeNote>
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
      return <NodeKind icon="solar:transfer-horizontal-bold-duotone">{copy(pc, "wsop.flow.decision")}</NodeKind>;
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
      const slots: WeighingList = d.group === "removal" ? "removalProofs" : d.group === "individual" ? "individualProofs" : "lumpSumProofs";
      const questions: WeighingList = d.group === "removal" ? "removalQuestions" : d.group === "individual" ? "individualQuestions" : "lumpSumQuestions";
      const title = d.group === "removal" ? copy(pc, "wsop.flow.removal") : d.group === "individual" ? copy(pc, "wsop.flow.individual") : copy(pc, "wsop.flow.lump_sum");
      return (
        <>
          <NodeKind>{d.group === "removal" ? copy(pc, "wsop.flow.removal_hint") : d.offered === false ? copy(pc, "wsop.flow.mode_not_offered") : copy(pc, "wsop.section.capture")}</NodeKind>
          <NodeTitle>{title}</NodeTitle>
          <NodeActions>
            <NodeButton onClick={() => onInsert({ list: slots, index: rows[slots].length })} testId={`flow-add-${slots}`}>
              {copy(pc, "wsop.flow.add_capture")}
            </NodeButton>
            <NodeButton onClick={() => onInsert({ list: questions, index: rows[questions].length })} testId={`flow-add-${questions}`}>
              {copy(pc, "wsop.flow.add_question")}
            </NodeButton>
          </NodeActions>
        </>
      );
    }
    const ref = d.ref!;
    if (isQuestionList(ref.list)) {
      const q = (rows[ref.list] as WeighingQuestionRow[]).find((x) => x.id === ref.id);
      return (
        <>
          <NodeKind>{copy(pc, "wsop.flow.question")}</NodeKind>
          <NodeTitle>{q?.title || copy(pc, "wsop.question.title")}</NodeTitle>
          <NodeNote>{q?.required ? copy(pc, "wsop.flow.compulsory") : copy(pc, "wsop.flow.optional")}</NodeNote>
        </>
      );
    }
    const p = (rows[ref.list] as (RemovalProofRow | CountedProofRow)[]).find((x) => x.id === ref.id)!;
    const counted = "min" in p;
    return (
      <>
        <NodeKind>{copy(pc, "wsop.flow.capture")}</NodeKind>
        <NodeTitle>{p.title || proofKindLabels[p.kind] || p.kind}</NodeTitle>
        <NodeNote>
          {proofKindLabels[p.kind] ?? p.kind} ·{" "}
          {counted
            ? `${(p as CountedProofRow).min || 0} ${copy(pc, "wsop.flow.count")} ${(p as CountedProofRow).max || 0}`
            : (p as RemovalProofRow).required
              ? copy(pc, "wsop.flow.compulsory")
              : copy(pc, "wsop.flow.optional")}
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
          hint={copy(pc, "wsop.flow.hint")}
        />
      }
    >
      {selected ? renderCard(selected) : <EmptyState title={copy(pc, "studio.flow.none_selected")} icon={<Iconify icon="solar:info-circle-bold" width={24} />} />}
    </FlowStudio>
  );
}
