"use client";

import { useEffect, useRef, useState } from "react";
import { X } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { signalTypeLabel } from "./herd-signals-kpis";
import { fmtBatteryVoltage, fmtCount, fmtMotionDelta, fmtRssi, fmtStaleness, fmtTagTemp } from "./format";
import type { HerdSignalsItem, HerdSignalsTimelineBucket } from "./types";

const RANGE_OPTIONS: Array<{ key: string; label: string; seconds: number; bucketSeconds: number }> = [
  { key: "1h", label: "1h", seconds: 3600, bucketSeconds: 300 },
  { key: "6h", label: "6h", seconds: 21600, bucketSeconds: 900 },
  { key: "24h", label: "24h", seconds: 86400, bucketSeconds: 3600 },
];

function rowId(row: HerdSignalsItem): string {
  return row.tag_id;
}

async function readTimeline(tagId: string, fromIso: string, toIso: string, bucketSeconds: number) {
  try {
    const response = await fetch(
      `/api/herd-signals/tags/${encodeURIComponent(tagId)}/timeline?from=${encodeURIComponent(fromIso)}&to=${encodeURIComponent(toIso)}&bucket_seconds=${bucketSeconds}`,
      { headers: { Accept: "application/json" }, cache: "no-store" },
    );
    const payload = (await response.json().catch(() => ({}))) as { buckets?: HerdSignalsTimelineBucket[]; error?: string };
    if (!response.ok || !payload.buckets) {
      return { ok: false as const, error: payload.error ?? `timeline_read_${response.status}` };
    }
    return { ok: true as const, buckets: payload.buckets };
  } catch {
    return { ok: false as const, error: "timeline_unreachable" };
  }
}

function MiniChart({ buckets }: { buckets: HerdSignalsTimelineBucket[] }) {
  if (buckets.length === 0) {
    return <div className="hs-chart-empty">{"No history for this range."}</div>;
  }
  const max = Math.max(1, ...buckets.map((bucket) => bucket.motion_delta ?? 0));
  return (
    <div className="hs-minichart" role="img" aria-label="Motion-count delta over time, packet gaps shown as red bands">
      {buckets.map((bucket) => {
        const heightPct = bucket.is_gap ? 100 : Math.max(4, Math.round(((bucket.motion_delta ?? 0) / max) * 100));
        return (
          <span
            key={bucket.bucket_start}
            className={bucket.is_gap ? "hs-bar hs-bar-gap" : "hs-bar"}
            style={{ height: `${heightPct}%` }}
            title={
              bucket.is_gap
                ? `Gap at ${new Date(bucket.bucket_start).toLocaleTimeString("en-IN", { timeZone: "Asia/Kolkata" })}`
                : `${new Date(bucket.bucket_start).toLocaleTimeString("en-IN", { timeZone: "Asia/Kolkata" })}: delta ${bucket.motion_delta ?? 0}`
            }
          />
        );
      })}
    </div>
  );
}

