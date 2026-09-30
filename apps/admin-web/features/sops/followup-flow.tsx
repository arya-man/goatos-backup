"use client";

import { EmptyState } from "@/components/app/empty-state";

import { Iconify } from "@/components/minimal/iconify";
import { useMemo, type ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { branchPhrase } from "./branch-field";
import { FlowCanvas, type CanvasLayout, type CanvasNode } from "./flow-canvas";
import { layoutTrack, nextBranchCondition, type FlowCondition, type FlowInsert, type FlowNode } from "./flow-layout";
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
  owners = [],
  selectedId,
  onSelect,
  onInsert,
  renderCard,
}: {
  pc: AdminUiPageContract;
  track: FollowUpTrackRows;
  answerKinds: Record<string, string>;
  /** Answer kind key -> farm label. */
  answerKindLabels: Record<string, string>;
  taskTypes: { key: string; label: string }[];
  /** Designations a step can be for; a node shows its owner's label as a chip. */
  owners?: { key: string; label: string }[];
  selectedId: string;
  onSelect: (id: string) => void;
  /** Insert a new step at `insert.index` carrying `insert.when`. */
  onInsert: (insert: FlowInsert) => void;
  /** Renders the selected step's editing card. */
  renderCard: (step: FollowUpStepRow, index: number) => ReactNode;
}) {
  const answerKindOf = (s: FollowUpStepRow) => stepAnswerKind(s, answerKinds);
  const layout = useMemo<CanvasLayout<FlowNode, FlowInsert>>(() => {
    const raw = layoutTrack(
      track.steps,
      answerKindOf,
      (c: FlowCondition) => {
        const q = track.steps.find((s) => s.key === c.whenStep);
        return branchPhrase(pc, { ...track.steps[0], whenStep: c.whenStep, whenOp: c.whenOp, whenValues: c.whenValues } as FollowUpStepRow, q?.title || c.whenStep).replace(`${copy(pc, "studio.branch.note")} `, "");
      },
      copy(pc, "studio.flow.otherwise"),
    );
    const tid = (n: FlowNode) => (n.step ? (n.kind === "decision" ? `${n.step.key}:decision` : n.step.key || `row${n.index}`) : n.id);
    return {
      nodes: raw.nodes.map((n) => ({ id: n.id, tid: tid(n), kind: n.kind, x: n.x, y: n.y, w: n.w, h: n.h, data: n, selectable: n.kind === "step" || n.kind === "question" })),
      edges: raw.edges.map((e) => ({ id: e.id, from: e.from, to: e.to, label: e.label, insert: e.insert })),
      width: raw.width,
      height: raw.height,
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [track, answerKinds, pc]);
  const typeLabel = (key: string) => taskTypes.find((t) => t.key === key)?.label ?? key;
  const ownerLabel = (key: string) => (key ? owners.find((o) => o.key === key)?.label ?? key : "");
  const selected = track.steps.find((s) => s.id === selectedId);
  const selectedIndex = track.steps.findIndex((s) => s.id === selectedId);

  const renderNode = (node: CanvasNode<FlowNode>): ReactNode => {
    const n = node.data!;
    if (n.kind === "start" || n.kind === "finish") {
      return (
        <>
          <span className="studio-node-kind">{copy(pc, n.kind === "start" ? "studio.flow.start" : "studio.flow.finish")}</span>
          <b>{n.kind === "start" ? track.label || track.key : copy(pc, "studio.flow.finish")}</b>
          <span className="muted small">{copy(pc, n.kind === "start" ? "studio.flow.start_hint" : "studio.flow.finish_hint")}</span>
        </>
      );
    }
    const s = n.step!;
    const kind = answerKindOf(s);
    if (n.kind === "decision") {
      const branches = track.steps.filter((x) => x.whenStep === s.key);
      const seen = new Map<string, FlowCondition>();
      for (const b of branches) seen.set(`${b.whenOp}|${b.whenValues.join(",")}`, { whenStep: s.key, whenOp: b.whenOp, whenValues: b.whenValues });
      return (
        <>
          <span className="studio-node-kind">
            <Iconify icon="solar:transfer-horizontal-bold-duotone" width={12} /> {copy(pc, "studio.flow.decision")}
          </span>
          <button
            type="button"
            className="btn sm ghost"
            onClick={() => {
              const insertAt = Math.max(...[n.index!, ...branches.map((b) => track.steps.indexOf(b))]) + 1;
              onInsert({ index: insertAt, when: nextBranchCondition(s, kind, [...seen.values()].map((c) => ({ condition: c }))) });
            }}
            data-testid={`flow-add-branch-${s.key}`}
          >
            <Iconify icon="mingcute:add-line" width={12} /> {copy(pc, "studio.flow.add_branch")}
          </button>
        </>
      );
    }
    const proof = [s.proofVideos > 0 ? `${s.proofVideos} ${copy(pc, "studio.flow.videos")}` : "", s.proofPhotos > 0 ? `${s.proofPhotos} ${copy(pc, "studio.flow.photos")}` : ""].filter(Boolean).join(" · ");
    // A question with NO branch yet still offers "Add branch": that is how the FIRST path is
    // started from the chart (edge-case audit 2026-09-18 -- before this the control appeared
    // only once a branch existed, so the first one had to be made from the Only-if field).
    const canBranch = n.branchable && s.key;
    return (
      <>
        <span className="studio-node-kind">{typeLabel(s.taskType)}</span>
        <b>{s.title || s.titlePattern || copy(pc, "followup.step.title")}</b>
        <span className="muted small">{[kind && kind !== "none" ? answerKindLabels[kind] ?? kind : "", proof].filter(Boolean).join(" · ")}</span>
        {s.owner ? (
          <span className="studio-node-owner" data-testid={`flow-owner-${s.key}`}>
            {copy(pc, "followup.step.owner")}: {ownerLabel(s.owner)}
          </span>
        ) : null}
        {canBranch ? (
          <span className="studio-node-actions">
            <button
              type="button"
              className="btn sm ghost"
              onClick={(e) => {
                e.stopPropagation();
                onInsert({ index: n.index! + 1, when: nextBranchCondition(s, kind, []) });
              }}
              data-testid={`flow-add-branch-${s.key}`}
            >
              <Iconify icon="mingcute:add-line" width={12} /> {copy(pc, "studio.flow.add_branch")}
            </button>
          </span>
        ) : null}
      </>
    );
  };

  return (
    <div className="studio-flow" data-testid="flow-view">
      <FlowCanvas pc={pc} layout={layout} selectedId={selectedId} onSelect={(node) => onSelect(node.id)} onInsert={(insert) => onInsert(insert)} renderNode={renderNode} />
      <aside className="studio-flow-props card" data-testid="flow-props">
        <div className="hd">
          <h3>{copy(pc, "studio.flow.properties")}</h3>
        </div>
        {selected ? renderCard(selected, selectedIndex) : <EmptyState title={copy(pc, "studio.flow.none_selected")} icon={<Iconify icon="eva:diagonal-arrow-left-down-fill" />} style={{ padding: 16 }} />}
      </aside>
    </div>
  );
}
