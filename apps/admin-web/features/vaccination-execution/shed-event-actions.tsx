"use client";

import { useActionState, useState, useTransition } from "react";
import { useFormStatus } from "react-dom";
import {
  acceptCompletionAction,
  rejectCompletionAction,
  submitVaccinationProof,
  type ActionResult,
} from "@/lib/api/vaccination-actions";
import { AlertCircle, Upload } from "lucide-react";

interface ShedEventActionsProps {
  obligationId?: string | null;
  sopTaskId?: string | null;
  completionId?: string | null;
}

/**
 * Interactive action block for shed-event drawer.
 *
 * Manages:
 * 1. Proof upload (file input + submit) — enabled when sopTaskId && obligationId are both non-null
 * 2. Accept / Reject buttons — enabled when completionId is non-null
 *
 * All controls render disabled with exact reasons when their required ids are null, and surface a
 * visible error band when a server action fails (never a silent failure).
 */
export function ShedEventActions({ obligationId, sopTaskId, completionId }: ShedEventActionsProps) {
  const proofUploadEnabled = !!(sopTaskId && obligationId);
  const acceptRejectEnabled = !!completionId;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      {/* Proof upload section */}
      <ProofUploadForm enabled={proofUploadEnabled} sopTaskId={sopTaskId} obligationId={obligationId} />

      {/* Accept / Reject buttons section */}
      <div style={{ display: "flex", gap: 8, alignItems: "flex-start" }}>
        <AcceptButton enabled={acceptRejectEnabled} completionId={completionId} />
        <RejectButton enabled={acceptRejectEnabled} completionId={completionId} />
      </div>
    </div>
  );
}

/** Visible error band for a failed server action (operator-facing, not console-only). */
function ActionError({ message }: { message: string }) {
  return (
    <div
      role="alert"
      className="small"
      style={{ color: "var(--danger)", lineHeight: 1.45, display: "flex", gap: 6, alignItems: "flex-start" }}
    >
      <AlertCircle className="ic" style={{ width: 13, flexShrink: 0, marginTop: 2 }} aria-hidden="true" />
      <span>{message}</span>
    </div>
  );
}

/** Shared disabled-reason note. */
function DisabledReason({ reason }: { reason: string }) {
  return (
    <div className="muted small" style={{ lineHeight: 1.45, display: "flex", gap: 6, alignItems: "flex-start" }}>
      <AlertCircle className="ic" style={{ width: 12, flexShrink: 0, opacity: 0.75, marginTop: 1 }} aria-hidden="true" />
      <span>{reason}</span>
    </div>
  );
}

interface ProofUploadFormProps {
  enabled: boolean;
  sopTaskId?: string | null;
  obligationId?: string | null;
}

/**
 * File upload form bound to submitVaccinationProof via useActionState, so a failed upload renders the
 * exact error inline. Disabled state displays the exact reason based on which id is missing.
 */
function ProofUploadForm({ enabled, sopTaskId, obligationId }: ProofUploadFormProps) {
  const [state, formAction] = useActionState<ActionResult | null, FormData>(submitVaccinationProof, null);
  const disabledReason = !enabled
    ? "No SOP task on this shed-drive rollup yet (sopTaskId null) — proof is uploaded per-goat in the operator SOP task once the drive is assigned/advanced."
    : undefined;

  return (
    <div className="fld" aria-disabled={!enabled}>
      <label>Upload vaccination proof</label>
      <form action={formAction} style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        {/* Hidden inputs to pass IDs to server action */}
        {obligationId && <input type="hidden" name="obligationId" value={obligationId} />}
        {sopTaskId && <input type="hidden" name="sopTaskId" value={sopTaskId} />}

        {/* File input */}
        <div
          className="videobox"
          style={{
            display: "flex",
            alignItems: "center",
            gap: 8,
            padding: "12px 14px",
            border: "1px solid var(--line2)",
            borderRadius: 6,
            opacity: enabled ? 1 : 0.6,
            pointerEvents: enabled ? "auto" : "none",
          }}
        >
          <Upload className="ic" style={{ width: 18, opacity: 0.75, flexShrink: 0 }} aria-hidden="true" />
          <input
            type="file"
            name="file"
            accept="video/*,image/*"
            disabled={!enabled}
            required
            style={{
              flex: 1,
              border: "none",
              background: "transparent",
              fontSize: 14,
              cursor: enabled ? "pointer" : "default",
            }}
            title={enabled ? "Select a video or image file" : "Proof upload disabled"}
          />
        </div>

        {/* Submit button */}
        <ProofUploadButton enabled={enabled} />
      </form>

      {/* Disabled reason */}
      {disabledReason && <DisabledReason reason={disabledReason} />}

      {/* Submission error */}
      {state && !state.ok && <ActionError message={state.error} />}
    </div>
  );
}

