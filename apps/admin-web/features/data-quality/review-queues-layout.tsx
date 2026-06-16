"use client";

import { useState, type ReactNode } from "react";
import { PanelRightClose, PanelRightOpen } from "lucide-react";

// ReviewQueuesLayout lays out Identity Conflicts and Match Candidates side by
// side and lets the reviewer collapse Match Candidates horizontally: when
// collapsed, Identity Conflicts takes the full width and Match Candidates
// shrinks to a slim expandable bar. Nothing is ever fully removed from the page.
export function ReviewQueuesLayout({
  conflicts,
  candidatesBody,
  candidateCount,
  defaultMatchOpen,
}: {
  conflicts: ReactNode;
  candidatesBody: ReactNode;
  candidateCount: number;
  defaultMatchOpen: boolean;
}) {
  const [open, setOpen] = useState(defaultMatchOpen);
  return (
    <div className={open ? "grid gap-5 xl:grid-cols-[1.05fr_0.95fr]" : "grid gap-5"}>
      <div className="min-w-0">{conflicts}</div>
      {open ? (
        <section className="min-w-0 rounded-xl border border-[#334155] bg-[#1A1D24]">
          <div className="flex items-start justify-between gap-3 border-b border-[#334155] px-4 py-3">
            <div>
              <h2 className="flex items-center gap-2 text-sm font-bold text-white">
                Match Candidates
                <span className="rounded border border-[#334155] px-2 py-0.5 text-xs font-semibold text-[#c7d1dc]">{candidateCount}</span>
              </h2>
              <p className="mt-1 text-sm text-[#8899AA]">
                Records that might be the SAME goat (a possible duplicate). Confirm a match or reject it. Empty means none are suspected right now.
              </p>
            </div>
            <button
              type="button"
              onClick={() => setOpen(false)}
              aria-expanded={true}
              aria-label="Collapse Match Candidates so Identity Conflicts fills the width"
              className="inline-flex h-10 shrink-0 items-center gap-1.5 rounded-md border border-[#334155] px-3 text-xs font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white"
            >
              <PanelRightClose className="h-4 w-4" aria-hidden="true" />
              Collapse
            </button>
          </div>
          <div className="p-4">{candidatesBody}</div>
        </section>
      ) : (
        <button
          type="button"
          onClick={() => setOpen(true)}
          aria-expanded={false}
          aria-label="Expand Match Candidates"
          className="flex w-full items-center justify-between gap-3 rounded-xl border border-[#334155] bg-[#1A1D24] px-4 py-3 text-left hover:border-[#14f1d9]/70"
        >
          <span className="flex flex-wrap items-center gap-2 text-sm font-bold text-white">
            <PanelRightOpen className="h-4 w-4 shrink-0 text-[#93a4b8]" aria-hidden="true" />
            Match Candidates
            <span className="rounded border border-[#334155] px-2 py-0.5 text-xs font-semibold text-[#c7d1dc]">{candidateCount}</span>
            <span className="text-sm font-normal text-[#8899AA]">possible duplicates to confirm or reject</span>
          </span>
          <span className="shrink-0 text-xs font-semibold text-[#14f1d9]">Expand</span>
        </button>
      )}
    </div>
  );
}
