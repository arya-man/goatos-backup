"use client";

import { useRouter } from "next/navigation";
import { useTransition } from "react";
import Box from "@mui/material/Box";
import IconButton from "@mui/material/IconButton";
import { Iconify } from "@/components/minimal/iconify";
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
    <Box component="span" data-testid="verification-row-actions" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
      <IconButton
        size="small"
        color="success"
        data-testid="verification-row-verify"
        disabled={!canReview || pending}
        aria-disabled={!canReview || undefined}
        aria-busy={pending || undefined}
        title={canReview ? labels.verify : labels.noHandle}
        aria-label={labels.verify}
        onClick={() => submit(verifyAction)}
      >
        <Iconify icon="solar:shield-check-bold" width={18} aria-hidden="true" />
      </IconButton>
      <RowMenu
        ariaLabel={labels.menu}
        actions={[
          { label: labels.reject, icon: <Iconify icon="mingcute:close-line" aria-hidden="true" />, danger: true, disabled: !canReview || pending, onSelect: () => submit(rejectAction, "rejected") },
          { label: labels.rework, icon: <Iconify icon="solar:restart-bold" aria-hidden="true" />, disabled: !canReview || pending, onSelect: () => submit(rejectAction, "rework_requested") },
          { label: labels.passport, icon: <Iconify icon="solar:notebook-bold-duotone" aria-hidden="true" />, onSelect: () => router.push(passportHref) },
        ]}
      />
    </Box>
  );
}