function ProofUploadButton({ enabled }: { enabled: boolean }) {
  const { pending } = useFormStatus();

  return (
    <button
      type="submit"
      disabled={!enabled || pending}
      className="btn"
      style={{
        opacity: enabled ? 1 : 0.6,
        cursor: enabled && !pending ? "pointer" : "default",
      }}
      aria-disabled={!enabled}
    >
      {pending ? "Uploading proof…" : "Submit proof"}
    </button>
  );
}

interface CompletionButtonProps {
  enabled: boolean;
  completionId?: string | null;
}

function AcceptButton({ enabled, completionId }: CompletionButtonProps) {
  const [isPending, startTransition] = useTransition();
  const [error, setError] = useState<string | null>(null);
  const disabledReason = !enabled
    ? "No single recorded completion on this rollup (completionId null) — verify a recorded dose from the verification queue."
    : undefined;

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 4 }}>
      <button
        type="button"
        disabled={!enabled || isPending}
        className="btn"
        title={disabledReason}
        aria-disabled={!enabled}
        style={{
          width: "100%",
          opacity: enabled ? 1 : 0.6,
          cursor: enabled && !isPending ? "pointer" : "default",
        }}
        onClick={() => {
          if (enabled && completionId) {
            setError(null);
            startTransition(async () => {
              const res = await acceptCompletionAction(completionId);
              if (!res.ok) {
                setError(res.error);
              }
            });
          }
        }}
      >
        {isPending ? "Accepting…" : "Accept"}
      </button>
      {disabledReason && <DisabledReason reason={disabledReason} />}
      {error && <ActionError message={error} />}
    </div>
  );
}

function RejectButton({ enabled, completionId }: CompletionButtonProps) {
  const [isPending, startTransition] = useTransition();
  const [arming, setArming] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const disabledReason = !enabled
    ? "No single recorded completion on this rollup (completionId null) — verify a recorded dose from the verification queue."
    : undefined;

  function confirmReject() {
    if (!enabled || !completionId) {
      return;
    }
    const trimmed = reason.trim();
    if (trimmed.length === 0) {
      setError("Rejection reason required — provide a reason for requiring rework.");
      return;
    }
    setError(null);
    startTransition(async () => {
      const res = await rejectCompletionAction(completionId, trimmed);
      if (!res.ok) {
        setError(res.error);
        return;
      }
      setArming(false);
      setReason("");
    });
  }

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 6 }}>
      {!arming ? (
        <button
          type="button"
          disabled={!enabled || isPending}
          className="btn"
          title={disabledReason}
          aria-disabled={!enabled}
          style={{
            width: "100%",
            opacity: enabled ? 1 : 0.6,
            cursor: enabled && !isPending ? "pointer" : "default",
          }}
          onClick={() => {
            if (enabled && completionId) {
              setError(null);
              setArming(true);
            }
          }}
        >
          Reject
        </button>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          <textarea
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="Reason for rejection (required)"
            rows={2}
            autoFocus
            disabled={isPending}
            style={{
              width: "100%",
              border: "1px solid var(--line2)",
              borderRadius: 6,
              padding: "8px 10px",
              fontSize: 14,
              background: "transparent",
              resize: "vertical",
            }}
          />
          <div style={{ display: "flex", gap: 6 }}>
            <button
              type="button"
              className="btn"
              disabled={isPending}
              onClick={confirmReject}
              style={{ flex: 1, cursor: isPending ? "default" : "pointer" }}
            >
              {isPending ? "Rejecting…" : "Confirm reject"}
            </button>
            <button
              type="button"
              className="btn"
              disabled={isPending}
              onClick={() => {
                setArming(false);
                setReason("");
                setError(null);
              }}
              style={{ flex: 1, cursor: isPending ? "default" : "pointer" }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}
      {disabledReason && <DisabledReason reason={disabledReason} />}
      {error && <ActionError message={error} />}
    </div>
  );
}
