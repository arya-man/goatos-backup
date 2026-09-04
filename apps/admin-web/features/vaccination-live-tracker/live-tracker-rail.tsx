import { Activity, Video } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { copy, optionLabel, optionTone, optionTitle, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  LiveTrackerActivity,
  LiveTrackerActivityItem,
  LiveTrackerAttentionRow,
  LiveTrackerVerification,
} from "@/lib/api/vaccination-live-tracker";
import { fmtClock } from "./format";

type ActivityAnimalRow = {
  key: string;
  newestAt: string;
  actorName: string;
  shedLabel: string;
  vaccineLabel: string;
  scannedIdentifier: string;
  detailCode: string;
  kinds: Set<string>;
};

function animalKey(item: LiveTrackerActivityItem): string {
  return item.goat_id || item.scanned_identifier || item.event_id;
}

function activityAnimalRows(items: LiveTrackerActivityItem[]): ActivityAnimalRow[] {
  const rows = new Map<string, ActivityAnimalRow>();
  for (const item of items) {
    const key = animalKey(item);
    const existing = rows.get(key);
    if (existing) {
      existing.kinds.add(item.kind);
      if (item.occurred_at > existing.newestAt) {
        existing.newestAt = item.occurred_at;
        existing.actorName = item.actor_name || existing.actorName;
        existing.shedLabel = item.shed_label || existing.shedLabel;
        existing.vaccineLabel = item.vaccine_label || existing.vaccineLabel;
        existing.scannedIdentifier = item.scanned_identifier || existing.scannedIdentifier;
        existing.detailCode = item.detail_code || existing.detailCode;
      } else {
        existing.actorName ||= item.actor_name;
        existing.shedLabel ||= item.shed_label;
        existing.vaccineLabel ||= item.vaccine_label;
        existing.scannedIdentifier ||= item.scanned_identifier;
        existing.detailCode ||= item.detail_code;
      }
      continue;
    }
    rows.set(key, {
      key,
      newestAt: item.occurred_at,
      actorName: item.actor_name,
      shedLabel: item.shed_label,
      vaccineLabel: item.vaccine_label,
      scannedIdentifier: item.scanned_identifier,
      detailCode: item.detail_code,
      kinds: new Set([item.kind]),
    });
  }
  return [...rows.values()].sort((a, b) => b.newestAt.localeCompare(a.newestAt));
}

function uniqueScanRate(items: LiveTrackerActivityItem[]): number | null {
  const scans = items.filter((item) => item.kind === "scan_capture" && item.goat_id);
  if (scans.length < 2) return null;
  const newest = new Date(scans.reduce((max, item) => (item.occurred_at > max ? item.occurred_at : max), scans[0].occurred_at));
  const oldest = new Date(scans.reduce((min, item) => (item.occurred_at < min ? item.occurred_at : min), scans[0].occurred_at));
  const minutes = (newest.getTime() - oldest.getTime()) / 60000;
  if (!(minutes > 0)) return null;
  const unique = new Set(scans.map((item) => item.goat_id)).size;
  return Math.round((unique / minutes) * 10) / 10;
}

function animalActivityLabel(row: ActivityAnimalRow, pageContract: AdminUiPageContract): string {
  const labels = [];
  if (row.kinds.has("scan_capture")) labels.push(optionLabel(pageContract, "live_activity_kind", "scan_capture"));
  if (row.kinds.has("proof_video")) labels.push(optionLabel(pageContract, "live_activity_kind", "proof_video"));
  if (row.kinds.has("administration")) labels.push(optionLabel(pageContract, "live_activity_kind", "administration"));
  if (row.kinds.has("obligation_closed")) labels.push(optionLabel(pageContract, "live_activity_kind", "obligation_closed"));
  if (row.kinds.has("scan_duplicate")) labels.push(optionLabel(pageContract, "live_activity_kind", "scan_duplicate"));
  if (row.kinds.has("scan_unknown")) labels.push(optionLabel(pageContract, "live_activity_kind", "scan_unknown"));
  return labels.join(" + ");
}

