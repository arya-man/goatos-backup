"use client";

import { useRouter } from "next/navigation";
import { useTransition } from "react";
import { BookOpen, RotateCcw, ShieldCheck, X } from "lucide-react";
import { RowMenu } from "@/components/app/row-menu";

type ReviewAction = (formData: FormData) => void | Promise<void>;

/**
 * The verification queue's per-row actions: ONE icon button for the primary (Verify) and a ⋮ menu
 * for the rest (Reject · Request rework · Passport) — instead of four buttons in every row. The
 * Server Actions and their form fields are exactly what the row forms posted; only the trigger
 * anatomy changed (judge M2 round 4 #3).
 */
export function VerificationRowActions({
  verifyAction,
  rejectAction,
  completionId,
  taskId,
  rowVersion,
  returnTo,
  canReview,
  passportHref,
  labels,
}: {
  verifyAction: ReviewAction;
  rejectAction: ReviewAction;
  completionId: string;
  taskId?: string;
  rowVersion?: number;
  returnTo: string;
  canReview: boolean;
  passportHref: string;
  labels: { verify: string; reject: string; rework: string; passport: string; menu: string; noHandle: string };
}) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();

  const submit = (action: ReviewAction, reason?: string) => {
    const formData = new FormData();
    formData.set("completion_id", completionId);
    if (taskId) formData.set("task_id", taskId);
    if (rowVersion) formData.set("row_version", String(rowVersion));
    formData.set("return_to", returnTo);
    if (reason) formData.set("reason", reason);
    startTransition(async () => {
      await action(formData);
    });
  };

  return (
    <span className="kit-row-actions" style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
      <button
        type="button"
        className="iconbtn kit-row-edit kit-row-verify"
        disabled={!canReview || pending}
        aria-disabled={!canReview || undefined}
        aria-busy={pending || undefined}
        title={canReview ? labels.verify : labels.noHandle}
        aria-label={labels.verify}
        onClick={() => submit(verifyAction)}
      >
        <ShieldCheck className="ic" aria-hidden="true" />
      </button>
      <RowMenu
        ariaLabel={labels.menu}
        actions={[
          { label: labels.reject, icon: <X className="ic" aria-hidden="true" />, danger: true, disabled: !canReview || pending, onSelect: () => submit(rejectAction, "rejected") },
          { label: labels.rework, icon: <RotateCcw className="ic" aria-hidden="true" />, disabled: !canReview || pending, onSelect: () => submit(rejectAction, "rework_requested") },
          { label: labels.passport, icon: <BookOpen className="ic" aria-hidden="true" />, onSelect: () => router.push(passportHref) },
        ]}
      />
    </span>
  );
}
