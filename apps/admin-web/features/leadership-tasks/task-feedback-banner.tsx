"use client";

import { AlertTriangle, CheckCircle2, X } from "lucide-react";
import { useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

import { statusChangeFeedbackFallback } from "./task-presentation";

/**
 * What every write on this page says when it lands.
 *
 * `actions.ts` has always stamped the outcome on the redirect URL and NOTHING read it: a status
 * change that came back 409 `version_conflict`, an attachment that failed to upload and a task
 * that was raised successfully all looked identical — the page simply re-rendered. This is the
 * reader.
 *
 * The code space is OPEN: it is whatever the backend's error vocabulary contains, so each code is
 * looked up as `feedback.<code>` with an explicit absent-value fallback (the one case `copy`
 * allows one) and an unrecognised code falls back to the contract's generic success/failure
 * sentence rather than rendering the raw token.
 */
export function TaskFeedbackBanner({
  pageContract,
  status,
  code,
}: {
  pageContract: AdminUiPageContract;
  status: string;
  code?: string;
}) {
  const [dismissed, setDismissed] = useState(false);
  if (dismissed) return null;
  const success = status === "success";
  // The fallback is no longer blank for the codes a status change can really return: three
  // different refusals reading as one generic failure is what made a bounced drag
  // unexplainable. The contract still overrides it whenever its owner authors `feedback.<code>`.
  const specific = code
    ? copy(pageContract, `feedback.${code}`, statusChangeFeedbackFallback(code))
    : "";
  const message =
    specific ||
    copy(pageContract, success ? "action.success_message" : "action.failed_message");
  return (
    <div
      className={`lt-banner${success ? " ok" : " bad"}`}
      role={success ? "status" : "alert"}
      aria-live="polite"
    >
      {success ? (
        <CheckCircle2 className="ic" aria-hidden="true" />
      ) : (
        <AlertTriangle className="ic" aria-hidden="true" />
      )}
      <div className="lt-banner-tx">
        <b>
          {success
            ? copy(pageContract, "action.success_tag")
            : copy(pageContract, "action.failed_title")}
        </b>
        <span>{message}</span>
      </div>
      <button
        type="button"
        className="btn sm"
        onClick={() => setDismissed(true)}
        aria-label={copy(pageContract, "action.close")}
      >
        <X className="ic" aria-hidden="true" />
      </button>
    </div>
  );
}
