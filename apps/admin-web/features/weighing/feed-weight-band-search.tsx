"use client";

import { Search } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

/**
 * The feed-by-weight-band table's text search, the vendor filter bar's shape: one search input
 * that applies on Enter (and on blur, so a tap away on a phone commits it), writes ONE URL
 * parameter beside the page's other filters, and clears the table's offset so a narrowed result
 * never opens on an empty later page. The draft re-syncs when the server answers with a different
 * applied value (Back/Forward, Clear), adjusted during render so the input never flashes stale.
 */
export function FeedWeightBandSearch({
  param,
  offsetParam,
  applied,
  placeholder,
  ariaLabel,
}: {
  param: string;
  offsetParam: string;
  applied: string;
  placeholder: string;
  ariaLabel: string;
}) {
  const router = useRouter();
  const params = useSearchParams();
  const [, startTransition] = useTransition();
  const [draft, setDraft] = useState(applied);
  const [synced, setSynced] = useState(applied);
  if (synced !== applied) {
    setSynced(applied);
    setDraft(applied);
  }

  function apply(): void {
    const value = draft.trim();
    if (value === applied) return;
    const next = new URLSearchParams(params.toString());
    if (value) next.set(param, value);
    else next.delete(param);
    next.delete(offsetParam);
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `${window.location.pathname}?${qs}` : window.location.pathname, { scroll: false });
    });
  }

  return (
    <label className="wt-feedband-search" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
      <Search className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
      <input
        type="search"
        className="tsize"
        value={draft}
        placeholder={placeholder}
        aria-label={ariaLabel}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={apply}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            apply();
          }
        }}
      />
    </label>
  );
}
