import Link from "@/components/no-prefetch-link";
import { ShieldCheck, X } from "lucide-react";
import type { ReactNode } from "react";

import { Tag, type Tone } from "@/components/ui-primitives";
import type { PositionListResponse, VerificationQueueItem } from "@/lib/api/server";
import { fmtDateTime, shortId } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { VERIFICATION_REVIEW_COPY as COPY } from "./copy";
import { reassignVerificationItemAction, reworkVerificationItemAction } from "./actions";
import { VerificationReviewActionTelemetry } from "./verification-review-telemetry";

const PATHNAME = "/verification";

function statusTone(status: VerificationQueueItem["status"]): Tone {
  if (status === "rejected") return "dng";
  if (status === "approved") return "ok";
  return "warn";
}

export function VerificationReviewDrawer({
  item,
  positions,
  searchParams,
  returnTo,
  feedback,
}: {
  item: VerificationQueueItem;
  positions: PositionListResponse | null;
  searchParams: RouteSearchParams;
  returnTo: string;
  feedback: { status?: string; code?: string };
}) {
  const closeHref = hrefWithout(searchParams, ["vi_row"]);
  const hasTask = Boolean(item.source.task_id);
  const isFlagged = item.status === "rejected";
  const reworkDisabled = !hasTask;
  const reassignDisabled = !hasTask || !positions || positions.items.length === 0;

  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={COPY.drawer.closeLabel} scroll={false} style={{ opacity: 1, pointerEvents: "auto" }} />
      <aside className="drawer on" aria-label={COPY.drawer.aria}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <ShieldCheck className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{COPY.drawer.eyebrow}</div>
            <h2>
              {item.category} · {shortId(item.item_id)}
            </h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={COPY.drawer.closeLabel} scroll={false}>
            <X className="ic" />
          </Link>
        </div>

        <div className="dc">
          <VerificationReviewActionTelemetry status={feedback.status} code={feedback.code} />
          {feedback.status ? (
            <div className={feedback.status === "success" ? "alert ok" : "alert warn"} style={{ marginBottom: 12 }}>
              <b>{feedback.status === "success" ? "Done" : "Action failed"}</b>&nbsp;
              {feedback.code ?? ""}
            </div>
          ) : null}

          <div className="note" style={{ marginBottom: 12 }}>
            {COPY.drawer.note}
          </div>

          <div className="metagrid">
            <Meta label={COPY.drawer.metaStatus}>
              <Tag tone={statusTone(item.status)}>{item.status}</Tag>
            </Meta>
            <Meta label={COPY.drawer.metaReason}>{item.verdict_reason || "—"}</Meta>
            <Meta label={COPY.drawer.metaVerifiedBy}>{item.verified_by ? shortId(item.verified_by) : "—"}</Meta>
            <Meta label={COPY.drawer.metaVerifiedAt}>{item.verified_at ? fmtDateTime(item.verified_at) : "—"}</Meta>
            <Meta label={COPY.drawer.metaCaptured}>{fmtDateTime(item.captured_at)}</Meta>
            <Meta label={COPY.drawer.metaOperator}>{item.operator_id ? shortId(item.operator_id) : "—"}</Meta>
            <Meta label={COPY.drawer.metaShed}>{item.shed_id ? shortId(item.shed_id) : "—"}</Meta>
            <Meta label={COPY.drawer.metaPark}>{item.park_id ? shortId(item.park_id) : "—"}</Meta>
            <Meta label={COPY.drawer.metaSourceModule}>
              {item.vertical} / {item.module}
            </Meta>
            <Meta label={COPY.drawer.metaSourceTask}>{item.source.task_id ? shortId(item.source.task_id) : "—"}</Meta>
            <Meta label={COPY.drawer.metaSourceSubmission}>{item.source.submission_id ? shortId(item.source.submission_id) : "—"}</Meta>
          </div>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{COPY.drawer.mediaTitle}</h3>
            </div>
            <div className="bd">
              {item.media.length === 0 ? (
                <div className="muted small">{COPY.drawer.mediaEmpty}</div>
              ) : (
                <div style={{ display: "grid", gap: 10 }}>
                  {item.media.map((media) =>
                    media.mime_type?.startsWith("video/") ? (
                      <video key={media.proof_id} controls preload="metadata" style={{ width: "100%", borderRadius: 8, background: "#000" }}>
                        <source src={media.download_url} type={media.mime_type} />
                      </video>
                    ) : (
                      <a key={media.proof_id} href={media.download_url} target="_blank" rel="noreferrer" className="btn sm">
                        {COPY.drawer.mediaOpen} · {shortId(media.proof_id)}
                      </a>
                    ),
                  )}
                </div>
              )}
            </div>
          </section>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{COPY.rework.title}</h3>
            </div>
            <div className="bd">
              <form action={reworkVerificationItemAction} style={{ display: "grid", gap: 8 }}>
                <input type="hidden" name="task_id" value={item.source.task_id ?? ""} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{COPY.rework.reasonLabel}</span>
                  <textarea name="reason" rows={2} placeholder={COPY.rework.reasonPlaceholder} disabled={reworkDisabled} required />
                </label>
                {!isFlagged ? <div className="note">{COPY.rework.disabledNotFlagged}</div> : null}
                {reworkDisabled ? <div className="note">{COPY.rework.disabledNoTask}</div> : null}
                <button type="submit" className="btn p" disabled={reworkDisabled} aria-disabled={reworkDisabled} title={reworkDisabled ? COPY.rework.disabledNoTask : undefined}>
                  {COPY.rework.submit}
                </button>
              </form>
            </div>
          </section>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{COPY.reassign.title}</h3>
            </div>
            <div className="bd">
              <form action={reassignVerificationItemAction} style={{ display: "grid", gap: 8 }}>
                <input type="hidden" name="task_id" value={item.source.task_id ?? ""} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{COPY.reassign.assigneeLabel}</span>
                  <select name="assigned_to" disabled={reassignDisabled} required defaultValue="">
                    <option value="" disabled>
                      {COPY.reassign.assigneePlaceholder}
                    </option>
                    {(positions?.items ?? []).map((position) => (
                      <option key={position.position_id} value={position.workforce_member_id}>
                        {position.position_code} · {position.position_tier} · {shortId(position.workforce_member_id)}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{COPY.reassign.reasonLabel}</span>
                  <textarea name="reason" rows={2} placeholder={COPY.reassign.reasonPlaceholder} disabled={reassignDisabled} required />
                </label>
                {reassignDisabled ? (
                  <div className="note">{!hasTask ? COPY.reassign.disabledNoTask : COPY.reassign.disabledNoRoster}</div>
                ) : null}
                <button
                  type="submit"
                  className="btn"
                  disabled={reassignDisabled}
                  aria-disabled={reassignDisabled}
                  title={reassignDisabled ? (!hasTask ? COPY.reassign.disabledNoTask : COPY.reassign.disabledNoRoster) : undefined}
                >
                  {COPY.reassign.submit}
                </button>
              </form>
            </div>
          </section>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{COPY.penalty.title}</h3>
            </div>
            <div className="bd">
              <div style={{ display: "grid", gap: 8 }}>
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{COPY.penalty.reasonLabel}</span>
                  <textarea name="penalty_note" rows={2} placeholder={COPY.penalty.reasonPlaceholder} disabled />
                </label>
                <div className="note">{COPY.penalty.disabled}</div>
                <button type="button" className="btn" disabled aria-disabled title={COPY.penalty.disabled}>
                  {COPY.penalty.submit}
                </button>
              </div>
            </div>
          </section>
        </div>

        <div className="df">
          <Link href={`/operations/audit?domain=preventive_care&module=${encodeURIComponent(item.module)}`} className="btn" scroll={false}>
            {COPY.action.openAuditLog}
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            {COPY.action.close}
          </Link>
        </div>
      </aside>
    </>
  );
}

function Meta({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <span className="muted small">{label}</span>
      <b style={{ display: "block", marginTop: 3, overflowWrap: "anywhere" }}>{children}</b>
    </div>
  );
}

function hrefWithout(params: RouteSearchParams, exclude: string[]): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (exclude.includes(key)) continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}