// Live activity. Built from the canonical event tables (proof uploads, scan captures, scan attempts,
// completions, obligation status events) — NOT from the audit log, which carries no scan and no
// proof event and would have produced a feed that silently omitted the two things this page exists
// to show.
function ActivityCard({
  activity,
  scanCaptureTotal,
  proofVideoTotal,
  pageContract,
}: {
  activity: LiveTrackerActivity;
  scanCaptureTotal: number;
  proofVideoTotal: number;
  pageContract: AdminUiPageContract;
}) {
  const placeholder = copy(pageContract, "label.placeholder");
  const animalRows = activityAnimalRows(activity.items);
  const scanRate = uniqueScanRate(activity.items);
  return (
    <section id="lt-activity" className="card lt-card">
      <div className="hd">
        <Activity className="ic" style={{ color: "var(--danger)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.activity.title")}</h3>
        <span className="tag t-live">
          <i />
          {scanRate == null
            ? copy(pageContract, "live.feed_rate_unavailable")
            : `${scanRate} ${copy(pageContract, "live.scan_rate_suffix")}`}
        </span>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">{copy(pageContract, "live.newest_first")}</span>
      </div>
      {activity.items.length > 0 ? (
        <div className="lt-feedsummary">
          <span><b>{scanCaptureTotal}</b> {copy(pageContract, "live.scanned_label")}</span>
          <span><b>{proofVideoTotal}</b> {copy(pageContract, "live.proofed_label")}</span>
          <span><b>{activity.items.length}</b> {copy(pageContract, "live.events_label")}</span>
          {activity.observed_per_min == null ? null : <span>{activity.observed_per_min}{copy(pageContract, "live.feed_rate_suffix")} {copy(pageContract, "live.events_label")}</span>}
        </div>
      ) : null}
      <div
        className="lt-feed"
        aria-live="polite"
        aria-label={copy(pageContract, "live.feed_aria")}
        tabIndex={0}
        role="log"
      >
        {/* The feed is capped like every other list on this page, and every other one declares it.
            next_cursor is non-null exactly when the server had more events than it returned, so the
            note fires on the same condition the (unused) paging cursor does. */}
        {activity.next_cursor != null ? (
          <div className="note lt-truncnote" role="status">
            {copy(pageContract, "section.activity.truncated_note")}
          </div>
        ) : null}
        {activity.items.length === 0 ? (
          <div className="lt-empty" style={{ padding: "14px 15px" }}>
            <div style={{ minWidth: 0, flex: 1 }}>
              <b style={{ fontSize: 13.5 }}>{copy(pageContract, "section.activity.empty_title")}</b>
              <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
                {copy(pageContract, "section.activity.empty_body")}
              </span>
            </div>
          </div>
        ) : (
          animalRows.map((row) => {
            const label = animalActivityLabel(row, pageContract) || placeholder;
            const meta = [row.shedLabel, row.vaccineLabel, row.scannedIdentifier, row.detailCode]
              .filter(Boolean)
              .join(" · ");
            return (
              <div key={row.key} className="lt-frow">
                <span className={`lt-fdot f-${row.kinds.has("scan_capture") ? "info" : optionTone(pageContract, "live_activity_kind", [...row.kinds][0]) || "mut"}`} aria-hidden="true" />
                <div className="lt-ftx">
                  <b>{row.actorName || placeholder}</b> · {label}
                  <div className="lt-fmeta">{meta || placeholder}</div>
                </div>
                <span className="lt-ftime">{fmtClock(row.newestAt)}</span>
              </div>
            );
          })
        )}
      </div>
    </section>
  );
}

