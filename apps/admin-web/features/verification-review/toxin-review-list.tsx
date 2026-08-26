"use client";

// The CEO/CXO Toxin review list + drawer (maintainer decision 2026-08-25).
//
// Rendered on /verify when the backend-declared `toxin_tab` control is enabled and ?toxin=1 is
// selected. Every visible business string is BACKEND-OWNED: rows render status_chip /
// context_line / outcome_label / origin_line verbatim (mapped in ./toxin-rows.ts), the drawer
// renders the detail payload's step titles/instructions and the page contract's toxin.* copy.
//
// The drawer is CLIENT-LOCAL overlay state: a row click sets React state and fetches the detail
// through a Server Action — never a navigation, never a router push (repo drawer rule).
import { useCallback, useEffect, useRef, useState, useTransition } from "react";
import { faro } from "@grafana/faro-web-sdk";

import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ToxinTask } from "@/lib/api/server";
import { fmtDateTime } from "@/lib/format";
import { loadToxinTaskDetailAction, recordToxinVerdictAction, type ToxinDetailLoad } from "./toxin-actions";
import { toxinReviewRows, type ToxinReviewRow } from "./toxin-rows";

export function ToxinReviewList({
  tasks,
  pageContract,
  returnTo,
  feedback,
}: {
  tasks: ToxinTask[];
  pageContract: AdminUiPageContract;
  /** The current /verify?toxin=1… URL, carried through the verdict action's redirect. */
  returnTo: string;
  feedback: { status?: string; code?: string };
}) {
  const rows = toxinReviewRows(tasks);
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);
  const [detail, setDetail] = useState<ToxinDetailLoad | undefined>(undefined);
  const [loading, startLoading] = useTransition();
  const text = useCallback((key: string) => copy(pageContract, key), [pageContract]);

  // TELEMETRY GUARDRAIL: tab open + verdict submit result, mirroring
  // verification-review-telemetry.tsx's Faro usage. Faro must never break the page.
  const openedReported = useRef(false);
  useEffect(() => {
    if (openedReported.current) return;
    openedReported.current = true;
    try {
      faro.api?.pushEvent("toxin_review_tab_opened", { rows: String(rows.length) });
    } catch {
      // Faro must never break the page.
    }
    // Row count at first paint is enough; re-reporting on refresh would double-count the visit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const feedbackReported = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!feedback.status) return;
    const key = `${feedback.status}:${feedback.code ?? ""}`;
    if (feedbackReported.current === key) return;
    feedbackReported.current = key;
    try {
      faro.api?.pushEvent("toxin_review_action", { status: feedback.status, code: feedback.code ?? "" });
    } catch {
      // Faro must never break the page.
    }
  }, [feedback.status, feedback.code]);

  const openRow = useCallback(
    (taskId: string) => {
      setSelectedId(taskId);
      setDetail(undefined);
      startLoading(async () => {
        const loaded = await loadToxinTaskDetailAction(taskId);
        setDetail(loaded);
      });
    },
    [startLoading],
  );
  const closeDrawer = useCallback(() => {
    setSelectedId(undefined);
    setDetail(undefined);
  }, []);

  useEffect(() => {
    if (!selectedId) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [selectedId, closeDrawer]);

  const feedbackText = feedback.status
    ? feedback.status === "success"
      ? text("toxin.feedback.done")
      : copy(pageContract, `toxin.feedback.${feedback.code ?? ""}`, "")
    : "";

  return (
    <>
      {feedback.status ? (
        <div className={feedback.status === "success" ? "small" : "alert"} style={{ marginBottom: 12 }}>
          {/* Unmapped error codes render nothing rather than leaking the raw token. */}
          {feedbackText || (feedback.status === "error" ? copy(pageContract, "feedback.failed", "") : "")}
        </div>
      ) : null}

      <div style={{ overflowX: "auto" }} tabIndex={0} role="group">
        <table data-enh="1" className="vr-table">
          <thead>
            <tr>
              <th>{text("toxin.drawer.title")}</th>
              <th>{text("toxin.drawer.reading")}</th>
              <th>{copy(pageContract, "drawer.meta.status")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={3}>
                  <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                    {text("toxin.state.empty")}
                  </div>
                </td>
              </tr>
            ) : (
              rows.map((row) => (
                <tr key={row.taskId} className="vr-row">
                  <td>
                    <button type="button" className="vr-rowlink" style={{ all: "unset", cursor: "pointer", display: "block", width: "100%" }} onClick={() => openRow(row.taskId)}>
                      <div className="vr-subj">
                        <span className="t">{row.contextLine}</span>
                        {/* The purchase date is NOT repeated here: context_line already ends with
                            it, and rendering both showed the same day twice in two formats
                            (2026-08-26 then 26-08-2026). The second line carries only what the
                            context line does not say — the retest round, when there is one. */}
                        {row.roundChip ? (
                          <span className="m">
                            <span className="chip count">{row.roundChip}</span>
                          </span>
                        ) : null}
                      </div>
                    </button>
                  </td>
                  <td className="muted" style={{ whiteSpace: "nowrap" }}>
                    <button type="button" style={{ all: "unset", cursor: "pointer", display: "block", width: "100%" }} onClick={() => openRow(row.taskId)}>
                      {row.outcomeLabel || "—"}
                    </button>
                  </td>
                  <td style={{ whiteSpace: "nowrap" }}>
                    <button type="button" style={{ all: "unset", cursor: "pointer", display: "block", width: "100%" }} onClick={() => openRow(row.taskId)}>
                      <Tag tone="warn">{row.statusChip}</Tag>
                    </button>
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      <ToxinDrawer
        // Keyed by the selected task so drawer-local state (the reject reason draft) resets when a
        // different test opens, without a setState-in-effect.
        key={selectedId ?? "closed"}
        open={Boolean(selectedId)}
        loading={loading}
        detail={detail}
        row={rows.find((row) => row.taskId === selectedId)}
        pageContract={pageContract}
        returnTo={returnTo}
        onClose={closeDrawer}
      />
    </>
  );
}

function ToxinDrawer({
  open,
  loading,
  detail,
  row,
  pageContract,
  returnTo,
  onClose,
}: {
  open: boolean;
  loading: boolean;
  detail: ToxinDetailLoad | undefined;
  row: ToxinReviewRow | undefined;
  pageContract: AdminUiPageContract;
  returnTo: string;
  onClose: () => void;
}) {
  const text = useCallback((key: string) => copy(pageContract, key), [pageContract]);
  const [reason, setReason] = useState("");
  if (!open || !row) return null;

  const loaded = detail?.ok ? detail : undefined;
  const task = loaded?.detail;
  return (
    <div className={`vr-modal-scrim${open ? " on" : ""}`} onClick={onClose}>
      <div
        className={`vr-modal${open ? " on" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={text("toxin.drawer.title")}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="vr-modal-hd">
          <div>
            {/* The headline is the backend-composed context line, never a client-assembled one. */}
            <h2>{row.contextLine}</h2>
            <div className="sb">{row.statusChip}</div>
          </div>
          <button type="button" className="x" aria-label={copy(pageContract, "drawer.close_label")} onClick={onClose}>
            &times;
          </button>
        </div>
        <div className="vr-modal-bd">
          {loading || !detail ? (
            <div className="muted small">…</div>
          ) : !detail.ok ? (
            <div className="alert">
              <b>{copy(pageContract, "feedback.failed")}</b>
            </div>
          ) : task ? (
            <>
              <div>
                <div className="bt">{text("toxin.drawer.steps")}</div>
                <ol style={{ margin: 0, paddingLeft: 18, display: "flex", flexDirection: "column", gap: 8 }}>
                  {task.steps.map((step) => {
                    const proofUrl = step.proof_ref ? (loaded?.proofUrls[step.proof_ref] ?? null) : null;
                    return (
                      <li key={step.step_no}>
                        <div style={{ display: "flex", gap: 8, alignItems: "baseline", flexWrap: "wrap" }}>
                          <b>{step.title}</b>
                          <span className="muted small">{step.instruction}</span>
                        </div>
                        <div className="muted small" style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
                          {step.completed_by ? <span>{step.completed_by}</span> : null}
                          {step.completed_at ? <span>{fmtDateTime(step.completed_at)}</span> : null}
                          {/* A resolvable proof gets a link to the signed URL; an unresolved one
                              honestly shows only who did the step and when — see
                              loadToxinTaskDetailAction's bounded resolver and its limitation note. */}
                          {proofUrl ? (
                            <a className="lk" href={proofUrl} target="_blank" rel="noreferrer">
                              {copy(pageContract, "drawer.media.open")}
                            </a>
                          ) : null}
                        </div>
                      </li>
                    );
                  })}
                </ol>
              </div>

              {task.strip_photo_ref ? (
                <div>
                  <div className="bt">{text("toxin.drawer.strip_photo")}</div>
                  {loaded?.proofUrls[task.strip_photo_ref] ? (
                    // A signed, short-lived proof URL on an external media host: next/image would
                    // proxy and cache evidence, so this stays a plain <img> (vr-image-proof
                    // precedent in verification-review-drawer.tsx).
                    {/* The media box MUST be position:relative and sized. `.vr-image-link` is
                        `position:absolute; inset:0; background:#000` and `.vr-image-proof` is
                        width/height 100% -- both are built to fill the verification drawer's
                        positioned `.vr-player` box. Used without such a parent the link escapes to
                        the nearest positioned ancestor (the modal itself) and paints the WHOLE
                        drawer black: the reviewer sees no steps, no reading, and no Accept button.
                        This box is that parent, so the evidence stays a fixed area whatever the
                        media does -- including an image that cannot be decoded. */}
                    <div style={{ position: "relative", height: 320, overflow: "hidden", borderRadius: 10 }}>
                      <a href={loaded.proofUrls[task.strip_photo_ref] ?? undefined} target="_blank" rel="noreferrer" className="vr-image-link">
                        {/* eslint-disable-next-line @next/next/no-img-element */}
                        <img
                          className="vr-image-proof"
                          src={loaded.proofUrls[task.strip_photo_ref] ?? undefined}
                          alt={text("toxin.drawer.strip_photo")}
                        />
                      </a>
                    </div>
                  ) : (
                    <div className="muted small">{copy(pageContract, "drawer.media.empty")}</div>
                  )}
                </div>
              ) : null}

              <div>
                <div className="bt">{text("toxin.drawer.reading")}</div>
                <div>{task.outcome_label || "—"}</div>
                {task.cancel_reason ? <div className="muted small">{task.cancel_reason}</div> : null}
              </div>

              {task.status === "pending_review" ? (
                <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
                  {/* Accept is ONE click; Reject requires a reason before its button enables — the
                      backend enforces the same rule (400 reject_reason_required). Both are one
                      Server Action with a derived idempotency key, redirect-feedback on tx_status/
                      tx_code, and the loaded row_version as the optimistic-concurrency fence. */}
                  <form action={recordToxinVerdictAction} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                    <input type="hidden" name="task_id" value={task.task_id} />
                    <input type="hidden" name="row_version" value={String(task.row_version)} />
                    <input type="hidden" name="return_to" value={returnTo} />
                    <div className="fld" style={{ marginBottom: 0 }}>
                      <label htmlFor="toxin-reject-reason">{text("toxin.drawer.reject_reason")}</label>
                      <textarea
                        id="toxin-reject-reason"
                        name="reason"
                        rows={2}
                        value={reason}
                        onChange={(event) => setReason(event.target.value)}
                        placeholder={text("toxin.drawer.reject_reason_hint")}
                      />
                    </div>
                    <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
                      <button type="submit" name="decision" value="accept" className="btn primary">
                        {text("toxin.action.accept")}
                      </button>
                      <button type="submit" name="decision" value="reject" className="btn" disabled={!reason.trim()} title={!reason.trim() ? text("toxin.drawer.reject_reason_hint") : undefined}>
                        {text("toxin.action.reject")}
                      </button>
                    </div>
                  </form>
                </div>
              ) : null}
            </>
          ) : null}
        </div>
      </div>
    </div>
  );
}
