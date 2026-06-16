"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";

// CollapsiblePanel mirrors admin-primitives Panel styling but adds an
// expand/collapse toggle. The panel header (and its count) stays visible when
// collapsed so a queue is never silently removed from the page.
export function CollapsiblePanel({
  title,
  description,
  children,
  defaultOpen = true,
  count,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
  count?: number;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <section className="min-w-0 rounded-xl border border-[#334155] bg-[#1A1D24]">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className={`flex w-full items-center justify-between gap-3 px-4 py-3 text-left ${open ? "border-b border-[#334155]" : ""}`}
      >
        <div className="flex items-start gap-2">
          {open ? (
            <ChevronDown className="mt-0.5 h-4 w-4 shrink-0 text-[#93a4b8]" aria-hidden="true" />
          ) : (
            <ChevronRight className="mt-0.5 h-4 w-4 shrink-0 text-[#93a4b8]" aria-hidden="true" />
          )}
          <div>
            <h2 className="flex items-center gap-2 text-sm font-bold text-white">
              {title}
              {typeof count === "number" ? (
                <span className="rounded border border-[#334155] px-2 py-0.5 text-xs font-semibold text-[#c7d1dc]">{count}</span>
              ) : null}
            </h2>
            {description ? <p className="mt-1 text-sm text-[#8899AA]">{description}</p> : null}
          </div>
        </div>
        <span className="shrink-0 text-xs font-semibold text-[#14f1d9]">{open ? "Collapse" : "Expand"}</span>
      </button>
      {open ? <div className="p-4">{children}</div> : null}
    </section>
  );
}
