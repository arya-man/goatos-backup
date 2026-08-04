"use client";

import Link from "@/components/no-prefetch-link";
import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { ShieldCheck, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { PositionListResponse, VerificationQueueItem } from "@/lib/api/server";
import { fmtDateTime, shortId } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { reassignVerificationItemAction, recordVerificationVerdictAction, reworkVerificationItemAction } from "./actions";
import { VerificationReviewActionTelemetry } from "./verification-review-telemetry";

const PATHNAME = "/actions";

function statusTone(status: VerificationQueueItem["status"]): Tone {
  if (status === "rejected") return "dng";
  if (status === "approved") return "ok";
  return "warn";
}

/**
 * Whether the signed-in principal holds a backend-declared control on this page.
 *
 * /actions serves two personas (verifier vs authority) and the backend contract decides which half
 * each one gets — see compileVerificationReviewControls. A missing control falls back to
 * `fallback` so an older cached contract degrades to the pre-verdict behaviour instead of throwing
 * the whole drawer, which is why this does not use the throwing `control()` helper.
 */
function controlEnabled(page: AdminUiPageContract, id: string, fallback: boolean): boolean {
  return page.controls.find((item) => item.id === id)?.enabled ?? fallback;
}

export function VerificationReviewDrawer({
  items,
  initialSelectedId,
  positions,
  searchParams,
  feedback,
  pageContract,
  statusLabels,
}: {
  items: VerificationQueueItem[];
  initialSelectedId?: string;
  positions: PositionListResponse | null;
  searchParams: RouteSearchParams;
  feedback: { status?: string; code?: string };
  pageContract: AdminUiPageContract;
  statusLabels: Record<string, string>;
}) {
  const initialItem = items.find((item) => item.item_id === initialSelectedId);
  const [activeId, setActiveId] = useState(initialItem?.item_id);
  const [displayedId, setDisplayedId] = useState(initialItem?.item_id);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const item = items.find((candidate) => candidate.item_id === displayedId);
  const drawerOpen = Boolean(activeId && item);
  const closeHref = hrefWithout(searchParams, ["vi_row"]);

  const syncFromUrl = useCallback((): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    const id = new URL(window.location.href).searchParams.get("vi_row") ?? undefined;
    const selected = items.find((candidate) => candidate.item_id === id);
    if (selected) {
      triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setActiveId(undefined);
      setDisplayedId(selected.item_id);
      openFrameRef.current = window.requestAnimationFrame(() => {
        setActiveId(selected.item_id);
        openFrameRef.current = null;
      });
      return;
    }
    setActiveId(undefined);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedId(undefined);
      closeTimerRef.current = null;
      triggerRef.current?.focus();
    }, 280);
  }, [items]);

  useEffect(() => {
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncFromUrl);
    window.addEventListener("popstate", syncFromUrl);
    return () => {
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncFromUrl);
      window.removeEventListener("popstate", syncFromUrl);
      if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, [syncFromUrl]);

  useEffect(() => {
    if (!drawerOpen) return;
    const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [drawerOpen]);

  const closeDrawer = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    setActiveId(undefined);
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

  useEffect(() => {
    if (!drawerOpen) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [closeDrawer, drawerOpen]);

  if (!item) return null;
  const scopedPositions: PositionListResponse | null = positions
    ? { ...positions, items: positions.items.filter((position) => position.scope_type === "center" && position.scope_id === item.park_id) }
    : null;
  const returnTo = hrefWithRow(searchParams, item.item_id);

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={copy(pageContract, "drawer.close_label")}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <VerificationReviewDrawerPanel
        item={item}
        positions={scopedPositions}
        returnTo={returnTo}
        feedback={feedback}
        open={drawerOpen}
        onClose={closeDrawer}
        closeButtonRef={closeButtonRef}
        pageContract={pageContract}
        statusLabel={statusLabels[item.status] ?? item.status}
      />
    </>
  );
}

