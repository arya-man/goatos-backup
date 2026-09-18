"use client";

import { useState } from "react";
import { Search, X } from "lucide-react";

import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { BandCell } from "./feed-weight-band-table";

/** One animal sold or dead inside the period, already resolved by the server component. */
export type FeedWeightBandExitItem = {
  key: string;
  park: string;
  pen: string;
  tag: string;
  gender: string;
  reason: string;
  sold: boolean;
  /** ISO business dates; formatted here. */
  exitedAt: string;
  lastWeighedAt: string;
  lastBand: string;
  lastBandLabel: string;
  lastWeightKg: number | null;
  feedType: string;
  feedTypeLabel: string;
  feedGiven: string;
};

/**
 * One thing the drawer can be opened on: every exit in the period (the chip and the stat tile),
 * or the exits behind one pen × bracket row. The id is what the `#fb_exit=` hash carries.
 */
export type FeedWeightBandExitScope = {
  id: string;
  title: string;
  /** Grouped by pen when the scope spans pens; a single pen scope has one group. */
  items: FeedWeightBandExitItem[];
};

export type FeedWeightBandExitsDrawerLabels = {
  aria: string;
  eyebrow: string;
  close: string;
  period: string;
  search: string;
  searchAria: string;
  never: string;
  noFeed: string;
  noGender: string;
  empty: string;
  animals: string;
};

const kg = (value: number) => value.toLocaleString("en-IN", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

function scopeId(scope: FeedWeightBandExitScope): string {
  return scope.id;
}

/**
 * The sold / dead panel: the app's local drawer (the Audit Log record drawer's shape and the same
 * `useLocalOverlaySelection` lifecycle), opened from a `#fb_exit=<scope>` hash so an ordinary
 * click never re-runs the route, Back/Escape/scrim/X close it, and the table's filters are exactly
 * where the reader left them. The list is grouped by pen and searchable inside the panel.
 */
export function FeedWeightBandExitsDrawer({
  scopes,
  initialSelectedId,
  closeHref,
  contract,
  periodLabel,
  labels,
}: {
  scopes: FeedWeightBandExitScope[];
  initialSelectedId?: string;
  closeHref: string;
  contract: AdminUiTableContract;
  periodLabel: string;
  labels: FeedWeightBandExitsDrawerLabels;
}) {
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: scopes,
    itemId: scopeId,
    selectionKey: "fb_exit",
    initialSelectedId,
    closeHref,
  });
  const [query, setQuery] = useState("");

  if (!displayedItem) return null;
  const q = query.trim().toLowerCase();
  const items = displayedItem.items.filter(
    (item) =>
      q === "" ||
      [item.tag, item.pen, item.gender, item.reason, item.feedGiven, item.park, item.lastBandLabel].join(" ").toLowerCase().includes(q),
  );
  // Grouped by pen in served order (newest exit first inside a pen); a pen never weighed in the
  // period groups under the "never weighed" label.
  const groups: { pen: string; park: string; items: FeedWeightBandExitItem[] }[] = [];
  for (const item of items) {
    const last = groups[groups.length - 1];
    if (last && last.pen === item.pen && last.park === item.park) last.items.push(item);
    else groups.push({ pen: item.pen, park: item.park, items: [item] });
  }
  const columns = contract.columns.filter((column) => column.visible && column.key !== "park" && column.key !== "pen");
  const blank = <span className="muted">—</span>;

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={labels.close}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside className={`drawer wt-feedband-drawer${drawerOpen ? " on" : ""}`} aria-label={labels.aria} aria-hidden={!drawerOpen} inert={!drawerOpen}>
        <div className="dh">
          <div>
            <div className="mt">{labels.eyebrow}</div>
            <h2>
              {displayedItem.title} · {displayedItem.items.length.toLocaleString("en-IN")} {labels.animals}
            </h2>
            <div className="sb">
              {labels.period} {periodLabel}
            </div>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={labels.close} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>
        <div className="dc">
          <label className="wt-feedband-search" style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            <Search className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
            <input
              type="search"
              className="tsize"
              value={query}
              placeholder={labels.search}
              aria-label={labels.searchAria}
              onChange={(event) => setQuery(event.target.value)}
            />
          </label>
          {groups.length === 0 ? (
            <div className="empty">
              <span className="muted small">{labels.empty}</span>
            </div>
          ) : (
            groups.map((group) => (
              <div key={`${group.park}|${group.pen}`} className="wt-feedband-exitgroup">
                <h3 className="h">
                  {group.pen ? group.pen : <span className="muted">{labels.never}</span>}
                  <span className="muted small"> · {group.park} · {group.items.length.toLocaleString("en-IN")}</span>
                </h3>
                <div className="tablewrap" tabIndex={0} role="group" aria-label={group.pen || labels.never}>
                  <table className="tbl wt-feedband">
                    <thead>
                      <tr>
                        {columns.map((column) => (
                          <th key={column.key} scope="col" className={column.key === "last_kg" ? "num" : undefined}>
                            {column.label}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {group.items.map((item) => (
                        <tr key={item.key}>
                          {columns.map((column) => (
                            <td key={column.key} className={column.key === "last_kg" ? "num" : undefined}>
                              {column.key === "tag" ? (
                                <b>{item.tag}</b>
                              ) : column.key === "gender" ? (
                                item.gender || <span className="muted">{labels.noGender}</span>
                              ) : column.key === "reason" ? (
                                <Tag tone="warn">{item.reason}</Tag>
                              ) : column.key === "exited_at" ? (
                                fmtDate(item.exitedAt)
                              ) : column.key === "last_weighed" ? (
                                item.lastWeighedAt ? fmtDate(item.lastWeighedAt) : blank
                              ) : column.key === "last_band" ? (
                                item.lastBand ? <BandCell band={item.lastBand} label={item.lastBandLabel} /> : blank
                              ) : column.key === "last_kg" ? (
                                item.lastWeightKg != null ? kg(item.lastWeightKg) : blank
                              ) : column.key === "feed_type" ? (
                                item.feedType ? <Tag tone={item.feedType === "experiment" ? "pur" : "mut"}>{item.feedTypeLabel}</Tag> : blank
                              ) : item.feedGiven ? (
                                <span className="wt-feedband-feed">{item.feedGiven}</span>
                              ) : (
                                <span className="muted">{labels.noFeed}</span>
                              )}
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            ))
          )}
        </div>
        <div className="df">
          <button type="button" className="btn" onClick={closeDrawer}>
            {labels.close}
          </button>
        </div>
      </aside>
    </>
  );
}
