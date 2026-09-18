"use client";

import { useState } from "react";
import { Search, X } from "lucide-react";

import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { AdminUiTableContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { BandCell } from "./feed-weight-band-table";

/** One animal that exited the register inside the period (sold, died or other), already resolved by the card. */
export type FeedWeightBandExitItem = {
  key: string;
  park: string;
  pen: string;
  tag: string;
  gender: string;
  /** The register's stored exit reason / lifecycle text, shown beside the bucket pill. */
  reason: string;
  /** The backend's exit bucket: sold / died / other. */
  bucket: string;
  /** The bucket as display copy. */
  bucketLabel: string;
  /** True when the animal has a weigh inside the period (the last-weigh fields are set). */
  weighed: boolean;
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
  /** The exits' bucket and weighed split, shown under the period line. */
  detail: string;
  close: string;
  period: string;
  search: string;
  searchAria: string;
  never: string;
  noFeed: string;
  noGender: string;
  empty: string;
  animals: string;
  animal: string;
  /** Heading of the last group: the animals with no weigh in the period. */
  notWeighed: string;
};

const kg = (value: number) => value.toLocaleString("en-IN", { minimumFractionDigits: 1, maximumFractionDigits: 1 });

function scopeId(scope: FeedWeightBandExitScope): string {
  return scope.id;
}

/**
 * The exited panel: the app's local drawer (the Audit Log record drawer's shape and the same
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
      [item.tag, item.pen, item.gender, item.reason, item.bucketLabel, item.feedGiven, item.park, item.lastBandLabel].join(" ").toLowerCase().includes(q),
  );
  // Weighed animals grouped by pen in served order (newest exit first inside a pen); the
  // animals with no weigh in the period come last, in one group, with their placement as pen.
  const groups: { pen: string; park: string; weighed: boolean; items: FeedWeightBandExitItem[] }[] = [];
  for (const item of items.filter((x) => x.weighed)) {
    const last = groups[groups.length - 1];
    if (last && last.pen === item.pen && last.park === item.park) last.items.push(item);
    else groups.push({ pen: item.pen, park: item.park, weighed: true, items: [item] });
  }
  const notWeighed = items.filter((x) => !x.weighed);
  if (notWeighed.length > 0) groups.push({ pen: "", park: "", weighed: false, items: notWeighed });
  const columns = contract.columns.filter((column) => column.visible && column.key !== "park" && column.key !== "pen");
  const columnsWithPen = contract.columns.filter((column) => column.visible && column.key !== "park");
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
              {displayedItem.title} · {displayedItem.items.length.toLocaleString("en-IN")}{" "}
              {displayedItem.items.length === 1 ? labels.animal : labels.animals}
            </h2>
            <div className="sb">
              {labels.period} {periodLabel}
              {labels.detail ? <span className="muted"> · {labels.detail}</span> : null}
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
              <div key={group.weighed ? `${group.park}|${group.pen}` : "not-weighed"} className="wt-feedband-exitgroup">
                <h3 className="h">
                  {group.weighed ? group.pen : <span className="muted">{labels.notWeighed}</span>}
                  <span className="muted small"> · {group.weighed ? `${group.park} · ` : ""}{group.items.length.toLocaleString("en-IN")}</span>
                </h3>
                <div className="tablewrap" tabIndex={0} role="group" aria-label={group.weighed ? group.pen : labels.notWeighed}>
                  <table className="tbl wt-feedband">
                    <thead>
                      <tr>
                        {(group.weighed ? columns : columnsWithPen).map((column) => (
                          <th key={column.key} scope="col" className={column.key === "last_kg" ? "num" : undefined}>
                            {column.label}
                          </th>
                        ))}
                      </tr>
                    </thead>
                    <tbody>
                      {group.items.map((item) => (
                        <tr key={item.key}>
                          {(group.weighed ? columns : columnsWithPen).map((column) => (
                            <td key={column.key} className={column.key === "last_kg" ? "num" : undefined}>
                              {column.key === "tag" ? (
                                <b>{item.tag}</b>
                              ) : column.key === "pen" ? (
                                item.pen ? <span title={item.pen}>{item.pen}</span> : blank
                              ) : column.key === "gender" ? (
                                item.gender || <span className="muted">{labels.noGender}</span>
                              ) : column.key === "reason" ? (
                                <span className="wt-feedband-reason">
                                  <Tag tone={item.bucket === "sold" ? "info" : item.bucket === "died" ? "dng" : "warn"}>{item.bucketLabel}</Tag>
                                  {item.reason && item.reason !== item.bucket ? <span className="muted small"> {item.reason}</span> : null}
                                </span>
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