function VerificationReviewDrawerPanel({
  item,
  positions,
  returnTo,
  feedback,
  open,
  onClose,
  closeButtonRef,
  pageContract,
  statusLabel,
}: {
  item: VerificationQueueItem;
  positions: PositionListResponse | null;
  returnTo: string;
  feedback: { status?: string; code?: string };
  open: boolean;
  onClose: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
  pageContract: AdminUiPageContract;
  statusLabel: string;
}) {
  const hasTask = Boolean(item.source.task_id);
  const isFlagged = item.status === "rejected";
  const reworkDisabled = !hasTask || !isFlagged;
  const reassignDisabled = !hasTask || !positions || positions.items.length === 0;
  const text = (key: string) => copy(pageContract, key);

  // Duty split (verifier-app-and-flow.md §Roles): the verifier records the verdict, the authority
  // acts on the source task. A principal sees only the half they hold — showing the other half
  // disabled would advertise an authority they do not have and, for the verifier-only workspace,
  // would put the authority's own actions in front of the person the separation exists to isolate.
  const mayReview = controlEnabled(pageContract, "record_verdict", false);
  const mayAct = controlEnabled(pageContract, "request_rework", true);
  // A verdict is terminal: approved/rejected items stay open for viewing but cannot be re-decided.
  const verdictSettled = item.status !== "pending";

  return (
      <aside className={`drawer${open ? " on" : ""}`} aria-label={text("drawer.aria")} aria-hidden={!open} inert={!open}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <ShieldCheck className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{text("drawer.eyebrow")}</div>
            <h2>
              {item.category} · {shortId(item.item_id)}
            </h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={text("drawer.close_label")} onClick={onClose}>
            <X className="ic" />
          </button>
        </div>

        <div className="dc">
          <VerificationReviewActionTelemetry status={feedback.status} code={feedback.code} />
          {feedback.status ? (
            <div className={feedback.status === "success" ? "alert ok" : "alert warn"} style={{ marginBottom: 12 }}>
              <b>{feedback.status === "success" ? text("feedback.done") : text("feedback.failed")}</b>&nbsp;
              {feedback.code ?? ""}
            </div>
          ) : null}

          {mayAct ? (
            <div className="note" style={{ marginBottom: 12 }}>
              {text("drawer.note")}
            </div>
          ) : null}

          <div className="metagrid">
            <Meta label={text("drawer.meta.status")}>
              <Tag tone={statusTone(item.status)}>{statusLabel}</Tag>
            </Meta>
            <Meta label={text("drawer.meta.reason")}>{item.verdict_reason || "—"}</Meta>
            {/* The raiser's own words about this work item (for a movement: why the animals are
                moving). Sits right after status so the reviewer reads the operator's reason
                before the provenance fields. */}
            <Meta label={text("drawer.meta.subject_note")}>{item.subject_note || "—"}</Meta>
            <Meta label={text("drawer.meta.verified_by")}>
              {item.verified_by_name || (item.verified_by ? shortId(item.verified_by) : "—")}
            </Meta>
            <Meta label={text("drawer.meta.verified_at")}>{item.verified_at ? fmtDateTime(item.verified_at) : "—"}</Meta>
            <Meta label={text("drawer.meta.captured")}>{fmtDateTime(item.captured_at)}</Meta>
            <Meta label={text("drawer.meta.operator")}>{item.operator_name || (item.operator_id ? shortId(item.operator_id) : "—")}</Meta>
            <Meta label={text("drawer.meta.shed")}>{item.shed_label || (item.shed_id ? shortId(item.shed_id) : "—")}</Meta>
            <Meta label={text("drawer.meta.park")}>{item.park_label || (item.park_id ? shortId(item.park_id) : "—")}</Meta>
            <Meta label={text("drawer.meta.source_module")}>
              {item.vertical} / {item.module}
            </Meta>
            <Meta label={text("drawer.meta.source_task")}>{item.source.task_id ? shortId(item.source.task_id) : "—"}</Meta>
            <Meta label={text("drawer.meta.source_submission")}>{item.source.submission_id ? shortId(item.source.submission_id) : "—"}</Meta>
          </div>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{text("drawer.media.title")}</h3>
            </div>
            <div className="bd">
              {item.media.length === 0 ? (
                <div className="muted small">{text("drawer.media.empty")}</div>
              ) : (
                <div style={{ display: "grid", gap: 10 }}>
                  {item.media.map((media) => (
                    <div key={media.proof_id} style={{ display: "grid", gap: 8 }}>
                      {media.label ? <b>{media.label}</b> : null}
                      {media.answer ? <div className="small muted">{media.answer}</div> : null}
                      {media.mime_type?.startsWith("video/") ? (
                        <video controls preload="metadata" style={{ width: "100%", borderRadius: 8, background: "#000" }}>
                          <source src={media.download_url} type={media.mime_type} />
                        </video>
                      ) : null}
                      <a href={media.download_url} target="_blank" rel="noreferrer" className="btn sm">
                        {text("drawer.media.open")} · {shortId(media.proof_id)}
                      </a>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </section>

          {mayReview ? (
            <section className="card" style={{ marginTop: 14 }}>
              <div className="hd">
                <h3>{text("verdict.title")}</h3>
              </div>
              <div className="bd">
                <form action={recordVerificationVerdictAction} style={{ display: "grid", gap: 8 }}>
                  <input type="hidden" name="item_id" value={item.item_id} />
                  {/* Guards THIS item's row: a verdict recorded elsewhere since render makes the
                      submit 409 instead of silently overwriting the other reviewer's decision. */}
                  <input type="hidden" name="row_version" value={item.row_version} />
                  <input type="hidden" name="return_to" value={returnTo} />
                  <div className="note">{text("verdict.note")}</div>
                  <label className="fld" style={{ marginBottom: 0 }}>
                    <span>{text("verdict.reason_label")}</span>
                    {/* Deliberately not `required`: the same field is mandatory for Reject and
                        unused for Approve, so the rule lives in the server action and the backend
                        (422), not in a per-button HTML attribute. */}
                    <textarea
                      name="reason"
                      rows={2}
                      placeholder={text("verdict.reason_placeholder")}
                      disabled={verdictSettled}
                    />
                  </label>
                  <div className="small muted">{text("verdict.reason_required")}</div>
                  {verdictSettled ? <div className="note">{text("verdict.disabled_not_pending")}</div> : null}
                  <div className="df" style={{ padding: 0, border: 0, background: "transparent" }}>
                    <button
                      type="submit"
                      name="decision"
                      value="approved"
                      className="btn p"
                      disabled={verdictSettled}
                      aria-disabled={verdictSettled}
                      title={verdictSettled ? text("verdict.disabled_not_pending") : undefined}
                    >
                      {text("verdict.approve")}
                    </button>
                    <button
                      type="submit"
                      name="decision"
                      value="rejected"
                      className="btn"
                      disabled={verdictSettled}
                      aria-disabled={verdictSettled}
                      title={verdictSettled ? text("verdict.disabled_not_pending") : undefined}
                    >
                      {text("verdict.reject")}
                    </button>
                  </div>
                </form>
              </div>
            </section>
          ) : null}

          {mayAct ? (
          <>
          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{text("rework.title")}</h3>
            </div>
            <div className="bd">
              <form action={reworkVerificationItemAction} style={{ display: "grid", gap: 8 }}>
                <input type="hidden" name="task_id" value={item.source.task_id ?? ""} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("rework.reason_label")}</span>
                  <textarea name="reason" rows={2} placeholder={text("rework.reason_placeholder")} disabled={reworkDisabled} required />
                </label>
                {!isFlagged ? <div className="note">{text("rework.disabled_not_rejected")}</div> : null}
                {!hasTask ? <div className="note">{text("rework.disabled_no_task")}</div> : null}
                <button type="submit" className="btn p" disabled={reworkDisabled} aria-disabled={reworkDisabled} title={reworkDisabled ? (!hasTask ? text("rework.disabled_no_task") : text("rework.disabled_not_rejected")) : undefined}>
                  {text("rework.submit")}
                </button>
              </form>
            </div>
          </section>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{text("reassign.title")}</h3>
            </div>
            <div className="bd">
              <form action={reassignVerificationItemAction} style={{ display: "grid", gap: 8 }}>
                <input type="hidden" name="task_id" value={item.source.task_id ?? ""} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("reassign.assignee_label")}</span>
                  <select name="assigned_to" disabled={reassignDisabled} required defaultValue="">
                    <option value="" disabled>
                      {text("reassign.assignee_placeholder")}
                    </option>
                    {(positions?.items ?? []).map((position) => (
                      <option key={position.position_id} value={position.workforce_member_id}>
                        {position.position_code} · {position.position_tier} · {shortId(position.workforce_member_id)}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("reassign.reason_label")}</span>
                  <textarea name="reason" rows={2} placeholder={text("reassign.reason_placeholder")} disabled={reassignDisabled} required />
                </label>
                {reassignDisabled ? (
                  <div className="note">{!hasTask ? text("reassign.disabled_no_task") : text("reassign.disabled_no_roster")}</div>
                ) : null}
                <button
                  type="submit"
                  className="btn"
                  disabled={reassignDisabled}
                  aria-disabled={reassignDisabled}
                  title={reassignDisabled ? (!hasTask ? text("reassign.disabled_no_task") : text("reassign.disabled_no_roster")) : undefined}
                >
                  {text("reassign.submit")}
                </button>
              </form>
            </div>
          </section>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{text("penalty.title")}</h3>
            </div>
            <div className="bd">
              <div style={{ display: "grid", gap: 8 }}>
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("penalty.reason_label")}</span>
                  <textarea name="penalty_note" rows={2} placeholder={text("penalty.reason_placeholder")} disabled />
                </label>
                <div className="note">{text("penalty.disabled")}</div>
                <button type="button" className="btn" disabled aria-disabled title={text("penalty.disabled")}>
                  {text("penalty.submit")}
                </button>
              </div>
            </div>
          </section>
          </>
          ) : null}
        </div>

        <div className="df">
          {/* Audit Log is an authority surface. The verifier-only workspace has no Audit Log page
              contract, so linking her there would dead-end on a route that throws. */}
          {mayAct ? (
            <Link href={`/operations/audit?module=${encodeURIComponent(item.module)}`} className="btn" scroll={false}>
              {text("action.open_audit_log")}
            </Link>
          ) : null}
          <button type="button" className="btn" onClick={onClose}>
            {text("action.close")}
          </button>
        </div>
      </aside>
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

function hrefWithRow(params: RouteSearchParams, itemId: string): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === "vi_row" || key === "va_status" || key === "va_code") continue;
    if (Array.isArray(value)) {
      for (const item of value) if (item) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  next.set("vi_row", itemId);
  return `${PATHNAME}?${next.toString()}`;
}
