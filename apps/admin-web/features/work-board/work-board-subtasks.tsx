"use client";

import Link from "@/components/no-prefetch-link";
import { useEffect, useState, useTransition } from "react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow, WorkBoardSubtask, WorkBoardSubtaskPage } from "@/lib/api/work-board-server";
import { loadSubtasksAction } from "./actions";
import { initials, lanes } from "./work-board-model";

// The issue view's subtask list, in the mock's shape: worst first, ten per page, each row with
// its name, subtitle, step chips, owner and status; a click unfolds the steps. The page is read
// through a Server Action only while the card is open; nothing here recomputes state, every
// state and label is the backend's.
const PAGE = 10;

const STEP_GLYPH: Record<string, string> = { done: "✓", in_review: "◔", in_progress: "▸", rework: "↻", needs_attention: "!", todo: "", locked: "" };

function stepLabel(pageContract: AdminUiPageContract, state: string): string {
  return copy(pageContract, `step.${state}`, state);
}

function SubtaskRow({ pageContract, sub }: { pageContract: AdminUiPageContract; sub: WorkBoardSubtask }) {
  const [open, setOpen] = useState(false);
  const laneOpt = lanes(pageContract).find((lane) => lane.key === sub.lane);
  const status = sub.needs_attention ? copy(pageContract, "tile.attention") : laneOpt?.label ?? sub.lane;
  const tone = sub.needs_attention ? "t-warn" : sub.lane === "done" ? "t-ok" : sub.lane === "in_review" || sub.lane === "in_progress" ? "t-info" : "t-mut";
  return (
    <>
      <button type="button" className={`it${sub.needs_attention ? " hot" : ""}${open ? " open" : ""}`} aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <span className="car" aria-hidden="true">▶</span>
        <span className="nm">
          <b>{sub.name}</b>
          {sub.subtitle ? <span className="s">{sub.subtitle}</span> : null}
          <div className="stp">
            {sub.steps.map((step, i) => (
              <span key={`${step.name}-${i}`} className={step.state} title={stepLabel(pageContract, step.state)}>
                {step.name}
                {STEP_GLYPH[step.state] ? ` ${STEP_GLYPH[step.state]}` : ""}
              </span>
            ))}
          </div>
        </span>
        <span className="who">
          {sub.owner?.name ? (
            <>
              <span className="av">{initials(sub.owner.name)}</span>
              <span className="n" title={sub.owner.name}>{sub.owner.name}</span>
            </>
          ) : null}
        </span>
        <span className={`tag ${tone}`}>{status}</span>
        {sub.href ? (
          <Link href={sub.href} className="lk" onClick={(e) => e.stopPropagation()}>
            {copy(pageContract, "drawer.subtasks.open")} →
          </Link>
        ) : (
          <span />
        )}
      </button>
      {open ? (
        <div className="steps">
          {sub.steps.map((step, i) => (
            <div className="step" key={`${step.name}-${i}`}>
              <span className="n">
                <span className="arrow">{i + 1}</span>
                <b>{step.name}</b>
                {step.detail ? <span className="s">{step.detail}</span> : null}
              </span>
              <span className={`tag ${step.state === "done" ? "t-ok" : step.state === "in_review" || step.state === "in_progress" ? "t-info" : step.state === "rework" ? "t-warn" : step.state === "needs_attention" ? "t-dng" : "t-mut"}`}>
                {stepLabel(pageContract, step.state)}
              </span>
            </div>
          ))}
        </div>
      ) : null}
    </>
  );
}

export function WorkBoardSubtasks({ pageContract, row, selectedOwner }: { pageContract: AdminUiPageContract; row: WorkBoardRow; selectedOwner?: string }) {
  const [pages, setPages] = useState<WorkBoardSubtaskPage[]>([]);
  const [error, setError] = useState(false);
  const [pending, startTransition] = useTransition();
  const load = (cursor: string | undefined, reset: boolean) => {
    startTransition(async () => {
      const result = await loadSubtasksAction({ rowKey: row.row_key, park: row.park_id, businessDate: row.business_date, owner: selectedOwner, cursor });
      if (!result.ok) {
        setError(true);
        return;
      }
      setError(false);
      setPages((prev) => (reset ? [result.page] : [...prev, result.page]));
    });
  };
  useEffect(() => {
    // The parent keys this component by row_key, so a new card is a fresh instance: the first
    // page is read once per open card and state never has to be reset here.
    load(undefined, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const current = pages[pages.length - 1];
  const pageNo = pages.length;
  const total = current?.total ?? 0;
  const first = current ? (pageNo - 1) * PAGE + 1 : 0;
  const last = current ? (pageNo - 1) * PAGE + current.subtasks.length : 0;
  const npages = Math.max(1, Math.ceil(total / PAGE));
  if (error) return <div className="note">{copy(pageContract, "drawer.subtasks.error")}</div>;
  if (!current) return <div className="note">{copy(pageContract, "drawer.subtasks.loading")}</div>;
  if (current.subtasks.length === 0) return <div className="note">{copy(pageContract, "drawer.subtasks.empty")}</div>;
  return (
    <div className="items" style={{ opacity: pending ? 0.7 : 1 }}>
      {current.subtasks.map((sub) => (
        <SubtaskRow key={sub.key} pageContract={pageContract} sub={sub} />
      ))}
      <div className="pager">
        <span>
          {copy(pageContract, "drawer.subtasks.showing")} <b>{first}–{last}</b> {copy(pageContract, "drawer.subtasks.of")} <b>{total}</b> {copy(pageContract, "card.total")} · {copy(pageContract, "drawer.subtasks.worst_first")}
        </span>
        {npages > 1 ? (
          <span className="pgnav">
            <button type="button" className="more" disabled={pageNo <= 1 || pending} onClick={() => setPages((prev) => prev.slice(0, -1))}>
              ‹ {copy(pageContract, "action.previous")}
            </button>
            <span>
              {copy(pageContract, "drawer.subtasks.page")} <b>{pageNo}</b> {copy(pageContract, "drawer.subtasks.of")} <b>{npages}</b>
            </span>
            <button type="button" className="more" disabled={!current.next_cursor || pending} onClick={() => load(current.next_cursor, false)}>
              {copy(pageContract, "action.next")} ›
            </button>
          </span>
        ) : null}
      </div>
    </div>
  );
}