// Attention. Each row is derived from the same rollups the tables render, so the count chip equals
// the number of rows by construction rather than by a constant that never updated.
//
// Three of the mock's attention affordances have no record behind them at all — a nudge dispatch, an
// escalation deadline, and a "finishes at current pace" projection all require fields that are not
// modelled. They stay in place, mock-shaped, disabled and carrying the reason.
function AttentionCard({
  attention,
  total,
  truncated,
  pageContract,
}: {
  attention: LiveTrackerAttentionRow[];
  total: number;
  truncated: boolean;
  pageContract: AdminUiPageContract;
}) {
  return (
    <section id="lt-attention" className="card lt-card">
      <div className="hd">
        <Activity className="ic" style={{ color: "var(--warn)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.attention.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="tag t-warn">{total}</span>
      </div>
      <div className="bd" style={{ display: "flex", flexDirection: "column", gap: 9 }}>
        {truncated ? (
          <div className="note lt-truncnote" role="status">
            <b>
              {attention.length}/{total}
            </b>{" "}
            {copy(pageContract, "section.attention.truncated_note")}
          </div>
        ) : null}
        {attention.length === 0 ? (
          <p className="muted small" style={{ margin: 0 }}>
            {copy(pageContract, "section.attention.empty")}
          </p>
        ) : (
          attention.map((row, index) => (
            <div key={`${row.kind}|${row.shed_id}|${row.operator_id}|${index}`} className="note">
              <b>{row.subject_label}</b> · {optionLabel(pageContract, "live_attention_kind", row.kind)}
              <div className="muted small" style={{ marginTop: 3 }}>
                {row.metric_count}
                {row.total_count > 0 ? `/${row.total_count}` : null}
                {/* Never a bare integer. A reader seeing "0/8 · 137 · 15:32" cannot tell whether
                    137 is minutes, animals or scans — and the backend no longer emits a policy
                    threshold here, so this figure is always a measured idle gap. */}
                {row.elapsed_minutes > 0
                  ? ` · ${row.elapsed_minutes} ${copy(pageContract, "section.attention.elapsed_suffix")}`
                  : null}
                {row.since_at ? ` · ${fmtClock(row.since_at)}` : null}
                {" · "}
                {optionTitle(pageContract, "live_attention_kind", row.kind)}
              </div>
              {/* These three chips have no record behind them. Their labels used to be bare factual
                  assertions about a dispatched nudge, a scheduled escalation and a projected finish
                  time, with the reason reachable only through a hover title on a non-focusable span:
                  invisible on touch and to assistive tech. A director was told the intervention had
                  already happened, which suppresses the very action this card exists to prompt. The
                  labels now name the missing capability and the reasons render as visible text, the
                  same treatment the combo card's truncated branch already uses. */}
              <div className="chipset" style={{ marginTop: 6, padding: 0 }}>
                <span className="chip" aria-disabled="true">
                  {copy(pageContract, "section.attention.nudge_label")}
                </span>
                <span className="chip" aria-disabled="true">
                  {copy(pageContract, "section.attention.escalate_label")}
                </span>
                <span className="chip" aria-disabled="true">
                  {copy(pageContract, "section.attention.pace_label")}
                </span>
              </div>
              <div className="muted small lt-truncnote">
                {copy(pageContract, "section.attention.nudge")}{" "}
                {copy(pageContract, "section.attention.escalation")}{" "}
                {copy(pageContract, "section.attention.pace")}
              </div>
            </div>
          ))
        )}
      </div>
    </section>
  );
}

function VerificationCard({
  verification,
  verifyHref,
  pageContract,
}: {
  verification: LiveTrackerVerification;
  verifyHref: string;
  pageContract: AdminUiPageContract;
}) {
  const shedsSuffix = (count: number) =>
    copy(pageContract, count === 1 ? "section.verification.sheds_suffix_one" : "section.verification.sheds_suffix");
  return (
    <section id="lt-verification" className="card lt-card">
      <div className="hd">
        <Video className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.verification.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">{copy(pageContract, "section.verification.badge")}</span>
      </div>
      <div className="bd" style={{ display: "flex", flexDirection: "column", gap: 7, fontSize: 12.5 }}>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
          <span>{copy(pageContract, "section.verification.awaiting")}</span>
          <b>
            {verification.awaiting_review_sheds} {shedsSuffix(verification.awaiting_review_sheds)}
          </b>
        </div>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
          <span>{copy(pageContract, "section.verification.verified")}</span>
          <b style={{ color: "var(--ok)" }}>
            {verification.verified_today_sheds} {shedsSuffix(verification.verified_today_sheds)}
          </b>
        </div>
        <div style={{ display: "flex", justifyContent: "space-between", gap: 10 }}>
          <span>{copy(pageContract, "section.verification.rework")}</span>
          <b style={{ color: "var(--warn)" }}>{verification.rework_requested}</b>
        </div>
        <div>
          {/* The counts above are park-scoped, and /verify reads parseScope. Linking bare "/verify"
              landed the reader on a company-scoped queue whose totals contradicted the card they
              just clicked through from. */}
          <Link href={verifyHref} className="btn sm" style={{ marginTop: 5 }}>
            {copy(pageContract, "action.open_verify")}
          </Link>
        </div>
      </div>
    </section>
  );
}

export function LiveTrackerRail({
  activity,
  scanCaptureTotal,
  proofVideoTotal,
  attention,
  attentionTotal,
  attentionTruncated,
  verification,
  verifyHref,
  pageContract,
}: {
  activity: LiveTrackerActivity;
  scanCaptureTotal: number;
  proofVideoTotal: number;
  attention: LiveTrackerAttentionRow[];
  attentionTotal: number;
  attentionTruncated: boolean;
  verification: LiveTrackerVerification;
  verifyHref: string;
  pageContract: AdminUiPageContract;
}) {
  return (
    <div className="lt-stack">
      <ActivityCard
        activity={activity}
        scanCaptureTotal={scanCaptureTotal}
        proofVideoTotal={proofVideoTotal}
        pageContract={pageContract}
      />
      <AttentionCard
        attention={attention}
        total={attentionTotal}
        truncated={attentionTruncated}
        pageContract={pageContract}
      />
      <VerificationCard verification={verification} verifyHref={verifyHref} pageContract={pageContract} />
    </div>
  );
}
