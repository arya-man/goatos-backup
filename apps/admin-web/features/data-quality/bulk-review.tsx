"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { CheckSquare, ExternalLink, Info, X } from "lucide-react";
import { bulkResolveConflictsAction } from "./actions";
import {
  bulkDecisions,
  decisionAppliesToSelection,
  reviewGroupLabel,
  type BulkDecisionType,
  type ReviewGroup,
} from "./review-groups";

export type BulkReviewRow = {
  conflictId: string;
  rowVersion: number;
  conflictTypeLabel: string;
  identifierLabel: string;
  reviewGroup: ReviewGroup;
  stateLabel: string;
  goatCount: number;
  createdAtLabel: string;
  detailHref: string;
};

const MAX_BULK = 200;

export function ConflictBulkReview({ rows, returnTo }: { rows: BulkReviewRow[]; returnTo: string }) {
  const [selected, setSelected] = useState<Record<string, number>>({});
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState<BulkDecisionType | null>(null);

  const selectedIds = useMemo(() => Object.keys(selected), [selected]);
  const selectedCount = selectedIds.length;
  const visibleIds = useMemo(() => rows.map((row) => row.conflictId), [rows]);
  const allVisibleSelected = visibleIds.length > 0 && visibleIds.every((id) => id in selected);

  const selectedGroups = useMemo<ReviewGroup[]>(() => {
    const byId = new Map(rows.map((row) => [row.conflictId, row.reviewGroup]));
    return Array.from(new Set(selectedIds.map((id) => byId.get(id)).filter((g): g is ReviewGroup => g !== undefined)));
  }, [rows, selectedIds]);

  const groupSummary = useMemo(() => {
    const counts = new Map<ReviewGroup, number>();
    const byId = new Map(rows.map((row) => [row.conflictId, row.reviewGroup]));
    for (const id of selectedIds) {
      const group = byId.get(id);
      if (group === undefined) continue;
      counts.set(group, (counts.get(group) ?? 0) + 1);
    }
    return Array.from(counts.entries());
  }, [rows, selectedIds]);

  const reasonValid = reason.trim().length >= 1 && reason.trim().length <= 2000;
  const overLimit = selectedCount > MAX_BULK;

  function toggleRow(row: BulkReviewRow, checked: boolean) {
    setPending(null);
    setSelected((current) => {
      const next = { ...current };
      if (checked) next[row.conflictId] = row.rowVersion;
      else delete next[row.conflictId];
      return next;
    });
  }

  function toggleAllVisible(checked: boolean) {
    setPending(null);
    setSelected((current) => {
      const next = { ...current };
      for (const row of rows) {
        if (checked) next[row.conflictId] = row.rowVersion;
        else delete next[row.conflictId];
      }
      return next;
    });
  }

  function clearSelection() {
    setSelected({});
    setPending(null);
  }

  const pendingDecision = bulkDecisions.find((decision) => decision.type === pending) ?? null;

  return (
    <div className="space-y-3">
      {selectedCount > 0 ? (
        <div className="sticky top-2 z-10 rounded-lg border border-[#14f1d9]/60 bg-[#0f1b1d] p-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2 text-sm font-semibold text-white">
              <CheckSquare className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
              {selectedCount} selected
              {overLimit ? <span className="text-[#fca5a5]"> · over the {MAX_BULK} limit, deselect some</span> : null}
            </div>
            <button
              type="button"
              onClick={clearSelection}
              className="inline-flex h-10 items-center gap-2 rounded-md border border-[#334155] px-3 text-xs font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white"
            >
              <X className="h-3.5 w-3.5" aria-hidden="true" />
              Clear
            </button>
          </div>

          {groupSummary.length > 1 ? (
            <p className="mt-2 flex items-start gap-2 text-xs text-[#facc15]">
              <Info className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              Mixed review groups: {groupSummary.map(([g, n]) => `${reviewGroupLabel(g)} ${n}`).join(", ")}. Some decisions may be unavailable for this
              selection.
            </p>
          ) : null}

          <div className="mt-3">
            <label className="text-xs uppercase text-[#93a4b8]">Reason</label>
            <textarea
              value={reason}
              onChange={(event) => {
                setReason(event.target.value);
                setPending(null);
              }}
              rows={2}
              maxLength={2000}
              placeholder="Reviewed legacy rows, passport value, and source evidence."
              className="mt-1 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 py-2 text-sm text-white outline-none focus:border-[#14f1d9]"
            />
          </div>

          <div className="mt-3 flex flex-wrap gap-2">
            {bulkDecisions.map((decision) => {
              const applicable = decisionAppliesToSelection(decision, selectedGroups);
              const enabled = applicable && reasonValid && !overLimit;
              const title = !applicable
                ? "Not valid for at least one selected review group"
                : !reasonValid
                  ? "Enter a reason first"
                  : overLimit
                    ? `Deselect down to ${MAX_BULK} or fewer`
                    : decision.intent;
              return (
                <button
                  key={decision.type}
                  type="button"
                  disabled={!enabled}
                  title={title}
                  onClick={() => setPending(decision.type)}
                  className={
                    decision.danger
                      ? "h-10 rounded-md border border-[#a16207] px-3 text-sm font-semibold text-[#facc15] hover:bg-[#1f1a0a] disabled:cursor-not-allowed disabled:opacity-40"
                      : "h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#0fd3bd] disabled:cursor-not-allowed disabled:opacity-40"
                  }
                >
                  {decision.label}
                </button>
              );
            })}
          </div>

          {pendingDecision ? (
            <form action={bulkResolveConflictsAction} className="mt-3 rounded-md border border-[#334155] bg-[#0f1115] p-3">
              <input type="hidden" name="decision_type" value={pendingDecision.type} />
              <input type="hidden" name="reason" value={reason} />
              <input type="hidden" name="return_to" value={returnTo} />
              {selectedIds.map((id) => (
                <input key={id} type="hidden" name="selected_conflict" value={`${id}:${selected[id]}`} />
              ))}
              <div className="text-sm font-bold text-white">Confirm: {pendingDecision.label}</div>
              <p className="mt-1 text-sm leading-6 text-[#c7d1dc]">{pendingDecision.intent}</p>
              <p className="mt-2 text-xs text-[#93a4b8]">
                Applies to {selectedCount} conflict{selectedCount === 1 ? "" : "s"}
                {groupSummary.length > 0 ? ` (${groupSummary.map(([g, n]) => `${reviewGroupLabel(g)} ${n}`).join(", ")})` : ""}. The whole batch is
                applied together; if any conflict has changed since this page loaded, none are changed and you can reload and retry.
              </p>
              <div className="mt-3 flex gap-2">
                <button type="submit" className="inline-flex h-10 items-center gap-2 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#0fd3bd]">
                  <CheckSquare className="h-4 w-4" aria-hidden="true" />
                  Apply to {selectedCount} conflict{selectedCount === 1 ? "" : "s"}
                </button>
                <button
                  type="button"
                  onClick={() => setPending(null)}
                  className="inline-flex h-10 items-center gap-2 rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white"
                >
                  <X className="h-4 w-4" aria-hidden="true" />
                  Cancel
                </button>
              </div>
            </form>
          ) : null}
        </div>
      ) : null}

      <div className="overflow-x-auto rounded-md border border-[#293241]">
        <table className="w-full border-collapse text-sm">
          <thead>
            <tr className="border-b border-[#293241] bg-[#10141b] text-left text-xs uppercase text-[#93a4b8]">
              <th className="w-12 p-0">
                <label className="flex h-10 w-12 cursor-pointer items-center justify-center">
                  <input
                    type="checkbox"
                    aria-label="Select all visible"
                    checked={allVisibleSelected}
                    onChange={(event) => toggleAllVisible(event.target.checked)}
                    className="h-4 w-4 accent-[#14f1d9]"
                  />
                </label>
              </th>
              <th className="p-2">Type</th>
              <th className="p-2">Review group</th>
              <th className="p-2">Identifier</th>
              <th className="p-2">Goats</th>
              <th className="p-2">State</th>
              <th className="p-2">Created</th>
              <th className="p-2"></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const isSelected = row.conflictId in selected;
              return (
                <tr
                  key={row.conflictId}
                  className={`border-b border-[#1c2530] ${isSelected ? "bg-[#0f1b1d]" : "bg-[#0d1117] hover:bg-[#111923]"}`}
                >
                  <td className="p-0 align-top">
                    <label className="flex h-10 w-12 cursor-pointer items-center justify-center">
                      <input
                        type="checkbox"
                        aria-label={`Select ${row.conflictTypeLabel}`}
                        checked={isSelected}
                        onChange={(event) => toggleRow(row, event.target.checked)}
                        className="h-4 w-4 accent-[#14f1d9]"
                      />
                    </label>
                  </td>
                  <td className="p-2 align-top font-semibold text-white">{row.conflictTypeLabel}</td>
                  <td className="p-2 align-top">
                    <span className="rounded border border-[#334155] px-2 py-0.5 text-xs text-[#c7d1dc]">{reviewGroupLabel(row.reviewGroup)}</span>
                  </td>
                  <td className="p-2 align-top text-[#93a4b8]">{row.identifierLabel}</td>
                  <td className="p-2 align-top text-[#c7d1dc]">{row.goatCount}</td>
                  <td className="p-2 align-top text-[#c7d1dc]">{row.stateLabel}</td>
                  <td className="p-2 align-top text-[#93a4b8]">{row.createdAtLabel}</td>
                  <td className="p-2 align-top text-right">
                    <Link href={row.detailHref} className="inline-flex h-10 items-center gap-1.5 px-2 text-xs font-semibold text-[#14f1d9] hover:underline">
                      Review
                      <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
                    </Link>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="text-xs text-[#64748b]">
        Current page selection only. Full per-goat evidence stays in each row workbench.
      </p>
    </div>
  );
}