export function HerdSignalsDrawer({
  items,
  initialSelectedTagId,
  closeHref,
  pageContract,
  nowMs,
}: {
  items: HerdSignalsItem[];
  initialSelectedTagId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
  nowMs: number;
}) {
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items,
    itemId: rowId,
    selectionKey: "hs_tag",
    initialSelectedId: initialSelectedTagId,
    closeHref,
  });
  const [rangeKey, setRangeKey] = useState("1h");
  const [buckets, setBuckets] = useState<HerdSignalsTimelineBucket[]>([]);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const requestId = useRef(0);

  useEffect(() => {
    if (!displayedItem) return;
    const range = RANGE_OPTIONS.find((option) => option.key === rangeKey) ?? RANGE_OPTIONS[0];
    const myRequest = ++requestId.current;
    setLoading(true);
    setLoadError(null);
    const to = new Date(nowMs).toISOString();
    const from = new Date(nowMs - range.seconds * 1000).toISOString();
    readTimeline(displayedItem.tag_id, from, to, range.bucketSeconds).then((result) => {
      if (requestId.current !== myRequest) return;
      setLoading(false);
      if (!result.ok) {
        setLoadError(result.error);
        setBuckets([]);
        return;
      }
      setBuckets(result.buckets);
    });
  }, [displayedItem, rangeKey, nowMs]);

  if (!displayedItem) return null;
  const item = displayedItem;

  return (
    <div className={`drawer-overlay${drawerOpen ? " open" : ""}`} role="presentation">
      <div className="drawer-scrim" onClick={closeDrawer} aria-hidden="true" />
      <aside className="drawer hs-drawer" role="dialog" aria-modal="true" aria-label={`Herd signal detail for ${item.display_id ?? item.tag_mac}`}>
        <header className="drawer-hd">
          <div>
            <div className="hs-drawer-eyebrow">{item.operational_location_display ?? item.shed_name ?? "—"}</div>
            <h2>{item.display_id ?? item.goat_id ?? "Unmapped tag"}</h2>
          </div>
          <button type="button" ref={closeButtonRef} className="drawer-close" onClick={closeDrawer} aria-label="Close">
            <X size={18} />
          </button>
        </header>
        <div className="drawer-bd hs-drawer-bd">
          <section className="hs-drawer-chart" aria-label="Movement history">
            <div className="hs-drawer-chart-hd">
              <div className="hs-range" role="group" aria-label="History range">
                {RANGE_OPTIONS.map((option) => (
                  <button key={option.key} type="button" className={rangeKey === option.key ? "on" : undefined} onClick={() => setRangeKey(option.key)}>
                    {option.label}
                  </button>
                ))}
              </div>
              <Link href={`/herd-signals/history?tag=${encodeURIComponent(item.tag_id)}&range=${rangeKey}`} className="btn hs-expand-btn">
                Expand
              </Link>
            </div>
            {loading ? <div className="hs-chart-loading">Loading…</div> : loadError ? <div className="hs-chart-error">Could not load history: {loadError}</div> : <MiniChart buckets={buckets} />}
          </section>

          <section className="hs-drawer-readings">
            <ReadingRow label="Signal strength (RSSI)" value={fmtRssi(item.rssi_dbm)} kind="direct" />
            <ReadingRow label="Signal state" value={item.signal_state} kind="derived" />
            <ReadingRow label="Battery voltage" value={fmtBatteryVoltage(item.battery_mv)} kind="direct" />
            <ReadingRow label="Estimated battery life" value={item.battery_life_estimate ?? "—"} kind="inferred" />
            <ReadingRow label="Motion count" value={fmtCount(item.motion_count)} kind="direct" />
            <ReadingRow label="15-minute motion-count delta" value={fmtMotionDelta(item.motion_delta)} kind="derived" />
            <ReadingRow label="1-hour motion-count delta" value={fmtMotionDelta(item.motion_delta_1h)} kind="derived" />
            <ReadingRow label="Movement trend" value={item.movement_state} kind="derived" />
            <ReadingRow label="Pattern (vs baseline)" value={item.pattern_state} kind="correlated" />
            <ReadingRow label="Tag temperature" value={fmtTagTemp(item.tag_temperature_c)} kind="direct" note="This is the temperature of the tag, not the animal." />
            <ReadingRow label="Sensor state" value={item.sensor_state} kind="derived" />
            <ReadingRow label="Last seen" value={fmtStaleness(item.last_seen_at, nowMs)} kind="direct" />
            <ReadingRow label="Mapping" value={item.mapping_state} kind="direct" />
          </section>
        </div>
      </aside>
    </div>
  );
}

function ReadingRow({ label, value, kind, note }: { label: string; value: string; kind: "direct" | "derived" | "correlated" | "inferred"; note?: string }) {
  return (
    <div className="hs-reading-row">
      <span className="hs-reading-label">{label}</span>
      <span className="hs-reading-value">{value}</span>
      <span className={`hs-badge hs-sig-${kind}`}>{signalTypeLabel(kind)}</span>
      {note ? <span className="hs-reading-note">{note}</span> : null}
    </div>
  );
}
